# Behaviour a client needs that the prose does not state

Everything here is read off the source in `src/`. It is the set of things that
will cost you an afternoon if you assume otherwise.

---

## 1. There are no version-comparison rules. Do not write any.

The helper reports a version in four places (`/health`, `/config`,
`PUT /caddyfile` success, and `caddy-reader version`). **Nothing compares it.**
It is display and support metadata only.

Capability is detected by **status code**, not by version:

| Situation | How the client learns it |
|---|---|
| helper predates the Caddyfile routes | `GET /caddyfile` → **404** |
| helper predates pairing | `POST /pair` → **404** with no JSON `error` field |
| helper is read-only | `GET /caddyfile` → `200` with `"writable": false`, or `PUT` → **403** |
| helper is sending unfiltered config | `GET /config` → `"projected": false` |

This is deliberate. Parsing `1.0.0` and comparing it means inventing a version
policy the helper does not enforce, and it breaks the moment someone builds from
source and gets `dev` — which is a perfectly ordinary thing to do and reports a
version that no comparison scheme orders correctly.

Ask what you need. Handle the refusal.

`Version` defaults to the string `"dev"` when `-X main.Version=` is not passed.

---

## 2. Flags, defaults, and two that are not what they look like

| Flag | Env var | Default | Note |
|---|---|---|---|
| `--listen` | `READER_LISTEN` | `0.0.0.0:9776` | |
| `--admin` | `CADDY_ADMIN` | `http://127.0.0.1:2019` | |
| `--token` | `READER_TOKEN` | *(none — refuses to start)* | **minimum 16 characters** |
| `--caddyfile` | `CADDYFILE` | `/etc/caddy/Caddyfile` | |
| `--caddy` | `CADDY_BIN` | `caddy` | **Unused.** Kept only so existing unit files still parse. Caddy adapts the Caddyfile in its own process, so the helper needs no `caddy` binary. |
| `--writable` | *(none)* | `false` | Deliberately has **no** env var — it must be visible in `ExecStart=`. |
| `--raw` | *(none)* | `false` | Sends the whole config, secrets included. |
| — | `PAIRING_FILE` | `/etc/caddy-reader.pairing` | Overrides where the pairing window is written. Exists so tests need not write to `/etc`. |

**The token is never auto-generated.** Starting without one exits `2` with
instructions. A token printed at startup is lost on the next `systemctl restart`.

**With `--writable`, startup validates and exits `2` if it cannot proceed:** the
Caddyfile must be readable, *and* a temp file must be creatable in its directory
(replacing a file needs write permission on the **directory**, not the file). So
a misconfigured writable helper fails at boot, not at someone's first save.

On successful start it writes one JSON line to stdout:

```json
{"msg":"caddy-reader listening","listen":"…","admin":"…","caddy":"reachable",
 "mode":"projected","version":"…","caddyfile":"…","write":"read-only"}
```

`caddy` here is either `"reachable"` or `"NOT reachable yet: <reason>"` — the
helper starts either way, because Caddy may simply be slower to come up.

---

## 3. Response bodies, field by field

Every response is JSON. Every error is `{"error": "<sentence>"}`, sometimes with
`"detail"`.

### `GET /health` — unauthenticated

```json
{"status": "ok",       "caddy": "reachable",    "version": "1.0.0"}   // 200
{"status": "degraded", "caddy": "<why not>",    "version": "1.0.0"}   // 503
```

`caddy` on failure is a full sentence naming the admin URL it tried. Both fields
are strings; `status` is exactly `ok` or `degraded`.

Deliberately says nothing about the configuration, so it is safe to poll from
anywhere. **Use this for liveness — it needs no token.**

### `GET /config` — Bearer

```json
{"config": { … }, "projected": true, "version": "1.0.0"}
```

`projected: false` means `--raw`: the helper is sending the **whole** config,
including basic-auth hashes and ACME EAB keys. Surface that loudly. devtracker
appends to its connection-test message:

> ⚠ This helper is running with `--raw`, so it is sending the WHOLE config,
> including any secrets in it.

### `GET /caddyfile` — Bearer

```json
{"path": "/etc/caddy/Caddyfile", "text": "…", "sha256": "<hex>", "writable": false}
```

`writable` reflects the **flag**, not filesystem permissions. It is the field to
gate your edit UI on.

### `PUT /caddyfile` — Bearer, `--writable` only

Request:

