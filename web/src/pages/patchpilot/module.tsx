import { Link, Outlet, useParams, useRouterState } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { AlertTriangle, CheckCircle2, ServerCog, X } from "lucide-react";
import { api, isApiError, unwrap } from "@/lib/api";
import { useLiveInvalidate } from "@/lib/live";
import { useSignals } from "@/pages/signals";
import { Button } from "@/ui/button";
import { useToast } from "@/ui/toast";
import { statusTone } from "@/lib/systems";
import { cn } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { Card } from "@/ui/card";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { useFleetInstances, usePatchpilotHosts, ppTopic } from "./shared";

/**
 * PatchpilotModulePage frames one fleet with the tab nav and a header
 * summarising host count and pending updates. Live WS events (bootstrap
 * steps, run events, host changes) invalidate the relevant queries.
 */
export function PatchpilotModulePage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/patchpilot/$instanceId" });
  const pathname = useRouterState({ select: (s) => s.location.pathname });

  const instances = useFleetInstances();
  useLiveInvalidate("integrations", [["integrations"]]);
  useLiveInvalidate(ppTopic(instanceId), [["patchpilot", instanceId]]);
  const hosts = usePatchpilotHosts(instanceId);

  if (instances.isLoading) return <PageLoader />;
  if (instances.isError) {
    return (
      <Card>
        <ErrorState error={instances.error} onRetry={() => void instances.refetch()} />
      </Card>
    );
  }
  const instance = instances.data?.find((row) => row.id === instanceId && row.type === "patchpilot");
  if (!instance) {
    return (
      <Card>
        <EmptyState icon={AlertTriangle} title={t("errors.notFound.title")} body={t("errors.notFound.body")} />
      </Card>
    );
  }

  const hostList = hosts.data ?? [];
  const pendingTotal = hostList.reduce((sum, h) => sum + h.updatesPending, 0);
  const rebootCount = hostList.filter((h) => h.rebootRequired).length;

  const base = `/patchpilot/${instanceId}`;
  const tabs = [
    { to: base, label: t("patchpilot.tabs.hosts"), exact: true },
    { to: `${base}/schedules`, label: t("patchpilot.tabs.schedules") },
    { to: `${base}/runs`, label: t("patchpilot.tabs.runs") },
  ];

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
          <ServerCog className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="text-fg flex items-center gap-2.5 text-xl font-semibold tracking-tight">
            {instance.name}
            <Badge tone={statusTone[instance.status]}>
              <StatusDot tone={statusTone[instance.status]} pulse={instance.status !== "connected"} />
              {t(`integrations.status.${instance.status}`)}
            </Badge>
          </h1>
          <p className="text-fg-muted text-sm">{t("patchpilot.subtitle")}</p>
        </div>
        <div className="flex flex-wrap items-center gap-1.5">
          <Badge tone="neutral">{t("patchpilot.chips.hosts", { count: hostList.length })}</Badge>
          {pendingTotal > 0 && (
            <Badge tone="warning">{t("patchpilot.chips.updates", { count: pendingTotal })}</Badge>
          )}
          {rebootCount > 0 && (
            <Badge tone="warning">{t("patchpilot.chips.reboot", { count: rebootCount })}</Badge>
          )}
        </div>
      </div>

      {instance.status !== "connected" && instance.statusMessage && (
        <div className="bg-danger-soft text-danger-soft-fg rounded-lg px-4 py-3 text-[13px]" role="alert">
          {instance.statusMessage}
        </div>
      )}

      <LastRunNotice instanceId={instanceId} />

      <nav className="border-edge no-scrollbar flex gap-1 overflow-x-auto overflow-y-hidden border-b">
        {tabs.map((tab) => {
          const active = tab.exact ? pathname === tab.to : pathname.startsWith(tab.to);
          return (
            <Link
              key={tab.to}
              to={tab.to}
              className={cn(
                "-mb-px shrink-0 border-b-2 px-3.5 py-2 text-[13px] font-medium transition-colors",
                active ? "border-accent text-fg" : "text-fg-muted hover:text-fg border-transparent",
              )}
            >
              {tab.label}
            </Link>
          );
        })}
      </nav>

      <Outlet />
    </div>
  );
}

/**
 * LastRunNotice is the fleet's "last run" signal, shown where the runs are
 * managed and dismissable right here. Dismissing acknowledges the signal
 * itself, so it leaves the Signals page and the overview at the same time,
 * and it only returns when a *later* run fails.
 */
function LastRunNotice({ instanceId }: { instanceId: string }) {
  const { t } = useTranslation();
  const toast = useToast();
  const qc = useQueryClient();
  const signals = useSignals();
  useLiveInvalidate("signals", [["signals"]]);

  const signal = (signals.data?.items ?? []).find((s) => {
    const d = s.detail as { kind?: unknown } | null | undefined;
    return s.collector === "patchpilot" && s.sourceId === instanceId && d?.kind === "last-run";
  });

  const dismiss = useMutation({
    mutationFn: async (ids: number[]) => unwrap(await api.POST("/signals/dismiss", { body: { ids } })),
    onSuccess: () => {
      toast("success", t("patchpilot.lastRun.dismissed"));
      void qc.invalidateQueries({ queryKey: ["signals"] });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  if (!signal) return null;
  const recovered = signal.severity === "recovered";
  const failed = ((signal.detail as { failedHosts?: unknown } | null)?.failedHosts ?? []) as {
    host: string;
    error: string;
  }[];

  return (
    <div
      className={
        recovered
          ? "bg-success-soft text-success-soft-fg rounded-lg px-4 py-3"
          : "bg-danger-soft text-danger-soft-fg rounded-lg px-4 py-3"
      }
      role={recovered ? "status" : "alert"}
    >
      <div className="flex items-start gap-3">
        {recovered ? (
          <CheckCircle2 className="mt-0.5 size-4 shrink-0" />
        ) : (
          <AlertTriangle className="mt-0.5 size-4 shrink-0" />
        )}
        <div className="min-w-0 flex-1 space-y-1">
          <p className="text-[13.5px] font-semibold">{signal.title}</p>
          <p className="text-[13px] opacity-90">{signal.body}</p>
          {!recovered && failed.length > 0 && (
            <ul className="mt-1.5 space-y-1 text-[12.5px]">
              {failed.map((f) => (
                <li key={f.host} className="flex flex-wrap gap-x-1.5">
                  <span className="font-medium">{f.host}</span>
                  <span className="opacity-90">— {f.error}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
        <Button
          variant="ghost"
          size="sm"
          loading={dismiss.isPending}
          onClick={() => dismiss.mutate([signal.id])}
          aria-label={t("patchpilot.lastRun.dismiss")}
        >
          <X className="size-3.5" />
          {t("patchpilot.lastRun.dismiss")}
        </Button>
      </div>
    </div>
  );
}

