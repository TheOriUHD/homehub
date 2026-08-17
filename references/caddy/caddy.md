# The Caddy interface

A complete technical description of how devtracker manages a Caddy reverse proxy
running on a different machine. It is written so that someone — or something —
holding only this document can rebuild the system without reading the original
source.

Everything here is implemented and shipping. Where a decision looks arbitrary,
the reason it is not is stated, because most of these were arrived at by getting
them wrong first.

---

## 1. The problem, stated precisely

Caddy's admin API listens on `127.0.0.1:2019` and is **completely
unauthenticated**. Anything that can open a TCP connection to it can `POST
/load` and replace the entire configuration — which on a reverse proxy means
routing every domain anywhere, serving any file on the box, and terminating TLS
on the attacker's behalf.

devtracker runs on a *different* machine. So the naive solution — bind the admin
API to `0.0.0.0:2019` and let devtracker talk to it — is not a smaller version
of the right answer. It is the single worst change available on that host.

> **Hard rule.** Nothing in this system, and nothing in its documentation, may
> ever instruct anyone to expose Caddy's admin API beyond loopback.

The answer is a small dedicated program, `caddy-reader`, that runs *on the Caddy
host*, holds a token, and exposes a narrow, auditable surface to the LAN.

### 1.1 What the system delivers

Two capabilities, and they are separate on purpose:

| Capability | What it is | Requires |
|---|---|---|
| **Manager** | A graphical CRUD interface over the sites in the Caddyfile — list, add, edit, remove — always reading live, never from a cache. | helper in `--writable` mode |
| **Import** | Read the *running* config, match its domains against recorded deployments, and propose filling in each deployment's `proxy_domain`. | read-only helper is enough |

And one link between the two halves of the product, deliberately the whole of it:
a project may be associated with one domain that Caddy serves, so there is a link
to click. Nothing else about Caddy touches the rest of devtracker.

---

## 2. Architecture

```
┌─────────────────────────── devtracker host ───────────────────────────┐
│                                                                       │
│  SPA (React)                                                          │
│    Caddy tab            ── the manager: list / add / edit / remove     │
│    Integrations tab     ── import proposals                           │
│    Project detail       ── domain picker (the one cross-link)         │
│         │  fetch /api/…                                               │
│  API (Hono)                                                           │
│    CaddyManagerService  ── read-modify-write the Caddyfile            │
│    CaddyService         ── pairing, live read, matching, apply        │
│    @devtracker/core     ── caddyfile.ts (split/render)                │
│                            caddy.ts     (parse adapted JSON)          │
│    credentials store    ── the helper token, encrypted at rest        │
└───────────────────────────────┬───────────────────────────────────────┘
                                │  HTTP + Bearer token, over the LAN
┌───────────────────────────────▼─── Caddy host ────────────────────────┐
│  caddy-reader (single static Go binary, systemd unit)                 │
│    GET  /health      unauthenticated liveness                         │
│    POST /pair        six-digit code  →  256-bit token                 │
│    GET  /config      PROJECTED running config (allowlisted)           │
│    GET  /caddyfile   the file text + sha256                           │
│    PUT  /caddyfile   replace it, only with --writable                 │
│         │                                                             │
│         │ GET  {admin}/config/     ← the only read                    │
│         │ POST {admin}/load        ← the only write, text/caddyfile   │
│  Caddy ─┘  127.0.0.1:2019, still loopback-only                        │
└───────────────────────────────────────────────────────────────────────┘
```

### 2.1 Why a separate binary

- **Static, zero dependencies, `CGO_ENABLED=0`.** Nothing to install on the
  machine that fronts all your traffic, and nothing that breaks when that
  machine's Python or OpenSSL is upgraded.
- **Auditable in one sitting.** "Can this thing write to Caddy?" is answerable
  by reading one constant and one function.
- **Ordinary systemd service.** `systemctl status caddy-reader` works, logs go
  to the journal, it restarts on failure.

---

## 3. The helper: `caddy-reader`

Go 1.26, standard library only. Build with `CGO_ENABLED=0 go build -ldflags
"-X main.Version=…"`, cross-compiled for `linux/amd64`, `linux/arm64`,
`linux/armv7`.

### 3.1 Two modes, and the difference is the whole point

| | Read-only (default) | `--writable` |
|---|---|---|
| `GET /config`, `GET /caddyfile` | yes | yes |
| `PUT /caddyfile` | **403** | yes |
| systemd sandbox | `DynamicUser=yes`, `ProtectSystem=strict`, `ReadOnlyPaths=/`, `CapabilityBoundingSet=` | `User=root`, `ProtectSystem=full`, `ReadWritePaths=/etc/caddy` |
| Privileges needed | none at all | write access to the Caddyfile's **directory** |

Write mode is off unless explicitly requested. A token that can rewrite a reverse
proxy is worth far more to an attacker than one that can read it, and most
installs only ever want the import.

The flag is written **literally into `ExecStart=`**, not passed through an
environment variable:

```
ExecStart=/usr/local/bin/caddy-reader --writable
```

so that `systemctl cat caddy-reader` answers "can this change my config?"
without anyone cross-referencing an env file. *(An earlier version routed it
through `$CADDY_READER_FLAGS`, and a re-run of the installer silently revoked
write access because the variable defaulted empty.)*

### 3.2 What it cannot do, in either mode

- **Cannot replace Caddy's configuration through the admin API.** The only read
  it performs there is `GET {admin}/config/`, and that path is a **compile-time
  constant**, never derived from an incoming request:

  ```go
  const caddyConfigPath = "/config/"
  ```

