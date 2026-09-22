package main

import (
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// logLevel is the verbosity of the process wide logger, set by LOG_LEVEL.
//
// The per connection lines (connect/disconnect) are debug: with a few thousand browsers
// they dwarf everything else in the log, while the lines worth waking up for are warnings
// and errors. `LOG_LEVEL=debug` brings the connection lines back when they are what you
// are looking for (a client that reconnects in a loop, a socket closed by the server).
type logLevel int

const (
	levelDebug logLevel = iota
	levelInfo
	levelWarn
	levelError
)

// currentLogLevel is written once by setupLogging(), before anything else runs, and only
// read afterwards - so it needs no synchronisation.
var currentLogLevel = levelInfo

func (l logLevel) String() string {
	switch l {
	case levelDebug:
		return "debug"
	case levelWarn:
		return "warn"
	case levelError:
		return "error"
	default:
		return "info"
	}
}

func parseLogLevel(value string) (logLevel, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return levelDebug, true
	case "info":
		return levelInfo, true
	case "warn", "warning":
		return levelWarn, true
	case "error":
		return levelError, true
	}
	return levelInfo, false
}

// setupLogging applies LOG_LEVEL (debug|info|warn|error, default info) and installs the
// timestamp format. It runs before loadConfig(), so the [config] warnings honour it too.
func setupLogging() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	if value, set := os.LookupEnv("LOG_LEVEL"); set && value != "" {
		level, valid := parseLogLevel(value)
		if !valid {
			log.Printf("[config] LOG_LEVEL=%q is not debug|info|warn|error, keeping %s", value, currentLogLevel)
		} else {
			currentLogLevel = level
		}
	}

	logInfof("[server] log level %s (set LOG_LEVEL=debug to log every socket connect/disconnect)", currentLogLevel)
}

func logAt(level logLevel, format string, args ...any) {
	if level < currentLogLevel {
		return
	}
	log.Printf(format, args...)
}

func logDebugf(format string, args ...any) {
	logAt(levelDebug, format, args...)
}

func logInfof(format string, args ...any) {
	logAt(levelInfo, format, args...)
}

func logWarnf(format string, args ...any) {
	logAt(levelWarn, format, args...)
}

func logErrorf(format string, args ...any) {
	logAt(levelError, format, args...)
}

// logWarnLine writes an already formatted warning. It exists for messages that
// are not printf style, e.g. ones forwarded from a third party logger.
func logWarnLine(message string) {
	if levelWarn < currentLogLevel {
		return
	}
	log.Print(message)
}

// Some conditions occur once per connection - a slow consumer being dropped, a
// subscribe that is refused. During a burst that is one line per socket (a 4000
// connection fan out produced 4359 of them), which buries everything else and
// serialises the writers behind log's mutex. These helpers give such a message
// a cooldown.
//
// Only constant keys are used, so the bookkeeping stays a handful of entries.
const logRateLimitWindow = 5 * time.Second

var (
	logRateLimitMu    sync.Mutex
	logRateLimitState = map[string]*logRateLimitEntry{}
)

type logRateLimitEntry struct {
	last       time.Time
	suppressed int
}

// logRateLimitedf logs a warning at most once per logRateLimitWindow for the
// given key. How many occurrences were suppressed becomes visible on the next
// line that is written, so the volume is not hidden completely.
func logRateLimitedf(key, format string, args ...any) {
	if levelWarn < currentLogLevel {
		return
	}

	now := time.Now()

	logRateLimitMu.Lock()
	entry, seen := logRateLimitState[key]
	if !seen {
		entry = &logRateLimitEntry{}
		logRateLimitState[key] = entry
	}
	if now.Sub(entry.last) < logRateLimitWindow {
		entry.suppressed++
		logRateLimitMu.Unlock()
		return
	}
	suppressed := entry.suppressed
	entry.suppressed = 0
	entry.last = now
	logRateLimitMu.Unlock()

	if suppressed > 0 {
		logWarnf("[log] %d earlier message(s) of this kind were suppressed in the last %s", suppressed, logRateLimitWindow)
	}
	logWarnf(format, args...)
}
