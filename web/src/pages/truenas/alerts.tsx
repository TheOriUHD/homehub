import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { BellOff, Check, X } from "lucide-react";
import type { TruenasAlert } from "@/lib/api";
import { formatRelative } from "@/lib/utils";
import { useToast } from "@/ui/toast";
import { Badge, StatusDot } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Card } from "@/ui/card";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { alertTone, useDismissTruenasAlert, useTruenasAlerts } from "./shared";

/**
 * TruenasAlertsPage is the full alert center behind the bell: every active
 * alert worst-first with per-row dismiss and a clear-all, then the already
 * dismissed ones muted below — acknowledged, not erased, exactly like the
 * NAS itself treats them.
 */
export function TruenasAlertsPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/truenas/$instanceId" });
  const toast = useToast();

  const alerts = useTruenasAlerts(instanceId);
  const dismiss = useDismissTruenasAlert(instanceId);

  if (alerts.isLoading) return <PageLoader />;
  if (alerts.isError) {
    return (
      <Card>
        <ErrorState error={alerts.error} onRetry={() => void alerts.refetch()} />
      </Card>
    );
  }

  const rank = { danger: 0, warning: 1, info: 2, neutral: 3 } as const;
  const sorted = [...(alerts.data ?? [])].sort(
    (a, b) => rank[alertTone(a.level)] - rank[alertTone(b.level)],
  );
  const active = sorted.filter((a) => !a.dismissed);
  const dismissed = sorted.filter((a) => a.dismissed);

  const onDismiss = (body: { uuid?: string; all?: boolean }) =>
    dismiss.mutate(body, {
      onError: (e) => toast("error", e instanceof Error ? e.message : String(e)),
    });

  if (active.length === 0 && dismissed.length === 0) {
    return (
      <Card>
        <EmptyState
          icon={BellOff}
          title={t("truenas.alerts.empty.title")}
          body={t("truenas.alerts.empty.body")}
        />
      </Card>
    );
  }

  return (
    <div className="space-y-4">
      <Card>
        <div className="divide-edge/70 divide-y">
          <div className="flex items-center gap-2 px-4 py-2.5">
            <span className="text-fg text-[13.5px] font-semibold">
              {t("truenas.alerts.activeCount", { count: active.length })}
            </span>
            {active.some((a) => a.uuid) && (
              <Button
                variant="ghost"
                size="sm"
                className="ml-auto"
                disabled={dismiss.isPending}
                onClick={() => onDismiss({ all: true })}
              >
                <Check className="size-3.5" />
                {t("truenas.alerts.clearAll")}
              </Button>
            )}
          </div>
          {active.length === 0 && (
            <p className="text-fg-subtle px-4 py-3 text-[13px]">{t("truenas.alerts.noneActive")}</p>
          )}
          {active.map((alert, i) => (
            <AlertRow
              key={alert.uuid ?? i}
              alert={alert}
              onDismiss={alert.uuid ? () => onDismiss({ uuid: alert.uuid }) : undefined}
              dismissing={dismiss.isPending}
            />
          ))}
        </div>
      </Card>

      {dismissed.length > 0 && (
        <Card>
          <div className="divide-edge/70 divide-y">
            <p className="text-fg-subtle px-4 py-2.5 text-[13px] font-semibold">
              {t("truenas.alerts.dismissedCount", { count: dismissed.length })}
            </p>
            {dismissed.map((alert, i) => (
              <AlertRow key={alert.uuid ?? `d-${i}`} alert={alert} muted />
            ))}
          </div>
        </Card>
      )}
    </div>
  );
}

function AlertRow({
  alert,
  muted,
  dismissing,
  onDismiss,
}: {
  alert: TruenasAlert;
  muted?: boolean;
  dismissing?: boolean;
  onDismiss?: () => void;
}) {
  const { t } = useTranslation();
  const tone = alertTone(alert.level);
  return (
    <div className={`flex items-start gap-2.5 px-4 py-2.5 text-[13px] ${muted ? "opacity-55" : ""}`}>
      <span className="mt-1"><StatusDot tone={tone === "neutral" ? "info" : tone} /></span>
      <div className="min-w-0 flex-1">
        <p className="text-fg">{alert.message}</p>
        <p className="text-fg-subtle mt-0.5 flex items-center gap-2 text-xs">
          <Badge tone={tone === "neutral" ? "info" : tone}>{alert.level}</Badge>
          {alert.createdAt && formatRelative(alert.createdAt)}
        </p>
      </div>
      {onDismiss && (
        <Button
          variant="ghost"
          size="icon"
          className="shrink-0"
          disabled={dismissing}
          onClick={onDismiss}
          aria-label={t("truenas.alerts.dismiss")}
        >
          <X className="size-3.5" />
        </Button>
      )}
    </div>
  );
}
