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
	"github.com/joeycumines/go-bigbuff"
)

// Comparison of logic:
// Watermill: Subscribe -> <-messages -> msg.Ack() (Block until Ack)
// BigBuff:   Subscribe -> <-C()      -> Wait()    (Block until Wait)
func Benchmark_highContention(b *testing.B) {
	b.ReportAllocs()

	const numReceivers = 150_000

	// 1. Setup - Create the PubSub
	// bigbuff requires a non-nil, unbuffered channel.
	// We use *string to mimic a pointer payload (like *message.Message).
	msgCh := make(chan *string)
	ps := bigbuff.NewChanPubSub(msgCh)

	var closeMsgChOnce sync.Once
	closeMsgCh := func() {
		closeMsgChOnce.Do(func() {
			close(msgCh)
		})
	}

	// Clean up: Closing the underlying channel signals receivers to stop.
	defer closeMsgCh()

	var allStopped sync.WaitGroup
	allStopped.Add(numReceivers)

	var count atomic.Int32

	// 2. Setup - Subscribers
	for i := 0; i < numReceivers; i++ {
		// Contract: "Values MUST NOT be received prior to incrementing the subscriber count"
		ps.Subscribe()

		go func() {
			defer allStopped.Done()

			// Contract: "Subscribers... MUST promptly unsubscribe"
			// We unsubscribe when the loop exits (channel closed).
			defer ps.Unsubscribe()

			// Contract: "Subscribers MUST promptly and continuously operate on a cycle
			// of receiving... then waiting"
			for range ps.C() {
				// Received message
				count.Add(1)

				// Contract: "MUST call Wait, immediately after each value received"
				// This creates the back-pressure equivalent to Watermill's BlockPublishUntilSubscriberAck
				ps.Wait()
			}
		}()
	}

	// Payload
	payload := "1"
	msg := &payload

	b.Logf(`publishing %d messages`, b.N)

	b.ResetTimer()

	// 3. Measurement - Publish Loop
	for i := 0; i < b.N; i++ {
		// Contract: Send blocks until all subscribers have called Wait().
		// This matches Watermill's behavior with BlockPublishUntilSubscriberAck: true.
		subscribersCount := ps.Send(msg)

		if subscribersCount != numReceivers {
			b.Fatalf("expected to send to %d subscribers, sent to %d", numReceivers, subscribersCount)
		}

		bmrInt++
	}

	b.StopTimer()

	// 4. Verification & Teardown
	countBefore := int(count.Load())
	bmrInt += countBefore
	b.Logf(`closing, received %d messages total across %d consumers`, countBefore, numReceivers)

	// Closing msgCh (via defer above, or explicitly here) breaks the receiver loops.
	// We close here to allow WaitGroup to finish.
	closeMsgCh()

	// Wait for all subscribers to Unsubscribe and exit.
	allStopped.Wait()

	countAfter := int(count.Load())

	// Validate delivery counts
	expectedTotal := b.N * numReceivers
	if countAfter != expectedTotal {
		b.Errorf(`expected %d messages, got %d`, expectedTotal, countAfter)
	}
	if countBefore != countAfter {
		b.Errorf(`received %d messages, but was %d prior to close`, countAfter, countBefore)
	}
}

// Benchmark_Production_EventBus_Buffered simulates a realistic, high-performance
// application bus using BigBuff.
//
// Comparison Note:
// In Watermill, "Buffered" means the Publisher writes to a channel and returns immediately
// (unless the buffer is full). The Subscriber consumes from that buffer.
// In BigBuff, Send() strictly blocks until *all* subscribers call Wait().
// To emulate "Buffered" behavior (decoupling publishing from processing), subscribers
// must receive, push to a local buffer, and immediately call Wait(), allowing the
// Publisher to proceed while a worker goroutine processes the message.
func Benchmark_Production_EventBus_Buffered(b *testing.B) {
	// 1. Production Config
	const (
		numSubscribers = 10
		channelBuffer  = 1000
		payloadSize    = 1024
	)

	// Setup BigBuff
	// Note: The underlying channel must be unbuffered. Buffering is implemented
	// at the subscriber level to mimic independent subscriber queues.
	c := make(chan *message.Message)
	ps := bigbuff.NewChanPubSub(c)

	var closeMsgChOnce sync.Once
	closeMsgCh := func() {
		closeMsgChOnce.Do(func() {
			close(c)
		})
	}
	defer closeMsgCh()

	var consumersWg sync.WaitGroup
	consumersWg.Add(numSubscribers * b.N)

	// Pre-allocate payload template
	payload := make([]byte, payloadSize)

	// 2. Setup Subscribers
	for i := 0; i < numSubscribers; i++ {
		ps.Subscribe()

		go func() {
			defer ps.Unsubscribe()

			// Create a local buffer for this subscriber.
			// This decouples the "Receive+Wait" (Pub/Sub mechanics) from "Processing".
			subBuffer := make(chan *message.Message, channelBuffer)

			// Worker Goroutine: Processes messages from the buffer
			go func() {
				for msg := range subBuffer {
					if len(msg.Payload) == 0 {
						b.Error("Empty payload received")
					}
					msg.Ack()
					consumersWg.Done()
				}
			}()

			// Consumer Goroutine: Interacts with BigBuff
			for msg := range ps.C() {
				// Push to local buffer.
				// If subBuffer is full, this blocks, which delays Wait(),
				// effectively creating backpressure on the Publisher (fair comparison).
				subBuffer <- msg

				// Signal to BigBuff that we have "received" the message so the
				// Publisher can move on.
				ps.Wait()
			}
			close(subBuffer)
		}()
	}

	// 3. The Benchmark Loop
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Mimic allocation cost of Watermill benchmark
		msg := message.NewMessage(watermill.NewUUID(), payload)

		// This will block only if subscribers' local buffers are full
		// (preventing them from calling Wait), or during the brief sync hand-off.
		count := ps.Send(msg)
		if count != numSubscribers {
			b.Fatalf("expected %d subscribers, got %d", numSubscribers, count)
		}
	}

	b.StopTimer()

	// 4. Cleanup
	closeMsgCh()
	consumersWg.Wait()
}

