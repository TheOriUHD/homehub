import { useMemo, useState } from "react";
import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import cronstrue from "cronstrue";
import { CalendarClock, Pencil, Play, Plus, Trash2 } from "lucide-react";
import { isApiError, type PatchpilotHost, type PatchpilotSchedule } from "@/lib/api";
import { cn, formatRelative } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Card } from "@/ui/card";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { PageLoader } from "@/ui/misc";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { EmptyState, ErrorState } from "@/ui/states";
import { Switch } from "@/ui/switch";
import { useToast } from "@/ui/toast";
import {
  useDeleteSchedule,
  usePatchpilotHosts,
  usePatchpilotSchedules,
  useRunSchedule,
  useSaveSchedule,
} from "./shared";

/**
 * PatchpilotSchedulesPage lists the fleet's recurring patch windows and hosts
 * the create/edit editor. Cron is never free-typed: schedules are composed
 * from a frequency + structured time inputs, and existing expressions are
 * parsed back into those inputs when editing.
 */
export function PatchpilotSchedulesPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/patchpilot/$instanceId" });

  const schedules = usePatchpilotSchedules(instanceId);
  const [editing, setEditing] = useState<PatchpilotSchedule | null>(null);
  const [creating, setCreating] = useState(false);

  if (schedules.isLoading) return <PageLoader />;
  if (schedules.isError) {
    return (
      <Card>
        <ErrorState error={schedules.error} onRetry={() => void schedules.refetch()} />
      </Card>
    );
  }

  const list = schedules.data ?? [];

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-fg text-lg font-semibold tracking-tight">{t("patchpilot.schedules.title")}</h1>
        <Button variant="primary" size="sm" onClick={() => setCreating(true)}>
          <Plus className="size-4" />
          {t("patchpilot.schedules.add")}
        </Button>
      </div>

      {list.length === 0 ? (
        <Card>
          <EmptyState
            icon={CalendarClock}
            title={t("patchpilot.schedules.empty.title")}
            body={t("patchpilot.schedules.empty.body")}
            action={
              <Button variant="primary" size="sm" onClick={() => setCreating(true)}>
                <Plus className="size-4" />
                {t("patchpilot.schedules.add")}
              </Button>
            }
          />
        </Card>
      ) : (
        <div className="space-y-3">
          {list.map((schedule) => (
            <ScheduleCard
              key={schedule.id}
              instanceId={instanceId}
              schedule={schedule}
              onEdit={() => setEditing(schedule)}
            />
          ))}
        </div>
      )}

      {(creating || editing) && (
        <ScheduleEditor
          instanceId={instanceId}
          schedule={editing}
          onClose={() => {
            setCreating(false);
            setEditing(null);
          }}
        />
      )}
    </div>
  );
}

// ---- one schedule row ------------------------------------------------------

