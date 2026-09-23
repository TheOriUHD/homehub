# Stubbed browser walk

A headless-Chromium walk of the built frontend that needs no session and
touches no data: `web/dist` is served from disk through Playwright request
routing, and every `/api/*` call is answered from examples generated out of
`api/openapi.yaml` (`gen-stubs.py`), with per-walk overrides for the identity
and whatever else the page under test needs.

It exists because the auto-mode classifier blocks minting a session row, which
the handoff's cookie-authenticated walk depends on. It is also the only way
to render a *secure* origin here: pick `https://homehub.walk` (default) for
what the owner sees through Caddy, or `ORIGIN=http://192.168.20.26:8080` for
the insecure LAN origin, where `crypto.randomUUID` and friends do not exist.

    SCRATCH=$(mktemp -d)
    cd $SCRATCH && npm init -y >/dev/null && npm i playwright-core
    npx playwright-core install chromium        # matching build into ~/.cache/ms-playwright
    python3 /root/homehub/references/browser-walk/gen-stubs.py stubs.json
    cp /root/homehub/references/browser-walk/*.mjs .
    node walk-model-picker.mjs /root/homehub/web/dist out
    node walk-local-guide.mjs /root/homehub/web/dist out

`THEME=dark` and `VIEWPORT=1440x520` apply to any walk. Screenshots land in
the output directory, and each walk prints its findings plus every console
error and unmatched API route as JSON.

## The pieces

- **`harness.mjs`** — the reusable half. `openApp()` serves the bundle, stubs
  the API, signs in with a grant on everything, and skips the first-run
  language prompt; `report()` prints and closes. Nothing specific to one
  question belongs here.
- **`gen-stubs.py`** — walks the OpenAPI spec and writes an example response
  for every route into `stubs.json`.
- **`walk-model-picker.mjs`** — the assistant's model menu: which element is
  actually *painted* at the menu's centre (judged with pointer events forced
  back on, since Radix disables them on `<body>` while a menu is open and a
  hidden menu still takes the click), Escape, selection, and the model a first
  turn posts.
- **`walk-local-guide.mjs`** — the local-model setup guide across all six
  runtimes in both modes, catching missing translation keys (which render as
  the key itself) and checking the fill-in button writes the form.
- **`walk-host-keys.mjs`** — SSH host-key recovery in its four states: pin
  changed, trusted, address unreachable, and only PatchPilot's own pin behind.
  Its stub messages are copied from the handler, because a stub that
  paraphrases the server cannot catch the two of them saying the same thing.

- **`walk-live-updates.mjs`** — push-driven refresh: that the calendar page
  subscribes, and that both a `calendar` broadcast and an `agent` announcement
  actually refetch what is on screen. It replaces `window.WebSocket` with a
  fake it can push through, which needs the `OPEN` constant — without it the
  client compares two `undefined`s, believes the socket is open, and sends on
  a null.

Write a new `walk-*.mjs` per question rather than growing an existing one.

**The stubs are spec-shaped, not server-shaped.** The generator emits `[]` for
an array and a filled value for every field, which is not what Go sends: a nil
slice marshals to `null`, and an `omitempty` field is simply absent. A walk
that only ever sees tidy data will pass while the real page throws. When a
response carries a list or an optional field, stub the awkward shape on purpose
— `null`, missing, empty — because that is the one the client will meet.

## What it has caught

The widget's model menu opening behind the panel; a `crypto.randomUUID` crash
that took down the whole assistant pane on the LAN origin; the model field's
label rendering as an i18next error string, broken since the model picker
landed; a host-key dialog that stated the same sentence twice; and a dead end
where only PatchPilot's pin was stale and the dialog offered no way out.

What it missed, once: a `null` list from the server where the spec said array,
which blanked the SSH Hosts page. The stubs were the reason, and the note above
is the fix.
