package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// --- helpers -----------------------------------------------------------------

// newTestSocket returns a Socket that is usable without a connection: the
// outbound queue and done channel are what enqueueText touches.
func newTestSocket(buffer int) *Socket {
	return &Socket{
		id:       "test-socket",
		outbound: make(chan []byte, buffer),
		done:     make(chan struct{}),
		subs:     make(map[string]struct{}),
	}
}

// --- publish path ------------------------------------------------------------

// TestPublishLocalEncodesOnce is the regression test for the fan out path: one
// encode per publish, the same bytes for every subscriber, and nothing at all
// for a socket that is already closed.
func TestPublishLocalEncodesOnce(t *testing.T) {
	hub := newHub()

	first := newTestSocket(4)
	second := newTestSocket(4)
	closed := newTestSocket(4)
	closed.closed.Store(true)

	hub.Add(first, "chat_1")
	hub.Add(second, "chat_1")
	hub.Add(closed, "chat_1")

	if got := hub.Count("chat_1"); got != 3 {
		t.Fatalf("Count = %d, want 3", got)
	}

	hub.PublishLocal("chat_1", json.RawMessage(`{"op":"cmsg","id":7}`))

	want := `{"event":"#publish","data":{"channel":"chat_1","data":{"op":"cmsg","id":7}}}`

	var packets [][]byte
	for name, s := range map[string]*Socket{"first": first, "second": second} {
		select {
		case got := <-s.outbound:
			if string(got) != want {
				t.Fatalf("%s: packet = %s, want %s", name, got, want)
			}
			packets = append(packets, got)
		default:
			t.Fatalf("%s: nothing was delivered", name)
		}
	}

	// Both subscribers must share one encoded buffer - that is the whole point
	// of encoding in PublishLocal instead of per socket.
	if len(packets) == 2 && &packets[0][0] != &packets[1][0] {
		t.Error("subscribers received different buffers, the packet is encoded more than once")
	}

	select {
	case got := <-closed.outbound:
		t.Fatalf("closed socket received %s", got)
	default:
	}

	// A publish to a channel nobody listens on must not panic or block.
	hub.PublishLocal("chat_nobody", json.RawMessage(`{}`))
}

func TestPublishLocalNullData(t *testing.T) {
	hub := newHub()
	socket := newTestSocket(4)
	hub.Add(socket, "c")

	hub.PublishLocal("c", nil)

	select {
	case got := <-socket.outbound:
		if want := `{"event":"#publish","data":{"channel":"c","data":null}}`; string(got) != want {
			t.Fatalf("packet = %s, want %s", got, want)
		}
	default:
		t.Fatal("nothing was delivered for a nil payload")
	}
}

func TestEncodePublishRejectsInvalidPayload(t *testing.T) {
	if packet := encodePublish("c", json.RawMessage(`{invalid`)); packet != nil {
		t.Fatalf("encodePublish accepted invalid JSON: %s", packet)
	}
}

func TestHubSubscribeHooks(t *testing.T) {
	hub := newHub()

	var subscribed, unsubscribed []string
	hub.onFirstSubscribe = func(channel string) { subscribed = append(subscribed, channel) }
	hub.onLastUnsubscribe = func(channel string) { unsubscribed = append(unsubscribed, channel) }

	first := newTestSocket(1)
	second := newTestSocket(1)

	if !hub.Add(first, "c") {
		t.Error("first Add should report the first subscriber")
	}
	if hub.Add(second, "c") {
		t.Error("second Add should not report the first subscriber")
	}
	if hub.Remove(first, "c") {
		t.Error("Remove of one of two subscribers should not report the last one")
	}
	if !hub.Remove(second, "c") {
		t.Error("Remove of the last subscriber should report it")
	}
	// Removing twice must not run the hook again.
	if hub.Remove(second, "c") {
		t.Error("removing an unknown subscription reported the last subscriber")
	}

	if len(subscribed) != 1 || subscribed[0] != "c" {
		t.Errorf("onFirstSubscribe calls = %v", subscribed)
	}
	if len(unsubscribed) != 1 || unsubscribed[0] != "c" {
		t.Errorf("onLastUnsubscribe calls = %v", unsubscribed)
	}
}

// --- websocket framing -------------------------------------------------------

// expectedFrameHeader returns the unmasked frame header writeFrame has to
// produce for a payload of the given size.
func expectedFrameHeader(size int) []byte {
	switch {
	case size < 126:
		return []byte{0x80 | opText, byte(size)}
	case size <= 0xFFFF:
		header := []byte{0x80 | opText, 126}
		return binary.BigEndian.AppendUint16(header, uint16(size))
	default:
		header := []byte{0x80 | opText, 127}
		return binary.BigEndian.AppendUint64(header, uint64(size))
	}
}