function ScheduleCard({
  instanceId,
  schedule,
  onEdit,
}: {
  instanceId: string;
  schedule: PatchpilotSchedule;
  onEdit: () => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const save = useSaveSchedule(instanceId);
  const run = useRunSchedule(instanceId);
  const remove = useDeleteSchedule(instanceId);
  const [confirmDelete, setConfirmDelete] = useState(false);

  const cronHuman = humanizeCron(schedule.cronExpr);

  const toggleEnabled = (enabled: boolean) => {
    save.mutate(
      { id: schedule.id, body: scheduleToInput(schedule, { enabled }) },
      {
        onError: (err) =>
          toast("error", isApiError(err) ? err.message : t("common.unknownError")),
      },
    );
  };

  const runNow = () => {
    run.mutate(schedule.id, {
      onSuccess: () => toast("success", t("patchpilot.schedules.runStarted", { name: schedule.name })),
      onError: (err) => {
        if (isApiError(err) && err.status === 409) {
          toast("warning", t("patchpilot.schedules.runBusy"));
          return;
        }
        toast("error", isApiError(err) ? err.message : t("common.unknownError"));
      },
    });
  };

  const doDelete = () => {
    remove.mutate(schedule.id, {
      onSuccess: () => {
        toast("success", t("patchpilot.schedules.deleted", { name: schedule.name }));
        setConfirmDelete(false);
      },
      onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
    });
  };

  return (
    <Card className={cn("px-5 py-4", !schedule.enabled && "opacity-70")}>
      <div className="flex flex-wrap items-start gap-4">
        <div className="min-w-0 flex-1 space-y-1.5">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-fg text-sm font-semibold">{schedule.name}</span>
            {schedule.suppressReboot && (
              <Badge tone="neutral">{t("patchpilot.schedules.noReboot")}</Badge>
            )}
          </div>
          <p className="text-fg-muted text-[13px]">{cronHuman}</p>
          <div className="text-fg-subtle flex flex-wrap items-center gap-x-4 gap-y-1 text-[12.5px]">
            <span>{t("patchpilot.schedules.hosts", { count: schedule.hostIds.length })}</span>
            {schedule.nextRunAt && (
              <span>
                {t("patchpilot.schedules.nextRun")} {formatRelative(schedule.nextRunAt)}
              </span>
            )}
            {schedule.lastRunAt && (
              <span>
                {t("patchpilot.schedules.lastRun")} {formatRelative(schedule.lastRunAt)}
              </span>
            )}
          </div>
          {schedule.lastSkipReason && (
            <p className="text-fg-subtle text-[12.5px] italic">
              {t("patchpilot.schedules.skipped", { reason: schedule.lastSkipReason })}
            </p>
          )}
        </div>

        <div className="flex shrink-0 items-center gap-1.5">
          <Switch
            checked={schedule.enabled}
            onCheckedChange={toggleEnabled}
            disabled={save.isPending}
            aria-label={t("patchpilot.schedules.enabledLabel")}
          />
          <Button
            variant="secondary"
            size="sm"
            loading={run.isPending}
            onClick={runNow}
          >
            <Play className="size-3.5" />
            {t("patchpilot.schedules.runNow")}
          </Button>
          <Button variant="ghost" size="icon" onClick={onEdit} aria-label={t("common.edit")}>
            <Pencil className="size-4" />
          </Button>
          <Button
            variant="danger-ghost"
            size="icon"
            onClick={() => setConfirmDelete(true)}
            aria-label={t("common.delete")}
          >
            <Trash2 className="size-4" />
          </Button>
        </div>
      </div>

      {confirmDelete && (
        <Dialog open onOpenChange={(open) => !open && setConfirmDelete(false)}>
          <DialogContent
            title={t("patchpilot.schedules.deleteTitle")}
            footer={
              <>
                <Button variant="ghost" onClick={() => setConfirmDelete(false)}>
                  {t("common.cancel")}
                </Button>
                <Button variant="danger" loading={remove.isPending} onClick={doDelete}>
                  {t("common.delete")}
                </Button>
              </>
            }
          >
            <p className="text-fg-muted text-[13px]">
              {t("patchpilot.schedules.deleteConfirm", { name: schedule.name })}
            </p>
          </DialogContent>
        </Dialog>
      )}
    </Card>
  );
}

// ---- editor ----------------------------------------------------------------

type Frequency = "hourly" | "daily" | "weekly" | "monthly";

const FREQUENCIES: Frequency[] = ["hourly", "daily", "weekly", "monthly"];

// A monthly schedule picks either a fixed calendar day ("dom") or a weekday
// occurrence ("weekday"), e.g. the first Saturday of the month.
type MonthlyMode = "dom" | "weekday";

// Occurrence values for the weekday mode: 1st–4th, plus "L" for the last one.
const OCCURRENCES = ["1", "2", "3", "4", "L"] as const;

function ScheduleEditor({
  instanceId,
  schedule,
  onClose,
}: {
  instanceId: string;
  schedule: PatchpilotSchedule | null;
  onClose: () => void;
}) {
  const { t, i18n } = useTranslation();
  const toast = useToast();
  const save = useSaveSchedule(instanceId);
  const hosts = usePatchpilotHosts(instanceId);
  const isEdit = schedule !== null;

  const seed = useMemo(() => parseCron(schedule?.cronExpr), [schedule?.cronExpr]);

  const [name, setName] = useState(schedule?.name ?? "");
  const [frequency, setFrequency] = useState<Frequency>(seed.frequency);
  const [minute, setMinute] = useState(seed.minute);
  const [hour, setHour] = useState(seed.hour);
  const [dow, setDow] = useState(seed.dow);
  const [dom, setDom] = useState(seed.dom);
  const [monthlyMode, setMonthlyMode] = useState<MonthlyMode>(seed.monthlyMode);
  const [nth, setNth] = useState(seed.nth);
  const [hostIds, setHostIds] = useState<string[]>(schedule?.hostIds ?? []);
  const [suppressReboot, setSuppressReboot] = useState(schedule?.suppressReboot ?? false);

  // Locale-aware weekday labels; 2024-01-07 (UTC) is a Sunday → index 0.
  const weekdayLabels = useMemo(() => {
    const fmt = new Intl.DateTimeFormat(i18n.language, { weekday: "long" });
    return Array.from({ length: 7 }, (_, i) => fmt.format(new Date(Date.UTC(2024, 0, 7 + i))));
  }, [i18n.language]);

  const cronExpr = buildCron(frequency, { minute, hour, dow, dom, monthlyMode, nth });
  const cronHuman = humanizeCron(cronExpr);

  const hostList = hosts.data ?? [];
  const canSave = name.trim().length > 0 && hostIds.length > 0;

  const toggleHost = (id: string) => {
    setHostIds((prev) => (prev.includes(id) ? prev.filter((h) => h !== id) : [...prev, id]));
  };

  const submit = () => {
    save.mutate(
      {
        id: schedule?.id,
        body: {
          name: name.trim(),
          cronExpr,
          enabled: schedule?.enabled ?? true,
          suppressReboot,
          hostIds,
        },
      },
      {
        onSuccess: () => {
          toast("success", isEdit ? t("patchpilot.schedules.saved") : t("patchpilot.schedules.created"));
          onClose();
        },
        onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
      },
    );
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        wide
        title={isEdit ? t("patchpilot.schedules.editTitle") : t("patchpilot.schedules.add")}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button variant="primary" disabled={!canSave} loading={save.isPending} onClick={submit}>
              {isEdit ? t("common.save") : t("common.create")}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label={t("patchpilot.schedules.form.name")}>
            {(p) => (
              <Input
                {...p}
                autoFocus
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={t("patchpilot.schedules.form.namePlaceholder")}
              />
            )}
          </Field>

          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t("patchpilot.schedules.form.frequency")}>
              {() => (
                <Select value={frequency} onValueChange={(v) => setFrequency(v as Frequency)}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {FREQUENCIES.map((f) => (
                      <SelectItem key={f} value={f}>
                        {t(`patchpilot.schedules.freq.${f}`)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            </Field>

            {frequency === "weekly" && (
              <Field label={t("patchpilot.schedules.form.dayOfWeek")}>
                {() => (
                  <Select value={dow} onValueChange={setDow}>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {weekdayLabels.map((label, i) => (
                        <SelectItem key={i} value={String(i)}>
                          {label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
              </Field>
            )}

            {frequency === "monthly" && (
              <Field label={t("patchpilot.schedules.form.monthlyMode")}>
                {() => (
                  <Select value={monthlyMode} onValueChange={(v) => setMonthlyMode(v as MonthlyMode)}>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="dom">{t("patchpilot.schedules.form.monthlyModeDom")}</SelectItem>
                      <SelectItem value="weekday">{t("patchpilot.schedules.form.monthlyModeWeekday")}</SelectItem>
                    </SelectContent>
                  </Select>
                )}
              </Field>
            )}
          </div>

          {frequency === "monthly" && monthlyMode === "dom" && (
            <Field label={t("patchpilot.schedules.form.dayOfMonth")} hint={t("patchpilot.schedules.form.dayOfMonthHint")}>
              {(p) => (
                <Input
                  {...p}
                  type="number"
                  min={1}
                  max={28}
                  value={dom}
                  onChange={(e) => setDom(clampField(e.target.value, 1, 28, 1))}
                />
              )}
            </Field>
          )}

          {frequency === "monthly" && monthlyMode === "weekday" && (
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label={t("patchpilot.schedules.form.occurrence")}>
                {() => (
                  <Select value={nth} onValueChange={setNth}>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {OCCURRENCES.map((n) => (
                        <SelectItem key={n} value={n}>
                          {t(`patchpilot.schedules.occurrence.${n}`)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
              </Field>
              <Field label={t("patchpilot.schedules.form.dayOfWeek")}>
                {() => (
                  <Select value={dow} onValueChange={setDow}>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {weekdayLabels.map((label, i) => (
                        <SelectItem key={i} value={String(i)}>
                          {label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
              </Field>
            </div>
          )}

          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t("patchpilot.schedules.form.minute")}>
              {(p) => (
                <Input
                  {...p}
                  type="number"
                  min={0}
                  max={59}
                  value={minute}
                  onChange={(e) => setMinute(clampField(e.target.value, 0, 59, 0))}
                />
              )}
            </Field>
            {frequency !== "hourly" && (
              <Field label={t("patchpilot.schedules.form.hour")}>
                {(p) => (
                  <Input
                    {...p}
                    type="number"
                    min={0}
                    max={23}
                    value={hour}
                    onChange={(e) => setHour(clampField(e.target.value, 0, 23, 0))}
                  />
                )}
              </Field>
            )}
          </div>

          <div className="bg-accent-soft text-accent-soft-fg rounded-lg px-3 py-2.5 text-[13px]">
            {t("patchpilot.schedules.form.cronPreview", { cron: cronHuman })}
          </div>

          <div className="space-y-2">
            <div className="text-fg flex items-baseline gap-2 text-[13px] font-medium">
              {t("patchpilot.schedules.form.hosts")}
              <span className="text-fg-subtle text-xs font-normal">
                {t("patchpilot.schedules.form.selectHosts")}
              </span>
            </div>
            {hosts.isLoading ? (
              <p className="text-fg-subtle text-[13px]">{t("common.loading")}</p>
            ) : hostList.length === 0 ? (
              <p className="text-fg-subtle text-[13px]">{t("patchpilot.schedules.form.noHosts")}</p>
            ) : (
              <div className="flex flex-wrap gap-2">
                {hostList.map((host) => (
                  <HostChip
                    key={host.id}
                    host={host}
                    selected={hostIds.includes(host.id)}
                    onToggle={() => toggleHost(host.id)}
                  />
                ))}
              </div>
            )}
          </div>

          <label className="border-edge flex items-start gap-3 rounded-lg border px-3 py-2.5">
            <Switch checked={suppressReboot} onCheckedChange={setSuppressReboot} className="mt-0.5" />
            <span className="min-w-0">
              <span className="text-fg block text-[13px] font-medium">
                {t("patchpilot.schedules.form.suppressReboot")}
              </span>
              <span className="text-fg-subtle block text-[12.5px]">
                {t("patchpilot.schedules.form.suppressRebootHint")}
              </span>
            </span>
          </label>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function HostChip({
  host,
  selected,
  onToggle,
}: {
  host: PatchpilotHost;
  selected: boolean;
  onToggle: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-pressed={selected}
      className={cn(
        "flex cursor-pointer flex-col items-start rounded-lg border px-3 py-2 text-left transition-colors",
        selected
          ? "border-accent bg-accent-soft text-accent-soft-fg"
          : "border-edge bg-bg-subtle text-fg-muted hover:text-fg",
      )}
    >
      <span className="text-[13px] font-medium">{host.name}</span>
      <span className="font-mono text-[11.5px] opacity-80">{host.hostname}</span>
    </button>
  );
}

// ---- cron helpers ----------------------------------------------------------

/** Best-effort plain-English summary; falls back to the raw expression. */
function humanizeCron(expr: string): string {
  try {
    return cronstrue.toString(expr, { use24HourTimeFormat: true });
  } catch {
    return expr;
  }
}

function buildCron(
  frequency: Frequency,
  parts: { minute: string; hour: string; dow: string; dom: string; monthlyMode: MonthlyMode; nth: string },
): string {
  const m = parts.minute || "0";
  const h = parts.hour || "0";
  switch (frequency) {
    case "hourly":
      return `${m} * * * *`;
    case "daily":
      return `${m} ${h} * * *`;
    case "weekly":
      return `${m} ${h} * * ${parts.dow || "0"}`;
    case "monthly":
      if (parts.monthlyMode === "weekday") {
        const d = parts.dow || "0";
        // "6L" = last <weekday>, "6#1".."6#4" = first..fourth <weekday>.
        return parts.nth === "L" ? `${m} ${h} * * ${d}L` : `${m} ${h} * * ${d}#${parts.nth || "1"}`;
      }
      return `${m} ${h} ${parts.dom || "1"} * *`;
  }
}

type CronSeed = {
  frequency: Frequency;
  minute: string;
  hour: string;
  dow: string;
  dom: string;
  monthlyMode: MonthlyMode;
  nth: string;
};

const DEFAULT_SEED: CronSeed = {
  frequency: "daily",
  minute: "0",
  hour: "3",
  dow: "0",
  dom: "1",
  monthlyMode: "dom",
  nth: "1",
};

/** Parse a 5-field cron back into the structured editor inputs. */
function parseCron(expr: string | undefined): CronSeed {
  if (!expr) return DEFAULT_SEED;
  const fields = expr.trim().split(/\s+/);
  if (fields.length !== 5) return DEFAULT_SEED;
  const [min, hr, dom, mon, dow] = fields;
  const isNum = (v: string) => /^\d+$/.test(v);

  // Anything with lists/ranges/steps in month is beyond the builder's shape.
  if (mon !== "*") return DEFAULT_SEED;

  // Monthly nth-weekday extension: "6#1".."6#4" (first..fourth) or "6L" (last).
  const nthMatch = /^([0-6])#([1-4])$/.exec(dow);
  const lastMatch = /^([0-6])L$/.exec(dow);
  if (isNum(min) && isNum(hr) && dom === "*" && (nthMatch || lastMatch)) {
    const wd = nthMatch ? nthMatch[1] : lastMatch![1];
    const nth = nthMatch ? nthMatch[2] : "L";
    return { ...DEFAULT_SEED, frequency: "monthly", minute: min, hour: hr, dow: wd, monthlyMode: "weekday", nth };
  }

  if (isNum(min) && hr === "*" && dom === "*" && dow === "*") {
    return { ...DEFAULT_SEED, frequency: "hourly", minute: min, hour: "0" };
  }
  if (isNum(min) && isNum(hr) && dow !== "*" && isNum(dow)) {
    return { ...DEFAULT_SEED, frequency: "weekly", minute: min, hour: hr, dow };
  }
  if (isNum(min) && isNum(hr) && dom !== "*" && isNum(dom)) {
    return { ...DEFAULT_SEED, frequency: "monthly", minute: min, hour: hr, dom };
  }
  if (isNum(min) && isNum(hr) && dom === "*" && dow === "*") {
    return { ...DEFAULT_SEED, frequency: "daily", minute: min, hour: hr };
  }
  return DEFAULT_SEED;
}

function clampField(raw: string, min: number, max: number, fallback: number): string {
  if (raw === "") return "";
  const n = Number(raw);
  if (!Number.isFinite(n)) return String(fallback);
  return String(Math.max(min, Math.min(max, Math.trunc(n))));
}

/** Rebuild a full schedule input from an existing schedule, applying a patch. */
function scheduleToInput(
  schedule: PatchpilotSchedule,
  patch: { enabled?: boolean } = {},
) {
  return {
    name: schedule.name,
    cronExpr: schedule.cronExpr,
    hostIds: schedule.hostIds,
    enabled: patch.enabled ?? schedule.enabled,
    suppressReboot: schedule.suppressReboot,
  };
}
