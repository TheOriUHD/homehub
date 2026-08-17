SHELL := /bin/bash
VERSION := $(shell cat VERSION)

.PHONY: generate build build-web dev-api dev-web test lint image

## Regenerate API server interfaces and the typed frontend client from api/openapi.yaml
generate:
	go tool oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml
	cd web && npm run generate

build-web:
	cd web && npm ci && npm run build

## Full production binary with the frontend embedded
build: build-web
	CGO_ENABLED=0 go build -trimpath -tags ui \
		-ldflags "-s -w -X github.com/TheOriUHD/homehub/internal/version.Version=$(VERSION)" \
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
