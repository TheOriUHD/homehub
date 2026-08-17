import { useCallback, useEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  ArrowLeft,
  ChevronRight,
  Clock,
  Cpu,
  Loader2,
  MemoryStick,
  MoreHorizontal,
  Play,
  Power,
  RotateCw,
  Skull,
  Square,
  Users,
} from "lucide-react";
import { api, isApiError, unwrap, type AmpConsoleLine } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { live } from "@/lib/ws";
import { cn } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Card } from "@/ui/card";
import { ConfirmDialog } from "@/ui/confirm";
import { Dropdown, DropdownContent, DropdownItem, DropdownTrigger } from "@/ui/dropdown";
import { PageLoader } from "@/ui/misc";
import { ErrorState } from "@/ui/states";
import { useToast } from "@/ui/toast";
import { ampResource, ampStateBusy, ampStateTone, ampTopic, useAmpOverview } from "./shared";

// Same deliberate terminal palette as the Minecraft console — a console is a
// dark surface in both themes.
const PANE = {
  bg: "#101319",
  chrome: "#161a22",
  edge: "#262b36",
  text: "#ccd2de",
  dim: "#5b6272",
  warn: "#fbbf24",
  error: "#f87171",
  accentText: "#e7e9f0",
};

const POLL_MS = 2500;
const MAX_BUFFER = 5000;
const PIN_THRESHOLD = 48;

