# patchpilot — Integration Specification

**Audience:** an engineer or AI agent who has never seen this codebase and needs to
integrate patchpilot's functionality into a different system — either by calling the
daemon, by importing its Go packages, or by re-implementing its behaviour in another
language.

**Source of truth:** this document describes the code as it exists at commit `c786ee8`
(release tag `v0.0.24`). Every claim below is traceable to a named file. Where the
shipped `README.md` and the code disagree, this document follows the code and says so.

---

## 1. Intent

patchpilot answers one question: *"How does one person keep 5–50 heterogeneous homelab
machines patched, on a schedule, without installing an agent on any of them and without
running a web platform?"*

The design commitments that follow from that, and which an integrator must preserve or
consciously discard:

| Commitment | Consequence |
| --- | --- |
| **Agentless** | Managed hosts get an SSH account and a sudoers file. Nothing else is installed. No daemon, no polling client, no callback. |
| **Single orchestrator owns all state** | One SQLite database on one machine. No clustering, no distributed locking, no remote state. |
| **Runs are serialized by default** | At most one update run executes at a time, process-wide. This is a safety property, not a performance oversight. |
| **Least privilege on targets** | The remote `patchpilot` account may run *exactly* the package-manager and reboot binaries, by absolute path, and nothing else. |
| **Never reboot without being told** | The daemon never reboots a host on its own. Reboot is always an explicit, separate action. |
| **Local-only control plane** | The API is a Unix socket. There is no network listener, no authentication, no TLS. |

If your target system violates one of these (e.g. you want a multi-tenant web service),
you are not integrating patchpilot — you are harvesting its algorithms. Section 14 tells
you which parts are worth harvesting.

---

## 2. Vocabulary

Learn these five nouns; the entire codebase is built from them.

- **Host** — one managed machine. Has a `host_type` that decides *how* it is updated.
- **Schedule** — a cron-like recurrence plus an ordered set of hosts.
- **Run** — one execution attempt. Contains one **HostRun** per host.
- **HostRun** — one host's slice of a run: status, exit code, package count, log file.
- **Bootstrap session** — a short-lived, streaming onboarding workflow for a new Standard
  host. Produces a committable host record; is not itself persisted.

Four host types (`internal/core/host.go:8`):

| `host_type` | Transport | Update mechanism |
| --- | --- | --- |
| `standard` | SSH + key | Package manager (`apt`/`dnf`/`pacman`/`zypper`/`apk`) |
| `truenas` | HTTPS REST | TrueNAS SCALE `/api/v2.0` |
| `haos` | HTTPS REST | Home Assistant `/api/hassio` Supervisor proxy |
| `self` | local | `systemd-run` transient unit (updates the orchestrator itself) |

---

## 3. Process and Deployment Model

One statically-linked Go binary, two everyday modes.

```
patchpilot daemon          patchpilot tui  /  patchpilot host|schedule
      │                                  │
      │  owns:                           │  owns: nothing
      │   • SQLite (WAL)                 │
      │   • gocron scheduler             │
      │   • run executor                 │
      │   • SSH keypair                  │
      │   • secret key                   │
      │   • event hub + log tailer       │
      └──── HTTP over Unix socket ───────┘
            /run/patchpilot/patchpilot.sock  (mode 0660)
```

- Entrypoint: `cmd/patchpilot/main.go` → `internal/cli/cli.go:26` dispatches
  `tui | daemon | host | schedule | help`. **No argument at all starts the TUI.**
- Daemon wiring — read this first, it is the dependency graph in 70 lines:
  `internal/daemon/daemon.go:26`.
- SQLite via `modernc.org/sqlite` (pure Go, **no CGO**). Pragmas at
  `internal/store/sqlite.go:99`: `foreign_keys=ON`, `busy_timeout=5000`,
  `journal_mode=WAL`, `synchronous=NORMAL`.
- systemd unit `deploy/patchpilot.service`: `Type=notify` (the daemon calls `sd_notify`
  itself in `internal/systemd/notify.go` — there is no libsystemd dependency), runs as
  user `patchpilot`, `ProtectSystem=strict` with an explicit `ReadWritePaths` allowlist,
  `Restart=on-failure` with `StartLimitBurst=3` in 60s so a crash-loop ends in a visible
  `failed` state.

### Filesystem contract

Defaults are compiled per-OS (`internal/config/defaults_linux.go`,
`defaults_other.go`); YAML at `ConfigPath` overrides them; environment variables
override the YAML (`internal/config/config.go:36`).

| Purpose | Linux default | Env override |
| --- | --- | --- |
| config file | `/etc/patchpilot/config.yaml` | `PATCHPILOT_CONFIG` |
| data dir | `/var/lib/patchpilot` | `PATCHPILOT_DATA_DIR` |
| database | `/var/lib/patchpilot/patchpilot.db` | `PATCHPILOT_DB_PATH` |
| socket | `/run/patchpilot/patchpilot.sock` | `PATCHPILOT_SOCKET_PATH` |
| daemon log | `/var/log/patchpilot/daemon.log` | `PATCHPILOT_LOG_PATH` |
| secret key | `/etc/patchpilot/secret.key` | `PATCHPILOT_SECRET_KEY_PATH` |

Derived, not configurable:

- **Orchestrator SSH keypair**: `<DataDir>/ssh/id_ed25519{,.pub}` (`internal/ssh/keys.go:129`).
- **Per-host run logs**: `<dirname(LogPath)>/runs/run-<runID>/<hostID>.log`
  (`internal/store/runs.go:49`). These paths are written into `host_runs.log_path` at run
  creation; the executor and the SSE tailer both read that column.
- **Self-update logs**: `<dirname(LogPath)>/self-update-<runID>.log`.

