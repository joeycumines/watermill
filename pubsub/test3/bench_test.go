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
	"github.com/joeycumines/watermill-memchan"
)

func Benchmark_highContention(b *testing.B) {
	b.ReportAllocs()

	const numReceivers = 150_000

	c := memchan.NewGoChannel(
		memchan.Config{BlockPublishUntilSubscriberAck: true},
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
	const (
		numSubscribers = 10
		channelBuffer  = 1000
		payloadSize    = 1024
	)

	c := memchan.NewGoChannel(
		memchan.Config{
			BlockPublishUntilSubscriberAck: false,
			OutputChannelBuffer:            channelBuffer,
		},
		watermill.NopLogger{},
	)
	defer c.Close()

	topicName := "order_created"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var consumersWg sync.WaitGroup
	consumersWg.Add(numSubscribers * b.N)

	payload := make([]byte, payloadSize)

	for i := 0; i < numSubscribers; i++ {
		msgs, err := c.Subscribe(ctx, topicName)
		if err != nil {
			b.Fatal(err)
		}

		go func(subID int) {
			for msg := range msgs {
				if len(msg.Payload) == 0 {
					b.Error("Empty payload received")
				}
				msg.Ack()
				consumersWg.Done()
			}
		}(i)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		msg := message.NewMessage(watermill.NewUUID(), payload)

		if err := c.Publish(topicName, msg); err != nil {
			b.Fatal(err)
		}
	}

	consumersWg.Wait()
}

// Benchmark_Production_EventBus_StrictSync simulates a strict consistency requirement.
func Benchmark_Production_EventBus_StrictSync(b *testing.B) {
	const (
		numSubscribers = 10
		payloadSize    = 1024
	)

	c := memchan.NewGoChannel(
		memchan.Config{
			BlockPublishUntilSubscriberAck: true,
			OutputChannelBuffer:            0,
		},
		watermill.NopLogger{},
	)
	defer c.Close()

	topicName := "user_signup_strict"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	payload := make([]byte, payloadSize)

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

// Benchmark_HighLoad_FanOut_Burst simulates a high-throughput "Firehose" scenario.
func Benchmark_HighLoad_FanOut_Burst(b *testing.B) {
	const (
		payloadSize     = 1024
		msgsPerOp       = 100_000
		subscriberCount = 8
		bufferSize      = 2000
	)

	pubSub := memchan.NewGoChannel(
		memchan.Config{
			OutputChannelBuffer:            bufferSize,
			BlockPublishUntilSubscriberAck: false,
		},
		watermill.NopLogger{},
	)
	defer pubSub.Close()

	topic := "high_load_burst_topic"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	totalMessagesPublished := b.N * msgsPerOp
	totalEventsToAck := totalMessagesPublished * subscriberCount

	var consumersWg sync.WaitGroup
	consumersWg.Add(totalEventsToAck)

	for i := 0; i < subscriberCount; i++ {
		msgs, err := pubSub.Subscribe(ctx, topic)
		if err != nil {
			b.Fatal(err)
		}

		go func(workerID int) {
			for msg := range msgs {
				if len(msg.Payload) == 0 {
					panic("empty payload received")
				}

				msg.Ack()
				consumersWg.Done()
			}
		}(i)
	}

	b.SetBytes(int64(payloadSize * msgsPerOp))
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		templateData := make([]byte, payloadSize)
		rnd := rand.NewChaCha8([32]byte{byte(time.Now().UnixNano())})
		_, _ = rnd.Read(templateData)

		for pb.Next() {
			for i := 0; i < msgsPerOp; i++ {
				payload := make([]byte, payloadSize)
				copy(payload, templateData)

				msg := message.NewMessage(watermill.NewUUID(), payload)
				msg.Metadata.Set("ts", strconv.FormatInt(time.Now().UnixNano(), 10))
				msg.Metadata.Set("type", "burst_test")

				if err := pubSub.Publish(topic, msg); err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	b.StopTimer()
	consumersWg.Wait()
}

// Benchmark_HighLoad_FanOut_Burst_Backpressure simulates high-throughput broadcasting
// with strict backpressure (BlockPublishUntilSubscriberAck: true).
func Benchmark_HighLoad_FanOut_Burst_Backpressure(b *testing.B) {
	const (
		payloadSize     = 1024
		msgsPerOp       = 100_000
		subscriberCount = 8
	)

	pubSub := memchan.NewGoChannel(
		memchan.Config{
			OutputChannelBuffer:            0,
			BlockPublishUntilSubscriberAck: true,
		},
		watermill.NopLogger{},
	)
	defer pubSub.Close()

	topic := "high_load_burst_backpressure_topic"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup Subscribers
	for i := 0; i < subscriberCount; i++ {
		msgs, err := pubSub.Subscribe(ctx, topic)
		if err != nil {
			b.Fatal(err)
		}

		go func(workerID int) {
			for msg := range msgs {
				if len(msg.Payload) == 0 {
					panic("empty payload received")
				}
				msg.Ack()
			}
		}(i)
	}

	b.SetBytes(int64(payloadSize * msgsPerOp))
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		templateData := make([]byte, payloadSize)
		rnd := rand.NewChaCha8([32]byte{byte(time.Now().UnixNano())})
		_, _ = rnd.Read(templateData)

		for pb.Next() {
			for i := 0; i < msgsPerOp; i++ {
				payload := make([]byte, payloadSize)
				copy(payload, templateData)

				msg := message.NewMessage(watermill.NewUUID(), payload)
				msg.Metadata.Set("ts", strconv.FormatInt(time.Now().UnixNano(), 10))
				msg.Metadata.Set("type", "burst_test")

				if err := pubSub.Publish(topic, msg); err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	b.StopTimer()
}
