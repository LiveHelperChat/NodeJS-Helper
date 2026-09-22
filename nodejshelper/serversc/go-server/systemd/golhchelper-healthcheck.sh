#!/bin/sh
#
# Watchdog for golhchelper.service.
#
# Polls /health-check (200 OK only while the Redis bridge is up) and restarts the server
# after FAILS_BEFORE_RESTART consecutive failures. The counter is there so a single blip or
# a short Redis outage does not restart the server and drop every websocket.
#
# Installed as /usr/local/bin/golhchelper-healthcheck and started by
# golhchelper-healthcheck.timer. Can be run by hand as well:
#
#   sudo FAILS_BEFORE_RESTART=1 /usr/local/bin/golhchelper-healthcheck
#
# Exit code is always 0 - a failing health check is logged, not reported as a unit failure,
# so `systemctl status golhchelper-healthcheck` stays meaningful only for real errors.

set -u

PORT="${SOCKETCLUSTER_PORT:-8000}"
URL="http://127.0.0.1:${PORT}/health-check"
FAILS_BEFORE_RESTART="${FAILS_BEFORE_RESTART:-3}"
# Set by systemd because the unit uses StateDirectory=, with a fallback for manual runs.
STATE_DIR="${STATE_DIRECTORY:-/var/lib/golhchelper-healthcheck}"
STATE_FILE="${STATE_DIR}/failures"

log() {
    # journald picks this up with the other unit logs. `logger` is in util-linux, which is
    # part of a base install everywhere systemd is.
    logger -t golhchelper-healthcheck -p "$1" "$2" 2>/dev/null || echo "$2" >&2
}

if curl -fsS --max-time 5 "$URL" >/dev/null 2>&1; then
    if [ -f "$STATE_FILE" ]; then
        rm -f "$STATE_FILE"
        log daemon.notice "health check ok again, failure counter reset"
    fi
    exit 0
fi

fails=0
if [ -f "$STATE_FILE" ]; then
    fails=$(cat "$STATE_FILE" 2>/dev/null || echo 0)
fi
# Anything unexpected in the file (empty, garbage) restarts the count from zero.
case "$fails" in
    ''|*[!0-9]*) fails=0 ;;
esac

fails=$((fails + 1))
if ! printf '%s\n' "$fails" > "$STATE_FILE" 2>/dev/null; then
    # Without the counter every failure would restart the server, so play it safe and
    # report instead of hammering it.
    log daemon.warning "cannot write $STATE_FILE, not restarting: $URL did not answer 200"
    exit 0
fi

if [ "$fails" -lt "$FAILS_BEFORE_RESTART" ]; then
    log daemon.warning "health check failed ($fails/$FAILS_BEFORE_RESTART): $URL did not answer 200"
    exit 0
fi

rm -f "$STATE_FILE"
log daemon.err "health check failed $fails times in a row, restarting golhchelper"
systemctl restart golhchelper