- **Cannot be used as a general proxy.** It forwards no paths, methods, headers
  or bodies from the incoming request to Caddy.
- **Cannot write anywhere except the one Caddyfile it was pointed at**, and only
  after Caddy itself has accepted the replacement.
- **Follows no redirects.** `CheckRedirect` returns `http.ErrUseLastResponse` —
  the admin address is trusted; anything it redirects to is not.

### 3.3 HTTP surface

Routing uses Go 1.22+ method patterns with exact paths. Both `/x` and `/x/` are
registered — someone checking by hand will type the trailing slash as often as
not, and a 404 there reads as "the helper is broken".

| Method + path | Auth | Behaviour |
|---|---|---|
| `GET /health`, `/health/` | none | `{status, caddy, version}`. `200` if Caddy is reachable, `503` if not. Says nothing about the configuration. |
| `POST /pair`, `/pair/` | the code | Exchanges a six-digit code for the token. §4. |
| `GET /config`, `/config/` | Bearer | `{config, projected, version}` — the **projection** (§5), or the raw config with `--raw`. |
| `GET /caddyfile`, `/caddyfile/` | Bearer | `{path, text, sha256, writable}` |
| `PUT /caddyfile`, `/caddyfile/` | Bearer + `--writable` | Replace the file. §6. |
| anything else | — | `404` for GET/HEAD, `405` for any other method, both with a sentence saying what it does serve. |

The `405` is spelled out rather than left to the mux default, so refusing writes
is a stated property of the program instead of an accident of the library.

**Token comparison is constant-time** (`crypto/subtle.ConstantTimeCompare`).

**Timeouts** on the HTTP server: `ReadHeaderTimeout` 10 s, `ReadTimeout` 30 s,
`WriteTimeout` 60 s, `IdleTimeout` 120 s. A client that connects and says nothing
must not hold a slot forever.

**Body ceiling**: `maxConfigBytes = 32 MiB`, applied both to what is read from
Caddy (`io.LimitReader`) and to incoming request bodies
(`http.MaxBytesReader`). Without it, pointing `--admin` at something that streams
forever is a way to exhaust the process.

### 3.4 Flags and environment

| Flag | Env | Default | Meaning |
|---|---|---|---|
| `--listen` | `READER_LISTEN` | `0.0.0.0:9776` | where to serve |
| `--admin` | `CADDY_ADMIN` | `http://127.0.0.1:2019` | Caddy's admin API |
| `--token` | `READER_TOKEN` | *(required)* | shared secret, min 16 chars |
| `--caddyfile` | `CADDYFILE` | `/etc/caddy/Caddyfile` | the file to read and write |
| `--writable` | — | `false` | allow `PUT /caddyfile` |
| `--raw` | — | `false` | send the whole config instead of the projection |

**The token is never generated at startup.** If none is supplied the program
exits with instructions. A token printed at boot is lost the moment systemd
restarts the service, and "it worked yesterday" is a terrible way to discover
that.

**Startup validation** — checked at boot, not on someone's first save:

1. `GET {admin}/config/` once, reporting reachability in the startup log line
   without refusing to start (Caddy may simply be slower to come up).
2. With `--writable`: the Caddyfile must be readable, **and** a temp file must be
   creatable *next to it*. Replacing a file needs write permission on its
   directory, not on the file, and discovering that when someone presses Save is
   too late.

### 3.5 Subcommands

```
sudo caddy-reader setup       set it up, and print a code to pair with
sudo caddy-reader pair        print a fresh pairing code
caddy-reader status           what it is doing right now
sudo caddy-reader start|stop|restart
caddy-reader verify [host]    check it does what it claims
sudo caddy-reader uninstall   remove it. Your Caddy config is untouched.
caddy-reader project          read a config on stdin, print what would be sent
caddy-reader paths            the files setup writes
caddy-reader version
```

Every one of these works without a token. Being told to generate a secret in
order to ask what a command does is a small insult, and an earlier version did
exactly that.

A bare word that is not a known subcommand is treated as a typo and gets the
usage text — falling through to flag parsing turned `caddy-reader verifyy` into a
confusing complaint about a missing token.

---

## 4. Pairing: six digits, not sixty-four characters

### 4.1 The design

The code is **not** the credential. Six digits is a million possibilities, which
is nothing to a script. What it buys is exactly one exchange:

1. `caddy-reader setup` (or `pair`) generates a code with `crypto/rand`,
   zero-padded to six characters, and writes `/etc/caddy-reader.pairing`
   mode `0600`:
   ```json
   { "code": "481502", "token": "<the real 64-hex token>",
     "expires_at": "…", "attempts": 0 }
   ```
2. The operator types the helper's address and the code into devtracker.
3. devtracker `POST`s `{"code": "481502"}` to `{baseUrl}/pair`.
4. The helper returns `{"token": "…"}` and **deletes the pairing file**.
5. devtracker stores the token in its encrypted credential store.

Three properties close every way this can be abused:

- **Expires** after 15 minutes (`pairWindow`).
- **Counts wrong guesses** — after 5 (`pairMaxAttempts`) the window is *burned*,
  not merely rejected. A new code costs one command; a guessed one costs the
  proxy.
- **Single use** — the window closes on success, so a code read over someone's
  shoulder is worth nothing a second time.

Comparison is constant-time. The response body distinguishes wrong / expired /
absent and reports attempts remaining, because the person typing it needs to know
which mistake they made.

