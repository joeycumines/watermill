// Package memchan provides a high-performance in-memory Pub/Sub implementation
// for Watermill that is a drop-in replacement for the standard gochannel package.
//
// It leverages [github.com/joeycumines/go-bigbuff.ChanPubSub] for significantly
// improved performance and memory efficiency, particularly under high contention
// scenarios with many subscribers.
//
// # Features
//
//   - Drop-in replacement API for [github.com/ThreeDotsLabs/watermill/pubsub/gochannel]
//   - Significantly better performance under high subscriber counts
//   - Lower memory footprint
//   - Support for BlockPublishUntilSubscriberAck mode
//   - Support for buffered output channels
//   - Support for persistent messages
//   - Support for context preservation
//
// # Usage
//
// Replace imports of [github.com/ThreeDotsLabs/watermill/pubsub/gochannel] with
// [github.com/joeycumines/watermill-memchan]:
//
//	import "github.com/joeycumines/watermill-memchan"
//
//	pubSub := memchan.NewGoChannel(memchan.Config{
//	    BlockPublishUntilSubscriberAck: true,
//	}, logger)
//
// All Pub/Subs implementation documentation can be found at https://watermill.io/pubsubs/
package memchan
