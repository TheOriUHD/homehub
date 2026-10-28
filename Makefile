SHELL := /bin/bash
# Every recipe line fails on the first failing command, including inside a
# pipeline — so a broken step can never be followed by the next one (above
# all: a failed build by a restart).
.SHELLFLAGS := -euo pipefail -c
VERSION := $(shell cat VERSION)
BIN := $(CURDIR)/bin/homehub

.PHONY: generate build build-web build-companion build-helper dev-api dev-web test lint image \
	install-service deploy start stop restart status logs

UNIT := homehub

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
		-o bin/homehub.new ./cmd/homehub
	mv -f bin/homehub.new bin/homehub

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

## Install the systemd unit (starts on boot, restarts itself if it exits) and
## the homehubctl wrapper, then start the service. The unit's /opt/homehub
## paths are rewritten to this checkout, so ExecStart is always the binary
## `make build` produces here. Re-run after editing deploy/homehub.service;
## systemd reloads the unit without a restart.
install-service:
	sed -e 's|/opt/homehub|$(CURDIR)|g' deploy/homehub.service > /etc/systemd/system/$(UNIT).service
	chmod 0644 /etc/systemd/system/$(UNIT).service
	ln -sfn $(CURDIR)/deploy/homehubctl /usr/local/bin/homehubctl
	systemctl daemon-reload
	systemctl enable --now $(UNIT)
	systemctl --no-pager --lines=0 status $(UNIT)

## Build and put the new binary live — the whole update loop in one target,
## so it can never run from the wrong directory and restart the old binary.
##
## Refuses to restart when the unit would start some other binary (the
## failure mode where a deploy "succeeds" and the old code keeps running),
## and afterwards checks that the process systemd is running really is the
## freshly built file and is still up a few seconds later.
deploy: build
	@running=$$(systemctl show -p ExecStart --value $(UNIT) | sed -n 's/.*path=\([^ ;]*\).*/\1/p'); \
	if [ "$$running" != "$(BIN)" ]; then \
		echo "deploy: $(UNIT) starts '$$running', not $(BIN) — run 'make install-service' from this checkout first" >&2; \
		exit 1; \
	fi
	systemctl restart $(UNIT)
	@sleep 5; \
	systemctl is-active --quiet $(UNIT) || { echo "deploy: $(UNIT) is not running after restart" >&2; systemctl --no-pager --lines=20 status $(UNIT) >&2 || true; exit 1; }; \
	pid=$$(systemctl show -p MainPID --value $(UNIT)); \
	exe=$$(readlink /proc/$$pid/exe); \
	if [ "$$exe" != "$(BIN)" ]; then echo "deploy: $(UNIT) is running '$$exe', not the new $(BIN)" >&2; exit 1; fi; \
	echo "deploy: $(UNIT) $(VERSION) is live (pid $$pid)"

start stop restart:
	systemctl $@ $(UNIT)
	@systemctl --no-pager --lines=0 status $(UNIT) || true

status:
	@systemctl --no-pager status $(UNIT) || true

logs:
	journalctl -u $(UNIT) -n 100 -f
