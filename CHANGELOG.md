# Changelog

## 0.2.0 — security and reliability pass

A full audit of HomeHub, with every confirmed finding fixed. Upgrading
applies database migrations 00040–00043 on first start.

### Security

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
