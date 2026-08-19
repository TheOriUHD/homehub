import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { AlertTriangle } from "lucide-react";
import { Button } from "@/ui/button";
import { Dialog, DialogContent } from "@/ui/dialog";

/**
 * CriticalRunConfirm gates an IMMEDIATE run (Run all, a manual host run, or a
 * "run schedule now") that includes a host marked critical / the hypervisor.
 * The confirm button stays disabled for a countdown (default 10s) so it can't
 * be fired reflexively — updating a hypervisor can reboot it and take every
 * service and VM it hosts down with it.
 */
export function CriticalRunConfirm({
  open,
  onOpenChange,
  hostNames,
  onConfirm,
  loading,
  seconds = 10,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  hostNames: string[];
  onConfirm: () => void;
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

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={t("patchpilot.critical.runTitle")}
        description={
          <span className="block space-y-2">
            <span className="text-danger flex items-start gap-2">
              <AlertTriangle className="mt-0.5 size-4 shrink-0" />
              <span>{t("patchpilot.critical.runWarn")}</span>
            </span>
            <span className="text-fg block pl-6 font-medium">{hostNames.join(", ")}</span>
            <span className="block pl-6">{t("patchpilot.critical.runWarn2")}</span>
          </span>
        }
        footer={
          <>
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              {t("common.cancel")}
            </Button>
            <Button variant="danger" loading={loading} disabled={!ready} onClick={onConfirm}>
              {ready ? t("patchpilot.critical.runConfirm") : t("patchpilot.critical.runWait", { s: left })}
            </Button>
          </>
        }
      />
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
        description={
          <span className="block space-y-2">
            <span className="text-warning-soft-fg flex items-start gap-2">
              <AlertTriangle className="mt-0.5 size-4 shrink-0" />
              <span>{t("patchpilot.critical.scheduleWarn")}</span>
            </span>
            <span className="text-fg block pl-6 font-medium">{hostNames.join(", ")}</span>
          </span>
        }
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
      />
    </Dialog>
  );
}
