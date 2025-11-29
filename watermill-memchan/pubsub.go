package memchan

import (
	"context"
	"sync"

	"github.com/joeycumines/go-bigbuff"
	"github.com/lithammer/shortuuid/v3"
	"github.com/pkg/errors"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
)

// Config holds the GoChannel Pub/Sub's configuration options.
type Config struct {
	// Output channel buffer size.
	OutputChannelBuffer int64

	// If persistent is set to true, when subscriber subscribes to the topic,
	// it will receive all previously produced messages.
	Persistent bool

	// When true, Publish will block until subscriber Ack's the message.
	BlockPublishUntilSubscriberAck bool

	// PreserveContext determines if message context should be preserved.
	PreserveContext bool
}

// GoChannel is a Pub/Sub implementation using go-bigbuff.ChanPubSub.
type GoChannel struct {
	config Config
	logger watermill.LoggerAdapter

	topics     map[string]*topic
	topicsLock sync.RWMutex

	closed     bool
	closedLock sync.Mutex
	closing    chan struct{}

	subscribersWg sync.WaitGroup
}

type topic struct {
	pubSub *bigbuff.ChanPubSub[chan *message.Message, *message.Message]
	ch     chan *message.Message

	subscribers     []*subscriber
	subscribersLock sync.RWMutex

	persistedMessages []*message.Message
	persistedLock     sync.RWMutex

	closedOnce sync.Once
}

type subscriber struct {
	uuid          string
	ctx           context.Context
	outputChannel chan *message.Message
	closing       chan struct{}
	closed        bool
	closedLock    sync.Mutex

	logger          watermill.LoggerAdapter
	preserveContext bool
	blockOnAck      bool

	topic     *topic
	gochannel *GoChannel
}

// NewGoChannel creates new GoChannel Pub/Sub.
func NewGoChannel(config Config, logger watermill.LoggerAdapter) *GoChannel {
	if logger == nil {
		logger = watermill.NopLogger{}
	}

	return &GoChannel{
		config: config,
		logger: logger.With(watermill.LogFields{"pubsub_uuid": shortuuid.New()}),
		topics: make(map[string]*topic),
		closing: make(chan struct{}),
	}
}

func (g *GoChannel) getOrCreateTopic(topicName string) *topic {
	g.topicsLock.RLock()
	t, ok := g.topics[topicName]
	g.topicsLock.RUnlock()
	if ok {
		return t
	}

	g.topicsLock.Lock()
	defer g.topicsLock.Unlock()

	if t, ok = g.topics[topicName]; ok {
		return t
	}

	ch := make(chan *message.Message)
	t = &topic{
		pubSub:      bigbuff.NewChanPubSub(ch),
		ch:          ch,
		subscribers: make([]*subscriber, 0),
	}
	g.topics[topicName] = t
	return t
}

// Publish publishes messages to the topic.
func (g *GoChannel) Publish(topicName string, messages ...*message.Message) error {
	select {
	case <-g.closing:
		return errors.New("Pub/Sub closed")
	default:
	}

	t := g.getOrCreateTopic(topicName)

	for _, msg := range messages {
		var msgToPublish *message.Message
		if g.config.PreserveContext {
			msgToPublish = msg.CopyWithContext()
		} else {
			msgToPublish = msg.Copy()
		}

		if g.config.Persistent {
			t.persistedLock.Lock()
			t.persistedMessages = append(t.persistedMessages, msgToPublish)
			t.persistedLock.Unlock()
		}

		t.subscribersLock.RLock()
		subCount := len(t.subscribers)
		t.subscribersLock.RUnlock()

		if subCount == 0 {
			continue
		}

		if g.config.BlockPublishUntilSubscriberAck {
			t.pubSub.Send(msgToPublish)
		} else {
			g.publishBuffered(t, msgToPublish)
		}
	}

	return nil
}

func (g *GoChannel) publishBuffered(t *topic, msg *message.Message) {
	t.subscribersLock.RLock()
	subscribers := make([]*subscriber, len(t.subscribers))
	copy(subscribers, t.subscribers)
	t.subscribersLock.RUnlock()

	var wg sync.WaitGroup
	wg.Add(len(subscribers))

	for _, sub := range subscribers {
		go func(s *subscriber) {
			defer wg.Done()
			s.sendBuffered(msg)
		}(sub)
	}

	wg.Wait()
}

