// LiveEngine: the browser side of Aperture's live-video WebSocket, built
// from references/aperture/aperture.md §5.2–5.6. WebRTC first (host
// candidates only — direct on the LAN), MSE fallback, reconnect with
// backoff, and a warm-connection pool so React re-renders never pay the
// 0–2 s keyframe penalty again.
//
// Two rules from the doc are load-bearing here:
//  - the <video> element must be IN the document before negotiation starts;
//  - a failure must surface a readable reason — never a silent black tile.

export type LiveState =
  | { phase: "connecting" }
  | { phase: "playing"; transport: "webrtc" | "mse" }
  | { phase: "error"; reason: string; retryInMs: number };

type StateListener = (state: LiveState) => void;

/** Fetches a fresh signed WebSocket URL — called on every (re)connect,
 * because stream tokens only gate NEW connections. useSub asks for the
 * low-resolution substream when the camera offers one. */
export type WsUrlProvider = (useSub: boolean) => Promise<string>;

const WEBRTC_TIMEOUT_MS = 5_000;
const MSE_FIRST_DATA_TIMEOUT_MS = 9_000;
const STALL_TIMEOUT_MS = 8_000;
const MAX_BACKOFF_MS = 15_000;
/** Seconds of MSE history kept behind the live edge; everything older is
 * removed so the SourceBuffer never hits the browser's quota. */
const MSE_LIVE_WINDOW_S = 5;

/** iPhone Safari only exposes MSE through ManagedMediaSource (iOS 17+),
 * which attaches via srcObject instead of an object URL. */
type MSWindow = Window & { ManagedMediaSource?: typeof MediaSource };
const managedMediaSource = () =>
  typeof window === "undefined" ? undefined : (window as unknown as MSWindow).ManagedMediaSource;
const MSE_CODECS =
  'avc1.640029,avc1.64002A,avc1.640033,hvc1.1.6.L153.B0,mp4a.40.2,mp4a.40.5,flac,opus';

export class LiveEngine {
  readonly video: HTMLVideoElement;
  private getWsUrl: WsUrlProvider;
  /** Grid tiles prefer the substream; some cameras have a broken or absent
   * substream, so after BOTH transports fail on sub the engine downgrades
   * to the main stream before reporting an error. */
  private preferSub: boolean;
  private subDowngraded = false;
  private listeners = new Set<StateListener>();
  private state: LiveState = { phase: "connecting" };

  private ws: WebSocket | null = null;
  private pc: RTCPeerConnection | null = null;
  private mediaSource: MediaSource | null = null;
  private sourceBuffer: SourceBuffer | null = null;
  private mseQueue: ArrayBuffer[] = [];

  private transport: "webrtc" | "mse" = "webrtc";
  private webrtcTimer: number | null = null;
  private stallTimer: number | null = null;
  private retryTimer: number | null = null;
  private lastTime = -1;
  private attempts = 0;
  private stopped = false;

  constructor(getWsUrl: WsUrlProvider, preferSub: boolean) {
    this.getWsUrl = getWsUrl;
    this.preferSub = preferSub;
    this.video = document.createElement("video");
    // All three, property AND attribute, or iOS silently refuses playback.
    this.video.muted = true;
    this.video.setAttribute("muted", "");
    this.video.autoplay = true;
    this.video.setAttribute("autoplay", "");
    this.video.playsInline = true;
    this.video.setAttribute("playsinline", "");
    this.video.className = "size-full object-contain";
  }

  onState(listener: StateListener): () => void {
    this.listeners.add(listener);
    listener(this.state);
    return () => this.listeners.delete(listener);
  }

  currentState(): LiveState {
    return this.state;
  }

  private setState(state: LiveState) {
    this.state = state;
    for (const l of this.listeners) l(state);
  }

  /** Start (or restart) the connection. The video element must already be
   * attached to the document. */
  start() {
    this.stopped = false;
    this.transport = "webrtc";
    void this.connect();
  }