| Condition | Status |
|---|---|
| no pairing file | `404` |
| past `expires_at` | `410` (and the file is deleted) |
| wrong code, attempts remain | `401` with a count |
| wrong code, 5th attempt | `429`, window burned |
| correct | `200 {token, version}` |

### 4.2 Why not just show the token

The alternative — read a 64-character hex string off a terminal and type it into
a browser — is what this replaced. People get it wrong, and it tempts everyone
involved into copying the secret somewhere more convenient than it should be.

The code is printed spaced (`481 502`) because that is easier to read across a
room.

---

## 5. The projection

`GET /config` does **not** return Caddy's configuration. It returns a reduction
of it.

A Caddy config carries secrets: `basic_auth` password hashes, credentials set on
headers going upstream, ACME external-account-binding HMAC keys. None of them are
needed to answer "which domain points at which upstream", so none of them are
sent.

### 5.1 It is an allowlist, not a denylist

This is the load-bearing decision. A denylist of sensitive keys silently leaks
whatever Caddy adds in its next version. An allowlist silently *drops* it — and
dropping a field devtracker needed is a visible bug, where leaking one is an
invisible disclosure.

### 5.2 What survives

```
apps.http.servers.<any key>
  ├── listen: [string]                    (not parsed; lets a human confirm the server)
  └── routes: [
        ├── match: [ { host: [string], path: [string] } ]
        └── handle: [
              ├── handler: string
              ├── routes: [...]           (recursion, for `subroute`)
              ├── upstreams: [ { dial: string } ]     ← dial ONLY
              └── dynamic_upstreams: {}   (presence only, contents dropped)
            ]
      ]
```

`upstreams[].dial` and nothing else: an upstream can also carry TLS
client-certificate settings, which are none of devtracker's business.
`dynamic_upstreams` is reduced to an empty object because only its *presence* is
read — it means "this cannot be resolved statically", and how the upstreams are
discovered does not change that answer.

### 5.3 The cross-language contract

The nested shape is preserved **exactly**, so devtracker's parser is identical
whether it is handed a projection or a config pasted by hand from `caddy adapt`.

This allowlist (Go) and that parser (TypeScript) are two halves of one contract
written in two languages, so there is a test that runs the real Go binary over a
real `caddy adapt` fixture and asserts both produce the same upstreams, the same
served-but-not-proxied domains, and no warnings. It also asserts that basic-auth
hashes, upstream header credentials and ACME EAB keys are gone.

`caddy-reader project` exposes the same code path on stdin/stdout, so anyone can
see exactly what would be sent before trusting it:

```bash
curl -s localhost:2019/config/ | caddy-reader project
```

---

## 6. Writing the Caddyfile

This is the part that can take a reverse proxy offline, so the order of
operations is chosen to make a rejected config a **non-event**.

### 6.1 The sequence

```
1. read the current file, hash it
2. compare the caller's sha256 against it     → 409 if it moved
3. POST the NEW TEXT to {admin}/load          → Caddy adapts, validates, applies
   ├── Caddy refuses  → 400, and NOTHING on disk was touched. Done.
   └── Caddy accepts  → continue
4. write a timestamped backup of the OLD text
5. write the new text, preserving the file's existing mode
6. prune backups beyond the newest 10
```

Validation happens **before** anything on disk moves. A refused config never
reaches the filesystem, so there is nothing to undo and no window in which the
file and the running proxy disagree.

If step 4 or 5 fails after Caddy accepted, the helper re-posts the *old* text to
restore Caddy and reports precisely which way round things ended up. Recoverable,
and the operator has to know.

### 6.2 Post the TEXT, let Caddy adapt it

```go
s.client.Post(admin+"/load", "text/caddyfile", strings.NewReader(text))
```

`Content-Type: text/caddyfile` selects Caddy's Caddyfile adapter, so **Caddy
parses the text itself, in its own process, with its own environment.**

This is the single most important detail in the whole system, and it was arrived
at the hard way.

> An earlier version ran `caddy adapt` in the helper and posted the resulting
> JSON. A Caddyfile containing `tls { dns cloudflare {env.CLOUDFLARE_API_TOKEN} }`
> adapted, in the helper's environment, to JSON with an **empty** token — and
> Caddy then rejected it as *invalid* rather than *missing*, producing the
> memorably useless error `API token "" appears invalid`.

Handing Caddy the text instead means:

- the secrets never leave the process that already has them;
- the helper needs no copy of any of them;
- the helper needs no `caddy` binary at all, so there is no second binary whose
  version has to match.

### 6.3 Optimistic concurrency

`GET /caddyfile` returns a `sha256` of the text. `PUT` carries the hash of the
version being replaced. If they differ, the helper answers `409` and changes
nothing:

> The Caddyfile changed since you loaded it — someone edited it by hand, or
> another tab saved first. Reload and reapply your change so theirs is not lost.

Someone editing the file by hand while a browser tab holds an older copy is not
exotic; it is Tuesday. Without this the browser silently wins.

### 6.4 Backups

`<caddyfile>.devtracker-20260817-143052.bak`, mode `0600`, newest 10 kept
(timestamped names sort chronologically, so pruning is `sort` + `slice`).
**Uninstall never deletes them** — removing someone's only copy of a working
config during an uninstall would be unforgivable; it counts them and says they
were left behind.

An empty or whitespace-only body is refused outright. An empty Caddyfile is
*valid* and would take every site offline; if that is genuinely wanted it can be
done by hand.

