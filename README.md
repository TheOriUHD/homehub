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

**DNS filtering** — Pi-hole (v5 and v6, autodetected) and AdGuard Home under
one normalized surface: query rates and block percentage with history, live
query log, blocklist and custom-rule management, client and group management,
timed blocking disable with countdown, and cross-instance rule comparison
with one-way sync.

**Home Assistant** — native WebSocket integration with instant state push:
an area-grouped dashboard with real controls per domain (lights with
brightness, climate, covers, media, locks, scenes, scripts), automations
with trigger and enable/disable, and sensor history recorded into the
metrics engine for graphs and alerting. Permissions scope down to a single
entity or a whole domain.

**Databases** — MySQL, MariaDB, and MongoDB under one module: live server
stats and history, a database and table/collection browser with a full
Adminer-style data editor (paginated rows with per-column filters, add /
edit / delete, truncate and drop behind confirmation; MongoDB document
insert / replace / delete), an audited SQL console, live sessions with
kill, and dumps to any backup target. Permissions scope to a single
database.

**TrueNAS** — storage at a glance: pools with capacity and health,
datasets and zvols, disks with type and SMART temperature, SMB / NFS /
iSCSI shares, snapshots, and system alerts, with capacity and temperature
history. Permissions scope to a single pool.

**Minecraft** — servers connect through the HomeHub Companion plugin
(Paper/Spigot, with Velocity, Fabric, and Forge builds), downloaded from
inside the app and paired with a 6-digit code — the server dials out, so
no ports open on the Minecraft side. Live console with scrollback all the
way to server start, instant command execution, online players, and
TPS/tick-time/heap history. The companion reconnects on its own.

**Also connected natively** — CubeCoders AMP game panel, Caddy reverse proxy
(with optional Cloudflare zone linking), Tailscale, the Aperture NVR, Klipper
3D printers over Moonraker, and PatchPilot, an agentless update manager for a
Linux fleet.

More integrations land as vertical slices — each ships complete or not at
all.

**[docs/FEATURES.md](docs/FEATURES.md) is the complete feature reference** —
every module, every platform feature, in detail. `docs/handoff-*.md` are
session handoffs: what changed, what is still open, and the non-obvious
traps in this codebase. The newest one is self-contained and is where an
agent or contributor picking the project up should start.

## Platform

**History** — every integration feeds a metrics engine (raw samples rolled
into 5-minute and 1-hour tiers, retention configurable in-app). The History
page graphs any recorded series across systems on one timeline.

**Alerting** — threshold rules over any metric with sustained-for windows,
edge-triggered firing/resolved notifications, and delivery to webhook, email,
ntfy, Gotify, Telegram, or Discord channels — all configured at runtime.
Per-user routing decides which events reach which channels.

**Signals** — one stream of everything HomeHub's monitors currently see across
every system: failures, warnings, notable events, and green "recovered on its
own" notes, each carrying its deep payload (a failed update run brings its
per-host errors and log tails along). Dismissable, groupable, and pushed to
Telegram as conditions appear and clear.

**Assistant** — chat over any OpenAI-compatible provider, cloud or local, that
answers from live tools rather than training data. Every tool call runs under
the asking user's own permissions, and each answer names the tools behind it.
Destructive actions need confirmation; SSH commands additionally pass a
classifier that refuses catastrophic ones outright. Reachable from a Telegram
chat linked to a HomeHub account.

**SSH hosts** — one registry of every machine HomeHub can reach over SSH,
entered once and selected everywhere else. One-click keypair generation
installs and verifies the key for you. Proxmox guest consoles, Minecraft,
Docker-over-SSH, and the assistant all dial from it.

**Utilities** — a workspace (folders, uploads, downloads) with a text editor
and full-page markdown documents the assistant can read, a universal file
converter across images, camera raw, video, audio, tables and documents, and
twenty small tools from subnet math to EXIF stripping.

**Calendar** — month and week views over local calendars, two-way CalDAV,
subscribed ICS feeds, and trash-pickup schedules, with recurring events that
ask how far an edit should reach.

**Backups** — scheduled application backups (`pg_dump` of the full state)
to local disk, SMB shares, NFS exports, or S3-compatible storage, with
per-target retention and optional age encryption. Targets are managed and
write-tested in the app; restore replays an archive and restarts cleanly.
The encryption key is exportable (audited) — store it offline. The database
dump never contains the master key, so a restored instance needs the
original `master.key` to unlock stored credentials.

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

### Run as a system service

On a host that runs the binary directly instead of the container, install the
systemd unit once, from the repo root:

```bash
make install-service
```

That copies `deploy/homehub.service` into place, enables it so HomeHub starts
on every boot and restarts itself if it ever exits, starts it now, and links
`homehubctl` into `/usr/local/bin`. From then on:

```bash
homehubctl start      # or stop, restart, status
homehubctl logs       # follow the journal
homehubctl deploy     # make build + restart: the whole update loop
```

Plain `systemctl start|stop|restart|status homehub` works too; the wrapper is
the same calls with the repo path filled in. Adjust the unit's
`HOMEHUB_DATABASE_URL` and paths for your install before the first run.

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

Requirements: Go 1.25+, Node 20.19+, PostgreSQL 17; JDK 21 for the
Minecraft companion jars (`make build-companion` — the Gradle wrapper fetches
Gradle, and the 26.x targets' Java 25 arrives through the toolchain resolver).

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
