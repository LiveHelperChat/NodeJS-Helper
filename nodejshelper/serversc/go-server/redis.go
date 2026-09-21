package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// Timeouts and intervals of the two Redis clients.
const (
	// publishTimeout bounds one PUBLISH, dial included. Publishes run in the socket
	// goroutine that received the event, so this is also how long an unreachable
	// Redis may stall that connection (the hand written client dialled with a 5s
	// timeout and then retried once, holding it for up to ~10s).
	publishTimeout = 2 * time.Second

	// subscribeTimeout bounds a SUBSCRIBE/UNSUBSCRIBE, which also comes from a socket
	// goroutine (first and last subscriber of a channel).
	subscribeTimeout = 2 * time.Second

	// redisProbeInterval is how long the subscription may stay quiet before a PING is
	// written to it. The same value go-redis uses for its own Channel() health check.
	redisProbeInterval = 3 * time.Second

	// redisProbeTimeout bounds that PING.
	redisProbeTimeout = 5 * time.Second
)

// redisBridge replaces node_modules/sc-redis. The wire format is unchanged (see
// below); only the connection layer is delegated now - to go-redis, which owns RESP
// parsing, dialing, reconnection and re-subscribing. That was the part of this file
// that used to be hand written.
//
// Original behaviour (node_modules/sc-redis/index.js):
//
//	publish  : <instanceId?>/o:<json>   for objects/arrays
//	           <instanceId?>/s:<value>  for scalars
//	subscribe: one Redis channel per SC channel that has local subscribers
//
// Messages published by Live Helper Chat (extension/nodejshelper/classes/lhpredis.php)
// look exactly like that, e.g. `o:{"op":"cmsg"}`.
type redisBridge struct {
	instanceID string

	// publisher serves PUBLISH. It is a small pool with tight timeouts, so one slow
	// publish cannot serialise the others (the old code used a single connection
	// behind a mutex, and a retry that could publish twice).
	publisher *redis.Client

	// subscriber is the one dedicated PubSub connection for every channel that has
	// local subscribers. PubSub keeps the channel set itself and re-subscribes all of
	// it after a reconnect - including channels that were added while Redis was down.
	subscriber *redis.PubSub

	// deliver is called for messages coming from Redis. It only fans out to
	// local sockets - it never publishes back to Redis (sc-simple-broker did
	// the same, which is what keeps the bridge loop free).
	deliver func(channel string, data json.RawMessage)

	mu         sync.Mutex
	subscribed map[string]struct{}
	lastError  string
	lastLog    time.Time

	// lastActivityNs is when a reply was last read, lastErrorNs when a connection
	// level failure last happened. Health means "a reply arrived after the last
	// failure", so a write that the kernel merely buffered can never make the bridge
	// look healthy - see Healthy().
	lastActivityNs atomic.Int64
	lastErrorNs    atomic.Int64

	// awaitingConnectLog makes the next reply log a "connected" line: it starts set,
	// and every failure sets it again (the hand written client logged each reconnect).
	awaitingConnectLog atomic.Bool
}

