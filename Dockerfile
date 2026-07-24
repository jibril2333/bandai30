# Multi-stage build producing a tiny static-binary image.
#
# modernc.org/sqlite is a pure-Go SQLite driver, so CGO_ENABLED=0 gives a
# fully static binary and the runtime stage needs nothing but certs + tzdata.
# The web SPA is compiled in via //go:embed (webfs.go), so there is no
# separate asset stage and nothing to COPY at runtime.

# --- build ---
FROM golang:1.26-alpine AS build
WORKDIR /src
# Split the module download from the source copy so `go mod download` stays
# cached unless go.mod/go.sum actually change.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Pure-Go modernc.org/sqlite means we don't need CGO; static binary.
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/bandai30 ./cmd/bandai30

# --- runtime ---
FROM alpine:3.20
# ca-certificates: the scheduled scraper talks to bandai-hobby.net /
# tamashiiweb.com over TLS. tzdata: release dates and the scrape schedule are
# reasoned about in JST.
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/bandai30 /usr/local/bin/bandai30
ENV BANDAI30_ADDR=0.0.0.0:8080 \
    BANDAI30_DATA=/data \
    TZ=Asia/Tokyo
# Mountpoint only. The real DB + photos live on the host and are bind-mounted
# here by docker-compose; nothing under /data is ever baked into the image.
# No VOLUME instruction on purpose — it would spawn an anonymous volume on any
# run that forgets the bind mount, silently hiding the real data.
RUN mkdir -p /data
# Deliberately no USER: compose pins the container to the host owner's uid:gid
# so the bind-mounted SQLite file is writable. See docker-compose.yml.
EXPOSE 8080
ENTRYPOINT ["bandai30"]
