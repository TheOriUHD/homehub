# ---- frontend ----
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# ---- minecraft companion plugins ----
FROM eclipse-temurin:21-jdk AS companion
RUN apt-get update && apt-get install -y --no-install-recommends curl unzip \
    && curl -sL https://services.gradle.org/distributions/gradle-8.10.2-bin.zip -o /tmp/gradle.zip \
    && unzip -q /tmp/gradle.zip -d /opt && rm /tmp/gradle.zip
WORKDIR /src/companion
COPY companion/ ./
RUN /opt/gradle-8.10.2/bin/gradle --no-daemon dist

# ---- backend ----
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
COPY --from=companion /src/companion/dist ./companion/dist
RUN CGO_ENABLED=0 go build -trimpath -tags "ui companion" \
    -ldflags "-s -w -X homehub/internal/version.Version=$(cat VERSION)" \
    -o /out/homehub ./cmd/homehub

# ---- runtime ----
FROM alpine:3.22
# postgresql client provides pg_dump/pg_restore for the backup system;
# mariadb-client and mongodb-tools provide the database dump tools.
RUN apk add --no-cache ca-certificates tzdata postgresql17-client mariadb-client mongodb-tools \
    && addgroup -g 10001 homehub \
    && adduser -D -u 10001 -G homehub homehub \
    && mkdir -p /data && chown homehub:homehub /data
COPY --from=build /out/homehub /usr/local/bin/homehub
USER homehub
ENV HOMEHUB_DATA_DIR=/data
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["homehub"]
