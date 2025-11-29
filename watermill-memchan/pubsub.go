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
	//
	// All messages are persisted to the memory (simple slice),
	// so be aware that with large amount of messages you can go out of the memory.
	Persistent bool

	// When true, Publish will block until subscriber Ack's the message.
	// If there are no subscribers, Publish will not block (also when Persistent is true).
	BlockPublishUntilSubscriberAck bool

	// PreserveContext is a flag that determines if the context should be preserved when sending messages to subscribers.
	// This behavior is different from other implementations of Publishers where data travels over the network,
	// hence context can't be preserved in those cases
	PreserveContext bool
}

// GoChannel is a high-performance Pub/Sub implementation that uses go-bigbuff.ChanPubSub.
// It provides the same API as github.com/ThreeDotsLabs/watermill/pubsub/gochannel.
//
// When BlockPublishUntilSubscriberAck is true, it leverages ChanPubSub for efficient
// broadcasting to all subscribers. Otherwise, it uses a traditional per-subscriber model.
//
// GoChannel has no global state,
// that means that you need to use the same instance for Publishing and Subscribing!
//
// When GoChannel is persistent, messages order is not guaranteed.
type GoChannel struct {
	config Config
	logger watermill.LoggerAdapter

	subscribersWg          sync.WaitGroup
	subscribers            map[string][]*subscriber
	subscribersLock        sync.RWMutex
	subscribersByTopicLock sync.Map // map of *sync.Mutex

	closed     bool
	closedLock sync.Mutex
	closing    chan struct{}

	persistedMessages     map[string][]*message.Message
	persistedMessagesLock sync.RWMutex

	// ChanPubSub per topic for BlockPublishUntilSubscriberAck mode
	topicBuses     map[string]*topicBus
	topicBusesLock sync.RWMutex
}

// topicBus holds ChanPubSub for a topic (used in BlockPublishUntilSubscriberAck mode)
type topicBus struct {
	pubSub     *bigbuff.ChanPubSub[chan *message.Message, *message.Message]
	ch         chan *message.Message
	closed     bool
	closedOnce sync.Once
	mu         sync.Mutex
}

// NewGoChannel creates new GoChannel Pub/Sub.
//
// This GoChannel is not persistent.
// That means if you send a message to a topic to which no subscriber is subscribed, that message will be discarded.
func NewGoChannel(config Config, logger watermill.LoggerAdapter) *GoChannel {
	if logger == nil {
		logger = watermill.NopLogger{}
	}

	return &GoChannel{
		config: config,

		subscribers:            make(map[string][]*subscriber),
		subscribersByTopicLock: sync.Map{},
		logger: logger.With(
			watermill.LogFields{
				"pubsub_uuid": shortuuid.New(),
			},
		),

		closing: make(chan struct{}),

		persistedMessages: map[string][]*message.Message{},
		topicBuses:        make(map[string]*topicBus),
	}
}

// getOrCreateTopicBus gets or creates a topic bus for BlockPublishUntilSubscriberAck mode
func (g *GoChannel) getOrCreateTopicBus(topic string) *topicBus {
	g.topicBusesLock.RLock()
	bus, ok := g.topicBuses[topic]
	g.topicBusesLock.RUnlock()
	if ok {
		return bus
	}

	g.topicBusesLock.Lock()
	defer g.topicBusesLock.Unlock()

	if bus, ok = g.topicBuses[topic]; ok {
		return bus
	}

	ch := make(chan *message.Message)
	bus = &topicBus{
		pubSub: bigbuff.NewChanPubSub(ch),
		ch:     ch,
	}
	g.topicBuses[topic] = bus
	return bus
}

