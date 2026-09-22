# LHC NodeJS Helper server (Go)

A single binary replacement for the SocketCluster stack that used to live in
`serversc/lhc` (`server.js` master + `worker.js` workers + `broker.js` brokers +
`sc-redis`).

Its only third party dependency is `github.com/redis/go-redis/v9`, the Redis client
maintained by Redis itself, and it is vendored in `vendor/`. Both `go build` and the
Docker build use that copy, so neither needs the network (`go.mod` therefore asks for
Go 1.24 or newer).

Nothing changes for Live Helper Chat itself:

* **Browsers** keep using `design/nodejshelpertheme/js/socketcluster-client.js`
  (v13) - the server speaks SocketCluster Protocol V1.
* **PHP** keeps publishing over Redis (`erLhcoreClassNodeJSRedis`, i.e.
  `extension/nodejshelper/classes/lhpredis.php`) - the same channel names and the
  same `o:{...}` payloads.

## Build and run

```bash
cd extension/nodejshelper/serversc/new-server
go build -o lhcnodejs .     # builds from vendor/ (Go >= 1.24), no network needed
SECRET_HASH="$(your site.secrethash)" REDIS_HOST=127.0.0.1 ./lhcnodejs
```

The server listens on `:8000` (HTTP + websocket upgrade on `/socketcluster/`).

## Docker Compose

Two files - pick the one matching where Redis lives:

| File | When to use it | Command |
| --- | --- | --- |
| `docker-compose.yml` | no Redis yet - it is started as a container too | `docker compose up -d --build` |
| `docker-compose.external-redis.yml` | Redis already runs on the host, on another machine or as a managed service | `docker compose -f docker-compose.external-redis.yml up -d --build` |
| `docker-compose.host-network.yml` | Redis on this host **bound to 127.0.0.1** (stock distro config: protected mode on, no password) - Linux only | `docker compose -f docker-compose.host-network.yml up -d --build` |
| `docker-compose.scaled.yml` | several servers behind nginx (needs `nginx.conf`, see *Running several servers*) | `docker compose -f docker-compose.scaled.yml up -d --build` |

The third one is the least surprising for a locally installed Redis: the container shares
the host network namespace, so `127.0.0.1` is the real loopback and `protected-mode`
accepts it without any Redis change. The only cost is losing network isolation for that
container, and it does not work on Docker Desktop for macOS/Windows.

```bash
cd extension/nodejshelper/serversc/new-server
cp .env.example .env                        # set SECRET_HASH to your site.secrethash

docker compose up -d --build                # bundled Redis
docker compose -f docker-compose.external-redis.yml up -d --build   # existing Redis
docker compose logs -f golhchelper
```

Both start the websocket server; the external file adds nothing but the pointer to your
Redis, taken from `.env`:

```ini
REDIS_HOST=host.docker.internal   # a Redis on this machine (host-gateway is configured for you)
#REDIS_HOST=10.0.0.5              # ...or on another machine
#REDIS_PORT=6379
#REDIS_PASS=
#REDIS_USER=                      # only for Redis 6+ ACL users, otherwise `default` is used
```

That Redis has to accept connections from the container network, which is not the default
on a distro install:

* a Redis bound to `127.0.0.1` only (Debian/Ubuntu default) is unreachable from a
  container - set `bind 0.0.0.0` (or the docker bridge IP) in `redis.conf`;
* `protected-mode` (on by default) refuses non-loopback clients while no password is set -
  either set `requirepass` (and `REDIS_PASS`) or `protected-mode no`.

Both mistakes are visible in `docker compose logs golhchelper` (`DENIED Redis is running
in protected mode` / connection refused) and make `/health-check` answer `503`.

Then point Live Helper Chat at it:

* `Settings -> NodeJS Helper`: hostname = the host running the stack, port = `8000`,
  path = `/socketcluster/`
* `extension/nodejshelper/settings/settings.ini.php`: `'connect_db' => 'localhost:6379'`
  when PHP runs on the same host as the bundled Redis (published on `127.0.0.1` with
  `REDIS_PUBLISH_PORT`). With the external file use the host/port of your own Redis.

Notes:

* Redis is bound to `127.0.0.1` because it is unauthenticated, and persistence is off
  since it is only used as the pub/sub bus. Protected mode is disabled for the same
  reason - container-to-container connections are not loopback - the loopback port
  binding is what keeps it off the network. Set `REDIS_PASS` and switch the
  `redis-server` args to `--requirepass` if you prefer a password.
