import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  ArrowDown,
  ArrowUp,
  ChevronDown,
  Droplets,
  Fan,
  Flame,
  Lock,
  LockOpen,
  Minus,
  Pause,
  Play,
  Plus,
  Power,
  RefreshCw,
  Snowflake,
  Square,
  SunSnow,
  Undo2,
  type LucideIcon,
} from "lucide-react";
import type { HaEntity } from "@/lib/api";
import { cn } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Card } from "@/ui/card";
import { Slider } from "@/ui/slider";
import { Switch } from "@/ui/switch";
import {
  attrNum,
  attrStr,
  attrStrArr,
  isUnknownState,
  lightSupportsBrightness,
  lightSupportsColor,
  lightSupportsColorTemp,
  useHaService,
} from "./shared";

/** Domains rendered as a plain on/off Switch. */
const TOGGLE_DOMAINS = ["switch", "input_boolean", "fan", "siren", "humidifier"];

/** Binary-sensor device classes where "on" means trouble. */
const DANGER_ON_CLASSES = ["moisture", "smoke", "gas", "carbon_monoxide", "problem", "safety", "tamper", "heat"];

/** Known binary-sensor device classes with dedicated on/off wording. */
const BINARY_CLASSES = [
  "battery",
  "battery_charging",
  "connectivity",
  "door",
  "garage_door",
  "gas",
  "lock",
  "moisture",
  "motion",
  "occupancy",
  "opening",
  "plug",
  "power",
  "presence",
  "problem",
  "safety",
  "smoke",
  "tamper",
  "window",
];

export function stateLabel(t: (k: string) => string, state: string): string {
  switch (state) {
    case "on":
      return t("ha.state.on");
    case "off":
      return t("ha.state.off");
    case "unknown":
      return t("ha.state.unknown");
    case "unavailable":
      return t("ha.state.unavailable");
    default:
      return state.replace(/_/g, " ");
  }
}

type ServiceMutation = ReturnType<typeof useHaService>;

/**
 * EntityCard renders one entity with its domain-appropriate control. Controls
 * are never permission-probed client-side — a 403 from the service call is
 * toasted by useHaService instead. `displayName` is a board-level rename: it
 * replaces the entity's own name, and the entity id stays visible as the mono
 * subtitle so the underlying truth is never hidden.
 */
export function EntityCard({
  instanceId,
  entity,
  displayName,
  onOpenDetail,
}: {
  instanceId: string;
  entity: HaEntity;
  displayName?: string;
  onOpenDetail: (entity: HaEntity) => void;
}) {
  const svc = useHaService(instanceId);
  const call = (service: string, data?: Record<string, unknown>) =>
    svc.mutate({ entityId: entity.entityId, service, data });
  const pendingFor = (service: string) => svc.isPending && svc.variables?.service === service;
  const unknown = isUnknownState(entity.state);

  const domain = entity.domain;
  const isSensor = domain === "sensor";
  const renamed = displayName !== undefined && displayName !== "";

  return (
    <Card
      className={cn("flex flex-col gap-3 p-4", isSensor && "hover:border-edge-strong cursor-pointer transition-colors")}
      onClick={isSensor ? () => onOpenDetail(entity) : undefined}
    >
      <div className="flex items-start justify-between gap-3">
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            onOpenDetail(entity);
          }}
          className="group min-w-0 cursor-pointer text-left"
        >
          <p className="text-fg group-hover:text-accent truncate text-sm font-medium transition-colors">
            {renamed ? displayName : entity.name}
          </p>
          <p className="text-fg-subtle truncate font-mono text-[11px]">
            {isSensor && !renamed
              ? (attrStr(entity, "device_class")?.replace(/_/g, " ") ?? entity.entityId)
              : entity.entityId}
          </p>
        </button>
        <PrimaryControl entity={entity} svc={svc} call={call} unknown={unknown} />
      </div>

      <SecondaryControls entity={entity} svc={svc} call={call} pendingFor={pendingFor} unknown={unknown} />
    </Card>
  );
}

/**
 * CompactEntityControl is the one-line control for dense rows (group cards on
 * the personal board): the same primary control as the full card, with a small
 * action button standing in for domains whose primary surface normally lives
 * below the card header (scenes, scripts, buttons), and a state badge for
 * media players.
 */