// Benchmark_Production_EventBus_StrictSync simulates strict consistency.
// Publisher blocks until all subscribers have processed the message.
func Benchmark_Production_EventBus_StrictSync(b *testing.B) {
	const (
		numSubscribers = 10
		payloadSize    = 1024
	)

	c := make(chan *message.Message)
	ps := bigbuff.NewChanPubSub(c)

	var closeMsgChOnce sync.Once
	closeMsgCh := func() {
		closeMsgChOnce.Do(func() {
			close(c)
		})
	}
	defer closeMsgCh()

	payload := make([]byte, payloadSize)

	// Setup Subscribers
	for i := 0; i < numSubscribers; i++ {
		ps.Subscribe()
		go func() {
			defer ps.Unsubscribe()
			for msg := range ps.C() {
				// Strict Sync: We process (Ack) *before* calling Wait().
				// This ensures the Publisher is blocked until work is done.
				msg.Ack()
				ps.Wait()
			}
		}()
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		msg := message.NewMessage(watermill.NewUUID(), payload)
		// Send blocks until all subscribers return from Wait()
		if sent := ps.Send(msg); sent != numSubscribers {
			b.Fatalf("sent to %d, expected %d", sent, numSubscribers)
		}
	}
}

// Benchmark_HighLoad_FanOut_Burst simulates high-throughput broadcasting.
// It uses parallel publishing (contending on the Bus) and buffered subscribers.
func Benchmark_HighLoad_FanOut_Burst(b *testing.B) {
	const (
		payloadSize     = 1024
		msgsPerOp       = 100_000
		subscriberCount = 8
		bufferSize      = 2000
	)

	c := make(chan *message.Message)
	ps := bigbuff.NewChanPubSub(c)

	// Using context for clean shutdown in this complex scenario
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 2. Calculate Total Expected Processing Events
	totalMessagesPublished := b.N * msgsPerOp
	totalEventsToAck := totalMessagesPublished * subscriberCount

	var consumersWg sync.WaitGroup
	consumersWg.Add(totalEventsToAck)

	// 3. Setup Subscribers (Fan-Out)
	for i := 0; i < subscriberCount; i++ {
		ps.Subscribe()
		go func() {
			defer ps.Unsubscribe()

			// Local buffer to allow bursting
			subBuffer := make(chan *message.Message, bufferSize)

			// Worker
			go func() {
				for msg := range subBuffer {
					if len(msg.Payload) == 0 {
						panic("empty payload received")
					}
					msg.Ack()
					consumersWg.Done()
				}
			}()

			// Receiver
			for {
				select {
				case <-ctx.Done():
					close(subBuffer)
					return
				case msg, ok := <-ps.C():
					if !ok {
						close(subBuffer)
						return
					}
					// Buffer the message
					subBuffer <- msg
					// Release the publisher
					ps.Wait()
				}
			}
		}()
	}

	// 4. Benchmark Loop
	b.SetBytes(int64(payloadSize * msgsPerOp))
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		templateData := make([]byte, payloadSize)
		rnd := rand.NewChaCha8([32]byte{byte(time.Now().UnixNano())})
		_, _ = rnd.Read(templateData)

		for pb.Next() {
			for i := 0; i < msgsPerOp; i++ {
				// A. Allocation Stress
				payload := make([]byte, payloadSize)
				copy(payload, templateData)

				// B. Metadata Stress
				msg := message.NewMessage(watermill.NewUUID(), payload)
				msg.Metadata.Set("ts", strconv.FormatInt(time.Now().UnixNano(), 10))
				msg.Metadata.Set("type", "burst_test")

				// C. Publish
				// BigBuff Send is thread-safe (uses internal Mutex), so this mimics
				// Watermill's behavior of contending on the topic lock.
				if sent := ps.Send(msg); sent != subscriberCount {
					// This might happen if context is cancelled or race on shutdown,
					// but strictly shouldn't in the middle of a run.
					b.Fatal("subscriber count mismatch during burst")
				}
			}
		}
	})

	b.StopTimer()

	// 5. Cleanup
	cancel()           // Stop subscribers
	close(c)           // Close underlying channel
	consumersWg.Wait() // Wait for buffers to drain
}