  /** Foreground return: nudge autoplay, then verify the picture actually
   * advances — iOS suspends WebRTC in background tabs and the stream comes
   * back frozen. A frozen stream reconnects immediately instead of waiting
   * for the slow stall watchdog. */
  resumeCheck() {
    if (this.stopped) return;
    void this.video.play().catch(() => {});
    if (this.state.phase !== "playing") return;
    const t = this.video.currentTime;
    window.setTimeout(() => {
      if (this.stopped || this.state.phase !== "playing") return;
      if (this.video.currentTime === t && !this.video.paused) this.restart();
    }, 700);
  }

  /** Clean reconnect without an error tile — for recoveries the viewer
   * should not be bothered with (foreground return). */
  private restart() {
    if (this.stopped) return;
    this.teardownConnection();
    this.transport = "webrtc";
    this.setState({ phase: "connecting" });
    void this.connect();
  }

  stop() {
    this.stopped = true;
    this.teardownConnection();
    if (this.retryTimer) window.clearTimeout(this.retryTimer);
  }

  private teardownConnection() {
    if (this.webrtcTimer) window.clearTimeout(this.webrtcTimer);
    if (this.stallTimer) window.clearInterval(this.stallTimer);
    this.webrtcTimer = null;
    this.stallTimer = null;
    this.ws?.close();
    this.ws = null;
    this.pc?.close();
    this.pc = null;
    if (this.sourceBuffer && this.mediaSource?.readyState === "open") {
      try {
        this.mediaSource.endOfStream();
      } catch {
        /* already ended */
      }
    }
    this.mediaSource = null;
    this.sourceBuffer = null;
    this.mseQueue = [];
  }

  private async connect() {
    if (this.stopped) return;
    this.teardownConnection();
    this.setState({ phase: "connecting" });

    let url: string;
    try {
      url = await this.getWsUrl(this.preferSub && !this.subDowngraded);
    } catch (err) {
      this.fail(err instanceof Error ? err.message : "could not get a stream token");
      return;
    }
    if (this.stopped) return;

    let ws: WebSocket;
    try {
      ws = new WebSocket(url);
    } catch {
      this.fail("the stream URL could not be opened");
      return;
    }
    this.ws = ws;
    ws.binaryType = "arraybuffer";

    ws.onopen = () => {
      if (this.transport === "webrtc") void this.startWebRTC(ws);
      else this.startMSE(ws);
    };
    ws.onerror = () => {
      // onclose follows with the real disposition.
    };
    ws.onclose = (e) => {
      if (this.stopped || this.ws !== ws) return;
      if (this.state.phase !== "error") {
        this.fail(e.code === 1006 ? "the stream connection dropped" : `stream closed (${e.code})`);
      }
    };
    ws.onmessage = (e) => {
      if (typeof e.data === "string") {
        this.handleSignal(ws, e.data);
      } else if (this.transport === "mse") {
        this.appendMSE(e.data as ArrayBuffer);
      }
    };
  }

