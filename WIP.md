# WIP: watermill-memchan Implementation

## Current Goal
Implement `github.com/joeycumines/watermill-memchan` as a separate module using `github.com/joeycumines/go-bigbuff` as the underlying pub/sub mechanism - a drop-in replacement for `github.com/ThreeDotsLabs/watermill/pubsub/gochannel` with significantly better performance.

## Action Plan
- [x] Investigate pubsub/test1 and pubsub/test2 for benchmark comparisons
- [x] Understand gochannel API surface (Config, GoChannel, FanOut, all methods)
- [x] Create watermill-memchan directory and initialize Go module
- [x] Implement memchan.Config (matching gochannel.Config structure)
- [x] Implement memchan.GoChannel as main pub/sub
- [x] Implement memchan.FanOut
- [x] Implement all tests from gochannel package
- [x] Create pubsub/test3 with memchan benchmarks
- [ ] Run full benchmarks and record results
- [ ] Create README.md with benchmark results and documentation
- [ ] Investigate external watermill-benchmark repo
- [ ] Update watermill README to mention memchan alternative

## Progress Log
- Analyzed benchmark results showing ~2-5x performance improvement with go-bigbuff
- Created watermill-memchan module with full API compatibility
- All tests passing (pubsub_test.go, fanout_test.go, pubsub_bench_test.go, stress test)
- Created test3 directory for memchan benchmarks