* **Already running a Redis?** Use `docker-compose.external-redis.yml` (above), it starts
  only the websocket server. With the bundled file, `REDIS_PUBLISH_PORT` is the *host* port
  the Redis container is published on - raise it when 6379 is already taken.
* `/health-check` answers `200 OK` only while the Redis bridge is up and `503 Failed`
  otherwise, so `docker compose ps` shows a broken bridge instead of a silently dead one.
* The server service raises `nofile` to `65535` (one file descriptor per websocket connection -
  the default soft limit of 1024 runs out quickly) and rotates `json-file` logs at 50 MB x 5.
  Keep the hard limit at or below the Docker daemon's own limit, otherwise `up` fails.
* Only the host port is configurable (`SOCKETCLUSTER_PORT`), the container keeps `8000`.
* For `wss://` terminate TLS in nginx/apache and proxy `/socketcluster/` (including the
  websocket upgrade) to this port.
* Scaling: several instances can share one Redis - see *Running several servers* below.

Without compose:

```bash
docker build -t lhc-golhchelper .
docker run -p 8000:8000 -e SECRET_HASH=... -e REDIS_HOST=redis lhc-golhchelper
```

## Running as a systemd service

The server is a single static binary, so no container runtime is needed - it also runs
directly on the host, supervised by systemd. The ready made units live in `systemd/`:

| File | Install as | Purpose |
| --- | --- | --- |
| `systemd/golhchelper.service` | `/etc/systemd/system/golhchelper.service` | the server |
| `systemd/golhchelper.env.example` | `/etc/golhchelper/golhchelper.env` | configuration (`SECRET_HASH`, Redis, ...) |
| `systemd/golhchelper-healthcheck.sh` | `/usr/local/bin/golhchelper-healthcheck` | polls `/health-check` |
| `systemd/golhchelper-healthcheck.service` + `.timer` | `/etc/systemd/system/` | restarts the server when the check keeps failing |

### Install

```bash
cd extension/nodejshelper/serversc/go-server

# 1. Dedicated unprivileged user - the server needs no root rights
sudo useradd --system --home /srv/golhchelper --shell /usr/sbin/nologin golhchelper

# 2. Binary (installed under the service name; the build output is called lhcnodejs,
#    the same name the Dockerfile produces) and the optional static directory
go build -mod=vendor -trimpath -ldflags="-s -w" -o lhcnodejs .
sudo install -m 0755 lhcnodejs /usr/local/bin/golhchelper
sudo install -d -m 0755 -o golhchelper -g golhchelper /srv/golhchelper/public

# 3. Configuration - 0640 root:golhchelper, it holds the secret hash
sudo install -d -m 0750 /etc/golhchelper
sudo install -m 0640 -o root -g golhchelper systemd/golhchelper.env.example /etc/golhchelper/golhchelper.env
sudoedit /etc/golhchelper/golhchelper.env        # SECRET_HASH, AUTH_KEY, REDIS_*

# 4. Units (+ the README the units point at in Documentation=)
sudo install -m 0644 systemd/golhchelper.service \
                    systemd/golhchelper-healthcheck.service \
                    systemd/golhchelper-healthcheck.timer /etc/systemd/system/
sudo install -m 0755 systemd/golhchelper-healthcheck.sh /usr/local/bin/golhchelper-healthcheck
sudo install -d -m 0755 /usr/local/share/doc/golhchelper
sudo install -m 0644 README.md /usr/local/share/doc/golhchelper/README.md
sudo systemctl daemon-reload

# 5. Start and verify
sudo systemctl enable --now golhchelper
sudo systemctl enable --now golhchelper-healthcheck.timer
systemctl --no-pager status golhchelper
curl -fsS http://127.0.0.1:8000/health-check && echo
```

Then configure Live Helper Chat exactly as for the container
(`Settings -> NodeJS Helper`: this host, port `8000`, path `/socketcluster/`).

### Configuration

`/etc/golhchelper/golhchelper.env` is a systemd `EnvironmentFile`: one `KEY=value` per
line, no `export`. The variables are the ones from the table above - `SECRET_HASH` and
`AUTH_KEY` are the two that matter, plus `REDIS_*` when Redis does not run on
`127.0.0.1:6379`.

* `SECRET_HASH` must equal `site.secrethash`, otherwise every websocket `login` fails. The
  unit refuses to start while it is unset or still the `change-me-...` placeholder, so that
  mistake shows up as a failed `systemctl start` instead of a log line.
* **Unlike compose's `.env`, `EnvironmentFile` does not interpolate `$`** - the hash can be
  pasted as is. Wrap the value in double quotes when it contains `#` or a space:
  `SECRET_HASH="...$#)8$asf931a171"`.
