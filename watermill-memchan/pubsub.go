package memchan

import (
	"context"
	"sync"
	"sync/atomic"

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

	// PreserveContext is a flag that determines if the context should be preserved.
	PreserveContext bool
}

// GoChannel is a high-performance Pub/Sub implementation built on go-bigbuff.ChanPubSub.
type GoChannel struct {
	config Config
	logger watermill.LoggerAdapter

	topics     map[string]*topic
	topicsLock sync.RWMutex

	closed     int32 // atomic
	closing    chan struct{}

	subscribersWg sync.WaitGroup
}

// topic holds ChanPubSub state for a single topic
type topic struct {
	pubSub *bigbuff.ChanPubSub[chan *message.Message, *message.Message]
	ch     chan *message.Message

	subscribers     []*subscriber
	subscribersLock sync.RWMutex

	persistedMessages []*message.Message
	persistedLock     sync.RWMutex

	closedOnce sync.Once
}

// subscriber wraps a ChanPubSub subscription
type subscriber struct {
	outputChannel chan *message.Message
	ctx           context.Context
	closing       chan struct{}
	closed        int32 // atomic

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
		logger: logger.With(
			watermill.LogFields{
				"pubsub_uuid": shortuuid.New(),
			},
		),
		topics:  make(map[string]*topic),
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
	if atomic.LoadInt32(&g.closed) == 1 {
		return errors.New("Pub/Sub closed")
	}

	t := g.getOrCreateTopic(topicName)

	for _, msg := range messages {
		// Copy message once
		var msgToPublish *message.Message
		if g.config.PreserveContext {
			msgToPublish = msg.CopyWithContext()
		} else {
			msgToPublish = msg.Copy()
		}

		// Persist if configured
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

		// ChanPubSub.Send() - O(1) broadcast
		t.pubSub.Send(msgToPublish)
	}

	return nil
}

// Subscribe returns channel to which all published messages are sent.
func (g *GoChannel) Subscribe(ctx context.Context, topicName string) (<-chan *message.Message, error) {
	if atomic.LoadInt32(&g.closed) == 1 {
		return nil, errors.New("Pub/Sub closed")
	}

	g.subscribersWg.Add(1)

	t := g.getOrCreateTopic(topicName)

	s := &subscriber{
		outputChannel:   make(chan *message.Message, g.config.OutputChannelBuffer),
		ctx:             ctx,
		closing:         make(chan struct{}),
		logger:          g.logger,
		preserveContext: g.config.PreserveContext,
		blockOnAck:      g.config.BlockPublishUntilSubscriberAck,
		topic:           t,
		gochannel:       g,
	}

	// Register with ChanPubSub
	t.subscribersLock.Lock()
	t.pubSub.Subscribe()
	t.subscribers = append(t.subscribers, s)
	t.subscribersLock.Unlock()

	// Start receiver goroutine
	go s.receiveLoop()

	// Handle persistent messages
	if g.config.Persistent {
		go s.sendPersistedMessages()
	}

	// Cleanup goroutine
	go func() {
		select {
		case <-ctx.Done():
		case <-g.closing:
		}
		s.close()
	}()

	return s.outputChannel, nil
}

// receiveLoop is the hot path - optimized for performance
func (s *subscriber) receiveLoop() {
	t := s.topic
	ps := t.pubSub
	g := s.gochannel
	blockOnAck := s.blockOnAck
	preserveCtx := s.preserveContext

	for {
		select {
		case <-s.closing:
			return
		case <-g.closing:
			return
		case msg, ok := <-ps.C():
			if !ok {
				return
			}

			if blockOnAck {
				s.handleBlocking(msg, preserveCtx, ps)
			} else {
				s.handleBuffered(msg, preserveCtx, ps)
			}
		}
	}
}

// handleBlocking - optimized blocking path
func (s *subscriber) handleBlocking(msg *message.Message, preserveCtx bool, ps *bigbuff.ChanPubSub[chan *message.Message, *message.Message]) {
	g := s.gochannel

	ctx := msg.Context()
	if !preserveCtx {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(s.ctx)
		defer cancel()
	}

	for {
		msgToSend := msg.Copy()
		msgToSend.SetContext(ctx)

		select {
		case <-s.closing:
			ps.Wait()
			return
		case <-g.closing:
			ps.Wait()
			return
		case s.outputChannel <- msgToSend:
		}

		select {
		case <-msgToSend.Acked():
			ps.Wait()
			return
		case <-msgToSend.Nacked():
			continue
		case <-s.closing:
			ps.Wait()
			return
		case <-g.closing:
			ps.Wait()
			return
		}
	}
}

// handleBuffered - optimized buffered path
func (s *subscriber) handleBuffered(msg *message.Message, preserveCtx bool, ps *bigbuff.ChanPubSub[chan *message.Message, *message.Message]) {
	g := s.gochannel

	ctx := msg.Context()
	if !preserveCtx {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(s.ctx)
		defer cancel()
	}

	msgToSend := msg.Copy()
	msgToSend.SetContext(ctx)

	select {
	case <-s.closing:
		ps.Wait()
		return
	case <-g.closing:
		ps.Wait()
		return
	case s.outputChannel <- msgToSend:
	}

	ps.Wait()
}

// sendPersistedMessages sends persisted messages to a new subscriber
func (s *subscriber) sendPersistedMessages() {
	t := s.topic

	t.persistedLock.RLock()
	msgs := make([]*message.Message, len(t.persistedMessages))
	copy(msgs, t.persistedMessages)
	t.persistedLock.RUnlock()

	for _, msg := range msgs {
		if atomic.LoadInt32(&s.closed) == 1 {
			return
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

// close cleans up the subscriber
func (s *subscriber) close() {
	if !atomic.CompareAndSwapInt32(&s.closed, 0, 1) {
		return
	}
	close(s.closing)

	t := s.topic
	g := s.gochannel

	t.subscribersLock.Lock()
	t.pubSub.Unsubscribe()
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
	return atomic.LoadInt32(&g.closed) == 1
}

// Close closes the GoChannel Pub/Sub.
func (g *GoChannel) Close() error {
	if !atomic.CompareAndSwapInt32(&g.closed, 0, 1) {
		return nil
	}

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
