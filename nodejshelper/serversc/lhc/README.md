SocketCluster Sample App
======

This is a sample SocketCluster app.

https://github.com/SocketCluster/socketcluster/blob/master/scc-guide.md
https://github.com/SocketCluster/socketcluster
publish sample 'o:{"a":123, "b":456}'

## Benchmarks

This stack is the baseline the dependency free rewrite in `../go-server` is
compared against. Both were driven by the same `loadtest` generator on the same
host; this folder was measured as shipped (`SOCKETCLUSTER_WORKERS=1`) and with
`SOCKETCLUSTER_WORKERS=8` for the multi core comparison.

* results: [`../go-server/README.md#measured`](../go-server/README.md#measured)
* reproduction commands: [`../go-server/loadtest/README.md`](../go-server/loadtest/README.md)

Headline numbers with one worker: 30 000 idle connections held from a single
process (subscribe p99 9.0 ms, 754 MB RSS, 103% of one core for the whole stack),
and on a single hot channel the worker stops completing websocket handshakes at
roughly 900 subscribers - 2 075 of 3 000 connections timed out in the fan out
scenario, where the Go server kept all 3 000.