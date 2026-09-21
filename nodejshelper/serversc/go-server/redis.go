package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// redisBridge is a dependency free replacement for node_modules/sc-redis.
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
	host       string
	port       int
	pass       string
	db         int
	instanceID string

	// deliver is called for messages coming from Redis. It only fans out to
	// local sockets - it never publishes back to Redis (sc-simple-broker did
	// the same, which is what keeps the bridge loop free).
	deliver func(channel string, data json.RawMessage)

	mu         sync.Mutex
	subConn    net.Conn
	subWriter  *bufio.Writer
	subscribed map[string]struct{}
	lastError  string
	lastLog    time.Time

	// connected tracks the subscription connection, it backs /health-check.
	connected atomic.Bool

	pubMu     sync.Mutex
	pubConn   net.Conn
	pubReader *bufio.Reader
	pubWriter *bufio.Writer
}

func newRedisBridge(host string, port int, pass string, db int, instanceID string, deliver func(string, json.RawMessage)) *redisBridge {
	return &redisBridge{
		host:       host,
		port:       port,
		pass:       pass,
		db:         db,
		instanceID: instanceID,
		deliver:    deliver,
		subscribed: make(map[string]struct{}),
	}
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

func (b *redisBridge) dial() (net.Conn, *bufio.Reader, *bufio.Writer, error) {
	address := net.JoinHostPort(b.host, strconv.Itoa(b.port))
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		return nil, nil, nil, err
	}

	// One reader per connection: bufio may buffer more than the current reply, so it
	// has to stay with the connection for its whole life.
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	commands := 0
	if b.pass != "" {
		if err := writeCommand(writer, "AUTH", b.pass); err != nil {
			conn.Close()
			return nil, nil, nil, err
		}
		commands++
	}
	if b.db != 0 {
		if err := writeCommand(writer, "SELECT", strconv.Itoa(b.db)); err != nil {
			conn.Close()
			return nil, nil, nil, err
		}
		commands++
	}
	// PING proves Redis accepted us, so a protected-mode or auth refusal is reported
	// as such instead of a bogus "connected" line.
	if err := writeCommand(writer, "PING"); err != nil {
		conn.Close()
		return nil, nil, nil, err
	}
	commands++

	if err := writer.Flush(); err != nil {
		conn.Close()
		return nil, nil, nil, err
	}

	for i := 0; i < commands; i++ {
		if _, err := readRESP(reader); err != nil {
			conn.Close()
			return nil, nil, nil, err
		}
	}

	return conn, reader, writer, nil
}

// Start launches the subscriber loop. It reconnects on its own and re-subscribes
// every channel that currently has local subscribers.
func (b *redisBridge) Start() {
	go b.subscribeLoop()
}

func (b *redisBridge) subscribeLoop() {
	backoff := time.Second

	for {
		conn, reader, writer, err := b.dial()
		if err != nil {
			b.connected.Store(false)
			b.reportError(err)
			time.Sleep(backoff)
			backoff = nextBackoff(backoff)
			continue
		}
		backoff = time.Second

		b.mu.Lock()
		b.subConn = conn
		b.subWriter = writer
		channels := make([]string, 0, len(b.subscribed))
		for channel := range b.subscribed {
			channels = append(channels, channel)
		}
		for _, channel := range channels {
			_ = writeCommand(writer, "SUBSCRIBE", channel)
		}
		flushErr := writer.Flush()
		b.mu.Unlock()

		if flushErr != nil {
			b.connected.Store(false)
			b.reportError(flushErr)
			conn.Close()
			continue
		}

		if !b.connected.Swap(true) {
			log.Printf("[redis] connected, subscribed to %d channel(s)", len(channels))
		}
		b.clearError()

		var configErr error
		for {
			reply, err := readRESP(reader)
			if err != nil {
				configErr = err
				b.connected.Store(false)
				b.reportError(err)
				conn.Close()

				b.mu.Lock()
				b.subConn = nil
				b.subWriter = nil
				b.mu.Unlock()
				break
			}

			// PubSub messages arrive as `*3` arrays of bulk strings:
			// ["message", <channel>, <payload>]
			parts, ok := reply.([]any)
			if !ok || len(parts) < 3 {
				continue // subscribe/unsubscribe confirmations and the like
			}
			if respString(parts[0]) != "message" {
				continue
			}
			channel := respString(parts[1])
			payload := respBytes(parts[2])
			b.handleMessage(channel, string(payload))
		}

		if isRedisConfigError(configErr) {
			time.Sleep(configErrorRetryDelay)
			continue
		}

		time.Sleep(backoff)
		backoff = nextBackoff(backoff)
	}
}

// Healthy reports whether the Redis subscription connection is up. /health-check
// uses it so a broken bridge shows up as a failing container instead of silently
// swallowing every message that PHP publishes.
func (b *redisBridge) Healthy() bool {
	return b.connected.Load()
}

