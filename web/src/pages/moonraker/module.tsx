import { useEffect, useMemo, useRef, useState } from "react";
import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  AlertTriangle,
  ArrowDown,
  ArrowDownToLine,
  ArrowLeft,
  ArrowRight,
  ArrowUp,
  ArrowUpFromLine,
  ChevronDown,
  ExternalLink,
  Fan as FanIcon,
  FlipHorizontal2,
  FlipVertical2,
  Home,
  Lightbulb,
  Maximize2,
  Move3d,
  OctagonX,
  Pause,
  Play,
  Power,
  Printer,
  RefreshCw,
  RotateCw,
  Search,
  Square,
  Terminal,
  Thermometer,
  Video,
  VideoOff,
} from "lucide-react";
import {
  type MoonrakerCamera,
  type MoonrakerFan,
  type MoonrakerLed,
  type MoonrakerOutputPin,
  type MoonrakerSnapshot,
} from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useIntegrations, statusTone } from "@/lib/systems";
import { live } from "@/lib/ws";
import { cn } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Card, CardBody } from "@/ui/card";
import { ConfirmDialog } from "@/ui/confirm";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Input } from "@/ui/input";
import { Slider } from "@/ui/slider";
import { Tooltip } from "@/ui/tooltip";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { useToast } from "@/ui/toast";
import {
  mrTopic,
  useMoonrakerEstop,
  useMoonrakerFirmwareRestart,
  useMoonrakerGcode,
  useMoonrakerPrint,
  useMoonrakerSetTemp,
  useMoonrakerSetWebcam,
  useMoonrakerSnapshot,
  webcamSnapshotUrl,
  webcamStreamUrl,
} from "./shared";

type Pt = { ts: number; value: number };
type HistoryRef = React.MutableRefObject<Map<string, Pt[]>>;

// Distinct colours for the temperature table dots + graph lines.
const TEMP_COLORS = [
  "#ef4444", "#f59e0b", "#eab308", "#22c55e", "#06b6d4", "#3b82f6",
  "#8b5cf6", "#ec4899", "#f97316", "#14b8a6", "#a3e635", "#60a5fa",
];

type TempRow = {
  key: string;
  object: string;
  name: string;
  color: string;
  temp: number;
  target?: number;
  power?: number; // heater duty 0..1
  speed?: number; // temp-fan speed 0..1
  kind: "extruder" | "bed" | "generic" | "tempfan" | "sensor";
  editable: boolean;
};

/**
 * MoonrakerModulePage is a Fluidd-style printer dashboard: a Status/Tool/Camera
 * /Macros/Fans column on the left and a Thermals (table + live graph) + Console
 * column on the right. It renders whatever the snapshot reports — the object set
 * is discovered per printer — so any Klipper machine gets a correct board.
 */
export function MoonrakerModulePage() {
  const { t } = useTranslation();
  const { can } = useAuth();
  const { instanceId } = useParams({ from: "/shell/moonraker/$instanceId" });
  const integrations = useIntegrations();
  const snapshot = useMoonrakerSnapshot(instanceId);
  const snap = snapshot.data;

  // Rolling temperature history for the graph + change rates.
  const history = useRef<Map<string, Pt[]>>(new Map());
  useEffect(() => {
    if (!snap) return;
    const now = Date.now();
    const push = (key: string, value: number) => {
      const arr = history.current.get(key) ?? [];
      if (arr.length === 0 || arr[arr.length - 1].value !== value || now - arr[arr.length - 1].ts > 3000) {
        arr.push({ ts: now, value });
      }
      while (arr.length > 0 && now - arr[0].ts > 10 * 60_000) arr.shift();
      while (arr.length > 400) arr.shift();
      history.current.set(key, arr);
    };
    snap.heaters.forEach((h) => push("h:" + h.object, h.temp));
    snap.tempFans.forEach((f) => push("f:" + f.object, f.temp));
    snap.sensors.forEach((s) => push("s:" + s.object, s.temp));
  }, [snap]);

  const tempRows = useMemo<TempRow[]>(() => {
    if (!snap) return [];
    const rows: TempRow[] = [];
    let i = 0;
    const nextColor = () => TEMP_COLORS[i++ % TEMP_COLORS.length];
    snap.heaters.forEach((h) =>
      rows.push({ key: "h:" + h.object, object: h.object, name: h.name, color: nextColor(), temp: h.temp, target: h.target, power: h.power, kind: h.kind as TempRow["kind"], editable: true }),
    );
    snap.tempFans.forEach((f) =>
      rows.push({ key: "f:" + f.object, object: f.object, name: f.name, color: nextColor(), temp: f.temp, target: f.target, speed: f.speed, kind: "tempfan", editable: true }),
    );
    snap.sensors.forEach((s) =>
      rows.push({ key: "s:" + s.object, object: s.object, name: s.name, color: nextColor(), temp: s.temp, kind: "sensor", editable: false }),
    );
    return rows;
  }, [snap]);

  if (integrations.isLoading || snapshot.isLoading) return <PageLoader />;
  const instance = integrations.data?.find((row) => row.id === instanceId);
  if (!instance) {
    return (
      <Card>
        <EmptyState icon={AlertTriangle} title={t("errors.notFound.title")} body={t("errors.notFound.body")} />
      </Card>
    );
  }
  if (snapshot.isError || !snap) {
    return (
      <div className="space-y-4">
        <Header name={instance.name} status={instance.status} snap={undefined} instanceId={instanceId} />
        <Card>
          <ErrorState error={snapshot.error} onRetry={() => void snapshot.refetch()} />
        </Card>
      </div>
    );
  }

  const klippyBad = snap.klippy.state === "shutdown" || snap.klippy.state === "error";
  const hasControl = can("moonraker/" + instanceId, "control");
  const baseUrl = typeof instance.config?.baseUrl === "string" ? instance.config.baseUrl : null;

  return (
    <div className="space-y-4">
      <Header name={instance.name} status={instance.status} snap={snap} instanceId={instanceId} canControl={hasControl} baseUrl={baseUrl} />

      {klippyBad && <KlippyBanner snap={snap} instanceId={instanceId} canControl={hasControl} />}

      <div className="grid items-start gap-4 xl:grid-cols-2">
        {/* Left column: status, camera, tool, macros, fans & outputs. */}
        <div className="space-y-4">
          <StatusCard snap={snap} instanceId={instanceId} canControl={hasControl} />
          {snap.cameras.length > 0 && <CameraSection cameras={snap.cameras} instanceId={instanceId} canControl={hasControl} />}
          {snap.toolhead && <ToolCard snap={snap} instanceId={instanceId} canControl={hasControl} />}
          {snap.macros.length > 0 && <MacroCard snap={snap} instanceId={instanceId} canControl={hasControl} />}
          <FansOutputsCard snap={snap} instanceId={instanceId} canControl={hasControl} />
        </div>

        {/* Right column: thermals (table + graph) and the console. */}
        <div className="space-y-4">
          {tempRows.length > 0 && (
            <ThermalsCard rows={tempRows} history={history} instanceId={instanceId} canControl={hasControl} />
          )}
          <ConsoleCard instanceId={instanceId} canControl={hasControl} />
          {snap.extra.length > 0 && (
            <FCard icon={Move3d} title={t("moonraker.otherObjects")} defaultOpen={false}>
              <div className="flex flex-wrap gap-1.5">
                {snap.extra.map((o) => (
                  <span key={o} className="border-edge text-fg-subtle rounded bg-bg-subtle px-1.5 py-0.5 font-mono text-[11px]">
                    {o}
                  </span>
                ))}
              </div>
            </FCard>
          )}
        </div>
      </div>
    </div>
  );
}

