package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// Server wires the HTTP frontend, the channel hub and the Redis bridge - the
// three processes (master, worker, broker) of the Node.js deployment collapsed
// into one.
type Server struct {
	cfg    *Config
	hub    *Hub
	bridge *redisBridge

	socketsMu sync.Mutex
	sockets   map[*Socket]struct{}
}

func newServer(cfg *Config) *Server {
	srv := &Server{cfg: cfg, hub: newHub(), sockets: make(map[*Socket]struct{})}

	srv.bridge = newRedisBridge(cfg.RedisHost, cfg.RedisPort, cfg.RedisUser, cfg.RedisPass, cfg.RedisDB, cfg.InstanceID,
		func(channel string, data json.RawMessage) {
			// Messages coming from Redis are only fanned out locally - they are
			// never published back to Redis (this is what keeps the bridge from
			// looping).
			srv.hub.PublishLocal(channel, data)
		})

	srv.hub.onFirstSubscribe = srv.bridge.EnsureSubscribed
	srv.hub.onLastUnsubscribe = srv.bridge.EnsureUnsubscribed

	return srv
}

// publish is the equivalent of `scServer.exchange.publish()` in worker.js:
// local sockets get the packet immediately and the other nodes get it over Redis.
func (s *Server) publish(channel string, data json.RawMessage) {
	s.hub.PublishLocal(channel, data)
	s.bridge.Publish(channel, data)
}

func (s *Server) registerSocket(socket *Socket) {
	s.socketsMu.Lock()
	s.sockets[socket] = struct{}{}
	s.socketsMu.Unlock()
}

func (s *Server) unregisterSocket(socket *Socket) {
	s.socketsMu.Lock()
	delete(s.sockets, socket)
	s.socketsMu.Unlock()
}

func (s *Server) socketCount() int {
	s.socketsMu.Lock()
	defer s.socketsMu.Unlock()
	return len(s.sockets)
}

// closeAllSockets drops every live connection (1001 "going away"). On a rolling restart
// or a redeploy this makes browsers reconnect at once - to another node when they sit
// behind a load balancer - instead of waiting for the process to disappear, and the
// per socket cleanup publishes vi_online:false on the way out.
func (s *Server) closeAllSockets(code uint16, reason string) {
	s.socketsMu.Lock()
	sockets := make([]*Socket, 0, len(s.sockets))
	for socket := range s.sockets {
		sockets = append(sockets, socket)
	}
	s.socketsMu.Unlock()

	for _, socket := range sockets {
		socket.closeWith(code, reason)
	}
}

func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request) {
	if isUpgradeRequest(r) {
		if r.URL.Path != s.cfg.Path {
			http.Error(w, "400 Bad Request", http.StatusBadRequest)
			return
		}

		ws, err := upgradeWS(w, r, s.cfg.AllowedOrigins, s.cfg.MaxPayload)
		if err != nil {
			// upgradeWS already answered with an HTTP error when the handshake
			// was rejected before hijacking the connection.
			logWarnf("[http] websocket upgrade from %s rejected: %v", r.RemoteAddr, err)
			return
		}

		socket := newSocket(s, ws)
		s.registerSocket(socket)
		// One line per connection: debug only, otherwise a busy install buries the
		// warnings under its own reconnects (LOG_LEVEL=debug brings it back).
		logDebugf("[socket %s] connected from %s", socket.id, ws.RemoteAddr())
		go socket.run()
		return
	}

	// Replaces express' `serve-static` which served the (usually empty) public dir.
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		if info, err := os.Stat(s.cfg.StaticDir); err == nil && info.IsDir() {
			http.FileServer(http.Dir(s.cfg.StaticDir)).ServeHTTP(w, r)
			return
		}
	}

	http.NotFound(w, r)
}

func main() {
	setupLogging()

	cfg := loadConfig()
	srv := newServer(cfg)
	srv.bridge.Start()

	mux := http.NewServeMux()
	mux.HandleFunc("/health-check", func(w http.ResponseWriter, r *http.Request) {
		// The old health check queried the broker cluster; here the Redis bridge is
		// the equivalent dependency, so a broken one fails the check instead of
		// silently swallowing everything PHP publishes.
		if !srv.bridge.Healthy() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("Failed"))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	mux.HandleFunc("/", srv.handleHTTP)

	addr := ":" + strconv.Itoa(cfg.Port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	logInfof("[server] listening on %s (websocket path %s, redis %s:%d, instance id %q, track visitors %v)",
		addr, cfg.Path, cfg.RedisHost, cfg.RedisPort, cfg.InstanceID, cfg.TrackVisitors)

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[server] %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	logInfof("[server] shutting down")

	// Drop the websockets first so clients can reconnect somewhere else immediately.
	srv.closeAllSockets(1001, "Server shutting down")
	shutdownDeadline := time.Now().Add(2 * time.Second)
	for srv.socketCount() > 0 && time.Now().Before(shutdownDeadline) {
		time.Sleep(20 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)

	logInfof("[server] stopped")
}
