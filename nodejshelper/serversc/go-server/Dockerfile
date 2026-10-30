# Build stage. The Go image has to satisfy the `go` directive in go.mod (go 1.24).
# NOTE: there is deliberately no `COPY vendor/ ./vendor/` here. `vendor/` is gitignored in
# NodeJS-Helper (nodejshelper/.gitignore), so it only exists in the maintainer's working
# copy and a fresh clone never has it - `COPY` of a missing directory aborts the whole
# build ("failed to compute cache key: ... vendor: not found"). Modules are downloaded
# instead, cached in their own layer keyed only on go.mod/go.sum, so source edits do not
# re-download them. A local `vendor/` is simply ignored by the image build.
FROM golang:1.26-alpine AS build

WORKDIR /src

ENV CGO_ENABLED=0 \
    GOFLAGS= \
    GOTOOLCHAIN=local

# Small dependency set (go-redis + 3 indirect), so this is quick and only re-runs when
# go.mod/go.sum change. Requires network on the *first* build of a fresh clone.
COPY go.mod go.sum ./
RUN go mod download

# Sources only - keeps .env, vendor/, loadtest/ and the compose files out of the build.
COPY *.go ./

RUN go build -mod=mod -trimpath -ldflags="-s -w" -o /out/lhcnodejs .

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
    LOG_LEVEL=info \
    TRACK_VISITORS=true

EXPOSE 8000

# Server can be reached at /health-check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
    CMD wget -q -O /dev/null http://127.0.0.1:8000/health-check || exit 1

ENTRYPOINT ["/usr/local/bin/lhcnodejs"]
