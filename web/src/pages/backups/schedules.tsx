import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { CalendarClock, Trash2 } from "lucide-react";
import {
  api,
  unwrap,
  isApiError,
  type BackupSchedule,
  type BackupTarget,
} from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { formatDateTime, formatRelative } from "@/lib/utils";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { Button } from "@/ui/button";
import { Input } from "@/ui/input";
import { Field } from "@/ui/field";
import { Switch } from "@/ui/switch";
import { Dialog, DialogContent } from "@/ui/dialog";
import { ConfirmDialog } from "@/ui/confirm";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { EmptyState, ErrorState } from "@/ui/states";
import { SkeletonRows } from "@/ui/skeleton";
import { useToast } from "@/ui/toast";
import { BACKUPS_RES } from "./shared";
import { buildCron, describeCron, looksLikeCron, weekdayName, type CronPreset } from "./cron";

export function SchedulesSection({ targets }: { targets: BackupTarget[] }) {
  const { t } = useTranslation();
  const toast = useToast();
  const { can } = useAuth();
  const queryClient = useQueryClient();

  const canConfigure = can(BACKUPS_RES, "configure");

  const schedules = useQuery({
    queryKey: ["backups", "schedules"],
    queryFn: async () => unwrap(await api.GET("/backups/schedules")),
  });

  const [addOpen, setAddOpen] = useState(false);
  const [deleting, setDeleting] = useState<BackupSchedule | null>(null);

  const toggle = useMutation({
    mutationFn: async ({ schedule, enabled }: { schedule: BackupSchedule; enabled: boolean }) =>
      unwrap(
        await api.PATCH("/backups/schedules/{id}", {
          params: { path: { id: schedule.id } },
          body: { cron: schedule.cron, enabled },
        }),
      ),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["backups", "schedules"] }),
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const remove = useMutation({
    mutationFn: async (id: string) =>
      unwrap(await api.DELETE("/backups/schedules/{id}", { params: { path: { id } } })),
    onSuccess: () => {
      setDeleting(null);
      toast("success", t("backups.schedules.deleted"));
      void queryClient.invalidateQueries({ queryKey: ["backups", "schedules"] });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <Card>
      <CardHeader
        title={t("backups.schedules.title")}
        description={t("backups.schedules.intro")}
        actions={
          canConfigure ? (
            <Button size="sm" variant="primary" disabled={targets.length === 0} onClick={() => setAddOpen(true)}>
              {t("backups.schedules.add")}
            </Button>
          ) : undefined
        }
      />
      <CardBody className="px-0 py-0">
        {schedules.isLoading ? (
          <SkeletonRows rows={2} className="p-5" />
        ) : schedules.isError ? (
          <ErrorState error={schedules.error} onRetry={() => void schedules.refetch()} />
        ) : (schedules.data ?? []).length === 0 ? (
          <EmptyState
            icon={CalendarClock}
            title={t("backups.schedules.empty.title")}
            body={t("backups.schedules.empty.body")}
          />
        ) : (
          <ul>
            {schedules.data!.map((schedule) => {
              const description = describeCron(schedule.cron, t);
              return (
                <li key={schedule.id} className="border-edge border-b px-5 py-3 last:border-b-0">
                  <div className="flex flex-wrap items-center gap-3">
                    <CalendarClock className="text-fg-subtle size-4 shrink-0" />
                    <div className="min-w-0 flex-1">
                      <p className="text-fg truncate text-sm font-medium">{schedule.targetName}</p>
                      <p className="text-fg-subtle mt-0.5 text-xs">
                        <code className="font-mono">{schedule.cron}</code>
                        {description && <> · {description}</>}
                      </p>
                      <p className="text-fg-subtle mt-0.5 text-xs">
                        {schedule.enabled && schedule.nextRunAt
                          ? t("backups.schedules.nextRun", { time: formatDateTime(schedule.nextRunAt) })
                          : t("backups.schedules.paused")}
                        {" · "}
                        {schedule.lastRunAt
                          ? t("backups.schedules.lastRun", { time: formatRelative(schedule.lastRunAt) })
                          : t("backups.schedules.lastNever")}
                      </p>
                    </div>
                    <Switch
                      checked={schedule.enabled}
                      disabled={!canConfigure || toggle.isPending}
                      aria-label={t("backups.schedules.toggleAria", { name: schedule.targetName })}
                      onCheckedChange={(enabled) => toggle.mutate({ schedule, enabled })}
                    />
                    {canConfigure && (
                      <Button
                        size="icon"
                        variant="danger-ghost"
                        aria-label={t("common.delete")}
                        onClick={() => setDeleting(schedule)}
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    )}
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </CardBody>

      <AddScheduleDialog open={addOpen} onOpenChange={setAddOpen} targets={targets} />

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={t("backups.schedules.deleteConfirmTitle")}
        body={t("backups.schedules.deleteConfirmBody", { name: deleting?.targetName ?? "" })}
        confirmLabel={t("common.delete")}
        loading={remove.isPending}
        onConfirm={() => deleting && remove.mutate(deleting.id)}
      />
    </Card>
  );
}

// ---- add dialog ------------------------------------------------------------

function AddScheduleDialog({
  open,
  onOpenChange,
  targets,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  targets: BackupTarget[];
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();

  const [targetId, setTargetId] = useState("");
  const [preset, setPreset] = useState<CronPreset>("daily");
  const [time, setTime] = useState("03:00");
  const [weekday, setWeekday] = useState("0");
  const [customCron, setCustomCron] = useState("");

  useEffect(() => {
    if (!open) return;
    setTargetId(targets.length === 1 ? targets[0].id : "");
    setPreset("daily");
    setTime("03:00");
    setWeekday("0");
    setCustomCron("");
  }, [open, targets]);

  const cron =
    preset === "custom" ? customCron.trim() : buildCron(preset, time, Number(weekday));
  const cronValid = looksLikeCron(cron);
  const description = useMemo(
    () => (cronValid ? describeCron(cron, t) : null),
    [cron, cronValid, t],
  );

  const create = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/backups/schedules", { body: { targetId, cron } })),
    onSuccess: () => {
      toast("success", t("backups.schedules.created"));
      void queryClient.invalidateQueries({ queryKey: ["backups", "schedules"] });
      onOpenChange(false);
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const valid = targetId !== "" && cronValid;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={t("backups.schedules.add")}
        footer={
          <>
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              {t("common.cancel")}
            </Button>
            <Button variant="primary" disabled={!valid} loading={create.isPending} onClick={() => create.mutate()}>
              {t("common.create")}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label={t("backups.schedules.target")}>
            {(p) => (
              <Select value={targetId} onValueChange={setTargetId}>
                <SelectTrigger id={p.id}>
                  <SelectValue placeholder={t("backups.schedules.targetPlaceholder")} />
                </SelectTrigger>
                <SelectContent>
                  {targets.map((target) => (
                    <SelectItem key={target.id} value={target.id}>
                      {target.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          </Field>

          <Field label={t("backups.schedules.cadence")}>
            {(p) => (
              <Select value={preset} onValueChange={(v) => setPreset(v as CronPreset)}>
                <SelectTrigger id={p.id}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="daily">{t("backups.schedules.presetDaily")}</SelectItem>
                  <SelectItem value="weekly">{t("backups.schedules.presetWeekly")}</SelectItem>
                  <SelectItem value="custom">{t("backups.schedules.presetCustom")}</SelectItem>
                </SelectContent>
              </Select>
            )}
          </Field>

          {preset === "weekly" && (
            <Field label={t("backups.schedules.day")}>
              {(p) => (
                <Select value={weekday} onValueChange={setWeekday}>
                  <SelectTrigger id={p.id}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {[1, 2, 3, 4, 5, 6, 0].map((day) => (
                      <SelectItem key={day} value={String(day)}>
                        {weekdayName(day)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            </Field>
          )}

          {preset !== "custom" ? (
            <Field label={t("backups.schedules.time")}>
              {(p) => (
                <Input
                  {...p}
                  type="time"
                  required
                  className="max-w-36"
                  value={time}
                  onChange={(e) => setTime(e.target.value || "03:00")}
                />
              )}
            </Field>
          ) : (
            <Field
              label={t("backups.schedules.cron")}
              hint={t("backups.schedules.cronHint")}
              error={customCron.trim() !== "" && !cronValid ? t("backups.schedules.invalidCron") : null}
            >
              {(p) => (
                <Input
                  {...p}
                  className="font-mono text-[13px]"
                  placeholder="30 3 * * *"
                  value={customCron}
                  onChange={(e) => setCustomCron(e.target.value)}
                />
              )}
            </Field>
          )}

          {cronValid && (
            <div className="bg-bg-subtle rounded-md px-3.5 py-2.5">
              <code className="text-fg font-mono text-[13px]">{cron}</code>
              <p className="text-fg-subtle mt-0.5 text-[13px]">
                {description
                  ? t("backups.schedules.preview", { text: description })
                  : t("backups.schedules.previewCustom")}
              </p>
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