* `AUTH_KEY` should be set to a fixed, long random value, otherwise browser tokens stop
  working after every restart. It has to be identical on every node - see
  *Running several servers*.
* A Redis on the same host needs no `redis.conf` change: PHP, the server and Redis all talk
  over loopback, so `protected-mode` accepts them (the `docker-compose.host-network.yml`
  workaround exists only to put a container back onto that loopback).
* `LimitNOFILE=65535` in the unit is what allows tens of thousands of clients; check it with
  `systemctl show -p LimitNOFILE --value golhchelper`. The host limit still applies
  (`sysctl fs.file-max`, `DefaultLimitNOFILE` in `/etc/systemd/system.conf`).
* The service is reachable on the host only - open `SOCKETCLUSTER_PORT` (`8000`) in the
  firewall, or better, let only nginx through to it.
* For `wss://` terminate TLS in nginx/apache and proxy `/socketcluster/` including the
  websocket upgrade to `127.0.0.1:8000`. The `nginx.conf` in this directory does the same
  for the compose stack - replace its `upstream` block with
  `upstream golhchelper { server 127.0.0.1:8000; }` and drop the containers.

### Health check

`/health-check` is the endpoint to watch: `200 OK` while the Redis bridge is up, `503
Failed` when Redis is unreachable (open sockets keep working, cross node delivery does not).

```bash
curl -fsS http://127.0.0.1:8000/health-check && echo      # OK, or Failed + HTTP 503
```

Point an existing monitor (Nagios, Zabbix, Uptime Kuma, ...) at that URL and use
`systemctl restart golhchelper` as the recovery action. Without such a monitor the bundled
watchdog does exactly that:

* `golhchelper-healthcheck.timer` fires 60 s after boot and then every 30 s;
* one `curl` with a 5 s timeout per run, the outcome is logged to the journal;
* the server is restarted only after **3 consecutive** failures, so a single blip or a short
  Redis outage does not drop every websocket;
* that restart is graceful: SIGTERM makes the server close every socket with code `1001`
  and exit within milliseconds, so browsers reconnect at once.

```bash
systemctl list-timers golhchelper-healthcheck.timer
journalctl -u golhchelper-healthcheck -n 20        # failures and restarts
journalctl -u golhchelper-healthcheck -f
```

The watchdog runs as root (only `systemctl restart` needs it), the server itself does not.

### Operating it

```bash
systemctl restart golhchelper           # graceful, clients are closed with 1001
journalctl -u golhchelper -f            # logs; journald rotates them, tune with
                                        # SystemMaxUse in /etc/systemd/journald.conf
systemctl show -p MainPID --value golhchelper                     # pid
ss -Htn state established '( sport = :8000 )' | wc -l             # websocket clients
ls /proc/$(systemctl show -p MainPID --value golhchelper)/fd | wc -l   # open fds
```

Open file descriptors are roughly `clients + 8`; when that approaches `LimitNOFILE`
(65535), raise the limit or add a second node - a copy of the unit under a second name with
its own `SOCKETCLUSTER_PORT`, pointed at the same Redis (see *Running several servers*; in
nginx that is one more `server 127.0.0.1:<port>` line in the upstream block). Give the extra
node its own copy of the health check units with `Environment=SOCKETCLUSTER_PORT=<port>`,
otherwise the watchdog keeps checking 8000 and restarts the wrong instance.

## Running several servers

Any number of instances can share one Redis and **no sticky sessions are needed**: the only
shared state lives in Redis, and PHP publishes there, so whichever node a browser lands on
can serve it.

* a client publish is fanned out locally and published to Redis, so subscribers on other
  nodes receive it (the publisher skips its own echo via `SC_INSTANCE_ID`);
* each node subscribes Redis only for the channels where it has local subscribers, so
  cross-node traffic stays proportional to what is actually needed;
* visitor presence (`vi_online`) travels the same way.

| Setting | Requirement |
| --- | --- |
| `SECRET_HASH` | identical everywhere (must match `site.secrethash`) |
| `AUTH_KEY` | **identical everywhere** - see below |
| `SC_INSTANCE_ID` | unique per node; auto-generated (random per process), so normally nothing to do. Never set the same value twice - a node ignores messages carrying its own id |
| `REDIS_HOST` / `REDIS_PORT` | the same Redis for every node |
| load balancer | websocket upgrade passthrough; stickiness not required |

**Set `AUTH_KEY` to the same value on every node.** With the default (a random key per
process) a token issued by node A is rejected by node B: the browser gets
`AuthTokenInvalidError`, clears the token and calls `login` again. It self-heals, but it
costs a round trip and an auth state change every time a client lands on a different node.