func newRedisBridge(host string, port int, user, pass string, db int, instanceID string, deliver func(string, json.RawMessage)) *redisBridge {
	// Shared by both clients.
	options := &redis.Options{
		Addr:     net.JoinHostPort(host, strconv.Itoa(port)),
		Username: user,
		Password: pass,
		DB:       db,
		// RESP2 on purpose: it is what the hand written client spoke, needs no HELLO
		// handshake and works on every Redis version Live Helper Chat supports, while
		// RESP3 would deliver PubSub replies as push messages.
		Protocol: 2,
		// No command retries - recovery belongs to the loops below, and a retry would
		// only delay the error the caller has to handle anyway.
		MaxRetries: -1,
		// One dial attempt instead of the default five (which add 100ms of backoff
		// each, and seconds per attempt when a host black holes the connection).
		DialerRetries: 1,
		DialTimeout:   2 * time.Second,
	}

	publishOptions := *options
	publishOptions.PoolSize = 8
	publishOptions.ReadTimeout = publishTimeout
	publishOptions.WriteTimeout = publishTimeout

	subscribeOptions := *options
	// Zero on purpose: an idle subscription blocks in a read, and a read timeout is
	// classified as a broken connection by go-redis, which would turn every quiet
	// period into a reconnect. probeLoop() checks liveness instead.
	subscribeOptions.ReadTimeout = 0
	subscribeOptions.WriteTimeout = subscribeTimeout

	b := &redisBridge{
		instanceID: instanceID,
		publisher:  redis.NewClient(&publishOptions),
		deliver:    deliver,
		subscribed: make(map[string]struct{}),
	}
	b.awaitingConnectLog.Store(true)

	// Client.Subscribe() only sends SUBSCRIBE when channels are given, so this creates
	// the PubSub without connecting yet - receiveLoop()/probeLoop() dial it.
	b.subscriber = redis.NewClient(&subscribeOptions).Subscribe(context.Background())

	return b
}

func nextBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > 10*time.Second {
		return 10 * time.Second
	}
	return next
}

// configErrorRetryDelay is used when Redis refused the connection for a reason that a
// retry will not fix (no password / protected mode). Retrying every second would only
// fill the log while the fix needs a Redis restart anyway.
const configErrorRetryDelay = 10 * time.Second

