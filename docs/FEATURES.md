# HomeHub — Feature Reference

A complete inventory of what HomeHub does. The [README](../README.md) is the
short pitch and the install guide; this document is the long version.

HomeHub is a single, self-hosted control plane for a homelab. Every connected
system is driven natively through its own API and rendered in one coherent
interface — no iframes, no link farms, no per-system tab sprawl.

## Conventions that hold everywhere

Six things are true across the entire app, so the per-feature sections below
don't repeat them:

- **Everything is configured at runtime, in the app.** Integrations, users,
  roles, channels, backups, schedules — all of it. Environment variables exist
  only for bootstrap concerns, and every one has a working default.
- **Multiple instances of every integration type** are supported. Two Proxmox
  clusters, five Docker hosts, three DNS filters — each is its own instance
  with its own permissions.
- **Test before save.** Every integration form tests the connection and reports
  a specific failure ("TLS certificate not trusted — enable skip verification
  or pin the fingerprint"), not "connection failed".
- **Live health supervision.** A connected system is polled and watched; status
  transitions (connected → degraded → unreachable) drive the UI, notifications,
  and signals.
- **Permissions are enforced server-side.** A user without access to a resource
  doesn't see it disabled — it doesn't appear at all, in navigation, in search,
  in notifications, or in API responses.
- **Real-time updates ride one multiplexed WebSocket.** Container events, VM
  status, console output, sensor pushes, print progress, and job progress all
  arrive over the same connection, filtered by permission.

---

# Part 1 — Connected systems

## Proxmox VE

Cluster and node management, top to bottom.

**Navigation.** A Proxmox-style resource tree sits beside the content:
datacenter → nodes → their guests and storage pools. Containers list before
VMs, each by VMID, exactly as Server View orders them, with CT/VM chips, live
status dots, and per-node running/total counts. A filter box matches VMID,
name, kind, node, or storage name. Node and storage rows scroll their target
into view on the destination tab rather than dropping you at the top.

**Overview.** Cluster identity and quorum, per-node CPU and memory with
capacity-toned bars, fleet aggregates weighted by core count, and per-node
history charts fed by the metrics engine.

**Guests.** Full VM and LXC lifecycle — start, shutdown, reboot, stop — plus
migration between nodes, cloning, and deletion. Bulk actions across a filtered
selection. Per-guest detail covers live status, the QEMU guest agent (network
interfaces, primary IPv4), raw hardware editing, and snapshots with create,
roll back, and delete.

**Backups.** On-demand vzdump plus scheduled backup jobs, with the job list
managed in-app.

**Storage and tasks.** Every pool with capacity, type, and content classes,
grouped by node with cluster-wide shared storage deduplicated into its own
section. The task list carries per-task logs.

**Consoles.** Three native in-app terminals per guest:

- **noVNC display** — the guest's own graphical console, straight from Proxmox,
  no guest credentials needed.
- **Serial terminal** — the pve-xtermjs serial console, with the keepalive
  pings Proxmox's termproxy expects.
- **SSH shell into the guest OS** — either a host from the SSH registry or
  credentials stored for just that guest, encrypted at rest.

## Docker

Multiple hosts over the local socket, TCP with mutual TLS, or SSH (with the SSH
registry or inline credentials).

Unified container list across hosts with live updates from the Docker event
stream; lifecycle actions; live log streaming; interactive in-container shells;
per-container CPU and memory history. Image management with registry update
detection and private registry credentials. Volumes and networks. Compose-stack
views with pull-and-recreate redeploys.

## DNS filtering — Pi-hole and AdGuard Home

Pi-hole v5 and v6 (autodetected) and AdGuard Home normalized onto one surface:
query rates and block percentage with history, the live query log, blocklist
and custom-rule management, client and group management, and timed protection
disable with a countdown.

Cross-instance rule comparison with one-way sync, so a second filter can be
brought in line with the first.

## Home Assistant

Native WebSocket integration with instant state push — no polling lag.

An area-grouped dashboard with real controls per domain: lights with
brightness, climate, covers, media players, locks, scenes, and scripts.
Automations list with manual trigger and enable/disable. Sensor history is
recorded into HomeHub's own metrics engine, so Home Assistant sensors graph on
the History page and can drive alert rules.

Permissions scope down to a single entity or a whole domain.

## Databases — MySQL, MariaDB, MongoDB

One module over three engines.

Live server stats and history (connections, query and op rates). A database and
table/collection browser with a full data editor: paginated rows, per-column
filters, add / edit / delete, truncate and drop behind confirmation, and
MongoDB document insert / replace / delete. An audited SQL console. Live
sessions with kill. Dumps to any configured backup target.

Permissions scope to a single database.

## TrueNAS

Pools with capacity and health, datasets and zvols, disks with type and SMART
temperature, SMB / NFS / iSCSI shares, snapshots, replication, and system
alerts — with capacity and temperature history recorded for graphing.

Permissions scope to a single pool.

## Minecraft

Servers connect through the **HomeHub Companion plugin** — Paper/Spigot, with
Velocity, Fabric, and Forge builds — downloaded from inside the app and paired
with a 6-digit code. The server dials outward, so nothing needs opening on the
Minecraft side, and the companion reconnects on its own.

Game versions: one Paper/Spigot jar covers 1.13 through 26.x, and one Velocity
jar covers 3.3 through 4.x. Fabric and Forge get one jar per version family,
because an intermediary-mapped mod only runs on the versions it was remapped
against, Forge's event bus and Mojang's names both shifted inside 1.21, and
the 26.x line dropped obfuscation altogether. Fabric: 1.20 – 1.20.1,
1.20.2 – 1.20.4, 1.21 – 1.21.1, 1.21.2 – 1.21.4, 1.21.5 – 1.21.8,
1.21.9 – 1.21.10, 1.21.11, 26.1.x, 26.2.x. Forge: 1.21 – 1.21.1,
1.21.2 – 1.21.4, 1.21.5, 1.21.6 – 1.21.8, 1.21.9 – 1.21.10, 1.21.11, 26.1.x,
26.2.x. The setup page shows the versions as a picker.

A network holds a proxy and its subservers under one roof. Live console with
scrollback all the way back to server start, instant command execution, the
online player list, and TPS / tick-time / heap history. Per-server offline
detection raises a signal when a subserver drops.

Optional SSH host access — from the SSH registry or inline — enables full
restarts of the machine behind the network.

## AMP (CubeCoders)

Every instance with live state, players, CPU and memory, a full console with
command dispatch, and both application and instance lifecycle control.

## Caddy

A Caddy reverse proxy managed through the `caddy-reader` helper installed on
its host. Every site in the Caddyfile listed live, with graphical add / edit /
remove — and **Caddy itself validates each change before anything touches
disk**. The running config's served domains are shown alongside.

Optionally linked to a Cloudflare zone, so a site's DNS record and its proxy
config are managed together.

## Tailscale

The whole tailnet: every device with connectivity, versions, and subnet routes;
device authorization and key expiry management; auth key creation; and tailnet
DNS settings.

## Aperture NVR

Every camera live in a grid with health pills, recording coverage, storage and
retention, and throughput. **Video streams directly from Aperture to the
browser — never proxied through HomeHub.** A camera pops out large for a closer
look. Live-picture health and recording health are reported separately, since a
perfectly live camera that isn't recording is a different problem.

## Moonraker / Klipper 3D printers

A Fluidd-style dashboard that **discovers whatever the printer exposes** —
every heater, temperature sensor, fan, LED, output pin, and macro — so any
Klipper machine gets a correct board rather than a hard-coded one.

Print progress and control, temperature setting with a live thermals graph,
motion and jog controls, extruder control, macro buttons, a camera panel, a
G-code console, emergency stop, and firmware restart.

## PatchPilot — fleet update management

An **agentless** update manager for a Linux fleet. Onboarding runs over SSH and
installs a dedicated least-privilege account — nothing else lands on the host.

Pending updates per host, patch on demand or on a schedule, live logs while a
run executes, run history with per-host outcomes, and a critical-updates view
that surfaces security patches ahead of the rest. **It never reboots a host on
its own.**

Hosts come from the **SSH host registry**: pick an entry and its address and
stored key or password do the one-time setup, with a sudo password asked for
only when that login cannot become root by itself. A bound host follows its
registry entry from then on — change the address there and the next run dials
the new one — while updates keep running as the dedicated least-privilege
account with the fleet key. Entering a host by hand remains the fallback for
machines outside the registry, and an existing host can be bound later. **Edit**
on any host reopens the same form as first-time setup — registry entry or
manual address and port, name, auto-reboot — with the pinned host key kept, so
a new address must still be the same machine. **Set up again** re-runs the
bootstrap on a known host over that credential: the
account, the fleet key with its source-address pin, and the sudoers rule are
re-established after HomeHub's own address changed or a host was reinstalled,
with the pinned host key verified rather than replaced.

A fleet carries one signal about its **last run**: when the most recent run
failed, the signal names the hosts and carries their errors and log tails;
it flips to a green "last run succeeded" when the next run is clean, and a
later failure surfaces again even after a dismissal. It sits at the top of
the PatchPilot page with a Dismiss button, and dismissing it there clears it
from the Signals page and the overview too. A failed run no longer marks the
fleet degraded — that was the same fault reported twice.

Aborting a run stops HomeHub waiting; it never kills the package manager on
the host, because interrupting dpkg mid-configure is the one way to leave a
machine worse than it was. The host is recorded as cancelled, not failed, and
apt in every later run waits up to ten minutes for a lock still held by that
command or by unattended-upgrades instead of failing on the spot. A run that
was in flight when HomeHub itself restarted is closed out at startup. While a
run is going, any host in it can be **skipped** from the run monitor: a queued
host is never started, a host being worked on has HomeHub stop waiting for it,
and the run moves on to the next host either way.

---

# Part 2 — The platform

## Overview

The home dashboard: HomeHub's own vitals up top, then every connected system's
headline number, each card linking into its module. Cards can be pinned (sort
first) or hidden, per user, stored server-side. Everything rides the same
permission-filtered endpoints, so a viewer sees only their slice.

## System health

The state of HomeHub itself: version, uptime, database reachability, size and
query latency, background job workers with pending / running / failed-in-24h
counts, live WebSocket client count, and per-integration health.

## Signals

The global stream of everything HomeHub's monitors currently see across every
system — failures, warnings, notable info, and green "recovered on its own"
notes.

Collectors sweep each system and report observations; the engine reconciles
them into signals that persist, escalate, recur (with an occurrence counter),
or auto-resolve when the condition clears. Signals group by severity or by
system, sort by recency or severity, and carry deep payloads — a failed update
run brings its per-host errors and log tails right into the list.

Dismissal is bulk-capable and asks for confirmation once anything worse than
info is involved. New and escalated warnings and criticals, plus recoveries,
push to Telegram — written by a small model in the house's own voice, with the
templated text as the fallback. Every transition passes a deterministic gate
first: repeats inside thirty minutes are dropped unless they got worse, an
all-clear waits ten quiet minutes and is withdrawn if the fault returns, and
nothing is announced as recovered that was never reported down. The narrator
also sees what was sent in the last half hour, so one fault reported under two
names goes out once.

## History and metrics

Every integration feeds a metrics engine. Raw samples roll into 5-minute and
1-hour tiers, each with its own configurable retention, so the 1h and 6h views
read raw data, 24h and 7d read 5-minute averages, and 30d and beyond read
hourly.

The History page graphs any recorded series across any systems on one timeline.

## Alerting

Threshold rules over any recorded metric, with sustained-for windows so a
single spike doesn't page anyone. Firing and resolved notifications are
edge-triggered. Delivery goes to any configured channel, and per-user routing
decides which events reach which channels.

## Notifications and outbound channels

In-app notifications with unread state and mark-all-read, plus outbound
channels shared by alerts and system events:

**Webhook** (with a configurable auth header) · **Email** (SMTP) · **ntfy** ·
**Gotify** · **Telegram** · **Discord**

Each channel is test-deliverable from its edit dialog. Users choose per event
type which channels they want, under their account.

## Calendar

Month and week views over every calendar, with a full event editor.

Four calendar kinds: **local** (stored in HomeHub), **CalDAV** (two-way),
**ICS feeds** (subscribed, read-only), and **trash pickup** schedules.
Recurring events support daily, weekly, biweekly, monthly, yearly, and custom
rules — and editing one asks how far the change should reach: this event only,
this and all following (splitting the series), or every occurrence.

Any calendar can be **published** as an iCalendar subscription: one long
random token in a URL (`/calendar/feed/<token>.ics`, no login — the link is
the key) that phones, Google or Apple Calendar, Thunderbird and Home
Assistant's remote calendar can subscribe to. Masters go out with their rules
and overrides, so the subscriber's client expands recurrence itself; the feed
answers 304 while nothing has changed. Rotate replaces the link, stop kills it.

## Utilities

A workbench with a persistent sidebar: the big apps pinned on top, then
categorized small tools. Everything under `/tools` renders in the same frame.

### Workspace (Files)

A simple personal cloud: folders, uploads of any file type from any device
(512 MiB per file), downloads back out. Text files open in the editor,
`.md` files open as rendered documents, binaries download. **The assistant can
read workspace files**, which is what makes it a workspace rather than a
dropbox.

### Text editor

Laid out like a desktop editor: a File menu (New / Open… / Open Recent / Save /
Save As… / Download), tabs, syntax highlighting, find, undo/redo, and a cursor
position readout. Text files are capped at 1 MiB — it stays a text editor.

### Markdown documents

Any workspace `.md` file renders as a full-page document at its own link, with
a table of contents that tracks scrolling. Write once, then read and share it
as a page.

### Converter

A universal file converter over a single source-of-truth format matrix,
batches included:

- **Images** — JPEG, PNG, WebP, HEIC, AVIF, TIFF, BMP, PSD, SVG, with PDF as
  an output
- **Camera raw** — CR2, CR3, NEF, ARW, DNG, ORF, RW2, RAF, PEF, SRW
- **Video** — MP4, MKV, MOV, AVI, WebM, M4V, MPEG, TS, WMV, FLV, 3GP
- **Audio** — MP3, WAV, FLAC, OGG, Opus, M4A, WMA, AIFF
- **Animation** — GIF, APNG
- **Tables** — CSV, TSV, XLSX, JSON, with Markdown and HTML as outputs
- **Documents** — Markdown, HTML, TXT, DOCX, ODT, RTF, EPUB (any to any)

Format detection sniffs content rather than trusting the extension. External
tools (ffmpeg, ImageMagick, libraw, pandoc) run privilege-dropped, killed as a
process group on timeout, and resource-limited — they parse untrusted uploads.

### Small tools

Twenty self-contained utilities, most of them running entirely in the browser:

| Network | Developer | Media | Everyday |
| --- | --- | --- | --- |
| QR code generator | JSON ↔ YAML | Bitrate calculator | Password generator |
| Subnet calculator | Cron builder | Timecode calculator | Base64 & URL |
| SSL certificate checker | Timestamp converter | EXIF viewer & stripper | Color converter |
| DNS lookup | Diff tool | | |
| Port check & ping | Hash & checksum | | |
| IP scanner | HTTP request tester | | |
| Wake-on-LAN | Regex tester | | |

The IP scanner sweeps a subnet for alive hosts with hostnames, MAC/vendor, and
open ports. Wake-on-LAN keeps saved targets. The EXIF tool strips GPS and
serial numbers as well as reading them.

## AI assistant

Chat over an LLM tool loop, against any **OpenAI-compatible provider** — a
cloud API or a model on your own hardware. A one-click LAN sweep finds LM
Studio endpoints and fills in the endpoint and loaded model.

**Every tool call runs under the signed-in user's own permissions**, so a
conversation can never surface more than its human is allowed to see. Each
assistant message wears chips naming the tools it used — the "how it knows"
trace.

**The tool surface.** Reads cover connected systems, alerts, metrics, signals,
calendars and their events, workspace files, and SSH hosts. Destructive writes
are never silent — deleting a calendar event, creating a calendar, dismissing
signals, and running an SSH command all return a pending action the user
confirms before anything happens. Creating a calendar event is the one write
that runs directly, on the grounds that adding an entry harms nothing.

**SSH tools** let the assistant run commands on registry hosts, gated by a
command classifier (`sshguard`) that fails closed. Read-only commands from an
allowlist may run unattended; anything that mutates state or isn't recognised
needs explicit approval; a set of catastrophic patterns (`rm -rf /`, `mkfs`,
`dd of=/dev/…`, fork bombs, `> /etc/passwd`, and friends) is refused outright
even with confirmation. It's defence in depth, not a sandbox — the real
boundary is the SSH user's own privileges.

The assistant is available as a full page and as a mini chat that floats above
everything else, including popped-out camera views.

### Telegram bridge

One bot (token from @BotFather, stored in the vault) long-polls `getUpdates` —
nothing inbound to expose. A chat links to a HomeHub user with a short-lived
code minted in the UI, and then talks to the assistant **under that user's own
permissions**.

Telegram and web conversations are fully separate threads, and the web thread
is scoped to one app session — closing the app resets it.

The Telegram thread and the alerts pushed into the chat are one system: every
sent alert is appended to the thread, stamped with when it went out, and kept
in a log the assistant can query with `sent_notifications` — with each
condition's standing right now — so "is that printer still down?" is answered
from the record, not from memory.

## SSH host registry

Every machine HomeHub can reach over SSH, entered once and reused everywhere.

A host carries a name, hostname, port, user, and either a password or a private
key. **Key creation is one click**: enter host and user, generate, and HomeHub
creates an ed25519 keypair, installs the public key into
`~/.ssh/authorized_keys` (idempotently), verifies the key works by reconnecting
with it, and stores the private key in the vault. Host keys are pinned on first
use.

Registry hosts are then selected — not retyped — wherever SSH is used: Proxmox
guest SSH consoles, Minecraft network access, Docker-over-SSH, assistant
commands, and ad-hoc terminals opened straight from the registry. Manual entry
remains available for one-offs.

Resolution is generic rather than per-integration, so a host edit applies on
the next reconnect, connection tests check the credential that will actually be
dialed, and a dangling binding fails closed instead of falling back to stale
inline credentials pointing at another machine.

Permissions scope to a single host, so a role can be granted one machine
without the rest.

## Backups

Scheduled application backups — a `pg_dump` of the full state — to **local
disk, SMB shares, NFS exports, or S3-compatible storage**. Targets are managed
and write-tested in the app, with per-target retention and optional age
encryption.

Archives are browsable, restore replays an archive and restarts cleanly, and
run history records every attempt. The encryption key is exportable (audited) —
store it offline.

The database dump never contains the master key, so a restored instance still
needs the original `master.key` to unlock stored credentials.

## Search and the command palette

`⌘K` / `Ctrl+K` opens a palette that jumps to any page or action, filtered by
what the user is allowed to reach.

---

# Part 3 — Administration

## Users and authentication

- Local accounts with **argon2id** password hashing.
- Per-IP and per-account rate limiting with escalating lockout on repeated
  failures.
- Optional **TOTP two-factor** with single-use recovery codes.
- **Passkeys** as either a passwordless primary or a second factor. (Passkeys
  need an HTTPS external URL configured, so they can be pinned to it.)
- **Administrators never choose user passwords.** Onboarding issues an invite
  link or a one-time temporary password that must be replaced at first sign-in.
- Sessions roll forward on activity and expire after a configurable idle
  lifetime.

## Roles and permissions

Resource-scoped roles, **deny by default**.

A grant applies to a resource node *and its subtree*, with four separate
levels — **view**, **control**, **configure**, **delete** — plus explicit deny.
Resource paths go as deep as the system does: a whole Proxmox cluster, one
guest on it, one Home Assistant entity, one database, one TrueNAS pool, one SSH
host.

Denied resources are hidden rather than disabled: navigation, search,
notifications, and API responses all filter server-side. Administrators can
preview the app as a role ("view as") to check what a user actually sees.

## Audit log

Every state-changing action is written to an append-only audit log — actor,
action, resource, outcome, and details — including denied attempts, which are
recorded as denials rather than dropped silently.

## Systems management

One page lists every connected system grouped by type, with health at a glance,
add / edit / test / remove, and a link into each module. Adding a system is a
guided form per type with test-before-save. **Reload** drops a system's live
connection and opens it again from the stored configuration with the reconnect
backoff reset — the way to get a stuck system moving without editing it, and
the same path an edit takes.

## Application settings

External URL (which also allows that origin behind a reverse proxy and pins
passkeys), session lifetime, and metric retention per tier.

---

# Part 4 — Under the hood

- **Secrets vault.** Every stored credential is envelope-encrypted with
  AES-GCM. The wrapping key lives in `HOMEHUB_MASTER_KEY` or
  `<data dir>/master.key` and is **never written to the database**. Secrets go
  in and never come back out over the API.
- **Background jobs.** A PostgreSQL-backed queue with a worker pool and a cron
  scheduler that turns schedule rows into queued jobs. Retries with backoff.
  Everything in-process — no external queue to operate.
- **Real-time.** One multiplexed WebSocket at `/api/ws`. Subscriptions are
  authorized against RBAC, including per-child scoping so a user with access to
  one guest doesn't receive events for its siblings.
- **API.** The web frontend consumes the same documented API a script could.
  `GET /api/openapi.json` serves the OpenAPI 3.0 document; `api/openapi.yaml`
  is the source of truth, and code generation derives both the Go server
  interfaces and the typed TypeScript client from it.
- **Database.** PostgreSQL, with migrations applied at startup.
- **Interface.** Light and dark themes. Full English and German translations,
  chosen per user. Responsive from phone to desktop.
- **Deployment.** A single Go binary with the frontend embedded, or a container
  image. `docker compose up -d` and a setup wizard is the whole install.