// TestWriteFrameLengthEncodings covers all three payload length encodings,
// which is where a hand written framer usually breaks. net.Pipe is not a
// *net.TCPConn, so this also exercises net.Buffers' fallback path.
func TestWriteFrameLengthEncodings(t *testing.T) {
	for _, size := range []int{0, 2, 125, 126, 65535, 65536} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			server, client := net.Pipe()
			defer client.Close()

			conn := &wsConn{conn: server}
			payload := bytes.Repeat([]byte("a"), size)
			header := expectedFrameHeader(size)

			read := make(chan []byte, 1)
			go func() {
				buf := make([]byte, len(header)+size)
				if _, err := io.ReadFull(client, buf); err != nil {
					read <- nil
					return
				}
				read <- buf
			}()

			if err := conn.writeFrame(opText, payload); err != nil {
				t.Fatalf("writeFrame: %v", err)
			}
			_ = server.Close()

			frame := <-read
			if frame == nil {
				t.Fatal("the frame was not written")
			}
			if !bytes.Equal(frame[:len(header)], header) {
				t.Fatalf("header = % x, want % x", frame[:len(header)], header)
			}
			if !bytes.Equal(frame[len(header):], payload) {
				t.Fatal("payload was not delivered intact")
			}
		})
	}
}

func TestWriteFrameRefusesClosedConnection(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	defer server.Close()

	conn := &wsConn{conn: server}
	_ = conn.Close()

	if err := conn.writeFrame(opText, []byte("x")); err != errWSClosed {
		t.Fatalf("writeFrame on a closed connection = %v, want %v", err, errWSClosed)
	}
}

// --- authentication ----------------------------------------------------------

func TestValidateLoginToken(t *testing.T) {
	const secret = "s3cret"
	now := time.Now().Unix()
	ts := strconv.FormatInt(now, 10)

	visitor := sha1Hex(ts+"Visitor"+secret) + "." + ts
	chatVisitor := sha1Hex(ts+"Visitor"+secret+"_55") + "." + ts
	operator := sha1Hex(ts+"Operator"+secret) + "." + ts

	cases := []struct {
		name       string
		hash       string
		chanelName string
		visitor    bool
		chatToken  bool
		ok         bool
	}{
		{"site visitor", visitor, "uo_123", true, false, true},
		{"chat visitor", chatVisitor, "chat_55", true, true, true},
		// Automated hosting prefixes the instance id; the chat id stays the
		// last "_" separated part, so the very same hash has to validate.
		{"chat visitor with instance id", chatVisitor, "chat_3_55", true, true, true},
		{"operator", operator, "admin", false, false, true},
		{"no separator", "abcdef", "chat_55", false, false, false},
		{"bad hash", sha1Hex("nope") + "." + ts, "chat_55", false, false, false},
		{"tampered timestamp", chatVisitor, "chat_56", false, false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			visitor, chatToken, ok := validateLoginToken(tc.hash, tc.chanelName, secret, now)
			if ok != tc.ok || visitor != tc.visitor || chatToken != tc.chatToken {
				t.Fatalf("= (%v, %v, %v), want (%v, %v, %v)",
					visitor, chatToken, ok, tc.visitor, tc.chatToken, tc.ok)
			}
		})
	}

	t.Run("expired", func(t *testing.T) {
		old := strconv.FormatInt(now-60*60, 10)
		hash := sha1Hex(old+"Visitor"+secret) + "." + old
		if _, _, ok := validateLoginToken(hash, "uo_1", secret, now); ok {
			t.Fatal("a token older than one hour was accepted")
		}
	})
}

func TestSignAndVerifyToken(t *testing.T) {
	key := []byte("signing-key")
	token := &authToken{
		Token:       "abc",
		Exp:         time.Now().Unix() + 60,
		ChanelName:  "chat_7",
		InstanceID:  3,
		IsChatToken: true,
		IsVisitor:   true,
	}

	signed, err := signToken(key, token)
	if err != nil {
		t.Fatalf("signToken: %v", err)
	}
	if strings.Count(signed, ".") != 2 {
		t.Fatalf("signed token is not a JWT: %s", signed)
	}

	verified, err := verifyToken(key, signed)
	if err != nil {
		t.Fatalf("verifyToken: %v", err)
	}
	if verified.ChanelName != token.ChanelName || !verified.IsVisitor ||
		!verified.IsChatToken || verified.InstanceID != token.InstanceID {
		t.Fatalf("verified token = %+v, want %+v", verified, token)
	}

	// Runtime only field must not survive the round trip.
	if verified.ChanelNameChat != "" {
		t.Error("ChanelNameChat was serialised into the token")
	}

	t.Run("wrong key", func(t *testing.T) {
		if _, err := verifyToken([]byte("other-key"), signed); err == nil {
			t.Fatal("a token signed with another key was accepted")
		}
	})

	t.Run("tampered payload", func(t *testing.T) {
		parts := strings.Split(signed, ".")
		parts[1] = parts[1] + "x"
		if _, err := verifyToken(key, strings.Join(parts, ".")); err == nil {
			t.Fatal("a tampered token was accepted")
		}
	})

	t.Run("expired", func(t *testing.T) {
		expired, err := signToken(key, &authToken{Exp: time.Now().Unix() - 1})
		if err != nil {
			t.Fatalf("signToken: %v", err)
		}
		if _, err := verifyToken(key, expired); err != errTokenExpired {
			t.Fatalf("verifyToken = %v, want %v", err, errTokenExpired)
		}
	})
}