// ---- card chrome ------------------------------------------------------------

/** FCard is the Fluidd-style card: a header bar (icon · title · actions ·
 * collapse) over a body that folds away. */
function FCard({
  icon: Icon,
  title,
  actions,
  defaultOpen = true,
  bodyClass,
  children,
}: {
  icon?: React.ComponentType<{ className?: string }>;
  title: React.ReactNode;
  actions?: React.ReactNode;
  defaultOpen?: boolean;
  bodyClass?: string;
  children: React.ReactNode;
}) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <Card className="overflow-hidden">
      <div className="border-edge bg-bg-subtle/40 flex items-center gap-2 border-b px-3 py-2">
        {Icon && <Icon className="text-fg-subtle size-4 shrink-0" />}
        <span className="text-fg truncate text-[13px] font-semibold">{title}</span>
        <div className="ml-auto flex items-center gap-1.5">
          {actions}
          <button
            type="button"
            onClick={() => setOpen((v) => !v)}
            aria-label="Collapse"
            className="text-fg-subtle hover:text-fg flex size-6 items-center justify-center rounded transition-colors"
          >
            <ChevronDown className={cn("size-4 transition-transform", !open && "-rotate-90")} />
          </button>
        </div>
      </div>
      {open && <div className={cn("p-3", bodyClass)}>{children}</div>}
    </Card>
  );
}

// ---- header + banners -------------------------------------------------------

function Header({
  name,
  status,
  snap,
  instanceId,
  canControl,
  baseUrl,
}: {
  name: string;
  status: string;
  snap: MoonrakerSnapshot | undefined;
  instanceId: string;
  canControl?: boolean;
  baseUrl?: string | null;
}) {
  const { t } = useTranslation();
  const estop = useMoonrakerEstop(instanceId);
  const toast = useToast();
  const [confirmEstop, setConfirmEstop] = useState(false);
  const tone = statusTone[status as keyof typeof statusTone] ?? "neutral";
  const klippy = snap?.klippy.state;

  return (
    <div className="flex flex-wrap items-center gap-3">
      <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
        <Printer className="size-5" />
      </span>
      <div className="min-w-0 flex-1">
        <h1 className="text-fg flex flex-wrap items-center gap-2.5 text-xl font-semibold tracking-tight">
          {name}
          <Badge tone={tone}>
            <StatusDot tone={tone} pulse={status !== "connected"} />
            {t(`integrations.status.${status}`)}
          </Badge>
          {klippy && <Badge tone={klippyTone(klippy)}>{t(`moonraker.klippy.${klippy}`, klippy)}</Badge>}
          {snap && (
            <span className="text-fg-subtle inline-flex items-center gap-1 text-[11.5px] font-normal">
              <StatusDot tone={snap.live ? "success" : "neutral"} pulse={snap.live} />
              {snap.live ? t("moonraker.live") : t("moonraker.polling")}
            </span>
          )}
        </h1>
        <p className="text-fg-muted text-sm">
          {snap?.printer.klipperVersion
            ? t("moonraker.subtitleVersion", { version: shortVersion(snap.printer.klipperVersion) })
            : t("moonraker.subtitle")}
        </p>
      </div>
      {baseUrl && (
        <a
          href={baseUrl}
          target="_blank"
          rel="noopener noreferrer"
          className="border-edge bg-surface hover:bg-surface-hover text-fg flex items-center gap-1.5 rounded-md border px-3 py-1.5 text-[13px] font-medium shadow-1 transition-colors"
        >
          <ExternalLink className="size-3.5" />
          {t("moonraker.open")}
        </a>
      )}
      {canControl && status === "connected" && (
        <Button variant="danger" onClick={() => setConfirmEstop(true)} loading={estop.isPending}>
          <OctagonX className="size-4" />
          {t("moonraker.estop")}
        </Button>
      )}
      <ConfirmDialog
        open={confirmEstop}
        onOpenChange={setConfirmEstop}
        tone="danger"
        title={t("moonraker.estopConfirm.title")}
        body={t("moonraker.estopConfirm.body")}
        confirmLabel={t("moonraker.estop")}
        onConfirm={() =>
          estop.mutate(undefined, {
            onSuccess: () => toast("warning", t("moonraker.toast.estop")),
            onError: (e) => toast("error", t("moonraker.toast.failed"), String(e)),
          })
        }
      />
    </div>
  );
}

function KlippyBanner({ snap, instanceId, canControl }: { snap: MoonrakerSnapshot; instanceId: string; canControl?: boolean }) {
  const { t } = useTranslation();
  const restart = useMoonrakerFirmwareRestart(instanceId);
  const toast = useToast();
  return (
    <Card className="border-danger/40">
      <CardBody className="flex flex-wrap items-start gap-3">
        <AlertTriangle className="text-danger mt-0.5 size-5 shrink-0" />
        <div className="min-w-0 flex-1">
          <p className="text-fg text-sm font-semibold">{t(`moonraker.klippy.${snap.klippy.state}`, snap.klippy.state)}</p>
          {snap.klippy.message && (
            <pre className="text-fg-muted mt-1 max-h-40 overflow-auto text-[12.5px] whitespace-pre-wrap">{snap.klippy.message}</pre>
          )}
        </div>
        {canControl && (
          <Button
            variant="secondary"
            loading={restart.isPending}
            onClick={() =>
              restart.mutate(undefined, {
                onSuccess: () => toast("info", t("moonraker.toast.firmwareRestart")),
                onError: (e) => toast("error", t("moonraker.toast.failed"), String(e)),
              })
            }
          >
            <RefreshCw className="size-4" />
            {t("moonraker.firmwareRestart")}
          </Button>
        )}
      </CardBody>
    </Card>
  );
}

