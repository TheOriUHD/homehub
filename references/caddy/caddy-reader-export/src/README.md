# caddy-reader

A read-only window onto a Caddy instance, for devtracker to read domains through.

A single **static binary**. No interpreter, no shared libraries, no config file,
no runtime to keep patched — one file you drop in `/usr/local/bin` and control
with `systemctl` like anything else.

The source is one small Go package, meant to be read in full before you run it:
it goes on the machine that fronts all your traffic, so "trust me" is not good
enough.

---

## Why this exists rather than just connecting to Caddy

Caddy's admin API listens on `127.0.0.1:2019` and has **no authentication at
all**. Anything that can reach it can `POST /load` and replace your entire
configuration — route your domains anywhere, serve any file on the box, and get
valid TLS certificates while doing it.

So the fix for "devtracker is on another machine" is **not** `admin 0.0.0.0:2019`.
That turns your reverse proxy into an open control plane for the whole LAN.

This runs on the Caddy host instead, keeps the admin API on localhost where it
belongs, and exposes one narrow thing.

## What it can and cannot do

| | |
|---|---|
| **Can** | answer `GET /config` with a projection of the running config, to a caller holding the token |
| **Can** | answer `GET /health` with whether it and Caddy are alive |
| **Cannot** | write to Caddy. The only request it makes is `GET {admin}/config/`, and that path is a **constant in the source**, never taken from the request |
| **Cannot** | act as a proxy. It forwards no paths, methods, headers or bodies |
| **Cannot** | leak your secrets — see below |

`POST`, `PUT`, `PATCH` and `DELETE` are refused with 405. That is a property of
the code, not a configuration setting.

## It does not send your whole config

A Caddy config contains secrets:

- `basic_auth` password hashes
- credentials you set on headers going upstream (`header_up Authorization ...`)
- ACME external-account-binding HMAC keys

None of that is needed to answer *"which domain points at which upstream"*, so
none of it is sent. What crosses the network is a projection containing only:

```
apps.http.servers.<name>.listen
apps.http.servers.<name>.routes[].match[].host
apps.http.servers.<name>.routes[].match[].path
apps.http.servers.<name>.routes[].handle[].handler
apps.http.servers.<name>.routes[].handle[].routes      (recursively)
apps.http.servers.<name>.routes[].handle[].upstreams[].dial
apps.http.servers.<name>.routes[].handle[].dynamic_upstreams   (presence only)
```

It is an **allowlist**, not a list of things to strip. A denylist silently leaks
whatever Caddy adds in the next version; an allowlist silently drops it, and
dropping something is a visible bug rather than an invisible disclosure.

`--raw` turns the projection off and sends everything. It exists for debugging,
and devtracker shows a warning whenever it sees a helper using it.

## It never needs your secrets

A change is sent to Caddy as **Caddyfile text**, not as adapted JSON, so **Caddy**
parses it — in its own process, with its own environment.

That matters if your config says `{env.CLOUDFLARE_API_TOKEN}`. Adapting the file
here would produce JSON containing an *empty* token, and Caddy would then reject
it as invalid rather than missing. Handing over the text means the secret never
leaves the process that already has it, this service needs no copy, and it does
not need a `caddy` binary either.

Verified by running the helper with a completely empty environment and editing a
site whose certificate comes from a DNS challenge.

