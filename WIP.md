# WIP: watermill-memchan Complete Rewrite

## Current Goal
Complete rewrite of watermill-memchan using ChanPubSub for BlockPublishUntilSubscriberAck mode.

## Benchmark Results (Same Execution)

### Benchmark_highContention (150k subscribers)
| Implementation | Time/Op | Allocs |
|----------------|---------|--------|
| gochannel | 252ms | 1.05M |
| ChanPubSub | 173ms | 9 |
| **memchan** | **208ms** | **6** |

**memchan is ~20% faster than gochannel with 99.999% fewer allocations**

### Benchmark_Production_EventBus_StrictSync (10 subscribers)
| Implementation | Time/Op | Allocs |
|----------------|---------|--------|
| gochannel | 19.2µs | 94 |
| ChanPubSub | 4.0µs | 6 |
| **memchan** | **7.2µs** | **9** |

**memchan is 2.7x faster than gochannel with 90% fewer allocations**

## Progress Log
- Rewrote pubsub.go from scratch using ChanPubSub
- Added backpressure benchmarks to test1, test2, test3
- Optimized blockingReceiver for minimal overhead
- Tests for BlockPublishUntilSubscriberAck mode passing
