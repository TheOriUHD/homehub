# Integrating Aperture into HomeHub

**Audience:** the engineer/agent building HomeHub. You need no prior knowledge of Aperture.
**Status:** everything described here works against Aperture **today**. No changes to Aperture
are required.
**Verified:** 2026-08-17, against the live server.

---

## 1. What you're integrating

**Aperture** is a self-hosted NVR (security camera recorder) running at
**`https://aperture.smarthomegup.de`**. It records 4 IP cameras continuously and exposes a
versioned REST API plus a live-video WebSocket.

HomeHub's "Aperture" tab needs three things:

| # | Feature | How | Aperture changes needed |
|---|---|---|---|
| 1 | **Live feeds for all cameras** | Browser connects directly to Aperture over WebSocket/WebRTC | None |
| 2 | **Metrics** (storage, camera health, bitrate) | HomeHub **backend** calls Aperture's REST API | None |
| 3 | **Link out to the full app** | `https://aperture.smarthomegup.de` | None |

Scope confirmed with the owner: **live view only** — a grid of all cameras, click to
expand/fullscreen. **No timeline, no playback, no export.** A simpler version of Aperture's own
dashboard.

---

## 2. The one architectural rule that shapes everything

> **Aperture has NO CORS support. A browser on a different origin cannot call its REST API.**

This is deliberate — Aperture's own UI is same-origin and never needed it. Verified live:

```
OPTIONS /api/v1/system/storage   →  405 Method Not Allowed      (preflight fails)
GET     /api/v1/system/health    →  200, but NO Access-Control-Allow-Origin header
```

So JavaScript running on `homehub.*` cannot read any JSON from `aperture.*`.

**But WebSocket connections are not subject to CORS.** That is the whole trick, and it's already
proven in production by Aperture's Home Assistant card.

### Therefore:

```
┌──────────────────────┐
│  HomeHub browser     │
│                      │
│  ① video tiles ──────┼──── wss:// ────────────────┐   (no CORS on WebSockets)
│                      │                            │
│  ② metrics ──────────┼── HomeHub's own API        │
└──────────────────────┘         │                  │
                                 ▼                  ▼
                       ┌──────────────────┐  ┌──────────────────────────┐
                       │ HomeHub backend  │──►│  Aperture                │
                       │ (holds the token)│   │ aperture.smarthomegup.de │
                       └──────────────────┘   └──────────────────────────┘
                          server-to-server
                          (no CORS involved)
```

- **Video** goes browser → Aperture **directly**. Never proxy video through HomeHub's backend —
  you'd add latency and destroy the whole point.
- **Everything else (JSON)** goes through HomeHub's backend.

---

## 3. Setup: create the API token (5 minutes, one time)

1. Open `https://aperture.smarthomegup.de`, sign in as an **administrator**.
2. **Settings → Integrations → API tokens**.
3. Name it `homehub`, set an expiry (or "No expiry").
4. Tick these scopes:
   - **`media:live`** — required to open live streams and mint stream tokens
   - **`system:read`** — camera health, bitrate, system status
   - **`storage:read`** — disk usage and per-camera storage
   - *(optional)* **`cameras:read`** — the camera list with full settings
   - *(optional)* **`events:read`** — motion events + the real-time change feed
5. Leave "Restrict to specific cameras" **unticked** (all cameras).
6. **Create token** → copy the `apt_…` secret. **It is shown exactly once.**

Store it as a secret in HomeHub's backend (env var / secret manager). **It must never reach the
browser.**

> The token inherits its creator's role. `/system/status` and `/system/metrics` additionally
> require the owner to be an **admin**, so create it from an admin account.

### Using it

```
Authorization: Bearer apt_xxxxxxxxxxxxxxxxxxxxxxxx
```

Base URL: **`https://aperture.smarthomegup.de/api/v1`**

---

## 4. Part 1 — Metrics (HomeHub backend → Aperture)

All of these are plain `GET` + Bearer token. Responses below are **real captured output**,
trimmed.

### 4.1 Camera health — `GET /api/v1/system/status`
*Scope: `system:read` (admin-owned token). **This is the single most useful endpoint for a
dashboard.***

