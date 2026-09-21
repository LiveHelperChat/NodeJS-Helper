// Command loadtest opens a configurable number of real SocketCluster connections against a
// nodejshelper server and reports how it copes.
//
// It speaks SocketCluster protocol v1 itself (websocket client + #handshake + login +
// #subscribe + #publish), so a single machine can hold tens of thousands of connections -
// the shipped browser client would be the bottleneck long before the server is.
//
//	go build -o loadtest ./loadtest
//
//	# 1000 visitors opened at 200/s, kept for 60s, all on chat_900000, one publisher
//	./loadtest -url 127.0.0.1:8000 -secret "<site.secrethash>" -connections 1000 -ramp 200 -duration 60s
//
//	# worst case fan out: 5000 subscribers on one channel, one publishing every 200ms
//	./loadtest -secret ... -connections 5000 -chat-ids 1 -publishers 1 -publish-interval 200ms
//
//	# realistic spread: one private channel per connection, no publishing
//	./loadtest -secret ... -connections 2000 -chat-ids 0 -publishers 0
//
// The server side can be watched while it runs with
// `docker stats` and `ls /proc/$(pgrep lhcnodejs)/fd | wc -l`.
package main

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/textproto"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type config struct {
	host        string
	path        string
	secure      bool
	secret      string
	connections int
	ramp        int
	duration    time.Duration
	chatIDs     int
	publishers  int
	pubInterval time.Duration
	mode        string
	localIPs    string
	jsonOut     bool
	progress    time.Duration
	timeout     time.Duration
}

var cfg config

// Source addresses to round robin the outgoing connections over.
//
// When this tool and the server run on the same host, every connection costs one
// ephemeral port of a single source address - 28232 of them by default on Linux. The
// tester then fails with "cannot assign requested address" long before the server is
// busy, which looks like a server limit but is not. Spreading over several source
// addresses (127.0.0.2, 127.0.0.3, ... on Linux) multiplies the available ports.
var (
	sourceIPs       []net.IP
	sourceIPCounter atomic.Int64
)

func main() {
	flag.StringVar(&cfg.host, "url", "127.0.0.1:8000", "host:port of the nodejshelper server")
	flag.StringVar(&cfg.path, "path", "/socketcluster/", "websocket path")
	flag.BoolVar(&cfg.secure, "secure", false, "use wss://")
	flag.StringVar(&cfg.secret, "secret", "", "site.secrethash (required)")
	flag.IntVar(&cfg.connections, "connections", 100, "number of connections to open")
	flag.IntVar(&cfg.ramp, "ramp", 100, "connections opened per second (0 = as fast as possible)")
	flag.DurationVar(&cfg.duration, "duration", 30*time.Second, "how long to keep the connections open")
	flag.IntVar(&cfg.chatIDs, "chat-ids", 0, "spread the connections over this many channels (0 = one private channel per connection, 1 = all on chat_900000)")
	flag.IntVar(&cfg.publishers, "publishers", 1, "how many connections also publish periodically")
	flag.DurationVar(&cfg.pubInterval, "publish-interval", time.Second, "publish interval for -publishers")
	flag.StringVar(&cfg.mode, "mode", "visitor", "visitor|operator (operator tokens publish no presence)")
	flag.StringVar(&cfg.localIPs, "local-ips", "", "comma separated source IPs to spread the connections over (needed above ~28k connections when the tester and the server share one host)")
	flag.BoolVar(&cfg.jsonOut, "json", false, "print the report as JSON")
	flag.DurationVar(&cfg.progress, "progress", 5*time.Second, "progress line interval (0 = quiet)")
	flag.DurationVar(&cfg.timeout, "timeout", 10*time.Second, "per step timeout")
	flag.Parse()

	if cfg.secret == "" {
		fmt.Fprintln(os.Stderr, "error: -secret is required (site.secrethash from the Live Helper Chat settings)")
		os.Exit(2)
	}
	if cfg.connections < 1 {
		fmt.Fprintln(os.Stderr, "error: -connections must be at least 1")
		os.Exit(2)
	}
	if !strings.Contains(cfg.host, ":") {
		cfg.host += ":8000"
	}
	if cfg.publishers > cfg.connections {
		cfg.publishers = cfg.connections
	}
	if cfg.mode != "visitor" && cfg.mode != "operator" {
		fmt.Fprintln(os.Stderr, `error: -mode must be "visitor" or "operator"`)
		os.Exit(2)
	}

	var err error
	if sourceIPs, err = parseSourceIPs(cfg.localIPs); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}

	raiseFileLimit(cfg.connections + 64)
	run()
}