  private async startWebRTC(ws: WebSocket) {
    // iceServers deliberately empty: host candidates only, direct on the LAN.
    const pc = new RTCPeerConnection({ iceServers: [], bundlePolicy: "max-bundle" });
    this.pc = pc;
    pc.addTransceiver("video", { direction: "recvonly" });
    pc.addTransceiver("audio", { direction: "recvonly" });

    const stream = new MediaStream();
    pc.ontrack = (e) => {
      stream.addTrack(e.track);
      if (this.video.srcObject !== stream) {
        this.video.srcObject = stream;
        void this.video.play().catch(() => {});
      }
    };
    pc.onicecandidate = (e) => {
      if (e.candidate && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: "webrtc/candidate", value: e.candidate.candidate }));
      }
    };

    this.video.onplaying = () => this.onPlaying();

    // If WebRTC produces nothing in time, retry the same URL with MSE.
    this.webrtcTimer = window.setTimeout(() => {
      if (this.state.phase !== "playing") this.fallbackToMSE();
    }, WEBRTC_TIMEOUT_MS);

    try {
      const offer = await pc.createOffer();
      await pc.setLocalDescription(offer);
      ws.send(JSON.stringify({ type: "webrtc/offer", value: offer.sdp }));
    } catch {
      this.fallbackToMSE();
    }
  }

  private startMSE(ws: WebSocket) {
    ws.send(JSON.stringify({ type: "mse", value: MSE_CODECS }));
    // The socket being open proves nothing — data has to arrive.
    this.webrtcTimer = window.setTimeout(() => {
      if (this.state.phase !== "playing") this.fail("the stream sent no data");
    }, MSE_FIRST_DATA_TIMEOUT_MS);
  }

  private handleSignal(ws: WebSocket, raw: string) {
    let msg: { type?: string; value?: string };
    try {
      msg = JSON.parse(raw);
    } catch {
      return;
    }
    switch (msg.type) {
      case "webrtc/answer":
        void this.pc
          ?.setRemoteDescription({ type: "answer", sdp: msg.value ?? "" })
          .catch(() => this.fallbackToMSE());
        break;
      case "webrtc/candidate":
        void this.pc
          ?.addIceCandidate({ candidate: msg.value ?? "", sdpMid: "0" })
          .catch(() => {});
        break;
      case "mse": {
        const mime = `video/mp4; codecs="${msg.value ?? ""}"`;
        this.openMediaSource(mime);
        break;
      }
      case "error":
        // The server said WHY — show exactly that.
        if (this.transport === "webrtc") {
          this.fallbackToMSE();
        } else {
          this.fail(msg.value || "the stream reported an error");
        }
        break;
    }
    void ws;
  }

  private fallbackToMSE() {
    if (this.stopped || this.transport === "mse") return;
    if (typeof MediaSource === "undefined" && !managedMediaSource()) {
      this.fail("live video is not supported in this browser");
      return;
    }
    this.transport = "mse";
    void this.connect();
  }

  private openMediaSource(mime: string) {
    const MMS = managedMediaSource();
    const ms = new (MMS ?? MediaSource)();
    this.mediaSource = ms;
    this.video.srcObject = null;
    ms.addEventListener("sourceopen", () => {
      if (this.video.src) URL.revokeObjectURL(this.video.src);
      try {
        const sb = ms.addSourceBuffer(mime);
        sb.mode = "segments";
        sb.addEventListener("updateend", () => {
          this.drainMSE();
          this.trimMSE(sb);
        });
        this.sourceBuffer = sb;
        this.drainMSE();
      } catch {
        this.fail(`this browser cannot play the stream (${mime})`);
      }
    });
    if (MMS) {
      this.video.disableRemotePlayback = true;
      (this.video as HTMLVideoElement & { srcObject: MediaSource }).srcObject = ms;
    } else {
      this.video.src = URL.createObjectURL(ms);
    }
    this.video.onplaying = () => this.onPlaying();
    void this.video.play().catch(() => {});
  }

  /** Keep only the last few seconds buffered and pin playback near the
   * live edge — an unbounded SourceBuffer eventually throws QuotaExceeded,
   * and a drifting one shows the past. */
  private trimMSE(sb: SourceBuffer) {
    if (sb !== this.sourceBuffer || sb.updating || sb.buffered.length === 0) return;
    const end = sb.buffered.end(sb.buffered.length - 1);
    const start = sb.buffered.start(0);
    const liveStart = end - MSE_LIVE_WINDOW_S;
    if (liveStart > start) {
      try {
        sb.remove(start, liveStart);
      } catch {
        /* transient; retried on the next updateend */
      }
    }
    if (this.video.currentTime < liveStart) this.video.currentTime = end - 0.4;
  }

  private appendMSE(chunk: ArrayBuffer) {
    this.mseQueue.push(chunk);
    this.drainMSE();
  }

  private drainMSE() {
    const sb = this.sourceBuffer;
    if (!sb || sb.updating || this.mseQueue.length === 0) return;
    try {
      sb.appendBuffer(this.mseQueue.shift()!);
    } catch {
      // Buffer errors are recovered by a clean reconnect.
      this.fail("the stream buffer failed");
    }
  }

  private onPlaying() {
    if (this.stopped) return;
    this.attempts = 0;
    if (this.webrtcTimer) window.clearTimeout(this.webrtcTimer);
    this.setState({ phase: "playing", transport: this.transport });

    // Stall watchdog: playback time must advance while "playing".
    this.lastTime = -1;
    if (this.stallTimer) window.clearInterval(this.stallTimer);
    this.stallTimer = window.setInterval(() => {
      if (document.hidden) return; // background tabs legitimately pause
      if (this.video.paused) {
        // Paused is autoplay policy, not a dead stream — nudge, don't fail.
        void this.video.play().catch(() => {});
        return;
      }
      const t = this.video.currentTime;
      if (t === this.lastTime) {
        this.fail("the stream stalled");
      }
      this.lastTime = t;
    }, STALL_TIMEOUT_MS);
  }

  private fail(reason: string) {
    if (this.stopped) return;
    this.teardownConnection();
    // Both transports failed on the substream: try the camera's main
    // stream before declaring the tile broken — substreams are an
    // optimization, not a requirement.
    if (this.preferSub && !this.subDowngraded) {
      this.subDowngraded = true;
      this.transport = "webrtc";
      this.setState({ phase: "connecting" });
      void this.connect();
      return;
    }
    this.attempts++;
    const retryInMs = Math.min(1_000 * 2 ** this.attempts, MAX_BACKOFF_MS);
    this.setState({ phase: "error", reason, retryInMs });
    this.retryTimer = window.setTimeout(() => {
      this.transport = "webrtc"; // each fresh attempt tries the good path first
      void this.connect();
    }, retryInMs);
  }
}