// isRedisConfigError reports whether Redis refused us because of its configuration
// rather than because of a transient network problem.
func isRedisConfigError(err error) bool {
	if err == nil {
		return false
	}

	message := err.Error()
	for _, marker := range []string{"protected mode", "NOAUTH", "WRONGPASS", "Client sent AUTH", "invalid username-password"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

// redisHint replaces Redis' multi-page refusal text with the one line fix.
func redisHint(err error) string {
	message := err.Error()

	switch {
	case strings.Contains(message, "protected mode"):
		return "Redis refused the connection: protected-mode is on and no password is set, so it only " +
			"accepts loopback clients. Fix (pick one): add `requirepass <password>` to redis.conf and set " +
			"REDIS_PASS, or set `protected-mode no` (keep the port firewalled), then restart Redis. " +
			"When Redis is bound to 127.0.0.1 on the Docker host, docker-compose.host-network.yml " +
			"avoids the Redis change entirely."
	case strings.Contains(message, "Client sent AUTH"):
		return "Redis has no password configured but REDIS_PASS is set - remove REDIS_PASS."
	case strings.Contains(message, "WRONGPASS"), strings.Contains(message, "invalid username-password"):
		return "Redis rejected the password - check REDIS_PASS (and REDIS_USER if set)."
	case strings.Contains(message, "NOAUTH"):
		return "Redis requires a password - set REDIS_PASS."
	}

	return ""
}

// reportError logs a Redis problem once: a new problem immediately, the same problem at
// most once a minute.
func (b *redisBridge) reportError(err error) {
	message := err.Error()
	if hint := redisHint(err); hint != "" {
		message = hint
	}

	b.mu.Lock()
	shouldLog := message != b.lastError || time.Since(b.lastLog) > time.Minute
	b.lastError = message
	if shouldLog {
		b.lastLog = time.Now()
	}
	b.mu.Unlock()

	if shouldLog {
		log.Printf("[redis] %s", message)
	}
}

// clearError resets the suppression state after a successful connection.
func (b *redisBridge) clearError() {
	b.mu.Lock()
	b.lastError = ""
	b.mu.Unlock()
}

// noteError records a connection level failure: it is logged through reportError()
// (deduplicated) and makes Healthy() false until a reply arrives again.
func (b *redisBridge) noteError(err error) {
	b.awaitingConnectLog.Store(true)
	b.lastErrorNs.Store(time.Now().UnixNano())
	b.reportError(err)
}

// markActivity records a reply from Redis - a message, a subscribe confirmation or the
// pong of a health ping. Any of them proves the connection works.
func (b *redisBridge) markActivity() {
	b.lastActivityNs.Store(time.Now().UnixNano())

	if b.awaitingConnectLog.Swap(false) {
		b.mu.Lock()
		channels := len(b.subscribed)
		b.mu.Unlock()

		log.Printf("[redis] connected, subscribed to %d channel(s)", channels)
	}
}

// Start launches the reader and the health probe. Both run for the lifetime of the
// process and reconnect on their own.
func (b *redisBridge) Start() {
	go b.receiveLoop()
	go b.probeLoop()
}

// receiveLoop reads replies and hands PubSub messages to deliver.
//
// Deliberately a single goroutine, like before: deliver() fans out to local sockets
// and must not run concurrently with itself. Receive() is used instead of
// PubSub.Channel() so that a slow fan out (see the note on Hub.PublishLocal) applies
// back pressure instead of dropping messages that do not fit in a buffer for a
// minute.
//
// Reconnecting does not happen here: a failed read closes the connection inside
// go-redis, and the next Receive() dials again and re-subscribes every channel it has
// on record. This loop only slows the retries down.
func (b *redisBridge) receiveLoop() {
	backoff := time.Second

	for {
		reply, err := b.subscriber.Receive(context.Background())
		if err != nil {
			b.noteError(err)

			if isRedisConfigError(err) {
				time.Sleep(configErrorRetryDelay)
				continue
			}

			time.Sleep(backoff)
			backoff = nextBackoff(backoff)
			continue
		}

		backoff = time.Second

		b.markActivity()

		if msg, ok := reply.(*redis.Message); ok {
			b.handleMessage(msg.Channel, msg.Payload)
		}
	}
}

// probeLoop keeps an idle connection honest. While replies keep arriving nothing is
// checked - traffic is the best health signal there is - but after
// redisProbeInterval of silence a PING is written, which is what notices a connection
// that died without the kernel telling the blocked reader about it.
//
// go-redis does exactly this for its own Channel() API (checkInterval/pingTimeout).
func (b *redisBridge) probeLoop() {
	// Immediately once, so /health-check (and the "connected" log line) does not have
	// to wait for the first interval, then every redisProbeInterval.
	b.probe()

	ticker := time.NewTicker(redisProbeInterval)
	defer ticker.Stop()

	for range ticker.C {
		b.probe()
	}
}

// probe writes a PING when the connection has been quiet for redisProbeInterval.
func (b *redisBridge) probe() {
	if time.Since(time.Unix(0, b.lastActivityNs.Load())) < redisProbeInterval {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), redisProbeTimeout)
	err := b.subscriber.Ping(ctx)
	cancel()

	if err != nil {
		b.noteError(err)
		return
	}

	// A working connection logs again after an outage instead of staying
	// suppressed by reportError().
	b.clearError()
}

// Healthy reports whether Redis is reachable and the subscription is being served.
// /health-check uses it so a broken bridge shows up as a failing container instead of
// silently swallowing every message that PHP publishes.
//
// Only a reply counts: a successful write can be no more than the local socket buffer
// accepting bytes, which is exactly what happens when the peer is gone.
func (b *redisBridge) Healthy() bool {
	activity := b.lastActivityNs.Load()
	return activity != 0 && activity > b.lastErrorNs.Load()
}

// EnsureSubscribed mirrors sc-redis' `broker.on('subscribe')` hook.
func (b *redisBridge) EnsureSubscribed(channel string) {
	b.mu.Lock()
	if _, ok := b.subscribed[channel]; ok {
		b.mu.Unlock()
		return
	}
	b.subscribed[channel] = struct{}{}
	b.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), subscribeTimeout)
	defer cancel()

	// PubSub records the channel even when the command fails, so a subscription that
	// arrives while Redis is down is replayed on the next successful connect - that is
	// what replaced the manual reconnect bookkeeping.
	if err := b.subscriber.Subscribe(ctx, channel); err != nil {
		b.noteError(err)
	}
}