Caveats worth knowing:

* Redis pub/sub is fire-and-forget. While one node's bridge is down its clients keep their
  sockets but miss whatever was published in that window. `/health-check` answers `503` so a
  load balancer can stop routing new connections to it, and the bridge re-subscribes by
  itself once Redis is back (verified by killing Redis during a live subscription: the
  subscription was restored and delivery resumed).
* When a visitor's connection moves from one node to another, the old node's
  `vi_online:false` and the new node's `vi_online:true` can arrive in either order, so the
  operator's online indicator may flicker once. Cosmetic.
* **Losing a node.** On a graceful stop the server closes every socket with code `1001`, so
  browsers reconnect at once. On a hard loss (crash, `kill -9`, host gone) the sockets just
  disappear: clients whose connection the proxy closes notice immediately, the others detect
  it through their own `pingTimeout` (20 s by default, tunable with `PING_INTERVAL_MS` /
  `PING_TIMEOUT_MS`), and anything published in that window is missed - the same
  fire-and-forget property as above. 
* `nginx.conf` logs the backend that served each request (`$upstream_addr`), which is the
  first thing you want when one node behaves differently from the other.

## Monitoring

Everywhere below `<container>` is the server container. Find it with

```bash
docker compose ps                                  # service is called golhchelper
docker ps --filter name=golhchelper --format '{{.Names}}'
```

Running it as a systemd service instead of a container? The same numbers are shown in
*Running as a systemd service -> Operating it* (`journalctl -u golhchelper`,
`systemctl show -p MainPID --value golhchelper`).

### Is it up?

```bash
docker compose ps                                     # "Up (healthy)"
docker inspect --format '{{.State.Health.Status}}' <container>
curl -fsS http://127.0.0.1:8000/health-check          # "OK", or "Failed" + HTTP 503
```

`/health-check` reports the Redis bridge only. It says nothing about how many clients are
connected - use the counts below for that.

### How many sockets are open

The server is a single binary and every client is one entry in its file descriptor table,
so the container's pid 1 is the whole story. These two return the same number, one from
inside the container, one from the host without entering it:

```bash
# inside the container (pid 1 is the server)
docker exec <container> sh -c 'ls /proc/1/fd | wc -l'

# from the host
pid=$(docker inspect -f '{{.State.Pid}}' <container>)
ls /proc/$pid/fd | wc -l
```

That total is **not** the client count. Measured on the running container with a known
number of connected clients:

| command | with N clients | what it is |
| --- | --- | --- |
| `ls /proc/1/fd \| wc -l` | N + 8 | sockets plus stdin/stdout/stderr, epoll, eventfd |
| `ls -l /proc/1/fd \| grep -c socket` | N + 3 | clients plus 1 listening socket plus 2 Redis connections |
| `ss -Htn state established '( sport = :8000 )' \| wc -l` | N | **the websocket client count** |

So with 3 clients the container shows 11 file descriptors, 6 sockets and 3 established
connections. Neither `N + 8` nor `N + 3` is a hard rule - the offset grows if you start
the binary from a shell (inherited log/pipe descriptors) or if the Redis bridge holds a
different number of connections - so take the baseline once after starting the container:

```bash
docker exec <container> sh -c 'ls -l /proc/1/fd | grep -c socket'   # sockets with 0 clients: 3
```

To see what the descriptors actually are (built on `readlink`, so a colourised `ls` alias
cannot break the output):

```bash
pid=$(docker inspect -f '{{.State.Pid}}' <container>)
for fd in /proc/$pid/fd/*; do readlink "$fd"; done | sed -E 's/[0-9]+//g' | sort | uniq -c | sort -rn
```

```text
      6 socket:[]                    # 3 clients + 1 listening socket + 2 Redis connections
      2 pipe:[]                      # stdout/stderr to the docker log driver
      1 anon_inode:[eventpoll]       # the socket event loop
      1 anon_inode:[eventfd]         # Go runtime wakeup
      1 /dev/null                    # stdin
```

That is the whole deployment: no threads or helper processes per connection.

For the number you actually want to graph, run this on the host - it counts the server's
side of every connection, so it is correct with `network_mode: host` as well as with
published ports, and clients running on the same host are not counted twice:

```bash
ss -Htn state established '( sport = :8000 )' | wc -l
# -H drops the header line, otherwise you are one too high
```

Baseline with nobody connected: 8 file descriptors, of which 3 are sockets (1 listening
socket, 2 Redis connections).

### Caveats

