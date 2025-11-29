#!/bin/sh

go test -c -o bench.test &&
  /usr/bin/time -l ./bench.test -test.run='^$' -test.bench="$1" -test.timeout=5m -test.count=6 -test.benchmem 2>&1 | tee bench."$(echo "$1" | md5sum | cut -d ' ' -f 1)".txt