```json
{
  "cameras": [
    {
      "cameraId": "e9f1fc8e89ca92e22a653c3bb540ea67",
      "name": "Zimmer PH",
      "enabled": true,
      "recordMode": "continuous",
      "recordingExpected": true,
      "recordingOk": true,
      "liveOk": true,
      "coveragePercent": 93,
      "lastSegmentTs": 1786987953965,
      "restartsToday": 0,
      "watchdogStale": false,
      "watchdogLastSeen": 1786987934016
    }
  ]
}
```

Field meanings that matter:
- **`recordingOk`** — is this camera actually recording *right now*. The headline health signal.
- **`liveOk`** — is a live stream currently obtainable.
- **`coveragePercent`** — how much of the recent window has footage. Below ~90% means gaps.
- **`restartsToday`** — reconnect churn. A high number means a flaky camera. (Values in the
  dozens are normal for some cameras and not an outage on their own.)
- **`watchdogStale`** — `true` means Aperture believes this camera has stopped delivering.
- **`lastSegmentTs`** — epoch **milliseconds** of the most recent recorded segment.

**Suggested HomeHub status pill:** `watchdogStale || !recordingOk` → red; `coveragePercent < 90`
→ amber; else green.

### 4.2 Storage — `GET /api/v1/system/storage`
*Scope: `storage:read`*

```json
{
  "capBytes": 53687091200,
  "allocatedBytes": 53687091200,
  "appBytes": 1403524452,
  "dataDir": "/data",
  "defaultPath": "/recordings",
  "cameras": [
    {
      "cameraId": "e9f1fc8e89ca92e22a653c3bb540ea67",
      "name": "Zimmer PH",
      "bytes": 29143159904,
      "count": 6573,
      "oldestTs": 1786922215000,
      "newestTs": 1786987944055
    }
  ]
}
```

- `capBytes` — the storage ceiling Aperture is allowed to use.
- `bytes` / `count` — per-camera footage size and segment count.
- `oldestTs` → `newestTs` — **how far back that camera's history reaches** (epoch ms). Nice as
  "3 days of history" on a tile.

> The system runs permanently at its storage cap by design — old footage is continuously evicted.
> **Do not alarm on "storage full"**; it's the normal steady state. Alarm on the *retention
> window shrinking* (`newestTs - oldestTs` dropping) instead.

### 4.3 Throughput — `GET /api/v1/system/bitrate?minutes=30`
*Scope: `system:read`*

```json
{ "buckets": [ { "ts": 1786986120000, "bps": 5868608 }, { "ts": 1786986180000, "bps": 5741244 } ] }
```

One bucket per minute, bits per second. Feeds a sparkline directly.

### 4.4 Camera list — `GET /api/v1/cameras`
*Scope: `cameras:read`*

> ### ⚠️ SECURITY — READ THIS
> Each camera object contains **`rtspMainUrl` and `rtspSubUrl` with the camera's username and
> password in plaintext**, in the form
> `rtsp://<user>:<password>@<camera-ip>:554/stream1`.
>
> **Never pass the raw response to HomeHub's frontend.** Whitelist fields on the backend.
> Leaking these hands out full, permanent control of the cameras themselves.

Safe subset to forward:

```jsonc
{ "id": "...", "name": "Zimmer PH", "enabled": true, "recordMode": "continuous" }
```

Other useful fields: `retentionDays`, `motionDetection`, `sortOrder` (Aperture's own display
order — respect it so both apps agree), `locationId`.

### 4.5 Other endpoints

| Endpoint | Scope | Notes |
|---|---|---|
| `GET /system/health` | **public** | Liveness. No auth. Ideal for HomeHub's "is Aperture up" check. |
| `GET /system/version` | **public** | Version string. |
| `GET /capabilities` | authed | Feature flags, `schemaVersion`, API version. Use to future-proof. |
| `GET /system/metrics` | `system:read` (admin) | Deeper internal metrics. |
| `GET /events?limit=50` | `events:read` | Recent motion events. |
| `GET /events/{id}/thumb.jpg` | `events:read` | Event thumbnail (JPEG bytes). |
| `GET /cameras/{id}/snapshot.jpg` | `media:read` | Still frame (JPEG bytes). |

**Snapshots and thumbnails are image bytes behind auth.** A browser `<img src>` **cannot**
authenticate cross-origin (no way to set a header; the session cookie is `SameSite=Lax`). If you
want stills, **proxy them through HomeHub's backend** and serve them from HomeHub's own origin.

### 4.6 Polling and rate limits

The token bucket for API tokens is **burst 40, refill 20/minute** — per token.

Recommended backend polling, comfortably inside that:

