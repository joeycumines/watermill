package main

import (
	"context"
	"math/rand/v2"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
)

func Benchmark_highContention(b *testing.B) {
	b.ReportAllocs()

	const numReceivers = 150_000

	c := gochannel.NewGoChannel(
		gochannel.Config{BlockPublishUntilSubscriberAck: true},
		watermill.NopLogger{},
	)
	defer c.Close()

	topicName := "test"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var allStopped sync.WaitGroup
	allStopped.Add(numReceivers)

	var count atomic.Int32

	for i := 0; i < numReceivers; i++ {
		messages, err := c.Subscribe(ctx, topicName)
		if err != nil {
			b.Fatal(err)
		}

		go func() {
			defer allStopped.Done()
			var n int
			//defer func() { b.Logf(`received %d messages`, n) }()
			for {
				msg := <-messages
				if msg == nil {
					break
				}
				count.Add(1)
				n++
				if !msg.Ack() {
					b.Error(`msg.Ack() returned false`)
					cancel()
					panic(`msg.Ack() returned false`)
				}
			}
		}()
	}

	msg := message.NewMessage("1", nil)

	b.Logf(`publishing %d messages`, b.N)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := c.Publish(topicName, msg)
		if err != nil {
			b.Fatal(err)
		}
		bmrInt++
	}
	b.StopTimer()

	countBefore := int(count.Load())
	bmrInt += countBefore
	b.Logf(`closing, received %d messages total across %d consumers`, countBefore, numReceivers)

	if err := c.Close(); err != nil {
		b.Fatal(err)
	}
	allStopped.Wait()

	countAfter := int(count.Load())

	if countAfter != b.N*numReceivers {
		b.Errorf(`expected %d messages, got %d`, b.N*numReceivers, countAfter)
	}
	if countBefore != countAfter {
		b.Errorf(`received %d messages, but was %d prior to close - should have blocked on acK?`, countAfter, countBefore)
	}
}

// Benchmark_Production_EventBus_Buffered simulates a realistic, high-performance
// application bus (e.g., a Modular Monolith) where:
// 1. We have a reasonable number of distinct domain subscribers (e.g., 10).
// 2. We use Buffered channels to decouple the Publisher from Subscribers (High Throughput).
// 3. We simulate realistic message payloads (1KB), not empty signals.
// 4. We measure the full lifecycle: Publish -> Route -> Process -> Ack.
func Benchmark_Production_EventBus_Buffered(b *testing.B) {
	// 1. Production Config:
	// We use a buffer. This allows Publish to return quickly even if subscribers
	// are momentarily busy. This is critical for "Valid, Production" performance.
	const (
		numSubscribers = 10   // e.g., Audit, Log, Email, Analytics, CacheInvalidator, etc.
		channelBuffer  = 1000 // Give the bus some breathing room
		payloadSize    = 1024 // 1KB payload (simulating JSON/Protobuf)
	)

	// Setup Watermill GoChannel
	c := gochannel.NewGoChannel(
		gochannel.Config{
			// In production high-perf scenarios, we usually want non-blocking publish
			// relying on the buffer. If the buffer is full, it yields error or blocks
			// depending on business logic. Here we block if buffer full to ensure data safety
			// but rely on buffer for speed.
			BlockPublishUntilSubscriberAck: false,
			OutputChannelBuffer:            channelBuffer,
		},
		watermill.NopLogger{},
	)
	defer c.Close()

	topicName := "order_created"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 2. Setup Subscribers
	// We need to wait for ALL subscribers to process ALL messages to get a fair
	// End-to-End benchmark.
	var consumersWg sync.WaitGroup
	consumersWg.Add(numSubscribers * b.N)

	// Pre-allocate a payload to simulate data cost without benchmarking `make()`
	payload := make([]byte, payloadSize)

	// Start Subscribers
	for i := 0; i < numSubscribers; i++ {
		msgs, err := c.Subscribe(ctx, topicName)
		if err != nil {
			b.Fatal(err)
		}

		go func(subID int) {
			for msg := range msgs {
				// Simulate standard overhead (extracting payload)
				if len(msg.Payload) == 0 {
					b.Error("Empty payload received")
				}
				msg.Ack()
				consumersWg.Done()
			}
		}(i)
	}

	// 3. The Benchmark Loop
	b.ResetTimer()
	b.ReportAllocs()

	// We run the publisher in the main loop, but we must ensure we don't exit
	// before consumers finish.
	for i := 0; i < b.N; i++ {
		// In production, we generate unique IDs per message.
		// We allocate the message struct here to capture the allocation cost,
		// as reusing the same struct pointer across 150k calls is unrealistic.
		msg := message.NewMessage(watermill.NewUUID(), payload)

		if err := c.Publish(topicName, msg); err != nil {
			b.Fatal(err)
		}
	}

	// 4. Wait for Drain
	// The benchmark isn't "done" until the consumers have Acked.
	consumersWg.Wait()
}