// Publish in GoChannel is NOT blocking until all consumers consume.
// Messages will be sent in background.
//
// Messages may be persisted or not, depending on persistent attribute.
func (g *GoChannel) Publish(topic string, messages ...*message.Message) error {
	if g.isClosed() {
		return errors.New("Pub/Sub closed")
	}

	messagesToPublish := make(message.Messages, len(messages))
	for i, msg := range messages {
		if g.config.PreserveContext {
			messagesToPublish[i] = msg.CopyWithContext()
		} else {
			messagesToPublish[i] = msg.Copy()
		}
	}

	g.subscribersLock.RLock()
	defer g.subscribersLock.RUnlock()

	subLock, loaded := g.subscribersByTopicLock.LoadOrStore(topic, &sync.Mutex{})
	subLock.(*sync.Mutex).Lock()

	if !loaded {
		defer g.subscribersByTopicLock.Delete(topic)
	}
	defer subLock.(*sync.Mutex).Unlock()

	if g.config.Persistent {
		g.persistedMessagesLock.Lock()
		if _, ok := g.persistedMessages[topic]; !ok {
			g.persistedMessages[topic] = make([]*message.Message, 0)
		}
		g.persistedMessages[topic] = append(g.persistedMessages[topic], messagesToPublish...)
		g.persistedMessagesLock.Unlock()
	}

	for i := range messagesToPublish {
		msg := messagesToPublish[i]

		if g.config.BlockPublishUntilSubscriberAck {
			// Use ChanPubSub for efficient blocking broadcast
			g.publishWithChanPubSub(topic, msg)
		} else {
			// Use traditional per-subscriber sending
			ackedBySubscribers, err := g.sendMessage(topic, msg)
			if err != nil {
				return err
			}
			// Don't wait for ack in non-blocking mode
			_ = ackedBySubscribers
		}
	}

	return nil
}

// publishWithChanPubSub uses ChanPubSub for efficient blocking broadcast
func (g *GoChannel) publishWithChanPubSub(topic string, msg *message.Message) {
	logFields := watermill.LogFields{"message_uuid": msg.UUID, "topic": topic}

	subscribers := g.topicSubscribers(topic)
	if len(subscribers) == 0 {
		g.logger.Info("No subscribers to send message", logFields)
		return
	}

	bus := g.getOrCreateTopicBus(topic)

	// Send via ChanPubSub - this blocks until all subscribers call Wait()
	sent := bus.pubSub.Send(msg)

	if sent != len(subscribers) {
		g.logger.Debug("Subscriber count mismatch", watermill.LogFields{
			"message_uuid": msg.UUID,
			"expected":     len(subscribers),
			"sent":         sent,
		})
	}

	g.logger.Trace("Message sent via ChanPubSub", logFields)
}

func (g *GoChannel) sendMessage(topic string, message *message.Message) (<-chan struct{}, error) {
	subscribers := g.topicSubscribers(topic)
	ackedBySubscribers := make(chan struct{})

	logFields := watermill.LogFields{"message_uuid": message.UUID, "topic": topic}

	if len(subscribers) == 0 {
		close(ackedBySubscribers)
		g.logger.Info("No subscribers to send message", logFields)
		return ackedBySubscribers, nil
	}

	go func(subscribers []*subscriber) {
		wg := &sync.WaitGroup{}

		for i := range subscribers {
			subscriber := subscribers[i]

			wg.Add(1)
			go func() {
				subscriber.sendMessageToSubscriber(message, logFields)
				wg.Done()
			}()
		}

		wg.Wait()
		close(ackedBySubscribers)
	}(subscribers)

	return ackedBySubscribers, nil
}

