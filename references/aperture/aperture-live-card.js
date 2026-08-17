/*
 * Aperture Live Card — instant WebRTC live view for Aperture cameras on a Home
 * Assistant dashboard.
 *
 * The browser talks directly to Aperture (nvrd), which brokers go2rtc's WebSocket
 * signalling — the same path Aperture's own web UI uses. WebRTC first (lowest
 * latency, host candidates only on the LAN), MSE/fMP4 fallback on the same socket
 * for when WebRTC can't traverse (e.g. remote through a reverse proxy).
 *
 * Single file, plain JS, no build step (dist/ is the source).
 *
 * Phase 2 — persistent warm connection. A module-level pool keeps one live
 * connection (and its still-playing <video>) per stream, keyed by url|camera|token.
 * Home Assistant re-renders the dashboard constantly and recreates card elements;
 * instead of tearing the stream down and renegotiating (which re-pays the camera's
 * keyframe wait — up to a whole GOP — every time), a re-mounted card RE-PARENTS the
 * already-playing node into itself. So the keyframe wait happens once, ever, and
 * switching views / waking the tab is instant. This is the fix for cheap cameras
 * whose keyframe interval can't be lowered.
 *
 * Config:
 *   type: custom:aperture-live-card
 *   url: https://aperture.example.com     # Aperture base URL (same scheme as HA)
 *   token: <the Live key from Aperture>    # Settings → Integrations → Live View Card
 *   camera: zimmer_ph                      # the camera's handle (or its id)
 *   substream: false                       # optional low-res
 *   name: Zimmer                           # optional overlay label
 *   aspect_ratio: "16:9"                   # optional
 *   debug: false                           # optional: log the 4-stage timing
 */

const CARD_VERSION = "0.2.0";

// go2rtc's documented WebSocket message protocol, spoken directly:
//   client -> {type:"webrtc/offer"|"webrtc/candidate"|"mse", value}
//   server -> {type:"webrtc/answer"|"webrtc/candidate"|"mse"|"error", value}
// Codecs offered on the MSE fallback path (H.264/H.265 + common audio).
const MSE_CODECS = [
  "avc1.640029", "avc1.64002A", "avc1.640033", // H.264
  "hvc1.1.6.L153.B0",                           // H.265
  "mp4a.40.2", "mp4a.40.5", "flac", "opus",     // audio
];

// How long the picture may be frozen (currentTime not advancing) before we treat the
// connection as dead and reconnect.
const STALL_MS = 6000;

// How long a warm connection is kept alive after the last card showing it goes away.
// Covers dashboard re-renders and view switches (which detach then re-attach within
// milliseconds) and brief backgrounding; a genuinely-closed dashboard releases after
// this. On an always-on kiosk the holder never leaves, so the stream simply stays warm.
const GRACE_MS = 5 * 60 * 1000;

/* ------------------------------------------------------------------ signalling */

// LiveEngine owns one connection to one stream: WebRTC with an MSE fallback, a
// stall watchdog, and reconnect with capped backoff. Ported from Aperture's own
// livestream.ts so behaviour matches the first-party UI exactly.
class LiveEngine {
  constructor(video, url, onState, metrics) {
    this.video = video;
    this.url = url;
    this.onState = onState;
    this.metrics = metrics || (() => {});
    this.mode = "webrtc";
    this.destroyed = false;
    this.playing = false;
    this.retries = 0;
    this.lastTime = -1;
    this.lastAdvanceAt = 0;
    this._t = {};
  }

  start() {
    this._startWatchdog();
    this.onState("connecting");
    this._mark("start");
    this._connectWebRTC();
  }

  destroy() {
    this.destroyed = true;
    if (this._watchdog) clearInterval(this._watchdog);
    if (this._reconnectTimer) clearTimeout(this._reconnectTimer);
    this._teardown();
  }

  _mark(stage) {
    const now = performance.now();
    this._t[stage] = now;
    this.metrics(stage, now, this._t);
  }

  _teardown() {
    if (this._timer) clearTimeout(this._timer);
    this._timer = undefined;
    const ws = this.ws, pc = this.pc;
    this.ws = undefined;
    this.pc = undefined;
    try { ws && ws.close(); } catch (e) { /* ignore */ }
    try { pc && pc.close(); } catch (e) { /* ignore */ }
  }

  _markPlaying() {
    this.playing = true;
    this.retries = 0;
    this.lastAdvanceAt = Date.now();
    if (this._timer) clearTimeout(this._timer);
    this.onState("playing");
  }

