import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, Circle, Copy, Download, ExternalLink, FileText, Play, RotateCcw, Square } from "lucide-react";
import { api, unwrap } from "@/lib/api";
import { cn, copyText } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Input } from "@/ui/input";
import { useToast } from "@/ui/toast";
import {
  Dropdown,
  DropdownContent,
  DropdownItem,
  DropdownTrigger,
} from "@/ui/dropdown";
import { ToolCard } from "../kit";

/**
 * The IP scanner — Angry-IP-Scanner-style. Sweeps a range (default: the
 * server's own subnet, .1–.254) from the HomeHub server, streaming per-host
 * results as they land: alive state (grey → amber → green), hostname,
 * MAC/vendor, and which ports are open. The row list is generated up front
 * in grey and fills in live over an NDJSON stream.
 */

type Status = "pending" | "dead" | "up" | "open";

type Row = {
  ip: string;
  status: Status;
  hostname?: string;
  mac?: string;
  vendor?: string;
  rttMs?: number;
  openPorts: number[];
};

function ipToInt(ip: string): number | null {
  const m = ip.trim().match(/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/);
  if (!m) return null;
  const o = m.slice(1).map(Number);
  if (o.some((n) => n > 255)) return null;
  return ((o[0] << 24) | (o[1] << 16) | (o[2] << 8) | o[3]) >>> 0;
}

const intToIp = (n: number) =>
  [(n >>> 24) & 255, (n >>> 16) & 255, (n >>> 8) & 255, n & 255].join(".");

function enumerateIps(start: string, end: string): string[] | { error: string } {
  const s = ipToInt(start);
  const e = ipToInt(end);
  if (s === null || e === null) return { error: "invalid" };
  if (e < s) return { error: "reversed" };
  if (e - s + 1 > 65536) return { error: "toobig" };
  const out: string[] = [];
  for (let n = s; n <= e; n++) out.push(intToIp(n));
  return out;
}

/** Parse a ports field: "22, 80, 8000-8010" → sorted unique list. Empty ⇒
 * [] (the server then uses its common-ports default). */
function parsePorts(text: string): number[] {
  const set = new Set<number>();
  for (const tok of text.split(/[\s,]+/).filter(Boolean)) {
    const range = tok.match(/^(\d+)-(\d+)$/);
    if (range) {
      const a = Number(range[1]);
      const b = Number(range[2]);
      for (let p = a; p <= b && p - a < 2000; p++) if (p >= 1 && p <= 65535) set.add(p);
    } else {
      const p = Number(tok);
      if (Number.isInteger(p) && p >= 1 && p <= 65535) set.add(p);
    }
  }
  return [...set].sort((a, b) => a - b);
}

const STATUS_TONE: Record<Status, string> = {
  pending: "text-fg-subtle/40",
  dead: "text-fg-subtle/50",
  up: "text-warning",
  open: "text-success",
};

/** Best-guess URL to open a host in the browser: https only when a secure
 *  web port is open and a plain one isn't, else http. */
function openUrl(row: Row): string {
  const p = row.openPorts;
  const secureOnly = (p.includes(443) || p.includes(8443)) && !(p.includes(80) || p.includes(8080));
  return `${secureOnly ? "https" : "http"}://${row.ip}`;
}