**Clients** resolve the socket differently and deliberately never read the daemon config:
`PATCHPILOT_SOCKET`, else `/run/patchpilot/patchpilot.sock` on Linux, else
`~/.patchpilot/patchpilot.sock` (`internal/config/config.go:79`). Note the client env var
is `PATCHPILOT_SOCKET`, *not* `PATCHPILOT_SOCKET_PATH` — mixing them up is the single most
common "TUI cannot connect" cause.

---

## 4. Data Model

Schema lives in `migrations/*.sql`, embedded via `go:embed` (`migrations/embed.go`) and
applied at startup by `internal/store/migrate.go`, which tracks applied versions in a
`schema_migrations` table. Migrations are numbered and append-only.

```sql
hosts(id, name UNIQUE, hostname, ssh_port=22, ssh_user, host_type, package_manager,
      os_pretty, arch, is_proxmox_host, is_self, api_token, api_token_nonce,
      api_username, enabled, auto_reboot, auto_reboot_countdown_seconds,
      notes, last_seen_at, created_at, updated_at)

schedules(id, name UNIQUE, frequency, minute, hour, day_of_week, day_of_month,
          enabled, max_concurrent, suppress_reboot, last_run_at, next_run_at,
          last_skipped_at, last_skip_reason, created_at, updated_at)

schedule_hosts(schedule_id, host_id, order_index)          -- PK(schedule_id, host_id)

runs(id, trigger, schedule_id, status, started_at, finished_at, summary)

host_runs(id, run_id, host_id, status, exit_code, started_at, finished_at,
          log_path, packages_upgraded_count, reboot_required, error_summary)

settings(key, value)                                        -- string KV
audit_events(id, occurred_at, actor, action, target_type, target_id, details, source_ip)
```

Enforced by `CHECK` constraints (trust these, they are the enum definitions):

- `host_type ∈ {standard, truenas, haos, self}`
- `frequency ∈ {hourly, daily, weekly, monthly}`
- `runs.trigger ∈ {manual, scheduled, cli}`
- `runs.status ∈ {queued, running, success, partial_failure, failed, cancelled}`
- `host_runs.status ∈ {pending, running, success, failed, skipped}`
- `max_concurrent BETWEEN 1 AND 10`
- `auto_reboot_countdown_seconds BETWEEN 5 AND 3600`

Two `AFTER UPDATE` triggers maintain `hosts.updated_at` and `schedules.updated_at`.
`ON DELETE CASCADE` on `schedule_hosts` and `host_runs`; `runs.schedule_id` is
`ON DELETE SET NULL` so run history survives schedule deletion.

**Crash recovery.** On every daemon start, `RecoverOrphanedRuns`
(`internal/store/sqlite.go:55`) flips any `running` run and its `pending`/`running` host
runs to `failed`. Queued runs are deliberately preserved and resumed. If you re-implement
this, do not skip it: without it a mid-run crash leaves a permanently "active" run that
blocks every future run forever.

---

## 5. The Daemon API

Transport: plain HTTP/1.1 over a Unix stream socket. Router: chi
(`internal/ipc/router.go`). Server: `internal/ipc/server.go`. Reference client:
`internal/ipc/client.go` — a plain `http.Client` with a `DialContext` that dials the
socket; the URL host is ignored.

**There is no authentication.** Access control is filesystem permissions on the socket
(0660, owned by the daemon user/group). Anyone who can open the socket has full control
including "run arbitrary package manager as root on every managed host". Any integration
that exposes this API beyond the local machine must add its own authentication and
authorization layer, and should treat that as a hard requirement, not a nicety.

Errors: non-2xx responses carry `{"error": "<message>"}` (`handlers/common.go:28`).
Success bodies are the JSON encoding of the Go types named below.

### 5.1 Health

| Method | Path | Response |
| --- | --- | --- |
| GET | `/api/health` | `{status, version, commit, build_date, started_at}` |

### 5.2 Hosts (`internal/ipc/handlers/hosts.go`)

| Method | Path | Body → Response | Notes |
| --- | --- | --- | --- |
| GET | `/api/hosts/` | → `[]Host` | |
| GET | `/api/hosts/{id}` | → `Host` | 404 if absent |
| PUT | `/api/hosts/{id}` | `HostUpdate` → `Host` | mutable fields only: name, enabled, auto_reboot, delay, notes |
| POST | `/api/hosts/{id}/toggle` | → `Host` | flips `enabled` |
| DELETE | `/api/hosts/{id}` | → `{status:"ok"}` | |
| POST | `/api/hosts/{id}/dry-run` | → `DryRunResult` | connects live, does not mutate the host |
| POST | `/api/hosts/{id}/api-credential` | `{api_token, api_username}` → `Host` | encrypts and stores; token never returned |
| POST | `/api/hosts/{id}/run` | → `RunDetail` (202) | creates or queues a single-host run |
| POST | `/api/hosts/{id}/reboot` | → `{status:"ok"}` | Standard non-self hosts only |
| POST | `/api/hosts/{id}/repair-sudoers` | → `{status:"ok"}` | rewrites `/etc/sudoers.d/patchpilot` with the current whitelist |

**Concurrency guard.** `PUT`, `toggle`, `DELETE`, `api-credential`, and `repair-sudoers`
all call `rejectHostMutationWhenInFlight` (`hosts.go:187`) and return **409** if that host
has a `pending` or `running` host_run. `dry-run`, `run`, and `reboot` do not.

### 5.3 Onboarding wizard (`internal/ipc/handlers/wizard.go`)

Note the doubled segment in the API-host routes — it is `/api/wizard/api/...`, a
consequence of nesting `r.Post("/api/verify")` inside `Route("/api/wizard")`.

