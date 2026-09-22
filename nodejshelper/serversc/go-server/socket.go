package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// errObj is the dehydrated error format sc-errors used on the wire.
type errObj struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

type inEvent struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
	Cid   *int64          `json:"cid"`
}

type outEvent struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data,omitempty"`
	Cid   *int64          `json:"cid,omitempty"`
}

type outResponse struct {
	Rid   int64           `json:"rid"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error *errObj         `json:"error,omitempty"`
}

type outPublish struct {
	Channel string          `json:"channel"`
	Data    json.RawMessage `json:"data"`
}

// Socket is one SocketCluster client connection, the equivalent of the
// SCServerSocket instance that worker.js configured.
type Socket struct {
	id       string
	ws       *wsConn
	srv      *Server
	outbound chan []byte
	done     chan struct{}
	stopOnce sync.Once

	mu          sync.Mutex
	handshaken  bool
	authed      bool
	token       *authToken
	signedToken string
	subs        map[string]struct{}
	callID      int64

	closed     atomic.Bool
	lastActive atomic.Int64
}

func newSocketID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func newSocket(srv *Server, ws *wsConn) *Socket {
	now := time.Now().UnixNano()
	s := &Socket{
		id:       newSocketID(),
		ws:       ws,
		srv:      srv,
		outbound: make(chan []byte, srv.cfg.SendBuffer),
		done:     make(chan struct{}),
		subs:     make(map[string]struct{}),
	}
	s.lastActive.Store(now)
	return s
}

// run is the connection life cycle: handshake timeout, ping/pong keep alive,
// message loop and finally the disconnect bookkeeping.
func (s *Socket) run() {
	go s.writeLoop()

	handshakeTimer := time.AfterFunc(s.srv.cfg.HandshakeTimeout, func() {
		if !s.isHandshaken() {
			s.closeWith(4005, "Did not receive #handshake from client before timeout")
		}
	})
	defer handshakeTimer.Stop()
	defer s.cleanup()
	defer s.srv.unregisterSocket(s)

	go s.pingLoop()

	for {
		// SocketCluster's pingTimeout is the amount of inactivity after which the
		// connection is considered dead.
		s.ws.SetReadDeadline(time.Now().Add(s.srv.cfg.PingTimeout + 5*time.Second))

		_, payload, err := s.ws.ReadMessage()
		if err != nil {
			s.reportReadError(err)
			return
		}

		s.lastActive.Store(time.Now().UnixNano())

		switch message := string(payload); message {
		case "#2":
			// pong - nothing else to do, the timestamp above is the ack
			continue
		case "#1":
			// Very old clients ping as well; answer like SCServerSocket did.
			s.enqueueText([]byte("#2"))
			continue
		}

		s.handleMessage(payload)
	}
}

func (s *Socket) pingLoop() {
	ticker := time.NewTicker(s.srv.cfg.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			// The socket is gone. Returning here instead of waiting for the next
			// tick keeps a goroutine (and the whole Socket, through the closure)
			// alive for up to PingInterval after every disconnect - at high churn
			// that piled up thousands of goroutines.
			return
		case <-ticker.C:
		}

		if time.Since(time.Unix(0, s.lastActive.Load())) > s.srv.cfg.PingTimeout {
			s.closeWith(4000, "Pong timeout")
			return
		}
		s.enqueueText([]byte("#1"))
	}
}

func (s *Socket) writeLoop() {
	for {
		select {
		case <-s.done:
			return
		case payload := <-s.outbound:
			if err := s.ws.WriteText(payload); err != nil {
				s.closed.Store(true)
				_ = s.ws.Close()
				return
			}
		}
	}
}

func (s *Socket) handleMessage(payload []byte) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return
	}

	// SocketCluster supports batches: a single frame carrying an array of events.
	if trimmed[0] == '[' {
		var batch []inEvent
		if err := json.Unmarshal(trimmed, &batch); err != nil {
			logWarnf("[socket %s] invalid batch message: %v", s.id, err)
			return
		}
		for i := range batch {
			s.handleEvent(&batch[i])
		}
		return
	}

	var event inEvent
	if err := json.Unmarshal(trimmed, &event); err != nil {
		logWarnf("[socket %s] invalid message: %v", s.id, err)
		return
	}
	s.handleEvent(&event)
}

func (s *Socket) handleEvent(ev *inEvent) {
	switch ev.Event {
	case "#handshake":
		s.handleHandshake(ev)
	case "#subscribe":
		s.handleSubscribe(ev)
	case "#unsubscribe":
		s.handleUnsubscribe(ev)
	case "#publish":
		s.handlePublish(ev)
	case "#authenticate":
		s.handleAuthenticate(ev)
	case "#removeAuthToken":
		s.setUnauthenticated()
		if ev.Cid != nil {
			s.respond(*ev.Cid, nil)
		}
	case "#disconnect":
		s.closeWith(1000, "")
	case "login":
		s.handleLogin(ev)
	default:
		// SCServer used to emit the event and let listeners answer; LHC only
		// registers 'login'. Answering with an error keeps client callbacks
		// from hanging until their ackTimeout.
		if ev.Cid != nil {
			s.respondError(*ev.Cid, "UnknownEventError", "No handler for event "+ev.Event)
		}
	}
}

// handleHandshake mirrors SCServer's `#handshake` handling: an optional auth
// token is verified and the status object is sent back as the response.
func (s *Socket) handleHandshake(ev *inEvent) {
	var request struct {
		AuthToken string `json:"authToken"`
	}
	if len(ev.Data) > 0 {
		_ = json.Unmarshal(ev.Data, &request)
	}

	var authError *errObj

	if request.AuthToken != "" {
		token, err := verifyToken(s.srv.cfg.AuthKey, request.AuthToken)
		if err != nil {
			authError = &errObj{Name: tokenErrorName(err), Message: err.Error()}
			s.setUnauthenticated()
		} else {
			s.setAuthState(token, request.AuthToken)
		}
	} else {
		s.setUnauthenticated()
	}

	s.setHandshaken()

	status := map[string]any{
		"id":              s.id,
		"pingTimeout":     int(s.srv.cfg.PingTimeout / time.Millisecond),
		"isAuthenticated": s.isAuthenticated(),
	}
	if authError != nil {
		status["authError"] = authError
	}

	if ev.Cid != nil {
		s.respond(*ev.Cid, status)
	}
}

// handleLogin is a port of the `socket.on('login', ...)` handler.
func (s *Socket) handleLogin(ev *inEvent) {
	var request struct {
		Hash       json.RawMessage `json:"hash"`
		ChanelName string          `json:"chanelName"`
		InstanceID *int            `json:"instance_id"`
	}

	if err := json.Unmarshal(ev.Data, &request); err != nil {
		s.loginFailed(ev)
		return
	}

	hash := ""
	instanceID := 0
	if request.InstanceID != nil {
		instanceID = *request.InstanceID
	}

	if len(request.Hash) > 0 {
		trimmed := bytes.TrimSpace(request.Hash)
		switch trimmed[0] {
		case '"':
			if err := json.Unmarshal(trimmed, &hash); err != nil {
				s.loginFailed(ev)
				return
			}
		case '{':
			// Newer clients send {hash: {hash: '...', instance_id: N}}
			var nested struct {
				Hash       string `json:"hash"`
				InstanceID *int   `json:"instance_id"`
			}
			if err := json.Unmarshal(trimmed, &nested); err != nil {
				s.loginFailed(ev)
				return
			}
			hash = nested.Hash
			instanceID = 0
			if nested.InstanceID != nil {
				instanceID = *nested.InstanceID
			}
		}
	}

	// worker.js: token.chanelName.indexOf('chat_') would throw for a missing
	// chanelName - here it is treated as a failed login.
	isVisitor, isChatToken, ok := validateLoginToken(hash, request.ChanelName, s.srv.cfg.SecretHash, time.Now().Unix())
	if !ok {
		s.loginFailed(ev)
		return
	}

	// `respond()` is called before setAuthToken in worker.js
	if ev.Cid != nil {
		s.respond(*ev.Cid, nil)
	}

	now := time.Now().Unix()
	token := &authToken{
		Token:       hash,
		Exp:         now + 120*60,
		ChanelName:  request.ChanelName,
		InstanceID:  instanceID,
		IsChatToken: isChatToken,
		IsVisitor:   isVisitor,
	}
	if !isVisitor {
		token.Exp = now + 12*60*60 // operators got 12 hours
	}

	signedToken, err := signToken(s.srv.cfg.AuthKey, token)
	if err != nil {
		logErrorf("[socket %s] failed to sign auth token: %v", s.id, err)
		return
	}

	s.setAuthState(token, signedToken)

	// setAuthToken emits '#setAuthToken' with {token: signedToken}. SCServerSocket
	// sent it as an invoke (it passed the response callback to emit), so it carries
	// a cid and the browser answers with a matching rid. The browser client stores
	// the token there and replays it in the next `#handshake`.
	s.sendEventWithCid("#setAuthToken", map[string]string{"token": signedToken}, s.nextCallID())
}

func (s *Socket) loginFailed(ev *inEvent) {
	if ev.Cid != nil {
		s.respondError(*ev.Cid, "LoginFailedError", "Login failed")
	}
}

// handleAuthenticate answers the `#authenticate` event (not used by the LHC
// frontend, but part of the protocol).
func (s *Socket) handleAuthenticate(ev *inEvent) {
	var signed string
	if len(ev.Data) > 0 {
		_ = json.Unmarshal(ev.Data, &signed)
	}

	var authError *errObj
	if signed != "" {
		token, err := verifyToken(s.srv.cfg.AuthKey, signed)
		if err != nil {
			authError = &errObj{Name: tokenErrorName(err), Message: err.Error()}
			s.setUnauthenticated()
		} else {
			s.setAuthState(token, signed)
		}
	}

	status := map[string]any{"isAuthenticated": s.isAuthenticated()}
	if authError != nil {
		status["authError"] = authError
	}

	if ev.Cid != nil {
		s.respond(*ev.Cid, status)
	}
}

// handleSubscribe ports the MIDDLEWARE_SUBSCRIBE callback of worker.js.
func (s *Socket) handleSubscribe(ev *inEvent) {
	channel, batch := parseChannelPayload(ev.Data)

	if channel == "" {
		if ev.Cid != nil {
			s.respondError(*ev.Cid, "InvalidActionError", "Socket provided a malformated channel payload")
		}
		return
	}

	if !s.isHandshaken() {
		if ev.Cid != nil {
			s.respondError(*ev.Cid, "InvalidActionError", "Cannot subscribe socket to a channel before it has completed the handshake")
		}
		return
	}

	token := s.authToken()
	if token == nil {
		s.subscribeFailed(ev, channel)
		return
	}

	isChatChannel := strings.Contains(channel, "chat_") && token.IsVisitor

	if isChatChannel {
		if !token.IsChatToken {
			s.subscribeFailed(ev, channel)
			return
		}

		// A visitor token belongs to exactly one chat: tokenvisitor.php folds the
		// chat id into the hash it signs, and the widget logs in with the very same
		// channel string it then subscribes to. Anything else is a visitor asking
		// for somebody else's conversation, and since chat ids are sequential that
		// is a practical way to read other visitors' chats.
		if s.srv.cfg.StrictChannelBinding && channel != token.ChanelName {
			logRateLimitedf("channel-binding",
				"[socket %s] refused subscribe to %s: token was issued for %s", s.id, channel, token.ChanelName)
			s.subscribeFailed(ev, channel)
			return
		}
	}

	s.mu.Lock()
	_, alreadySubscribed := s.subs[channel]
	limitReached := !alreadySubscribed && len(s.subs) >= s.srv.cfg.ChannelLimit
	if !limitReached {
		s.subs[channel] = struct{}{}
	}
	s.mu.Unlock()

	if limitReached {
		if ev.Cid != nil {
			s.respondError(*ev.Cid, "InvalidActionError",
				fmt.Sprintf("Socket %s tried to exceed the channel subscription limit of %d", s.id, s.srv.cfg.ChannelLimit))
		}
		return
	}

	// Presence is announced only now that the subscription is really accepted.
	// Publishing it before the channel limit check announced a visitor that never
	// got subscribed - and therefore never published the matching vi_online:false.
	if isChatChannel {
		s.mu.Lock()
		token.ChanelNameChat = channel
		s.mu.Unlock()

		s.publishPresence(channel, true, "")
	} else if token.IsVisitor && s.srv.cfg.TrackVisitors {
		// Online visitor tracking: inform the `ous_<instance>` channel.
		s.publishPresence("ous_"+strconv.Itoa(token.InstanceID), true, lastTokenPart(token.ChanelName))
	}

	s.srv.hub.Add(s, channel)

	if ev.Cid != nil {
		if batch {
			s.respond(*ev.Cid, map[string]bool{"batch": true})
		} else {
			s.respond(*ev.Cid, nil)
		}
	}
}

func (s *Socket) subscribeFailed(ev *inEvent, channel string) {
	if ev.Cid != nil {
		s.respondError(*ev.Cid, "UnauthorizedError", "You are not authorized to subscribe to "+channel)
	}
}

func (s *Socket) handleUnsubscribe(ev *inEvent) {
	channel, _ := parseChannelPayload(ev.Data)
	if channel == "" {
		return
	}

	s.mu.Lock()
	_, ok := s.subs[channel]
	if ok {
		delete(s.subs, channel)
	}
	s.mu.Unlock()

	if ok {
		s.srv.hub.Remove(s, channel)
	}

	if ev.Cid != nil {
		s.respond(*ev.Cid, nil)
	}
}

// handlePublish mirrors the `#publish` handling: it is auto-acked first and
// then fanned out (locally + to Redis, like sc-redis did).
func (s *Socket) handlePublish(ev *inEvent) {
	if ev.Cid != nil {
		s.respond(*ev.Cid, nil)
	}

	var request struct {
		Channel string          `json:"channel"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(ev.Data, &request); err != nil || request.Channel == "" {
		return
	}

	if !s.srv.cfg.AllowClientPublish {
		return
	}

	// SocketCluster's allowClientPublish let a client publish anywhere, and the
	// widget uses that for typing notifications. RESTRICT_CLIENT_PUBLISH narrows
	// it to the channels the socket actually holds, which stops one connection
	// from injecting into other visitors' chat channels.
	if s.srv.cfg.RestrictClientPublish && !s.isSubscribed(request.Channel) {
		logRateLimitedf("publish-restricted",
			"[socket %s] dropped #publish to %s: socket is not subscribed to it", s.id, request.Channel)
		return
	}

	s.srv.publish(request.Channel, request.Data)
}

func (s *Socket) publishPresence(channel string, status bool, vid string) {
	var payload string
	if vid != "" {
		encoded, _ := json.Marshal(vid)
		payload = fmt.Sprintf(`{"op":"vi_online","status":%t,"vid":%s}`, status, encoded)
	} else {
		payload = fmt.Sprintf(`{"op":"vi_online","status":%t}`, status)
	}
	s.srv.publish(channel, json.RawMessage(payload))
}

// cleanup runs the `disconnect` handler of worker.js and drops every channel
// subscription.
func (s *Socket) cleanup() {
	s.mu.Lock()
	channels := make([]string, 0, len(s.subs))
	for channel := range s.subs {
		channels = append(channels, channel)
	}
	s.subs = make(map[string]struct{})
	token := s.token
	s.mu.Unlock()

	for _, channel := range channels {
		s.srv.hub.Remove(s, channel)
	}

	if token != nil && token.IsVisitor {
		switch {
		case token.ChanelNameChat != "":
			s.publishPresence(token.ChanelNameChat, false, "")
		case s.srv.cfg.TrackVisitors:
			s.publishPresence("ous_"+strconv.Itoa(token.InstanceID), false, lastTokenPart(token.ChanelName))
		}
	}

	s.stopOnce.Do(func() { close(s.done) })
	s.closed.Store(true)
	_ = s.ws.Close()

	// Pairs with the "connected from" line, both are debug only.
	logDebugf("[socket %s] disconnected", s.id)
}

// closeWith closes the connection from the server side. Protocol level decisions (ping
// timeout, handshake timeout) are logged so a dropped socket can be explained afterwards;
// client requests (1000) and shutdown (1001) stay silent.
func (s *Socket) closeWith(code uint16, reason string) {
	if s.closed.CompareAndSwap(false, true) {
		if code >= 4000 {
			logInfof("[socket %s] closed by server: code=%d %s", s.id, code, reason)
		}
		_ = s.ws.WriteClose(code, reason)
	}
}

// reportReadError logs only unexpected read failures. A peer that goes away (EOF) and this
// server closing the socket itself are normal parts of the life cycle - logging those
// produced a scary "use of closed network connection" line for every single disconnect.
func (s *Socket) reportReadError(err error) {
	if err == nil || err == errWSClosed || isExpectedDisconnect(err) {
		return
	}
	logWarnf("[socket %s] read error: %v", s.id, err)
}

func isExpectedDisconnect(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return true
	}

	// Some platforms do not wrap these as sentinel errors.
	message := err.Error()
	for _, marker := range []string{
		"use of closed network connection",
		"connection reset by peer",
		"broken pipe",
		"connection aborted",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}

	return false
}

// --- helpers ----------------------------------------------------------------

// parseChannelPayload accepts both the object form `{channel: 'x', batch: true}`
// and the plain string form used by `#unsubscribe`.
func parseChannelPayload(data json.RawMessage) (channel string, batch bool) {
	if len(data) == 0 {
		return "", false
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return "", false
	}

	if trimmed[0] == '"' {
		_ = json.Unmarshal(trimmed, &channel)
		return channel, false
	}

	var payload struct {
		Channel string `json:"channel"`
		Batch   bool   `json:"batch"`
	}
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		return "", false
	}
	return payload.Channel, payload.Batch
}

func (s *Socket) setHandshaken() {
	s.mu.Lock()
	s.handshaken = true
	s.mu.Unlock()
}

func (s *Socket) isHandshaken() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handshaken
}

func (s *Socket) setAuthState(token *authToken, signed string) {
	s.mu.Lock()
	s.token = token
	s.signedToken = signed
	s.authed = true
	s.mu.Unlock()
}

func (s *Socket) setUnauthenticated() {
	s.mu.Lock()
	s.token = nil
	s.signedToken = ""
	s.authed = false
	s.mu.Unlock()
}

func (s *Socket) authToken() *authToken {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.token
}

// isSubscribed reports whether the socket currently holds the channel.
func (s *Socket) isSubscribed(channel string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.subs[channel]
	return ok
}

func (s *Socket) isAuthenticated() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authed
}