export function CompactEntityControl({ instanceId, entity }: { instanceId: string; entity: HaEntity }) {
  const { t } = useTranslation();
  const svc = useHaService(instanceId);
  const call = (service: string, data?: Record<string, unknown>) =>
    svc.mutate({ entityId: entity.entityId, service, data });
  const unknown = isUnknownState(entity.state);

  if (entity.domain === "scene" || entity.domain === "script") {
    return (
      <Button size="sm" loading={svc.isPending} onClick={() => call("turn_on")}>
        {!svc.isPending && <Play className="size-3.5" />}
        {entity.domain === "scene" ? t("ha.scene.activate") : t("ha.script.run")}
      </Button>
    );
  }
  if (entity.domain === "button" || entity.domain === "input_button") {
    return (
      <Button size="sm" loading={svc.isPending} onClick={() => call("press")}>
        {t("ha.buttonEntity.press")}
      </Button>
    );
  }
  if (entity.domain === "media_player") {
    return <Badge tone={unknown ? "neutral" : "info"}>{stateLabel(t, entity.state)}</Badge>;
  }
  return <PrimaryControl entity={entity} svc={svc} call={call} unknown={unknown} />;
}

/** The compact control in the card's top-right corner. */
function PrimaryControl({
  entity,
  svc,
  call,
  unknown,
}: {
  entity: HaEntity;
  svc: ServiceMutation;
  call: (service: string, data?: Record<string, unknown>) => void;
  unknown: boolean;
}) {
  const { t } = useTranslation();
  const domain = entity.domain;

  if (domain === "light" || TOGGLE_DOMAINS.includes(domain)) {
    return (
      <Switch
        checked={entity.state === "on"}
        disabled={svc.isPending || unknown}
        onCheckedChange={(on) => call(on ? "turn_on" : "turn_off")}
        aria-label={entity.name}
      />
    );
  }

  if (domain === "binary_sensor") {
    const deviceClass = attrStr(entity, "device_class");
    const on = entity.state === "on";
    const known = deviceClass && BINARY_CLASSES.includes(deviceClass);
    const label = unknown
      ? stateLabel(t, entity.state)
      : known
        ? t(`ha.binaryClass.${deviceClass}.${on ? "on" : "off"}`)
        : stateLabel(t, entity.state);
    const tone = unknown
      ? "neutral"
      : on
        ? deviceClass && DANGER_ON_CLASSES.includes(deviceClass)
          ? "danger"
          : deviceClass === "battery"
            ? "warning"
            : deviceClass === "connectivity"
              ? "success"
              : "info"
        : "neutral";
    return <Badge tone={tone}>{label}</Badge>;
  }

  if (domain === "sensor") {
    const unit = attrStr(entity, "unit_of_measurement");
    return (
      <span className="text-fg shrink-0 text-right text-sm font-semibold whitespace-nowrap">
        {unknown ? "—" : entity.state}
        {!unknown && unit && <span className="text-fg-muted ml-0.5 text-xs font-normal">{unit}</span>}
      </span>
    );
  }

  if (domain === "lock" || domain === "alarm_control_panel" || domain === "vacuum" || domain === "cover") {
    const tone =
      entity.state === "locked" || entity.state === "closed" || entity.state === "disarmed" || entity.state === "docked"
        ? "neutral"
        : entity.state === "triggered" || entity.state === "jammed"
          ? "danger"
          : unknown
            ? "neutral"
            : "info";
    return <Badge tone={tone}>{stateLabel(t, entity.state)}</Badge>;
  }

  if (domain === "scene" || domain === "script" || domain === "button" || domain === "input_button" || domain === "media_player") {
    return null;
  }

  return <Badge tone={unknown ? "neutral" : "info"}>{stateLabel(t, entity.state)}</Badge>;
}