  _reconnect() {
    if (this.destroyed || this._reconnectTimer) return;
    this._teardown();
    this.playing = false;
    this.mode = "webrtc";
    this.lastAdvanceAt = Date.now();
    this.onState("connecting");
    const delay = Math.min(250 * 2 ** this.retries, 5000) + Math.random() * 200;
    this.retries = Math.min(this.retries + 1, 6);
    this._reconnectTimer = setTimeout(() => {
      this._reconnectTimer = undefined;
      if (!this.destroyed) { this._t = {}; this._mark("start"); this._connectWebRTC(); }
    }, delay);
  }

  _startWatchdog() {
    if (this._watchdog) return;
    this.lastTime = -1;
    this.lastAdvanceAt = Date.now();
    this._watchdog = setInterval(() => {
      if (this.destroyed || !this.playing) return;
      const t = this.video.currentTime;
      if (t !== this.lastTime) {
        this.lastTime = t;
        this.lastAdvanceAt = Date.now();
      } else if (!this.video.paused && Date.now() - this.lastAdvanceAt > STALL_MS) {
        this._reconnect();
      }
    }, 2500);
  }

  // Called on foreground/resume: nudge autoplay and reconnect if the picture is frozen.
  resumeCheck() {
    if (this.destroyed) return;
    this.video.play().catch(() => {});
    if (!this.playing) return;
    const t = this.video.currentTime;
    setTimeout(() => {
      if (this.destroyed || !this.playing) return;
      if (this.video.currentTime === t && !this.video.paused) this._reconnect();
    }, 700);
  }

  _fail() {
    if (this.destroyed) return;
    if (this.mode === "webrtc") {
      this._teardown();
      this.mode = "mse";
      this._connectMSE();
    } else {
      this.onState("error");
    }
  }

  _connectWebRTC() {
    const pc = new RTCPeerConnection({ iceServers: [], bundlePolicy: "max-bundle" });
    this.pc = pc;
    const stream = new MediaStream();
    pc.addTransceiver("video", { direction: "recvonly" });
    pc.addTransceiver("audio", { direction: "recvonly" });

    pc.ontrack = (ev) => {
      if (pc !== this.pc) return;
      stream.addTrack(ev.track);
      this.video.srcObject = stream;
      this.video.play().catch(() => {});
    };
    pc.onconnectionstatechange = () => {
      if (pc !== this.pc) return;
      const st = pc.connectionState;
      if (st === "connected") {
        this._mark("ice");
        this._markPlaying();
      } else if (st === "failed") {
        if (this.playing) this._reconnect(); else this._fail();
      } else if (st === "closed" && this.playing) {
        this._reconnect();
      }
    };

    const ws = new WebSocket(this.url);
    this.ws = ws;
    ws.binaryType = "arraybuffer";

    pc.onicecandidate = (ev) => {
      if (ev.candidate && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: "webrtc/candidate", value: ev.candidate.candidate }));
      }
    };
    ws.onopen = async () => {
      if (ws !== this.ws) return;
      try {
        const offer = await pc.createOffer();
        await pc.setLocalDescription(offer);
        ws.send(JSON.stringify({ type: "webrtc/offer", value: offer.sdp }));
        this._mark("offer");
      } catch (e) { this._fail(); }
    };
    ws.onmessage = (ev) => {
      if (ws !== this.ws || typeof ev.data !== "string") return;
      let msg;
      try { msg = JSON.parse(ev.data); } catch (e) { return; }
      if (msg.type === "webrtc/answer") {
        this._mark("answer");
        pc.setRemoteDescription({ type: "answer", sdp: msg.value }).catch(() => this._fail());
      } else if (msg.type === "webrtc/candidate" && msg.value) {
        pc.addIceCandidate({ candidate: msg.value, sdpMid: "0" }).catch(() => {});
      } else if (msg.type === "error") {
        this._fail();
      }
    };
    ws.onerror = () => { if (ws === this.ws) this._fail(); };

    this._timer = setTimeout(() => {
      if (pc === this.pc && pc.connectionState !== "connected") this._fail();
    }, 5000);
  }

  _connectMSE() {
    this.onState("connecting");
    this.video.srcObject = null;
    const MS = window.ManagedMediaSource || window.MediaSource;
    const ws = new WebSocket(this.url);
    this.ws = ws;
    ws.binaryType = "arraybuffer";

    let sb, started = false;
    const queue = [];
    const flush = () => {
      if (!sb || sb.updating || queue.length === 0) return;
      try { sb.appendBuffer(queue.shift()); } catch (e) { queue.shift(); }
    };

    ws.onopen = () => {
      if (ws !== this.ws) return;
      const ms = new MS();
      ms.addEventListener("sourceopen", () => {
        if (this.video.src) URL.revokeObjectURL(this.video.src);
        const codecs = MSE_CODECS.filter((c) => MS.isTypeSupported(`video/mp4; codecs="${c}"`)).join();
        ws.send(JSON.stringify({ type: "mse", value: codecs }));
      }, { once: true });

      if (window.ManagedMediaSource) {
        this.video.disableRemotePlayback = true;
        this.video.srcObject = ms;
      } else {
        this.video.src = URL.createObjectURL(ms);
      }
      this.video.play().catch(() => {});

      ws.onmessage = (ev) => {
        if (typeof ev.data === "string") {
          let msg;
          try { msg = JSON.parse(ev.data); } catch (e) { return; }
          if (msg.type === "mse") {
            try {
              sb = ms.addSourceBuffer(msg.value);
              sb.mode = "segments";
              sb.addEventListener("updateend", () => {
                flush();
                if (sb && !sb.updating && sb.buffered.length) {
                  const end = sb.buffered.end(sb.buffered.length - 1);
                  const start0 = sb.buffered.start(0);
                  const liveStart = end - 5;
                  if (liveStart > start0) { try { sb.remove(start0, liveStart); } catch (e) {} }
                  if (this.video.currentTime < liveStart) this.video.currentTime = end - 0.4;
                }
              });
            } catch (e) { this.onState("error"); }
          } else if (msg.type === "error") {
            this.onState("error");
          }
          return;
        }
        queue.push(ev.data);
        flush();
        if (!started) { started = true; this._mark("ice"); this._markPlaying(); this.video.play().catch(() => {}); }
      };
    };
    ws.onerror = () => { if (ws === this.ws && !started) this.onState("error"); };
    ws.onclose = () => { if (ws === this.ws && !this.destroyed && !started) this.onState("error"); };
    this._timer = setTimeout(() => { if (!started && !this.destroyed) this.onState("error"); }, 9000);
  }
}

