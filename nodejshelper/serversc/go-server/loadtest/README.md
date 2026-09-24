# Benchmark harness

How the numbers in [`../README.md`](../README.md#measured) and in the extension's
main README were produced. Everything runs on one host against a local Redis -
there is no Docker involved and nothing here touches Live Helper Chat.

## 1. Prerequisites

```bash
redis-cli ping                     # PONG
node --version                     # only for the Node.js baseline
go version                         # >= 1.24
```

Both servers must be given the *same* secret hash, and it has to be the one the
generator signs its tokens with. The Node.js baseline has it hard coded in
`serversc/lhc/server.js` (`secretHash`); the Go server reads `SECRET_HASH`.

Keep the host otherwise quiet - the generator is co-located, so anything else
burning CPU shows up in the latency numbers.

## 2. Build

```bash
cd nodejshelper/serversc/go-server
go build -mod=vendor -o /tmp/bench/lhcnodejs .        # the server under test
go build -mod=vendor -o /tmp/bench/loadtest ./loadtest
```

For the Node.js baseline, install its dependencies once
(`cd ../../lhc && npm install`, 43 MB in `node_modules`).

## 3. Start a server (fully detached - it must outlive the shell)

```bash
# Go server
SECRET_HASH='...' AUTH_KEY=bench SOCKETCLUSTER_PORT=8000 REDIS_HOST=127.0.0.1 \
  setsid nohup /tmp/bench/lhcnodejs > /tmp/bench/go.log 2>&1 < /dev/null &
curl -fsS http://127.0.0.1:8000/health-check && echo     # OK

# Node.js baseline (1 worker is what ships)
cd nodejshelper/serversc/lhc
SOCKETCLUSTER_PORT=8000 REDIS_HOST=127.0.0.1 \
  setsid nohup node server.js > /tmp/bench/node.log 2>&1 < /dev/null &
curl -fsS http://127.0.0.1:8000/health-check && echo     # OK
```

Stop one before starting the other - both default to port 8000, and running them
side by side would make the CPU numbers meaningless.

## 4. Scenarios

`"$SECRET"` is `site.secrethash` (`e9ekdkld4d0_...` in the benchmark above), and
`-local-ips` is only needed for the >20 000 connection runs because a single
source address runs out of ephemeral ports around 28 232 connections.

| # | scenario | flags |
| --- | --- | --- |
| 1 | realistic, 2 000 visitors | `-connections 2000 -ramp 200 -duration 30s -chat-ids 0 -publishers 1 -publish-interval 1s` |
| 2 | scale, 5 000 visitors | `-connections 5000 -ramp 1000 -duration 30s -chat-ids 0 -publishers 1 -publish-interval 1s` |
| 3 | worst case fan out | `-connections 3000 -ramp 300 -duration 30s -chat-ids 1 -publishers 5 -publish-interval 200ms` |
| 4 | connection storm | `-connections 3000 -ramp 0 -duration 30s -chat-ids 1 -publishers 5 -publish-interval 200ms` |
| 5 | capacity, 20 000 | `-connections 20000 -ramp 2000 -duration 30s -chat-ids 0 -publishers 0 -local-ips 127.0.0.2,127.0.0.3,127.0.0.4,127.0.0.5` |
| 6 | capacity, 30 000 | `-connections 30000 -ramp 3000 -duration 30s -chat-ids 0 -publishers 0 -local-ips 127.0.0.2,127.0.0.3,127.0.0.4,127.0.0.5,127.0.0.6,127.0.0.7` |

Add `-json` to get the report as JSON, `-progress 5s` to watch it live.

Between two scenarios wait ~5 s (sockets need to leave `TIME_WAIT`) and warm the
new server up once with a throw away run, otherwise the first scenario of a fresh
process pays for cold code paths:

```bash
/tmp/bench/loadtest -url 127.0.0.1:8000 -secret "$SECRET" \
  -connections 50 -ramp 200 -duration 5s -chat-ids 0 -publishers 0 -progress 0
```

## 5. Measure the server, not only the generator

The loadtest report knows nothing about the server. Sample it from `/proc` while
a scenario runs (CPU in % of one core, summed over every process of the stack,
plus total RSS and open file descriptors):

```bash
# the PIDs: the Go server is one process, the Node.js stack is master + broker +
# worker cluster + workers (the log has their PIDs, or use pgrep)
printf '%s\n' "$(cat /tmp/bench/go.pid)" > /tmp/bench/pids.txt
pgrep -f 'serversc/lhc' >> /tmp/bench/pids.txt
```

Read `/proc/<pid>/stat` fields 14+15 for CPU jiffies and `VmRSS` from
`/proc/<pid>/status`, dividing the jiffy delta by `getconf CLK_TCK` and the wall
time. Sample every 200 ms and keep the peak - RSS and CPU at the end of a run are
zero, because every connection has been closed by then.

To separate "opening connections" from "holding connections", start the
generator with a longer `-duration` and only begin sampling once the ramp is over
(for example sample the last 12 s of a `-duration 40s` run). That is where the
idle CPU numbers in the README come from - they are dominated by the 8 s keep
alive ping, so `PING_INTERVAL_MS` moves them a lot.

## 6. Traps

* **Ephemeral ports.** `connect: cannot assign requested address` above ~28 k
  connections is the generator running out of source ports, not a server limit.
  Use `-local-ips` with several `127.0.0.x` addresses.
* **The generator is not free.** It holds one goroutine per connection; at
  30 000 connections it uses a few hundred MB and real CPU. Both processes
  compete for the same cores, so absolute latency is worse than with the
  generator on a separate machine.
* **`SOCKETCLUSTER_WORKERS`.** The Node.js baseline defaults to 1 worker, which
  leaves 7 of the 8 cores idle. Compare like with like - either run both with
  their defaults, or also test Node.js with `SOCKETCLUSTER_WORKERS=8` (the Go
  server always uses all cores).
* **Slow consumers.** A socket whose outbound queue is full is disconnected
  (`1008 outbound queue full`, `SOCKET_SEND_BUFFER` = 256 messages). The
  generator itself can be the slow consumer once a single channel fans out
  faster than one Go process can read - that is what happens in scenario 4.