function downloadBlob(name: string, content: string, mime: string) {
  const url = URL.createObjectURL(new Blob([content], { type: mime }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}

const csvCell = (v: string | number) => {
  const s = String(v ?? "");
  return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
};

export default function IpScanTool() {
  const toast = useToast();
  const [start, setStart] = useState("");
  const [end, setEnd] = useState("");
  const [portsText, setPortsText] = useState("");
  const [showPorts, setShowPorts] = useState(false);
  const [aliveOnly, setAliveOnly] = useState(false);
  const [rows, setRows] = useState<Map<string, Row>>(new Map());
  const [scanning, setScanning] = useState(false);
  const [total, setTotal] = useState(0);
  const [portCount, setPortCount] = useState(0);
  const abortRef = useRef<AbortController | null>(null);

  const interfaces = useQuery({
    queryKey: ["netscan-interfaces"],
    queryFn: async () => unwrap(await api.GET("/netscan/interfaces")),
    staleTime: 60_000,
  });

  // Default the range to the detected primary subnet once.
  const seeded = useRef(false);
  useEffect(() => {
    if (seeded.current || !interfaces.data || interfaces.data.length === 0) return;
    const primary = interfaces.data.find((i) => i.primary) ?? interfaces.data[0];
    setStart(primary.start);
    setEnd(primary.end);
    seeded.current = true;
  }, [interfaces.data]);

  const resetToSubnet = () => {
    const primary = interfaces.data?.find((i) => i.primary) ?? interfaces.data?.[0];
    if (primary) {
      setStart(primary.start);
      setEnd(primary.end);
    }
  };

  const stop = () => {
    abortRef.current?.abort();
    abortRef.current = null;
    setScanning(false);
  };

  const scan = async () => {
    const ips = enumerateIps(start, end);
    if ("error" in ips) {
      toast(
        "error",
        ips.error === "reversed"
          ? "The end address is before the start."
          : ips.error === "toobig"
            ? "That range is over 65,536 addresses — narrow it down."
            : "Enter two valid IPv4 addresses.",
      );
      return;
    }
    // Pre-seed every address as a grey pending row (the Angry-IP feel).
    const init = new Map<string, Row>();
    for (const ip of ips) init.set(ip, { ip, status: "pending", openPorts: [] });
    setRows(init);
    setTotal(ips.length);

    const ports = parsePorts(portsText);
    const controller = new AbortController();
    abortRef.current = controller;
    setScanning(true);

    try {
      const res = await fetch("/api/netscan", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify({ start, end, ports }),
        signal: controller.signal,
      });
      if (!res.ok || !res.body) {
        toast("error", "The scan could not start.");
        setScanning(false);
        return;
      }
      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buf = "";
      // Batch DOM updates so 254 quick messages don't thrash React.
      let pending: Row[] = [];
      let raf = 0;
      const flush = () => {
        raf = 0;
        if (pending.length === 0) return;
        const batch = pending;
        pending = [];
        setRows((prev) => {
          const next = new Map(prev);
          for (const r of batch) next.set(r.ip, r);
          return next;
        });
      };
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true });
        const lines = buf.split("\n");
        buf = lines.pop() ?? "";
        for (const line of lines) {
          if (!line.trim()) continue;
          let msg: Record<string, unknown>;
          try {
            msg = JSON.parse(line);
          } catch {
            continue;
          }
          if (msg.type === "meta") {
            setPortCount(Number(msg.ports) || 0);
          } else if (msg.type === "host") {
            const openPorts = (msg.openPorts as number[]) ?? [];
            const alive = Boolean(msg.alive);
            pending.push({
              ip: String(msg.ip),
              status: alive ? (openPorts.length > 0 ? "open" : "up") : "dead",
              hostname: (msg.hostname as string) || undefined,
              mac: (msg.mac as string) || undefined,
              vendor: (msg.vendor as string) || undefined,
              rttMs: typeof msg.rttMs === "number" && msg.rttMs > 0 ? msg.rttMs : undefined,
              openPorts,
            });
            if (!raf) raf = requestAnimationFrame(flush);
          }
        }
      }
      flush();
    } catch (err) {
      if (!(err instanceof DOMException && err.name === "AbortError")) {
        toast("error", "The scan was interrupted.");
      }
    } finally {
      abortRef.current = null;
      setScanning(false);
    }
  };

  // Stop on unmount.
  useEffect(() => () => abortRef.current?.abort(), []);

  const allRows = useMemo(
    () =>
      [...rows.values()].sort((a, b) => (ipToInt(a.ip)! - ipToInt(b.ip)!)),
    [rows],
  );
  const shown = aliveOnly ? allRows.filter((r) => r.status === "up" || r.status === "open") : allRows;
  const scanned = allRows.filter((r) => r.status !== "pending").length;
  const aliveCount = allRows.filter((r) => r.status === "up" || r.status === "open").length;
  const progress = total > 0 ? Math.round((scanned / total) * 100) : 0;

  const copyAlive = async () => {
    const list = allRows
      .filter((r) => r.status === "up" || r.status === "open")
      .map((r) => r.ip)
      .join("\n");
    if (list && (await copyText(list))) toast("success", `Copied ${aliveCount} addresses`);
  };

  const aliveRows = () => allRows.filter((r) => r.status === "up" || r.status === "open");

  const exportTxt = () => {
    const rows = aliveRows();
    if (rows.length === 0) return;
    downloadBlob("alive-ips.txt", rows.map((r) => r.ip).join("\n") + "\n", "text/plain;charset=utf-8");
    toast("success", `Exported ${rows.length} addresses`);
  };

  const exportCsv = () => {
    const rows = aliveRows();
    if (rows.length === 0) return;
    const header = ["IP", "Hostname", "MAC", "Vendor", "RTT (ms)", "Open Ports"];
    const lines = [header.join(",")];
    for (const r of rows) {
      lines.push(
        [
          r.ip,
          r.hostname ?? "",
          r.mac ?? "",
          r.vendor ?? "",
          r.rttMs !== undefined ? (r.rttMs < 10 ? r.rttMs.toFixed(1) : Math.round(r.rttMs)) : "",
          r.openPorts.join(" "),
        ]
          .map(csvCell)
          .join(","),
      );
    }
    downloadBlob("ip-scan.csv", lines.join("\n") + "\n", "text/csv;charset=utf-8");
    toast("success", `Exported ${rows.length} hosts`);
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4">
      <ToolCard title="Range" className="shrink-0">
        <div className="flex flex-wrap items-end gap-3">
          <label className="min-w-0 flex-1">
            <span className="text-fg-muted mb-1 block text-[12px]">From</span>
            <Input value={start} onChange={(e) => setStart(e.target.value)} placeholder="192.168.10.1" className="font-mono" />
          </label>
          <label className="min-w-0 flex-1">
            <span className="text-fg-muted mb-1 block text-[12px]">To</span>
            <Input value={end} onChange={(e) => setEnd(e.target.value)} placeholder="192.168.10.254" className="font-mono" />
          </label>
          <Button variant="ghost" size="sm" onClick={resetToSubnet} title="Reset to detected subnet" disabled={!interfaces.data?.length}>
            <RotateCcw className="size-3.5" />
          </Button>
          {scanning ? (
            <Button variant="danger" onClick={stop}>
              <Square className="size-3.5" />
              Stop
            </Button>
          ) : (
            <Button variant="primary" onClick={scan}>
              <Play className="size-3.5" />
              Scan
            </Button>
          )}
        </div>

        <button
          type="button"
          onClick={() => setShowPorts((v) => !v)}
          className="text-fg-subtle hover:text-fg mt-3 flex items-center gap-1 text-[12.5px]"
        >
          <ChevronDown className={cn("size-3.5 transition-transform", showPorts && "rotate-180")} />
          Ports {portsText.trim() ? "" : "(common set)"}
        </button>
        {showPorts && (
          <div className="mt-2">
            <Input
              value={portsText}
              onChange={(e) => setPortsText(e.target.value)}
              placeholder="22, 80, 443, 8000-8010 — blank uses the common-ports set"
              className="font-mono text-[12.5px]"
            />
          </div>
        )}

        {total > 0 && (
          <div className="mt-3 space-y-1.5">
            <div className="bg-bg-subtle h-1.5 overflow-hidden rounded-full">
              <div
                className={cn("h-full rounded-full transition-all", scanning ? "bg-accent" : "bg-success")}
                style={{ width: `${progress}%` }}
              />
            </div>
            <div className="text-fg-subtle flex items-center justify-between text-[11.5px]">
              <span>
                {scanned} / {total} scanned · {aliveCount} alive · {portCount} ports each
              </span>
              {scanning && <span>scanning…</span>}
            </div>
          </div>
        )}
      </ToolCard>

      {total > 0 && (
        <ToolCard
          title="Hosts"
          className="flex min-h-0 flex-1 flex-col overflow-hidden"
          right={
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={() => setAliveOnly((v) => !v)}
                className={cn(
                  "rounded-full border px-2.5 py-1 text-[11.5px] transition-colors",
                  aliveOnly
                    ? "border-accent bg-accent-soft text-accent-soft-fg font-medium"
                    : "border-edge text-fg-muted hover:text-fg",
                )}
              >
                Alive only
              </button>
              <Dropdown>
                <DropdownTrigger asChild>
                  <Button size="sm" variant="ghost" disabled={aliveCount === 0}>
                    <Download className="size-3.5" />
                    Export
                    <ChevronDown className="size-3" />
                  </Button>
                </DropdownTrigger>
                <DropdownContent align="end">
                  <DropdownItem onSelect={copyAlive}>
                    <Copy className="size-3.5" />
                    Copy alive IPs
                  </DropdownItem>
                  <DropdownItem onSelect={exportTxt}>
                    <FileText className="size-3.5" />
                    Download IP list (.txt)
                  </DropdownItem>
                  <DropdownItem onSelect={exportCsv}>
                    <Download className="size-3.5" />
                    Download CSV (all details)
                  </DropdownItem>
                </DropdownContent>
              </Dropdown>
            </div>
          }
        >
          <div className="border-edge flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border">
            <div className="text-fg-subtle bg-bg-subtle grid shrink-0 grid-cols-[24px_156px_1.1fr_1.2fr_1.4fr_60px] items-center gap-2 px-3 py-1.5 text-[11px] font-medium uppercase tracking-wide">
              <span />
              <span>Address</span>
              <span>Hostname</span>
              <span>MAC</span>
              <span>Open ports</span>
              <span className="text-right">RTT</span>
            </div>
            <div className="divide-edge min-h-0 flex-1 divide-y overflow-y-auto">
              {shown.map((r) => (
                <HostRow key={r.ip} row={r} onCopy={async () => {
                  if (await copyText(r.ip)) toast("success", `Copied ${r.ip}`);
                }} />
              ))}
              {shown.length === 0 && (
                <p className="text-fg-subtle px-3 py-6 text-center text-[13px]">No alive hosts yet.</p>
              )}
            </div>
          </div>
        </ToolCard>
      )}
    </div>
  );
}

