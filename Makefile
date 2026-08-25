SHELL := /bin/bash
VERSION := $(shell cat VERSION)

.PHONY: generate build build-web build-companion build-helper dev-api dev-web test lint image

## Regenerate API server interfaces and the typed frontend client from api/openapi.yaml
generate:
	go tool oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml
	cd web && npm run generate

build-web:
	cd web && npm ci && npm run build

## Minecraft companion plugin jars (JDK 21 on the host; the wrapper fetches
## Gradle, and the 26.x targets' Java 25 arrives through the toolchain resolver)
build-companion:
	cd companion && ./gradlew --no-daemon --console=plain -q dist

## caddy-reader helper binaries (amd64/arm64/armv7) + installer into helper/dist,
## embedded into the server by the "helper" build tag. helper/caddy-reader is its
## own Go module (vendored from references/caddy/caddy-reader-export).
build-helper:
	cd helper/caddy-reader && \
	for target in amd64 arm64 armv7; do \
		arch=$$target; goarm=""; \
		if [ $$target = armv7 ]; then arch=arm; goarm=7; fi; \
		GOTOOLCHAIN=auto CGO_ENABLED=0 GOOS=linux GOARCH=$$arch GOARM=$$goarm \
			go build -trimpath -ldflags "-s -w -X main.Version=$(VERSION)" \
			-o ../dist/caddy-reader-linux-$$target . || exit 1; \
	done
	cp helper/install.sh helper/dist/install.sh
	cd helper/dist && sha256sum caddy-reader-linux-* > SHA256SUMS

## Full production binary with the frontend, companion jars and caddy-reader
## helper embedded
build: build-web build-companion build-helper
	CGO_ENABLED=0 go build -trimpath -tags "ui companion helper" \
		-ldflags "-s -w -X homehub/internal/version.Version=$(VERSION)" \
		-o bin/homehub ./cmd/homehub

## API only, no embedded UI (frontend served by `make dev-web`)
dev-api:
	go run ./cmd/homehub

dev-web:
	cd web && npm run dev

test:
	go test ./...
	cd web && npm run typecheck

image:
	docker build -t homehub:$(VERSION) -t homehub:latest .