// ---- status / print ---------------------------------------------------------

function StatusCard({ snap, instanceId, canControl }: { snap: MoonrakerSnapshot; instanceId: string; canControl?: boolean }) {
  const { t } = useTranslation();
  const print = snap.print;
  const printMut = useMoonrakerPrint(instanceId);
  const toast = useToast();
  const [confirmCancel, setConfirmCancel] = useState(false);
  const state = print?.state ?? "standby";
  const active = state === "printing" || state === "paused";
  const printing = state === "printing";
  const pct = Math.round(clamp01(print?.progress ?? 0) * 100);
  const eta =
    printing && print && print.progress > 0.01 && print.printDuration > 0
      ? (print.printDuration * (1 - print.progress)) / print.progress
      : null;
  const act = (action: "pause" | "resume" | "cancel") =>
    printMut.mutate(action, { onError: (e) => toast("error", t("moonraker.toast.failed"), String(e)) });

  return (
    <FCard
      icon={Printer}
      title={t("moonraker.sections.status")}
      actions={<Badge tone={printStateTone(state)}>{t(`moonraker.printState.${state}`, state)}</Badge>}
    >
      <div className="flex items-center gap-4">
        {active || state === "complete" ? <ProgressRing pct={pct} state={state} /> : null}
        <div className="min-w-0 flex-1">
          {print && print.filename ? (
            <p className="text-fg truncate text-[14px] font-medium" title={print.filename}>
              {print.filename}
            </p>
          ) : (
            <p className="text-fg-muted text-[14px]">{t("moonraker.print.idle")}</p>
          )}
          {print && (active || state === "complete") && (
            <div className="text-fg-muted mt-1 flex flex-wrap gap-x-4 gap-y-0.5 text-[12.5px] tabular-nums">
              {print.totalLayer > 0 && <span>{t("moonraker.print.layer", { current: print.currentLayer, total: print.totalLayer })}</span>}
              <span>{t("moonraker.print.elapsed")} {fmtDuration(print.printDuration)}</span>
              {eta !== null && <span>{t("moonraker.print.remaining")} {fmtDuration(eta)}</span>}
              {print.filamentUsed > 0 && <span>{(print.filamentUsed / 1000).toFixed(2)} m</span>}
            </div>
          )}
        </div>
        {canControl && active && (
          <div className="flex shrink-0 items-center gap-2">
            {printing ? (
              <Button variant="secondary" onClick={() => act("pause")} loading={printMut.isPending}>
                <Pause className="size-4" />
                {t("moonraker.print.pause")}
              </Button>
            ) : (
              <Button variant="primary" onClick={() => act("resume")} loading={printMut.isPending}>
                <Play className="size-4" />
                {t("moonraker.print.resume")}
              </Button>
            )}
            <Button variant="danger-ghost" onClick={() => setConfirmCancel(true)}>
              <Square className="size-4" />
              {t("moonraker.print.cancel")}
            </Button>
          </div>
        )}
      </div>
      {active && (
        <div className="bg-bg-subtle mt-3 h-1.5 w-full overflow-hidden rounded-full">
          <div className="bg-accent h-full rounded-full transition-all" style={{ width: `${pct}%` }} />
        </div>
      )}
      <ConfirmDialog
        open={confirmCancel}
        onOpenChange={setConfirmCancel}
        tone="danger"
        title={t("moonraker.print.cancelConfirm.title")}
        body={t("moonraker.print.cancelConfirm.body")}
        confirmLabel={t("moonraker.print.cancel")}
        onConfirm={() => act("cancel")}
      />
    </FCard>
  );
}

function ProgressRing({ pct, state }: { pct: number; state: string }) {
  const r = 24;
  const c = 2 * Math.PI * r;
  const off = c * (1 - pct / 100);
  const stroke = state === "complete" ? "var(--color-success)" : state === "paused" ? "var(--color-warning)" : "var(--color-accent)";
  return (
    <div className="relative size-14 shrink-0">
      <svg viewBox="0 0 60 60" className="size-14 -rotate-90">
        <circle cx="30" cy="30" r={r} fill="none" stroke="var(--color-edge)" strokeWidth="6" />
        <circle cx="30" cy="30" r={r} fill="none" stroke={stroke} strokeWidth="6" strokeLinecap="round" strokeDasharray={c} strokeDashoffset={off} className="transition-all" />
      </svg>
      <span className="text-fg absolute inset-0 flex items-center justify-center text-[12px] font-semibold tabular-nums">{pct}%</span>
    </div>
  );
}

// ---- thermals (table + graph) ----------------------------------------------