function HostRow({ row, onCopy }: { row: Row; onCopy: () => void }) {
  const dim = row.status === "dead" || row.status === "pending";
  return (
    <div
      className={cn(
        "group hover:bg-bg-subtle/60 grid grid-cols-[24px_156px_1.1fr_1.2fr_1.4fr_60px] items-center gap-2 px-3 py-1.5 text-[12.5px]",
        dim && "opacity-55",
      )}
    >
      <Circle className={cn("size-2.5 fill-current", STATUS_TONE[row.status])} />
      <div className="flex items-center gap-1">
        <span className="text-fg font-mono">{row.ip}</span>
        <button
          type="button"
          onClick={onCopy}
          className="text-fg-subtle hover:text-fg opacity-0 transition-opacity group-hover:opacity-100"
          title="Copy IP"
        >
          <Copy className="size-3" />
        </button>
        {(row.status === "up" || row.status === "open") && (
          <a
            href={openUrl(row)}
            target="_blank"
            rel="noreferrer"
            onClick={(e) => e.stopPropagation()}
            className="text-fg-subtle hover:text-accent opacity-0 transition-opacity group-hover:opacity-100"
            title="Open in browser"
          >
            <ExternalLink className="size-3" />
          </a>
        )}
      </div>
      {/* Hostname */}
      <span className="text-fg min-w-0 truncate">
        {row.hostname || (row.status === "pending" ? "" : <span className="text-fg-subtle">—</span>)}
      </span>
      {/* MAC + vendor */}
      <div className="min-w-0">
        {row.mac ? (
          <>
            <span className="text-fg-muted block truncate font-mono text-[11.5px]">{row.mac}</span>
            {row.vendor && <span className="text-fg-subtle block truncate text-[11px]">{row.vendor}</span>}
          </>
        ) : (
          row.status !== "pending" && <span className="text-fg-subtle">—</span>
        )}
      </div>
      <div className="flex flex-wrap gap-1">
        {row.openPorts.map((p) => (
          <Badge key={p} tone={PORT_TONE(p)}>
            {p}
          </Badge>
        ))}
      </div>
      <span className="text-fg-subtle text-right tabular-nums">
        {row.rttMs !== undefined ? `${row.rttMs < 10 ? row.rttMs.toFixed(1) : Math.round(row.rttMs)} ms` : ""}
      </span>
    </div>
  );
}

/** A few well-known ports get a colored badge; the rest are neutral. */
function PORT_TONE(p: number): "success" | "warning" | "info" | "neutral" {
  if (p === 443 || p === 22 || p === 8006) return "success";
  if (p === 80 || p === 8080 || p === 8123 || p === 8443) return "info";
  if (p === 23 || p === 21 || p === 3389 || p === 445) return "warning";
  return "neutral";
}