| Method | Path | Purpose |
| --- | --- | --- |
| POST | `/api/wizard/standard/detect` | probe a host over SSH+password, return `StandardDetectResult` |
| POST | `/api/wizard/standard/root-assist` | log in as root, install sudo and/or add the user to the sudo group |
| POST | `/api/wizard/standard/bootstrap` | start async bootstrap, returns `{id}` (202) |
| GET | `/api/wizard/{id}/stream` | SSE progress for that session |
| POST | `/api/wizard/{id}/commit` | `CommitHostInput` → persisted `Host` (201) |
| POST | `/api/wizard/{id}/cancel` | cancel in-flight session, triggers rollback |
| POST | `/api/wizard/api/verify` | verify a TrueNAS/HAOS endpoint + token |
| POST | `/api/wizard/api/check` | update-availability check for TrueNAS/HAOS |
| POST | `/api/wizard/api/commit` | persist an API-managed host (201) |

Secrets in request bodies (`password`, `root_password`, `api_token`) are copied into
`[]byte`, and every handler `defer`s `core.ZeroBytes` on them (`core/secret.go`). The
`[]byte` secret fields on the Go structs are tagged `json:"-"` so they can never be echoed
back in a response.

### 5.4 Schedules (`internal/ipc/handlers/schedules.go`)

| Method | Path | Body → Response |
| --- | --- | --- |
| GET | `/api/schedules/` | → `[]Schedule` |
| POST | `/api/schedules/` | `ScheduleInput` → `Schedule` (201) |
| GET | `/api/schedules/{id}` | → `Schedule` |
| PUT | `/api/schedules/{id}` | `ScheduleInput` → `Schedule` |
| DELETE | `/api/schedules/{id}` | → `{status:"ok"}` |
| POST | `/api/schedules/{id}/run` | → `RunDetail` (202), trigger `manual` |

Create/update compute `next_run_at` server-side via `scheduler.NextRuns` and reject
invalid timing with 400. Any mutation fires an async `engine.Reload` so the cron table
matches the database.

### 5.5 Runs (`internal/ipc/handlers/runs.go`)

| Method | Path | Notes |
| --- | --- | --- |
| GET | `/api/runs/` | query: `limit` (≤100, default 100), `offset`, `status` (CSV), `trigger` (CSV), `range` ∈ `24h\|7d\|30d` |
| GET | `/api/runs/{id}` | → `RunDetail` |
| GET | `/api/runs/{id}/logs` | → `RunLogResponse`, capped at 2000 lines |
| POST | `/api/runs/{id}/cancel` | cancels active or drops queued |
| GET | `/api/runs/{id}/stream` | SSE: status events **merged with live log lines** |

### 5.6 Settings (`internal/ipc/handlers/settings.go`)

| Method | Path | Notes |
| --- | --- | --- |
| GET | `/api/settings/` | `SettingsSnapshot`: paths, concurrency flag, **orchestrator public key**, self-update logs, last 80 daemon log lines, build info |
| PUT | `/api/settings/concurrency` | `{concurrency_enabled}` → snapshot |
| POST | `/api/settings/ssh/rotate` | all-or-nothing key rotation, see §11 |
| POST | `/api/settings/inventory/export` | `{path}` — writes YAML **on the daemon host**, 0600 |
| POST | `/api/settings/inventory/import` | `{path, conflicts}` — reads YAML from the daemon host |

Backup/restore is path-based, not payload-based: the daemon reads and writes files on its
own filesystem. A remote integration must either ship files to the daemon host first or
replace these two endpoints.

---

## 6. Event Streams (SSE)

Two producers, one wire format.

**Hub** (`internal/stream/hub.go`) — in-memory, per-session, with **full history replay**:
`Subscribe` pre-loads every prior event into the new subscriber's buffered channel before
attaching it live. A late subscriber therefore misses nothing. `Publish` is
**non-blocking** — a subscriber whose buffer is full silently drops that event
(`hub.go:93`, `select { case ch <- event: default: }`). `Complete` closes all subscriber
channels, which is the stream's EOF.

**LogTailer** (`internal/stream/tailer.go`) — polls each `host_runs.log_path` every 350 ms,
but only while at least one subscriber is attached. No subscribers, no file I/O.

Wire format (`handlers/wizard.go:serveSSE`):

```
id: <monotonic per session>
event: <type>
data: <JSON>

```

| Stream | Event types | Payload |
| --- | --- | --- |
| `/api/wizard/{id}/stream` | `bootstrap.step` | `BootstrapEvent{step, status, message, code?, guidance?}` |
| | `bootstrap.error` | `{error, code, guidance}` |
| | `bootstrap.complete` | `BootstrapSummary{facts, dry_run, resolved_paths, sudoers_entry}` |
| `/api/runs/{id}/stream` | `run.event` | `RunEvent{run_id, host_run_id, host_id, host_name, run_status, host_status, message, reboot_required, occurred_at}` |
| | `log.line` | `LogLine{run_id, host_run_id, host_id, host_name, line, at}` |

The run stream merges N+1 channels (one hub + one tailer per host log) into a single SSE
response via `serveSSEMany` (`handlers/runs.go:138`). **Ordering between status events and
log lines is not guaranteed** — merge is by goroutine race, not timestamp. Consumers that
care must sort by `occurred_at` / `at` themselves.

Terminal conditions: `bootstrap.complete` (or `bootstrap.error`) followed by channel
close; for runs, a `run.event` whose `run_status` is one of
`success|partial_failure|failed|cancelled`.

---

## 7. Standard Host Onboarding — the Bootstrap Protocol

This is the most valuable and most battle-tested part of the codebase. The last ten
releases are almost entirely fixes here. If you port one thing, port this.

Files: `internal/core/detection.go`, `escalation.go`, `bootstrap.go`,
`bootstrap_errors.go`, `service.go`, `internal/pkgmgr/`.

### 7.1 Phase 1 — Detect