```json
{"text": "<the whole new file>", "sha256": "<hash of the version being replaced>"}
```

Success:

```json
{"ok": true, "sha256": "<new hash>", "backup": "/etc/caddy/Caddyfile.devtracker-20260817-143052.bak", "version": "1.0.0"}
```

The `backup` path is returned. Showing it is a cheap way to make an edit feel
reversible.

### `POST /pair` — the code itself is the credential

```json
{"token": "<64 hex chars>", "version": "1.0.0"}
```

---

## 4. Status codes, and what each one actually means

| Code | Route | Meaning for the client |
|---|---|---|
| `401` | any Bearer route | Wrong or missing token. Not recoverable by retrying. |
| `403` | `PUT /caddyfile` | Helper is read-only. Tell the operator to re-run setup with `--writable`. |
| `400` | `PUT /caddyfile` | **Caddy rejected the config.** `detail` holds the tidied reason. Nothing changed. |
| `400` | `PUT /caddyfile` | Also: body is not JSON, or the text is empty/whitespace. |
| `409` | `PUT /caddyfile` | The file changed underneath. Re-read, re-apply, retry. |
| `500` | `PUT /caddyfile` | Caddy accepted it but the disk write failed. **Read the message** — it says which way round things ended up. |
| `502` | `GET /config`, `GET /caddyfile` | Helper is fine, Caddy or the file is not. |
| `503` | `GET /health` | Same, in liveness form. |
| `404` | `POST /pair` | No pairing window is open. |
| `410` | `POST /pair` | Code expired; the window was deleted. |
| `429` | `POST /pair` | Fifth wrong code. **The window is burned** — a new one must be generated on the host. |
| `405` | any non-GET on an unknown path | Refusing writes is a stated property, not a mux accident. |

### The one string worth matching

Nothing requires string matching. Match codes, and **pass the helper's `error`
through to the user verbatim** — especially for pairing, where the helper knows
whether the code was wrong, expired or spent, and how many attempts remain:

> `Wrong code. 3 attempt(s) left before it is cancelled.`

Reproducing that logic client-side means keeping two counters in sync.

One special case worth handling explicitly, because the naive reading is wrong:

```
POST /pair → 404 AND the body has no "error" string
```

means *"something answered, but it is not a caddy-reader"* — most often the
operator typed Caddy's own admin address, or an unrelated service. A plain `404`
**with** an `error` string means "no pairing window is open", which is a
completely different instruction to give.

---

## 5. `sha256` on PUT is optional, and omitting it means "force"

```go
if req.Sha256 != "" && req.Sha256 != current.Sha256 { /* 409 */ }
```

An empty or absent `sha256` **skips the conflict check entirely** and overwrites
whatever is there.

Always send it. The only legitimate use for omitting it is a deliberate
"overwrite anyway" the operator asked for after seeing the conflict — and if you
offer that, say what it discards.

---

## 6. Pairing details a client must handle

- **15 minutes** (`pairWindow`), **5 attempts** (`pairMaxAttempts`), single use.
- The attempt counter is **persisted to the file**, so it survives a restart of
  the helper. You cannot reset it by reconnecting.
- The helper compares after `strings.TrimSpace` only. It does **not** strip
  interior spaces, so `481 502` — the format the helper *prints* — will fail if
  sent verbatim. **Strip all whitespace client-side** before sending:
  `code.replace(/\s+/g, '')`.
- Validate six digits client-side (`/^\d{6}$/`) before spending an attempt.
- On success the window is deleted. A second POST with the same code gets `404`.
- `/pair` is the only unauthenticated write-shaped route. It is rate-limited
  **only** by the attempt counter — there is no IP-based limit.

---

## 7. Writing: order of operations you can rely on

```
validate against Caddy  →  refused? nothing on disk was touched. Stop.
                        →  accepted? back up, then write, then prune
```

A `400` from `PUT /caddyfile` means the running proxy is **unchanged and
correct**. You can say so plainly rather than telling someone to check.

A `500` means Caddy accepted it and the disk write failed. The helper re-posts
the old config to put Caddy back and says so in the message. This is the one case
where the client must show the raw message rather than a summary.

Backups: `<caddyfile>.devtracker-<UTC yyyymmdd-hhmmss>.bak`, mode `0600`, newest
**10** kept. `uninstall` never deletes them — it counts them and says they were
left behind.

The existing file's permission bits are **preserved** on write; only new backups
get `0600`.

---

## 8. Limits and timeouts

