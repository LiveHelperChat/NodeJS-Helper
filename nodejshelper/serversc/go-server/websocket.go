package main

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Minimal RFC6455 server side implementation. The only peer is the
// socketcluster-client (v13/v16) shipped in design/nodejshelpertheme/js, which
// uses plain text frames and does not negotiate extensions or sub protocols.

const (
	opContinuation byte = 0x0
	opText         byte = 0x1
	opBinary       byte = 0x2
	opClose        byte = 0x8
	opPing         byte = 0x9
	opPong         byte = 0xA

	wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

var (
	errWSClosed        = errors.New("websocket: connection closed")
	errWSMessageTooBig = errors.New("websocket: message exceeds max payload")
	errWSCorruptFrame  = errors.New("websocket: invalid frame")
)

type wsConn struct {
	conn net.Conn
	br   *bufio.Reader

	writeMu sync.Mutex
	closed  bool

	// Scratch space for writeFrame, guarded by writeMu. The header is assembled
	// in place and the two slices are handed to net.Buffers as the writev iovec,
	// so a frame costs no allocation and no copy of the payload.
	writeHeader [10]byte
	writeBufs   [2][]byte

	maxPayload int64
}

// tokenListContains reports whether a comma separated HTTP header value (e.g.
// `keep-alive, Upgrade`) contains the given token.
func tokenListContains(header, token string) bool {
	for _, part := range strings.Split(header, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

func isUpgradeRequest(r *http.Request) bool {
	return tokenListContains(r.Header.Get("Connection"), "upgrade") &&
		strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket")
}

// originAllowed mirrors SCServer.verifyHandshake origin handling. `*:*` allows
// everything (the default), otherwise entries of the form host:port,
// host:*, *:port are matched.
func originAllowed(origins string, r *http.Request) bool {
	if strings.Contains(origins, "*:*") {
		return true
	}

	origin := r.Header.Get("Origin")
	if origin == "" || origin == "null" {
		origin = "*"
	}

	originURL, err := url.Parse(origin)
	if err != nil {
		return false
	}
	port := originURL.Port()
	if port == "" {
		if originURL.Scheme == "https" || originURL.Scheme == "wss" {
			port = "443"
		} else {
			port = "80"
		}
	}
	hostname := originURL.Hostname()

	for _, entry := range strings.Split(origins, ",") {
		entry = strings.TrimSpace(entry)
		if entry == hostname+":"+port || entry == hostname+":*" || entry == "*:"+port {
			return true
		}
	}
	return false
}

// upgradeWS performs the WebSocket handshake and hijacks the connection.
func upgradeWS(w http.ResponseWriter, r *http.Request, origins string, maxPayload int64) (*wsConn, error) {
	if !strings.EqualFold(r.Header.Get("Sec-WebSocket-Version"), "13") {
		msg := fmt.Sprintf("unsupported websocket version %q", r.Header.Get("Sec-WebSocket-Version"))
		http.Error(w, msg, http.StatusBadRequest)
		return nil, errors.New(msg)
	}

	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		http.Error(w, "missing Sec-WebSocket-Key header", http.StatusBadRequest)
		return nil, errors.New("missing Sec-WebSocket-Key header")
	}

	if !originAllowed(origins, r) {
		msg := fmt.Sprintf("origin %q is not allowed", r.Header.Get("Origin"))
		http.Error(w, msg, http.StatusForbidden)
		return nil, errors.New(msg)
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("response writer does not support hijacking")
	}

	conn, brw, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}

	accept := base64.StdEncoding.EncodeToString(sha1Sum(key + wsGUID))

	response := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"

	if _, err := brw.WriteString(response); err != nil {
		conn.Close()
		return nil, err
	}
	if err := brw.Flush(); err != nil {
		conn.Close()
		return nil, err
	}

	return &wsConn{
		conn: conn,
		// brw.Reader may already hold frames that arrived together with the
		// upgrade request, so it must be used for all further reads.
		br:         brw.Reader,
		maxPayload: maxPayload,
	}, nil
}

func sha1Sum(s string) []byte {
	sum := sha1.Sum([]byte(s))
	return sum[:]
}

func (c *wsConn) RemoteAddr() string {
	if c.conn == nil {
		return ""
	}
	return c.conn.RemoteAddr().String()
}

func (c *wsConn) SetReadDeadline(t time.Time) {
	_ = c.conn.SetReadDeadline(t)
}

// ReadMessage returns the next complete data message. Control frames are
// handled transparently (ping is answered with pong, close closes the socket).
func (c *wsConn) ReadMessage() (byte, []byte, error) {
	var (
		messageOpcode byte
		messageBuf    []byte
		fragmented    bool
	)

	for {
		fin, opcode, payload, err := c.readFrame()
		if err != nil {
			return 0, nil, err
		}

		switch opcode {
		case opPing:
			if err := c.writeFrame(opPong, payload); err != nil {
				return 0, nil, err
			}
			continue
		case opPong:
			continue
		case opClose:
			_ = c.WriteClose(1000, "")
			return 0, nil, errWSClosed
		case opContinuation:
			if !fragmented {
				return 0, nil, errWSCorruptFrame
			}
			// Checked before appending: maxPayload is what bounds the memory a
			// client can pin, and appending first let a fragmented message grow
			// to twice that.
			if int64(len(messageBuf))+int64(len(payload)) > c.maxPayload {
				return 0, nil, errWSMessageTooBig
			}
			messageBuf = append(messageBuf, payload...)
			if fin {
				return messageOpcode, messageBuf, nil
			}
		case opText, opBinary:
			if fragmented {
				return 0, nil, errWSCorruptFrame
			}
			if fin {
				return opcode, payload, nil
			}
			fragmented = true
			messageOpcode = opcode
			messageBuf = append(messageBuf, payload...)
		default:
			return 0, nil, errWSCorruptFrame
		}
	}
}

func (c *wsConn) readFrame() (fin bool, opcode byte, payload []byte, err error) {
	var header [2]byte
	if _, err = io.ReadFull(c.br, header[:]); err != nil {
		return false, 0, nil, err
	}

	fin = header[0]&0x80 != 0
	if header[0]&0x70 != 0 {
		return false, 0, nil, errWSCorruptFrame // RSV bits without a negotiated extension
	}

	opcode = header[0] & 0x0f
	masked := header[1]&0x80 != 0
	if !masked {
		return false, 0, nil, errWSCorruptFrame
	}
	length := int64(header[1] & 0x7f)

	switch length {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return false, 0, nil, err
		}
		length = int64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return false, 0, nil, err
		}
		length = int64(binary.BigEndian.Uint64(ext[:]))
	}

	if length < 0 || length > c.maxPayload {
		return false, 0, nil, errWSMessageTooBig
	}
	if opcode >= opClose && (!fin || length > 125) {
		return false, 0, nil, errWSCorruptFrame // control frames cannot be fragmented
	}

	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(c.br, mask[:]); err != nil {
			return false, 0, nil, err
		}
	}

	payload = make([]byte, length)
	if _, err = io.ReadFull(c.br, payload); err != nil {
		return false, 0, nil, err
	}

	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}

	return fin, opcode, payload, nil
}