One SSH password login, **one shell round-trip**. The probe is a single constant,
`core.StandardDetectionProbe` (`detection.go:12`), emitting `---SECTION---` delimited
output that `ParseHostFacts` splits back into `HostFacts`:

```
---OS---              /etc/os-release
---KERNEL---          uname -a
---ARCH---            uname -m
---HOSTNAME---        hostnamectl || hostname
---PKGMGR---          command -v for apt-get/dnf/pacman/zypper/apk
---PROXMOX---         test -f /etc/pve/.version
---HAOS---            grep -i "Home Assistant" /etc/os-release
---TRUENAS---         grep -i truenas /etc/version
---SUDO---            command -v sudo
---SUDO_NPW---        sudo -n true
---SUDOERS_FILE---    test -f /etc/sudoers
---SUDOERS_DIR---     test -d /etc/sudoers.d
---SUDOERS_INCLUDE--- grep for @includedir /etc/sudoers.d
---SU---              command -v su
---CURRENT_UID---     id -u
---UPMGR_USER---      id patchpilot
```

Post-processing rules worth copying:

- The first package manager found wins, in the probe's own order (apt → dnf → pacman →
  zypper → apk).
- `apt` + Proxmox marker is rewritten to the distinct manager `apt_proxmox`.
- If the probe says HAOS or TrueNAS, Standard mode **refuses** with a message telling the
  operator to switch wizard modes (`StandardRefusal`).
- Missing package manager on a non-HAOS/non-TrueNAS host is a hard parse error.

Then `diagnosePrivilege` (`service.go:543`) decides whether bootstrap can
proceed and returns a `PermissionDiagnosis{kind, reason, path_forward, manual_fix[],
can_auto_fix, preinstall_ok}`. Kinds: `sudo_missing`, `sudo_denied`. Root or
passwordless-sudo returns `nil` (nothing to fix). This structure exists so the UI can say
*"here is exactly what is wrong and here are the three commands to fix it by hand"*
instead of showing a stack trace.

### 7.2 Phase 2 — Escalation strategy

`internal/core/escalation.go` picks one of four modes, in this priority order
(`chooseEscalationMode:56`):

| Mode | Condition | Command shape |
| --- | --- | --- |
| `direct-root` | `uid == 0` | `sh -c '<cmd>'` |
| `sudo-nopasswd` | `sudo -n true` succeeded | `sudo -n sh -c '<cmd>'` |
| `su-root` | forced, or sudo unusable and a root password was supplied | `su -c '<cmd>'` **with a PTY**, password on stdin |
| `sudo-password` | sudo present and an SSH password is known | `sudo -S -p '' sh -c '<cmd>'`, password on stdin |

Hard-won details you must replicate if you go the `su` route:

- `su` requires a **real PTY** (`RequestPty("xterm", 80, 24, ECHO:0)`) — it reads the
  password from the terminal, not from a pipe.
- The password must be written to stdin **after a delay** (`ptyStdinDelay = 400ms`,
  `internal/ssh/client.go:17`) or `su` misses it.
- The echoed `Password:` prompt must be stripped from captured stdout
  (`stripLeadingSuPasswordPrompts`).
- Under `su`, `PATH` is not what you expect. The batch script begins with an explicit
  `export PATH=/usr/sbin:/usr/bin:/sbin:/bin:$PATH` and every admin tool is invoked by
  absolute path.

### 7.3 Phase 3 — Absolute path resolution

Before generating any command, patchpilot resolves every needed executable to an absolute
path on the target, using a shell function that tries `command -v -p`, then `which`, then
canonical directories (`commandPathResolutionScript`, `bootstrap.go:~500`).

Two tiers:

- **Required** (`pkgmgr.RequiredCommands`) — missing one aborts bootstrap with a named
  error. Per manager: apt → `apt-get, apt, dpkg, test`; dnf → `dnf, needs-restarting,
  test`; pacman → `pacman, test`; zypper → `zypper, test`; apk → `apk, test`. Plus, for
  all of them, `systemctl, reboot, shutdown`.
- **Optional admin tools** — `id, install, chown, chmod, rm, useradd, usermod, groupadd,
  userdel, visudo, deluser` (+`adduser` on Alpine). `validateBootstrapAdminTools` then
  hard-fails if any *actually required* one didn't resolve to a `/`-prefixed path.

The point of this tier is that the generated sudoers file contains only absolute paths, so
the remote account cannot be tricked by a `PATH` shadow.

### 7.4 Phase 4 — The bootstrap steps

`BootstrapStandardWithRunner` (`bootstrap.go:134`). Two code paths that produce the same
end state.

**Path A — step-by-step** (root, sudo-nopasswd, or sudo-password). Each step is a separate
escalated command with its own progress event:

1. Privilege check (`true`)
2. If root *and* sudo missing → install sudo (`installSudoDirectRoot`)
3. Resolve command paths
4. Validate admin tools, generate commands
5. **Verify the sudoers subsystem** — `/etc/sudoers` must exist *and* must contain
   `@includedir /etc/sudoers.d`. If not, hard-fail with that exact string in the guidance.
   patchpilot deliberately refuses to edit `/etc/sudoers` itself.
6. Create user: `id patchpilot || useradd --system --create-home --shell /bin/sh patchpilot`
   (Alpine: `adduser -D -h /home/patchpilot -s /bin/sh patchpilot`)
7. `install -d -m 0700 -o patchpilot -g patchpilot /home/patchpilot/.ssh`
8. Write `authorized_keys`, `chown patchpilot:patchpilot`, `chmod 0600`
9. `install -d -m 0750 /etc/sudoers.d`
10. Write `/etc/sudoers.d/patchpilot` via heredoc, `chmod 0440`
11. `visudo -cf /etc/sudoers.d/patchpilot` — **on failure, immediately `rm -f` the drop-in**
    and report both the validation error and any rollback error

