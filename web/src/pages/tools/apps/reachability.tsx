import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { AlertTriangle, ArrowRight, Cable, Info, Network, Radio } from "lucide-react";
import { api, isApiError, unwrap } from "@/lib/api";
import type { ApiSchemas } from "@/lib/api";
import { cn } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { useToast } from "@/ui/toast";
import { CopyIconButton, ToolCard } from "../kit";

/**
 * Port check + ICMP ping. Posts a host (and an optional TCP port) to
 * /tools/reachability and renders the two probes the server ran back: an
 * ICMP echo and, when a port was given, a TCP connect. The ping card copes
 * with hosts where ICMP is unavailable (no CAP_NET_RAW); the TCP card only
 * appears when a port was actually probed.
 */

type ReachResult = ApiSchemas["ReachResult"];

interface Preset {
  label: string;
  host: string;
  port?: number;
}

const PRESETS: Preset[] = [
  { label: "192.168.1.1:443", host: "192.168.1.1", port: 443 },
  { label: "1.1.1.1:53", host: "1.1.1.1", port: 53 },
  { label: "1.1.1.1 · ping only", host: "1.1.1.1" },
];

/** Human milliseconds — more precision for the small, sub-ms numbers. */
function fmtMs(n: number): string {
  if (n >= 100) return String(Math.round(n));
  if (n >= 10) return n.toFixed(1);
  return n.toFixed(2);
}

export default function Tool() {
  const toast = useToast();
  const [host, setHost] = useState("");
  const [portStr, setPortStr] = useState("");
  const [portErr, setPortErr] = useState<string | null>(null);

  const check = useMutation({
    mutationFn: async (vars: { host: string; port?: number }) =>
      unwrap(await api.POST("/tools/reachability", { body: vars })),
    onError: (e) => toast("error", isApiError(e) ? e.message : String(e)),
  });

  const run = (h: string, port?: number) => {
    setPortErr(null);
    check.mutate({ host: h, port });
  };

  const submit = () => {
    const h = host.trim();
    if (!h) return;
    let port: number | undefined;
    const p = portStr.trim();
    if (p !== "") {
      const n = Number(p);
      if (!/^\d+$/.test(p) || n < 1 || n > 65535) {
        setPortErr("Port must be a number from 1 to 65535.");
        return;
      }
      port = n;
    }
    run(h, port);
  };

  const applyPreset = (preset: Preset) => {
    setHost(preset.host);
    setPortStr(preset.port != null ? String(preset.port) : "");
    setPortErr(null);
    run(preset.host, preset.port);
  };

  const result = check.data;
  const errMsg = check.isError ? (isApiError(check.error) ? check.error.message : String(check.error)) : null;

  return (
    <div className="space-y-4">
      <ToolCard
        title={
          <span className="flex items-center gap-2">
            <Network className="text-fg-muted size-4" />
            Reachability
          </span>
        }
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start">
            <div className="min-w-0 flex-1">
              <Field label="Host" hint="Hostname or IP address">
                {(p) => (
                  <Input
                    {...p}
                    value={host}
                    onChange={(e) => setHost(e.target.value)}
                    placeholder="192.168.1.1"
                    spellCheck={false}
                    autoComplete="off"
                    autoCapitalize="off"
                    className="font-mono"
                  />
                )}
              </Field>
            </div>
            <div className="sm:w-36">
              <Field label="Port" optional="optional" hint="Leave empty to ping only" error={portErr}>
                {(p) => (
                  <Input
                    {...p}
                    value={portStr}
                    onChange={(e) => {
                      setPortStr(e.target.value.replace(/[^\d]/g, ""));
                      if (portErr) setPortErr(null);
                    }}
                    placeholder="443"
                    inputMode="numeric"
                    maxLength={5}
                    className="font-mono"
                  />
                )}
              </Field>
            </div>
            <div className="sm:mt-[1.625rem]">
              <Button
                type="submit"
                variant="primary"
                loading={check.isPending}
                disabled={!host.trim()}
                className="w-full sm:w-auto"
              >
                Check
              </Button>
            </div>
          </div>
        </form>

        <div className="mt-3 flex flex-wrap items-center gap-1.5">
          <span className="text-fg-subtle mr-1 text-[12px]">Try</span>
          {PRESETS.map((preset) => (
            <Button
              key={preset.label}
              size="sm"
              variant="ghost"
              className="font-mono text-[12px]"
              onClick={() => applyPreset(preset)}
            >
              {preset.label}
            </Button>
          ))}
        </div>
      </ToolCard>

      {errMsg ? (
        <ToolCard>
          <div className="text-danger flex items-start gap-2 text-[13px]">
            <AlertTriangle className="mt-px size-4 shrink-0" />
            <span>{errMsg}</span>
          </div>
        </ToolCard>
      ) : result ? (
        <Results data={result} />
      ) : (
        <ToolCard>
          <div className="text-fg-muted flex items-center gap-2 text-[13px]">
            <Radio className="text-fg-subtle size-4 shrink-0" />
            <span>Enter a host to ping it, or add a port to also test a TCP connection.</span>
          </div>
        </ToolCard>
      )}
    </div>
  );
}

