# ---- frontend ----
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# ---- backend ----
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -tags ui \
    -ldflags "-s -w -X github.com/TheOriUHD/homehub/internal/version.Version=$(cat VERSION)" \
    -o /out/homehub ./cmd/homehub

# ---- runtime ----
FROM alpine:3.22
# postgresql client provides pg_dump/pg_restore for the backup system.
RUN apk add --no-cache ca-certificates tzdata postgresql17-client \
    && addgroup -g 10001 homehub \
    && adduser -D -u 10001 -G homehub homehub \
    && mkdir -p /data && chown homehub:homehub /data
COPY --from=build /out/homehub /usr/local/bin/homehub
USER homehub
ENV HOMEHUB_DATA_DIR=/data
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["homehub"]