// parseSourceIPs turns the -local-ips value into addresses, keeping the order so a
// round robin gives every address an even share of the port space.
func parseSourceIPs(value string) ([]net.IP, error) {
	var ips []net.IP
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		ip := net.ParseIP(part)
		if ip == nil {
			return nil, fmt.Errorf("invalid -local-ips entry %q", part)
		}
		ips = append(ips, ip)
	}
	return ips, nil
}

// --- metrics -----------------------------------------------------------------

type latency struct {
	mu    sync.Mutex
	vals  []int64 // microseconds
	limit int
	max   int64
}

func newLatency(limit int) *latency { return &latency{limit: limit} }

func (l *latency) add(d time.Duration) {
	us := d.Microseconds()
	if us < 0 {
		us = 0
	}
	l.mu.Lock()
	if l.max < us {
		l.max = us
	}
	if len(l.vals) < l.limit {
		l.vals = append(l.vals, us)
	}
	l.mu.Unlock()
}

func (l *latency) snapshot() map[string]float64 {
	l.mu.Lock()
	vals := make([]int64, len(l.vals))
	copy(vals, l.vals)
	max := l.max
	l.mu.Unlock()

	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
	pick := func(p float64) float64 {
		if len(vals) == 0 {
			return 0
		}
		return float64(vals[int(float64(len(vals)-1)*p)]) / 1000.0 // -> ms
	}

	return map[string]float64{
		"samples": float64(len(vals)),
		"p50":     round2(pick(0.50)),
		"p90":     round2(pick(0.90)),
		"p99":     round2(pick(0.99)),
		"p999":    round2(pick(0.999)),
		"max":     round2(float64(max) / 1000.0),
	}
}

func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }

var (
	opened       atomic.Int64
	failed       atomic.Int64
	live         atomic.Int64
	handshakeOK  atomic.Int64
	loginOK      atomic.Int64
	subscribed   atomic.Int64
	publishesOut atomic.Int64
	publishesIn  atomic.Int64
	pings        atomic.Int64
	disconnects  atomic.Int64
	bytesIn      atomic.Int64

	errMu   sync.Mutex
	errSeen = map[string]int{}
)

func recordError(err error) {
	if err == nil {
		return
	}
	msg := err.Error()
	if len(msg) > 90 {
		msg = msg[:90]
	}
	errMu.Lock()
	errSeen[msg]++
	errMu.Unlock()
}

func run() {
	fmt.Printf("target      %s%s%s\n", scheme(), cfg.host, cfg.path)
	fmt.Printf("connections %d, ramp %d/s, hold %s, mode %s\n", cfg.connections, cfg.ramp, cfg.duration, cfg.mode)
	fmt.Printf("channels    %d, publishers %d every %s\n", channelCount(), cfg.publishers, cfg.pubInterval)
	if len(sourceIPs) > 0 {
		fmt.Printf("source ips  %d (%s)\n", len(sourceIPs), cfg.localIPs)
	}
	fmt.Println()

	if channelCount() < cfg.connections/10 && cfg.mode == "visitor" {
		fmt.Printf("note  %d connections share only %d channel(s). Every subscribe publishes a\n"+
			"      vi_online event to that channel, so with visitors this is O(N^2) - a worst\n"+
			"      case fan out test, not a realistic layout. Use -chat-ids 0 for capacity.\n\n",
			cfg.connections, channelCount())
	}

	handshakeL := newLatency(200000)
	loginL := newLatency(200000)
	subscribeL := newLatency(200000)
	fanoutL := newLatency(200000)

	stopProgress := make(chan struct{})
	if cfg.progress > 0 {
		go progressLoop(stopProgress)
	}

	// Peak tracking: the interesting numbers happen while the test runs, not after it.
	var peakLive, peakGoroutines atomic.Int64
	stopSampler := make(chan struct{})
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopSampler:
				return
			case <-ticker.C:
				recordPeak(&peakLive, live.Load())
				recordPeak(&peakGoroutines, int64(runtime.NumGoroutine()))
			}
		}
	}()

	clients := make([]*client, 0, cfg.connections)
	interval := time.Duration(0)
	if cfg.ramp > 0 {
		interval = time.Second / time.Duration(cfg.ramp)
	}

	rampStart := time.Now()
	for i := 0; i < cfg.connections; i++ {
		c := &client{
			id:     i,
			chatID: chatIDFor(i),
			isPub:  i < cfg.publishers,
			metrics: metrics{
				handshake: handshakeL,
				login:     loginL,
				subscribe: subscribeL,
				fanout:    fanoutL,
			},
		}
		clients = append(clients, c)

		go c.run()

		if interval > 0 && i < cfg.connections-1 {
			time.Sleep(interval)
		}
	}
	rampTook := time.Since(rampStart)

	interrupted := make(chan os.Signal, 1)
	signal.Notify(interrupted, os.Interrupt, syscall.SIGTERM)

	select {
	case <-time.After(cfg.duration):
	case <-interrupted:
		fmt.Println("\ninterrupted - closing connections")
	}
	close(stopProgress)
	close(stopSampler)

	var wg sync.WaitGroup
	for _, c := range clients {
		wg.Add(1)
		go func(c *client) { defer wg.Done(); c.close() }(c)
	}
	waited := make(chan struct{})
	go func() { wg.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
	}

	report(rampTook, peakLive.Load(), peakGoroutines.Load(), handshakeL, loginL, subscribeL, fanoutL)
}

