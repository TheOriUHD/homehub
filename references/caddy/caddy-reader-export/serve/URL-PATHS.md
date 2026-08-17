# Serving the installer and binaries

`install.sh` is a **template**. The placeholder `__BASE_URL__` appears once, on
the `BASE_URL=` line, and is left in place in the copy in this directory —
substitute at serve time, not at build time.

## What the serving app must do

```
GET  /helper/install.sh                     → this file, with __BASE_URL__
                                              replaced by the request's own origin
                                              Content-Type: text/x-shellscript; charset=utf-8

GET  /helper/caddy-reader-linux-amd64       → the binary
GET  /helper/caddy-reader-linux-arm64       → the binary
GET  /helper/caddy-reader-linux-armv7       → the binary
                                              Content-Type: application/octet-stream

GET  /helper/SHA256SUMS                     → the checksum file from build.sh release
```

The script builds its download URL as:

```
${BASE_URL%/}/helper/caddy-reader-linux-${ARCH}
```

so the `/helper/` prefix and the exact filenames are part of the contract. Serve
those five paths and the one-liner works.

### Substitution

Replace the literal string `__BASE_URL__` with the origin of the incoming
request — not a configured hostname. devtracker does:

```ts
const origin = new URL(c.req.url).origin;   // e.g. http://10.0.0.20:8090
return c.text(script.replace('__BASE_URL__', origin), 200, {
  'content-type': 'text/x-shellscript; charset=utf-8',
});
```

Using the *request's* origin rather than configuration means the download comes
back to the address the operator actually reached, which is the one that is
demonstrably routable from where they are standing. A configured
`PUBLIC_URL` is a second source of truth that is wrong exactly when it matters —
someone on the LAN reaching the app by IP while the config names a domain that
only resolves externally.

If the placeholder is left unsubstituted, the script falls back to a GitHub
release URL. Keep or change that fallback (`REPO=` near the top) to suit; if
HomeHub publishes no releases, point it at your own or delete the branch.

## These routes must be unauthenticated

The binary is not a secret, and **`curl` cannot log in.** The whole point of the
one-liner is that someone SSH'd into the Caddy host can run it without first
obtaining a session from a web app on another machine.

Nothing sensitive is exposed by serving them: the install script contains no
credentials, and the binary is the same one anyone can build from `src/`.

What *is* protected is everything the helper does after installation — that is
what the pairing code and the bearer token are for.

## Why serve them at all, rather than linking to GitHub

1. **A LAN tool should not need the internet to set up a LAN helper.** The Caddy
   host may deliberately have no outbound route.
2. **A private repository 404s** for an unauthenticated `curl`, which produces a
   baffling failure at the worst moment. (This is not hypothetical — it is how
   devtracker discovered the problem.)
3. **Version coupling.** The app serves the helper build it was tested against,
   so "update the helper" and "update the app" cannot drift into an untested
   combination.

## Re-running is the upgrade path

The script detects an existing service and restarts it after replacing the
binary, printing the new version. It does **not** re-run setup, so the token and
the read-only/writable choice survive an upgrade and the client stays paired.

It also refuses to install a binary that does not run: it downloads to a temp
file, executes `version`, and only then `mv`s it into place. A bad download
cannot take out a working install.

(`mv` rather than `cp`: a running binary cannot be overwritten in place —
`ETXTBSY` — but moving replaces the directory entry, which works while the
service is up.)