/** Domain-specific controls beneath the card header. */
function SecondaryControls({
  entity,
  svc,
  call,
  pendingFor,
  unknown,
}: {
  entity: HaEntity;
  svc: ServiceMutation;
  call: (service: string, data?: Record<string, unknown>) => void;
  pendingFor: (service: string) => boolean;
  unknown: boolean;
}) {
  const { t } = useTranslation();
  const domain = entity.domain;

  if (domain === "light") {
    return <LightControls entity={entity} svc={svc} call={call} unknown={unknown} />;
  }

  if (domain === "climate") return <ClimateControls entity={entity} svc={svc} call={call} unknown={unknown} />;

  if (domain === "cover") {
    const position = attrNum(entity, "current_position");
    return (
      <div className="space-y-2.5">
        <div className="flex gap-1.5">
          <Button size="sm" disabled={svc.isPending} loading={pendingFor("open_cover")} onClick={() => call("open_cover")}>
            <ArrowUp className="size-3.5" /> {t("ha.cover.open")}
          </Button>
          <Button size="sm" disabled={svc.isPending} loading={pendingFor("stop_cover")} onClick={() => call("stop_cover")}>
            <Square className="size-3.5" /> {t("ha.cover.stop")}
          </Button>
          <Button size="sm" disabled={svc.isPending} loading={pendingFor("close_cover")} onClick={() => call("close_cover")}>
            <ArrowDown className="size-3.5" /> {t("ha.cover.close")}
          </Button>
        </div>
        {position !== undefined && (
          <Slider
            value={position}
            min={0}
            max={100}
            disabled={svc.isPending || unknown}
            aria-label={t("ha.cover.position")}
            onValueCommit={(pos) => call("set_cover_position", { position: pos })}
          />
        )}
      </div>
    );
  }

  if (domain === "lock") {
    return (
      <div className="flex gap-1.5">
        <Button
          size="sm"
          variant={entity.state === "unlocked" ? "primary" : "secondary"}
          disabled={svc.isPending}
          loading={pendingFor("lock")}
          onClick={() => call("lock")}
        >
          <Lock className="size-3.5" /> {t("ha.lock.lock")}
        </Button>
        <Button
          size="sm"
          variant={entity.state === "locked" ? "primary" : "secondary"}
          disabled={svc.isPending}
          loading={pendingFor("unlock")}
          onClick={() => call("unlock")}
        >
          <LockOpen className="size-3.5" /> {t("ha.lock.unlock")}
        </Button>
      </div>
    );
  }

  if (domain === "media_player") {
    const title = attrStr(entity, "media_title");
    const volume = attrNum(entity, "volume_level");
    const playing = entity.state === "playing";
    return (
      <div className="space-y-2.5">
        <div className="flex items-center gap-2.5">
          <Button
            size="icon"
            className="size-7 shrink-0"
            disabled={svc.isPending || unknown}
            loading={pendingFor("media_play_pause")}
            aria-label={t("ha.media.playPause")}
            onClick={() => call("media_play_pause")}
          >
            {!pendingFor("media_play_pause") &&
              (playing ? <Pause className="size-3.5" /> : <Play className="size-3.5" />)}
          </Button>
          <p className="text-fg-muted min-w-0 flex-1 truncate text-[13px]">
            {title ?? stateLabel(t, entity.state)}
          </p>
        </div>
        {volume !== undefined && (
          <Slider
            value={Math.round(volume * 100)}
            min={0}
            max={100}
            disabled={svc.isPending}
            aria-label={t("ha.media.volume")}
            onValueCommit={(v) => call("volume_set", { volume_level: v / 100 })}
          />
        )}
      </div>
    );
  }

  if (domain === "scene") {
    return (
      <Button size="sm" className="self-start" disabled={svc.isPending} loading={pendingFor("turn_on")} onClick={() => call("turn_on")}>
        <Play className="size-3.5" /> {t("ha.scene.activate")}
      </Button>
    );
  }

  if (domain === "script") {
    return (
      <Button size="sm" className="self-start" disabled={svc.isPending} loading={pendingFor("turn_on")} onClick={() => call("turn_on")}>
        <Play className="size-3.5" /> {t("ha.script.run")}
      </Button>
    );
  }

  if (domain === "button" || domain === "input_button") {
    return (
      <Button size="sm" className="self-start" disabled={svc.isPending} loading={pendingFor("press")} onClick={() => call("press")}>
        {t("ha.buttonEntity.press")}
      </Button>
    );
  }

  if (domain === "vacuum") {
    return (
      <div className="flex flex-wrap gap-1.5">
        <Button size="sm" disabled={svc.isPending} loading={pendingFor("start")} onClick={() => call("start")}>
          <Play className="size-3.5" /> {t("ha.vacuum.start")}
        </Button>
        <Button size="sm" disabled={svc.isPending} loading={pendingFor("pause")} onClick={() => call("pause")}>
          <Pause className="size-3.5" /> {t("ha.vacuum.pause")}
        </Button>
        <Button size="sm" disabled={svc.isPending} loading={pendingFor("return_to_base")} onClick={() => call("return_to_base")}>
          <Undo2 className="size-3.5" /> {t("ha.vacuum.returnToBase")}
        </Button>
      </div>
    );
  }

  if (domain === "alarm_control_panel") {
    return (
      <div className="flex flex-wrap gap-1.5">
        <Button size="sm" disabled={svc.isPending} loading={pendingFor("alarm_arm_home")} onClick={() => call("alarm_arm_home")}>
          {t("ha.alarm.armHome")}
        </Button>
        <Button size="sm" disabled={svc.isPending} loading={pendingFor("alarm_arm_away")} onClick={() => call("alarm_arm_away")}>
          {t("ha.alarm.armAway")}
        </Button>
        <Button size="sm" disabled={svc.isPending} loading={pendingFor("alarm_disarm")} onClick={() => call("alarm_disarm")}>
          {t("ha.alarm.disarm")}
        </Button>
      </div>
    );
  }

  return null;
}

