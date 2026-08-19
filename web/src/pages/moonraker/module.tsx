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
  Gauge,
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
  ToggleLeft,
  Video,
  VideoOff,
} from "lucide-react";
import {
  type MoonrakerCamera,
  type MoonrakerFan,
  type MoonrakerHeater,
  type MoonrakerLed,
  type MoonrakerOutputPin,
  type MoonrakerSensor,
  type MoonrakerSnapshot,
  type MoonrakerTempFan,
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
import { Popover, PopoverContent, PopoverTrigger } from "@/ui/popover";
import { Slider } from "@/ui/slider";
import { Sparkline } from "@/ui/sparkline";
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

/**
 * MoonrakerModulePage is the printer dashboard. It renders whatever the
 * snapshot reports — the object set is discovered per-printer — so a machine
 * with two hotends, a chamber, three fans, a Neopixel and forty macros and a
 * bare single-extruder rig both get a correct, complete board with no
 * per-model configuration.
 */
export function MoonrakerModulePage() {
  const { t } = useTranslation();
  const { can } = useAuth();
  const { instanceId } = useParams({ from: "/shell/moonraker/$instanceId" });
  const integrations = useIntegrations();
  const snapshot = useMoonrakerSnapshot(instanceId);

  // Rolling temperature history for the card sparklines, kept in a ref so it
  // survives re-renders and only the newest ~180 points are held.
  const history = useRef<Map<string, Pt[]>>(new Map());
  const snap = snapshot.data;
  useEffect(() => {
    if (!snap) return;
    const now = Date.now();
    const push = (key: string, value: number) => {
      const arr = history.current.get(key) ?? [];
      if (arr.length === 0 || arr[arr.length - 1].value !== value || now - arr[arr.length - 1].ts > 4000) {
        arr.push({ ts: now, value });
      }
      while (arr.length > 180) arr.shift();
      history.current.set(key, arr);
    };
    snap.heaters.forEach((h) => push("h:" + h.object, h.temp));
    snap.sensors.forEach((s) => push("s:" + s.object, s.temp));
    snap.tempFans.forEach((f) => push("f:" + f.object, f.temp));
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
      <div className="space-y-5">
        <Header name={instance.name} status={instance.status} snap={undefined} instanceId={instanceId} />
        <Card>
          <ErrorState error={snapshot.error} onRetry={() => void snapshot.refetch()} />
        </Card>
      </div>
    );
  }

  const klippyBad = snap.klippy.state === "shutdown" || snap.klippy.state === "error";
  const hasControl = can("moonraker/" + instanceId, "control");

  return (
    <div className="space-y-5">
      <Header
        name={instance.name}
        status={instance.status}
        snap={snap}
        instanceId={instanceId}
        canControl={hasControl}
        baseUrl={typeof instance.config?.baseUrl === "string" ? instance.config.baseUrl : null}
      />

      {klippyBad && <KlippyBanner snap={snap} instanceId={instanceId} canControl={hasControl} />}

      {snap.print && snap.print.state !== "standby" && (
        <PrintCard snap={snap} instanceId={instanceId} canControl={hasControl} />
      )}

      {snap.cameras.length > 0 && (
        <CameraSection cameras={snap.cameras} instanceId={instanceId} canControl={hasControl} />
      )}

      {/* Temperatures — the hero. Heaters (controllable), then sensors, then
          temperature fans, all discovered dynamically. */}
      {(snap.heaters.length > 0 || snap.sensors.length > 0 || snap.tempFans.length > 0) && (
        <Section icon={Thermometer} title={t("moonraker.sections.temperatures")}>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
            {snap.heaters.map((h) => (
              <HeaterCard key={h.object} heater={h} instanceId={instanceId} canControl={hasControl} points={history.current.get("h:" + h.object) ?? []} />
            ))}
            {snap.tempFans.map((f) => (
              <TempFanCard key={f.object} fan={f} points={history.current.get("f:" + f.object) ?? []} />
            ))}
            {snap.sensors.map((s) => (
              <SensorCard key={s.object} sensor={s} points={history.current.get("s:" + s.object) ?? []} />
            ))}
          </div>
        </Section>
      )}

      {snap.toolhead && (
        <Section icon={Move3d} title={t("moonraker.sections.control")}>
          <div className="grid gap-3 lg:grid-cols-2">
            <MovementCard snap={snap} instanceId={instanceId} canControl={hasControl} />
            <ExtruderCard snap={snap} instanceId={instanceId} canControl={hasControl} />
          </div>
        </Section>
      )}

      {snap.fans.length > 0 && (
        <Section icon={FanIcon} title={t("moonraker.sections.fans")}>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
            {snap.fans.map((f) => (
              <FanCard key={f.object} fan={f} instanceId={instanceId} canControl={hasControl} />
            ))}
          </div>
        </Section>
      )}

      {(snap.leds.length > 0 || snap.outputPins.length > 0) && (
        <Section icon={Lightbulb} title={t("moonraker.sections.lights")}>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
            {snap.leds.map((l) => (
              <LedCard key={l.object} led={l} instanceId={instanceId} canControl={hasControl} />
            ))}
            {snap.outputPins.map((p) => (
              <OutputPinCard key={p.object} pin={p} instanceId={instanceId} canControl={hasControl} />
            ))}
          </div>
        </Section>
      )}

      {snap.macros.length > 0 && <MacroBoard snap={snap} instanceId={instanceId} canControl={hasControl} />}

      <Console instanceId={instanceId} canControl={hasControl} />

      {snap.extra.length > 0 && (
        <div className="text-fg-subtle flex flex-wrap items-center gap-1.5 text-[11.5px]">
          <span className="font-medium">{t("moonraker.otherObjects")}:</span>
          {snap.extra.map((o) => (
            <span key={o} className="border-edge rounded bg-bg-subtle px-1.5 py-0.5 font-mono">
              {o}
            </span>
          ))}
        </div>
      )}
    </div>
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
          <p className="text-fg text-sm font-semibold">
            {t(`moonraker.klippy.${snap.klippy.state}`, snap.klippy.state)}
          </p>
          {snap.klippy.message && (
            <pre className="text-fg-muted mt-1 max-h-40 overflow-auto text-[12.5px] whitespace-pre-wrap">
              {snap.klippy.message}
            </pre>
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

// ---- print job --------------------------------------------------------------

function PrintCard({ snap, instanceId, canControl }: { snap: MoonrakerSnapshot; instanceId: string; canControl?: boolean }) {
  const { t } = useTranslation();
  const print = snap.print!;
  const printMut = useMoonrakerPrint(instanceId);
  const toast = useToast();
  const [confirmCancel, setConfirmCancel] = useState(false);
  const pct = Math.round(clamp01(print.progress) * 100);
  const printing = print.state === "printing";
  const paused = print.state === "paused";
  const eta =
    printing && print.progress > 0.01 && print.printDuration > 0
      ? (print.printDuration * (1 - print.progress)) / print.progress
      : null;

  const act = (action: "pause" | "resume" | "cancel") =>
    printMut.mutate(action, {
      onError: (e) => toast("error", t("moonraker.toast.failed"), String(e)),
    });

  return (
    <Card>
      <CardBody className="space-y-3">
        <div className="flex flex-wrap items-center gap-3">
          <ProgressRing pct={pct} state={print.state} />
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2">
              <Badge tone={printStateTone(print.state)}>{t(`moonraker.printState.${print.state}`, print.state)}</Badge>
              {print.totalLayer > 0 && (
                <span className="text-fg-muted text-[12.5px]">
                  {t("moonraker.print.layer", { current: print.currentLayer, total: print.totalLayer })}
                </span>
              )}
            </div>
            <p className="text-fg mt-1 truncate text-[15px] font-medium" title={print.filename}>
              {print.filename || t("moonraker.print.untitled")}
            </p>
            <div className="text-fg-muted mt-1 flex flex-wrap gap-x-4 gap-y-0.5 text-[12.5px]">
              <span>{t("moonraker.print.elapsed")}: {fmtDuration(print.printDuration)}</span>
              {eta !== null && <span>{t("moonraker.print.remaining")}: {fmtDuration(eta)}</span>}
              {print.filamentUsed > 0 && (
                <span>{t("moonraker.print.filament")}: {(print.filamentUsed / 1000).toFixed(2)} m</span>
              )}
            </div>
          </div>
          {canControl && (printing || paused) && (
            <div className="flex items-center gap-2">
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
        {(printing || paused) && (
          <div className="bg-bg-subtle h-1.5 w-full overflow-hidden rounded-full">
            <div className="bg-accent h-full rounded-full transition-all" style={{ width: `${pct}%` }} />
          </div>
        )}
      </CardBody>
      <ConfirmDialog
        open={confirmCancel}
        onOpenChange={setConfirmCancel}
        tone="danger"
        title={t("moonraker.print.cancelConfirm.title")}
        body={t("moonraker.print.cancelConfirm.body")}
        confirmLabel={t("moonraker.print.cancel")}
        onConfirm={() => act("cancel")}
      />
    </Card>
  );
}

function ProgressRing({ pct, state }: { pct: number; state: string }) {
  const r = 26;
  const c = 2 * Math.PI * r;
  const off = c * (1 - pct / 100);
  const stroke = state === "complete" ? "var(--color-success)" : state === "paused" ? "var(--color-warning)" : "var(--color-accent)";
  return (
    <div className="relative size-16 shrink-0">
      <svg viewBox="0 0 64 64" className="size-16 -rotate-90">
        <circle cx="32" cy="32" r={r} fill="none" stroke="var(--color-edge)" strokeWidth="6" />
        <circle
          cx="32"
          cy="32"
          r={r}
          fill="none"
          stroke={stroke}
          strokeWidth="6"
          strokeLinecap="round"
          strokeDasharray={c}
          strokeDashoffset={off}
          className="transition-all"
        />
      </svg>
      <span className="text-fg absolute inset-0 flex items-center justify-center text-[13px] font-semibold tabular-nums">
        {pct}%
      </span>
    </div>
  );
}

// ---- temperature cards ------------------------------------------------------

function TempShell({
  name,
  temp,
  sub,
  tone,
  points,
  right,
}: {
  name: string;
  temp: number;
  sub?: React.ReactNode;
  tone: "hot" | "warm" | "cool" | "neutral";
  points: Pt[];
  right?: React.ReactNode;
}) {
  const dot = tone === "hot" ? "danger" : tone === "warm" ? "warning" : tone === "cool" ? "info" : "neutral";
  return (
    <Card>
      <CardBody className="space-y-1.5 p-3.5">
        <div className="flex items-start justify-between gap-2">
          <span className="text-fg-muted flex min-w-0 items-center gap-1.5 text-[12.5px] font-medium">
            <StatusDot tone={dot} />
            <span className="truncate" title={name}>{name}</span>
          </span>
          {right}
        </div>
        <div className="flex items-end justify-between gap-2">
          <span className="text-fg text-[22px] leading-none font-semibold tabular-nums">
            {temp.toFixed(1)}<span className="text-fg-subtle text-sm">°C</span>
          </span>
          {sub}
        </div>
        {points.length > 1 && <Sparkline points={points} height={30} />}
      </CardBody>
    </Card>
  );
}

function HeaterCard({
  heater,
  instanceId,
  canControl,
  points,
}: {
  heater: MoonrakerHeater;
  instanceId: string;
  canControl?: boolean;
  points: Pt[];
}) {
  const { t } = useTranslation();
  const setTemp = useMoonrakerSetTemp(instanceId);
  const toast = useToast();
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState("");
  const heating = heater.target > 0;
  const tone: "hot" | "warm" | "neutral" = heating ? (heater.temp < heater.target - 1.5 ? "warm" : "hot") : "neutral";

  const apply = (target: number) => {
    setTemp.mutate(
      { heater: heater.object, target },
      {
        onError: (e) => toast("error", t("moonraker.toast.failed"), String(e)),
      },
    );
    setOpen(false);
  };

  const target = (
    <span className={cn("text-[12.5px] tabular-nums", heating ? "text-warning" : "text-fg-subtle")}>
      → {heater.target > 0 ? `${heater.target.toFixed(0)}°` : t("moonraker.temp.off")}
    </span>
  );

  return (
    <TempShell
      name={heater.name}
      temp={heater.temp}
      tone={tone}
      points={points}
      sub={
        canControl ? (
          <Popover open={open} onOpenChange={setOpen}>
            <PopoverTrigger asChild>
              <button className="hover:bg-surface-hover -mr-1 rounded px-1 py-0.5 transition-colors">{target}</button>
            </PopoverTrigger>
            <PopoverContent align="end" className="w-56">
              <p className="text-fg mb-2 text-[13px] font-semibold">{t("moonraker.temp.setTarget", { name: heater.name })}</p>
              <div className="mb-2 flex flex-wrap gap-1.5">
                {presetsFor(heater.kind).map((p) => (
                  <button
                    key={p}
                    onClick={() => apply(p)}
                    className="border-edge hover:border-accent hover:bg-accent-soft/40 rounded-md border px-2 py-1 text-[12px] font-medium tabular-nums transition-colors"
                  >
                    {p === 0 ? t("moonraker.temp.off") : `${p}°`}
                  </button>
                ))}
              </div>
              <div className="flex items-center gap-2">
                <Input
                  type="number"
                  autoFocus
                  placeholder={heater.target.toFixed(0)}
                  value={draft}
                  onChange={(e) => setDraft(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && draft.trim() !== "") apply(Number(draft));
                  }}
                  className="h-8"
                />
                <Button
                  variant="primary"
                  className="h-8 shrink-0"
                  disabled={draft.trim() === ""}
                  onClick={() => apply(Number(draft))}
                >
                  {t("common.set")}
                </Button>
              </div>
            </PopoverContent>
          </Popover>
        ) : (
          target
        )
      }
      right={
        heater.power > 0.01 ? (
          <Tooltip label={t("moonraker.temp.power", { pct: Math.round(heater.power * 100) })}>
            <span className="text-fg-subtle inline-flex items-center gap-1 text-[11px] tabular-nums">
              <Power className="size-3" />
              {Math.round(heater.power * 100)}%
            </span>
          </Tooltip>
        ) : undefined
      }
    />
  );
}

function SensorCard({ sensor, points }: { sensor: MoonrakerSensor; points: Pt[] }) {
  return (
    <TempShell
      name={sensor.name}
      temp={sensor.temp}
      tone="cool"
      points={points}
      right={
        sensor.maxTemp != null ? (
          <span className="text-fg-subtle text-[11px] tabular-nums">max {sensor.maxTemp.toFixed(0)}°</span>
        ) : undefined
      }
    />
  );
}

function TempFanCard({ fan, points }: { fan: MoonrakerTempFan; points: Pt[] }) {
  const { t } = useTranslation();
  return (
    <TempShell
      name={fan.name}
      temp={fan.temp}
      tone={fan.temp >= fan.target ? "warm" : "cool"}
      points={points}
      sub={
        <span className="text-fg-subtle text-[12.5px] tabular-nums">
          → {fan.target.toFixed(0)}° · {Math.round(fan.speed * 100)}%
        </span>
      }
      right={<span className="text-fg-subtle text-[11px]">{t("moonraker.tempFan")}</span>}
    />
  );
}

// ---- motion -----------------------------------------------------------------

const JOG_STEPS = [0.1, 1, 10, 50] as const;

function MovementCard({ snap, instanceId, canControl }: { snap: MoonrakerSnapshot; instanceId: string; canControl?: boolean }) {
  const { t } = useTranslation();
  const th = snap.toolhead!;
  const gcode = useMoonrakerGcode(instanceId);
  const toast = useToast();
  const [step, setStep] = useState(10);
  const printing = snap.print?.state === "printing";
  const run = (script: string) =>
    gcode.mutate(script, { onError: (e) => toast("error", t("moonraker.toast.failed"), String(e)) });
  // A relative jog, bracketed so absolute mode is restored afterwards.
  const jog = (axis: "X" | "Y" | "Z", sign: 1 | -1) => {
    const feed = axis === "Z" ? 900 : 6000;
    run(`G91\nG1 ${axis}${sign * step} F${feed}\nG90`);
  };
  const homed = (axis: string) => th.homedAxes.toLowerCase().includes(axis);
  const axes = ["x", "y", "z"] as const;
  const canJog = canControl && !printing;

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
    <Card>
      <CardBody className="space-y-3 p-3.5">
        <div className="grid grid-cols-3 gap-2">
          {axes.map((axis, i) => (
            <div key={axis} className="border-edge rounded-lg border p-2 text-center">
              <div className="text-fg-subtle text-[11px] font-semibold uppercase">{axis}</div>
              <div className="text-fg text-[16px] font-semibold tabular-nums">
                {th.position[i] != null ? th.position[i].toFixed(1) : "—"}
              </div>
              <div className={cn("text-[10.5px]", homed(axis) ? "text-success" : "text-fg-subtle")}>
                {homed(axis) ? t("moonraker.motion.homed") : t("moonraker.motion.notHomed")}
              </div>
            </div>
          ))}
        </div>

        {canControl && (
          <>
            {/* Jog pad: XY cross + Z column, gated by a step size. */}
            <div className="flex items-start justify-center gap-5">
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

            {/* Step size */}
            <div className="flex items-center justify-center gap-1.5">
              {JOG_STEPS.map((s) => (
                <button
                  key={s}
                  type="button"
                  onClick={() => setStep(s)}
                  className={cn(
                    "rounded-md border px-2.5 py-1 text-[12px] font-medium tabular-nums transition-colors",
                    step === s ? "border-accent bg-accent-soft text-accent-soft-fg" : "border-edge text-fg-muted hover:border-accent",
                  )}
                >
                  {s}
                </button>
              ))}
              <span className="text-fg-subtle ml-0.5 text-[11px]">mm</span>
            </div>
            {printing && <p className="text-fg-subtle text-center text-[11px]">{t("moonraker.jog.lockedPrinting")}</p>}

            {/* Speed & flow factors — safe to nudge mid-print. */}
            <FactorSlider label={t("moonraker.motion.speed")} value={Math.round((th.speedFactor ?? 1) * 100)} onCommit={(v) => run(`M220 S${v}`)} disabled={!canControl} />
            <FactorSlider label={t("moonraker.motion.flow")} value={Math.round((th.extrudeFactor ?? 1) * 100)} onCommit={(v) => run(`M221 S${v}`)} disabled={!canControl} />

            <div className="flex flex-wrap gap-2">
              <Button variant="ghost" className="h-7 text-[12px]" disabled={!canJog} onClick={() => run("G28 X Y")}>
                {t("moonraker.motion.homeXY")}
              </Button>
              <Button variant="ghost" className="h-7 text-[12px]" disabled={!canJog} onClick={() => run("G28 Z")}>
                {t("moonraker.motion.homeZ")}
              </Button>
              <Button variant="ghost" className="h-7 text-[12px]" disabled={!canControl || printing} onClick={() => run("M84")}>
                <Power className="size-3.5" />
                {t("moonraker.motion.motorsOff")}
              </Button>
            </div>
          </>
        )}
      </CardBody>
    </Card>
  );
}

/** A 50–200% factor slider (speed M220 / flow M221) with a live readout. */
function FactorSlider({ label, value, onCommit, disabled }: { label: string; value: number; onCommit: (v: number) => void; disabled?: boolean }) {
  const [draft, setDraft] = useState<number | null>(null);
  const shown = draft ?? value;
  return (
    <div>
      <div className="mb-1 flex items-center justify-between">
        <span className="text-fg-muted inline-flex items-center gap-1 text-[12px]">
          <Gauge className="size-3.5" /> {label}
        </span>
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

function ExtruderCard({ snap, instanceId, canControl }: { snap: MoonrakerSnapshot; instanceId: string; canControl?: boolean }) {
  const { t } = useTranslation();
  const gcode = useMoonrakerGcode(instanceId);
  const toast = useToast();
  const [amount, setAmount] = useState(10);
  const extruder = snap.heaters.find((h) => h.kind === "extruder");
  // Extruding is for idle filament loading — block it during a print, paused
  // included (a manual purge mid-job corrupts the slicer's E tracking).
  const busy = snap.print?.state === "printing" || snap.print?.state === "paused";
  const tooCold = !extruder || extruder.temp < 170;
  const canExtrude = canControl && !tooCold && !busy;
  // SAVE/RESTORE brackets the relative-E (M83) move so the interpreter's
  // extrusion mode is left exactly as it was — a latched M83 would otherwise
  // turn a later absolute-E print's moves into runaway over-extrusion.
  const run = (e: number) =>
    gcode.mutate(`SAVE_GCODE_STATE NAME=hh_extrude\nM83\nG1 E${e} F300\nRESTORE_GCODE_STATE NAME=hh_extrude`, {
      onError: (err) => toast("error", t("moonraker.toast.failed"), String(err)),
    });

  return (
    <Card>
      <CardBody className="space-y-3 p-3.5">
        <div className="flex items-center justify-between">
          <span className="text-fg text-[13px] font-semibold">{t("moonraker.extruder.title")}</span>
          {extruder && (
            <span className="text-fg-muted text-[12px] tabular-nums">
              {extruder.name}: {extruder.temp.toFixed(0)}°
              {extruder.target > 0 ? ` → ${extruder.target.toFixed(0)}°` : ""}
            </span>
          )}
        </div>

        <div className="flex items-center justify-center gap-1.5">
          {[1, 5, 10, 50].map((a) => (
            <button
              key={a}
              type="button"
              onClick={() => setAmount(a)}
              className={cn(
                "rounded-md border px-2.5 py-1 text-[12px] font-medium tabular-nums transition-colors",
                amount === a ? "border-accent bg-accent-soft text-accent-soft-fg" : "border-edge text-fg-muted hover:border-accent",
              )}
            >
              {a}
            </button>
          ))}
          <span className="text-fg-subtle ml-0.5 text-[11px]">mm</span>
        </div>

        <div className="grid grid-cols-2 gap-2">
          <Button variant="secondary" disabled={!canExtrude} onClick={() => run(-amount)}>
            <ArrowUpFromLine className="size-4" />
            {t("moonraker.extruder.retract")}
          </Button>
          <Button variant="secondary" disabled={!canExtrude} onClick={() => run(amount)}>
            <ArrowDownToLine className="size-4" />
            {t("moonraker.extruder.extrude")}
          </Button>
        </div>
        {canControl && tooCold && !busy && (
          <p className="text-warning text-center text-[11px]">{t("moonraker.extruder.cold")}</p>
        )}
        {busy && <p className="text-fg-subtle text-center text-[11px]">{t("moonraker.jog.lockedPrinting")}</p>}
      </CardBody>
    </Card>
  );
}

// ---- fans -------------------------------------------------------------------

function FanCard({ fan, instanceId, canControl }: { fan: MoonrakerFan; instanceId: string; canControl?: boolean }) {
  const { t } = useTranslation();
  const gcode = useMoonrakerGcode(instanceId);
  const toast = useToast();
  const [draft, setDraft] = useState<number | null>(null);
  const pct = Math.round((draft ?? fan.speed) * 100);

  const commit = (value: number) => {
    setDraft(null);
    const frac = value / 100;
    const script =
      fan.kind === "part"
        ? frac <= 0
          ? "M107"
          : `M106 S${Math.round(frac * 255)}`
        : `SET_FAN_SPEED FAN=${configName(fan.object)} SPEED=${frac.toFixed(2)}`;
    gcode.mutate(script, { onError: (e) => toast("error", t("moonraker.toast.failed"), String(e)) });
  };

  return (
    <Card>
      <CardBody className="space-y-2 p-3.5">
        <div className="flex items-center justify-between gap-2">
          <span className="text-fg flex min-w-0 items-center gap-1.5 text-[13px] font-medium">
            <FanIcon className={cn("size-3.5 shrink-0", fan.speed > 0 ? "text-info animate-spin [animation-duration:1.6s]" : "text-fg-subtle")} />
            <span className="truncate" title={fan.name}>{fan.name}</span>
          </span>
          <span className="text-fg text-[13px] font-semibold tabular-nums">
            {pct}%{fan.rpm != null ? <span className="text-fg-subtle"> · {Math.round(fan.rpm)} rpm</span> : null}
          </span>
        </div>
        {fan.controllable && canControl ? (
          <Slider
            value={pct}
            min={0}
            max={100}
            step={1}
            aria-label={fan.name}
            onValueChange={(v) => setDraft(v / 100)}
            onValueCommit={commit}
          />
        ) : (
          <div className="bg-bg-subtle h-1.5 w-full overflow-hidden rounded-full">
            <div className="bg-info h-full rounded-full" style={{ width: `${pct}%` }} />
          </div>
        )}
      </CardBody>
    </Card>
  );
}

// ---- lights -----------------------------------------------------------------

function LedCard({ led, instanceId, canControl }: { led: MoonrakerLed; instanceId: string; canControl?: boolean }) {
  const { t } = useTranslation();
  const gcode = useMoonrakerGcode(instanceId);
  const toast = useToast();
  const hex = rgbToHex(led.color);
  const on = led.color.some((c) => c > 0.01);

  const setColor = (rgbHex: string) => {
    const [r, g, b] = hexToRgb(rgbHex);
    const w = led.color[3] ?? 0;
    gcode.mutate(
      `SET_LED LED=${configName(led.object)} RED=${r.toFixed(2)} GREEN=${g.toFixed(2)} BLUE=${b.toFixed(2)} WHITE=${w.toFixed(2)} SYNC=0 TRANSMIT=1`,
      { onError: (e) => toast("error", t("moonraker.toast.failed"), String(e)) },
    );
  };
  const off = () =>
    gcode.mutate(`SET_LED LED=${configName(led.object)} RED=0 GREEN=0 BLUE=0 WHITE=0 SYNC=0 TRANSMIT=1`, {
      onError: (e) => toast("error", t("moonraker.toast.failed"), String(e)),
    });

  return (
    <Card>
      <CardBody className="flex items-center gap-3 p-3.5">
        <span
          className="border-edge size-9 shrink-0 rounded-full border shadow-1"
          style={{ background: on ? hex : "var(--color-bg-subtle)" }}
        />
        <div className="min-w-0 flex-1">
          <p className="text-fg truncate text-[13px] font-medium" title={led.name}>{led.name}</p>
          <p className="text-fg-subtle text-[11.5px]">
            {led.pixels > 1 ? t("moonraker.led.pixels", { n: led.pixels }) : t("moonraker.led.single")}
          </p>
        </div>
        {canControl && (
          <div className="flex shrink-0 items-center gap-1.5">
            <input
              type="color"
              value={hex}
              onChange={(e) => setColor(e.target.value)}
              aria-label={t("moonraker.led.pick", { name: led.name })}
              className="border-edge size-8 cursor-pointer rounded-md border bg-transparent p-0.5"
            />
            <Button variant="ghost" className="h-8 px-2" onClick={off} title={t("moonraker.temp.off")}>
              <Power className="size-3.5" />
            </Button>
          </div>
        )}
      </CardBody>
    </Card>
  );
}

function OutputPinCard({ pin, instanceId, canControl }: { pin: MoonrakerOutputPin; instanceId: string; canControl?: boolean }) {
  const { t } = useTranslation();
  const gcode = useMoonrakerGcode(instanceId);
  const toast = useToast();
  const on = pin.value > 0.01;
  const toggle = () =>
    gcode.mutate(`SET_PIN PIN=${configName(pin.object)} VALUE=${on ? 0 : 1}`, {
      onError: (e) => toast("error", t("moonraker.toast.failed"), String(e)),
    });
  return (
    <Card>
      <CardBody className="flex items-center gap-3 p-3.5">
        <ToggleLeft className={cn("size-5 shrink-0", on ? "text-accent" : "text-fg-subtle")} />
        <div className="min-w-0 flex-1">
          <p className="text-fg truncate text-[13px] font-medium" title={pin.name}>{pin.name}</p>
          <p className="text-fg-subtle text-[11.5px] tabular-nums">
            {pin.pwm ? `${Math.round(pin.value * 100)}%` : on ? t("moonraker.pin.on") : t("moonraker.pin.off")}
          </p>
        </div>
        {canControl && (
          <Button variant="secondary" className="h-8" onClick={toggle}>
            {on ? t("moonraker.pin.turnOff") : t("moonraker.pin.turnOn")}
          </Button>
        )}
      </CardBody>
    </Card>
  );
}

// ---- macros -----------------------------------------------------------------

function MacroBoard({ snap, instanceId, canControl }: { snap: MoonrakerSnapshot; instanceId: string; canControl?: boolean }) {
  const { t } = useTranslation();
  const gcode = useMoonrakerGcode(instanceId);
  const toast = useToast();
  const [q, setQ] = useState("");
  const filtered = useMemo(() => {
    const needle = q.trim().toLowerCase();
    if (!needle) return snap.macros;
    return snap.macros.filter(
      (m) => m.name.toLowerCase().includes(needle) || (m.description ?? "").toLowerCase().includes(needle),
    );
  }, [snap.macros, q]);

  const run = (name: string) =>
    gcode.mutate(name, {
      onSuccess: () => toast("success", t("moonraker.toast.macroRan", { name })),
      onError: (e) => toast("error", t("moonraker.toast.failed"), String(e)),
    });

  return (
    <Section
      icon={Terminal}
      title={t("moonraker.sections.macros")}
      count={snap.macros.length}
      right={
        snap.macros.length > 8 ? (
          <div className="relative">
            <Search className="text-fg-subtle absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2" />
            <Input
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder={t("moonraker.macros.search")}
              className="h-8 w-44 pl-8"
            />
          </div>
        ) : undefined
      }
    >
      {filtered.length === 0 ? (
        <p className="text-fg-subtle text-[13px]">{t("moonraker.macros.noMatch")}</p>
      ) : (
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
          {filtered.map((m) => {
            const btn = (
              <button
                key={m.object}
                disabled={!canControl || gcode.isPending}
                onClick={() => run(m.name)}
                className="border-edge hover:border-accent hover:bg-accent-soft/30 flex items-center gap-2 rounded-lg border px-3 py-2 text-left transition-colors disabled:cursor-not-allowed disabled:opacity-60"
              >
                <Play className="text-fg-subtle size-3.5 shrink-0" />
                <span className="text-fg truncate font-mono text-[12.5px]" title={m.name}>{m.name}</span>
              </button>
            );
            return m.description ? (
              <Tooltip key={m.object} label={m.description}>
                {btn}
              </Tooltip>
            ) : (
              btn
            );
          })}
        </div>
      )}
    </Section>
  );
}

// ---- console ----------------------------------------------------------------

function Console({ instanceId, canControl }: { instanceId: string; canControl?: boolean }) {
  const { t } = useTranslation();
  const gcode = useMoonrakerGcode(instanceId);
  const toast = useToast();
  const [openC, setOpenC] = useState(false);
  const [lines, setLines] = useState<string[]>([]);
  const [cmd, setCmd] = useState("");
  const preRef = useRef<HTMLPreElement>(null);

  useEffect(() => {
    if (!openC) return;
    return live.subscribe(mrTopic(instanceId), (raw: unknown) => {
      const { event, data } = (raw ?? {}) as { event?: string; data?: { line?: string } };
      if (event === "gcode" && data?.line != null) {
        setLines((prev) => [...prev.slice(-400), data.line as string]);
      }
    });
  }, [openC, instanceId]);

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
    <Card>
      <button
        onClick={() => setOpenC((v) => !v)}
        className="hover:bg-bg-subtle flex w-full items-center gap-2 px-4 py-2.5 text-left transition-colors"
      >
        <Terminal className="text-fg-subtle size-4" />
        <span className="text-fg text-[13.5px] font-semibold">{t("moonraker.sections.console")}</span>
        <ChevronDown className={cn("text-fg-subtle ml-auto size-4 transition-transform", openC && "rotate-180")} />
      </button>
      {openC && (
        <div className="border-edge border-t p-3">
          <pre
            ref={preRef}
            className="bg-surface text-fg-muted h-56 overflow-auto rounded-lg p-3 font-mono text-[12px] leading-relaxed whitespace-pre-wrap"
          >
            {lines.length ? lines.join("\n") : t("moonraker.console.empty")}
          </pre>
          {canControl && (
            <div className="mt-2 flex items-center gap-2">
              <Input
                value={cmd}
                onChange={(e) => setCmd(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && send()}
                placeholder={t("moonraker.console.placeholder")}
                className="font-mono text-[13px]"
              />
              <Button variant="primary" className="shrink-0" onClick={send} disabled={!cmd.trim()}>
                {t("moonraker.console.send")}
              </Button>
            </div>
          )}
        </div>
      )}
    </Card>
  );
}

// ---- cameras ----------------------------------------------------------------

function CameraSection({ cameras, instanceId, canControl }: { cameras: MoonrakerCamera[]; instanceId: string; canControl?: boolean }) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState<MoonrakerCamera | null>(null);
  return (
    <Section icon={Video} title={t("moonraker.sections.cameras")} count={cameras.length}>
      <div className={cn("grid gap-3", cameras.length > 1 ? "sm:grid-cols-2" : "")}>
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
    </Section>
  );
}

function aspectStyle(ratio: string): React.CSSProperties {
  const m = ratio.match(/^(\d+)\s*:\s*(\d+)$/);
  if (m && Number(m[1]) > 0 && Number(m[2]) > 0) return { aspectRatio: `${m[1]} / ${m[2]}` };
  return { aspectRatio: "16 / 9" };
}

function CameraTile({
  cam,
  instanceId,
  canControl,
  onExpand,
  expanded,
}: {
  cam: MoonrakerCamera;
  instanceId: string;
  canControl?: boolean;
  onExpand?: () => void;
  expanded?: boolean;
}) {
  const { t } = useTranslation();
  const setWebcam = useMoonrakerSetWebcam(instanceId);
  const toast = useToast();
  // Optimistic local flags so a toggle feels instant before the snapshot poll.
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

  // Persist, reverting the optimistic flag if the write fails (e.g. a camera
  // Moonraker sources from a config file, or an upstream error) so the toolbar
  // never claims a flip that didn't stick.
  const save = (
    patch: { flipHorizontal?: boolean; flipVertical?: boolean; rotation?: 0 | 90 | 180 | 270 },
    revert: () => void,
  ) =>
    setWebcam.mutate(
      { name: cam.name, ...patch },
      {
        onError: (e) => {
          revert();
          toast("error", t("moonraker.toast.failed"), String(e));
        },
      },
    );
  const toggleH = () => {
    const prev = flipH;
    const v = !flipH;
    setFlipH(v);
    save({ flipHorizontal: v }, () => setFlipH(prev));
  };
  const toggleV = () => {
    const prev = flipV;
    const v = !flipV;
    setFlipV(v);
    save({ flipVertical: v }, () => setFlipV(prev));
  };
  const rotate = () => {
    const prev = rotation;
    const v = (((rotation + 90) % 360) as 0 | 90 | 180 | 270);
    setRotation(v);
    save({ rotation: v }, () => setRotation(prev));
  };

  const transform = `rotate(${rotation}deg) scaleX(${flipH ? -1 : 1}) scaleY(${flipV ? -1 : 1})`;
  // MJPEG services stream into an <img> directly; WebRTC/HLS ones don't, so
  // fall back to polling the still-image endpoint (universal) for those.
  const streamable = !/webrtc|hls|rtsp/i.test(cam.service ?? "");
  const [tick, setTick] = useState(0);
  useEffect(() => {
    if (streamable || !cam.snapshot) return;
    const id = window.setInterval(() => setTick((x) => x + 1), 1000);
    return () => window.clearInterval(id);
  }, [streamable, cam.snapshot]);
  const src = streamable
    ? `${webcamStreamUrl(instanceId, cam.name)}&_=${nonce}`
    : cam.snapshot
      ? `${webcamSnapshotUrl(instanceId, cam.name)}&_=${tick}`
      : null;

  return (
    <div
      className="border-edge group relative mx-auto w-full overflow-hidden rounded-lg border bg-black"
      style={{ ...aspectStyle(cam.aspectRatio), maxHeight: expanded ? "78vh" : undefined }}
    >
      {src == null ? (
        <div className="absolute inset-0 flex flex-col items-center justify-center gap-2 px-4 text-center">
          <VideoOff className="size-6 text-white/60" />
          <span className="text-[12.5px] text-white/70">{t("moonraker.camera.unsupported")}</span>
          <span className="text-[11px] text-white/45">{cam.service}</span>
        </div>
      ) : streamable && errored ? (
        <button
          type="button"
          onClick={() => {
            setErrored(false);
            setNonce((n) => n + 1);
          }}
          className="absolute inset-0 flex flex-col items-center justify-center gap-2 text-center"
        >
          <VideoOff className="size-6 text-white/60" />
          <span className="text-[12.5px] text-white/70">{t("moonraker.camera.offline")}</span>
          <span className="text-[11px] text-white/45">{t("moonraker.camera.retry")}</span>
        </button>
      ) : (
        <img
          src={src}
          alt={cam.name}
          onError={() => streamable && setErrored(true)}
          className="absolute inset-0 size-full object-contain"
          style={{ transform }}
        />
      )}

      {/* Top toolbar — flip both directions independently, rotate, expand. */}
      <div className="absolute top-2 right-2 flex items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100">
        {canControl && (
          <>
            <CamBtn active={flipH} label={t("moonraker.camera.flipH")} onClick={toggleH}>
              <FlipHorizontal2 className="size-3.5" />
            </CamBtn>
            <CamBtn active={flipV} label={t("moonraker.camera.flipV")} onClick={toggleV}>
              <FlipVertical2 className="size-3.5" />
            </CamBtn>
            <CamBtn active={rotation !== 0} label={t("moonraker.camera.rotate")} onClick={rotate}>
              <RotateCw className="size-3.5" />
            </CamBtn>
          </>
        )}
        {onExpand && (
          <CamBtn label={t("moonraker.camera.expand")} onClick={onExpand}>
            <Maximize2 className="size-3.5" />
          </CamBtn>
        )}
      </div>

      <div className="pointer-events-none absolute inset-x-0 bottom-0 flex items-center gap-2 bg-gradient-to-t from-black/70 to-transparent px-3 pt-6 pb-2">
        <span className="truncate text-[12.5px] font-medium text-white">{cam.name}</span>
        {(flipH || flipV || rotation !== 0) && (
          <span className="text-[10.5px] text-white/55">
            {[flipH && "H", flipV && "V", rotation !== 0 && `${rotation}°`].filter(Boolean).join(" · ")}
          </span>
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
      className={cn(
        "flex size-7 items-center justify-center rounded-md border backdrop-blur transition-colors",
        active ? "border-accent bg-accent/80 text-white" : "border-white/20 bg-black/50 text-white/85 hover:bg-black/70",
      )}
    >
      {children}
    </button>
  );
}

// ---- shared bits ------------------------------------------------------------

function Section({
  icon: Icon,
  title,
  count,
  right,
  children,
}: {
  icon: React.ComponentType<{ className?: string }>;
  title: string;
  count?: number;
  right?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <section className="space-y-2.5">
      <div className="flex items-center gap-2">
        <Icon className="text-fg-subtle size-4" />
        <h2 className="text-fg text-[14px] font-semibold">{title}</h2>
        {count != null && <span className="text-fg-subtle text-[12.5px]">{count}</span>}
        {right && <div className="ml-auto">{right}</div>}
      </div>
      {children}
    </section>
  );
}

// ---- helpers ----------------------------------------------------------------

function clamp01(n: number): number {
  return Math.max(0, Math.min(1, n));
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
  // Klipper versions look like "v0.12.0-123-gabcdef"; keep the leading tag.
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

/** Klipper's SET_* commands address a device by its config name — the object
 * name minus its type prefix ("neopixel status" → "status", "fan" → "fan"). */
function configName(object: string): string {
  const i = object.indexOf(" ");
  return i >= 0 ? object.slice(i + 1) : object;
}

function presetsFor(kind: string): number[] {
  if (kind === "bed") return [0, 60, 70, 100, 110];
  if (kind === "extruder") return [0, 180, 200, 210, 230, 250];
  return [0, 40, 60, 80];
}

function rgbToHex(color: number[]): string {
  const to = (n: number) =>
    Math.round(clamp01(n ?? 0) * 255)
      .toString(16)
      .padStart(2, "0");
  return `#${to(color[0])}${to(color[1])}${to(color[2])}`;
}

function hexToRgb(hex: string): [number, number, number] {
  const m = hex.replace("#", "");
  const r = parseInt(m.slice(0, 2), 16) / 255;
  const g = parseInt(m.slice(2, 4), 16) / 255;
  const b = parseInt(m.slice(4, 6), 16) / 255;
  return [r || 0, g || 0, b || 0];
}