func TestLastTokenPart(t *testing.T) {
	if got := lastTokenPart("chat_3_55"); got != "55" {
		t.Fatalf("lastTokenPart = %q, want %q", got, "55")
	}
}

// --- redis wire format -------------------------------------------------------

func TestEncodeAndSplitRedisPayload(t *testing.T) {
	cases := []struct {
		name     string
		instance string
		data     json.RawMessage
		want     string
		sender   string
		message  string
	}{
		{"object without instance", "", json.RawMessage(`{"op":"cmsg"}`), `/o:{"op":"cmsg"}`, "", `o:{"op":"cmsg"}`},
		{"object with instance", "node1", json.RawMessage(`{"op":"cmsg"}`), `node1/o:{"op":"cmsg"}`, "node1", `o:{"op":"cmsg"}`},
		{"scalar string", "", json.RawMessage(`"hello"`), `/s:hello`, "", "s:hello"},
		{"scalar number", "", json.RawMessage(`42`), `/s:42`, "", "s:42"},
		// sc-redis stripped the prefix with a regex that also matched the first
		// "/" inside the payload; these two must survive untouched.
		{"slash inside payload", "", json.RawMessage(`{"url":"a/b"}`), `/o:{"url":"a/b"}`, "", `o:{"url":"a/b"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := encodeForRedis(tc.instance, tc.data)
			if got != tc.want {
				t.Fatalf("encodeForRedis = %q, want %q", got, tc.want)
			}

			sender, message := splitRedisPayload(got)
			if sender != tc.sender || message != tc.message {
				t.Fatalf("splitRedisPayload = (%q, %q), want (%q, %q)", sender, message, tc.sender, tc.message)
			}
		})
	}

	t.Run("bare payload from lhpredis.php", func(t *testing.T) {
		sender, message := splitRedisPayload(`o:{"op":"lt"}`)
		if sender != "" || message != `o:{"op":"lt"}` {
			t.Fatalf("= (%q, %q)", sender, message)
		}
	})
}

func TestHandleMessageSkipsOwnInstance(t *testing.T) {
	var delivered []string
	bridge := &redisBridge{
		instanceID: "me",
		deliver: func(channel string, data json.RawMessage) {
			delivered = append(delivered, channel+"="+string(data))
		},
	}

	bridge.handleMessage("chat_1", `me/o:{"op":"own"}`)
	if len(delivered) != 0 {
		t.Fatalf("own message was delivered: %v", delivered)
	}

	bridge.handleMessage("chat_1", `other/o:{"op":"remote"}`)
	if len(delivered) != 1 || delivered[0] != `chat_1={"op":"remote"}` {
		t.Fatalf("remote message = %v", delivered)
	}

	// Plain string payloads become JSON strings.
	bridge.handleMessage("chat_1", `/s:hello`)
	if len(delivered) != 2 || delivered[1] != `chat_1="hello"` {
		t.Fatalf("scalar message = %v", delivered)
	}
}

// --- protocol helpers --------------------------------------------------------

func TestParseChannelPayload(t *testing.T) {
	channel, batch := parseChannelPayload(json.RawMessage(`"chat_1"`))
	if channel != "chat_1" || batch {
		t.Fatalf("string form = (%q, %v)", channel, batch)
	}

	channel, batch = parseChannelPayload(json.RawMessage(`{"channel":"chat_2","batch":true}`))
	if channel != "chat_2" || !batch {
		t.Fatalf("object form = (%q, %v)", channel, batch)
	}

	if channel, _ := parseChannelPayload(nil); channel != "" {
		t.Fatalf("nil payload = %q, want empty", channel)
	}
	if channel, _ := parseChannelPayload(json.RawMessage(`{"channel":5}`)); channel != "" {
		t.Fatalf("malformed payload = %q, want empty", channel)
	}
}

func TestTokenListContains(t *testing.T) {
	if !tokenListContains("keep-alive, Upgrade", "upgrade") {
		t.Error("comma separated token with different case was not found")
	}
	if tokenListContains("keep-alive", "upgrade") {
		t.Error("unrelated token was found")
	}
}

func requestWithOrigin(origin string) *http.Request {
	req, _ := http.NewRequest(http.MethodGet, "/socketcluster/", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	return req
}

func TestOriginAllowed(t *testing.T) {
	cases := []struct {
		origins string
		origin  string
		want    bool
	}{
		{"*:*", "https://example.com", true},
		{"example.com:443", "https://example.com", true},
		{"example.com:*", "https://example.com", true},
		{"*:443", "https://example.com", true},
		{"example.com:80", "http://example.com", true},
		{"example.com:443", "http://example.com", false},
		{"other.com:443", "https://example.com", false},
		{"example.com:443,other.com:443", "https://other.com", true},
		// No Origin header is treated as "*", so a restricted list refuses it.
		{"example.com:443", "", false},
		{"*:*", "", true},
	}

	for _, tc := range cases {
		if got := originAllowed(tc.origins, requestWithOrigin(tc.origin)); got != tc.want {
			t.Errorf("originAllowed(%q, %q) = %v, want %v", tc.origins, tc.origin, got, tc.want)
		}
	}
}

// --- configuration -----------------------------------------------------------

func TestSendBufferIsClamped(t *testing.T) {
	for _, value := range []string{"0", "-5"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("SOCKET_SEND_BUFFER", value)
			if got := loadConfig().SendBuffer; got != 1 {
				t.Fatalf("SendBuffer = %d, want 1", got)
			}
		})
	}

	t.Run("explicit value", func(t *testing.T) {
		t.Setenv("SOCKET_SEND_BUFFER", "64")
		if got := loadConfig().SendBuffer; got != 64 {
			t.Fatalf("SendBuffer = %d, want 64", got)
		}
	})
}

func TestSecurityOptionDefaults(t *testing.T) {
	t.Setenv("STRICT_CHANNEL_BINDING", "")
	t.Setenv("RESTRICT_CLIENT_PUBLISH", "")

	cfg := loadConfig()
	if !cfg.StrictChannelBinding {
		t.Error("STRICT_CHANNEL_BINDING must default to true")
	}
	if cfg.RestrictClientPublish {
		t.Error("RESTRICT_CLIENT_PUBLISH must default to false")
	}

	t.Setenv("STRICT_CHANNEL_BINDING", "false")
	t.Setenv("RESTRICT_CLIENT_PUBLISH", "true")

	cfg = loadConfig()
	if cfg.StrictChannelBinding {
		t.Error("STRICT_CHANNEL_BINDING=false was ignored")
	}
	if !cfg.RestrictClientPublish {
		t.Error("RESTRICT_CLIENT_PUBLISH=true was ignored")
	}
}

// --- logging -----------------------------------------------------------------

func resetRateLimitState() {
	logRateLimitMu.Lock()
	logRateLimitState = map[string]*logRateLimitEntry{}
	logRateLimitMu.Unlock()
}

func TestLogRateLimitedSuppressesBurst(t *testing.T) {
	previousOutput := log.Writer()
	previousLevel := currentLogLevel

	var buf bytes.Buffer
	log.SetOutput(&buf)
	currentLogLevel = levelWarn
	resetRateLimitState()

	defer func() {
		log.SetOutput(previousOutput)
		currentLogLevel = previousLevel
		resetRateLimitState()
	}()

	for i := 0; i < 5; i++ {
		logRateLimitedf("burst", "[test] event %d", i)
	}

	lines := strings.Count(buf.String(), "[test] event")
	if lines != 1 {
		t.Fatalf("logged %d lines for a burst of 5, want 1 (%q)", lines, buf.String())
	}

	// Age the entry so the cooldown has passed, then check the suppressed count
	// is reported once.
	logRateLimitMu.Lock()
	logRateLimitState["burst"].last = time.Now().Add(-2 * logRateLimitWindow)
	logRateLimitMu.Unlock()

	buf.Reset()
	logRateLimitedf("burst", "[test] event 9")

	output := buf.String()
	if !strings.Contains(output, "suppressed") {
		t.Errorf("the suppressed count was not reported: %q", output)
	}
	if !strings.Contains(output, "[test] event 9") {
		t.Errorf("the new event was not logged: %q", output)
	}
}

func TestLogRateLimitedRespectsLevel(t *testing.T) {
	previousOutput := log.Writer()
	previousLevel := currentLogLevel

	var buf bytes.Buffer
	log.SetOutput(&buf)
	currentLogLevel = levelError
	resetRateLimitState()

	defer func() {
		log.SetOutput(previousOutput)
		currentLogLevel = previousLevel
		resetRateLimitState()
	}()

	logRateLimitedf("level", "[test] quiet")

	if buf.Len() != 0 {
		t.Fatalf("LOG_LEVEL=error still logged: %q", buf.String())
	}
}