**Path B — one `su` batch** (`bootstrapStandardWithSu` / `suBootstrapBatch`). Same steps
concatenated into a single `set -eu` script with `patchpilot bootstrap: <step>` progress
markers, executed in one `su` invocation. Reason: each `su` call needs its own PTY and
password round-trip, so doing eleven of them is slow and fragile.

Then, back in `runBootstrapSession` (`service.go:411`), over a **new connection as
`patchpilot` using the key**:

12. Reconnect with retry/backoff `500ms, 1s, 1.5s, 2s, 3s` (six attempts total) — sshd
    needs a moment after the account is created
13. Verify NOPASSWD: `out=$(sudo -n <mgr> --version 2>&1); code=$?; printf '%s\n' "$out" | head -n 1; exit "$code"`
    — bounded output, preserved exit status
14. Dry run (see §8)
15. Zero the password buffers
16. Publish `bootstrap.complete`

The host is **not** in the database yet. `POST /api/wizard/{id}/commit` creates it, with
`ssh_user` forced to `patchpilot` and `host_type=standard`.

### 7.5 The installed artifacts

`authorized_keys` line (`hardenedAuthorizedKey`, `bootstrap.go:693`):

```
from="<orchestrator-ip>",no-pty,no-X11-forwarding,no-agent-forwarding,no-port-forwarding ssh-ed25519 AAAA... 
```

The source IP is discovered by opening a throwaway UDP socket toward the target and
reading the local address (`ResolveSourceIP`). If that fails the `from=` clause is omitted
rather than guessed.

`/etc/sudoers.d/patchpilot` (`internal/pkgmgr/sudoers.go`) — sorted, absolute, no
wildcards. For Debian:

```
patchpilot ALL=(ALL) NOPASSWD: /usr/bin/apt, /usr/bin/dpkg, /usr/bin/test, /usr/bin/apt-get, /usr/sbin/reboot, /usr/bin/systemctl reboot, /usr/sbin/shutdown -r now
```

Note that `systemctl` and `shutdown` are argument-constrained (`systemctl reboot`,
`shutdown -r now`); everything else is path-only.

### 7.6 Rollback

On failure or cancellation before `completed=true`, a deferred `RollbackBootstrap`
(`bootstrap.go:116`) removes, over the still-open privileged session:

1. `/etc/sudoers.d/patchpilot`
2. `/home/patchpilot/.ssh/authorized_keys`
3. the `patchpilot` user — **only if `facts.PatchpilotUserExists` was false**, i.e. only if
   this run created it

It never uninstalls sudo. Rollback errors are reported separately and never mask the
original classified error.

### 7.7 Failure classification

`internal/core/bootstrap_errors.go` defines `BootstrapError{Step, Code, Summary, Guidance,
Output, Cause}` and 21 codes:

```
unknown, ssh_connection, detection_probe, command_missing, admin_tool_missing,
public_key_invalid, sudo_install_failed, sudoers_missing, sudoers_include_missing,
user_creation_failed, ssh_directory_failed, authorized_keys_failed, sudoers_write_failed,
visudo_failed, patchpilot_reconnect_failed, nopasswd_verify_failed, dry_run_failed,
package_lock, network_or_repo, permission_denied, su_auth_failed
```

The design rule, from `V0.4_ROADMAP.md`: *no user-facing bootstrap failure may be a bare
`Process exited with status 1`.* Every failure carries a code, a human summary, and an
actionable `Guidance` string. The code and guidance travel to the UI inside the
`bootstrap.step` / `bootstrap.error` SSE payloads.

---

## 8. Package Manager Matrix

All in `internal/pkgmgr/manager.go`. `paths` is the resolved absolute-path map from §7.3.
Every command is prefixed `sudo -n` — non-interactive, so a missing NOPASSWD entry fails
loudly instead of hanging on a prompt.

| Manager | Update | Dry run | Reboot-required probe (exit 0 = reboot needed) |
| --- | --- | --- | --- |
| `apt`, `apt_proxmox` | `apt-get update && DEBIAN_FRONTEND=noninteractive apt-get -y -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold dist-upgrade && apt-get -y autoremove` | `apt-get update && apt-get -s dist-upgrade && apt list --upgradable` | `test -f /var/run/reboot-required`, else `needrestart -b -r l` |
| `dnf` | `dnf -y upgrade` | `dnf check-update` (**exit 100 = updates available, not an error**) | `needs-restarting -r` |
| `pacman` | `pacman -Syu --noconfirm` | `pacman -Sup` | compare `uname -r` to installed `linux` |
| `zypper` | `zypper -n update` | `zypper -n list-updates` | compare `uname -r` to `kernel-default` |
| `apk` | `apk update && apk upgrade` | `apk -s upgrade` | `/var/run/reboot-required` or kernel compare |

`ParseDryRunOutput` (`internal/pkgmgr/dryrun.go`) turns raw output into
`DryRunResult{packages_upgraded, packages_removed, download_size, reboot_required,
summary, raw_output}` with a per-manager line parser (apt counts `Inst `/`Remv ` lines,
apk uses a `name ver -> ver` regex, etc.). These parsers are heuristic and locale-sensitive
— treat the counts as advisory and `raw_output` as authoritative.

---

## 9. Run Execution Semantics

`internal/core/orchestrator.go`. This is the concurrency contract; get it wrong and you
get two `apt` processes fighting over `/var/lib/dpkg/lock`.

### 9.1 Admission — one active run, globally

`createOrQueue` (`orchestrator.go:235`) holds a mutex and checks for an active run
(in-memory `activeRunID`, falling back to a DB query). Then:

- **No active run** → insert with status `running`, register cancel func, `go executeRun`.
- **Active run + trigger is `manual` or `cli`** → insert with status `queued`. It will be
  promoted automatically when the current run finishes.