---

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/TheOriUHD/devtracker/main/helper/install.sh | sudo bash
```

That only puts the binary in place. Then:

```bash
sudo caddy-reader setup
```

It reads your running Caddy for its own answers, asks four questions you can
press Enter through, and ends with an address and a **six-digit code**. Put those
into devtracker and you are connected.

The one question worth reading is whether devtracker may **change** your sites.
No gives a read-only list from a service with no privileges at all. Yes is what
the manager needs.

Skip the questions entirely:

```bash
sudo caddy-reader setup --yes                    everything detected
sudo caddy-reader setup --writable               …and allow changes
sudo caddy-reader setup --read-only --listen 0.0.0.0:9776
```

### Running it

```bash
caddy-reader status          what it is doing, and whether it can change anything
sudo caddy-reader start
sudo caddy-reader stop
sudo caddy-reader restart
caddy-reader verify          check it does what it claims
sudo caddy-reader pair       a fresh six-digit code
sudo caddy-reader uninstall  remove it; your Caddy config is untouched
```

### The six-digit code

The code is **not** the credential. Six digits is a million possibilities, which
is nothing to a script — so it lasts fifteen minutes, works once, and cancels
itself after five wrong guesses. What it buys is a single exchange: devtracker
presents the code, the helper returns a real 256-bit token, and the code is
burned.

That is there so nobody has to read a 64-character secret off a terminal and
retype it into a browser — which people get wrong, and which tempts everyone into
copying it somewhere more convenient than it should be.

### Building it yourself

```bash
git clone https://github.com/TheOriUHD/devtracker && cd devtracker/helper
./build.sh            # this machine
./build.sh release    # amd64, arm64, armv7, with checksums
```

`CGO_ENABLED=0`, so the result is static and runs on any Linux of that
architecture. CI fails the build if that stops being true.

> **"Go is not installed"** and you know it is? The official tarball unpacks to
> `/usr/local/go` and does not touch `PATH`. `build.sh` looks there anyway; to
> fix it properly:
>
> ```bash
> echo 'export PATH=$PATH:/usr/local/go/bin' | sudo tee /etc/profile.d/go.sh
> ```

Each binary records the commit it was built from:

```bash
go version -m /usr/local/bin/caddy-reader | grep vcs
```

### Upgrading

Re-run the install line. A running binary cannot be overwritten in place, so the
script lands it beside the old one and moves it into position, then restarts the
service. Your token survives, so devtracker stays connected.

### If Caddy runs in a container and the helper does not

The admin API is inside the container, so `--admin http://127.0.0.1:2019` finds
nothing. Either use `--network container:caddy` as above, or publish the admin
port to loopback only — `-p 127.0.0.1:2019:2019` — never to `0.0.0.0`.

---

## Connect it

In devtracker: **Integrations → Connect a Caddy host**. Give it the helper's
address (`http://<caddy-host>:9776`) and the token.

## Check it yourself

Everything above is a claim. This checks the claims on your machine:

```bash
./verify.sh                 # a helper on this host
./verify.sh 10.0.0.2:9776   # one somewhere else
```

```
  PASS  a request with no token is refused         401
  PASS  a POST is refused even with a valid token  405
  PASS  admin path /load is not forwarded          404
  PASS  it is filtering, not forwarding wholesale  filtered
  PASS  credentials in your config stay on this host 0
  ——    size of config vs what is sent             1019 bytes -> 726 bytes

  All 14 checks passed. It is doing what it says.
```

It exits non-zero if anything fails, and the failures are the interesting part:
run a helper with `--raw` and it reports the leak and tells you not to connect it.

To see the filtered payload yourself, rather than a summary of it:

```bash
curl -s localhost:2019/config/ | caddy-reader project
```

That is the same projection the running service uses — not a description of it.
Diff it against the raw `curl -s localhost:2019/config/` and the difference is
exactly what stays on this host.

## Options

| Flag | Default | |
|---|---|---|
| `--token` | — | **Required.** `READER_TOKEN` also works. |
| `--listen` | `0.0.0.0:9776` | `READER_LISTEN` |
| `--admin` | `http://127.0.0.1:2019` | `CADDY_ADMIN` |
| `--raw` | off | Send the whole config, secrets included. Debugging only. |
| `--writable` | off | Let devtracker change the Caddyfile. |
| `--caddyfile` | `/etc/caddy/Caddyfile` | `CADDYFILE` |

Subcommands: `install`, `verify`, `project` (stdin → projected stdout), `paths`,
`version`.

## The honest caveats

- **Plain HTTP.** The token crosses your LAN in the clear, as does the projected
  config. That is the same posture as devtracker itself, which is a LAN tool that
  expects a reverse proxy in front if you want TLS. Put this behind one too if
  that matters to you.
- **The token is the whole boundary.** Anyone holding it can read which domains
  point where. That is not nothing, but it is a great deal less than the admin
  API, which is the point.
- **Read-only means read-only.** If you want devtracker to *manage* Caddy, this
  is not that, and deliberately so.
