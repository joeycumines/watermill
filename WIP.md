# WIP: watermill-memchan Implementation

## Current Goal
Implement `github.com/joeycumines/watermill-memchan` as a separate module using `github.com/joeycumines/go-bigbuff` as the underlying pub/sub mechanism - a drop-in replacement for `github.com/ThreeDotsLabs/watermill/pubsub/gochannel` with significantly better performance.

## Action Plan
- [x] Investigate pubsub/test1 and pubsub/test2 for benchmark comparisons
- [x] Understand gochannel API surface (Config, GoChannel, FanOut, all methods)
- [ ] Create watermill-memchan directory and initialize Go module
- [ ] Implement memchan.Config (matching gochannel.Config structure)
- [ ] Implement memchan.GoChannel as main pub/sub using go-bigbuff/ChanPubSub
- [ ] Implement memchan.FanOut
- [ ] Implement all tests from gochannel package
- [ ] Create pubsub/test3 benchmarks and run them
- [ ] Create README.md with benchmark results and documentation
- [ ] Investigate external watermill-benchmark repo
- [ ] Update watermill README to mention memchan alternative

## Progress Log
- Analyzed benchmark results showing ~2-5x performance improvement with go-bigbuff
- gochannel.Config has: OutputChannelBuffer, Persistent, BlockPublishUntilSubscriberAck, PreserveContext
- GoChannel has: NewGoChannel, Publish, Subscribe, Close
- FanOut has: NewFanOut, AddSubscription, Run, Running, IsClosed, Subscribe, Close