function fmtTime(ts: number): string {
  const d = new Date(ts);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

function lineColor(type: string | undefined): string {
  switch ((type ?? "").toLowerCase()) {
    case "error":
      return PANE.error;
    case "warning":
      return PANE.warn;
    case "chat":
      return PANE.accentText;
    default:
      return PANE.text;
  }
}

export function AmpInstancePage() {
  const { t } = useTranslation();
  const { instanceId, serverId } = useParams({ from: "/shell/amp/$instanceId/i/$serverId" });
  const { can } = useAuth();
  const toast = useToast();
  const queryClient = useQueryClient();

  const overview = useAmpOverview(instanceId);
  const inst = overview.data?.targets
    .flatMap((target) => target.instances)
    .find((candidate) => candidate.id === serverId);

  const instForGate = overview.data?.targets
    .flatMap((target) => target.instances)
    .find((candidate) => candidate.id === serverId);
  const status = useQuery({
    queryKey: ["amp", instanceId, "status", serverId],
    queryFn: async () =>
      unwrap(
        await api.GET("/amp/{id}/instances/{instanceId}/status", {
          params: { path: { id: instanceId, instanceId: serverId } },
        }),
      ),
    refetchInterval: 5000,
    // A stopped instance container cannot be proxied to — do not hammer it.
    enabled: instForGate?.state !== "offline",
  });

  const controllable = can(`${ampResource(instanceId)}/instance/${serverId}`, "control");
  const [confirmKill, setConfirmKill] = useState(false);

  const act = useMutation({
    mutationFn: async ({ kind, action }: { kind: "app" | "lifecycle"; action: string }) => {
      const path = kind === "app" ? "/amp/{id}/instances/{instanceId}/app" : "/amp/{id}/instances/{instanceId}/lifecycle";
      unwrap(
        await api.POST(path, {
          params: { path: { id: instanceId, instanceId: serverId } },
          body: { action } as never,
        }),
      );
    },
    onSuccess: () => {
      setConfirmKill(false);
      void queryClient.invalidateQueries({ queryKey: ["amp", instanceId] });
    },
    onError: (err) =>
      toast("error", t("amp.instance.actionFailed"), isApiError(err) ? err.message : t("common.unknownError")),
  });

  if (overview.isLoading) return <PageLoader />;
  if (overview.isError) {
    return (
      <Card>
        <ErrorState error={overview.error} onRetry={() => void overview.refetch()} />
      </Card>
    );
  }

  const offline = inst?.state === "offline";
  const state = offline ? "offline" : (status.data?.state ?? inst?.state ?? "undefined");
  const tone = ampStateTone(state);
  const busy = ampStateBusy(state) || act.isPending;
  const players = status.data?.players ?? inst?.players;
  const maxPlayers = status.data?.maxPlayers ?? inst?.maxPlayers;
  const cpu = status.data?.cpuPercent ?? inst?.cpuPercent;
  const memUsed = status.data?.memUsedMb ?? inst?.memUsedMb;
  const memMax = status.data?.memMaxMb ?? inst?.memMaxMb;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <Link
          to="/amp/$instanceId"
          params={{ instanceId }}
          className="text-fg-muted hover:text-fg flex items-center gap-1 text-[13px] transition-colors"
        >
          <ArrowLeft className="size-3.5" />
          {t("amp.instance.back")}
        </Link>
        <h2 className="text-fg flex min-w-0 items-center gap-2.5 text-lg font-semibold tracking-tight">
          <span className="truncate">{inst?.friendlyName ?? serverId}</span>
          <Badge tone={tone}>
            <StatusDot tone={tone} pulse={ampStateBusy(state)} />
            {t(`amp.state.${state}`, { defaultValue: state })}
          </Badge>
        </h2>

        {controllable && !offline && (
          <div className="ml-auto flex items-center gap-2">
            {state === "stopped" || state === "failed" ? (
              <Button size="sm" onClick={() => act.mutate({ kind: "app", action: "start" })} disabled={busy}>
                <Play className="size-3.5" />
                {t("amp.instance.start")}
              </Button>
            ) : (
              <>
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={() => act.mutate({ kind: "app", action: "restart" })}
                  disabled={busy}
                >
                  <RotateCw className="size-3.5" />
                  {t("amp.instance.restart")}
                </Button>
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={() => act.mutate({ kind: "app", action: "stop" })}
                  disabled={busy}
                >
                  <Square className="size-3.5" />
                  {t("amp.instance.stop")}
                </Button>
              </>
            )}
            <Dropdown>
              <DropdownTrigger asChild>
                <Button size="sm" variant="ghost" aria-label={t("amp.instance.moreActions")}>
                  <MoreHorizontal className="size-4" />
                </Button>
              </DropdownTrigger>
              <DropdownContent align="end">
              <DropdownItem onSelect={() => setConfirmKill(true)}>
                <Skull className="size-3.5" />
                {t("amp.instance.kill")}
              </DropdownItem>
              <DropdownItem onSelect={() => act.mutate({ kind: "app", action: "update" })}>
                <RotateCw className="size-3.5" />
                {t("amp.instance.update")}
              </DropdownItem>
              <DropdownItem onSelect={() => act.mutate({ kind: "lifecycle", action: "start" })}>
                <Play className="size-3.5" />
                {t("amp.instance.lifecycleStart")}
              </DropdownItem>
              <DropdownItem onSelect={() => act.mutate({ kind: "lifecycle", action: "stop" })}>
                <Square className="size-3.5" />
                {t("amp.instance.lifecycleStop")}
              </DropdownItem>
              <DropdownItem onSelect={() => act.mutate({ kind: "lifecycle", action: "restart" })}>
                <RotateCw className="size-3.5" />
                {t("amp.instance.lifecycleRestart")}
              </DropdownItem>
              </DropdownContent>
            </Dropdown>
          </div>
        )}
      </div>

      <div className="flex flex-wrap gap-2">
        <StatChip icon={Clock} label={t("amp.instance.uptime")} value={status.data?.uptime ?? "—"} />
        <StatChip
          icon={Users}
          label={t("amp.overview.players")}
          value={players != null ? `${players}/${maxPlayers ?? "?"}` : "—"}
        />
        <StatChip icon={Cpu} label="CPU" value={cpu != null ? `${cpu.toFixed(0)}%` : "—"} />
        <StatChip
          icon={MemoryStick}
          label={t("amp.overview.memory")}
          value={
            memUsed != null && memMax ? `${(memUsed / 1024).toFixed(1)} / ${(memMax / 1024).toFixed(0)} GB` : "—"
          }
        />
      </div>

      {offline ? (
        <Card>
          <div className="flex flex-col items-center gap-3 px-6 py-14 text-center">
            <Power className="text-fg-subtle size-8" />
            <p className="text-fg text-[15px] font-semibold">{t("amp.instance.offlineTitle")}</p>
            <p className="text-fg-muted max-w-md text-[13px]">{t("amp.instance.offlineBody")}</p>
            {controllable && (
              <Button
                className="mt-1"
                onClick={() => act.mutate({ kind: "lifecycle", action: "start" })}
                loading={act.isPending}
              >
                <Play className="size-4" />
                {t("amp.instance.lifecycleStart")}
              </Button>
            )}
          </div>
        </Card>
      ) : (
        <AmpConsolePane panelId={instanceId} serverId={serverId} controllable={controllable} />
      )}

      <ConfirmDialog
        open={confirmKill}
        onOpenChange={setConfirmKill}
        title={t("amp.instance.killTitle")}
        body={t("amp.instance.killBody", { name: inst?.friendlyName ?? serverId })}
        confirmLabel={t("amp.instance.kill")}
        loading={act.isPending}
        onConfirm={() => act.mutate({ kind: "app", action: "kill" })}
      />
    </div>
  );
}

