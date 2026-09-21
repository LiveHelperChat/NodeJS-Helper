# Build stage - the server has no third party dependencies, so this works offline.
FROM golang:1.23-alpine AS build

WORKDIR /src

COPY go.mod ./
COPY *.go ./

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/lhcnodejs .

# Runtime stage (alpine instead of scratch so CA certificates are available if
# Redis is ever reached over TLS or the public/ directory is served).
FROM alpine:3.20

RUN apk add --no-cache ca-certificates && adduser -D -H -u 10001 lhc

COPY --from=build /out/lhcnodejs /usr/local/bin/lhcnodejs

# Optional static files, replaces express' serve-static on ./public
RUN mkdir -p /srv/public && chown -R lhc /srv
WORKDIR /srv
USER lhc

ENV SOCKETCLUSTER_PORT=8000 \
    SOCKETCLUSTER_PATH=/socketcluster/ \
    STATIC_DIR=/srv/public \
    REDIS_HOST=127.0.0.1 \
    REDIS_PORT=6379 \
    TRACK_VISITORS=true

EXPOSE 8000

# Server can be reached at /health-check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
    CMD wget -q -O /dev/null http://127.0.0.1:8000/health-check || exit 1

ENTRYPOINT ["/usr/local/bin/lhcnodejs"]