| Data | Interval |
|---|---|
| `/system/status` | 10–15 s |
| `/system/storage` | 60 s |
| `/system/bitrate` | 60 s |
| `/cameras` | 5 min (or on change-feed signal) |

Cache in HomeHub's backend and serve many HomeHub users from one cached copy. **Never** poll
per-browser-client. A `429` response means you've exceeded the bucket — back off.

---

## 5. Part 2 — Live camera feeds (browser → Aperture, direct)

### 5.1 Credential strategy — use short-lived tokens

The browser needs *some* credential to open the WebSocket. Three options, best first:

#### ✅ Recommended: backend mints a short-lived stream token per camera

**Verified working.** HomeHub's backend exchanges its long-lived token for a **5-minute,
single-camera** token, and hands *that* to the browser.

```http
GET /api/v1/cameras/{CAMERA_ID}/stream-token
Authorization: Bearer apt_...
```
```json
{
  "token": "<short-lived, 5 min, this camera only>",
  "wsUrl": "/api/v1/cameras/e9f1fc8e.../ws?token=...",
  "hasSub": true
}
```

- The browser never holds a long-lived credential.
- A leaked token expires in 5 minutes and only opens **one** camera.
- **Once the WebSocket is established it stays open** past the 5 minutes — expiry only gates
  *new* connections. So refresh the token on reconnect, not on a timer.

> ⚠️ **`/stream-token` requires the RAW camera ID — a name handle returns 404.** (Handles work on
> the `/ws` route only.) Get IDs from `/cameras` or `/system/status`.

#### ⚠️ Alternative: the shared "Live key"

Aperture can generate a single self-managed key (**Settings → Integrations → Live View Card**)
that opens **any** camera and never expires. Simpler, but anyone who can read HomeHub's frontend
gets permanent live access to every camera. Acceptable for a single-user dashboard; **not**
recommended for HomeHub given it has a proper backend. It can be re-rolled at any time.

#### ⚠️ Not recommended: putting the `apt_` token in the browser
It carries metrics and storage scopes too. Don't.

### 5.2 The WebSocket protocol

```
wss://aperture.smarthomegup.de/api/v1/cameras/{ID_OR_HANDLE}/ws?token=<token>[&q=sub]
```

- `{ID_OR_HANDLE}` — the raw ID, **or** a readable handle (`zimmer_ph`, `a1_mini`).
- `q=sub` — request the low-resolution substream. **Use this for grid tiles**; drop it when a
  tile is expanded to fullscreen.

Aperture proxies this to its internal streaming engine (go2rtc). The protocol is JSON:

**Client → server**
```jsonc
{ "type": "webrtc/offer",     "value": "<SDP offer>" }
{ "type": "webrtc/candidate", "value": "<ICE candidate>" }
{ "type": "mse",              "value": "<comma-separated codec list>" }   // fallback
```

**Server → client**
```jsonc
{ "type": "webrtc/answer",    "value": "<SDP answer>" }
{ "type": "webrtc/candidate", "value": "<ICE candidate>" }
{ "type": "mse",              "value": "<chosen mime type>" }
{ "type": "error",            "value": "<message>" }
```
Binary frames on the socket are fMP4 segments (MSE path only).

### 5.3 How to connect (the sequence that works)

1. Create `RTCPeerConnection({ iceServers: [], bundlePolicy: "max-bundle" })`.
   **`iceServers: []` is intentional** — host candidates only, direct on the LAN. No STUN/TURN.
2. `addTransceiver("video", {direction:"recvonly"})` and the same for `"audio"`.
3. Open the WebSocket. On open: `createOffer()` → `setLocalDescription()` → send `webrtc/offer`.
4. Trickle ICE: send each `onicecandidate` as `webrtc/candidate`.
5. On `webrtc/answer` → `setRemoteDescription()`.
6. On `ontrack` → add to a `MediaStream`, assign to `video.srcObject`, call `video.play()`.
7. If WebRTC fails or times out (~5 s), reconnect on the **same URL** and send `{type:"mse"}`
   instead — fMP4 arrives as binary frames for MediaSource.

### 5.4 Non-negotiable `<video>` attributes

```js
video.muted = true;         video.setAttribute("muted", "");
video.autoplay = true;
video.playsInline = true;   video.setAttribute("playsinline", "");
```