### 6.5 Making Caddy's errors readable

Caddy logs JSON to stderr, repeats the same lint warning once per site, and puts
the actual failure last. A config with fifteen sites produced twenty-four
identical "Unnecessary header_up" lines above the one sentence that said what
went wrong — which is a way of reporting an error that successfully hides it.

`tidyCaddyOutput` therefore:

1. parses each line as JSON when it starts with `{`, taking `level` and `msg`;
2. treats `level == "error"` or a `Error:` prefix as an error;
3. for errors, keeps only the **last** segment of the `loading X: loading Y:
   <reason>` chain — that chain is Caddy narrating its own call stack, and the
   reason is at the end;
4. deduplicates errors and warnings separately;
5. prints **errors first**, then `Caddy also warned:` with the unique warnings
   indented;
6. replaces the temporary filename with `Caddyfile`, because the temp name means
   nothing to whoever is reading.

---

## 7. Splitting the Caddyfile — never parsing it

devtracker owns the whole file: it reads it, you edit entries graphically, and it
writes the file back. The obvious implementation — parse into a model, regenerate
from the model — quietly drops everything the model does not know about. A
reverse proxy is a bad place to discover that a directive went missing.

**So this does not parse the Caddyfile. It splits it.**

### 7.1 The invariant

> For any file, parsing and re-rendering **without edits** produces
> byte-identical text — and therefore byte-identical adapted JSON.

This is what makes "devtracker owns the file" safe to say, and it is asserted by
tests against real Caddyfiles including a DNS-challenge one with `header_up`
blocks, comments between blocks, and unusual whitespace.

### 7.2 The block model

The top level of a Caddyfile is a sequence of blocks. Finding their boundaries
needs only brace counting that respects quotes and comments — not a grammar.
Every block keeps its **exact original text** in `raw`.

```ts
type CaddyfileBlock =
  | { kind: 'global'; raw }                              // the leading { … }
  | { kind: 'entry';  raw; entry: CaddyfileEntry }       // structured-editable
  | { kind: 'raw';    raw; addresses; reason }           // preserved, edited as text
  | { kind: 'gap';    raw }                              // comments and blank space
```

`renderCaddyfile` is `blocks.map(b => b.raw).join('')`. Nothing else.

`gap` blocks exist because dropping the whitespace and comments between blocks
would reformat someone's file as a side effect of editing one entry.

### 7.3 Scanning rules