| | |
|---|---|
| Max config read from Caddy | 32 MiB (`io.LimitReader`) |
| Max request body | 32 MiB (`http.MaxBytesReader`); `/pair` is 4 KiB |
| Server read-header / read / write / idle | 10 s / 30 s / 60 s / 120 s |
| Redirects | **never followed** (`http.ErrUseLastResponse`) |

Client side, devtracker uses 10 s for reads and 30 s for manager operations, and
streams the response with an 8 MiB ceiling rather than buffering it whole. A
`response.text()` on an endpoint that streams forever is an out-of-memory crash
in your app, reachable from a settings screen.

---

## 9. Routing quirks

- Both `/x` and `/x/` are registered for every route. A `404` on the trailing
  slash reads as "the helper is broken" to whoever is testing by hand.
- Unknown path, `GET`/`HEAD` → `404`. Unknown path, any other method → `405`.
  Both carry a sentence naming what *is* served.
- **No TLS.** The token is the boundary; this is a LAN tool. An `https://` base
  URL works if you supply the certificate.
- **No rate limiting on `/config`.** It is behind a 256-bit bearer token, which
  has nothing to guess.

---

## 10. `caddy-reader project` is a client-side tool, not a test hook

```bash
curl -s localhost:2019/config/ | caddy-reader project
```

Reads a config on stdin, writes the projection on stdout, exits `2` if stdin is
not JSON. It exists so an operator can see exactly what would leave their machine
before trusting it — and it is what makes the cross-language fixture check in
`fixtures/` possible. Use it in CI against your own parser.

---

## 11. Exit codes

| Command | Codes |
|---|---|
| `run` (no subcommand) | `2` no/short token, unknown flag, or `--writable` preflight failed; `1` listen failed or server died |
| `setup` | `0` ready; `1` service did not stay up, Caddyfile unreadable, or a post-install check failed; `2` unknown option |
| `verify` | `0` all passed; `1` a check failed or nothing answered; `2` no token available |
| `pair` | `0`; `1` not root, or nothing set up |
| `status` | `0` running; `1` not installed or not active |
| `uninstall` | `0`; `1` not root, wrong confirmation code, or partial removal |
| `project` | `0`; `2` stdin is not JSON |
| unknown subcommand | `2`, with usage |

`setup` returning `1` while the service **is** running is a real state: it means
the service came up but a post-install check failed. Treat it as "installed,
verify before connecting", not "not installed".

---

## 12. Rebranding surface

The source says **devtracker** in 39 places across the `.go` files, all of them
operator-facing strings. None affect protocol behaviour. If HomeHub ships this,
the ones that matter:

| Where | What |
|---|---|
| `setup.go` → `unitFile()` | `Description=` and `Documentation=` (points at devtracker's GitHub) |
| `setup.go` → `envFileText()` | the comment *"The token devtracker uses"* |
| `setup.go`, `pairing.go` | the pairing panel: *"In devtracker: Integrations → Connect a Caddy host"* |
| `caddyfile.go` | the backup suffix `.devtracker-<ts>.bak` — **changing this changes which files `pruneBackups` and `uninstall` find.** If you change it, change both, or old backups become invisible and unpruned. |
| `main.go`, `README.md` | the doc comments and help text |
| `go.mod` | already set to `module caddy-reader` in this export (it was `github.com/TheOriUHD/devtracker/helper`; the package is `main` with no internal imports, so the path is arbitrary) |

The **service name, unit path, env path, pairing path, listen port and every
route** are unchanged and should stay that way: two apps managing the same Caddy
host should find the same helper rather than installing two of them.

That is also the interesting consequence — **one helper can serve several
clients.** They share one token, so a client that can read can also write if the
helper is writable. If that is not acceptable, run separate helpers on different
ports with different `PAIRING_FILE` and env paths, and accept that each needs its
own systemd unit.

---

## 13. Two stale comments were corrected in this export

Both referenced `$CADDY_READER_FLAGS`, an environment variable removed when the
`--writable` flag moved into `ExecStart=` literally. The code was right; the
comments were describing the old mechanism.

- `setup.go`, in the writable unit template — now says *"Re-run `caddy-reader
  setup --read-only` to go back"*.
- `verify.go`, in the write-access warning — same correction.

`caddyfile_test.go` still mentions the variable, correctly: it is a regression
test asserting the indirection is **gone**.

These are also fixed upstream in devtracker, so the two copies do not diverge.