function Results({ data }: { data: ReachResult }) {
  const { ping, tcp } = data;
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 px-0.5">
        <Network className="text-fg-muted size-4 shrink-0" />
        <span className="text-fg font-mono text-[15px] font-semibold">{data.host}</span>
        {data.ip && data.ip !== data.host && (
          <span className="text-fg-subtle flex items-center gap-1 font-mono text-[12px]">
            <ArrowRight className="size-3 shrink-0" />
            {data.ip}
            <CopyIconButton value={data.ip} label="Copy resolved IP" />
          </span>
        )}
      </div>

      <div className={cn("grid gap-4", tcp && "md:grid-cols-2")}>
        {ping && (
          <ToolCard
            title={
              <span className="flex items-center gap-2">
                <Radio className="text-fg-muted size-4" />
                ICMP Ping
              </span>
            }
          >
            {ping.available === false ? (
              <div className="text-fg-muted flex items-start gap-2 text-[13px]">
                <Info className="text-fg-subtle mt-px size-4 shrink-0" />
                <span>ICMP ping unavailable on this host{ping.error ? ` — ${ping.error}` : ""}.</span>
              </div>
            ) : (
              <div className="flex items-center justify-between gap-4">
                <div className="min-w-0">
                  <Badge tone={ping.alive ? "success" : "danger"}>{ping.alive ? "Alive" : "No reply"}</Badge>
                  <p className="text-fg-subtle mt-1.5 text-[12px]">
                    {ping.alive
                      ? "Round-trip time via ICMP echo"
                      : ping.error || "Host did not answer the echo request"}
                  </p>
                </div>
                {ping.alive && ping.rttMs != null && (
                  <div className="shrink-0 text-right">
                    <div className="text-fg font-mono text-2xl font-semibold tabular-nums">
                      {fmtMs(ping.rttMs)}
                      <span className="text-fg-subtle ml-1 text-sm font-normal">ms</span>
                    </div>
                  </div>
                )}
              </div>
            )}
          </ToolCard>
        )}

        {tcp && (
          <ToolCard
            title={
              <span className="flex items-center gap-2">
                <Cable className="text-fg-muted size-4" />
                TCP Port {tcp.port}
              </span>
            }
          >
            <div className="flex items-center justify-between gap-4">
              <div className="min-w-0">
                <Badge tone={tcp.open ? "success" : "danger"}>{tcp.open ? "Open" : "Closed"}</Badge>
                <p className="text-fg-subtle mt-1.5 text-[12px]">
                  {tcp.open ? "TCP handshake completed" : tcp.error || "Connection refused"}
                </p>
              </div>
              <div className="shrink-0 text-right">
                <div className="text-fg-subtle text-[11px] font-medium tracking-wide uppercase">
                  {tcp.open ? "Latency" : "Port"}
                </div>
                <div className="text-fg font-mono text-2xl font-semibold tabular-nums">
                  {tcp.open ? (
                    <>
                      {fmtMs(tcp.latencyMs)}
                      <span className="text-fg-subtle ml-1 text-sm font-normal">ms</span>
                    </>
                  ) : (
                    tcp.port
                  )}
                </div>
              </div>
            </div>
          </ToolCard>
        )}
      </div>
    </div>
  );
}
