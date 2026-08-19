import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { AlertTriangle } from "lucide-react";
import { Button } from "@/ui/button";
import { Dialog, DialogContent } from "@/ui/dialog";

/** The danger/warning banner listing the critical host(s) a dialog concerns. */
function CriticalBanner({ names, tone }: { names: string[]; tone: "danger" | "warning" }) {
  const { t } = useTranslation();
  const box =
    tone === "danger"
      ? "border-danger/30 bg-danger-soft"
      : "border-warning/40 bg-warning-soft";
  const head = tone === "danger" ? "text-danger" : "text-warning-soft-fg";
  return (
    <div className={`flex items-start gap-2.5 rounded-lg border px-3 py-2.5 ${box}`}>
      <AlertTriangle className={`mt-px size-4 shrink-0 ${head}`} />
      <div className="min-w-0">
        <p className={`text-[11.5px] font-semibold tracking-wide uppercase ${head}`}>
          {t("patchpilot.critical.label", { count: names.length })}
        </p>
        <p className="text-fg mt-0.5 text-[13.5px] font-medium break-words">{names.join(", ")}</p>
      </div>
    </div>
  );
}

/**
 * CriticalRunConfirm gates an IMMEDIATE run (Run all, a manual host run, or a
 * "run schedule now") that includes a host marked critical / the hypervisor.
 * The confirm button stays disabled for a countdown (default 10s) so it can't
 * be fired reflexively — updating a hypervisor can reboot it and take every
 * service and VM it hosts down with it. When the run also has non-critical
 * hosts, a "Skip critical" action runs just those, immediately.
 */
export function CriticalRunConfirm({
  open,
  onOpenChange,
  criticalNames,
  skipCount = 0,
  onConfirm,
  onSkipCritical,
  loading,
  seconds = 10,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  criticalNames: string[];
  skipCount?: number;
  onConfirm: () => void;
  onSkipCritical?: () => void;
  loading?: boolean;
  seconds?: number;
}) {
  const { t } = useTranslation();
  const [left, setLeft] = useState(seconds);
  useEffect(() => {
    if (!open) return;
    setLeft(seconds);
    const start = Date.now();
    const iv = setInterval(() => {
      setLeft(Math.max(0, seconds - Math.floor((Date.now() - start) / 1000)));
    }, 250);
    return () => clearInterval(iv);
  }, [open, seconds]);
  const ready = left <= 0;
  const canSkip = Boolean(onSkipCritical) && skipCount > 0;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={t("patchpilot.critical.runTitle")}
        footer={
          <>
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              {t("common.cancel")}
            </Button>
            {canSkip && (
              <Button variant="secondary" disabled={loading} onClick={onSkipCritical}>
                {t("patchpilot.critical.skip", { count: skipCount })}
              </Button>
            )}
            <Button variant="danger" loading={loading} disabled={!ready} onClick={onConfirm}>
              {ready ? t("patchpilot.critical.runConfirm") : t("patchpilot.critical.runWait", { s: left })}
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <CriticalBanner names={criticalNames} tone="danger" />
          <p className="text-fg-muted text-[13px]">{t("patchpilot.critical.runWarn2")}</p>
          {canSkip && (
            <p className="text-fg-subtle text-[12.5px]">
              {t("patchpilot.critical.skipNote", { count: skipCount })}
            </p>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

/**
 * CriticalScheduleConfirm is the lighter, no-timer confirmation shown when
 * SAVING a schedule that targets a critical host. Scheduled runs are unchanged;
 * this is just an acknowledgement that the schedule can touch the hypervisor.
 */
export function CriticalScheduleConfirm({
  open,
  onOpenChange,
  hostNames,
  onConfirm,
  loading,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  hostNames: string[];
  onConfirm: () => void;
  loading?: boolean;
}) {
  const { t } = useTranslation();
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={t("patchpilot.critical.scheduleTitle")}
        footer={
          <>
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              {t("common.cancel")}
            </Button>
            <Button variant="primary" loading={loading} onClick={onConfirm}>
              {t("patchpilot.critical.scheduleConfirm")}
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <CriticalBanner names={hostNames} tone="warning" />
          <p className="text-fg-muted text-[13px]">{t("patchpilot.critical.scheduleWarn")}</p>
        </div>
      </DialogContent>
    </Dialog>
  );
}