// writeFrame writes one unmasked server frame. The header is built on the stack
// and the payload is passed to the connection untouched: net.Buffers uses writev
// here (the hijacked connection is a *net.TCPConn), so no copy of the message is
// made. Building a contiguous frame buffer instead cost one allocation and one
// full copy of every single outbound message.
func (c *wsConn) writeFrame(opcode byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.closed {
		return errWSClosed
	}

	header := c.writeHeader[:0]
	header = append(header, 0x80|opcode)

	switch n := len(payload); {
	case n < 126:
		header = append(header, byte(n))
	case n <= 0xFFFF:
		header = append(header, 126, byte(n>>8), byte(n))
	default:
		header = append(header, 127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		header = append(header, ext[:]...)
	}

	c.writeBufs[0] = header
	buffers := net.Buffers(c.writeBufs[:1])

	if len(payload) > 0 {
		c.writeBufs[1] = payload
		buffers = net.Buffers(c.writeBufs[:2])
	}

	_, err := buffers.WriteTo(c.conn)
	return err
}

// WriteText sends a text frame (SocketCluster payloads are JSON strings).
func (c *wsConn) WriteText(payload []byte) error {
	return c.writeFrame(opText, payload)
}

// WriteClose sends a close frame and tears the TCP connection down.
func (c *wsConn) WriteClose(code uint16, reason string) error {
	payload := make([]byte, 0, len(reason)+2)
	payload = append(payload, byte(code>>8), byte(code))
	payload = append(payload, reason...)

	err := c.writeFrame(opClose, payload)

	c.writeMu.Lock()
	c.closed = true
	c.writeMu.Unlock()

	_ = c.conn.Close()
	return err
}

// Close closes the TCP connection without a close frame (used for protocol
// level errors where the socket has already been written to).
func (c *wsConn) Close() error {
	c.writeMu.Lock()
	c.closed = true
	c.writeMu.Unlock()
	return c.conn.Close()
}