All three are **required** or iOS silently refuses to start playback. Set both the property and
the attribute. The card is muted anyway — cameras using AAC audio (which WebRTC can't carry) then
simply play video-only.

### 5.5 ⚠️ Attach the `<video>` to the DOM *before* you connect

**This exact mistake cost a debugging session on the Home Assistant card.** If you negotiate
against a `<video>` element that isn't in the document yet, you can end up with a card that
renders its chrome but never shows a picture, with no recovery path.

**Rule: insert the `<video>` into the live DOM first, then start the connection.**

### 5.6 Keeping tiles instant

Cameras have a keyframe roughly every 2 seconds, and **WebRTC cannot paint anything until a
keyframe arrives**. A fresh connection therefore takes 0–2 s to show a picture, at random.

If HomeHub tears down and rebuilds connections on every re-render (React remounts, tab switches,
route changes), every single one pays that penalty again — the feeds will feel randomly slow.

**Keep connections alive across re-renders.** Hold them in a module-level cache keyed by camera,
and on re-mount **move the existing `<video>` node** into the new container rather than creating
a new one — moving a playing `<video>` does not reload it. Release a stream only after it's been
unused for a few minutes.

Also: nudge playback on `visibilitychange` when the tab becomes visible again — iOS suspends
WebRTC in the background.

### 5.7 Reference implementation — don't write this from scratch

A complete, working, dependency-free implementation exists at:

```
/home/philipp/aperture-live-card/dist/aperture-live-card.js
```

It's ~450 lines of plain JS containing exactly what you need:
- **`class LiveEngine`** — the full WebRTC + MSE fallback + stall watchdog + reconnect-with-backoff
  logic. **Port this directly.** It is battle-tested against this exact server.
- The warm-connection pool (§5.6) and the first-paint guard.
- `test/lifecycle.mjs` — a headless jsdom test asserting the video is attached before connecting
  and that re-renders reuse the connection.

Ignore its Home Assistant card wrapper; take `LiveEngine` and the pooling.

### 5.8 Current cameras

Verified 2026-08-17. **Fetch these at runtime — do not hardcode.**

| Name | Handle | ID |
|---|---|---|
| 3D-Drucker | `3d_drucker` | `b2a26f0042e85ef0685168eab03f4283` |
| A1 Mini | `a1_mini` | `4dc6bf3f3699967fcfd2abc16cfee84c` |
| Zimmer PH | `zimmer_ph` | `e9f1fc8e89ca92e22a653c3bb540ea67` |
| Schlafzimmer | `schlafzimmer` | `03274ba04e9b42c6755e4376ddd17caa` |

> **Handles are derived from camera names** (lowercase, non-alphanumerics → `_`). **Renaming a
> camera changes its handle** and breaks anything referencing the old one. **Use raw IDs for
> anything persisted**; treat handles as display convenience only. (A camera rename has already
> silently broken an integration once.)

---

## 6. Part 3 — The link

```html
<a href="https://aperture.smarthomegup.de" target="_blank" rel="noopener noreferrer">
  Open Aperture
</a>
```

Aperture has its own authentication; users land on its login or straight into the app if they
have a session. Nothing to integrate.

---

## 7. Real-time updates (optional, better than polling)

Aperture exposes a **Server-Sent Events** change feed. HomeHub's **backend** can hold one
connection and push updates to its clients — no polling, near-instant.

```http
GET /api/v1/feed
Authorization: Bearer apt_...
Accept: text/event-stream
```

Real captured output:

```
: connected

data: {"id":18,"type":"camera.state","ts":1786981364834,"camera":"4dc6bf3f...","data":{"state":"online"}}
data: {"id":12,"type":"camera.state","ts":1786874069876,"camera":"03274ba0...","data":{"state":"online"}}
```

Useful event types: `camera.state` (online/offline), `event.created` / `event.closed` (motion),
`detection.created`, `camera.create` / `camera.update` / `camera.delete`, `storage.allocate`,
`alarm.state`, `export.progress`.

Each carries a monotonic `id` for resume. Events are filtered to what the token may see.

**Recommended:** subscribe to the feed for camera online/offline and motion, and keep a slow
poll of `/system/status` (~30 s) as a safety net in case the stream drops.

---

## 8. Gotchas — read before writing code

1. **No CORS.** All JSON goes through HomeHub's backend. Only WebSockets work from the browser.
2. **`/cameras` leaks camera passwords** in `rtspMainUrl`/`rtspSubUrl`. Whitelist fields before
   forwarding. (§4.4)
3. **Attach `<video>` to the DOM before connecting.** (§5.5)
4. **HTTPS ↔ WSS must match.** HomeHub on `https://` can only open `wss://`. Mixed content is
   blocked silently by the browser — the stream simply never starts, with no obvious error.
   Both are behind HTTPS today, so this works; don't "simplify" to `http://` in dev against the
   production host.
5. **`/stream-token` needs the raw ID; handles 404 there.** Handles work on `/ws` only. (§5.1)
6. **Unknown JSON fields are rejected.** Aperture's write endpoints reject any field they don't
   recognise with a hard `400`. Don't echo objects back — send only documented fields. (Read-only
   integrations are unaffected.)