// recordPeak keeps the highest value seen (the value read at the end of a run is 0 once
// every connection has been closed).
func recordPeak(target *atomic.Int64, value int64) {
	for {
		current := target.Load()
		if value <= current || target.CompareAndSwap(current, value) {
			return
		}
	}
}

func scheme() string {
	if cfg.secure {
		return "wss://"
	}
	return "ws://"
}

func channelCount() int {
	if cfg.chatIDs <= 0 {
		return cfg.connections
	}
	return cfg.chatIDs
}

func chatIDFor(i int) int {
	if cfg.chatIDs <= 0 {
		return 900000 + i // one private channel per connection
	}
	return 900000 + (i % cfg.chatIDs)
}

func progressLoop(stop chan struct{}) {
	ticker := time.NewTicker(cfg.progress)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			fmt.Printf("  ... live=%d opened=%d failed=%d subs=%d in=%d out=%d\n",
				live.Load(), opened.Load(), failed.Load(), subscribed.Load(),
				publishesIn.Load(), publishesOut.Load())
		}
	}
}

func report(rampTook time.Duration, peakLive, peakGoroutines int64, handshake, login, subscribe, fanout *latency) {
	errMu.Lock()
	errors := make(map[string]int, len(errSeen))
	for k, v := range errSeen {
		errors[k] = v
	}
	errMu.Unlock()

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	rate := float64(0)
	if rampTook > 0 {
		rate = float64(opened.Load()) / rampTook.Seconds()
	}
	goroutines := runtime.NumGoroutine()
	heapMB := round2(float64(mem.HeapAlloc) / 1024 / 1024)

	handshakeS := handshake.snapshot()
	loginS := login.snapshot()
	subscribeS := subscribe.snapshot()
	fanoutS := fanout.snapshot()

	result := map[string]any{
		"target":                  scheme() + cfg.host + cfg.path,
		"mode":                    cfg.mode,
		"connections_wanted":      cfg.connections,
		"connections_opened":      opened.Load(),
		"connections_failed":      failed.Load(),
		"connections_peak":        peakLive,
		"ramp_seconds":            round2(rampTook.Seconds()),
		"ramp_rate_per_s":         round2(rate),
		"authenticated":           loginOK.Load(),
		"subscribed":              subscribed.Load(),
		"publishes_sent":          publishesOut.Load(),
		"publishes_received":      publishesIn.Load(),
		"pongs_sent":              pings.Load(),
		"bytes_received":          bytesIn.Load(),
		"unexpected_close":        disconnects.Load(),
		"handshake_ms":            handshakeS,
		"login_ms":                loginS,
		"subscribe_ms":            subscribeS,
		"fanout_ms":               fanoutS,
		"errors":                  errors,
		"loadgen_goroutines":      goroutines,
		"loadgen_goroutines_peak": peakGoroutines,
		"loadgen_heap_mb":         heapMB,
	}

	if cfg.jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(result)
		return
	}

	fmt.Println("\n--- results ---")
	fmt.Printf("connections   opened %d/%d (%d failed) in %.1fs => %.0f conn/s\n",
		opened.Load(), cfg.connections, failed.Load(), rampTook.Seconds(), rate)
	fmt.Printf("state         authenticated %d, subscribed %d, peak live %d, still live %d\n",
		loginOK.Load(), subscribed.Load(), peakLive, live.Load())
	fmt.Printf("traffic       publishes out %d, in %d, pongs %d, received %.2f MB\n",
		publishesOut.Load(), publishesIn.Load(), pings.Load(), float64(bytesIn.Load())/1024/1024)
	fmt.Printf("unexpected    %d connection(s) closed by the server\n", disconnects.Load())

	printLatency("handshake", handshakeS)
	printLatency("login", loginS)
	printLatency("subscribe", subscribeS)
	printLatency("fanout", fanoutS)

	if len(errors) > 0 {
		fmt.Println("errors")
		for msg, n := range errors {
			fmt.Printf("  %6d  %s\n", n, msg)
		}
	}

	fmt.Printf("loadgen       %d goroutines (peak %d), %.1f MB heap\n", goroutines, peakGoroutines, heapMB)
}