* **`network_mode: host`** (that compose file): inside the container `/proc/net/tcp`,
  `netstat` and `ss` show **the entire host**, not just the container - on this dev box
  `netstat -tn` listed 169 TCP entries for a server holding 11 file descriptors. Always
  filter by the port.
* **busybox `netstat` double counts locally connected clients.**
  `docker exec <c> netstat -tn | grep -c ':8000'` reported 100 for 50 clients because each
  connection appears twice - once from the server's side and once from the client's. For
  remote clients the number is correct. `ss -Htn state established '( sport = :8000 )'`
  does not have this problem.
* The image is Alpine with busybox: `ls`, `wc`, `netstat`, `ps` and `grep` exist, **`ss`
  does not**. Use `ss` from the host, or `ls /proc/1/fd` inside the container.

### Live view

```bash
while true; do
  pid=$(docker inspect -f '{{.State.Pid}}' <container>)
  printf '%s fds=%s ws-clients=%s rss=%s\n' "$(date +%T)" \
    "$(ls /proc/$pid/fd | wc -l)" \
    "$(ss -Htn state established '( sport = :8000 )' | wc -l)" \
    "$(awk '/VmRSS/{print $2" kB"}' /proc/$pid/status)"
  sleep 1
done
```

`watch -n1 docker stats` gives CPU and memory instead (about 28 KB per connection plus a
~10 MB base, measured at 1.4 GB for 50 000 connections).

### Several nodes

Each instance holds its own sockets, so sum the replicas - but note the socket count from
`ss` is already host wide and must not be summed:

```bash
# one line per node, then the total
for c in $(docker ps --filter name=golhchelper --format '{{.Names}}'); do
  printf '%s %s\n' "$c" "$(docker exec "$c" sh -c 'ls /proc/1/fd | wc -l')"
done | awk '{print; s+=$NF} END {print "total open fds:", s}'
```

With `docker-compose.scaled.yml` that is `golhchelper-1` and `golhchelper-2`; the
`nginx` and `redis` containers are not matched by the name filter.

### What to watch

| metric | how | why |
| --- | --- | --- |
| open fds | `ls /proc/<pid>/fd \| wc -l` | the limit is 65535 from `ulimits` in the compose files; one per client plus ~10 |
| websocket clients | `ss -Htn state established '( sport = :8000 )' \| wc -l` | correlate with the operators/visitors PHP thinks are online |
| memory | `docker stats` | ~28 KB per connection |
| CPU | `docker stats` | idle cost is mostly keep alive pings - see Load testing below |
| bridge state | `curl -f http://127.0.0.1:8000/health-check` | `503` means Redis is unreachable: sockets keep working, cross-node delivery does not |

The container's own limit can be checked from inside:

```bash
docker exec <container> sh -c 'grep -i "open files" /proc/1/limits'
# Max open files 65535 65535 files
```

Rule of thumb: when open fds reach ~80% of that limit, raise the `ulimits` (and the host's
`fs.file-max` / systemd `LimitNOFILE` if it runs without Docker) or add another instance -
the nodes need no coordination, so scaling out is the simple answer.

## Troubleshooting

### Containers take 10 s to stop and exit with code 137

`docker stop` sends SIGTERM and waits `stop_grace_period` (10 s) before SIGKILL, so `137`
means the signal never reached the process. Some daemon/security setups cannot deliver
signals to containers at all - `docker stop` or `docker kill -s TERM` then reports

    unable to signal init: permission denied