- **Active run + trigger is `scheduled`** → **skip, do not queue.** Record
  `schedules.last_skipped_at` / `last_skip_reason`, write an audit event, return
  `ErrScheduledOverlap`.

That asymmetry is intentional: a human asking twice means "do it twice"; a cron firing
during a long run means "you're already patching, don't stack".

`finishActive` → `promoteNextQueued` picks up the oldest queued run. `Orchestrator.Start`
also calls `promoteNextQueued` once at boot, so queued runs survive a daemon restart.

### 9.2 Ordering — five execution groups

`hostExecutionGroup` (`orchestrator.go:528`) and `orderHosts`:

| Group | Contents | Concurrency |
| --- | --- | --- |
| 0 | Standard Linux, non-Proxmox | parallel (if enabled) |
| 1 | TrueNAS | always sequential |
| 2 | HAOS | always sequential |
| 3 | Proxmox hosts | always sequential |
| 4 | self (the orchestrator) | always sequential, always last |

Groups execute strictly in order 0→4; group N+1 does not begin until group N is done. The
self host runs last so the orchestrator does not restart itself out from under an
in-flight run.

### 9.3 Concurrency — off by default, triple-gated

`maxConcurrentForRun` returns >1 only if **all** of: the run belongs to a schedule, the
global `experimental_concurrency_enabled` setting is true, and that schedule's
`max_concurrent` is ≥2 (clamped to 10). Manual and CLI runs are always sequential.
Implementation is a semaphore + WaitGroup over group 0 only.

### 9.4 Per-host execution

`executeHostRun` dispatches on type, then for Standard hosts:

1. `mkdir -p` the log dir, open the log `O_APPEND`, mode 0640
2. `Keys.Ensure` → connect as `host.ssh_user` with the key
3. Re-resolve absolute command paths (fresh, every run — the host may have changed)
4. `pkgmgr.UpdateCommand` → `runner.RunStream`, writing chunks to the log as they arrive
   with **stateful ANSI stripping** (`ansiStripper`, carries escape state across chunk
   boundaries)
5. Reboot detection unless suppressed
6. Return `hostOutcome{status, exit_code, packages, reboot_required, summary}`

### 9.5 Reboot policy

- The daemon **never reboots automatically.**
- `schedules.suppress_reboot` is a property of the *schedule*, and applies to every
  execution of it including "run now". When set, reboot detection is skipped entirely and
  the log records `reboot suppressed by schedule policy`.
- `hosts.auto_reboot` + `auto_reboot_countdown_seconds` exist in the schema and API, **but
  are enforced entirely client-side by the TUI** (`internal/tui/runs.go:569`
  `prepareAutoRebootQueue` → countdown ticks → `POST /api/hosts/{id}/reboot`). A headless
  integration that only talks to the daemon gets **no auto-reboot at all**. If you need
  server-side auto-reboot you must implement it; the daemon-side primitive
  (`POST /api/hosts/{id}/reboot`) already exists.

### 9.6 Terminal status

```
failures > 0 and successes == 0      → failed
failures > 0 or  skipped  > 0        → partial_failure
otherwise                            → success
context cancelled                    → cancelled
```

Summary string is always `"<n> succeeded, <n> failed, <n> skipped"`.

---

## 10. Scheduling

`internal/scheduler/`. gocron v2 wraps a five-field cron expression generated from
structured fields (`CronExpression`, `frequency.go:50`):

| Frequency | Expression | Validation |
| --- | --- | --- |
| hourly | `M * * * *` | minute 0–59 |
| daily | `M H * * *` | + hour 0–23 |
| weekly | `M H * * D` | + day_of_week 0–6 (Sunday=0) |
| monthly | `M H D * *` | + **day_of_month 1–28** |

Day-of-month is capped at 28 so a monthly schedule never silently skips February.

`Engine.Reload` rebuilds the entire job table from `ListEnabled()` into a *new* gocron
scheduler, swaps it in under a mutex, then shuts the old one down. There is no incremental
job add/remove — mutation endpoints just call `Reload`.

On fire (`engine.fire`): compute the *next* occurrence, then call
`RunSchedule(..., RunTriggerScheduled, "scheduler", nextRunAt)`. `ErrScheduledOverlap` is
swallowed as an expected outcome, not logged as an error.

`NextRuns` is a hand-rolled calendar walk independent of gocron, used for UI previews and
for stamping `next_run_at`. All times are UTC.

---

## 11. Secrets, Keys, and Trust

### Orchestrator SSH key — `internal/ssh/keys.go`

ed25519, generated on first use, private key `0600`, public `0644`, under
`<DataDir>/ssh/id_ed25519`. `Ensure()` is idempotent and also adopts keys from two legacy
paths. `GenerateInMemory()` produces a keypair without touching disk — that is what
rotation uses.

**Rotation** (`SettingsService.RotateKeypair`, `settings_service.go:122`) is all-or-nothing
over every enabled Standard non-self host:

1. Generate a new keypair in memory.
2. For each host: connect with the **old** key, append the new public key, disconnect,
   then reconnect with the **new** key to prove it works.
3. If any host fails → remove the new key from every host it reached, keep the old local
   key, abort with the list of failures.
4. Only then, remove the old key from every host (with restore-on-failure).
5. Only then, write the new keypair to disk.

The append/remove commands are idempotent (`grep -qxF ... || printf ... >>`, and a
`grep -vxF` rewrite through a temp file).

### Host API tokens — `internal/secret/manager.go`

AES-256-GCM, 32-byte key at `SecretKeyPath` created `0600` on first use with `O_EXCL`.
Ciphertext and nonce are stored base64 in `hosts.api_token` / `hosts.api_token_nonce`.
Tokens are decrypted only at point of use and are never returned by any API.