- **Quoting**: `"` (with `\` escapes) and backtick, both skipped wholesale.
- **Comments**: `#` to end of line, skipped wherever they appear.
- **Braces**: counted, ignoring anything inside quotes or comments.
- **`{` only opens a block if it is the last thing on its line.** Caddy requires
  this, so the distinction is exact rather than a guess. Without it,
  `redir https://x{uri}` reads as a nested block and a perfectly ordinary
  redirect gets pushed into the unmanageable pile.

### 7.4 Refusals — the whole file, or nothing

Two conditions abandon the entire file, returning `{ blocks: [], refusal }`:

| Condition | Message |
|---|---|
| single-site shorthand (an address with no `{ }`) | *"devtracker will not edit it, because splitting that form reliably needs a real Caddyfile parser. Wrap the site in braces and it can."* |
| braces do not balance | *"…so it cannot be read safely."* |

When it refuses, **nothing** is manageable — not "the parts it understood". Half
a model of a file that routes production traffic is worse than none.

### 7.5 Recognising a simple reverse proxy

A site block is offered to a recogniser. It becomes an `entry` only if it
contains **exactly one** `reverse_proxy` with **exactly one** argument, and every
other directive is either modelled or a plain single-line directive.

Modelled:

| Directive | Field |
|---|---|
| `reverse_proxy <upstream>` | `upstream: {host, port, scheme}` |
| `tls { dns <provider> <args> }` | `options.dnsChallenge` |
| `encode …` | `options.encode` |
| `reverse_proxy { header_up N V }` | `options.headerUp[]` |
| `reverse_proxy { transport http { tls_insecure_skip_verify } }` | `options.insecureUpstream` |
| upstream scheme `https://` | `options.httpsUpstream` |

Anything else that is a **bare line** goes into `unknownDirectives[]` and is
re-emitted verbatim. Anything with a **block of its own** makes the whole site
`kind: 'raw'` with a stated reason: `no reverse_proxy`, `more than one
reverse_proxy`, `contains a nested block`, `tls block does more than a DNS
challenge`, `unrecognised transport`, `reverse_proxy has matchers or several
upstreams`, `unrecognised reverse_proxy option: <name>`.

The reason is surfaced in the UI. "This block is advanced" is unhelpful; "this
block has a nested `handle`" is actionable.

**DNS-challenge args are kept verbatim.** They are usually a placeholder like
`{env.CLOUDFLARE_API_TOKEN}`, and re-quoting or expanding one would either break
it or write a secret into the file.

### 7.6 Rendering an entry

`renderEntry` produces the canonical shape used by the file:

```
domain.example.com, alt.example.com {
	tls {
		dns cloudflare {env.CLOUDFLARE_API_TOKEN}
	}

	encode gzip zstd
	<unknown directives, verbatim>
	reverse_proxy https://10.0.0.5:8080 {
		transport http {
			tls_insecure_skip_verify
		}
		header_up Host {host}
	}
}
```

- The `reverse_proxy` block is only emitted when something needs to go in it.
- IPv6 hosts are bracketed: `[fd00::1]:8080`.
- **The scheme is written exactly as it was found.** `http://x` and `x` mean the
  same thing to Caddy and adapt identically — but dropping an explicit `http://`
  would still silently edit a line nobody asked to change. The promise is that
  editing a port changes the port.

`DEFAULT_HEADER_UP` lists the three headers Caddy already sets
(`Host {host}`, `X-Forwarded-For {remote_host}`, `X-Forwarded-Proto {scheme}`)
and `headersAreDefaults()` reports when a block only restates them — so the UI
can offer to drop redundant lines without guessing.

---

## 8. The manager service

`CaddyManagerService` — the API-side owner of the Caddyfile.

### 8.1 No cache, ever

**The Caddyfile is the source of truth, not the database.** Every operation is a
full read-modify-write against the live file:

```
GET /caddyfile → parseCaddyfile → mutate one block → renderCaddyfile
              → PUT /caddyfile {text, sha256}      → re-list and return
```

There is no local copy to drift. That is what "always in sync" has to mean if it
is to mean anything — a cached list plus a Sync button is two sources of truth
and a bug waiting for a quiet afternoon.

The cost is one round trip per operation. On a file with fifteen sites that is
nothing, and it buys the absence of an entire class of problem.

The list is also polled every 30 s in the UI, because the file is the truth and
someone may edit it by hand on the box.

### 8.2 Every mutation goes through one function

`create`, `update` and `remove` all funnel through a private `mutate()`, so there
is exactly one place that writes and exactly one place that has to get the
concurrency right. It:

1. fetches the file (and refuses if the helper reports `writable: false`, with
   instructions);
2. refuses if the document has a `refusal`;
3. applies the caller's block-list mutation;
4. `PUT`s with the sha256 that came back from step 1;
5. maps `409` → conflict, non-200 → bad request with the helper's `detail`;
6. writes an audit entry;
7. returns a freshly listed state.

### 8.3 The three mutations

**create** — refuses a duplicate domain; appends `gap('\n')`, the entry, and
`gap('\n')`, so the file stays readable rather than accreting a wall of blocks.

**update** — finds the entry by case-insensitive domain, refuses a rename onto an
existing domain, and **carries `unknownDirectives` across from the block being
replaced**. Changing a port must not drop the header line above it.

**remove** — deletes the entry, and the `gap` that follows it *only if that gap
contains no `#`*. Otherwise the file grows a blank line every time something is
deleted — but a gap holding a comment is left alone, because someone wrote that
on purpose.

### 8.4 Validation

Domains are lowercased and trailing dots stripped. Rejected: a scheme
(`https://x` → *"Just the domain, without the scheme: x"*), a path, and anything
failing `^[a-z0-9*]([a-z0-9.-]*[a-z0-9])?$`. Upstream host: non-empty, no
whitespace, no `/` (*"the target is an address and port, not a URL"*). Port, when
present: 1–65535.

These reject what Caddy would reject, plus a few things Caddy would accept but
nobody means — most often from pasting a URL into a domain field.

---

## 9. Reading the running config (the import path)

`packages/core/src/caddy.ts` — `parseCaddyConfig(config): CaddyParseResult`.

### 9.1 JSON, never Caddyfile text

Input is Caddy's JSON: `caddy adapt`'s output, or a `GET` of the admin API's
`/config/`. Caddyfile text is deliberately unsupported here. Its grammar has
snippets, imports, environment substitution, matcher shorthands and site-block
inheritance, and a parser for it would be a half-correct reimplementation of
Caddy that silently disagrees with the real one. The JSON is what Caddy actually
runs.

*(§7's splitter is not a contradiction: it never claims to understand the file,
only to cut it at brace boundaries and hand the pieces back unchanged.)*

### 9.2 The two shapes that trip people up

1. **`servers` is a map with arbitrary keys.** `caddy adapt` happens to name them
   `srv0`, `srv1`…, but a hand-written config names them anything. Iterate
   `Object.entries()`; never index by name.
2. **Routes nest**, through `subroute` handlers, and a host matcher on an outer
   route applies to everything inside it. Reading only the top level finds
   *nothing* in a config with more than one site. The walk therefore carries
   matchers downward: an inner route with no host matcher inherits the outer
   one; paths accumulate. Depth is capped at 25 with a warning, guarding against
   a config that references itself.

### 9.3 Unresolvable rather than dropped

A domain that IS proxied somewhere unknown is a different fact from a domain that
is not proxied, so these are reported with a reason:

| Dial value | Reason |
|---|---|
| contains `{` | `address is a placeholder: …` — resolved by Caddy per-request; there is no static answer |
| starts with `unix/` | `unix socket: …` |
| empty | `empty upstream address` |
| `dynamic_upstreams` present | `upstreams are chosen dynamically at request time` |
| no `upstreams` at all | `the reverse proxy lists no upstream address` |

`parseDial` strips a scheme (`h2c://10.0.0.5:8080` is valid), handles bracketed
IPv6 (`[::1]:8080`) — a naive `split(':')` turns that into nonsense rather than
failing — and returns `{host, port}`.

A `reverse_proxy` with **no host matcher** answers for any domain, so there is
nothing to import from it: that produces a warning, not an upstream.

Domains served by something that is not a reverse proxy (a file server, a
redirect) are collected into `domainsWithoutProxy`, minus any domain that *was*
proxied somewhere else in the config.

---

## 10. Matching and applying

`CaddyService.preview()` turns parsed upstreams into **proposals**, and never
applies anything by itself. Same rule as the Proxmox sync, for the same reason:
an import that silently edits rows is indistinguishable from a bug until you
notice the wrong answer.

### 10.1 Loopback resolution

```ts
const LOOPBACK = new Set(['127.0.0.1', 'localhost', '::1', '0.0.0.0']);
```

A loopback upstream means "the machine Caddy runs on" — which is *not* the
machine devtracker runs on. Comparing it against a recorded `primary_ip` is
meaningless and actively dangerous: any host someone happened to record as
`127.0.0.1` would match every loopback upstream in the config.

When the config came from a helper, the helper's own hostname **is** that
machine, so loopback resolves to it. When the config was pasted, it cannot be
resolved and says so:

> `x.example.com` points at `127.0.0.1`, which means the machine Caddy itself
> runs on. Pasted configs do not say which machine that is — connect the Caddy
> host and read it from there, and this resolves.

### 10.2 The six proposal kinds

A host matches if its `primary_ip` equals the target, or its `hostname` matches
case-insensitively. Candidates are then narrowed by `host_port == upstreamPort`.

| Kind | Condition | Actionable |
|---|---|---|
| `already_set` | one candidate, and its `proxy_domain` is already this domain | no |
| `match` | one candidate | **yes** — set/change `proxy_domain` |
| `conflict` | more than one candidate | no — that is a port conflict; the registry reports it, and guessing here would pick one at random and write it in |
| `create_deployment` | the machine is recorded, nothing on that port | **yes** |
| `create_host` | neither the machine nor what runs on it is recorded | **yes** |
| `unresolvable` | §9.3, or unresolvable loopback | no |

`create_deployment` and `create_host` are distinct because they need different
amounts of invention: one adds a fact about a machine already described, the
other invents the machine as well. *(An earlier version said "NOT FOUND HERE" to
both, which made the screen useless.)*

Every proposal carries an `explanation` — a full sentence, generated server-side,
naming what will happen. The UI displays it; it does not compose it.

### 10.3 Applying

The client sends back the **domains** it chose. Each is matched again server-side
before anything is written.

> Trusting a list of deployment ids as sent would let a caller name any row and
> any domain, which is a write endpoint wearing an import's clothing.

For `match`: update `proxy_domain`, audit with before/after.

For the create kinds:

- Host: looked up by name if it was said to exist, otherwise inserted with
  `kind: 'other'` and `primary_ip` = the upstream address. **Named by its
  address**, because that is the only fact available — a name invented from the
  domain would imply we know what the machine is.
- A `hostIdByAddress` map deduplicates within one apply: two domains can share a
  machine, and without it applying both would create the host twice.
- Project: found by slug, else created, named from the domain's first label
  (`amp.example.com` → `amp`, except `www`, which keeps the whole domain).
- Deployment: inserted with `declaredState: 'running'` — Caddy is proxying to it,
  so something is answering there; recording it as `never_deployed` would be
  contradicted by the evidence that produced the row.

Returns `{applied, created: {hosts, projects, deployments}, skipped: string[]}`,
where each skip carries its explanation.

---

## 11. The API surface

Mounted at `/api/integrations/caddy`. All writes are **admin-only**; reading the
site list is not, because it *is* the screen.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/endpoints` | registered helpers |
| `POST` | `/endpoints` | **pair** — `{label, baseUrl, code}` → redeems the code, stores the token |
| `DELETE` | `/endpoints/{id}` | remove a helper and destroy its credential |
| `POST` | `/endpoints/{id}/test` | liveness + "Caddy is serving N proxied domains" |
| `POST` | `/endpoints/{id}/preview` | live read → proposals |
| `POST` | `/endpoints/{id}/apply` | `{domains: []}` — **re-reads and re-matches** first |
| `POST` | `/preview` | same, from a pasted config |
| `POST` | `/apply` | same, from a pasted config |
| `GET` | `/endpoints/{id}/sites` | the manager list, read live |
| `POST` | `/endpoints/{id}/sites` | add a site |
| `PUT` | `/endpoints/{id}/sites/{domain}` | change a site |
| `DELETE` | `/endpoints/{id}/sites/{domain}` | remove a site |

Each of the three mutations returns **the whole re-listed state**, so the client
never has to reconstruct what happened.

### 11.1 Deliberate schema looseness

The import body is `config: string | Record<string, unknown>` — validating
against a copy of Caddy's JSON schema would mean rejecting configs that use a
feature this app has never heard of. The parser already treats every field as
untrusted and reports what it could not read, which is the useful behaviour: a
config with one exotic handler should still yield the other nine domains.

`coerceCaddyConfig` accepts a string and `JSON.parse`s it, because people paste
`caddy adapt` output straight from a terminal. Failing on that with a schema
error would be correct and useless. Its error names the three valid sources and
says a Caddyfile must be adapted first.

### 11.2 Outbound-request safety

The API fetches from an address the operator typed. That is request forgery by
design — the same shape as the Proxmox endpoint, gated the same way: an
authenticated, admin-only action on a LAN tool, not a URL any unauthenticated
caller can influence.

Both directions are bounded: 10 s timeout for reads, 30 s for manager
operations, and an 8 MiB ceiling enforced by **streaming** the response
(`readCapped`) rather than `response.text()`, which would buffer whatever
arrives, however much that is.

Status codes are translated into sentences that say *which of the two machines is
broken* — a `401` is the token, a `502/503` is the helper reaching Caddy, a `404`
is a helper too old to have the route.

### 11.3 Storage

```sql
caddy_endpoints (
  id uuid primary key,
  label text not null,
  base_url text not null,            -- e.g. http://10.0.0.2:9776
  credential_slug text,              -- points at credentials.slug
  enabled boolean not null default true,
  last_fetch_at timestamptz,
  last_error text,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
)
```

**The token is never in this row.** It goes to the envelope-encrypted credential
store under `caddy:<endpoint-id>`, and the row holds a pointer. `hasToken` is
exposed to the UI; the token itself is never returned by any endpoint.

`last_error` / `last_fetch_at` are written on every fetch attempt, so the
Integrations screen can say what went wrong last time without another round trip.

---

## 12. The user interface

### 12.1 Caddy is a top-level tab

Not a section at the bottom of Integrations. It is a management surface with its
own CRUD, comparable to a DNS manager, and burying it inside a settings page
misrepresents what it is.

### 12.2 The manager

For each connected helper: a table of sites — domain, upstream, options — with
Edit and Remove per row and an Add button. Below it, the **advanced blocks** the
splitter refused to model, each shown with its reason and its exact text. They
are *shown, not hidden*: a manager that silently omits three of your sites is
lying about what the file contains.

The global options block is displayed too, so the UI can say what applies to
every site.

When the helper is read-only, the write controls are absent and the panel says so
along with what to do about it. When the file was refused wholesale (§7.4), the
refusal is the entire content of the panel.

The edit form covers exactly what the model covers: domain, extra addresses,
upstream scheme/host/port, `encode`, insecure-upstream, DNS challenge, and the
default `header_up` set. Every save returns the re-listed state and reports
`Saved <domain>. Caddy reloaded.`

### 12.3 Connecting a helper

Two fields — address and six digits — plus the exact command to run on the Caddy
host. The Pair button is disabled until both are filled.

The form states the most dangerous available mistake, at the point the decision
is made: *do not point this at Caddy's admin API*, which has no authentication.

### 12.4 The domain picker

On a project's detail page: choose one domain from the live site list, stored in
`projects.caddy_domain` as **plain text**, not a foreign key.

A cascade would silently forget the association when a domain leaves the
Caddyfile. Plain text means a removed domain leaves the record alone and the UI
reports *"no longer served by Caddy"* — a stale value you can see and fix beats a
silent deletion.

The picker renders nothing at all when no helper is connected and no domain is
recorded: there is no reason to advertise a feature that is not set up.

---

## 13. Installation

### 13.1 One line, served by devtracker itself

```bash
curl -fsSL http://<devtracker>/helper/install.sh | sudo bash
```

devtracker serves the install script and the binaries from its own API,
substituting its own origin into the script (`__BASE_URL__`). A LAN tool should
not need the internet to set up a LAN helper, and while the repository is private
a GitHub URL 404s for everyone.

These routes are unauthenticated on purpose: the binary is not a secret, and
`curl` cannot log in.

The script: refuses without root, maps `uname -m` to
amd64/arm64/armv7, downloads to a temp file, **runs `version` before replacing
anything** so a bad download cannot take out a working install, then `mv`s it
into place — a running binary cannot be overwritten in place, but moving replaces
the directory entry, which works while the service is up. If the service is
already running it restarts it and reports the new version. Otherwise it prints
the next command and stops.

**It installs only the binary.** It starts nothing and changes nothing.

### 13.2 `caddy-reader setup`

Three ways to drive it, all ending in the same place:

```bash
sudo caddy-reader setup                        # wizard; Enter accepts every default
sudo caddy-reader setup --yes                  # no questions, everything detected
sudo caddy-reader setup --writable --listen …  # no questions, everything stated
```

Naming any flag implies `--yes`: you know what you want, so skip the questions.
The wizard defaults to **what is already configured**, so re-running it confirms
rather than resets.

**Step 1 — detect.** `systemctl show caddy --property=ExecStart,EnvironmentFiles,Environment,LoadState`
yields the Caddyfile path (from `--config`), the caddy binary, and — critically —
Caddy's *own* environment files and inline `Environment=` pairs. It counts site
blocks in the Caddyfile and reports which `{env.*}` placeholders the config
expands, noting that Caddy resolves them, not this service.

**Step 2 — confirm.** Caddyfile, admin API, listen address, and the one question
that matters:

```
May devtracker change your sites?
  no    a read-only list. The service gets no privileges at all.
  yes   add, edit and remove sites from the browser. The service runs
        as the owner of your Caddy config so it can replace the file.
```

**Step 3 — write.** `/etc/caddy-reader.env` (mode `0600`, holding
`READER_TOKEN`) and `/etc/systemd/system/caddy-reader.service` (mode `0644`).
An existing token is **kept**, so re-running setup does not disconnect
devtracker. A new one is 32 bytes from `crypto/rand`, hex-encoded.

The unit also carries `EnvironmentFile=-<each of Caddy's own env files>` (the
`-` makes each optional). Since §6.2 these are no longer *required* — Caddy
expands its own placeholders — but they are kept: they cost nothing and make the
service's view of the environment match Caddy's.