/** Reads the light's current color as a #rrggbb hex, defaulting to warm white. */
function lightHexColor(entity: HaEntity): string {
  const v = entity.attributes["rgb_color"];
  if (Array.isArray(v) && v.length >= 3 && v.slice(0, 3).every((n) => typeof n === "number")) {
    return (
      "#" +
      (v.slice(0, 3) as number[])
        .map((n) => Math.min(255, Math.max(0, Math.round(n))).toString(16).padStart(2, "0"))
        .join("")
    );
  }
  return "#ffd8a8";
}

/**
 * LightControls is the collapsible section under a light card: brightness,
 * color temperature, and color — each only when the light supports it. It is
 * collapsed by default and the expansion is view state, never persisted. When
 * both color and temperature exist, a small segmented toggle picks which one
 * is shown.
 */
function LightControls({
  entity,
  svc,
  call,
  unknown,
}: {
  entity: HaEntity;
  svc: ServiceMutation;
  call: (service: string, data?: Record<string, unknown>) => void;
  unknown: boolean;
}) {
  const { t } = useTranslation();
  const hasBrightness = lightSupportsBrightness(entity);
  const hasColor = lightSupportsColor(entity);
  const hasTemp = lightSupportsColorTemp(entity);

  const [expanded, setExpanded] = useState(false);
  // Which pane shows when the light does both; seeded from the current mode.
  const [pane, setPane] = useState<"color" | "temp">(() =>
    attrStr(entity, "color_mode") === "color_temp" ? "temp" : "color",
  );
  // Color drafts settle briefly before committing — the native picker fires
  // continuously while dragging, and one turn_on per swatch is plenty.
  const [colorDraft, setColorDraft] = useState<string | null>(null);
  useEffect(() => {
    if (colorDraft == null) return;
    const id = window.setTimeout(() => {
      const rgb = [1, 3, 5].map((i) => parseInt(colorDraft.slice(i, i + 2), 16));
      if (rgb.every((n) => Number.isFinite(n))) call("turn_on", { rgb_color: rgb });
      setColorDraft(null);
    }, 400);
    return () => window.clearTimeout(id);
    // call is stable enough for this lifetime; re-arm only when the draft moves.
  }, [colorDraft]); // eslint-disable-line react-hooks/exhaustive-deps

  if (!hasBrightness && !hasColor && !hasTemp) return null;

  const brightness = attrNum(entity, "brightness") ?? 0;
  const pct = Math.round((brightness / 255) * 100);
  const on = entity.state === "on";

  const tempMin = attrNum(entity, "min_color_temp_kelvin") ?? 2000;
  const tempMax = attrNum(entity, "max_color_temp_kelvin") ?? 6500;
  const tempNow = attrNum(entity, "color_temp_kelvin") ?? Math.round((tempMin + tempMax) / 2);

  const showTempPane = hasTemp && (!hasColor || pane === "temp");
  const showColorPane = hasColor && (!hasTemp || pane === "color");

  return (
    <div className="space-y-2.5">
      <button
        type="button"
        aria-expanded={expanded}
        onClick={() => setExpanded((v) => !v)}
        className="text-fg-subtle hover:text-fg flex w-full cursor-pointer items-center justify-between gap-2 text-xs font-medium transition-colors"
      >
        <span>
          {t("ha.light.controls")}
          {on && hasBrightness && <span className="text-fg-muted"> · {pct}%</span>}
        </span>
        <ChevronDown
          className={cn("size-3.5 transition-transform duration-150", expanded && "rotate-180")}
          aria-hidden
        />
      </button>

      {expanded && (
        <div className="space-y-2.5">
          {hasBrightness && (
            <div className="space-y-1">
              <p className="text-fg-subtle text-[11px]">{t("ha.light.brightness")}</p>
              <Slider
                value={brightness}
                min={0}
                max={255}
                disabled={svc.isPending || unknown}
                aria-label={t("ha.light.brightness")}
                onValueCommit={(b) => call("turn_on", { brightness: b })}
              />
            </div>
          )}

          {hasColor && hasTemp && (
            <div
              className="border-edge bg-bg-subtle/60 flex rounded-md border p-0.5"
              role="group"
              aria-label={t("ha.light.colorMode")}
            >
              {(["color", "temp"] as const).map((p) => (
                <button
                  key={p}
                  type="button"
                  aria-pressed={pane === p}
                  onClick={() => setPane(p)}
                  className={cn(
                    "h-6 flex-1 cursor-pointer rounded-[5px] text-[11px] font-medium transition-colors",
                    pane === p
                      ? "bg-surface text-fg shadow-1"
                      : "text-fg-muted hover:text-fg",
                  )}
                >
                  {p === "color" ? t("ha.light.color") : t("ha.light.colorTemp")}
                </button>
              ))}
            </div>
          )}

          {showTempPane && (
            <div className="space-y-1">
              <div className="flex items-baseline justify-between">
                <p className="text-fg-subtle text-[11px]">{t("ha.light.colorTemp")}</p>
                <p className="text-fg-muted text-[11px] tabular-nums">{tempNow} K</p>
              </div>
              <Slider
                value={tempNow}
                min={tempMin}
                max={tempMax}
                step={50}
                disabled={svc.isPending || unknown}
                aria-label={t("ha.light.colorTemp")}
                onValueCommit={(k) => call("turn_on", { color_temp_kelvin: k })}
              />
            </div>
          )}

          {showColorPane && (
            <div className="space-y-1">
              <p className="text-fg-subtle text-[11px]">{t("ha.light.color")}</p>
              <input
                type="color"
                value={colorDraft ?? lightHexColor(entity)}
                disabled={unknown}
                aria-label={t("ha.light.color")}
                onChange={(e) => setColorDraft(e.target.value)}
                className="border-edge bg-surface h-8 w-full cursor-pointer rounded-md border p-1 disabled:pointer-events-none disabled:opacity-55"
              />
            </div>
          )}
        </div>
      )}
    </div>
  );
}

