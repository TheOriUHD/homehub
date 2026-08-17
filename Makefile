SHELL := /bin/bash
VERSION := $(shell cat VERSION)

.PHONY: generate build build-web build-companion dev-api dev-web test lint image

## Regenerate API server interfaces and the typed frontend client from api/openapi.yaml
generate:
	go tool oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml
	cd web && npm run generate

build-web:
	cd web && npm ci && npm run build

## Minecraft companion plugin jars (requires JDK 21 + Gradle)
build-companion:
	cd companion && gradle --no-daemon dist

## Full production binary with the frontend and companion jars embedded
build: build-web build-companion
	CGO_ENABLED=0 go build -trimpath -tags "ui companion" \
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