**Step 4 — check it works.** This matters more than the questions.

> An earlier version reported "Running" while the helper could not validate a
> single change.

With `--writable`, it takes the Caddyfile Caddy is *already serving* and posts it
through the real code path. Caddy re-applies its own configuration: the whole
route is proven and nothing changes. Then it runs `caddy-reader verify` and
surfaces only the *names* of failed checks, because verify's full output is a
page of JSON and would bury the summary.

**Step 5 — pair.** Opens a pairing window and prints a panel with the address
(derived from `ip -4 route get`), the spaced code, and the access mode.

### 13.3 The systemd unit

Common to both modes: `Type=simple`, `After=network-online.target caddy.service`,
`Restart=on-failure`, `RestartSec=5s`, and a full hardening set —
`NoNewPrivileges`, `PrivateTmp`, `PrivateDevices`, `ProtectHome`,
`ProtectKernelTunables`, `ProtectKernelModules`, `ProtectControlGroups`,
`ProtectClock`, `ProtectHostname`, `RestrictNamespaces`, `RestrictRealtime`,
`RestrictSUIDSGID`, `LockPersonality`, `MemoryDenyWriteExecute`,
`SystemCallArchitectures=native`,
`SystemCallFilter=@system-service` / `~@privileged @resources @obsolete`,
`RestrictAddressFamilies=AF_INET AF_INET6`, `UMask=0077`.