// EnsureUnsubscribed mirrors sc-redis' `broker.on('unsubscribe')` hook.
func (b *redisBridge) EnsureUnsubscribed(channel string) {
	b.mu.Lock()
	if _, ok := b.subscribed[channel]; !ok {
		b.mu.Unlock()
		return
	}
	delete(b.subscribed, channel)
	b.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), subscribeTimeout)
	defer cancel()

	// The channel always has to be named here: PubSub.Unsubscribe() without arguments
	// drops every subscription, not just this one.
	if err := b.subscriber.Unsubscribe(ctx, channel); err != nil {
		b.noteError(err)
	}
}

// Publish mirrors sc-redis' `broker.on('publish')` hook.
func (b *redisBridge) Publish(channel string, data json.RawMessage) {
	payload := encodeForRedis(b.instanceID, data)

	// Bounded, and without a retry, so an unreachable Redis cannot hold the calling
	// socket goroutine - the pool re-establishes the connection on its own.
	ctx, cancel := context.WithTimeout(context.Background(), publishTimeout)
	defer cancel()

	if err := b.publisher.Publish(ctx, channel, payload).Err(); err != nil {
		// Not logged per channel: when Redis is down every publish fails, and
		// noteError() already deduplicates the message to once a minute.
		b.noteError(err)
	}
}

// handleMessage decodes one Redis message the same way sc-redis did, with one
// deliberate deviation: sc-redis used the regex `^[^\/]*\/` to strip the
// instance prefix, which also swallowed the first `/` found *inside* a JSON
// payload (that is why LHC replaces `/` with `__SL__` in streamed content).
// Here the prefix is only stripped when the message really starts with
// `<id>/o:` or `<id>/s:`.
func (b *redisBridge) handleMessage(channel, raw string) {
	sender, message := splitRedisPayload(raw)

	// Do not deliver messages this instance published itself - they were
	// already fanned out locally.
	if sender != "" && sender == b.instanceID {
		return
	}
	if len(message) < 2 || message[1] != ':' {
		return
	}

	var data json.RawMessage
	if message[0] == 'o' {
		body := message[2:]
		if json.Valid([]byte(body)) {
			data = json.RawMessage(body)
		} else {
			encoded, _ := json.Marshal(body)
			data = encoded
		}
	} else {
		encoded, _ := json.Marshal(message[2:])
		data = encoded
	}

	if b.deliver != nil {
		b.deliver(channel, data)
	}
}

func splitRedisPayload(raw string) (sender, message string) {
	// sc-redis emitted a bare "/o:<payload>" when no instance id was configured.
	if strings.HasPrefix(raw, "/") {
		return "", raw[1:]
	}
	// `o:` / `s:` - no prefix at all (this is what lhpredis.php sends)
	if strings.HasPrefix(raw, "o:") || strings.HasPrefix(raw, "s:") {
		return "", raw
	}
	// `<instanceId>/o:<payload>` - sc-redis with an instance id. The prefix is only
	// stripped when a type tag follows, so a `/` inside the JSON payload is safe.
	if slash := strings.IndexByte(raw, '/'); slash > 0 {
		rest := raw[slash+1:]
		if strings.HasPrefix(rest, "o:") || strings.HasPrefix(rest, "s:") {
			return raw[:slash], rest
		}
	}
	return "", raw
}

// encodeForRedis builds the wire payload exactly like sc-redis:
//
//	'/o:' + JSON.stringify(data)  when data is an object/array
//	'/s:' + data                  otherwise
//	optionally prefixed with the instance id
func encodeForRedis(instanceID string, data json.RawMessage) string {
	trimmed := bytes.TrimSpace(data)

	var payload string
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		payload = "/o:" + string(trimmed)
	} else {
		value := string(trimmed)
		if len(value) > 1 && value[0] == '"' {
			var unquoted string
			if err := json.Unmarshal(trimmed, &unquoted); err == nil {
				value = unquoted
			}
		}
		payload = "/s:" + value
	}

	if instanceID != "" {
		payload = instanceID + payload
	}

	return payload
}