and *every* container on that host is affected, not just this one. The stack itself is
fine: when SIGTERM does arrive the server closes every websocket with code `1001` ("going
away") and exits in milliseconds, which is what lets browsers rebalance at once during a
rolling restart. Where signals cannot be delivered, stop the container with `-t 0`
(immediate SIGKILL) so it goes away at once instead of lingering for the grace period.

### `redis: DENIED ... protected mode is enabled and no password is set`

Redis accepted the TCP connection but refuses clients that are not loopback while no
password is set. Pick one:

```bash
# 1. Recommended - give Redis a password (protected mode stays enabled)
sudo sed -i 's/^# *requirepass .*/requirepass mysecret/' /etc/redis/redis.conf
sudo systemctl restart redis-server
#    then in .env:  REDIS_PASS=mysecret
#    and in extension/nodejshelper/settings/settings.ini.php:
#      'connect_db_pass' => 'mysecret'
#    PHP publishes over the same Redis, so it needs the password too

# 2. Or disable protected mode and keep the port firewalled
sudo sed -i 's/^protected-mode yes/protected-mode no/' /etc/redis/redis.conf
sudo systemctl restart redis-server

# 3. Or leave Redis completely alone when it is bound to 127.0.0.1 on this host - use
#    docker-compose.host-network.yml (Linux only). The container then shares the host
#    network namespace, so 127.0.0.1 is the real loopback and is always accepted
```

While this is broken `/health-check` answers `503` and the log repeats the hint at most
once a minute (it never dumps Redis' multi-page refusal text).

### `connection refused` / `i/o timeout`

Redis listens on loopback only (`bind 127.0.0.1`). Set `bind 0.0.0.0` (and firewall the
port) or use the `network_mode: host` variant above.

### `SECRET_HASH` containing `$` is silently mangled

Compose interpolates `$` inside `.env` and LHC secret hashes usually contain it, so
**single quote** the value:

```ini
SECRET_HASH='e9ekdkld4d0_D934-+_4535d_D9jasd@ASGFjkSDFfjksdffksdF456$#)8$asf931a171'
```

The symptom is a compose warning (`The "asf931a171" variable is not set`) followed by
every websocket `login` failing. Confirm what the container really got with
`docker compose exec golhchelper printenv SECRET_HASH`. The same applies to `AUTH_KEY`.

## Configuration

Everything that was spread between `server.js` (SocketCluster options +
`brokerOptions`) and `worker.js` is now environment driven:

| `server.js` / `worker.js` | Environment variable | Default |
| --- | --- | --- |
| `port` (`SOCKETCLUSTER_PORT`) | `SOCKETCLUSTER_PORT` / `PORT` | `8000` |
| `options.path` | `SOCKETCLUSTER_PATH` | `/socketcluster/` |
| `secretHash` (hard coded!) | `SECRET_HASH` | the value that was hard coded in `server.js`, **must match `site.secrethash`** |
| `options.authKey` (random per start when unset) | `AUTH_KEY` | random per start |
| `options.trackVisitors` | `TRACK_VISITORS` | `true` |
| `options.socketChannelLimit` | `SOCKET_CHANNEL_LIMIT` | `1000` |
| `options.pingInterval` / `pingTimeout` | `PING_INTERVAL_MS` / `PING_TIMEOUT_MS` | `8000` / `20000` |
| `options.handshakeTimeout` | `HANDSHAKE_TIMEOUT_MS` | `10000` |
| `options.origins` | `ORIGINS` | `*:*` |
| `allowClientPublish` | `ALLOW_CLIENT_PUBLISH` | `true` |
| - | `RESTRICT_CLIENT_PUBLISH` | `false` |
| - | `STRICT_CHANNEL_BINDING` | `true` |
| - | `SOCKET_SEND_BUFFER` | `256` |
| `brokerOptions.host/port/auth_pass` | `REDIS_HOST` / `REDIS_PORT` / `REDIS_PASS` / `REDIS_DB` | `127.0.0.1` / `6379` / - / `0` |
| `options.instanceId` (left unset) | `SC_INSTANCE_ID` | random per process |
| `serveStatic(path.resolve(__dirname, 'public'))` | `STATIC_DIR` | `public` |
| - | `MAX_PAYLOAD` | `4194304` (4 MiB) |
| `logLevel` (SocketCluster was always chatty) | `LOG_LEVEL` | `info` |

`LOG_LEVEL` is `debug`, `info`, `warn` or `error`. Each websocket connect/disconnect is
logged at `debug`, so the default output only contains what changed: the listening banner,
Redis (dis)connects, upgrade rejections and errors. Raise it to `debug` when a client
connects in a loop - the pair of lines with the socket id is then there again (the server
prints the level it picked as the first line, `LOG_LEVEL=warn` for a quiet journal).

`SOCKETCLUSTER_WORKERS`, `SOCKETCLUSTER_BROKERS` and the related clustering
variables are ignored - see *Scaling* below.

Set `AUTH_KEY` in production. Without it a random signing key is generated on
startup (exactly like SocketCluster did), so tokens stored in browsers stop
working after a restart and every client transparently calls `login` again.

## What was ported

`worker.js`:

* `#handshake` - verifies the auth token (HS256 JWT) and answers
  `{id, pingTimeout, isAuthenticated, authError?}`.
* `login` - the SHA1 scheme is unchanged:
  `SHA1(ts + 'Visitor' + secretHash [+ '_' + chatId]) . ts` and
  `SHA1(ts + 'Operator' + secretHash) . ts`, both valid for one hour.
  On success it stores `{token, exp, chanelName, instance_id, isChatToken, isVisitor}`
  (visitors 120 min, operators 12 h) and emits `#setAuthToken`.
* `MIDDLEWARE_SUBSCRIBE` - anonymous sockets are rejected, a visitor may only use
  the `chat_*` channel its token was issued for (`isChatToken` plus
  `STRICT_CHANNEL_BINDING`), and visitor presence (`{op:'vi_online'}`) is
  published to `chat_*` / `ous_<instance>` once the subscription is accepted.
* `#publish` - auto acked (`{rid}`) and fanned out locally plus to Redis.
  `RESTRICT_CLIENT_PUBLISH=true` additionally refuses channels the socket does
  not hold.
* `disconnect` - publishes `vi_online:false` to the chat channel or
  `ous_<instance>`.
* Ping/pong - the server sends `#1` every `PING_INTERVAL_MS`, clients answer `#2`,
  and a connection without traffic for `PING_TIMEOUT_MS` is closed with `4000`
  (`4005` if the handshake never arrived).
* HTTP - the optional `public/` static directory and `/health-check`, which reports
  `503` while the Redis bridge is down (`200 OK` otherwise).

`broker.js` / `sc-redis`:

* Redis channels are subscribed on the first local subscriber of a channel and
  unsubscribed when the last one leaves.
* Published payloads use the sc-redis wire format: `/o:<json>` for objects/arrays,
  `/s:<value>` for scalars, prefixed with `SC_INSTANCE_ID` when set.
* Incoming messages are delivered to local subscribers only (never re-published),
  so the bridge cannot loop.

## Scaling

The Node deployment needed a master process, N worker processes, M broker
processes, optional `scc-broker-client` state servers and Redis to keep them in
sync. Here one process serves every socket and Redis is only used to fan out
between instances, so horizontal scaling is just "run more containers" - no
sticky sessions are required, because PHP publishes into Redis and every instance
delivers to its own sockets.

## Intentional differences

Three changes are deliberate, everything else is a 1:1 port:

1. **Own messages are never delivered twice.** sc-redis recognised its own
   messages only through the `instanceId` prefix, and since `server.js` never set
   `instanceId`, every publish was also consumed back from Redis and delivered a
   second time to local subscribers. `SC_INSTANCE_ID` now defaults to a random
   value generated at start up (not hostname/pid, which would collide for two
   replicas running in the same host network namespace), which removes the
   duplicate.
2. **`/` inside JSON payloads is safe.** sc-redis stripped the instance prefix
   with `/^[^\/]*\//`, which also matched the first `/` inside the payload (that
   is why LHC replaces `/` with `__SL__` in streamed content). The prefix is now
   only stripped when it is directly followed by `o:` / `s:`. Both `o:{...}` (PHP)
   and `<instanceId>/o:{...}` (another node) are still accepted.
3. **Message size is capped** at `MAX_PAYLOAD` (SocketCluster's default was
   unlimited) so one connection cannot exhaust memory.
4. **A visitor token is bound to its chat channel.** worker.js accepted any
   `chat_*` channel as long as the token carried `isChatToken`, so a visitor
   could subscribe to - and with `allowClientPublish`, publish into - any other
   visitor's chat; chat ids are sequential, so that was a practical way to read
   them. `tokenvisitor.php` already signs the chat id into the hash and the
   widget logs in with the very same channel it subscribes to, so
   `STRICT_CHANNEL_BINDING` (default `true`) simply enforces what the comment in
   `worker.js` claimed. Set it to `false` for the old behaviour.

## Tests

```bash
go test ./...        # add -race for the detector
```

They run without Redis and without a network: websocket framing (all three
payload length encodings), the sc-redis wire format and the self-message skip,
token signing/verification, the `login` hash (visitor, chat, instance prefixed
and operator), origin checks, the hub subscribe/unsubscribe hooks, the fan out
encoding, the configuration defaults and the log rate limiter.

## Load testing

`loadtest/` is a load generator for the server. Like the server it uses the
standard library only, and it speaks the protocol itself (websocket, `#handshake`,
login, `#subscribe`, `#publish`, ping/pong), so one machine can hold tens of
thousands of connections - a browser would give up long before the server does.
It is not copied into the container image.

```bash
cd extension/nodejshelper/serversc/new-server
go build -o loadtest ./loadtest

# realistic: 5000 visitors, each on its own chat_* channel
./loadtest -url 127.0.0.1:8000 -secret "$SECRET_HASH" -connections 5000 -ramp 1000 -duration 60s

# worst case fan out: 5000 subscribers on one channel, one publishing every 200ms
./loadtest -secret "$SECRET_HASH" -connections 5000 -chat-ids 1 -publishers 1 -publish-interval 200ms

# pure broadcast: 10000 operator sockets on one channel, 2 publishes/s
./loadtest -secret "$SECRET_HASH" -connections 10000 -chat-ids 1 -mode operator -publishers 1 -publish-interval 500ms
```

The report counts opened/authenticated/subscribed connections, unexpected closes,
traffic and the p50/p90/p99/max latency of handshake, login, subscribe and fan
out (measured from the timestamp the publisher puts in the payload).

| flag | meaning |
| --- | --- |
| `-connections` | how many sockets to open |
| `-ramp` | connections opened per second (`0` = as fast as possible) |
| `-duration` | how long to hold them open and keep measuring |
| `-chat-ids` | `0` = one private channel per connection (realistic), `1` = all on `chat_900000` (fan out) |
| `-mode` | `visitor` publishes `vi_online` presence on subscribe, `operator` does not |
| `-publishers`, `-publish-interval` | how many connections publish, and how often |
| `-local-ips` | source addresses to spread the connections over |
| `-json` | machine readable report |
| `-progress` | live progress line (`0` = quiet) |

`-secret` is `site.secrethash` from the Live Helper Chat settings. While a test runs, watch
what the server does with it - see [Monitoring](#monitoring) for the fd, client and memory
commands.

### Two traps when the generator and the server share a host

* **Ephemeral ports.** Every connection to the same destination consumes one local
  port, and Linux has 28232 of them by default
  (`/proc/sys/net/ipv4/ip_local_port_range`). Past that the tester fails with
  `connect: cannot assign requested address` - which looks like a server limit but
  is not. Pass several loopback addresses to multiply the port space:
  `-local-ips 127.0.0.1,127.0.0.2,127.0.0.3,127.0.0.4`.
* **The generator costs resources too.** Roughly 5 goroutines and 6 KB of heap per
  connection, so at 50000 connections it is a real CPU consumer itself. On one box
  the two processes compete, which makes the server look slower than it is.

### Measured

8 vCPU host, server and generator on the same machine, all connections on
loopback, Go 1.21 build. Treat these as ballpark figures for one box: the
generator holds 50000 goroutines itself and both processes compete for the same 8
cores, so absolute latency is worse than with a generator on a separate machine
and CPU figures include that contention.

| scenario | result |
| --- | --- |
| 50000 connections, one channel each | 50000/50000 opened, 0 failed, 0 unexpected closes, 4838 conn/s, handshake p50 0.07 ms, login p50 0.06 ms, subscribe p50 0.12 ms |
| server during that run | 50016 open fds, 1.42 GB peak RSS (~28 KB per connection), 14 threads |
| server idle, no connections | 0.00% of a core |
| 20000 connections idle, default 8 s ping | 48% of one core |
| 20000 connections idle, `PING_INTERVAL_MS=16000` | 11.5% of one core |
| 10000 operator sockets on one channel, 2 publishes/s | 200000 deliveries, fan out p50 85 ms / p99 201 ms, about 117000 socket writes/s |
| 5000 *visitors* on one channel | subscribe p50 8.8 s - every subscribe publishes `vi_online` to all 5000 subscribers, so this layout is O(N^2); it is the worst case the tool can produce, not a realistic LHC layout |

Conclusions worth keeping in mind:

* Connection count itself is cheap - tens of thousands per instance, about 28 KB
  of memory each, no threads per connection.
* Idle CPU is dominated by the keep alive ping. SocketCluster's 8 s default is
  conservative; 15 s (still below the 20 s `pingTimeout` the browser client uses)
  cut idle CPU by a factor of four in the measurement above. The relationship was
  not linear (48% at 8 s, 11.5% at 16 s), which is what sharing 8 cores with a
  50000 goroutine generator looks like - verify against your own deployment before
  making it a tuning decision.
* Real LHC traffic gives every visitor a private channel with one or two
  subscribers, so the fan out path is normally exercised with a handful of
  sockets, not with thousands.

## Verifying

The behaviour was verified against the real v13 client bundle (`node_modules`
SocketCluster was not used) and a real Redis:

* handshake/login/JWT storage, invalid-token rejection
* subscribe + `vi_online` presence on subscribe and disconnect
* client publish fan out, Redis forwarding with the instance prefix
* subscribe restrictions (anonymous, online visitor on a `chat_*` channel)
* Redis -> websocket for PHP style `o:{...}`, for another node's
  `<id>/o:{...}`, and no duplicate for our own instance
* idle connection surviving longer than `pingTimeout` (ping loop works)
* two instances on ports 8000/8001 sharing one Redis (cross node fan out)