func printLatency(name string, s map[string]float64) {
	if s["samples"] == 0 {
		fmt.Printf("%-13s (no samples)\n", name)
		return
	}
	fmt.Printf("%-13s p50 %7.2f  p90 %7.2f  p99 %7.2f  max %8.2f  ms  (%d samples)\n",
		name, s["p50"], s["p90"], s["p99"], s["max"], int(s["samples"]))
}

// --- one connection ----------------------------------------------------------

type metrics struct {
	handshake *latency
	login     *latency
	subscribe *latency
	fanout    *latency
}

type client struct {
	id      int
	chatID  int
	isPub   bool
	metrics metrics

	ws  *wsClient
	cid atomic.Int64
}

func (c *client) channel() string { return "chat_" + strconv.Itoa(c.chatID) }

func (c *client) run() {
	ws, err := dialWS(cfg.host, cfg.path, cfg.secure, cfg.timeout)
	if err != nil {
		failed.Add(1)
		recordError(fmt.Errorf("dial: %w", err))
		return
	}
	c.ws = ws
	opened.Add(1)
	live.Add(1)

	// #handshake - measures websocket + protocol handshake.
	start := time.Now()
	if _, err := c.rpc("#handshake", map[string]any{}); err != nil {
		failed.Add(1)
		live.Add(-1)
		recordError(fmt.Errorf("handshake: %w", err))
		ws.close()
		return
	}
	c.metrics.handshake.add(time.Since(start))
	handshakeOK.Add(1)

	// login
	start = time.Now()
	if _, err := c.rpc("login", map[string]any{
		"hash":        c.token(),
		"chanelName":  c.channel(),
		"instance_id": 0,
	}); err != nil {
		live.Add(-1)
		recordError(fmt.Errorf("login: %w", err))
		ws.close()
		return
	}
	c.metrics.login.add(time.Since(start))
	loginOK.Add(1)

	// #subscribe
	start = time.Now()
	if _, err := c.rpc("#subscribe", map[string]any{"channel": c.channel()}); err != nil {
		live.Add(-1)
		recordError(fmt.Errorf("subscribe: %w", err))
		ws.close()
		return
	}
	c.metrics.subscribe.add(time.Since(start))
	subscribed.Add(1)

	go c.steadyState()
}

// steadyState answers server pings and records incoming publishes until the socket ends.
func (c *client) steadyState() {
	defer live.Add(-1)

	if c.isPub {
		go c.publisher()
	}

	for {
		_, payload, err := c.ws.readMessage(cfg.timeout + 30*time.Second)
		if err != nil {
			if !c.ws.isClosed() {
				disconnects.Add(1)
				recordError(fmt.Errorf("read: %w", err))
			}
			return
		}
		bytesIn.Add(int64(len(payload)))

		switch string(payload) {
		case "#1":
			if err := c.ws.writeText([]byte("#2")); err == nil {
				pings.Add(1)
			}
			continue
		case "#2":
			continue
		}

		var packet struct {
			Event string `json:"event"`
			Data  struct {
				Channel string `json:"channel"`
				Data    struct {
					Op string `json:"op"`
					T  int64  `json:"t"`
				} `json:"data"`
			} `json:"data"`
		}
		if err := json.Unmarshal(payload, &packet); err != nil || packet.Event != "#publish" {
			continue
		}

		publishesIn.Add(1)
		if packet.Data.Data.T > 0 {
			c.metrics.fanout.add(time.Duration(time.Now().UnixMilli()-packet.Data.Data.T) * time.Millisecond)
		}
	}
}

