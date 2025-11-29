package main

import (
	"context"
	"log"
	"sync"
	"sync/atomic"

	"github.com/joeycumines/go-bigbuff"
)

var bmrInt int

func main() {
	const numReceivers = 150_000
	const numMessages = 9

	// 1. Setup ChanPubSub
	// Contract: Must use NewChanPubSub with a non-nil, unbuffered channel.
	// We use string to match the message payload concept ("1"), though bigbuff is generic.
	ch := make(chan string)
	ps := bigbuff.NewChanPubSub(ch)

	// Note: We manage the lifecycle of 'ch' manually to trigger shutdown,
	// similar to how c.Close() is used in the reference.

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var allStopped sync.WaitGroup
	allStopped.Add(numReceivers)

	var count atomic.Int32

	for i := 0; i < numReceivers; i++ {
		// 2. Subscribe
		// SubscribeContext immediately registers the subscriber (Add(1)) and
		// returns an iterator (iter.Seq[V]) that manages the receive loop.
		messages := ps.SubscribeContext(ctx)

		go func() {
			defer allStopped.Done()
			var n int

			// 3. Consume
			// We range over the iterator.
			// BigBuff Contract: Subscribers must Wait() after receiving.
			// The SubscribeContext iterator handles the underlying x.Wait() call
			// automatically before yielding the value, ensuring strictly
			// synchronous "Ack" behavior equivalent to Watermill's BlockPublishUntilSubscriberAck.
			for _ = range messages {
				count.Add(1)
				n++
			}
		}()
	}

	msg := "1"

	log.Printf("publishing %d messages", numMessages)

	for i := 0; i < numMessages; i++ {
		// 4. Publish
		// Send blocks until all current subscribers have received the message
		// and the internal Wait() (Ack) has been completed.
		ps.Send(msg)
		bmrInt++
	}

	countBefore := int(count.Load())
	bmrInt += countBefore
	log.Printf(`closing, received %d messages total across %d consumers`, countBefore, numReceivers)

	// 5. Teardown
	// Closing the underlying channel signals the iterators in SubscribeContext to return,
	// effectively stopping all subscribers.
	close(ch)

	allStopped.Wait()

	countAfter := int(count.Load())

	if countAfter != numMessages*numReceivers {
		log.Printf(`expected %d messages, got %d`, numMessages*numReceivers, countAfter)
	}
	if countBefore != countAfter {
		log.Printf(`received %d messages, but was %d prior to close - should have blocked on acK?`, countAfter, countBefore)
	}
}