function ThermalsCard({ rows, history, instanceId, canControl }: { rows: TempRow[]; history: HistoryRef; instanceId: string; canControl?: boolean }) {
  const { t } = useTranslation();
  const setTemp = useMoonrakerSetTemp(instanceId);
  const gcode = useMoonrakerGcode(instanceId);
  const toast = useToast();

  const setTarget = (row: TempRow, v: number) => {
    if (row.kind === "tempfan") {
      gcode.mutate(`SET_TEMPERATURE_FAN_TARGET temperature_fan=${configName(row.object)} target=${v}`, {
        onError: (e) => toast("error", t("moonraker.toast.failed"), String(e)),
      });
    } else {
      setTemp.mutate({ heater: row.object, target: v }, { onError: (e) => toast("error", t("moonraker.toast.failed"), String(e)) });
    }
  };

  return (
    <FCard icon={Thermometer} title={t("moonraker.sections.thermals")}>
      <div className="overflow-x-auto">
        <table className="w-full text-[12.5px]">
          <thead>
            <tr className="text-fg-subtle text-[10.5px] font-semibold uppercase tracking-wide">
              <th className="py-1 pr-2 text-left font-semibold">{t("moonraker.thermals.name")}</th>
              <th className="px-2 py-1 text-right font-semibold">{t("moonraker.thermals.power")}</th>
              <th className="px-2 py-1 text-right font-semibold">{t("moonraker.thermals.change")}</th>
              <th className="px-2 py-1 text-right font-semibold">{t("moonraker.thermals.actual")}</th>
              <th className="py-1 pl-2 text-right font-semibold">{t("moonraker.thermals.target")}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => {
              const pts = history.current.get(row.key) ?? [];
              const change = rateOf(pts);
              const power =
                row.kind === "tempfan"
                  ? `${Math.round((row.speed ?? 0) * 100)}%`
                  : row.kind === "sensor"
                    ? ""
                    : (row.target ?? 0) > 0
                      ? `${Math.round((row.power ?? 0) * 100)}%`
                      : t("moonraker.temp.off");
              return (
                <tr key={row.key} className="border-edge/60 border-t">
                  <td className="py-1.5 pr-2">
                    <span className="flex items-center gap-2">
                      <span className="size-2 shrink-0 rounded-full" style={{ background: row.color }} />
                      <span className="text-fg truncate" title={row.name}>{row.name}</span>
                    </span>
                  </td>
                  <td className="text-fg-muted px-2 text-right tabular-nums">{power}</td>
                  <td className={cn("px-2 text-right tabular-nums", change > 0.05 ? "text-warning" : change < -0.05 ? "text-info" : "text-fg-subtle")}>
                    {change >= 0 ? "+" : ""}
                    {change.toFixed(1)}
                  </td>
                  <td className="text-fg px-2 text-right font-semibold tabular-nums">{row.temp.toFixed(1)}°</td>
                  <td className="py-1.5 pl-2 text-right">
                    {row.editable ? (
                      <TargetInput value={row.target ?? 0} canControl={canControl} onSet={(v) => setTarget(row, v)} />
                    ) : (
                      <span className="text-fg-subtle">–</span>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <TemperatureGraph rows={rows} history={history} />
    </FCard>
  );
}

function TargetInput({ value, canControl, onSet }: { value: number; canControl?: boolean; onSet: (v: number) => void }) {
  const [draft, setDraft] = useState<string | null>(null);
  if (!canControl) {
    return <span className="text-fg-muted tabular-nums">{value > 0 ? `${Math.round(value)}°` : "–"}</span>;
  }
  const shown = draft ?? (value > 0 ? String(Math.round(value)) : "");
  const commit = () => {
    if (draft == null) return;
    const n = Number(draft);
    setDraft(null);
    if (!Number.isNaN(n)) onSet(Math.max(0, Math.round(n)));
  };
  return (
    <input
      type="number"
      value={shown}
      placeholder="0"
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === "Enter") e.currentTarget.blur();
      }}
      className="border-edge focus:border-accent w-14 rounded border bg-transparent px-1.5 py-0.5 text-right text-[12.5px] tabular-nums outline-none"
    />
  );
}

function TemperatureGraph({ rows, history }: { rows: TempRow[]; history: HistoryRef }) {
  const { t } = useTranslation();
  const W = 600;
  const H = 190;
  const series = rows
    .map((r) => ({ color: r.color, points: history.current.get(r.key) ?? [] }))
    .filter((s) => s.points.length > 1);
  if (series.length === 0) {
    return <p className="text-fg-subtle mt-3 text-center text-[12px]">{t("moonraker.thermals.collecting")}</p>;
  }
  let tMin = Infinity;
  let tMax = -Infinity;
  let vMax = 60;
  for (const s of series) {
    for (const p of s.points) {
      if (p.ts < tMin) tMin = p.ts;
      if (p.ts > tMax) tMax = p.ts;
      if (p.value > vMax) vMax = p.value;
    }
  }
  vMax = Math.ceil(vMax / 20) * 20; // round up to a tidy gridline
  const span = Math.max(1, tMax - tMin);
  const x = (ts: number) => ((ts - tMin) / span) * W;
  const y = (v: number) => H - (v / vMax) * H;
  const gridlines = [0, 0.25, 0.5, 0.75, 1];

  return (
    <div className="mt-3">
      <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" className="h-44 w-full">
        {gridlines.map((g) => (
          <line key={g} x1={0} x2={W} y1={g * H} y2={g * H} stroke="var(--color-edge)" strokeWidth={1} opacity={0.5} />
        ))}
        {series.map((s, i) => (
          <polyline
            key={i}
            fill="none"
            stroke={s.color}
            strokeWidth={1.6}
            strokeLinejoin="round"
            strokeLinecap="round"
            vectorEffect="non-scaling-stroke"
            points={s.points.map((p) => `${x(p.ts).toFixed(1)},${y(p.value).toFixed(1)}`).join(" ")}
          />
        ))}
      </svg>
      <div className="text-fg-subtle mt-0.5 flex justify-between text-[10px] tabular-nums">
        <span>0°</span>
        <span>{vMax}°</span>
      </div>
    </div>
  );
}

// ---- tool (jog + extrude + factors) ----------------------------------------

const JOG_STEPS = [0.1, 1, 10, 25, 50, 100] as const;

function ToolCard({ snap, instanceId, canControl }: { snap: MoonrakerSnapshot; instanceId: string; canControl?: boolean }) {
  const { t } = useTranslation();
  const th = snap.toolhead!;
  const gcode = useMoonrakerGcode(instanceId);
  const toast = useToast();
  const [step, setStep] = useState(10);
  const [extrudeLen, setExtrudeLen] = useState(10);
  const [extrudeSpeed, setExtrudeSpeed] = useState(5);
  const printing = snap.print?.state === "printing";
  const busy = printing || snap.print?.state === "paused";
  const extruder = snap.heaters.find((h) => h.kind === "extruder");
  const tooCold = !extruder || extruder.temp < 170;
  const canJog = canControl && !printing;
  const canExtrude = canControl && !tooCold && !busy;

  const run = (script: string) => gcode.mutate(script, { onError: (e) => toast("error", t("moonraker.toast.failed"), String(e)) });
  const jog = (axis: "X" | "Y" | "Z", sign: 1 | -1) => run(`G91\nG1 ${axis}${sign * step} F${axis === "Z" ? 900 : 6000}\nG90`);
  const extrude = (sign: 1 | -1) =>
    run(`SAVE_GCODE_STATE NAME=hh_extrude\nM83\nG1 E${sign * extrudeLen} F${Math.round(extrudeSpeed * 60)}\nRESTORE_GCODE_STATE NAME=hh_extrude`);
  const homed = (a: string) => th.homedAxes.toLowerCase().includes(a);

  const JogBtn = ({ axis, sign, icon: Icon, label }: { axis: "X" | "Y" | "Z"; sign: 1 | -1; icon: typeof ArrowUp; label: string }) => (
    <button
      type="button"
      disabled={!canJog}
      aria-label={label}
      onClick={() => jog(axis, sign)}
      className="border-edge hover:border-accent hover:bg-accent-soft/40 text-fg flex size-9 items-center justify-center rounded-md border transition-colors disabled:cursor-not-allowed disabled:opacity-40"
    >
      <Icon className="size-4" />
    </button>
  );

  return (
    <FCard
      icon={Move3d}
      title={t("moonraker.sections.tool")}
      actions={
        canControl ? (
          <button
            type="button"
            disabled={busy}
            onClick={() => run("M84")}
            className="border-edge hover:border-accent text-fg-muted rounded border px-2 py-0.5 text-[11px] font-medium uppercase transition-colors disabled:opacity-40"
          >
            {t("moonraker.motion.motorsOff")}
          </button>
        ) : undefined
      }
    >
      <div className="grid gap-4 sm:grid-cols-2">
        {/* Jog pad + step */}
        <div className="space-y-3">
          <div className="flex items-start justify-center gap-4">
            <div className="grid grid-cols-3 grid-rows-3 gap-1">
              <span />
              <JogBtn axis="Y" sign={1} icon={ArrowUp} label={t("moonraker.jog.yPlus")} />
              <span />
              <JogBtn axis="X" sign={-1} icon={ArrowLeft} label={t("moonraker.jog.xMinus")} />
              <button
                type="button"
                disabled={!canJog}
                aria-label={t("moonraker.motion.homeAll")}
                onClick={() => run("G28")}
                className="border-edge hover:border-accent hover:bg-accent-soft/40 text-fg-muted flex size-9 items-center justify-center rounded-md border transition-colors disabled:cursor-not-allowed disabled:opacity-40"
              >
                <Home className="size-4" />
              </button>
              <JogBtn axis="X" sign={1} icon={ArrowRight} label={t("moonraker.jog.xPlus")} />
              <span />
              <JogBtn axis="Y" sign={-1} icon={ArrowDown} label={t("moonraker.jog.yMinus")} />
              <span />
            </div>
            <div className="flex flex-col items-center gap-1">
              <span className="text-fg-subtle text-[11px] font-semibold">Z</span>
              <JogBtn axis="Z" sign={1} icon={ArrowUp} label={t("moonraker.jog.zPlus")} />
              <JogBtn axis="Z" sign={-1} icon={ArrowDown} label={t("moonraker.jog.zMinus")} />
            </div>
          </div>
          <div className="flex flex-wrap items-center justify-center gap-1">
            {JOG_STEPS.map((s) => (
              <button
                key={s}
                type="button"
                onClick={() => setStep(s)}
                className={cn(
                  "rounded border px-2 py-0.5 text-[11.5px] font-medium tabular-nums transition-colors",
                  step === s ? "border-accent bg-accent-soft text-accent-soft-fg" : "border-edge text-fg-muted hover:border-accent",
                )}
              >
                {s}
              </button>
            ))}
            <span className="text-fg-subtle text-[10.5px]">mm</span>
          </div>
        </div>

        {/* Position + extrude */}
        <div className="space-y-3">
          <div className="grid grid-cols-3 gap-2">
            {(["x", "y", "z"] as const).map((axis, i) => (
              <div key={axis} className="border-edge rounded-lg border p-1.5 text-center">
                <div className="text-fg-subtle text-[10.5px] font-semibold uppercase">{axis}</div>
                <div className="text-fg text-[15px] font-semibold tabular-nums">{th.position[i] != null ? th.position[i].toFixed(1) : "—"}</div>
                <div className={cn("text-[9.5px]", homed(axis) ? "text-success" : "text-fg-subtle")}>{homed(axis) ? t("moonraker.motion.homed") : "—"}</div>
              </div>
            ))}
          </div>
          <div className="flex items-end gap-2">
            <label className="flex-1">
              <span className="text-fg-subtle mb-0.5 block text-[10.5px]">{t("moonraker.extruder.length")}</span>
              <Input type="number" value={extrudeLen} onChange={(e) => setExtrudeLen(Number(e.target.value) || 0)} className="h-8" />
            </label>
            <label className="flex-1">
              <span className="text-fg-subtle mb-0.5 block text-[10.5px]">{t("moonraker.extruder.speed")}</span>
              <Input type="number" value={extrudeSpeed} onChange={(e) => setExtrudeSpeed(Number(e.target.value) || 0)} className="h-8" />
            </label>
          </div>
          <div className="grid grid-cols-2 gap-2">
            <Button variant="secondary" className="h-8" disabled={!canExtrude} onClick={() => extrude(-1)}>
              <ArrowUpFromLine className="size-3.5" />
              {t("moonraker.extruder.retract")}
            </Button>
            <Button variant="secondary" className="h-8" disabled={!canExtrude} onClick={() => extrude(1)}>
              <ArrowDownToLine className="size-3.5" />
              {t("moonraker.extruder.extrude")}
            </Button>
          </div>
          {canControl && tooCold && !busy && <p className="text-warning text-center text-[10.5px]">{t("moonraker.extruder.cold")}</p>}
        </div>
      </div>

      {/* Speed & flow — safe mid-print */}
      <div className="mt-3 grid gap-3 sm:grid-cols-2">
        <FactorSlider label={t("moonraker.motion.speed")} value={Math.round((th.speedFactor ?? 1) * 100)} onCommit={(v) => run(`M220 S${v}`)} disabled={!canControl} />
        <FactorSlider label={t("moonraker.motion.flow")} value={Math.round((th.extrudeFactor ?? 1) * 100)} onCommit={(v) => run(`M221 S${v}`)} disabled={!canControl} />
      </div>
      {printing && <p className="text-fg-subtle mt-2 text-center text-[11px]">{t("moonraker.jog.lockedPrinting")}</p>}
    </FCard>
  );
}

function FactorSlider({ label, value, onCommit, disabled }: { label: string; value: number; onCommit: (v: number) => void; disabled?: boolean }) {
  const [draft, setDraft] = useState<number | null>(null);
  const shown = draft ?? value;
  return (
    <div>
      <div className="mb-1 flex items-center justify-between">
        <span className="text-fg-muted text-[12px]">{label}</span>
        <span className="text-fg text-[12.5px] font-semibold tabular-nums">{shown}%</span>
      </div>
      <Slider
        value={shown}
        min={50}
        max={200}
        step={5}
        disabled={disabled}
        aria-label={label}
        onValueChange={setDraft}
        onValueCommit={(v) => {
          setDraft(null);
          onCommit(v);
        }}
      />
    </div>
  );
}

// ---- fans & outputs ---------------------------------------------------------

function FansOutputsCard({ snap, instanceId, canControl }: { snap: MoonrakerSnapshot; instanceId: string; canControl?: boolean }) {
  const { t } = useTranslation();
  if (snap.fans.length === 0 && snap.leds.length === 0 && snap.outputPins.length === 0) return null;
  return (
    <FCard icon={FanIcon} title={t("moonraker.sections.fansOutputs")}>
      <div className="divide-edge/60 divide-y">
        {snap.fans.map((f) => (
          <FanRow key={f.object} fan={f} instanceId={instanceId} canControl={canControl} />
        ))}
        {snap.leds.map((l) => (
          <LedRow key={l.object} led={l} instanceId={instanceId} canControl={canControl} />
        ))}
        {snap.outputPins.map((p) => (
          <OutputPinRow key={p.object} pin={p} instanceId={instanceId} canControl={canControl} />
        ))}
      </div>
    </FCard>
  );
}

function Row({ label, icon, children }: { label: string; icon?: React.ReactNode; children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-3 py-2 first:pt-0 last:pb-0">
      <span className="text-fg flex min-w-0 items-center gap-1.5 text-[12.5px]">
        {icon}
        <span className="truncate" title={label}>{label}</span>
      </span>
      <div className="ml-auto flex min-w-0 items-center gap-2">{children}</div>
    </div>
  );
}

function FanRow({ fan, instanceId, canControl }: { fan: MoonrakerFan; instanceId: string; canControl?: boolean }) {
  const { t } = useTranslation();
  const gcode = useMoonrakerGcode(instanceId);
  const toast = useToast();
  const [draft, setDraft] = useState<number | null>(null);
  const pct = Math.round((draft ?? fan.speed) * 100);
  const commit = (value: number) => {
    setDraft(null);
    const frac = value / 100;
    const script = fan.kind === "part" ? (frac <= 0 ? "M107" : `M106 S${Math.round(frac * 255)}`) : `SET_FAN_SPEED FAN=${configName(fan.object)} SPEED=${frac.toFixed(2)}`;
    gcode.mutate(script, { onError: (e) => toast("error", t("moonraker.toast.failed"), String(e)) });
  };
  return (
    <Row
      label={fan.name}
      icon={<FanIcon className={cn("size-3.5 shrink-0", fan.speed > 0 ? "text-info animate-spin [animation-duration:1.6s]" : "text-fg-subtle")} />}
    >
      {fan.controllable && canControl ? (
        <>
          <div className="w-32 sm:w-40">
            <Slider value={pct} min={0} max={100} step={1} aria-label={fan.name} onValueChange={(v) => setDraft(v / 100)} onValueCommit={commit} />
          </div>
          <span className="text-fg w-9 text-right text-[12px] font-semibold tabular-nums">{pct}%</span>
        </>
      ) : (
        <span className="text-fg-muted text-[12px] tabular-nums">
          {pct > 0 ? `${pct}%` : t("moonraker.pin.off")}
          {fan.rpm != null ? ` · ${Math.round(fan.rpm)} rpm` : ""}
        </span>
      )}
    </Row>
  );
}

function LedRow({ led, instanceId, canControl }: { led: MoonrakerLed; instanceId: string; canControl?: boolean }) {
  const { t } = useTranslation();
  const gcode = useMoonrakerGcode(instanceId);
  const toast = useToast();
  const hex = rgbToHex(led.color);
  const on = led.color.some((c) => c > 0.01);
  const setColor = (rgbHex: string) => {
    const [r, g, b] = hexToRgb(rgbHex);
    const w = led.color[3] ?? 0;
    gcode.mutate(`SET_LED LED=${configName(led.object)} RED=${r.toFixed(2)} GREEN=${g.toFixed(2)} BLUE=${b.toFixed(2)} WHITE=${w.toFixed(2)} SYNC=0 TRANSMIT=1`, {
      onError: (e) => toast("error", t("moonraker.toast.failed"), String(e)),
    });
  };
  const off = () =>
    gcode.mutate(`SET_LED LED=${configName(led.object)} RED=0 GREEN=0 BLUE=0 WHITE=0 SYNC=0 TRANSMIT=1`, {
      onError: (e) => toast("error", t("moonraker.toast.failed"), String(e)),
    });
  return (
    <Row label={led.name} icon={<Lightbulb className={cn("size-3.5 shrink-0", on ? "text-accent" : "text-fg-subtle")} />}>
      {canControl ? (
        <>
          <input
            type="color"
            value={hex}
            onChange={(e) => setColor(e.target.value)}
            aria-label={t("moonraker.led.pick", { name: led.name })}
            className="border-edge size-7 cursor-pointer rounded-md border bg-transparent p-0.5"
          />
          <Button variant="ghost" className="h-7 px-2" onClick={off} title={t("moonraker.temp.off")}>
            <Power className="size-3.5" />
          </Button>
        </>
      ) : (
        <span className="border-edge size-5 rounded-full border" style={{ background: on ? hex : "var(--color-bg-subtle)" }} />
      )}
    </Row>
  );
}

function OutputPinRow({ pin, instanceId, canControl }: { pin: MoonrakerOutputPin; instanceId: string; canControl?: boolean }) {
  const { t } = useTranslation();
  const gcode = useMoonrakerGcode(instanceId);
  const toast = useToast();
  const on = pin.value > 0.01;
  const toggle = () =>
    gcode.mutate(`SET_PIN PIN=${configName(pin.object)} VALUE=${on ? 0 : 1}`, { onError: (e) => toast("error", t("moonraker.toast.failed"), String(e)) });
  return (
    <Row label={pin.name} icon={<Power className={cn("size-3.5 shrink-0", on ? "text-accent" : "text-fg-subtle")} />}>
      <span className="text-fg-muted text-[12px] tabular-nums">{pin.pwm ? `${Math.round(pin.value * 100)}%` : on ? t("moonraker.pin.on") : t("moonraker.pin.off")}</span>
      {canControl && (
        <Button variant="secondary" className="h-7" onClick={toggle}>
          {on ? t("moonraker.pin.turnOff") : t("moonraker.pin.turnOn")}
        </Button>
      )}
    </Row>
  );
}

// ---- macros -----------------------------------------------------------------

function MacroCard({ snap, instanceId, canControl }: { snap: MoonrakerSnapshot; instanceId: string; canControl?: boolean }) {
  const { t } = useTranslation();
  const gcode = useMoonrakerGcode(instanceId);
  const toast = useToast();
  const [q, setQ] = useState("");
  const filtered = useMemo(() => {
    const needle = q.trim().toLowerCase();
    if (!needle) return snap.macros;
    return snap.macros.filter((m) => m.name.toLowerCase().includes(needle) || (m.description ?? "").toLowerCase().includes(needle));
  }, [snap.macros, q]);
  const run = (name: string) =>
    gcode.mutate(name, {
      onSuccess: () => toast("success", t("moonraker.toast.macroRan", { name })),
      onError: (e) => toast("error", t("moonraker.toast.failed"), String(e)),
    });

  return (
    <FCard
      icon={Terminal}
      title={
        <span className="flex items-center gap-1.5">
          {t("moonraker.sections.macros")}
          <span className="text-fg-subtle text-[12px] font-normal">{snap.macros.length}</span>
        </span>
      }
      actions={
        snap.macros.length > 8 ? (
          <div className="relative">
            <Search className="text-fg-subtle absolute top-1/2 left-2 size-3.5 -translate-y-1/2" />
            <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder={t("moonraker.macros.search")} className="h-7 w-36 pl-7 text-[12px]" />
          </div>
        ) : undefined
      }
    >
      {filtered.length === 0 ? (
        <p className="text-fg-subtle text-[13px]">{t("moonraker.macros.noMatch")}</p>
      ) : (
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
          {filtered.map((m) => {
            const btn = (
              <button
                key={m.object}
                disabled={!canControl || gcode.isPending}
                onClick={() => run(m.name)}
                className="border-edge hover:border-accent hover:bg-accent-soft/30 flex items-center gap-2 rounded-lg border px-3 py-2 text-left transition-colors disabled:cursor-not-allowed disabled:opacity-60"
              >
                <Play className="text-fg-subtle size-3.5 shrink-0" />
                <span className="text-fg truncate font-mono text-[12px]" title={m.name}>{m.name}</span>
              </button>
            );
            return m.description ? (
              <Tooltip key={m.object} label={m.description}>{btn}</Tooltip>
            ) : (
              btn
            );
          })}
        </div>
      )}
    </FCard>
  );
}

// ---- console ----------------------------------------------------------------

function ConsoleCard({ instanceId, canControl }: { instanceId: string; canControl?: boolean }) {
  const { t } = useTranslation();
  const gcode = useMoonrakerGcode(instanceId);
  const toast = useToast();
  const [lines, setLines] = useState<string[]>([]);
  const [cmd, setCmd] = useState("");
  const preRef = useRef<HTMLPreElement>(null);

  useEffect(() => {
    return live.subscribe(mrTopic(instanceId), (raw: unknown) => {
      const { event, data } = (raw ?? {}) as { event?: string; data?: { line?: string } };
      if (event === "gcode" && data?.line != null) {
        setLines((prev) => [...prev.slice(-400), data.line as string]);
      }
    });
  }, [instanceId]);
  useEffect(() => {
    if (preRef.current) preRef.current.scrollTop = preRef.current.scrollHeight;
  }, [lines]);

  const send = () => {
    const script = cmd.trim();
    if (!script) return;
    setLines((prev) => [...prev.slice(-400), "> " + script]);
    setCmd("");
    gcode.mutate(script, { onError: (e) => toast("error", t("moonraker.toast.failed"), String(e)) });
  };

  return (
    <FCard icon={Terminal} title={t("moonraker.sections.console")}>
      <pre ref={preRef} className="bg-surface text-fg-muted h-64 overflow-auto rounded-lg p-3 font-mono text-[12px] leading-relaxed whitespace-pre-wrap">
        {lines.length ? lines.join("\n") : t("moonraker.console.empty")}
      </pre>
      {canControl && (
        <div className="mt-2 flex items-center gap-2">
          <Input value={cmd} onChange={(e) => setCmd(e.target.value)} onKeyDown={(e) => e.key === "Enter" && send()} placeholder={t("moonraker.console.placeholder")} className="font-mono text-[13px]" />
          <Button variant="primary" className="shrink-0" onClick={send} disabled={!cmd.trim()}>
            {t("moonraker.console.send")}
          </Button>
        </div>
      )}
    </FCard>
  );
}

// ---- cameras ----------------------------------------------------------------

function CameraSection({ cameras, instanceId, canControl }: { cameras: MoonrakerCamera[]; instanceId: string; canControl?: boolean }) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState<MoonrakerCamera | null>(null);
  return (
    <FCard icon={Video} title={t("moonraker.sections.cameras")} bodyClass="p-2.5">
      <div className="flex flex-wrap gap-2.5">
        {cameras.map((cam) => (
          <CameraTile key={cam.name} cam={cam} instanceId={instanceId} canControl={canControl} onExpand={() => setExpanded(cam)} />
        ))}
      </div>
      {expanded && (
        <Dialog open onOpenChange={(o) => !o && setExpanded(null)}>
          <DialogContent wide className="max-w-[min(94vw,1400px)]" title={expanded.name}>
            <div className="w-full">
              <CameraTile cam={expanded} instanceId={instanceId} canControl={canControl} expanded />
            </div>
          </DialogContent>
        </Dialog>
      )}
    </FCard>
  );
}

