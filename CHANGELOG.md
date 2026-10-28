# Changelog

## 0.2.0 — security and reliability pass

A full audit of HomeHub, with every confirmed finding fixed. Upgrading
applies database migrations 00040–00043 on first start.

### Security

- **Shells need configure.** Terminals, container exec, VM consoles, guest
  SSH, the SQL console and the assistant's SSH commands now need configure on
  the resource instead of control, so the built-in Operator role no longer
  gets root on every system. Dropping or emptying a table needs delete.
  **Upgrade note:** custom roles that relied on control for shells need
  configure on those resources.
- **No borrowing SSH keys.** Binding a resource or an integration to a
  registry SSH host needs the shell right on that host.
- **Stored secrets stay with their destination.** Changing the address, port,
  TLS setting or login of an integration, backup target, notification channel,
  inline SSH credential or password SSH host requires re-entering its secret.
- **Nobody grants more than they hold.** Assigning roles, writing grants,
  editing or deleting roles, and resetting, revoking or deleting users all
  require the caller to hold every right involved.
- **Calendars have permissions** (`calendar/<id>`: view, control, configure,
  delete) instead of only requiring a session; repeat rules more frequent than
  hourly are refused and expansion is bounded.
- **Database browser:** the server's own schemas (password hashes) are no
  longer reachable through it; the live query list needs control; the
  create-table builder escapes backslashes (SQL injection); passwords are
  redacted from SQL console audit entries; MySQL uses TLS when offered.
- **Caddy:** site fields are validated so they cannot inject directives into
  the Caddyfile, and editing sites needs configure.
- **Assistant:** the SSH command classifier is shell-aware and fails closed
  (substitutions, redirects, acting flags of find/sort/env/ip…, interpreters);
  every command it runs is audited; unlocking, opening, disarming and taking
  systems down wait for a confirmation; answers no longer load remote images.
- **Telegram:** the bot token never appears in errors or status; proposals
  carry a code and a yes only runs the proposal it answers; link previews are
  off; the bridge retries instead of stopping after one failed start.
- **Sign-in:** the setup wizard needs a one-time token from the server log or
  `<data dir>/setup-token`, and only one first Owner can be created; failed
  logins lock only the attacking address, not the account; passkeys require a
  PIN or biometric (**upgrade note:** keys without a PIN stop working until one
  is set); recovery codes carry 80 bits and are stored salted; sessions have a
  90-day hard cap and signing out closes open terminals; invites redeem
  atomically; password hashing is concurrency-limited.
- **HTTP:** the client IP behind trusted proxies is taken from the right of
  X-Forwarded-For; a Content-Security-Policy on every response and HSTS over
  HTTPS; converter uploads up to the promised 1 GiB get through.
- **Backups and keys:** the backup key is only created when genuinely absent,
  never replaced on a read error; a restore is all-or-nothing, refuses
  archives from a newer HomeHub and is never retried; backups now include
  uploaded workspace files; NFS/SMB/S3 paths cannot escape the backup folder;
  a missing `master.key` stops startup instead of being regenerated when
  secrets exist; new secrets are bound to the record they belong to
  (**upgrade note:** a binary older than 0.2.0 cannot read secrets saved by
  0.2.0); an SSH host key is refused when its pin cannot be read.

- **Converter sandbox without root.** Media conversions (ffmpeg, ImageMagick,
  libraw, pandoc) used to run unsandboxed whenever HomeHub was not root,
  including in the Docker image. They now run in user, mount, network and PID
  namespaces on a private read-only root that does not contain the data
  directory or `master.key`, with every capability dropped. Where no sandbox
  is available, media conversions are refused unless
  `HOMEHUB_CONVERT_UNSANDBOXED=1` is set.
- **Network tools cannot reach HomeHub's own host.** The SSL check, TCP/port
  check, IP scanner, ping, DNS-against-a-server and Wake-on-LAN now share the
  HTTP tester's egress guard: loopback, link-local, cloud-metadata and
  `0.0.0.0/8` addresses are refused at connect time; LAN addresses stay
  reachable.
- **Minecraft pairing codes cannot be brute-forced.** Wrong codes are limited
  per IP and globally, and each live code is withdrawn after 10 wrong guesses.
- **Minecraft skin proxy** now requires a session, accepts only real player
  uuids, and its cache is bounded (512 entries, one hour).
- **LIKE wildcards in names are escaped.** Deleting a folder named `%` deleted
  every nested file you owned; all pattern queries now match literally.
- **Workspace quota** (2 GiB per user) is enforced on every write: new and
  edited text files, and parallel uploads, not just a single upload.
- **Audit log is append-only at the database.** A trigger refuses updates
  and deletes except retention pruning.
- **Setup snippets prefer HTTPS.** Minecraft pairing and the caddy-reader
  installer use the configured https external URL when the page is open over
  plain http, and the installer verifies the binary's SHA-256 before running it.

### Reliability

- Startup now actually waits for PostgreSQL (up to a minute) instead of failing
  on the first refused connection.
- Background jobs interrupted by a restart are marked failed ("interrupted by
  restart") instead of staying "running" forever. Jobs for the same target no
  longer pile up (backups, dumps, container actions, schedules), and every job
  has a time limit (one hour by default).
- One slow browser tab can no longer stall live updates for everyone.
- The audit log and PatchPilot run history are pruned nightly (defaults: 365
  and 90 days, configurable in *Settings → History retention*).
- Metric rollups and retention use new time indexes instead of scanning the
  whole sample table every five minutes.

### Operations

- `make deploy` refuses to restart into a binary other than the one it just
  built and verifies the running process afterwards; recipes fail on any
  failing command, including inside pipelines.
- The systemd unit in `deploy/` gains hardening compatible with what HomeHub
  does (NoNewPrivileges, PrivateTmp, ProtectSystem=full and related settings),
  and `make install-service` writes it with this checkout's paths.
- The Dockerfile now matches `make build`: the Go version from `go.mod`, the
  Gradle wrapper, the embedded caddy-reader helper, the VERSION stamp, and the
  runtime tools the app runs (pg_dump 17, ffmpeg, ImageMagick, libraw, pandoc).
- `homehub migrate` applies the schema without starting the server.
- Continuous integration runs go vet, go test (with a PostgreSQL service for the
  database-backed tests), and the web typecheck and build.

## 0.1.0

First public release.