/** Icons for the HVAC segmented control; unmapped modes fall back to text. */
const HVAC_MODE_ICONS: Record<string, LucideIcon> = {
  off: Power,
  heat: Flame,
  cool: Snowflake,
  heat_cool: SunSnow,
  auto: RefreshCw,
  dry: Droplets,
  fan_only: Fan,
};

/** Tone for the hvac_action line: heating warm, cooling cool, idle subtle. */
function hvacActionClass(action: string): string {
  if (action === "heating" || action === "preheating") return "text-warning";
  if (action === "cooling") return "text-info";
  return "text-fg-subtle";
}

/**
 * ClimateControls is a proper thermostat: large current temperature with the
 * live hvac_action underneath, a big debounced target stepper honoring the
 * entity's min/max/step, and the instance's hvac modes as an icon segmented
 * control (only modes the entity reports are offered).
 */
function ClimateControls({
  entity,
  svc,
  call,
  unknown,
}: {
  entity: HaEntity;
  svc: ServiceMutation;
  call: (service: string, data?: Record<string, unknown>) => void;
  unknown: boolean;
}) {
  const { t } = useTranslation();
  const current = attrNum(entity, "current_temperature");
  const target = attrNum(entity, "temperature");
  const min = attrNum(entity, "min_temp") ?? 7;
  const max = attrNum(entity, "max_temp") ?? 35;
  const step = attrNum(entity, "target_temp_step") ?? 0.5;
  const hvacModes = attrStrArr(entity, "hvac_modes");
  const action = attrStr(entity, "hvac_action");

  // Stepper clicks accumulate into a draft, committed once after a short
  // settle — rapid taps become one set_temperature call.
  const [draft, setDraft] = useState<number | null>(null);
  useEffect(() => {
    if (draft == null) return;
    const id = window.setTimeout(() => {
      call("set_temperature", { temperature: draft });
      setDraft(null);
    }, 700);
    return () => window.clearTimeout(id);
    // call is stable enough for this lifetime; re-arm only when draft moves.
  }, [draft]); // eslint-disable-line react-hooks/exhaustive-deps

  const shownTarget = draft ?? target;
  const adjust = (dir: 1 | -1) => {
    const from = shownTarget ?? current ?? min;
    const next = Math.min(max, Math.max(min, Math.round((from + dir * step) * 10) / 10));
    setDraft(next);
  };

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="text-fg text-3xl font-semibold tracking-tight">
            {current !== undefined ? `${current}°` : "—"}
          </p>
          <p className={cn("text-[11px] font-medium", action ? hvacActionClass(action) : "text-fg-subtle")}>
            {action
              ? t(`ha.hvacAction.${action}`, { defaultValue: action.replace(/_/g, " ") })
              : t("ha.climate.currentTemp")}
          </p>
        </div>
        {target !== undefined && (
          <div className="flex shrink-0 items-center gap-1.5">
            <Button
              size="icon"
              className="size-9 rounded-full"
              disabled={unknown}
              aria-label={t("ha.climate.decrease")}
              onClick={() => adjust(-1)}
            >
              <Minus className="size-4" />
            </Button>
            <div className="w-16 text-center">
              <p
                className={cn(
                  "text-fg text-lg leading-tight font-semibold tabular-nums",
                  (draft != null || svc.isPending) && "text-fg-muted",
                )}
              >
                {shownTarget}°
              </p>
              <p className="text-fg-subtle text-[10px]">{t("ha.climate.target")}</p>
            </div>
            <Button
              size="icon"
              className="size-9 rounded-full"
              disabled={unknown}
              aria-label={t("ha.climate.increase")}
              onClick={() => adjust(1)}
            >
              <Plus className="size-4" />
            </Button>
          </div>
        )}
      </div>
      {hvacModes.length > 0 && (
        <div
          className="border-edge bg-bg-subtle/60 flex rounded-lg border p-0.5"
          role="group"
          aria-label={t("ha.climate.hvacMode")}
        >
          {hvacModes.map((mode) => {
            const Icon = HVAC_MODE_ICONS[mode];
            const active = entity.state === mode;
            const label = t(`ha.hvacMode.${mode}`, { defaultValue: mode.replace(/_/g, " ") });
            return (
              <button
                key={mode}
                type="button"
                title={label}
                aria-label={label}
                aria-pressed={active}
                disabled={svc.isPending || unknown}
                onClick={() => call("set_hvac_mode", { hvac_mode: mode })}
                className={cn(
                  "flex h-7 flex-1 cursor-pointer items-center justify-center rounded-md transition-colors",
                  "disabled:pointer-events-none disabled:opacity-55",
                  active
                    ? "bg-accent-soft text-accent-soft-fg"
                    : "text-fg-muted hover:bg-surface-hover hover:text-fg",
                )}
              >
                {Icon ? <Icon className="size-4" aria-hidden /> : <span className="px-1 text-[11px]">{label}</span>}
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}