function StatChip({ icon: Icon, label, value }: { icon: typeof Clock; label: string; value: string }) {
  return (
    <span className="border-edge bg-surface text-fg-muted flex items-center gap-1.5 rounded-lg border px-2.5 py-1 text-[12px]">
      <Icon className="size-3.5" />
      {label}
      <span className="text-fg font-medium">{value}</span>
    </span>
  );
}

// ---- console ----------------------------------------------------------------

function AmpConsolePane({
  panelId,
  serverId,
  controllable,
}: {
  panelId: string;
  serverId: string;
  controllable: boolean;
}) {
  const { t } = useTranslation();
  const [lines, setLines] = useState<AmpConsoleLine[]>([]);
  const [phase, setPhase] = useState<"loading" | "ready" | "error">("loading");
  const [retryToken, setRetryToken] = useState(0);
  const lastSeqRef = useRef(-1);
  const scrollRef = useRef<HTMLDivElement>(null);
  const pinnedRef = useRef(true);

  const push = useCallback((fresh: AmpConsoleLine[]) => {
    if (fresh.length === 0) return;
    setLines((prev) => {
      const merged = [...prev];
      for (const line of fresh) {
        if (line.seq > lastSeqRef.current) {
          merged.push(line);
          lastSeqRef.current = line.seq;
        }
      }
      return merged.length > MAX_BUFFER ? merged.slice(merged.length - MAX_BUFFER) : merged;
    });
  }, []);

  // Initial load + poll loop. The GET itself keeps the server-side poller
  // alive; WS pushes fill the gaps between polls.
  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const fetchPage = async (after: number) => {
      const res = unwrap(
        await api.GET("/amp/{id}/instances/{instanceId}/console", {
          params: { path: { id: panelId, instanceId: serverId }, query: { after, limit: 1000 } },
        }),
      );
      return res;
    };

    const tick = async () => {
      try {
        const res = await fetchPage(lastSeqRef.current);
        if (cancelled) return;
        push(res.lines);
        setPhase("ready");
      } catch {
        if (cancelled) return;
        if (lastSeqRef.current < 0) setPhase("error");
      }
      if (!cancelled) timer = setTimeout(() => void tick(), POLL_MS);
    };

    lastSeqRef.current = -1;
    setLines([]);
    setPhase("loading");
    void tick();
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, [panelId, serverId, push, retryToken]);

  useEffect(() => {
    return live.subscribe(ampTopic(panelId), (payload) => {
      const frame = payload as { event?: string; data?: { instanceId?: string; lines?: AmpConsoleLine[] } };
      if (frame?.event === "console" && frame.data?.instanceId === serverId && frame.data.lines) {
        push(frame.data.lines);
      }
    });
  }, [panelId, serverId, push]);

  // Pin-to-bottom scrolling.
  useEffect(() => {
    const el = scrollRef.current;
    if (el && pinnedRef.current) el.scrollTop = el.scrollHeight;
  }, [lines]);

  const onScroll = () => {
    const el = scrollRef.current;
    if (!el) return;
    pinnedRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < PIN_THRESHOLD;
  };

  return (
    <div
      className="border-edge flex flex-col overflow-hidden rounded-lg border"
      style={{ background: PANE.bg, height: "max(420px, calc(100vh - 24rem))" }}
    >
      <div ref={scrollRef} onScroll={onScroll} className="flex-1 overflow-y-auto px-3 py-2">
        {phase === "loading" && (
          <p className="font-mono text-[12px]" style={{ color: PANE.dim }}>
            {t("amp.console.loading")}
          </p>
        )}
        {phase === "error" && (
          <p className="font-mono text-[12px]" style={{ color: PANE.error }}>
            {t("amp.console.loadFailed")}{" "}
            <button className="underline" onClick={() => setRetryToken((n) => n + 1)}>
              {t("common.retry")}
            </button>
          </p>
        )}
        {phase === "ready" && lines.length === 0 && (
          <p className="font-mono text-[12px]" style={{ color: PANE.dim }}>
            {t("amp.console.empty")}
          </p>
        )}
        {lines.map((line) => (
          <div key={line.seq} className="flex gap-2 font-mono text-[12.5px] leading-5">
            <span className="shrink-0 select-none" style={{ color: PANE.dim }}>
              {fmtTime(line.ts)}
            </span>
            <span className="min-w-0 break-words whitespace-pre-wrap" style={{ color: lineColor(line.type) }}>
              {line.contents}
            </span>
          </div>
        ))}
      </div>
      {controllable && <AmpCommandBar panelId={panelId} serverId={serverId} />}
    </div>
  );
}