/* ------------------------------------------------- warm-connection pool (Phase 2) */

// One entry per distinct stream. It owns the LiveEngine AND the DOM subtree (ha-card +
// <video> + overlay). A card element does not build its own video — it borrows the
// entry's, which keeps playing across element teardown/recreate because moving a live
// <video> node with appendChild does not reload it.
const POOL = new Map();

function streamKey(c) {
  return `${String(c.url).replace(/\/+$/, "")}|${c.camera}|${c.substream ? 1 : 0}|${c.token}`;
}

function wsUrl(c) {
  const base = String(c.url).replace(/\/+$/, "").replace(/^http/i, "ws"); // http→ws, https→wss
  const q = c.substream ? "&q=sub" : "";
  return `${base}/api/v1/cameras/${encodeURIComponent(c.camera)}/ws?token=${encodeURIComponent(c.token)}${q}`;
}

function buildContainer(c) {
  const root = document.createElement("ha-card");
  root.style.cssText = "overflow:hidden;position:relative;";

  const wrap = document.createElement("div");
  const ar = (c.aspect_ratio || "16:9").split(":");
  const pad = ar.length === 2 ? (Number(ar[1]) / Number(ar[0])) * 100 : 56.25;
  wrap.style.cssText = `position:relative;width:100%;padding-bottom:${pad}%;background:#0b0e13;`;

  const video = document.createElement("video");
  video.muted = true;
  video.autoplay = true;
  video.playsInline = true;
  video.setAttribute("playsinline", "");
  video.setAttribute("muted", "");
  video.setAttribute("disableRemotePlayback", "");
  video.style.cssText = "position:absolute;inset:0;width:100%;height:100%;object-fit:cover;background:#0b0e13;";

  const overlay = document.createElement("div");
  overlay.style.cssText =
    "position:absolute;inset:0;display:flex;align-items:flex-end;justify-content:space-between;" +
    "padding:8px 10px;pointer-events:none;font:12px/1.4 system-ui,sans-serif;color:#fff;" +
    "text-shadow:0 1px 3px rgba(0,0,0,.6);";
  const label = document.createElement("span");
  label.textContent = c.name || "";
  const dot = document.createElement("span");
  dot.style.cssText = "width:9px;height:9px;border-radius:50%;background:#e6b45a;flex:none;transition:background .2s;";
  overlay.appendChild(label);
  overlay.appendChild(dot);

  wrap.appendChild(video);
  wrap.appendChild(overlay);
  root.appendChild(wrap);
  return { container: root, video, dot };
}

