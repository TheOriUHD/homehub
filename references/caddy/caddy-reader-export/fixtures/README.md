# Projection contract fixtures

The helper sends a **projection** of Caddy's running config, not the config
itself: an allowlist of the fields a client needs to answer "which domain points
at which upstream", with everything else dropped.

That creates a seam. The allowlist lives in Go (`projection.go`); your parser
lives in whatever HomeHub is written in. **If the two drift, the failure mode is
a domain quietly going missing from an import** — no error, no warning, just a
site that stopped appearing.

These fixtures close that seam. Every expected output here was produced by
running the shipped binary, not by hand:

```bash
caddy-reader project < caddy-adapted.json
```

## Files

| File | What it is |
|---|---|
| `caddy-source.Caddyfile` | The Caddyfile that produced the fixture. Kept so you can regenerate it with `caddy adapt` and see that nothing was hand-edited. |
| `caddy-adapted.json` | Real `caddy adapt` output. Compact, exactly as Caddy emits it. |
| `caddy-adapted.projected.json` | What the helper sends for that config. Pretty-printed for reading; compare parsed, not as text. |
| `secrets-input.json` | A config containing four things that must never cross the network. |
| `secrets-input.projected.json` | The same config after projection, with all four gone. |

## The two assertions worth writing

**1. Your parser agrees with the projection.**

Parse `caddy-adapted.json` and `caddy-adapted.projected.json` with the *same*
parser. They must yield identical results:

- the same set of `{domain, upstreamHost, upstreamPort}` upstreams;
- the same "served but not proxied" domains;
- **no warnings from the projected one** — a warning there means your parser
  wants a field the projection drops.

Expected upstreams from this fixture:

| domain | upstream | note |
|---|---|---|
| `tracker.example.com` | `10.0.0.5:8080` | |
| `media.example.com` | `10.0.0.6:8096` | |
| `api.example.com` | `[fd00::1]:9000` | path `/admin/*`; bracketed IPv6 |
| `api.example.com` | `10.0.0.7:3000` | path `/v1/*` |

Served but **not** proxied: `files.example.com` (a file server) and
`old.example.com` (a redirect).

This fixture is chosen to break naive parsers. It exercises:

- **`servers` is a map** — the key is `srv0` here, but treat it as arbitrary and
  iterate entries. A hand-written config names it anything.
- **Routes nest through `subroute`**, and the host matcher sits on the *outer*
  route. A top-level-only read finds **nothing** in this file.
- **Two upstreams on one domain**, distinguished only by path matchers, nested
  two subroutes deep.
- **Bracketed IPv6** (`[fd00::1]:9000`), which a naive `split(':')` turns into
  nonsense rather than failing.
- **Non-proxy handlers** (`file_server`, `static_response`) that must be reported
  as served-not-proxied rather than dropped or mistaken for upstreams.

**2. The projection strips secrets.**

`secrets-input.json` carries four things that genuinely appear in real Caddy
configs. Assert none of them survive into `secrets-input.projected.json`:

| Secret | Where it appears |
|---|---|
| `$2a$14$aVeryRealBcryptHash` | a `basic_auth` account password hash |
| `super-secret-upstream-key` | a credential set on a header going upstream |
| `secret-hmac` | an ACME external-account-binding MAC key |
| `must-not-appear` | credentials inside `dynamic_upstreams` |

And assert the things it *is* for do survive: `secret.example.com`,
`10.0.0.9:3000`, and `dynamic_upstreams` reduced to `{}` — presence is preserved
so your parser can still report "upstreams are chosen at request time, so this
cannot be resolved statically".

Note that `handler: "authentication"` survives while its `providers` block does
not. That is the allowlist working as intended: the handler *name* is a
recognised key, its payload is not.

## Regenerating

```bash
caddy adapt --config caddy-source.Caddyfile --pretty > /tmp/adapted.json
caddy-reader project < caddy-adapted.json | jq . > caddy-adapted.projected.json
caddy-reader project < secrets-input.json | jq . > secrets-input.projected.json
```

Compare **parsed structures, not text**. Go's `encoding/json` sorts map keys, so
key order in these files is stable but is not part of the contract.