function AmpCommandBar({ panelId, serverId }: { panelId: string; serverId: string }) {
  const { t } = useTranslation();
  const [value, setValue] = useState("");
  const [feedback, setFeedback] = useState<string | null>(null);
  const historyRef = useRef<string[]>([]);
  const indexRef = useRef<number | null>(null);
  const draftRef = useRef("");

  const send = useMutation({
    mutationFn: async (command: string) => {
      unwrap(
        await api.POST("/amp/{id}/instances/{instanceId}/command", {
          params: { path: { id: panelId, instanceId: serverId } },
          body: { command },
        }),
      );
    },
    // Failures print inline like a console would — sending is never blocked.
    onError: (err) => setFeedback(isApiError(err) ? err.message : t("common.unknownError")),
  });

  const submit = () => {
    const command = value.trim();
    if (!command) return;
    const history = historyRef.current;
    if (history[history.length - 1] !== command) history.push(command);
    indexRef.current = null;
    setValue("");
    setFeedback(null);
    send.mutate(command);
  };

  const onKeyDown = (e: ReactKeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") {
      e.preventDefault();
      submit();
      return;
    }
    const history = historyRef.current;
    if (e.key === "ArrowUp") {
      if (history.length === 0) return;
      e.preventDefault();
      if (indexRef.current === null) {
        draftRef.current = value;
        indexRef.current = history.length - 1;
      } else if (indexRef.current > 0) {
        indexRef.current -= 1;
      }
      setValue(history[indexRef.current]);
    } else if (e.key === "ArrowDown") {
      if (indexRef.current === null) return;
      e.preventDefault();
      if (indexRef.current < history.length - 1) {
        indexRef.current += 1;
        setValue(history[indexRef.current]);
      } else {
        indexRef.current = null;
        setValue(draftRef.current);
      }
    } else {
      indexRef.current = null;
    }
  };

  return (
    <div style={{ background: PANE.chrome }}>
      {feedback && (
        <p className="border-t px-3 py-1 font-mono text-[12px]" style={{ borderColor: PANE.edge, color: "var(--hh-warning)" }}>
          ! {feedback}
        </p>
      )}
      <div className="flex items-center gap-1.5 border-t px-3" style={{ borderColor: PANE.edge }}>
        {send.isPending ? (
          <Loader2 aria-hidden className="size-4 shrink-0 animate-spin" style={{ color: PANE.dim }} />
        ) : (
          <ChevronRight aria-hidden className="size-4 shrink-0" style={{ color: PANE.dim }} />
        )}
        <input
          value={value}
          onChange={(e) => {
            setFeedback(null);
            setValue(e.target.value);
          }}
          onKeyDown={onKeyDown}
          placeholder={t("amp.console.commandPlaceholder")}
          aria-label={t("amp.console.commandAria")}
          autoComplete="off"
          autoCorrect="off"
          autoCapitalize="off"
          spellCheck={false}
          className={cn("h-10 w-full bg-transparent font-mono text-[13px] placeholder:text-[#5b6272] focus:outline-none")}
          style={{ color: PANE.accentText, caretColor: PANE.accentText }}
        />
      </div>
    </div>
  );
}
