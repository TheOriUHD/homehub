# Stubbed browser walk

A headless-Chromium walk of the built frontend that needs no session and
touches no data: `web/dist` is served from disk through Playwright request
routing, and every `/api/*` call is answered from examples generated out of
`api/openapi.yaml` (`gen-stubs.py`), with hand overrides in `walk.mjs` for the
identity (`/auth/me`, a grant on everything) and the assistant catalog.

It exists because the auto-mode classifier blocks minting a session row, which
the handoff's cookie-authenticated walk depends on. It is also the only way
to render a *secure* origin here: pick `https://homehub.walk` (default) for
what the owner sees through Caddy, or `ORIGIN=http://192.168.20.26:8080` for
the insecure LAN origin, where `crypto.randomUUID` and friends do not exist.

    SCRATCH=$(mktemp -d)
    cd $SCRATCH && npm init -y >/dev/null && npm i playwright-core
    npx playwright-core install chromium        # matching build into ~/.cache/ms-playwright
    python3 /root/homehub/references/browser-walk/gen-stubs.py stubs.json
    cp /root/homehub/references/browser-walk/walk.mjs .
    node walk.mjs /root/homehub/web/dist out    # THEME=dark, VIEWPORT=1440x520 as needed

The walk as committed probes the assistant model menu (widget and page):
which element is *painted* at the menu's centre — judged with pointer events
forced back on, since Radix turns them off on `<body>` while a menu is open
and a hidden menu still takes the click — plus Escape and item selection.
Adapt the probe functions for the next question; the harness part is generic.
Screenshots land in the output directory.