// Subscribe returns channel to which all published messages are sent.
func (g *GoChannel) Subscribe(ctx context.Context, topicName string) (<-chan *message.Message, error) {
	g.closedLock.Lock()

	if g.closed {
		g.closedLock.Unlock()
		return nil, errors.New("Pub/Sub closed")
	}

	g.subscribersWg.Add(1)
	g.closedLock.Unlock()

	t := g.getOrCreateTopic(topicName)

	s := &subscriber{
		uuid:            watermill.NewUUID(),
		ctx:             ctx,
		outputChannel:   make(chan *message.Message, g.config.OutputChannelBuffer),
		closing:         make(chan struct{}),
		logger:          g.logger,
		preserveContext: g.config.PreserveContext,
		blockOnAck:      g.config.BlockPublishUntilSubscriberAck,
		topic:           t,
		gochannel:       g,
	}

	t.subscribersLock.Lock()
	t.subscribers = append(t.subscribers, s)
	if g.config.BlockPublishUntilSubscriberAck {
		t.pubSub.Subscribe()
	}
	t.subscribersLock.Unlock()

	if g.config.BlockPublishUntilSubscriberAck {
		go s.blockingReceiver()
	}

	if g.config.Persistent {
		go s.sendPersistedMessages()
	}

	go func() {
		select {
		case <-ctx.Done():
		case <-g.closing:
		}
		s.close()
	}()

	return s.outputChannel, nil
}

// blockingReceiver - hot path for BlockPublishUntilSubscriberAck mode
func (s *subscriber) blockingReceiver() {
	ps := s.topic.pubSub
	out := s.outputChannel
	closing := s.closing
	gClosing := s.gochannel.closing

	for {
		select {
		case <-closing:
			return
		case <-gClosing:
			return
		case msg, ok := <-ps.C():
			if !ok {
				return
			}

			// Forward and handle Ack/Nack with retry
			s.forwardWithAck(msg, ps, out, closing, gClosing)
		}
	}
}

// forwardWithAck handles message forwarding with Nack retry support
func (s *subscriber) forwardWithAck(msg *message.Message, ps *bigbuff.ChanPubSub[chan *message.Message, *message.Message], out chan *message.Message, closing, gClosing chan struct{}) {
	for {
		select {
		case out <- msg:
		case <-closing:
			ps.Wait()
			return
		case <-gClosing:
			ps.Wait()
			return
		}

		select {
		case <-msg.Acked():
			ps.Wait()
			return
		case <-msg.Nacked():
			msg = msg.Copy()
			continue
		case <-closing:
			ps.Wait()
			return
		case <-gClosing:
			ps.Wait()
			return
		}
	}
}

func (s *subscriber) sendBuffered(msg *message.Message) {
	select {
	case <-s.closing:
		return
	default:
	}

	ctx := msg.Context()
	if !s.preserveContext {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(s.ctx)
		defer cancel()
	}

	for {
		msgToSend := msg.Copy()
		msgToSend.SetContext(ctx)

		select {
		case <-s.closing:
			return
		case <-s.gochannel.closing:
			return
		case s.outputChannel <- msgToSend:
		}

		select {
		case <-msgToSend.Acked():
			return
		case <-msgToSend.Nacked():
			continue
		case <-s.closing:
			return
		case <-s.gochannel.closing:
			return
		}
	}
}

func (s *subscriber) sendPersistedMessages() {
	t := s.topic

	t.persistedLock.RLock()
	msgs := make([]*message.Message, len(t.persistedMessages))
	copy(msgs, t.persistedMessages)
	t.persistedLock.RUnlock()

	for _, msg := range msgs {
		select {
		case <-s.closing:
			return
		default:
		}

		select {
		case <-s.closing:
			return
		case <-s.gochannel.closing:
			return
		case s.outputChannel <- msg.Copy():
		}
	}
}

func (s *subscriber) close() {
	s.closedLock.Lock()
	if s.closed {
		s.closedLock.Unlock()
		return
	}
	s.closed = true
	close(s.closing)
	s.closedLock.Unlock()

	t := s.topic
	g := s.gochannel

	t.subscribersLock.Lock()
	if s.blockOnAck {
		t.pubSub.Unsubscribe()
	}
	for i, sub := range t.subscribers {
		if sub == s {
			t.subscribers = append(t.subscribers[:i], t.subscribers[i+1:]...)
			break
		}
	}
	t.subscribersLock.Unlock()

	close(s.outputChannel)
	g.subscribersWg.Done()
}

func (g *GoChannel) isClosed() bool {
	g.closedLock.Lock()
	defer g.closedLock.Unlock()
	return g.closed
}

// Close closes the GoChannel Pub/Sub.
func (g *GoChannel) Close() error {
	g.closedLock.Lock()
	defer g.closedLock.Unlock()

	if g.closed {
		return nil
	}

	g.closed = true
	close(g.closing)

	g.topicsLock.Lock()
	for _, t := range g.topics {
		t.closedOnce.Do(func() {
			close(t.ch)
		})
	}
	g.topicsLock.Unlock()

	g.subscribersWg.Wait()
	return nil
}