7. **Rate limit: burst 40, refill 20/min per token.** Cache server-side. `429` = back off. (§4.6)
8. **Camera renames change handles.** Persist IDs. (§5.8)
9. **Timestamps are epoch milliseconds** (except a few internal ones). `lastSegmentTs`,
   `oldestTs`, `newestTs`, `ts` are all ms.
10. **Storage sits at its cap permanently by design.** Don't alarm on it. (§4.2)
11. **Substream for grids.** Use `&q=sub` for tiles; full stream only when expanded. Four
    full-resolution WebRTC streams in one browser tab is a lot of decoding.
12. **Don't proxy video** through HomeHub's backend.

---

## 9. Error reference

| Status | Meaning | Fix |
|---|---|---|
| `401` on `/ws` | Bad/expired/wrong-camera token | Mint a fresh stream token; check it's for this camera |
| `404` on `/ws` | **Camera handle doesn't exist** | Verify against `/cameras`; a rename likely changed it |
| `404` on `/stream-token` | You used a handle | Use the raw camera ID |
| `401` on REST | Missing/invalid Bearer token | Check the header and that the token wasn't revoked |
| `403` + `forbidden.scope` | Token lacks the scope | Recreate the token with the scopes in §3 |
| `403` + `forbidden.camera` | Token is camera-restricted | Recreate without camera restrictions |
| `429` | Rate limited | Back off; cache server-side |
| `405` on `OPTIONS` | You tried a cross-origin fetch | Go through HomeHub's backend — there is no CORS |

Errors are JSON: `{"code":"forbidden.scope","error":"..."}`.

---

## 10. Build checklist

**Backend**
- [ ] Store the `apt_` token as a secret; never expose it
- [ ] `GET /system/status` every 10–15 s → cache
- [ ] `GET /system/storage` + `/system/bitrate` every 60 s → cache
- [ ] `GET /cameras` → **whitelist fields** → cache
- [ ] Endpoint: camera list for the frontend (id, name, enabled, health)
- [ ] Endpoint: mint a stream token for camera `{id}` (calls `/cameras/{id}/stream-token`)
- [ ] *(optional)* subscribe to `/feed` and push to clients
- [ ] *(optional)* proxy `/cameras/{id}/snapshot.jpg` if you want stills
- [ ] Handle Aperture being unreachable — degrade the tab, don't break HomeHub

**Frontend**
- [ ] Grid of camera tiles, all live, using `&q=sub`
- [ ] Port `LiveEngine` from the reference implementation (§5.7)
- [ ] Attach `<video>` before connecting (§5.5)
- [ ] `muted` + `autoplay` + `playsinline`, property **and** attribute
- [ ] Module-level connection cache; move the `<video>` node on re-render (§5.6)
- [ ] Click → expand/fullscreen, switching to the full-resolution stream
- [ ] `visibilitychange` handler to nudge playback
- [ ] Per-tile status indicator from `/system/status`
- [ ] Metrics panel: storage, retention window, bitrate sparkline, camera health
- [ ] "Open Aperture" link
- [ ] Show a real reason on failure — don't leave a silent black tile

---

## 11. Further reading (in the Aperture repo)

- **`ONBOARDING.md`** — full technical context on Aperture itself.
- **`features.md`** — what Aperture does, in plain language.
- **`docs/`** — Home Assistant and detection documentation.
- **`/home/philipp/aperture-live-card/`** — the reference live-view implementation (separate repo).

**If something here is wrong or missing, say so** — Aperture's own maintainer can change the
server side. In particular, if HomeHub ever needs to call the REST API **directly from the
browser**, CORS support can be added to Aperture (origin-allowlisted); it's roughly half a day's
work and is not currently implemented.