// ---- warm-connection pool ---------------------------------------------------
// Connections survive React re-renders and route changes: the pool owns the
// engine AND its <video> node; mounting moves the node (a playing <video>
// keeps playing when moved), unmounting starts an idle timer.

type PoolEntry = {
  engine: LiveEngine;
  refs: number;
  idleTimer: number | null;
};

const pool = new Map<string, PoolEntry>();
const IDLE_RELEASE_MS = 3 * 60_000;

export function acquireLive(key: string, container: HTMLElement, getWsUrl: WsUrlProvider, preferSub: boolean): LiveEngine {
  let entry = pool.get(key);
  if (!entry) {
    entry = { engine: new LiveEngine(getWsUrl, preferSub), refs: 0, idleTimer: null };
    pool.set(key, entry);
    // Attach FIRST, then connect (§5.5).
    container.appendChild(entry.engine.video);
    entry.engine.start();
  } else {
    if (entry.idleTimer) {
      window.clearTimeout(entry.idleTimer);
      entry.idleTimer = null;
    }
    // Move the existing (possibly playing) video node into the new home.
    container.appendChild(entry.engine.video);
    if (entry.engine.currentState().phase === "error") entry.engine.start();
  }
  entry.refs++;
  return entry.engine;
}

export function releaseLive(key: string) {
  const entry = pool.get(key);
  if (!entry) return;
  entry.refs--;
  if (entry.refs > 0) return;
  entry.idleTimer = window.setTimeout(() => {
    entry.engine.stop();
    entry.engine.video.remove();
    pool.delete(key);
  }, IDLE_RELEASE_MS);
}

// iOS suspends WebRTC in background tabs — on return, nudge every stream
// and reconnect any whose picture stays frozen.
if (typeof document !== "undefined") {
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) {
      for (const entry of pool.values()) entry.engine.resumeCheck();
    }
  });
}