func (s *Socket) sendJSON(value any) {
	if s.closed.Load() {
		return
	}
	payload, err := json.Marshal(value)
	if err != nil {
		logErrorf("[socket %s] failed to encode message: %v", s.id, err)
		return
	}
	s.enqueueText(payload)
}

func (s *Socket) enqueueText(payload []byte) {
	if s.closed.Load() {
		return
	}

	select {
	case <-s.done:
		return
	case s.outbound <- payload:
	default:
		logRateLimitedf("slow-consumer", "[socket %s] closed slow consumer (outbound queue full)", s.id)
		s.closeWith(1008, "Outbound queue full")
	}
}

func (s *Socket) sendEventWithCid(event string, data any, cid *int64) {
	out := outEvent{Event: event, Cid: cid}
	if data != nil {
		encoded, err := json.Marshal(data)
		if err != nil {
			logErrorf("[socket %s] failed to encode %s payload: %v", s.id, event, err)
			return
		}
		out.Data = encoded
	}
	s.sendJSON(out)
}

// nextCallID mirrors SCServerSocket._nextCallId().
func (s *Socket) nextCallID() *int64 {
	s.mu.Lock()
	s.callID++
	id := s.callID
	s.mu.Unlock()
	return &id
}

func (s *Socket) respond(cid int64, data any) {
	out := outResponse{Rid: cid}
	if data != nil {
		encoded, err := json.Marshal(data)
		if err != nil {
			logErrorf("[socket %s] failed to encode response: %v", s.id, err)
			return
		}
		out.Data = encoded
	}
	s.sendJSON(out)
}

func (s *Socket) respondError(cid int64, name, message string) {
	s.sendJSON(outResponse{Rid: cid, Error: &errObj{Name: name, Message: message}})
}
