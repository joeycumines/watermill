# WIP: watermill-memchan Complete Rewrite

## Current Goal
Completely rewrite watermill-memchan from scratch to properly leverage ChanPubSub throughout the implementation.

## Critical Issues to Address
1. Must COMPARE benchmarks between test1, test2, and test3 in the SAME execution
2. Cannot just switch between old/new behavior - must rewrite from scratch
3. Need to add Benchmark_HighLoad_FanOut_Burst with backpressure variant to all three test packages

## Action Plan
- [ ] Completely rewrite watermill-memchan/pubsub.go using ChanPubSub as the core
- [ ] Add Benchmark_HighLoad_FanOut_Burst_Backpressure to test1, test2, test3
- [ ] Run all benchmarks in same execution and compare results
- [ ] Ensure memchan maintains performance parity with raw ChanPubSub (test2)
- [ ] Update benchmark results in repository

## Progress Log
- Starting fresh rewrite