**The token is in `EnvironmentFile=`, never in `ExecStart=`.** A unit file is
world-readable and `systemctl cat` prints it. (The same rule applies everywhere:
never put a secret in argv, where `ps` shows it.)

### 13.4 `verify`

An end-to-end assertion that the deployed thing has the properties claimed:

- something answers, and `/health` reports Caddy reachable;
- a request with **no** token is `401`;
- a request with the **wrong** token is `401`;
- admin paths are **not forwarded** (`404`);
- `POST`/`PUT`/`DELETE` to `/config` are `405`;
- a request **with** the token is `200`;
- the response is **filtered, not raw** — it fails loudly if `--raw` is on.

### 13.5 `uninstall`

Confirmed by typing a **freshly generated six-digit number**, not "yes". A prompt
you can clear by holding Enter is not a confirmation, and this one takes a
reverse proxy's management off the machine. `--yes` skips it for scripts.

Removes the unit, the env file, the pairing file and the binary; `daemon-reload`s;
counts leftover Caddyfile backups and says they were left in place. States
plainly that Caddy, the Caddyfile and the sites are untouched, and that
devtracker will now report this host as unreachable.

---

## 14. Threat model

| Attacker position | What they get | Why |
|---|---|---|
| On the LAN, no token | `GET /health` only. Everything else is `401`. | Bearer token on every other route, constant-time compared. |
| Holds a **read-only** helper's token | The projection: domains and dial addresses. | No basic-auth hashes, no upstream credentials, no ACME EAB keys — allowlist, §5. |
| Holds a **writable** helper's token | Can replace the Caddyfile. | This is the granted capability; that is why it is off by default and stated in `ExecStart`. |
| Racing the pairing window | ~5 guesses in 15 minutes against 10⁶ codes, then the window burns. | §4. |
| Compromised devtracker database | Endpoint rows, no tokens. | Tokens live in the envelope-encrypted credential store; ciphertext is useless without the KEK, which is in `.env` and never committed. |
| Anything at all | **Cannot** reach Caddy's admin API. | It never left loopback. The helper's only admin paths are two compile-time constants. |