// EnsureSubscribed mirrors sc-redis' `broker.on('subscribe')` hook.
func (b *redisBridge) EnsureSubscribed(channel string) {
	b.mu.Lock()
	if _, ok := b.subscribed[channel]; ok {
		b.mu.Unlock()
		return
	}
	b.subscribed[channel] = struct{}{}
	writer := b.subWriter
	if writer == nil {
		b.mu.Unlock()
		return // reconnect handler will SUBSCRIBE it
	}
	err := writeCommand(writer, "SUBSCRIBE", channel)
	if err == nil {
		err = writer.Flush()
	}
	b.mu.Unlock()

	if err != nil {
		log.Printf("[redis] SUBSCRIBE %s failed: %v", channel, err)
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
	writer := b.subWriter
	if writer == nil {
		b.mu.Unlock()
		return
	}
	err := writeCommand(writer, "UNSUBSCRIBE", channel)
	if err == nil {
		err = writer.Flush()
	}
	b.mu.Unlock()

	if err != nil {
		log.Printf("[redis] UNSUBSCRIBE %s failed: %v", channel, err)
	}
}

// Publish mirrors sc-redis' `broker.on('publish')` hook.
func (b *redisBridge) Publish(channel string, data json.RawMessage) {
	payload := encodeForRedis(b.instanceID, data)
	if err := b.writePublish(channel, payload); err != nil {
		log.Printf("[redis] PUBLISH %s failed: %v", channel, err)
	}
}

func (b *redisBridge) writePublish(channel, payload string) error {
	b.pubMu.Lock()
	defer b.pubMu.Unlock()

	err := b.sendPublishLocked(channel, payload)
	if err == nil {
		return nil
	}

	// Retry once with a fresh connection - the previous one may have been idle
	// for too long and dropped by Redis.
	if b.pubConn != nil {
		b.pubConn.Close()
		b.pubConn = nil
		b.pubReader = nil
		b.pubWriter = nil
	}

	if err := b.sendPublishLocked(channel, payload); err != nil {
		return err
	}
	return nil
}

func (b *redisBridge) sendPublishLocked(channel, payload string) error {
	if b.pubConn == nil {
		conn, reader, writer, err := b.dial()
		if err != nil {
			return err
		}
		b.pubConn = conn
		b.pubReader = reader
		b.pubWriter = writer
	}

	// Bound the round trip so a hung Redis cannot block a socket goroutine forever.
	_ = b.pubConn.SetDeadline(time.Now().Add(5 * time.Second))
	defer func() { _ = b.pubConn.SetDeadline(time.Time{}) }()

	if err := writeCommand(b.pubWriter, "PUBLISH", channel, payload); err != nil {
		return err
	}
	if err := b.pubWriter.Flush(); err != nil {
		return err
	}

	// Consume the `:N` reply - otherwise replies pile up on this connection until
	// Redis hits its client output buffer limit.
	_, err := readRESP(b.pubReader)
	return err
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

// --- minimal RESP client -----------------------------------------------------

var errRESP = errors.New("redis: protocol error")

func writeCommand(writer *bufio.Writer, args ...string) error {
	if _, err := writer.WriteString("*" + strconv.Itoa(len(args)) + "\r\n"); err != nil {
		return err
	}
	for _, arg := range args {
		if _, err := writer.WriteString("$" + strconv.Itoa(len(arg)) + "\r\n"); err != nil {
			return err
		}
		if _, err := writer.WriteString(arg + "\r\n"); err != nil {
			return err
		}
	}
	return nil
}

func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// readRESP parses one reply. Bulk strings are returned as []byte, arrays as
// []any, integers as int64 and simple strings as string.
func readRESP(reader *bufio.Reader) (any, error) {
	line, err := readLine(reader)
	if err != nil {
		return nil, err
	}
	if len(line) == 0 {
		return nil, errRESP
	}

	switch line[0] {
	case '+':
		return line[1:], nil
	case '-':
		return nil, errors.New("redis: " + line[1:])
	case ':':
		n, err := strconv.ParseInt(line[1:], 10, 64)
		if err != nil {
			return nil, errRESP
		}
		return n, nil
	case '$':
		size, err := strconv.Atoi(line[1:])
		if err != nil {
			return nil, errRESP
		}
		if size < 0 {
			return nil, nil
		}
		buf := make([]byte, size+2)
		if _, err := readFull(reader, buf); err != nil {
			return nil, err
		}
		return buf[:size], nil
	case '*':
		count, err := strconv.Atoi(line[1:])
		if err != nil {
			return nil, errRESP
		}
		if count < 0 {
			return nil, nil
		}
		items := make([]any, 0, count)
		for i := 0; i < count; i++ {
			item, err := readRESP(reader)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, nil
	default:
		return nil, errRESP
	}
}

func readFull(reader *bufio.Reader, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := reader.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// respString converts a parsed RESP scalar to a string. Depending on the reply
// type Redis uses either a simple string (`+`) or a bulk string (`$`).
func respString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	}
	return ""
}

func respBytes(value any) []byte {
	switch typed := value.(type) {
	case []byte:
		return typed
	case string:
		return []byte(typed)
	}
	return nil
}