func (c *client) publisher() {
	ticker := time.NewTicker(cfg.pubInterval)
	defer ticker.Stop()

	for range ticker.C {
		if c.ws.isClosed() {
			return
		}
		encoded, err := json.Marshal(map[string]any{
			"event": "#publish",
			"data": map[string]any{
				"channel": c.channel(),
				"data":    map[string]any{"op": "lt", "t": time.Now().UnixMilli(), "from": c.id},
			},
			"cid": c.nextCid(),
		})
		if err != nil {
			return
		}
		if err := c.ws.writeText(encoded); err != nil {
			recordError(fmt.Errorf("publish: %w", err))
			return
		}
		publishesOut.Add(1)
	}
}

// token builds the same hash lhpredis.php/tokenvisitor.php does.
func (c *client) token() string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	if cfg.mode == "operator" {
		sum := sha1.Sum([]byte(ts + "Operator" + cfg.secret))
		return hex.EncodeToString(sum[:]) + "." + ts
	}
	sum := sha1.Sum([]byte(ts + "Visitor" + cfg.secret + "_" + strconv.Itoa(c.chatID)))
	return hex.EncodeToString(sum[:]) + "." + ts
}

func (c *client) nextCid() int64 { return c.cid.Add(1) }

func (c *client) close() {
	if c.ws != nil {
		c.ws.close()
	}
}