### 14.1 Non-goals, stated

- No TLS between devtracker and the helper. This is a LAN tool; the token is the
  boundary. `https://` base URLs work if you provide the certificate.
- No rate limiting on `/config`. It is behind a 256-bit bearer token, which has
  nothing to guess.
- The helper does not authenticate *Caddy*. If something else is listening on the
  admin address, the helper reports "that address is probably not Caddy's admin
  API" and stops.

---

## 15. Rebuild order

1. **`packages/core/caddyfile.ts`** — splitter, recogniser, renderer. Write the
   byte-exact round-trip test first; it is the invariant everything else leans
   on.
2. **`packages/core/caddy.ts`** — `parseCaddyConfig` over adapted JSON. Test with
   a real `caddy adapt` fixture containing nested subroutes.
3. **`helper/projection.go`** — the allowlist. Then the cross-language test
   asserting it and step 2 agree.
4. **`helper/main.go`** — routes, auth, timeouts, limits, startup checks.
5. **`helper/caddyfile.go`** — GET, and the PUT sequence in §6.1. Post
   `text/caddyfile`. Do not adapt in the helper.
6. **`helper/pairing.go`** — six digits, expiry, attempt counting, burn on use.
7. **API services** — `CaddyManagerService` (no cache, single `mutate`),
   `CaddyService` (pairing redemption, live read, matching, apply).
8. **Routes** — admin-gated writes, loose config schema, streamed reads.
9. **UI** — Caddy tab, manager table with advanced blocks visible, connect form,
   domain picker.
10. **`setup.go` / `install.sh` / `verify.go`** — last, because they are only
    worth writing once there is something to verify.

### 15.1 The five that will bite

1. **Post `text/caddyfile` to `/load`.** Adapting in the helper needs every
   secret the config references and produces `API token "" appears invalid`.
2. **`caddy reload --address` prepends its own scheme**, producing
   `http:///host:port/load`. Post the config directly.
3. **Caddy's admin API enforces an Origin allowlist when not on loopback**, which
   breaks `caddy reload` from off-box. Posting directly sends no Origin.
4. **`{` only opens a block when it ends its line.** Otherwise `{uri}` in a
   `redir` makes an ordinary site unmanageable.
5. **Validate before touching disk.** Any other order leaves a window where the
   file and the running proxy disagree, and a rollback path that must itself be
   correct under failure.