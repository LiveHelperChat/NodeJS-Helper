package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

// defaultSecretHash is the value that was hard coded in serversc/lhc/server.js
// (`secretHash`). It has to match `site.secrethash` from the Live Helper Chat
// settings, otherwise the `login` event will never authenticate a client.
const defaultSecretHash = "e9ekdkld4d0_D934-+_4535d_D9jasd@ASGFjkSDFfjksdffksdF456$#)8$asf931a171"

// Config holds everything that used to be spread between
// serversc/lhc/server.js (SocketCluster options + brokerOptions) and worker.js
// (per socket behaviour).
type Config struct {
	// Network
	Port int    // SOCKETCLUSTER_PORT (default 8000)
	Path string // SOCKETCLUSTER_PATH (default /socketcluster/)

	// Authentication
	SecretHash string // SECRET_HASH - must match LHC site.secrethash
	AuthKey    []byte // AUTH_KEY - HMAC key used to sign the socket auth tokens

	// Behaviour
	TrackVisitors      bool          // TRACK_VISITORS (server.js `trackVisitors`)
	ChannelLimit       int           // SOCKET_CHANNEL_LIMIT (default 1000)
	PingInterval       time.Duration // PING_INTERVAL_MS (SocketCluster default 8000ms)
	PingTimeout        time.Duration // PING_TIMEOUT_MS (SocketCluster default 20000ms)
	HandshakeTimeout   time.Duration // HANDSHAKE_TIMEOUT_MS (default 10000ms)
	MaxPayload         int64         // MAX_PAYLOAD (bytes, default 4MiB)
	AllowedOrigins     string        // ORIGINS (default *:*)
	AllowClientPublish bool          // ALLOW_CLIENT_PUBLISH (default true)

	// Redis (server.js `brokerOptions` + sc-redis)
	RedisHost  string // REDIS_HOST
	RedisPort  int    // REDIS_PORT
	RedisUser  string // REDIS_USER - ACL user name (Redis 6+), empty = `default`
	RedisPass  string // REDIS_PASS
	RedisDB    int    // REDIS_DB
	InstanceID string // SC_INSTANCE_ID - node id used as the sc-redis message prefix

	// HTTP
	StaticDir string // STATIC_DIR - replaces express `serve-static`
}

func envStr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			log.Printf("[config] %s=%q is not a number, keeping %d", key, v, def)
			return def
		}
		return n
	}
	return def
}

func envBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
		log.Printf("[config] %s=%q is not a boolean, keeping %v", key, v, def)
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	ms := envInt(key, int(def/time.Millisecond))
	if ms <= 0 {
		return def
	}
	return time.Duration(ms) * time.Millisecond
}

// defaultInstanceID returns the id used as the sc-redis message prefix.
//
// SocketCluster left `instanceId` undefined unless it was configured, and sc-redis then
// had no way to recognise its own messages: everything published was delivered twice (once
// locally and once after the Redis round trip). A unique default keeps the federated
// behaviour without the duplicate delivery.
//
// It is random per process on purpose: a hostname+pid id collides for two replicas running
// with `network_mode: host` on the same machine (both see pid 1 and the host's hostname),
// and colliding nodes silently discard each other's messages.
func defaultInstanceID() string {
	if configured := os.Getenv("SC_INSTANCE_ID"); configured != "" {
		return sanitizeInstanceID(configured)
	}

	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("node-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func sanitizeInstanceID(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == ' ' {
			return '-'
		}
		return r
	}, value)
}

func loadConfig() *Config {
	cfg := &Config{
		Port:               envInt("SOCKETCLUSTER_PORT", envInt("PORT", 8000)),
		Path:               envStr("SOCKETCLUSTER_PATH", "/socketcluster/"),
		SecretHash:         envStr("SECRET_HASH", envStr("SOCKETCLUSTER_SECRET_HASH", defaultSecretHash)),
		TrackVisitors:      envBool("TRACK_VISITORS", true),
		ChannelLimit:       envInt("SOCKET_CHANNEL_LIMIT", 1000),
		PingInterval:       envDuration("PING_INTERVAL_MS", 8*time.Second),
		PingTimeout:        envDuration("PING_TIMEOUT_MS", 20*time.Second),
		HandshakeTimeout:   envDuration("HANDSHAKE_TIMEOUT_MS", 10*time.Second),
		MaxPayload:         int64(envInt("MAX_PAYLOAD", 4*1024*1024)),
		AllowedOrigins:     envStr("ORIGINS", "*:*"),
		AllowClientPublish: envBool("ALLOW_CLIENT_PUBLISH", true),
		RedisHost:          envStr("REDIS_HOST", "127.0.0.1"),
		RedisPort:          envInt("REDIS_PORT", 6379),
		RedisUser:          envStr("REDIS_USER", ""),
		RedisPass:          envStr("REDIS_PASS", ""),
		RedisDB:            envInt("REDIS_DB", 0),
		InstanceID:         defaultInstanceID(),
		StaticDir:          envStr("STATIC_DIR", "public"),
	}

	// SocketCluster generated a random 32 byte key on every start when authKey was
	// not configured, which means stored client tokens stop working after a restart
	// (clients transparently call `login` again). Set AUTH_KEY to keep them valid.
	if key := os.Getenv("AUTH_KEY"); key != "" {
		cfg.AuthKey = []byte(key)
	} else {
		cfg.AuthKey = randomHexKey()
		log.Printf("[config] AUTH_KEY not set - using a random token signing key, clients will re-login after restart")
	}

	// Make sure the WS path has both a leading and a trailing slash, exactly like
	// SCServer does with `opts.path`.
	if !strings.HasPrefix(cfg.Path, "/") {
		cfg.Path = "/" + cfg.Path
	}
	if !strings.HasSuffix(cfg.Path, "/") {
		cfg.Path += "/"
	}

	if cfg.MaxPayload < 1024 {
		cfg.MaxPayload = 1024
	}

	return cfg
}