// Subscribe returns channel to which all published messages are sent.
// Messages are not persisted. If there are no subscribers and message is produced it will be gone.
//
// There are no consumer groups support etc. Every consumer will receive every produced message.
func (g *GoChannel) Subscribe(ctx context.Context, topic string) (<-chan *message.Message, error) {
	g.closedLock.Lock()

	if g.closed {
		g.closedLock.Unlock()
		return nil, errors.New("Pub/Sub closed")
	}

	g.subscribersWg.Add(1)
	g.closedLock.Unlock()

	g.subscribersLock.Lock()

	subLock, _ := g.subscribersByTopicLock.LoadOrStore(topic, &sync.Mutex{})
	subLock.(*sync.Mutex).Lock()

	s := &subscriber{
		ctx:             ctx,
		uuid:            watermill.NewUUID(),
		outputChannel:   make(chan *message.Message, g.config.OutputChannelBuffer),
		logger:          g.logger,
		closing:         make(chan struct{}),
		preserveContext: g.config.PreserveContext,
		blockOnAck:      g.config.BlockPublishUntilSubscriberAck,
	}

	// If using BlockPublishUntilSubscriberAck, register with ChanPubSub
	if g.config.BlockPublishUntilSubscriberAck {
		bus := g.getOrCreateTopicBus(topic)
		bus.pubSub.Subscribe()
		s.topicBus = bus

		// Start the ChanPubSub consumer goroutine
		go s.consumeFromChanPubSub(ctx, g, topic)
	}

	go func(s *subscriber, g *GoChannel) {
		select {
		case <-ctx.Done():
			// unblock
		case <-g.closing:
			// unblock
		}

		s.Close()

		g.subscribersLock.Lock()
		defer g.subscribersLock.Unlock()

		subLock, _ := g.subscribersByTopicLock.Load(topic)
		subLock.(*sync.Mutex).Lock()
		defer subLock.(*sync.Mutex).Unlock()

		g.removeSubscriber(topic, s)
		g.subscribersWg.Done()
	}(s, g)

	if !g.config.Persistent {
		defer g.subscribersLock.Unlock()
		defer subLock.(*sync.Mutex).Unlock()

		g.addSubscriber(topic, s)

		return s.outputChannel, nil
	}

	go func(s *subscriber) {
		defer g.subscribersLock.Unlock()
		defer subLock.(*sync.Mutex).Unlock()

		g.persistedMessagesLock.RLock()
		messages, ok := g.persistedMessages[topic]
		g.persistedMessagesLock.RUnlock()

		if ok {
			for i := range messages {
				msg := g.persistedMessages[topic][i]
				logFields := watermill.LogFields{"message_uuid": msg.UUID, "topic": topic}

				go s.sendMessageToSubscriber(msg, logFields)
			}
		}

		g.addSubscriber(topic, s)
	}(s)

	return s.outputChannel, nil
}

func (g *GoChannel) addSubscriber(topic string, s *subscriber) {
	if _, ok := g.subscribers[topic]; !ok {
		g.subscribers[topic] = make([]*subscriber, 0)
	}
	g.subscribers[topic] = append(g.subscribers[topic], s)
}

func (g *GoChannel) removeSubscriber(topic string, toRemove *subscriber) {
	removed := false
	for i, sub := range g.subscribers[topic] {
		if sub == toRemove {
			g.subscribers[topic] = append(g.subscribers[topic][:i], g.subscribers[topic][i+1:]...)
			removed = true

			if len(g.subscribers[topic]) == 0 && !g.config.Persistent {
				delete(g.subscribers, topic)
				g.subscribersByTopicLock.Delete(topic)
			}
			break
		}
	}
	if !removed {
		panic("cannot remove subscriber, not found " + toRemove.uuid)
	}

	// Unsubscribe from ChanPubSub if applicable
	if toRemove.topicBus != nil {
		toRemove.topicBus.pubSub.Unsubscribe()
	}
}

func (g *GoChannel) topicSubscribers(topic string) []*subscriber {
	subscribers, ok := g.subscribers[topic]
	if !ok {
		return nil
	}

	subscribersCopy := make([]*subscriber, len(subscribers))
	copy(subscribersCopy, subscribers)

	return subscribersCopy
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

	g.logger.Debug("Closing Pub/Sub, waiting for subscribers", nil)

	// Close all ChanPubSub channels
	g.topicBusesLock.Lock()
	for _, bus := range g.topicBuses {
		bus.closedOnce.Do(func() {
			bus.closed = true
			close(bus.ch)
		})
	}
	g.topicBusesLock.Unlock()

	g.subscribersWg.Wait()

	g.logger.Info("Pub/Sub closed", nil)
	g.persistedMessages = nil

	return nil
}