// Benchmark_Production_EventBus_StrictSync simulates a strict consistency requirement.
// Even in memory, sometimes you need to know *for sure* that all subscribers handled
// the event before the HTTP handler returns (e.g., Database Transactional Integrity).
// This matches your original 'BlockPublishUntilSubscriberAck: true' but with
// realistic subscriber counts.
func Benchmark_Production_EventBus_StrictSync(b *testing.B) {
	const (
		numSubscribers = 10
		payloadSize    = 1024
	)

	c := gochannel.NewGoChannel(
		gochannel.Config{
			// The Publisher will BLOCK until all 10 subscribers have Acked.
			BlockPublishUntilSubscriberAck: true,
			OutputChannelBuffer:            0, // Buffer is irrelevant in strict sync mode
		},
		watermill.NopLogger{},
	)
	defer c.Close()

	topicName := "user_signup_strict"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	payload := make([]byte, payloadSize)

	// We don't need a massive WaitGroup here for correctness because Publish blocks,
	// but we still need the subscribers running.
	for i := 0; i < numSubscribers; i++ {
		msgs, err := c.Subscribe(ctx, topicName)
		if err != nil {
			b.Fatal(err)
		}

		go func() {
			for msg := range msgs {
				msg.Ack()
			}
		}()
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		msg := message.NewMessage(watermill.NewUUID(), payload)
		if err := c.Publish(topicName, msg); err != nil {
			b.Fatal(err)
		}
	}
}

// Benchmark_HighLoad_FanOut_Burst simulates a high-throughput "Firehose" scenario
// where messages are produced in bursts (batches) and fanned out to multiple workers.
//
// Scenario:
// - Fan-Out: Every message published is sent to ALL subscribers (Broadcasting).
// - Bursting: Every benchmark iteration sends a batch of messages (`msgsPerOp`).
// - GC Stress: High rate of allocations for payloads and metadata.
func Benchmark_HighLoad_FanOut_Burst(b *testing.B) {
	// 1. Configuration
	const (
		// The size of the message body (simulating JSON/Protobuf)
		payloadSize = 1024 // 1KB

		// Multiplier: How many messages to send per single benchmark operation (b.N).
		// This simulates a "batch" or "burst" of requests.
		// N.B. the ns/op 100_000 vs 1_000 messages was almost exactly linear.
		msgsPerOp = 100_000

		// How many subscribers will receive EVERY message.
		// Total ops = b.N * msgsPerOp * subscriberCount
		subscriberCount = 8

		// Buffer size per subscriber channel.
		// Large enough to absorb bursts, but small enough to eventually force backpressure.
		bufferSize = 2000
	)

	// Setup Watermill GoChannel
	pubSub := gochannel.NewGoChannel(
		gochannel.Config{
			// OutputChannelBuffer: The buffer size for *each* subscriber channel.
			OutputChannelBuffer: bufferSize,

			// BlockPublishUntilSubscriberAck:
			// If false, Publish returns as soon as the message is written to the buffer.
			// If the buffer is full, Publish will block until space is available (Backpressure).
			BlockPublishUntilSubscriberAck: false,
		},
		watermill.NopLogger{},
	)
	defer pubSub.Close()

	topic := "high_load_burst_topic"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 2. Calculate Total Expected Processing Events
	// We are broadcasting.
	// 1 Op = (1 * msgsPerOp) messages published.
	// 1 Message Published = 1 copy delivered to EACH subscriber.
	totalMessagesPublished := b.N * msgsPerOp
	totalEventsToAck := totalMessagesPublished * subscriberCount

	var consumersWg sync.WaitGroup
	consumersWg.Add(totalEventsToAck)

	// 3. Setup Subscribers (Fan-Out)
	for i := 0; i < subscriberCount; i++ {
		msgs, err := pubSub.Subscribe(ctx, topic)
		if err != nil {
			b.Fatal(err)
		}

		go func(workerID int) {
			for msg := range msgs {
				// Simulate simple validation to ensure memory was actually read
				if len(msg.Payload) == 0 {
					panic("empty payload received")
				}

				msg.Ack()
				consumersWg.Done()
			}
		}(i)
	}

	// 4. Benchmark Loop (Parallel Producers)
	// We report metrics based on the data flowing through the system.
	b.SetBytes(int64(payloadSize * msgsPerOp))
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		// Allocate a source buffer once per routine to copy from (optimization),
		// but we will allocate FRESH slices for every message to stress GC.
		templateData := make([]byte, payloadSize)
		rnd := rand.NewChaCha8([32]byte{byte(time.Now().UnixNano())})
		_, _ = rnd.Read(templateData)

		for pb.Next() {
			// BURST LOOP: Publish multiple messages per Operation
			for i := 0; i < msgsPerOp; i++ {
				// A. Allocation Stress: Create a fresh payload
				payload := make([]byte, payloadSize)
				copy(payload, templateData)

				// B. Metadata Stress: Create fresh map
				msg := message.NewMessage(watermill.NewUUID(), payload)
				msg.Metadata.Set("ts", strconv.FormatInt(time.Now().UnixNano(), 10))
				msg.Metadata.Set("type", "burst_test")

				// C. Publish
				// In a Fan-Out scenario, this iterates over all subscriber channels
				// and attempts to send. If buffers are full, it may block here.
				if err := pubSub.Publish(topic, msg); err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	// 5. Cleanup & Drain
	b.StopTimer()

	// Wait for all subscribers to finish processing everything in their buffers.
	consumersWg.Wait()
}
