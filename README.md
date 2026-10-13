<div align="center">

# HomeHub

**One self-hosted control plane for your whole homelab.**<br>
Proxmox, Docker, Home Assistant, TrueNAS, your DNS filters, your game servers, your cameras and your update schedule, driven natively through each system's own API and shown in one interface. No iframes, no link farm.

<p>
  <a href="LICENSE"><img alt="License: PolyForm Noncommercial" src="https://img.shields.io/badge/license-PolyForm%20Noncommercial-5b7cfa?style=flat-square"></a>
  <img alt="Version 0.1.0" src="https://img.shields.io/badge/version-0.1.0-2ea8a8?style=flat-square">
  <img alt="Go 1.25" src="https://img.shields.io/badge/go-1.25-00add8?style=flat-square">
  <img alt="English and German" src="https://img.shields.io/badge/ui-English%20%C2%B7%20Deutsch-6b7280?style=flat-square">
</p>

<img src="docs/images/overview.webp" alt="The HomeHub overview: platform vitals, live signals and a headline number for every connected system" width="900">

</div>

---

## Contents

[What it is](#what-it-is) · [Works with Aperture and has PatchPilot built in](#works-with-aperture-and-has-patchpilot-built-in) · [Connected systems](#connected-systems) · [The platform](#the-platform) · [Install](#install) · [First setup](#first-setup) · [Connecting Aperture](#connecting-aperture) · [Configuration](#configuration) · [Security model](#security-model) · [What it does not do](#what-it-does-not-do) · [Development](#development)

## What it is

Running a homelab usually means a browser tab per service, a different login for each, and no single place that tells you something is wrong. HomeHub replaces the tabs with one app:

- **Native, not embedded.** Every system is driven through its own API (REST, WebSocket, SSH) and rendered with HomeHub's own interface. Containers, VMs, DNS queries and camera tiles look and behave the same everywhere.
- **Configured in the app.** Add a system, press *Test*, and it tells you exactly what is wrong (a certificate it does not trust, a token with the wrong scope) before anything is saved. Environment variables only exist for bootstrap.
- **One timeline for everything.** Every system feeds the same metrics engine, so a Proxmox node's CPU, a pool's capacity and a Home Assistant sensor can sit on one chart and drive one alert rule.
- **Permissions that hide.** A role grants access down to a single container, VM, database, pool or entity. What you may not see does not appear, in navigation, search, notifications or API responses.
- **Small footprint.** One Go binary with the interface embedded, plus PostgreSQL. `docker compose up -d` is the whole install.

## Works with Aperture and has PatchPilot built in

HomeHub is the hub for two other projects of the same author.

<table>
<tr>
<td width="50%" valign="top">

### Aperture, the camera system

[**Aperture**](https://github.com/TheOriUHD/aperture) is a self-hosted NVR in a single Go binary. If you already run it, HomeHub shows it as a first-class system: a live grid of every camera next to the rest of your homelab, with recording health, storage and retention, and throughput on the overview.

- **Video goes straight from Aperture to your browser.** HomeHub never proxies the stream, so there is no extra latency and no extra load.
- Live-picture health and recording health are reported separately, because a live camera that is not recording is a different problem.
- Needs only a read-scoped API token. See [Connecting Aperture](#connecting-aperture).

</td>
<td width="50%" valign="top">

### PatchPilot, the update manager

[**PatchPilot**](https://github.com/TheOriUHD/patchpilot) is an agentless update orchestrator for a Linux fleet. HomeHub has the same idea built in, as a native module: you do not install anything extra.

- Onboard a host over SSH once; HomeHub creates a dedicated least-privilege account and leaves nothing else behind.
- Pending updates per host, patch on demand or on a schedule, live logs, run history, and a critical-host guard.
- Hosts come from the shared [SSH registry](#the-platform), and a failed run becomes a signal and a Telegram message.
- Prefer a terminal and no web stack? The standalone PatchPilot is the same approach as a single TUI.

</td>
</tr>
</table>

## Connected systems

Each module is complete or it is not shipped. Everything below can be added more than once (two Proxmox clusters, five Docker hosts, three DNS filters), each instance with its own permissions.

### Proxmox VE

Cluster and node management with a Proxmox-style resource tree beside the content. Full VM and LXC lifecycle, bulk actions, migration, cloning, snapshots, hardware editing, scheduled backups, storage and task views, and three native consoles per guest: noVNC display, serial terminal and an SSH shell into the guest.

<table>
<tr>
<td width="50%"><img src="docs/images/proxmox.webp" alt="Proxmox cluster overview with node CPU and memory graphs"></td>
<td width="50%"><img src="docs/images/proxmox-guest.webp" alt="A guest with live status, snapshots and console tabs"></td>
</tr>
</table>

### Docker

Several hosts over the local socket, TCP with mutual TLS, or SSH. One container list across hosts that updates live from the event stream, with lifecycle actions, streaming logs, in-container shells, per-container CPU and memory history, image update detection, volumes, networks and compose stacks with pull-and-recreate redeploys.

### Home Assistant

A native WebSocket integration with instant state push. An area-grouped dashboard with real controls per domain (lights with brightness, climate, covers, media, locks, scenes, scripts), automations you can trigger or toggle, and sensor history that lands in the metrics engine so it can be graphed and alerted on. Permissions scope down to a single entity or a whole domain.

### DNS filtering (Pi-hole and AdGuard Home)

Pi-hole v5 and v6 (detected automatically) and AdGuard Home on one surface: query rates and block percentage with history, the live query log, blocklists and custom rules, clients and groups, timed disabling with a countdown, and one-way rule sync to bring a second filter in line with the first.

### TrueNAS

Pools with capacity and health, datasets and zvols, disks with SMART temperature, SMB, NFS and iSCSI shares, snapshots, replication and system alerts, with capacity and temperature recorded for graphs. Permissions scope to a single pool.

<img src="docs/images/truenas.webp" alt="TrueNAS pools, disks and alerts" width="760">

### Databases (MySQL, MariaDB, MongoDB)

Live server stats and history, a database and table browser with an Adminer-style data editor, an audited SQL console, sessions you can kill, and dumps to any backup target. Permissions scope to a single database.

### Game servers (Minecraft and AMP)

**Minecraft** servers connect through the HomeHub Companion plugin (Paper and Spigot, plus Velocity, Fabric and Forge builds), downloaded from inside the app and paired with a six-digit code. The server dials out, so no port opens on the Minecraft side. You get a live console with scrollback to server start, instant commands, online players with full inventory view, and TPS and heap history.

**CubeCoders AMP** instances show live state, players, CPU and memory, a console with command dispatch, and application and instance lifecycle control.

<img src="docs/images/amp.webp" alt="AMP game servers with players, CPU and memory" width="760">

### Caddy and Tailscale

**Caddy** is managed through the small `caddy-reader` helper installed next to it: every site in the Caddyfile listed live, graphical add, edit and remove, and Caddy itself validates each change before anything touches disk. Optionally linked to a Cloudflare zone so a site's DNS record and proxy config are managed together.

**Tailscale** shows the whole tailnet: devices with connectivity, versions and subnet routes, device authorization and key expiry, auth keys and tailnet DNS.

<table>
<tr>
<td width="50%"><img src="docs/images/caddy.webp" alt="Caddy sites with upstreams and options"></td>
<td width="50%"><img src="docs/images/tailscale.webp" alt="Tailscale devices with routes and update status"></td>
</tr>
</table>

### Moonraker and Klipper 3D printers

A Fluidd-style dashboard that discovers whatever the printer exposes (every heater, sensor, fan, LED, pin and macro), so any Klipper machine gets a correct board: print progress and control, thermals graph, jog and extruder controls, macros, camera panel, G-code console, emergency stop and firmware restart.

### Aperture and PatchPilot

Described [above](#works-with-aperture-and-has-patchpilot-built-in) and in more detail below.

## PatchPilot, built in

An agentless update manager for the Linux machines you already have. Onboarding runs over SSH once and installs a dedicated least-privilege account with a minimal sudoers whitelist; nothing else lands on the host and nothing runs on it between updates.

<img src="docs/images/patchpilot-live-run.gif" alt="An Update-all run in HomeHub: hosts update one after another with their package-manager output streaming live" width="900">

- **See what is pending, per host.** Package manager, OS, pending count and reboot state at a glance, with a per-host detail page for checks, updates, reboots and settings.
- **Update now or on a schedule.** Cron-style schedules with an optional "never reboot" switch and the ability to skip a host mid-run.
- **Careful by design.** Runs are serialized. A host only reboots when you ask: the *Reboot* button, or the per-host auto-reboot switch you turn on yourself, and a schedule can forbid reboots outright. Hosts marked critical (a hypervisor, say) force a timed confirmation before any immediate run that includes them, with a one-click "skip critical".
- **Honest about failure.** A failed run becomes one signal that names the hosts and carries their errors and log tails; aborting a run stops HomeHub waiting without killing the package manager on the host.

<table>
<tr>
<td width="50%"><img src="docs/images/patchpilot-hosts.webp" alt="Fleet of seven hosts with pending updates and reboot state"></td>
<td width="50%"><img src="docs/images/patchpilot-confirm.webp" alt="The critical-host confirmation with a countdown and a skip-critical option"></td>
</tr>
<tr>
<td width="50%"><img src="docs/images/patchpilot-run.webp" alt="A finished run with per-host results and logs"></td>
<td width="50%"><img src="docs/images/patchpilot-add-host.webp" alt="Add-host dialog that picks a host from the SSH registry"></td>
</tr>
</table>

## The platform

### Overview and signals

The home page shows HomeHub's own vitals, then every connected system's headline number. **Signals** is one stream of everything the monitors currently see: failures, warnings, notable events and green "recovered on its own" notes, grouped by severity or system and carrying their deep payload.

<table>
<tr>
<td width="50%"><img src="docs/images/signals.webp" alt="Signals grouped by severity"></td>
<td width="50%"><img src="docs/images/system-health.webp" alt="System health: version, database, background jobs and per-integration state"></td>
</tr>
</table>

### History and alerting

Every integration feeds a metrics engine that rolls raw samples into 5-minute and hourly tiers with retention you set in the app. The **History** page graphs any series from any system on one timeline. **Alerts** are threshold rules over any metric with sustained-for windows, edge-triggered, delivered to webhook, email, ntfy, Gotify, Telegram or Discord, with per-user routing.

<table>
<tr>
<td width="50%"><img src="docs/images/history.webp" alt="Several CPU series compared on one chart"></td>
<td width="50%"><img src="docs/images/alerts.webp" alt="Alert rules with state and severity"></td>
</tr>
</table>

### Assistant and Telegram

Chat over any OpenAI-compatible provider, cloud or on your own hardware, that answers from live tools rather than from training data. **Every tool call runs under the asking user's own permissions**, each answer names the tools behind it, and destructive actions need confirmation. SSH commands additionally pass a classifier that fails closed and refuses catastrophic patterns outright. The same assistant is reachable from a Telegram chat linked to a HomeHub account, which also receives signals under a notification policy you set (what interrupts, quiet hours, digest times).

### Backups

Scheduled application backups to local disk, SMB shares, NFS exports or S3-compatible storage, with per-target retention and optional age encryption. Targets are write-tested in the app; restore replays an archive and restarts cleanly.

<img src="docs/images/backups.webp" alt="Backup targets, schedules and history" width="760">

### Calendar and utilities

Month and week views over local calendars, two-way CalDAV, subscribed ICS feeds and trash-pickup schedules; any calendar can be published as a subscription link. **Utilities** adds a personal workspace with a text editor and rendered Markdown, a universal file converter (images, camera raw, video, audio, tables, documents) and twenty small tools from subnet maths to EXIF stripping.

<table>
<tr>
<td width="50%"><img src="docs/images/calendar.webp" alt="Month view with recurring events"></td>
<td width="50%"><img src="docs/images/converter.webp" alt="The file converter"></td>
</tr>
</table>

### Users, roles and the SSH registry

Roles are resource-scoped and deny-by-default, with separate view, control, configure and delete levels and explicit deny. Administrators never choose a user's password: onboarding issues an invite link or a one-time temporary password. The **SSH registry** holds every machine HomeHub can reach, entered once and selected everywhere (guest consoles, Minecraft, Docker-over-SSH, PatchPilot, the assistant), with one-click key setup and pinned host keys.

<table>
<tr>
<td width="50%"><img src="docs/images/roles.webp" alt="Built-in and custom roles"></td>
<td width="50%"><img src="docs/images/ssh-hosts.webp" alt="The SSH host registry"></td>
</tr>
</table>

### Light, dark and small screens

The interface follows your theme, speaks English and German, and works down to phone width. `Ctrl+K` / `⌘K` opens a palette that jumps to any page or action you are allowed to reach.

<table>
<tr>
<td width="55%"><img src="docs/images/overview-light.webp" alt="The overview in the light theme"></td>
<td width="22%"><img src="docs/images/mobile-overview.webp" alt="The overview on a phone"></td>
<td width="22%"><img src="docs/images/mobile-patchpilot.webp" alt="PatchPilot on a phone"></td>
</tr>
</table>

> [!NOTE]
> Every screenshot and animation here comes from a demo installation: simulated Proxmox, TrueNAS, AMP, Caddy and Tailscale APIs, a pretend seven-host Linux fleet on a local test harness, and generated history. The application is real; the machines are not. No real address, credential or person appears in this repository.

## Install

You need Docker with the compose plugin. HomeHub builds its own image, which compiles the web interface and the Minecraft companion plugins, so the first build takes a few minutes.

```bash
git clone https://github.com/TheOriUHD/homehub.git
cd homehub
docker compose up -d
```

Open `http://<host>:8080`. The compose file starts HomeHub and a PostgreSQL 17 database, both with persistent volumes.

Useful overrides, set in your shell or an `.env` file next to `docker-compose.yml`:

| Variable | Default | Purpose |
|---|---|---|
| `POSTGRES_PASSWORD` | `homehub` | Database password. Change it for anything beyond a first look. |
| `HOMEHUB_PORT` | `8080` | Published port. |

Optional mounts in `docker-compose.yml`: the Docker socket (to manage the host's own Docker) and a host folder for local backup targets.

### Without Docker

Build the single binary and run it against your own PostgreSQL 17:

```bash
make build                     # needs Go 1.25+, Node 20.19+ and JDK 21
HOMEHUB_DATABASE_URL='postgres://homehub:...@127.0.0.1:5432/homehub?sslmode=disable' ./bin/homehub
```

To run it as a service that starts on boot and restarts itself, use the unit in `deploy/homehub.service` or:

```bash
make install-service
homehubctl status | logs | restart
```

## First setup

1. **Create the administrator.** On first open a short wizard asks for a username, a password of at least twelve characters and a display name. You are signed in afterwards.
2. **Back up `master.key`.** Everything HomeHub stores for you (API tokens, SSH keys, passwords) is envelope-encrypted with a key kept in `<data dir>/master.key` and never written to the database. Copy it somewhere safe now; without it a restored database cannot unlock its secrets.
3. **Set the external URL.** *Settings* takes the address people use to reach HomeHub. It is used for invite links and passkeys (passkeys need HTTPS).
4. **Add your first system.** *Systems* → *Add system*, pick a type and fill in the address and a token. *Test* before saving tells you what is wrong in plain words.
5. **Add people.** *Users* → *Create user* issues an invite link or a one-time password. Give them a built-in role (*Owner*, *Operator*, *Viewer*) or build one that grants a single container.
6. **Turn on backups and alerts.** Add a backup target and a schedule, then a channel (ntfy, Telegram, email, webhook) and an alert rule or two.

## Connecting Aperture

1. In Aperture, open *Settings → Integrations → API tokens* and create a token with the scopes `media:live`, `system:read`, `storage:read` and `cameras:read`. Nothing writable is needed.
2. In HomeHub, *Systems* → *Add system* → *Aperture*. Enter the address of your Aperture (for example `https://aperture.example.com`) and the token, then press *Test*.
3. The camera grid appears under *Aperture* in the navigation.

How it works, for the curious: Aperture has no CORS support by design, so the JSON reads (health, storage, camera list) go through HomeHub's backend. Only the live-video WebSocket connects **from your browser directly to Aperture**, authorized by short-lived stream tokens that HomeHub mints. Make sure the browser can reach Aperture's address, and that it uses the same scheme as HomeHub (an HTTPS HomeHub needs an HTTPS Aperture, or the browser blocks the stream as mixed content).

## Configuration

HomeHub is configured at runtime, in the app. Environment variables exist only for bootstrap concerns and every one has a working default:

| Variable | Default | Purpose |
|---|---|---|
| `HOMEHUB_DATA_DIR` | `/data` (image), `./data` (binary) | Key file and local working files |
| `HOMEHUB_DATABASE_URL` | local PostgreSQL | PostgreSQL connection string |
| `HOMEHUB_PORT` | `8080` | Listen port (`HOMEHUB_ADDR` takes a full address) |
| `HOMEHUB_MASTER_KEY` | generated key file | Base64 32-byte secret-wrapping key |
| `HOMEHUB_TRUSTED_PROXIES` | none | Reverse proxies whose `X-Forwarded-For` may be believed |
| `HOMEHUB_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |

Behind a reverse proxy, set the external URL in *Settings* and list the proxy in `HOMEHUB_TRUSTED_PROXIES`, otherwise rate limiting and the audit log see the proxy's address.

### Minecraft item icons

Mojang's textures are not part of this repository. To get item icons in the Minecraft inventory view, run once on the server:

```bash
docker compose exec app homehub minecraft-textures        # or: homehub minecraft-textures
```

This downloads the official client from Mojang's own servers, verifies its published checksum and keeps the few images the interface needs in the data directory. Until then the inventory view falls back to text labels.

## Security model

- Local accounts with argon2id hashing, per-IP and per-account rate limiting and escalating lockout.
- Optional TOTP two-factor with single-use recovery codes; passkeys as a passwordless primary or a second factor.
- Administrators never choose user passwords.
- Resource-scoped roles, deny by default; denied resources are filtered out server-side everywhere.
- Every state-changing action, including denied attempts, is written to an append-only audit log.
- Stored credentials are envelope-encrypted with AES-GCM and never returned over the API.
- Fetched content and conversions run privilege-dropped, resource-limited and with network isolation where they parse untrusted input.
- SSH host keys are pinned on first use; a changed key is refused until you accept it.
- Assistant actions that change state need confirmation, and shell commands pass a fail-closed classifier.

The OpenAPI document served at `/api/openapi.json` describes the same API the interface uses.

## What it does not do

- **No single sign-on.** Accounts are local; there is no OIDC, SAML or LDAP.
- **One instance, one database.** There is no clustering or high-availability mode.
- **No prebuilt images or binaries yet.** You build from source (the compose file does it for you).
- **Not every integration is demonstrated here.** The screenshots cover the systems that have a simulator; Docker, Home Assistant, DNS, databases, Minecraft and Moonraker are described from the feature reference and tested against live systems, not shown.
- **Some tools need host packages.** The converter uses ffmpeg, ImageMagick, libraw and pandoc when present and tells you which is missing.
- **Platform scope.** HomeHub reads and operates systems; it does not replace their own configuration UIs for deep changes.

## Development

Requirements: Go 1.25+, Node 20.19+, PostgreSQL 17, and JDK 21 for the Minecraft companion jars.

```bash
make dev-api      # API on :8080
make dev-web      # frontend on :5173, proxying /api
make test         # go test ./... and the TypeScript typecheck
```

`api/openapi.yaml` is the source of truth for the HTTP API; `make generate` regenerates the Go server interfaces and the typed TypeScript client from it. Each integration has a simulator under `cmd/` (`pvesim`, `truenassim`, `ampsim`, `caddysim`, `tailscalesim`, `aperturesim`) that the test suite and the demo use.

The full feature inventory lives in [docs/FEATURES.md](docs/FEATURES.md).

## License

HomeHub is source-available under the [PolyForm Noncommercial License 1.0.0](LICENSE). You may use, modify and redistribute it for any noncommercial purpose, including personal homelabs, hobby projects, education and research, as long as the license and the required notice stay with it. Commercial use needs a separate agreement.

Third-party components and their licenses are listed in [THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md).
