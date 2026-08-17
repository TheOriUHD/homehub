# HomeHub

A single, self-hosted control plane for your homelab. HomeHub manages your
connected systems natively — driven by each system's API and rendered in one
coherent interface. No iframes, no link farms.

## Integrations

Everything is added and configured in the app, at runtime, with
test-before-save and live health supervision. Multiple instances of every
type are supported.

**Proxmox VE** — cluster and node overview with live graphs, full VM/LXC
lifecycle, migration, cloning, snapshots, raw hardware editing, vzdump
(on-demand and scheduled), storage and task views, and native in-app
consoles: noVNC display, serial terminal, and per-guest SSH shells with
encrypted stored credentials.

**Docker** — multiple hosts over the local socket, TCP with mutual TLS, or
SSH. Unified container list with live updates from the event stream,
lifecycle actions, live log streaming, in-container shells, per-container
CPU/memory history, image management with registry update detection and
private registry credentials, volumes, networks, and compose-stack views
with pull-and-recreate redeploys.

More integrations land as vertical slices — each ships complete or not at
all.

## Quickstart

Requirements: Docker with the compose plugin.

```bash
docker compose up -d
```

Open `http://<host>:8080` and follow the setup wizard: create the
administrator account and you're in. Everything else — users, roles,
integrations, backups — is configured inside the app.

### Configuration

HomeHub is configured at runtime, in the app. Environment variables exist only
for bootstrap concerns, and every one has a working default:

| Variable               | Default                              | Purpose                                   |
| ---------------------- | ------------------------------------ | ----------------------------------------- |
| `HOMEHUB_DATA_DIR`     | `/data` (image) · `./data` (binary)  | Keyfile and local working files           |
| `HOMEHUB_DATABASE_URL` | local PostgreSQL                     | PostgreSQL connection string              |
| `HOMEHUB_PORT`         | `8080`                               | Listen port (`HOMEHUB_ADDR` for full addr)|
| `HOMEHUB_MASTER_KEY`   | auto-generated keyfile               | Base64 32-byte secret-wrapping key        |
| `HOMEHUB_LOG_LEVEL`    | `info`                               | `debug` · `info` · `warn` · `error`       |

**Back up `master.key`.** Credentials stored by HomeHub are envelope-encrypted;
the wrapping key lives in `HOMEHUB_MASTER_KEY` or `<data dir>/master.key` and
is never written to the database. Without it, stored secrets are unrecoverable.

## Security model

- Local accounts with argon2id hashing, per-IP and per-account rate limiting,
  and escalating lockout on repeated failures.
- Optional TOTP two-factor with single-use recovery codes; passkeys as a
  passwordless primary or a second factor.
- Administrators never choose user passwords: onboarding issues an invite link
  or a one-time temporary password that must be replaced at first sign-in.
- Resource-scoped roles, deny-by-default. A grant applies to a resource node
  and its subtree, with separate view / control / configure / delete levels
  and explicit deny. Denied resources are hidden, not disabled — navigation,
  search, and notifications all filter server-side.
- Every state-changing action is written to an append-only audit log.

## API

The web frontend consumes the same documented API a script could:
`GET /api/openapi.json` serves the OpenAPI 3.0 document. Real-time updates run
over a single multiplexed WebSocket at `/api/ws` (see the description in the
spec).

## Development

Requirements: Go 1.24+, Node 20.19+, PostgreSQL 17.

```bash
# terminal 1 — API on :8080
make dev-api

# terminal 2 — frontend on :5173, proxying /api
make dev-web
```

`api/openapi.yaml` is the source of truth for the HTTP API. After editing it,
`make generate` regenerates the Go server interfaces and the typed TypeScript
client. `make build` produces `bin/homehub` with the frontend embedded;
`make image` builds the container image.