function aspectStyle(ratio: string): React.CSSProperties {
  const m = ratio.match(/^(\d+)\s*:\s*(\d+)$/);
  if (m && Number(m[1]) > 0 && Number(m[2]) > 0) return { aspectRatio: `${m[1]} / ${m[2]}` };
  return { aspectRatio: "16 / 9" };
}

function CameraTile({ cam, instanceId, canControl, onExpand, expanded }: { cam: MoonrakerCamera; instanceId: string; canControl?: boolean; onExpand?: () => void; expanded?: boolean }) {
  const { t } = useTranslation();
  const setWebcam = useMoonrakerSetWebcam(instanceId);
  const toast = useToast();
  const [flipH, setFlipH] = useState(cam.flipHorizontal);
  const [flipV, setFlipV] = useState(cam.flipVertical);
  const [rotation, setRotation] = useState(cam.rotation);
  const [nonce, setNonce] = useState(0);
  const [errored, setErrored] = useState(false);
  useEffect(() => {
    setFlipH(cam.flipHorizontal);
    setFlipV(cam.flipVertical);
    setRotation(cam.rotation);
  }, [cam.flipHorizontal, cam.flipVertical, cam.rotation]);

  const save = (patch: { flipHorizontal?: boolean; flipVertical?: boolean; rotation?: 0 | 90 | 180 | 270 }, revert: () => void) =>
    setWebcam.mutate({ name: cam.name, ...patch }, { onError: (e) => { revert(); toast("error", t("moonraker.toast.failed"), String(e)); } });
  const toggleH = () => { const prev = flipH; const v = !flipH; setFlipH(v); save({ flipHorizontal: v }, () => setFlipH(prev)); };
  const toggleV = () => { const prev = flipV; const v = !flipV; setFlipV(v); save({ flipVertical: v }, () => setFlipV(prev)); };
  const rotate = () => { const prev = rotation; const v = (((rotation + 90) % 360) as 0 | 90 | 180 | 270); setRotation(v); save({ rotation: v }, () => setRotation(prev)); };

  const transform = `rotate(${rotation}deg) scaleX(${flipH ? -1 : 1}) scaleY(${flipV ? -1 : 1})`;
  const streamable = !/webrtc|hls|rtsp/i.test(cam.service ?? "");
  const [tick, setTick] = useState(0);
  useEffect(() => {
    if (streamable || !cam.snapshot) return;
    const id = window.setInterval(() => setTick((x) => x + 1), 1000);
    return () => window.clearInterval(id);
  }, [streamable, cam.snapshot]);
  const src = streamable ? `${webcamStreamUrl(instanceId, cam.name)}&_=${nonce}` : cam.snapshot ? `${webcamSnapshotUrl(instanceId, cam.name)}&_=${tick}` : null;

  return (
    <div className={cn("border-edge group relative overflow-hidden rounded-lg border bg-black", expanded ? "mx-auto w-full" : "w-full")} style={{ ...aspectStyle(cam.aspectRatio), maxHeight: expanded ? "78vh" : undefined }}>
      {src == null ? (
        <div className="absolute inset-0 flex flex-col items-center justify-center gap-2 px-4 text-center">
          <VideoOff className="size-6 text-white/60" />
          <span className="text-[12.5px] text-white/70">{t("moonraker.camera.unsupported")}</span>
          <span className="text-[11px] text-white/45">{cam.service}</span>
        </div>
      ) : streamable && errored ? (
        <button type="button" onClick={() => { setErrored(false); setNonce((n) => n + 1); }} className="absolute inset-0 flex flex-col items-center justify-center gap-2 text-center">
          <VideoOff className="size-6 text-white/60" />
          <span className="text-[12.5px] text-white/70">{t("moonraker.camera.offline")}</span>
          <span className="text-[11px] text-white/45">{t("moonraker.camera.retry")}</span>
        </button>
      ) : (
        <img src={src} alt={cam.name} onError={() => streamable && setErrored(true)} className="absolute inset-0 size-full object-contain" style={{ transform }} />
      )}

      <div className="absolute top-2 right-2 flex items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100">
        {canControl && (
          <>
            <CamBtn active={flipH} label={t("moonraker.camera.flipH")} onClick={toggleH}><FlipHorizontal2 className="size-3.5" /></CamBtn>
            <CamBtn active={flipV} label={t("moonraker.camera.flipV")} onClick={toggleV}><FlipVertical2 className="size-3.5" /></CamBtn>
            <CamBtn active={rotation !== 0} label={t("moonraker.camera.rotate")} onClick={rotate}><RotateCw className="size-3.5" /></CamBtn>
          </>
        )}
        {onExpand && <CamBtn label={t("moonraker.camera.expand")} onClick={onExpand}><Maximize2 className="size-3.5" /></CamBtn>}
      </div>

      <div className="pointer-events-none absolute inset-x-0 bottom-0 flex items-center gap-2 bg-gradient-to-t from-black/70 to-transparent px-3 pt-6 pb-2">
        <span className="truncate text-[12.5px] font-medium text-white">{cam.name}</span>
        {(flipH || flipV || rotation !== 0) && (
          <span className="text-[10.5px] text-white/55">{[flipH && "H", flipV && "V", rotation !== 0 && `${rotation}°`].filter(Boolean).join(" · ")}</span>
        )}
      </div>
    </div>
  );
}

