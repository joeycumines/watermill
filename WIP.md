# WIP: watermill-memchan Implementation

## Current Goal
Implement `github.com/joeycumines/watermill-memchan` as a separate module using `github.com/joeycumines/go-bigbuff` ChanPubSub - a drop-in replacement for `github.com/ThreeDotsLabs/watermill/pubsub/gochannel` with better performance in BlockPublishUntilSubscriberAck mode.

## Action Plan
- [x] Investigate pubsub/test1 and pubsub/test2 for benchmark comparisons
- [x] Understand gochannel API surface (Config, GoChannel, FanOut, all methods)
- [x] Create watermill-memchan directory and initialize Go module
- [x] Implement memchan.Config (matching gochannel.Config structure)
- [x] Implement memchan.GoChannel using ChanPubSub for BlockPublishUntilSubscriberAck mode
- [x] Implement memchan.FanOut
- [x] Implement all tests from gochannel package
- [x] Create pubsub/test3 with memchan benchmarks
- [ ] Run full benchmarks and record results
- [ ] Create README.md with benchmark results and documentation
- [ ] Investigate external watermill-benchmark repo
- [ ] Update watermill README to mention memchan alternative

## Progress Log
- Analyzed benchmark results showing performance improvements with go-bigbuff
- Created watermill-memchan module with full API compatibility
- Key insight: ChanPubSub shines in BlockPublishUntilSubscriberAck mode
- Implementation uses ChanPubSub for BlockPublishUntilSubscriberAck, traditional approach otherwise
- All tests passing with race detector
