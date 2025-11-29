package main

import (
	"context"
	"log"
	"sync"
	"sync/atomic"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
)

var bmrInt int

func main() {
	const numReceivers = 150_000
	const numMessages = 9

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
			log.Fatal(err)
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
					log.Fatal(`msg.Ack() returned false`)
				}
			}
		}()
	}

	msg := message.NewMessage("1", nil)

	log.Printf("publishing %d messages", numMessages)

	for i := 0; i < numMessages; i++ {
		err := c.Publish(topicName, msg)
		if err != nil {
			log.Fatal(err)
		}
		bmrInt++
	}

	countBefore := int(count.Load())
	log.Printf(`closing, received %d messages total across %d consumers (published %d)`, countBefore, numReceivers, bmrInt)

	if err := c.Close(); err != nil {
		log.Fatal(err)
	}
	allStopped.Wait()

	countAfter := int(count.Load())

	if countAfter != numMessages*numReceivers {
		log.Printf(`expected %d messages, got %d`, numMessages*numReceivers, countAfter)
	}
	if countBefore != countAfter {
		log.Printf(`received %d messages, but was %d prior to close - should have blocked on acK?`, countAfter, countBefore)
	}
}
