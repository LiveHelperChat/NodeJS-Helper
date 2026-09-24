NodeJS-Helper
=============

NodeJS Helper extension. Add NodeJS support for Live Helper Chat and reduce server load.

Official documentation: https://doc.livehelperchat.com/docs/node-js

## Requirements

* Node.js on the server (required)
* Redis (optional - needed for publish notifications)
* Browser with WebSocket support (optional - works with IE8+, Firefox and Chrome)

## Installation

1. Place the `nodejshelper` folder in your server's extensions folder, so the path looks like `extension/nodejshelper/...`.
2. Install Node.js (Ubuntu, CentOS - on CentOS the easiest way is the EPEL repository).
3. To enable publish notifications install [Redis](http://redis.io/):
   * CentOS: `yum install redis`
   * Enable on boot: `systemctl enable redis.service`
   * Start: `systemctl start redis.service`
4. Install Composer dependencies: `cd extension/nodejshelper && composer install`
5. Install Node dependencies: `cd extension/nodejshelper/serversc/lhc && npm install`
6. In `nodejshelper/serversc/lhc/server.js` set the same secret hash as in your `settings.ini.php`.
7. Copy `extension/nodejshelper/settings/settings.ini.default.php` to `extension/nodejshelper/settings/settings.ini.php`.
8. Enable the extension in `settings.ini.php`:

   ```php
   'extensions' => array(
       0 => 'nodejshelper',
   ),
   ```

9. Clear the cache from the back office.
10. To verify the installation, go to `extension/nodejshelper/serversc/lhc` and execute `node server.js`.
11. Start a chat - you should see Node.js messages in the console.
12. To run Node.js as a service on CentOS: `npm install -g forever`.
13. Create a service file (e.g. `/usr/lib/systemd/system/nodejshelper.service`) and adjust the paths for your environment. You may also want a dedicated `nodejs` user (`adduser nodejs`):

    ```ini
    [Unit]
    Description=Live Helper Chat NodeJS Daemon

    [Service]
    User=nodejs
    ExecStart=/usr/bin/forever /var/www/client/lhc_web/extension/nodejshelper/serversc/lhc/server.js
    LimitNOFILE=100000

    [Install]
    WantedBy=multi-user.target
    ```

14. Start the service: `systemctl start nodejshelper.service`
15. Enable the service on startup: `systemctl enable nodejshelper.service`

## Running Node.js behind an Nginx proxy

In this example Node.js listens on port `8000` and Nginx proxies the whole domain to it:

```nginx
server {
    listen         *:80;
    server_name    node.livehelperchat.com;

    location / {
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header Host $http_host;
        proxy_set_header X-NginX-Proxy true;

        proxy_pass http://127.0.0.1:8000;
        proxy_redirect off;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
```

If you use a sub-location there is no need for a separate subdomain:

```nginx
location /socketcluster/ {
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header Host $http_host;
    proxy_set_header X-NginX-Proxy true;

    proxy_pass http://127.0.0.1:8000;
    proxy_redirect off;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
}
```

And the matching settings in `extension/nodejshelper/settings/settings.ini.php`:

```php
return array(
    'connect_db' => 'localhost',
    'connect_db_id' => 0,
    'automated_hosting' => false,
    'public_settings' => array(
        'hostname' => (isset($_SERVER['HTTP_HOST']) ? $_SERVER['HTTP_HOST'] : null),
        'path' => '/socketcluster/',
        'port' => null, // some custom port
        'secure' => null, // true || false
    )
);
```

If your website runs in `HTTPS` mode set `secure` to `true`.

## Running Node.js as a background service

Note that you may have to increase `ulimit` in the system to accept more than 1024 connections - otherwise Node.js stops accepting connections once the limit is reached.

## How it works and which AJAX calls it eliminates

1. When a user sends a message over ajax, the already formatted message list HTML is sent directly to Node.js and distributed in real time to all connected users (chatbox case). There is no background sync anymore with this extension. When a message is sent, the administrator is informed that there is information and only then the operator executes a sync call.
2. When the operator sends a message, the visitor is informed that a message is pending and an ajax call is executed. There are no continuously running ajax calls in the background - ajax runs only when there is information.
3. With publish notifications enabled, administration interface sync calls are also eliminated.

## Why AJAX calls are still involved

Rewriting everything would take a lot of time - it would involve permission checking, database access and in general duplicating PHP module actions. This extension distributes load across connected users and does not have to care about permissions, because standard ajax calls take care of that. It does not override a single core file or template. It is a hybrid between a full WebSockets application and an ajax based one.

## Troubleshooting: no messages received

* Enable debug output in the NodeJS extension by editing `settings.js`.
* When you accept or start a new chat as a client you should see actions in the console. Also check in the browser developer tools that there are no errors and that it connects to your server.

## FAQ

**Does it reduce back office operator sync calls?**
Yes. It waits for user actions and only when there is information does it execute an ajax sync call.

**Does this extension support the automated hosting plugin?**
Yes.

**What does the publish notifications option do?**
When enabled it eliminates administration sync calls (the chats list in the right column). Publish notifications are also used when the desktop client writes a message - the desktop client does not connect to Node.js, so the workflow is: Desktop client -> Web server -> Redis -> Node.js pulls notification -> emits signal to listening socket -> web browser issues an ajax request to update its messages list.

**Is there failover if Node.js dies or a client cannot connect?**
Yes. Ajax calls are eliminated only if the client successfully connects to Node.js. If during a chat session the customer or operator loses the connection to Node.js (for example Node.js dies), Live Helper Chat automatically falls back to standard ajax queries.

**Can I run several installations on one server?**
Yes. If you provide separate installations per client manually, just set a unique `instance_id` for each installation - it can be a number or text. Each installation then has its own space, so the extension can serve an unlimited number of chat instances on the same server as long as `instance_id` is unique.

## Go server (experimental - test project)

> **Warning** - this is an **experimental test project**. It is not battle tested and is
> **used entirely at your own risk**. It may contain bugs, change without notice, or be
> removed. Do not run it in production, and always test it in a staging environment first.
>
> **For maximum stability use the Node.js version** (`serversc/lhc`). The Go server is an
> alternative that is being evaluated, not a replacement for the proven Node.js stack.

`nodejshelper/serversc/go-server` is a dependency free, single binary rewrite of the
SocketCluster stack that used to live in `serversc/lhc` (`server.js` master + `worker.js`
workers + `broker.js` brokers + `sc-redis`).

Nothing changes for Live Helper Chat itself:

* **Browsers** keep using `design/nodejshelpertheme/js/socketcluster-client.js` (v13) -
  the server speaks SocketCluster Protocol V1.
* **PHP** keeps publishing over Redis (`erLhcoreClassNodeJSRedis`, i.e.
  `extension/nodejshelper/classes/lhpredis.php`) - the same channel names and the same
  `o:{...}` payloads.

In short:

* built as a single binary, no Node.js/npm dependencies at runtime;
* configuration is environment driven (`SECRET_HASH`, `REDIS_HOST`, `REDIS_PORT`,
  `AUTH_KEY`, `SOCKETCLUSTER_PORT`, `PING_INTERVAL_MS`, ...);
* `SECRET_HASH` must match `site.secrethash`, and `AUTH_KEY` must be identical on every
  node when running several instances;
* several instances can share one Redis and **no sticky sessions are needed**;
* `/health-check` reports `200 OK` while the Redis bridge is up and `503` otherwise;
* Docker Compose files are provided for a bundled Redis, an external Redis, host network
  (Redis bound to `127.0.0.1`, Linux only) and scaled/multi node setups.

Because it is a test project, treat the Node.js version as the supported path for
production and use the Go server only for experiments, comparisons and load testing.

See `nodejshelper/serversc/go-server/README.md` for build instructions, Docker Compose
usage, scaling, monitoring and troubleshooting details.

### Benchmarks (Go server vs Node.js server)

Both stacks were driven by the same `loadtest` generator on one host (Intel Xeon
E3-1270 v6, 4 cores / 8 threads, 46 GB RAM, Redis 8.0.5 on loopback, Go 1.26 build
against Node.js v22.22.1 with its shipped `SOCKETCLUSTER_WORKERS=1`). The
generator is co-located, so treat these as ballpark figures for a single box. The
full tables - CPU, RSS, the fan out and storm runs, and the 8 worker variant - are
in `nodejshelper/serversc/go-server/README.md#measured`, the reproduction steps in
`nodejshelper/serversc/go-server/loadtest/README.md`.

| | Go | Node.js (1 worker) |
| --- | --- | --- |
| fresh start, 0 connections | 14 MB RSS, 1 process, ready in ~0.1 s | 416 MB RSS, 4 processes, ready in ~0.8 s |
| 2 000 visitors, one private channel each | 2000/2000 opened, subscribe p99 1.2 ms, fan out p99 1 ms, 70 MB RSS | 2000/2000 opened, subscribe p99 7.6 ms, fan out p99 7 ms, 416 MB RSS |
| 5 000 visitors, one private channel each | 5000/5000 opened, subscribe p99 0.8 ms, fan out p99 1 ms, 146 MB RSS | 5000/5000 opened, subscribe p99 6.3 ms, fan out p99 7 ms, 421 MB RSS |
| 30 000 idle connections | 30000/30000 opened, subscribe p99 0.6 ms, 67% of a core, 897 MB RSS | 30000/30000 opened, subscribe p99 9.0 ms, 103% of a core, 754 MB RSS |
| 3 000 visitors on one hot channel | 3000/3000 kept, 7.4 M messages delivered, fan out p99 152 ms | 925/3000 kept, 1.2 M messages delivered, fan out p99 324 ms |

In short: for the layouts Live Helper Chat actually produces (one private channel
per visitor) both stacks cope with tens of thousands of connections - the Go
server with roughly ten times lower subscribe latency and a four hundred megabytes
smaller footprint. The difference shows up on a single hot channel, where the
Node.js worker stops completing websocket handshakes at about 900 subscribers
while the Go server holds 3 000 on the same channel and machine.

The hot channel row is a "how much of the requested load did each stack keep
alive" comparison, not a latency comparison at equal load: the Node.js worker
never reached 3 000 subscribers, so its fan out latency was measured with two
thirds fewer sockets on the channel. Adding `SOCKETCLUSTER_WORKERS=8` moves that
wall to ~2 275 but makes the fan out latency nearly three times worse and grows
the footprint to 1.4 GB.