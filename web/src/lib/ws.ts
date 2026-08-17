// The single multiplexed WebSocket connection. Reconnects with backoff,
// re-subscribes surviving topics, and dispatches events to listeners.

type Listener = (data: unknown) => void;

type Frame = {
  type: string;
  topic?: string;
  data?: unknown;
  message?: string;
};

class LiveSocket {
  private ws: WebSocket | null = null;
  private listeners = new Map<string, Set<Listener>>();
  private backoff = 1000;
  private closedByUser = true;
  private reconnectTimer: number | undefined;

  /** connect opens (or re-opens) the socket; safe to call repeatedly. */
  connect() {
    this.closedByUser = false;
    if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) {
      return;
    }
    const proto = location.protocol === "https:" ? "wss" : "ws";
    const ws = new WebSocket(`${proto}://${location.host}/api/ws`);
    this.ws = ws;

    ws.onopen = () => {
      this.backoff = 1000;
      for (const topic of this.listeners.keys()) {
        ws.send(JSON.stringify({ type: "subscribe", topic }));
      }
    };
    ws.onmessage = (ev) => {
      if (typeof ev.data !== "string") return;
      let frame: Frame;
      try {
        frame = JSON.parse(ev.data);
      } catch {
        return;
      }
      if (frame.type === "event" && frame.topic) {
        for (const fn of this.listeners.get(frame.topic) ?? []) fn(frame.data);
      }
    };
    ws.onclose = () => {
      this.ws = null;
      if (this.closedByUser) return;
      this.reconnectTimer = window.setTimeout(() => this.connect(), this.backoff);
      this.backoff = Math.min(this.backoff * 2, 30_000);
    };
  }

  disconnect() {
    this.closedByUser = true;
    window.clearTimeout(this.reconnectTimer);
    this.ws?.close(1000, "signed out");
    this.ws = null;
  }

  /** subscribe registers a listener; returns an unsubscribe function. */
  subscribe(topic: string, fn: Listener): () => void {
    let set = this.listeners.get(topic);
    const isNewTopic = !set;
    if (!set) {
      set = new Set();
      this.listeners.set(topic, set);
    }
    set.add(fn);
    if (isNewTopic && this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify({ type: "subscribe", topic }));
    }
    return () => {
      const listeners = this.listeners.get(topic);
      if (!listeners) return;
      listeners.delete(fn);
      if (listeners.size === 0) {
        this.listeners.delete(topic);
        if (this.ws?.readyState === WebSocket.OPEN) {
          this.ws.send(JSON.stringify({ type: "unsubscribe", topic }));
        }
      }
    };
  }
}

export const live = new LiveSocket();