type subscriber struct {
	ctx context.Context

	uuid string

	sending       sync.Mutex
	outputChannel chan *message.Message

	logger  watermill.LoggerAdapter
	closed  bool
	closing chan struct{}

	preserveContext bool
	blockOnAck      bool

	// For BlockPublishUntilSubscriberAck mode
	topicBus *topicBus
}

func (s *subscriber) Close() {
	if s.closed {
		return
	}
	close(s.closing)

	s.logger.Debug("Closing subscriber, waiting for sending lock", nil)

	s.sending.Lock()
	defer s.sending.Unlock()

	s.logger.Debug("GoChannel Pub/Sub Subscriber closed", nil)
	s.closed = true

	close(s.outputChannel)
}

// consumeFromChanPubSub consumes messages from ChanPubSub and handles Ack/Nack
func (s *subscriber) consumeFromChanPubSub(ctx context.Context, g *GoChannel, topic string) {
	bus := s.topicBus

	for {
		select {
		case <-ctx.Done():
			return
		case <-g.closing:
			return
		case <-s.closing:
			return
		case msg, ok := <-bus.pubSub.C():
			if !ok {
				return
			}

			logFields := watermill.LogFields{"message_uuid": msg.UUID, "topic": topic}

			// Handle Ack/Nack with retry
			s.processMessageFromChanPubSub(msg, logFields)

			// Signal to ChanPubSub that we're done - this unblocks the publisher
			bus.pubSub.Wait()
		}
	}
}

// processMessageFromChanPubSub handles a message with Ack/Nack retry logic
// sendMessageWithAckHandling handles message delivery with Ack/Nack retry logic.
// This is the core message handling logic used by both ChanPubSub mode and traditional mode.
func (s *subscriber) sendMessageWithAckHandling(msg *message.Message, logFields watermill.LogFields) {
	ctx := msg.Context()

	if !s.preserveContext {
		var cancelCtx context.CancelFunc
		ctx, cancelCtx = context.WithCancel(s.ctx)
		defer cancelCtx()
	}

SendToSubscriber:
	for {
		msgToSend := msg.Copy()
		msgToSend.SetContext(ctx)

		s.logger.Trace("Sending msg to subscriber", logFields)

		s.sending.Lock()
		if s.closed {
			s.logger.Info("Pub/Sub closed, discarding msg", logFields)
			s.sending.Unlock()
			return
		}

		select {
		case s.outputChannel <- msgToSend:
			s.logger.Trace("Sent message to subscriber", logFields)
		case <-s.closing:
			s.logger.Trace("Closing, message discarded", logFields)
			s.sending.Unlock()
			return
		}
		s.sending.Unlock()

		select {
		case <-msgToSend.Acked():
			s.logger.Trace("Message acked", logFields)
			return
		case <-msgToSend.Nacked():
			s.logger.Trace("Nack received, resending message", logFields)
			continue SendToSubscriber
		case <-s.closing:
			s.logger.Trace("Closing, message discarded", logFields)
			return
		}
	}
}

// processMessageFromChanPubSub handles a message received from ChanPubSub
func (s *subscriber) processMessageFromChanPubSub(msg *message.Message, logFields watermill.LogFields) {
	s.sendMessageWithAckHandling(msg, logFields)
}

// sendMessageToSubscriber sends a message to the subscriber (traditional mode)
func (s *subscriber) sendMessageToSubscriber(msg *message.Message, logFields watermill.LogFields) {
	s.sendMessageWithAckHandling(msg, logFields)
}
