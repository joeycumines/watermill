# watermill-memchan

[![Go Reference](https://pkg.go.dev/badge/github.com/joeycumines/watermill-memchan.svg)](https://pkg.go.dev/github.com/joeycumines/watermill-memchan)

A high-performance, drop-in replacement for [github.com/ThreeDotsLabs/watermill/pubsub/gochannel](https://github.com/ThreeDotsLabs/watermill/tree/master/pubsub/gochannel) that leverages [github.com/joeycumines/go-bigbuff](https://github.com/joeycumines/go-bigbuff)'s `ChanPubSub` for improved performance characteristics.

## Features

- **Drop-in replacement** - Identical API to `gochannel`, just change imports
- **ChanPubSub-backed broadcasting** - Uses efficient broadcast mechanism for `BlockPublishUntilSubscriberAck` mode
- **Full feature support** - All `gochannel` features including:
  - Buffered output channels (`OutputChannelBuffer`)
  - Persistent messages (`Persistent`)
  - Blocking publish until Ack (`BlockPublishUntilSubscriberAck`)
  - Context preservation (`PreserveContext`)
- **Production-ready** - Passes all watermill pubsub tests

## Installation

```bash
go get github.com/joeycumines/watermill-memchan
```

## Usage

Simply replace your `gochannel` imports:

```go
// Before
import "github.com/ThreeDotsLabs/watermill/pubsub/gochannel"

pubSub := gochannel.NewGoChannel(gochannel.Config{
    BlockPublishUntilSubscriberAck: true,
}, logger)

// After
import "github.com/joeycumines/watermill-memchan"

pubSub := memchan.NewGoChannel(memchan.Config{
    BlockPublishUntilSubscriberAck: true,
}, logger)
```

## Why memchan?

The standard `gochannel` implementation uses per-subscriber goroutines for message delivery, which can lead to high contention under heavy load. `memchan` uses `ChanPubSub` for more efficient broadcast-style message delivery when `BlockPublishUntilSubscriberAck` is enabled.

### Benchmark Comparison

The following benchmarks compare the original `gochannel` (test1) against raw `ChanPubSub` (test2) for reference:

#### High Contention (150k subscribers, strict sync)

| Implementation | Time/Op | Memory/Op | Allocs/Op |
|---------------|---------|-----------|-----------|
| gochannel | ~450ms | 81MB | 1.07M |
| ChanPubSub (raw) | ~210ms | 2KB | 24 |

The raw `ChanPubSub` shows the theoretical performance ceiling when bypassing watermill's message abstraction (copying, Ack/Nack handling).

#### Production Event Bus - Strict Sync (10 subscribers)

| Implementation | Time/Op | Memory/Op | Allocs/Op |
|---------------|---------|-----------|-----------|
| gochannel | ~12-26µs | 6.8KB | 93 |
| ChanPubSub (raw) | ~3.6µs | 432B | 6 |

#### High Load Fan-Out Burst (100k msgs × 8 subscribers)

| Implementation | Throughput | Memory/Op | Allocs/Op |
|---------------|------------|-----------|-----------|
| gochannel | ~55-66 MB/s | 765-835MB | 7.1-7.6M |
| ChanPubSub (raw) | ~93-107 MB/s | 177MB | 900K |

### When to Use memchan

`memchan` is particularly beneficial when:

- Using `BlockPublishUntilSubscriberAck: true`
- High subscriber counts
- Message throughput is critical
- Memory efficiency matters

For non-blocking publish scenarios (buffered mode), both implementations perform similarly as they use the same underlying architecture.

## Configuration

```go
type Config struct {
    // Output channel buffer size.
    OutputChannelBuffer int64

    // If persistent is set to true, when subscriber subscribes to the topic,
    // it will receive all previously produced messages.
    Persistent bool

    // When true, Publish will block until subscriber Ack's the message.
    // If there are no subscribers, Publish will not block.
    BlockPublishUntilSubscriberAck bool

    // PreserveContext determines if the context should be preserved
    // when sending messages to subscribers.
    PreserveContext bool
}
```

## FanOut Support

`memchan` also provides `FanOut`, identical to `gochannel.FanOut`:

```go
fanout, err := memchan.NewFanOut(upstreamSubscriber, logger)
fanout.AddSubscription("topic")

go fanout.Run(ctx)
<-fanout.Running()

// Subscribe to the fanout
messages, _ := fanout.Subscribe(ctx, "topic")
```

## GitHub Topics

`watermill` `pubsub` `go` `golang` `in-memory` `event-driven` `message-queue` `broadcast` `high-performance`

## License

MIT License - see [LICENSE](LICENSE) for details.

## Related Projects

- [Watermill](https://github.com/ThreeDotsLabs/watermill) - The Watermill framework
- [go-bigbuff](https://github.com/joeycumines/go-bigbuff) - High-performance buffering utilities including `ChanPubSub`