// rpc sends one event and waits for the matching rid, answering server pings on the way.
func (c *client) rpc(event string, data any) (json.RawMessage, error) {
	cid := c.nextCid()
	encoded, err := json.Marshal(map[string]any{"event": event, "data": data, "cid": cid})
	if err != nil {
		return nil, err
	}
	if err := c.ws.writeText(encoded); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(cfg.timeout)
	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout waiting for the %s response", event)
		}
		_, payload, err := c.ws.readMessage(cfg.timeout)
		if err != nil {
			return nil, err
		}
		bytesIn.Add(int64(len(payload)))

		if string(payload) == "#1" {
			if err := c.ws.writeText([]byte("#2")); err == nil {
				pings.Add(1)
			}
			continue
		}

		var response struct {
			Rid   *int64          `json:"rid"`
			Data  json.RawMessage `json:"data"`
			Error *struct {
				Name    string `json:"name"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(payload, &response); err != nil {
			continue
		}
		if response.Rid == nil || *response.Rid != cid {
			continue // e.g. a #publish delivered before the ack
		}
		if response.Error != nil {
			return nil, fmt.Errorf("%s (%s)", response.Error.Message, response.Error.Name)
		}
		return response.Data, nil
	}
}

// --- minimal websocket client ------------------------------------------------

type wsClient struct {
	conn net.Conn
	br   *bufio.Reader

	writeMu sync.Mutex
	closed  atomic.Bool
}

func dialWS(host, path string, secure bool, timeout time.Duration) (*wsClient, error) {
	dialer := &net.Dialer{Timeout: timeout}
	if len(sourceIPs) > 0 {
		index := int(sourceIPCounter.Add(1)) % len(sourceIPs)
		dialer.LocalAddr = &net.TCPAddr{IP: sourceIPs[index]}
	}

	var conn net.Conn
	var err error
	if secure {
		serverName := host
		if idx := strings.LastIndex(host, ":"); idx > 0 {
			serverName = host[:idx]
		}
		conn, err = tls.DialWithDialer(dialer, "tcp", host, &tls.Config{ServerName: serverName})
	} else {
		conn, err = dialer.Dial("tcp", host)
	}
	if err != nil {
		return nil, err
	}
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetNoDelay(true)
	}

	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		conn.Close()
		return nil, err
	}

	request := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + base64.StdEncoding.EncodeToString(keyBytes) + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"

	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write([]byte(request)); err != nil {
		conn.Close()
		return nil, err
	}

	reader := bufio.NewReader(conn)
	header := textproto.NewReader(reader)
	statusLine, err := header.ReadLine()
	if err != nil {
		conn.Close()
		return nil, err
	}
	if !strings.Contains(statusLine, "101") {
		conn.Close()
		return nil, fmt.Errorf("upgrade refused: %s", statusLine)
	}
	if _, err := header.ReadMIMEHeader(); err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})

	return &wsClient{conn: conn, br: reader}, nil
}

func (c *wsClient) isClosed() bool { return c.closed.Load() }

func (c *wsClient) writeText(payload []byte) error {
	return c.writeFrame(0x1, payload)
}

func (c *wsClient) writeFrame(opcode byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.closed.Load() {
		return fmt.Errorf("connection closed")
	}
	return c.writeFrameLocked(opcode, payload)
}

// writeFrameLocked expects writeMu to be held.
func (c *wsClient) writeFrameLocked(opcode byte, payload []byte) error {
	frame := make([]byte, 0, len(payload)+14)
	frame = append(frame, 0x80|opcode)

	n := len(payload)
	switch {
	case n < 126:
		frame = append(frame, 0x80|byte(n))
	case n <= 0xFFFF:
		frame = append(frame, 0x80|126, byte(n>>8), byte(n))
	default:
		frame = append(frame, 0x80|127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		frame = append(frame, ext[:]...)
	}

	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	frame = append(frame, mask[:]...)

	offset := len(frame)
	frame = append(frame, payload...)
	for i := 0; i < n; i++ {
		frame[offset+i] ^= mask[i%4]
	}

	_ = c.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	_, err := c.conn.Write(frame)
	return err
}

func (c *wsClient) readMessage(timeout time.Duration) (byte, []byte, error) {
	_ = c.conn.SetReadDeadline(time.Now().Add(timeout))

	for {
		var header [2]byte
		if _, err := readFull(c.br, header[:]); err != nil {
			return 0, nil, err
		}

		fin := header[0]&0x80 != 0
		opcode := header[0] & 0x0f
		masked := header[1]&0x80 != 0
		length := int64(header[1] & 0x7f)

		switch length {
		case 126:
			var ext [2]byte
			if _, err := readFull(c.br, ext[:]); err != nil {
				return 0, nil, err
			}
			length = int64(binary.BigEndian.Uint16(ext[:]))
		case 127:
			var ext [8]byte
			if _, err := readFull(c.br, ext[:]); err != nil {
				return 0, nil, err
			}
			length = int64(binary.BigEndian.Uint64(ext[:]))
		}
		if length > 8*1024*1024 {
			return 0, nil, fmt.Errorf("frame too large: %d", length)
		}

		var mask [4]byte
		if masked {
			if _, err := readFull(c.br, mask[:]); err != nil {
				return 0, nil, err
			}
		}

		payload := make([]byte, length)
		if _, err := readFull(c.br, payload); err != nil {
			return 0, nil, err
		}
		if masked {
			for i := range payload {
				payload[i] ^= mask[i%4]
			}
		}

		switch opcode {
		case 0x8:
			return opcode, nil, fmt.Errorf("closed by server")
		case 0x9: // ping -> pong
			if err := c.writeFrame(0xA, payload); err != nil {
				return 0, nil, err
			}
			continue
		case 0xA: // pong
			continue
		}

		if fin {
			return opcode, payload, nil
		}
		return 0, nil, fmt.Errorf("unexpected fragmented frame")
	}
}

func (c *wsClient) close() {
	c.writeMu.Lock()
	if !c.closed.Swap(true) {
		_ = c.writeFrameLocked(0x8, []byte{0x03, 0xE8}) // 1000 normal closure
	}
	c.writeMu.Unlock()
	_ = c.conn.Close()
}

func readFull(r *bufio.Reader, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// raiseFileLimit lifts the soft limit so the process can really open -connections sockets.
func raiseFileLimit(want int) {
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
		return
	}
	if uint64(want) <= limit.Cur {
		return
	}

	raised := limit
	raised.Cur = limit.Max
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &raised); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not raise the open file limit from %d: %v\n", limit.Cur, err)
		return
	}
	if uint64(want) > raised.Cur {
		fmt.Fprintf(os.Stderr, "warning: open file limit is %d but %d connections are requested\n", raised.Cur, want)
	}
}