### Threat model, stated plainly

- Whoever can open the Unix socket controls every managed host. Full stop.
- Whoever holds `<DataDir>/ssh/id_ed25519` can log in as `patchpilot` on every managed
  host and run the whitelisted package-manager commands as root — which in practice means
  arbitrary root, since `apt-get install` exists.
- Whoever holds **both** the database and `secret.key` can decrypt all stored API tokens.
- `HostKeyCallback` is `InsecureIgnoreHostKey()` for **all** SSH connections
  (`internal/ssh/client.go:49`, `:60`). There is no known-hosts verification anywhere.
  patchpilot is MITM-able on a hostile LAN. **If you integrate this into anything less
  trusted than a home network, fixing this is the first thing to do.**

---

## 12. API-Managed Hosts (read this before relying on them)

### TrueNAS SCALE — `internal/truenas/client.go`

REST v2 with a bearer token. `Verify` GETs `/api/v2.0/system/info` and requires the
version/product string to contain `SCALE`; a `CORE` marker is refused with a fixed message
(`CoreUnsupportedMessage`). Update flow: `POST /api/v2.0/update/check_available` → if
status is `AVAILABLE`, `POST /api/v2.0/update/update` with `{"reboot": false}` → poll the
returned job id until terminal, streaming progress lines to the host log.

> **Known broken in production.** Real current SCALE instances return
> `server did not respond like TrueNAS SCALE`. Recent SCALE moved much of this surface to
> WebSocket JSON-RPC. See `CONTEXT_HANDOFF.md` §5.4. Do not assume this path works.

### Home Assistant OS — `internal/haos/client.go`

Long-lived access token against the `/api/hassio` Supervisor proxy. `Verify` fetches
`supervisor/info` and `core/info`; `ApplyUpdates` updates **Supervisor first, then Core**,
polling jobs, never rebooting. Rich `HTTPError` with status, path, and a 200-char body
excerpt.

> **Known architecturally wrong.** A working LLAT authenticates against Core `/api/` but is
> rejected by the `/api/hassio` proxy, which needs a different token context and only
> exists on HA OS/Supervised installs. The documented fix is to rebuild around Core
> `update.*` entities. See `CONTEXT_HANDOFF.md` §5.3. The port hint `8123` is also missing
> from the wizard.

**Guidance for an integrator:** treat §7–§10 (SSH onboarding, package managers, run
orchestration, scheduling) as production-grade and well-tested. Treat §12 as a working
sketch of the right *shape* with the wrong *endpoints*.

---

## 13. Self-Update

`internal/selfupdate/selfupdate.go`. Updating the machine that runs the orchestrator is
special: `apt-get dist-upgrade` may replace and restart patchpilot itself mid-run.

Solution: hand the work to systemd and return immediately.

```
systemd-run --unit patchpilot-self-update-<runID>.service --on-active=30s \
  /bin/sh -c "apt-get update && DEBIAN_FRONTEND=noninteractive apt-get -y \
    -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold dist-upgrade \
    && apt-get -y autoremove > '<logdir>/self-update-<runID>.log' 2>&1"
```

The 30-second delay lets the current run finish and the daemon settle. The host run is
marked `success` with summary `self-update scheduled, see <file>`; it does **not** reflect
the eventual outcome. Logs are surfaced through `GET /api/settings/`. Non-Linux returns
`ErrUnsupported`, which the orchestrator maps to host status `skipped`, not `failed`.

**Only apt is implemented for self-update.**

---

## 14. Integration Playbook

Pick the approach that matches how much of patchpilot you actually want.

### A. Drive the daemon (loosest coupling, recommended)

Run `patchpilot daemon` as-is; talk to the socket. You get everything, including
onboarding, for the cost of an HTTP-over-Unix-socket client. Copy the pattern from
`internal/ipc/client.go:28`:

```go
transport := &http.Transport{
    DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
        return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", socketPath)
    },
}
```

Any language works — Python `requests_unixsocket`, Node `http` with `socketPath`, `curl
--unix-socket`. Add your own authn/authz if you expose it beyond the box.

Minimum end-to-end flow:

```
POST /api/wizard/standard/detect   → inspect facts + diagnosis
POST /api/wizard/standard/bootstrap → {id}
GET  /api/wizard/{id}/stream        → consume until bootstrap.complete
POST /api/wizard/{id}/commit        → Host
POST /api/schedules/                → Schedule
GET  /api/runs/{id}/stream          → watch execution
```

### B. Import the Go packages

Module `github.com/philippb/patchpilot`, Go 1.23, everything under `internal/` — you must
vendor or fork to import it. Dependency graph (leaves first, safe to lift individually):

```
pkgmgr    → (stdlib only)          command generation, sudoers, dry-run parsing
secret    → (stdlib only)          AES-256-GCM token storage
ssh       → x/crypto/ssh           runner + keystore
stream    → (stdlib only)          SSE hub + log tailer
scheduler → gocron, core           cron conversion + engine
store     → modernc.org/sqlite     repositories
core      → pkgmgr, ssh, stream, truenas, haos, selfupdate
ipc       → chi, core, scheduler, stream
tui       → bubbletea, lipgloss, bubbles, ipc
```

`core` is the coupling bottleneck: it defines the domain types *and* the orchestration
logic, and imports the protocol clients directly. To reuse orchestration without the
integration clients you would need to extract an interface at
`Orchestrator.executeHostRun`.

Good news for testability — the seams already exist:

- `Orchestrator.ConnectKey` and `HostService.ConnectKey` are injectable connector funcs;
  the test suite substitutes fakes throughout (`orchestrator_test.go`, `bootstrap_test.go`).
- Every store dependency is a narrow interface (`HostRepository`, `OrchestratorRunStore`,
  `SettingsStore`, …) declared at the *consumer*, so swapping SQLite for another database
  means implementing interfaces, not editing `core`.
