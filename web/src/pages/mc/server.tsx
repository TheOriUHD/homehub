import { Link, Outlet, useParams, useRouterState } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { ArrowLeft, Play, RotateCw, Square } from "lucide-react";
import { useState, type ReactNode } from "react";
import { api, isApiError, unwrap } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { Button } from "@/ui/button";
import { ConfirmDialog } from "@/ui/confirm";
import { useToast } from "@/ui/toast";
import { capacityTextClass } from "@/lib/capacity";
import { useMetricSeries } from "@/lib/metrics";
import { cn } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { Card } from "@/ui/card";
import { Sparkline } from "@/ui/sparkline";
import { EmptyState } from "@/ui/states";
import { AlertTriangle } from "lucide-react";
import { loaderLabel, mcOverviewKey, mcServerResource, tpsTone, useMcOverview, useMcServer } from "./shared";

/**
 * McServerPage frames ONE server of the network: back-link, live header
 * stats, and the console/players/performance tabs beneath.
 */
export function McServerPage() {
  const { t } = useTranslation();
  const { instanceId, serverId } = useParams({ from: "/shell/mc/$instanceId/s/$serverId" });
  const path = useRouterState({ select: (s) => s.location.pathname });
  const { can } = useAuth();
  const sshConfigured = useMcOverview(instanceId).data?.sshConfigured ?? false;

  const server = useMcServer(instanceId, serverId);
  if (!server) {
    return (
      <Card>
        <EmptyState icon={AlertTriangle} title={t("errors.notFound.title")} body={t("mc.server.gone")} />
      </Card>
    );
  }

  const base = `/mc/${instanceId}/s/${serverId}`;
  const tabs = [
    { to: base, label: t("mc.tabs.console"), exact: true },
    { to: `${base}/players`, label: t("mc.tabs.players") },
    { to: `${base}/performance`, label: t("mc.tabs.performance") },
  ];
  const st = server.connected ? server.status : null;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <Link
          to="/mc/$instanceId"
          params={{ instanceId }}
          className="text-fg-muted hover:text-fg flex items-center gap-1 text-[13px] font-medium transition-colors"
        >
          <ArrowLeft className="size-3.5" />
          {t("mc.server.back")}
        </Link>
        <div className="min-w-0 flex-1">
          <p className="text-fg flex items-center gap-2 text-[15px] font-semibold">
            {server.label}
            <StatusDot tone={server.connected ? "success" : "neutral"} pulse={!server.connected} />
            {loaderLabel(server.loader) && <Badge tone="neutral">{loaderLabel(server.loader)}</Badge>}
            {server.mcVersion && <span className="text-fg-subtle text-[12px] font-normal">{server.mcVersion}</span>}
          </p>
        </div>

        {can(mcServerResource(instanceId, serverId), "control") && sshConfigured && (
          <PowerControls instanceId={instanceId} serverId={serverId} label={server.label} online={server.connected} />
        )}
        {st && (
          <div className="flex flex-wrap items-stretch gap-2">
            {typeof st.tps === "number" && (
              <HeaderStat
                label={t("mc.stats.tps")}
                value={st.tps.toFixed(1)}
                valueClass={capacityTextClass[tpsTone(st.tps)]}
              >
                {server.loader !== "velocity" && (
                  <HeaderTpsSparkline instanceId={instanceId} serverId={serverId} />
                )}
              </HeaderStat>
            )}
            <HeaderStat
              label={t("mc.stats.players")}
              value={t("mc.chips.players", { online: st.playersOnline, max: st.playersMax })}
            />
            {typeof st.heapUsedMb === "number" && (
              <HeaderStat
                label={t("mc.stats.heap")}
                value={
                  typeof st.heapMaxMb === "number"
                    ? t("mc.stats.heapValue", { used: Math.round(st.heapUsedMb), max: Math.round(st.heapMaxMb) })
                    : t("mc.stats.heapValueNoMax", { used: Math.round(st.heapUsedMb) })
                }
              />
            )}
          </div>
        )}
      </div>

      <nav className="border-edge no-scrollbar flex gap-1 overflow-x-auto overflow-y-hidden border-b">
        {tabs.map((tab) => {
          const active = tab.exact ? path === tab.to : path.startsWith(tab.to);
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

/** One compact header stat: tiny uppercase label over the live value. */
function HeaderStat({
  label,
  value,
  valueClass,
  children,
}: {
  label: string;
  value: string;
  valueClass?: string;
  children?: ReactNode;
}) {
  return (
    <div className="bg-bg-subtle flex items-center gap-3 rounded-lg px-3 py-1.5">
      <div className="min-w-0">
        <p className="text-fg-subtle text-[10px] font-semibold tracking-wide uppercase">{label}</p>
        <p className={cn("text-[13px] leading-tight font-semibold tabular-nums", valueClass ?? "text-fg")}>
          {value}
        </p>
      </div>
      {children}
    </div>
  );
}

/** The last hour of TPS as a header-scale sparkline beside the live number. */
function HeaderTpsSparkline({ instanceId, serverId }: { instanceId: string; serverId: string }) {
  const resource = mcServerResource(instanceId, serverId);
  const points = useMetricSeries(`metrics:${resource}`, resource, "tps");
  if (points.length < 2) return null;
  return (
    <div className="w-[110px] shrink-0">
      <Sparkline points={points} height={36} />
    </div>
  );
}


/**
 * Start / Stop / Restart over the network's SSH access, shown by state:
 * a stopped server offers only Start; a running one offers Stop and
 * Restart. Each disruptive action confirms first.
 */
function PowerControls({
  instanceId,
  serverId,
  label,
  online,
}: {
  instanceId: string;
  serverId: string;
  label: string;
  online: boolean;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();
  const [confirm, setConfirm] = useState<null | "start" | "stop" | "restart">(null);

  const run = useMutation({
    mutationFn: async (action: "start" | "stop" | "restart") => {
      const path =
        action === "start"
          ? "/mc/{id}/servers/{serverId}/start"
          : action === "stop"
            ? "/mc/{id}/servers/{serverId}/stop"
            : "/mc/{id}/servers/{serverId}/restart";
      return unwrap(await api.POST(path, { params: { path: { id: instanceId, serverId } } }));
    },
    onSuccess: (res, action) => {
      setConfirm(null);
      toast("success", t(`mc.server.${action}Done`, { name: label }), res.message);
      void queryClient.invalidateQueries({ queryKey: mcOverviewKey(instanceId) });
    },
    onError: (err) => {
      setConfirm(null);
      toast("error", t("mc.server.powerFailed"), isApiError(err) ? err.message : t("common.unknownError"));
    },
  });

  // Start needs no confirmation; stop and restart drop players, so they do.
  const start = () => run.mutate("start");

  return (
    <div className="flex items-center gap-2">
      {online ? (
        <>
          <Button size="sm" variant="secondary" loading={run.isPending} onClick={() => setConfirm("restart")}>
            <RotateCw className="size-3.5" />
            {t("mc.server.restart")}
          </Button>
          <Button size="sm" variant="danger-ghost" loading={run.isPending} onClick={() => setConfirm("stop")}>
            <Square className="size-3.5" />
            {t("mc.server.stop")}
          </Button>
        </>
      ) : (
        <Button size="sm" variant="primary" loading={run.isPending} onClick={start}>
          <Play className="size-3.5" />
          {t("mc.server.start")}
        </Button>
      )}

      <ConfirmDialog
        open={confirm === "restart"}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={t("mc.server.restartTitle", { name: label })}
        body={t("mc.server.restartBody")}
        confirmLabel={t("mc.server.restart")}
        loading={run.isPending}
        onConfirm={() => run.mutate("restart")}
      />
      <ConfirmDialog
        open={confirm === "stop"}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={t("mc.server.stopTitle", { name: label })}
        body={t("mc.server.stopBody")}
        confirmLabel={t("mc.server.stop")}
        loading={run.isPending}
        onConfirm={() => run.mutate("stop")}
      />
    </div>
  );
}
