# HomeHub container image — the same artifact `make build` produces (UI,
# companion jars and caddy-reader helper embedded; tags "ui companion helper";
# VERSION stamped in), on a runtime with every tool the app shells out to.
#
#   docker build -t homehub:$(cat VERSION) .
#
# Versions track the repo: Go from go.mod (the helper module asks for a newer
# toolchain, which GOTOOLCHAIN=auto fetches, exactly as the Makefile does), the
# Gradle wrapper and its Java toolchains from companion/, Postgres client tools
# matching the postgres:17 server in docker-compose.yml.
ARG GO_VERSION=1.25.7
ARG NODE_VERSION=22
ARG ALPINE_VERSION=3.22

# ---- frontend ----
FROM node:${NODE_VERSION}-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---- minecraft companion plugins ----
# JDK 21 runs the Gradle wrapper; the 26.x targets' Java 25 arrives through
# the toolchain resolver, as on a dev host (make build-companion).
FROM eclipse-temurin:21-jdk AS companion
WORKDIR /src/companion
COPY companion/ ./
RUN ./gradlew --no-daemon --console=plain -q dist

# ---- backend + caddy-reader helper ----
FROM golang:${GO_VERSION}-alpine AS build
ENV GOTOOLCHAIN=auto CGO_ENABLED=0
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
COPY --from=companion /src/companion/dist ./companion/dist
# make build-helper, inlined: three arches, the installer, and SHA256SUMS.
RUN set -eu; \
    version=$(cat VERSION); \
    cd helper/caddy-reader; \
    for target in amd64 arm64 armv7; do \
        arch=$target; goarm=""; \
        if [ "$target" = armv7 ]; then arch=arm; goarm=7; fi; \
        GOOS=linux GOARCH=$arch GOARM=$goarm \
            go build -trimpath -ldflags "-s -w -X main.Version=$version" \
            -o ../dist/caddy-reader-linux-$target .; \
    done; \
    cp ../install.sh ../dist/install.sh; \
    cd ../dist && sha256sum caddy-reader-linux-* > SHA256SUMS
RUN go build -trimpath -tags "ui companion helper" \
    -ldflags "-s -w -X homehub/internal/version.Version=$(cat VERSION)" \
    -o /out/homehub ./cmd/homehub

# ---- runtime ----
FROM alpine:${ALPINE_VERSION}
# What HomeHub executes at runtime:
#   pg_dump / pg_restore        app backups and restores (major 17 = the server)
#   mariadb-client, mongodb-tools  database dumps of managed MySQL/MariaDB/Mongo
#   ffmpeg, imagemagick, libraw-tools (dcraw_emu), pandoc-cli  file converter
#   util-linux-misc (ionice)    converters run at idle I/O priority
#   ca-certificates, tzdata     outbound TLS, local-time schedules
RUN apk add --no-cache \
        ca-certificates tzdata \
        postgresql17-client mariadb-client mongodb-tools \
        ffmpeg imagemagick libraw-tools pandoc-cli util-linux-misc \
    && addgroup -g 10001 homehub \
    && adduser -D -H -u 10001 -G homehub homehub \
    && mkdir -p /data && chown homehub:homehub /data && chmod 0700 /data
COPY --from=build /out/homehub /usr/local/bin/homehub
# Non-root. The converter then runs in its user-namespace sandbox, which
# Docker's default seccomp/AppArmor profiles block — see docker-compose.yml
# for the two ways to enable conversions of media files.
USER homehub
ENV HOMEHUB_DATA_DIR=/data
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=60s \
    CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["homehub"]