- Optional capabilities are discovered by type assertion (`OrchestratorRunPageStore`,
  `OrchestratorRunLogStore`, `HostAPITokenRepository`), so a minimal store still works.
- `SelfUpdateScheduler` and `SecretCipher` are one-method interfaces.

### C. Re-implement in another language

Port in this order; each stage is independently useful:

1. **`pkgmgr`** (~300 lines) — the command matrix and sudoers generation. Highest
   value-to-effort ratio in the repo.
2. **Detection probe + `ParseHostFacts`** — one shell constant and a section parser.
3. **Escalation modes** — including the `su` PTY/delay/echo-strip details from §7.2.
4. **Bootstrap sequence + rollback** — §7.4 and §7.6.
5. **Error classification** — §7.7. Skipping this produces a tool people can't debug.
6. **Run admission and grouping** — §9.1 and §9.2. This is where correctness lives.
7. Streaming, scheduling, TUI — in whatever form your system already prefers.

---

## 15. Traps

Things that will bite an integrator, collected from the code and from
`CONTEXT_HANDOFF.md`'s record of what actually broke in production.

1. **`PATCHPILOT_SOCKET` (clients) ≠ `PATCHPILOT_SOCKET_PATH` (daemon).** Different
   variables, both real.
2. **`PrivateTmp=yes`** in the systemd unit means the daemon's `/tmp` is a private
   namespace. Anything the daemon writes to `/tmp` is invisible from a normal shell. Debug
   output must go to `/var/log/patchpilot/`.
3. **`dnf check-update` exits 100** when updates exist. Both the dry-run parser and
   `dryRunWithRunner` special-case it. Any new manager needs the same audit.
4. **Hub `Publish` drops events** for slow subscribers (non-blocking send). History replay
   on subscribe compensates for late joiners, but not for a subscriber that reads slowly.
5. **Run-stream ordering is not guaranteed** between status events and log lines (§6).
6. **Auto-reboot is TUI-only** (§9.5). The most likely silent surprise for a headless
   integration.
7. **Every SSH connection ignores host keys** (§11). Deliberate for LAN use; unacceptable
   elsewhere.
8. **Backup export/import are daemon-local file paths**, not payloads (§5.6).
9. **Bootstrap emits a `Wiping your password from memory` step that is not terminal.**
   Historically the wizard hung right after it; the fix was the SSE race in v0.0.19. Wait
   for `bootstrap.complete`, never for that step.
10. **Host mutations 409 during in-flight runs** on five endpoints (§5.2) — handle the
    conflict, don't retry blindly.
11. **`/etc/sudoers` is never auto-edited.** A missing `@includedir /etc/sudoers.d` is a
    hard failure by design. Don't "helpfully" append to it.
12. **`README.md` is stale** on two points: it says "no automatic reboots" and "no
    notifications", but reboot support (per-host auto-reboot, per-schedule suppression) is
    shipped, and `internal/notify/notifier.go` exists as an interface-only seam with **zero
    implementations**. Both are accurate about the daemon never *self*-initiating a reboot.

---

## 16. Symbol Index

| Concern | Start here |
| --- | --- |
| CLI dispatch | `internal/cli/cli.go:26` |
| Daemon wiring (the dependency graph) | `internal/daemon/daemon.go:26` |
| Route table | `internal/ipc/router.go:16` |
| Socket lifecycle, stale-socket handling | `internal/ipc/server.go:35`, `:88` |
| Reference API client | `internal/ipc/client.go` |
| Domain types | `internal/core/host.go`, `run.go`, `schedule.go`, `settings.go`, `inventory.go` |
| Detection probe + parser | `internal/core/detection.go:12`, `:59` |
| Escalation modes | `internal/core/escalation.go:34`, `:56` |
| Bootstrap orchestration | `internal/core/bootstrap.go:134` |
| Generated remote commands | `internal/core/bootstrap.go:72` |
| Rollback | `internal/core/bootstrap.go:116` |
| Error taxonomy | `internal/core/bootstrap_errors.go:15` |
| Bootstrap session driver | `internal/core/service.go:411` (`runBootstrapSession`) |
| Run admission / queue / skip | `internal/core/orchestrator.go:235` (`createOrQueue`) |
| Execution groups | `internal/core/orchestrator.go:528` (`hostExecutionGroup`, `orderHosts`) |
| Per-host execution | `internal/core/orchestrator.go:543` (`executeHostRun`) |
| Key rotation | `internal/core/settings_service.go:122` |
| Package-manager matrix | `internal/pkgmgr/manager.go` |
| Sudoers generation | `internal/pkgmgr/sudoers.go` |
| Dry-run parsing | `internal/pkgmgr/dryrun.go` |
| SSH runner (incl. PTY/stdin timing) | `internal/ssh/client.go` |
| Keystore | `internal/ssh/keys.go` |
| Token encryption | `internal/secret/manager.go` |
| Event hub | `internal/stream/hub.go` |
| Log tailer | `internal/stream/tailer.go` |
| Cron conversion | `internal/scheduler/frequency.go:50` |
| Scheduler engine | `internal/scheduler/engine.go:56` |
| Crash recovery | `internal/store/sqlite.go:55` |
| Run log paths | `internal/store/runs.go:49` |
| Schema | `migrations/001_initial.sql` … `007_api_username.sql` |
| systemd unit | `deploy/patchpilot.service` |
| Installer | `deploy/install.sh` |
| Build/release | `Makefile` |

### Verification commands

```bash
go test -count=1 ./... && go build ./... && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./... && go vet ./...
```

All packages pass as of `c786ee8`. Packages without tests: `cli`, `daemon`, `diagnostics`,
`logging`, `notify`, `stream`, `systemd`, `version`, `migrations`.