function CamBtn({ active, label, onClick, children }: { active?: boolean; label: string; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      aria-pressed={active}
      onClick={onClick}
      className={cn("flex size-7 items-center justify-center rounded-md border backdrop-blur transition-colors", active ? "border-accent bg-accent/80 text-white" : "border-white/20 bg-black/50 text-white/85 hover:bg-black/70")}
    >
      {children}
    </button>
  );
}

// ---- helpers ----------------------------------------------------------------

function clamp01(n: number): number {
  return Math.max(0, Math.min(1, n));
}

function rateOf(points: Pt[]): number {
  if (points.length < 2) return 0;
  const a = points[points.length - 2];
  const b = points[points.length - 1];
  const dt = (b.ts - a.ts) / 1000;
  return dt > 0 ? (b.value - a.value) / dt : 0;
}

function fmtDuration(secs: number): string {
  secs = Math.max(0, Math.round(secs));
  const h = Math.floor(secs / 3600);
  const m = Math.floor((secs % 3600) / 60);
  const s = secs % 60;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m ${s}s`;
  return `${s}s`;
}

function shortVersion(v: string): string {
  const m = v.match(/^v?\d+\.\d+\.\d+/);
  return m ? m[0] : v;
}

function klippyTone(state: string): "success" | "warning" | "danger" | "info" | "neutral" {
  switch (state) {
    case "ready":
      return "success";
    case "startup":
      return "info";
    case "shutdown":
    case "error":
      return "danger";
    default:
      return "neutral";
  }
}

function printStateTone(state: string): "success" | "warning" | "danger" | "info" | "neutral" {
  switch (state) {
    case "printing":
      return "info";
    case "paused":
      return "warning";
    case "complete":
      return "success";
    case "cancelled":
    case "error":
      return "danger";
    default:
      return "neutral";
  }
}

/** Klipper SET_* commands address a device by its config name — the object name
 * minus its type prefix ("neopixel status" → "status", "fan" → "fan"). */
function configName(object: string): string {
  const i = object.indexOf(" ");
  return i >= 0 ? object.slice(i + 1) : object;
}

function rgbToHex(color: number[]): string {
  const to = (n: number) => Math.round(clamp01(n ?? 0) * 255).toString(16).padStart(2, "0");
  return `#${to(color[0])}${to(color[1])}${to(color[2])}`;
}

function hexToRgb(hex: string): [number, number, number] {
  const m = hex.replace("#", "");
  const r = parseInt(m.slice(0, 2), 16) / 255;
  const g = parseInt(m.slice(2, 4), 16) / 255;
  const b = parseInt(m.slice(4, 6), 16) / 255;
  return [r || 0, g || 0, b || 0];
}