// acquire returns the warm entry for a stream, creating and starting it on first use.
function acquire(key, c) {
  let e = POOL.get(key);
  if (e) {
    if (e.idle) { clearTimeout(e.idle); e.idle = null; }
    return e;
  }
  const { container, video, dot } = buildContainer(c);
  const setState = (st) => {
    dot.style.background = st === "playing" ? "#3fce7c" : st === "error" ? "#f0494e" : "#e6b45a";
  };

  const debug = !!c.debug, cam = c.camera, t0 = performance.now();
  const metrics = (stage, now, t) => {
    if (!debug) return;
    const d = (a, b) => (t[a] != null && t[b] != null ? `${(t[b] - t[a]).toFixed(0)}ms` : "—");
    if (stage === "offer") console.log(`[aperture ${cam}] offer sent @ ${(now - t0).toFixed(0)}ms`);
    if (stage === "answer") console.log(`[aperture ${cam}] answer +${d("offer", "answer")} (rtt)`);
    if (stage === "ice") console.log(`[aperture ${cam}] connected +${d("answer", "ice")} (after answer)`);
  };
  if (debug && "requestVideoFrameCallback" in video) {
    const started = performance.now();
    video.requestVideoFrameCallback(() => {
      console.log(`[aperture ${cam}] FIRST FRAME @ ${(performance.now() - started).toFixed(0)}ms from connect`);
    });
  }

  const engine = new LiveEngine(video, wsUrl(c), setState, metrics);
  e = { engine, container, video, dot, holder: null, idle: null };
  POOL.set(key, e);
  engine.start();
  return e;
}

// Nudge every warm stream when the tab/app returns to the foreground (iOS especially
// suspends WebRTC while backgrounded).
if (typeof document !== "undefined" && !window.__apertureLiveVis) {
  window.__apertureLiveVis = true;
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState !== "visible") return;
    POOL.forEach((e) => { try { e.engine.resumeCheck(); } catch (err) { /* ignore */ } });
  });
}

/* ------------------------------------------------------------------- the card */

class ApertureLiveCard extends HTMLElement {
  setConfig(config) {
    if (!config || !config.url) throw new Error("aperture-live-card: `url` is required (your Aperture base URL).");
    if (!config.camera) throw new Error("aperture-live-card: `camera` is required (the camera's handle, e.g. zimmer_ph).");
    if (!config.token) throw new Error("aperture-live-card: `token` is required (the Live key from Aperture → Settings → Integrations).");
    this._config = config;
  }

  set hass(hass) {
    // The card renders live video directly from Aperture and uses no HA entities, so
    // a hass update must NOT restart the stream — just keep the reference.
    this._hass = hass;
  }

  getCardSize() {
    return 4;
  }

  static getStubConfig() {
    return { url: "https://aperture.example.com", token: "", camera: "", name: "" };
  }

  connectedCallback() {
    if (!this._config) return;
    const key = streamKey(this._config);
    const e = acquire(key, this._config);
    this._key = key;
    this._entry = e;
    e.holder = this;
    if (e.idle) { clearTimeout(e.idle); e.idle = null; }
    // Re-parent the (already-playing) subtree into this card. Moving a live <video>
    // node does not reload it, so this is instant and seamless.
    this.appendChild(e.container);
    try { e.engine.resumeCheck(); } catch (err) { /* ignore */ }
  }

  disconnectedCallback() {
    const e = this._entry;
    if (!e) return;
    // Only the current holder schedules teardown; if a newer card element already took
    // the stream over (a dashboard re-render), this is a stale unmount — leave it warm.
    if (e.holder !== this) return;
    e.holder = null;
    if (e.idle) clearTimeout(e.idle);
    const key = this._key;
    e.idle = setTimeout(() => {
      if (POOL.get(key) !== e || e.holder) return;
      POOL.delete(key);
      try { e.engine.destroy(); } catch (err) { /* ignore */ }
      try { e.container.remove(); } catch (err) { /* ignore */ }
    }, GRACE_MS);
  }
}

if (!customElements.get("aperture-live-card")) {
  customElements.define("aperture-live-card", ApertureLiveCard);
}

// Register in the Lovelace card picker.
window.customCards = window.customCards || [];
window.customCards.push({
  type: "aperture-live-card",
  name: "Aperture Live Card",
  description: "Instant WebRTC live view for an Aperture camera.",
  preview: false,
  documentationURL: "https://github.com/yourname/aperture-live-card",
});

console.info(`%c APERTURE-LIVE-CARD %c ${CARD_VERSION} `, "background:#1b9df0;color:#fff;border-radius:3px 0 0 3px", "background:#0b0e13;color:#fff;border-radius:0 3px 3px 0");