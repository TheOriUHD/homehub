import { useMemo, useState } from "react";
import { useMutation, useQueries, useQueryClient } from "@tanstack/react-query";
import { Link, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { AlertTriangle, ArrowUpRight, Boxes, Container, FileText, Play, RotateCcw, Server, Square, SquareTerminal } from "lucide-react";
import type { ReactNode } from "react";
import type { LucideIcon } from "lucide-react";
import { api, unwrap, isApiError, type DockerContainer, type Integration } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useLiveInvalidate } from "@/lib/live";
import { SYSTEMS_RES, statusTone, useIntegrations } from "@/lib/systems";
import { cn } from "@/lib/utils";
import { Card } from "@/ui/card";
import { Button } from "@/ui/button";
import { Badge, StatusDot } from "@/ui/badge";
import { Input } from "@/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { MiniStat } from "@/components/mini-stat";
import { SkeletonRows } from "@/ui/skeleton";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { useToast } from "@/ui/toast";
import { Tooltip } from "@/ui/tooltip";
import { containerStateLabel, containerStateTone, type DockerContainerAction } from "./containers";

const CONTAINER_STATES = ["running", "exited", "created", "paused", "restarting", "dead"] as const;

/** The Docker hosts the caller can see, in stable name order. */
function useDockerHosts() {
  const integrations = useIntegrations();
  const hosts = useMemo(
    () =>
      (integrations.data ?? [])
        .filter((row) => row.type === "docker" && row.enabled)
        .sort((a, b) => a.name.localeCompare(b.name)),
    [integrations.data],
  );
  return { integrations, hosts };
}

/** One containers query per host — cache keys match the per-host module. */
function useHostContainers(hosts: Integration[]) {
  return useQueries({
    queries: hosts.map((host) => ({
      queryKey: ["docker", host.id, "containers"],
      queryFn: async () =>
        unwrap(await api.GET("/docker/{id}/containers", { params: { path: { id: host.id } } })),
    })),
  });
}

/** HostLive keeps one host's containers fresh over its WS topic. */
function HostLive({ hostId }: { hostId: string }) {
  useLiveInvalidate(`integration:docker/${hostId}`, [["docker", hostId, "containers"]], ["containers-changed"]);
  return null;
}

/**
 * DockerHubPage is the unified Docker module at /docker: every visible host's
 * containers in one table, plus a hosts overview. Per-host drill-down
 * (images, volumes, networks, stacks) stays at /docker/$instanceId.
 */
export function DockerHubPage() {
  const { t } = useTranslation();
  const { can } = useAuth();
  const pathname = useRouterState({ select: (s) => s.location.pathname });

  const { integrations, hosts } = useDockerHosts();
  const containerResults = useHostContainers(hosts);
  useLiveInvalidate("integrations", [["integrations"]]);

  if (integrations.isLoading) return <PageLoader />;
  if (integrations.isError) {
    return (
      <Card>
        <ErrorState error={integrations.error} onRetry={() => void integrations.refetch()} />
      </Card>
    );
  }

  if (hosts.length === 0) {
    return (
      <Card>
        <EmptyState
          icon={Container}
          title={t("docker.unified.empty.title")}
          body={t("docker.unified.empty.body")}
          action={
            can(SYSTEMS_RES, "configure") ? (
              <Link to="/systems" className="text-accent text-[13px] font-medium hover:underline">
                {t("docker.unified.empty.action")} →
              </Link>
            ) : undefined
          }
        />
      </Card>
    );
  }

  const online = hosts.filter((h) => h.status === "connected").length;
  const containers = containerResults.flatMap((r) => r.data ?? []);
  const running = containers.filter((c) => c.state === "running").length;
  const stopped = containers.filter((c) => c.state === "exited").length;
  const trouble = containers.filter(
    (c) => c.state === "restarting" || c.state === "dead" || (c.status ?? "").toLowerCase().includes("unhealthy"),
  ).length;
  const tabs = [
    { to: "/docker", label: t("docker.unified.tabs.containers"), exact: true },
    { to: "/docker/hosts", label: t("docker.unified.tabs.hosts") },
  ];

  return (
    <div className="space-y-5">
      {hosts.map((host) => (
        <HostLive key={host.id} hostId={host.id} />
      ))}
      <div className="flex flex-wrap items-center gap-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
          <Container className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="text-fg text-xl font-semibold tracking-tight">{t("systems.types.docker")}</h1>
          <p className="text-fg-muted text-sm">
            {t("docker.unified.hostsSummary", { online, total: hosts.length })}
          </p>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <MiniStat
          icon={Server}
          label={t("docker.unified.stats.hosts")}
          value={`${online}/${hosts.length}`}
          tone={online === hosts.length ? "success" : "warning"}
        />
        <MiniStat icon={Play} label={t("docker.unified.stats.running")} value={running} />
        <MiniStat icon={Square} label={t("docker.unified.stats.stopped")} value={stopped} />
        <MiniStat
          icon={AlertTriangle}
          label={t("docker.unified.stats.problems")}
          value={trouble}
          tone={trouble > 0 ? "danger" : undefined}
        />
      </div>

      <nav className="border-edge flex gap-1 overflow-x-auto overflow-y-hidden border-b">
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

// ---- containers across all hosts -------------------------------------------

type MergedRow = { host: Integration; container: DockerContainer };

export function DockerAllContainersPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { can } = useAuth();
  const toast = useToast();
  const queryClient = useQueryClient();

  const { hosts } = useDockerHosts();
  const results = useHostContainers(hosts);

  const [filter, setFilter] = useState("");
  const [hostFilter, setHostFilter] = useState("all");
  const [stateFilter, setStateFilter] = useState("all");

  const loading = hosts.length > 0 && results.every((r) => r.isLoading);
  const failedHosts = hosts.filter((_, i) => results[i]?.isError);

  // Fleet counters for the tile row. Container sums settle once every
  // per-host list has answered; partial sums would flicker upward.
  const hostsOnline = hosts.filter((h) => h.status === "connected").length;
  const settled = results.every((r) => !r.isPending);
  const totalContainers = results.reduce((sum, r) => sum + (r.data?.length ?? 0), 0);
  const runningContainers = results.reduce(
    (sum, r) => sum + (r.data?.filter((c) => c.state === "running").length ?? 0),
    0,
  );
  const stoppedContainers = results.reduce(
    (sum, r) => sum + (r.data?.filter((c) => c.state === "exited" || c.state === "dead").length ?? 0),
    0,
  );
  const unreachableCount = hosts.filter(
    (h, i) => h.status === "unreachable" || results[i]?.isError,
  ).length;

  const rows = useMemo(() => {
    const merged: MergedRow[] = [];
    hosts.forEach((host, i) => {
      for (const container of results[i]?.data ?? []) merged.push({ host, container });
    });
    merged.sort(
      (a, b) =>
        a.host.name.localeCompare(b.host.name) || a.container.name.localeCompare(b.container.name),
    );
    const needle = filter.trim().toLowerCase();
    return merged.filter(({ host, container }) => {
      if (hostFilter !== "all" && host.id !== hostFilter) return false;
      if (stateFilter !== "all" && container.state !== stateFilter) return false;
      if (!needle) return true;
      return (
        container.name.toLowerCase().includes(needle) ||
        container.image.toLowerCase().includes(needle) ||
        (container.composeProject ?? "").toLowerCase().includes(needle) ||
        host.name.toLowerCase().includes(needle)
      );
    });
  }, [hosts, results, filter, hostFilter, stateFilter]);

  const action = useMutation({
    mutationFn: async (vars: { hostId: string; name: string; action: DockerContainerAction }) =>
      unwrap(
        await api.POST("/docker/{id}/containers/{name}/action", {
          params: { path: { id: vars.hostId, name: vars.name } },
          body: { action: vars.action },
        }),
      ),
    onSuccess: (_data, vars) => {
      toast("success", t("docker.actions.done", { action: t(`docker.actions.${vars.action}`) }));
      void queryClient.invalidateQueries({ queryKey: ["docker", vars.hostId, "containers"] });
    },
    onError: (err, vars) =>
      toast(
        "error",
        t("docker.actions.failedFor", { name: vars.name }),
        isApiError(err) ? err.message : t("common.unknownError"),
      ),
  });

  if (loading) return <SkeletonRows rows={6} />;

  return (
    <div className="space-y-4">
      <div
        className={cn(
          "grid gap-4 sm:grid-cols-2",
          unreachableCount > 0 ? "lg:grid-cols-4" : "lg:grid-cols-3",
        )}
      >
        <StatTile
          icon={Server}
          label={t("docker.unified.stats.hostsOnline")}
          value={
            <span className="flex items-baseline gap-1.5">
              {hostsOnline}/{hosts.length}
              <StatusDot
                tone={hostsOnline === hosts.length ? "success" : hostsOnline > 0 ? "warning" : "danger"}
              />
            </span>
          }
        />
        <StatTile
          icon={Play}
          label={t("docker.unified.stats.running")}
          value={
            settled ? (
              <span>
                {runningContainers}
                <span className="text-fg-muted text-sm font-medium">/{totalContainers}</span>
              </span>
            ) : (
              "—"
            )
          }
        />
        <StatTile
          icon={Square}
          label={t("docker.unified.stats.stopped")}
          value={settled ? stoppedContainers.toLocaleString() : "—"}
        />
        {unreachableCount > 0 && (
          <StatTile
            icon={AlertTriangle}
            tone="danger"
            label={t("docker.unified.stats.unreachable")}
            value={unreachableCount.toLocaleString()}
          />
        )}
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <Input
          placeholder={t("docker.containers.searchPlaceholder")}
          className="max-w-72"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
        <Select value={hostFilter} onValueChange={setHostFilter}>
          <SelectTrigger className="w-44">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t("docker.unified.allHosts")}</SelectItem>
            {hosts.map((host) => (
              <SelectItem key={host.id} value={host.id}>
                {host.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={stateFilter} onValueChange={setStateFilter}>
          <SelectTrigger className="w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t("docker.unified.allStates")}</SelectItem>
            {CONTAINER_STATES.map((state) => (
              <SelectItem key={state} value={state}>
                {t(`docker.containers.state.${state}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {failedHosts.length > 0 && (
        <div
          className="bg-warning-soft text-warning-soft-fg flex items-start gap-2 rounded-lg px-3 py-2 text-[13px]"
          role="alert"
        >
          <AlertTriangle className="mt-0.5 size-4 shrink-0" />
          {t("docker.unified.unreachableHosts", {
            hosts: failedHosts.map((h) => h.name).join(", "),
          })}
        </div>
      )}

      <Card>
        {rows.length === 0 ? (
          <EmptyState
            icon={Boxes}
            title={t("docker.containers.empty.title")}
            body={t("docker.unified.emptyBody")}
          />
        ) : (
          <Table>
            <THead>
              <tr>
                <TH>{t("docker.unified.columns.host")}</TH>
                <TH>{t("docker.containers.columns.container")}</TH>
                <TH>{t("docker.containers.columns.state")}</TH>
                <TH className="w-40">{t("common.actions")}</TH>
              </tr>
            </THead>
            <TBody>
              {rows.map(({ host, container }) => {
                const canControl = can(`docker/${host.id}/container/${container.name}`, "control");
                const busy =
                  action.isPending &&
                  action.variables?.hostId === host.id &&
                  action.variables?.name === container.name;
                const running = container.state === "running";
                return (
                  <TR
                    key={`${host.id}/${container.id}`}
                    interactive
                    onClick={() =>
                      void navigate({
                        to: "/docker/$instanceId/containers/$name",
                        params: { instanceId: host.id, name: container.name },
                      })
                    }
                  >
                    <TD className="whitespace-nowrap">
                      <span className="text-fg flex items-center gap-2 font-medium">
                        <StatusDot tone={statusTone[host.status]} />
                        {host.name}
                      </span>
                    </TD>
                    <TD>
                      <div className="min-w-0">
                        <p className="text-fg flex items-center gap-1.5 truncate font-medium">
                          {container.name}
                          {container.composeProject && (
                            <Badge tone="accent">{container.composeProject}</Badge>
                          )}
                        </p>
                        <p className="text-fg-subtle truncate text-xs">{container.image}</p>
                      </div>
                    </TD>
                    <TD>
                      <Badge tone={containerStateTone[container.state as keyof typeof containerStateTone] ?? "neutral"}>
                        {containerStateLabel(t, container.state)}
                      </Badge>
                    </TD>
                    <TD onClick={(e) => e.stopPropagation()}>
                      <div className="flex items-center gap-0.5">
                        {canControl && !running && (
                          <Tooltip label={t("docker.actions.start")}>
                            <Button
                              variant="ghost"
                              size="icon"
                              aria-label={t("docker.actions.start")}
                              loading={busy}
                              onClick={() =>
                                action.mutate({ hostId: host.id, name: container.name, action: "start" })
                              }
                            >
                              <Play className="size-3.5" />
                            </Button>
                          </Tooltip>
                        )}
                        {canControl && running && (
                          <>
                            <Tooltip label={t("docker.actions.stop")}>
                              <Button
                                variant="ghost"
                                size="icon"
                                aria-label={t("docker.actions.stop")}
                                loading={busy}
                                onClick={() =>
                                  action.mutate({ hostId: host.id, name: container.name, action: "stop" })
                                }
                              >
                                <Square className="size-3.5" />
                              </Button>
                            </Tooltip>
                            <Tooltip label={t("docker.actions.restart")}>
                              <Button
                                variant="ghost"
                                size="icon"
                                aria-label={t("docker.actions.restart")}
                                loading={busy}
                                onClick={() =>
                                  action.mutate({ hostId: host.id, name: container.name, action: "restart" })
                                }
                              >
                                <RotateCcw className="size-3.5" />
                              </Button>
                            </Tooltip>
                          </>
                        )}
                        <Tooltip label={t("docker.container.logs")}>
                          <Link
                            to="/docker/$instanceId/containers/$name"
                            params={{ instanceId: host.id, name: container.name }}
                            search={{ tab: "logs" }}
                            aria-label={t("docker.container.logs")}
                            className="interactive-muted flex size-7 items-center justify-center"
                          >
                            <FileText className="size-3.5" />
                          </Link>
                        </Tooltip>
                        <Tooltip label={t("docker.container.shell")}>
                          <Link
                            to="/docker/$instanceId/containers/$name"
                            params={{ instanceId: host.id, name: container.name }}
                            search={{ tab: "shell" }}
                            aria-label={t("docker.container.shell")}
                            className="interactive-muted flex size-7 items-center justify-center"
                          >
                            <SquareTerminal className="size-3.5" />
                          </Link>
                        </Tooltip>
                      </div>
                    </TD>
                  </TR>
                );
              })}
            </TBody>
          </Table>
        )}
      </Card>
    </div>
  );
}

// ---- hosts overview --------------------------------------------------------

export function DockerHostsPage() {
  const { hosts } = useDockerHosts();
  const results = useHostContainers(hosts);

  return (
    <div className="grid items-start gap-4 md:grid-cols-2">
      {hosts.map((host, i) => (
        <HostCard
          key={host.id}
          host={host}
          containers={results[i]?.data}
          failed={Boolean(results[i]?.isError)}
        />
      ))}
    </div>
  );
}

/**
 * One host's dashboard card in the fleet-card language: icon tile, name with
 * a status dot, the endpoint it talks to, a running/total bar with per-state
 * chips, and the quick action into the per-host module. Unreachable hosts
 * stay on the board — dimmed, with the reported error.
 */
function HostCard({
  host,
  containers,
  failed,
}: {
  host: Integration;
  containers: DockerContainer[] | undefined;
  failed: boolean;
}) {
  const { t } = useTranslation();
  const offline = host.status !== "connected";
  const list = containers ?? [];
  const running = list.filter((c) => c.state === "running").length;

  // The endpoint this host is reached over, straight from the stored config —
  // a TCP address, or the local socket.
  const endpoint =
    typeof host.config.host === "string" && host.config.host
      ? host.config.host
      : host.config.transport === "socket"
        ? t("docker.unified.socket")
        : null;

  // Non-running states as toned chips; the bar already tells the running story.
  const stateCounts = CONTAINER_STATES.filter((s) => s !== "running")
    .map((state) => [state, list.filter((c) => c.state === state).length] as const)
    .filter(([, n]) => n > 0);

  return (
    <Card className={cn("flex flex-col", offline && "opacity-75")}>
      <div className="flex items-start gap-3 p-4 pb-0">
        <span className="bg-accent-soft text-accent-soft-fg flex size-9 shrink-0 items-center justify-center rounded-lg">
          <Container className="size-4.5" aria-hidden />
        </span>
        <div className="min-w-0 flex-1">
          <p className="text-fg flex items-center gap-2 text-sm font-semibold">
            <Link
              to="/docker/$instanceId"
              params={{ instanceId: host.id }}
              className="hover:text-accent truncate transition-colors"
            >
              {host.name}
            </Link>
            <StatusDot tone={statusTone[host.status]} pulse={offline} />
          </p>
          <p className="text-fg-muted truncate text-xs">
            {t("systems.types.docker")}
            {endpoint && <span className="text-fg-subtle font-mono"> · {endpoint}</span>}
          </p>
        </div>
      </div>

      {offline ? (
        <div className="p-4">
          <div
            className="bg-danger-soft text-danger-soft-fg flex items-start gap-2.5 rounded-lg px-3.5 py-2.5 text-[13px]"
            role="alert"
          >
            <AlertTriangle className="mt-0.5 size-4 shrink-0" aria-hidden />
            <span>
              {t(`integrations.status.${host.status}`)}
              {host.statusMessage && ` — ${host.statusMessage}`}
            </span>
          </div>
        </div>
      ) : failed ? (
        <div className="p-4">
          <div
            className="bg-warning-soft text-warning-soft-fg flex items-start gap-2.5 rounded-lg px-3.5 py-2.5 text-[13px]"
            role="alert"
          >
            <AlertTriangle className="mt-0.5 size-4 shrink-0" aria-hidden />
            {t("docker.unified.countUnavailable")}
          </div>
        </div>
      ) : (
        <div className="px-4 pt-3.5">
          <div className="flex items-baseline justify-between gap-2">
            <p className="text-fg-subtle text-[11px] font-semibold tracking-wide uppercase">
              {t("docker.unified.tabs.containers")}
            </p>
            <p className="text-fg text-sm font-semibold tabular-nums">
              {containers ? (
                <>
                  {running}
                  <span className="text-fg-muted font-medium">/{list.length}</span>
                </>
              ) : (
                "—"
              )}
            </p>
          </div>
          <div className="bg-bg-subtle mt-1.5 h-1.5 w-full overflow-hidden rounded-full">
            <div
              className="bg-accent h-full rounded-full transition-[width]"
              style={{ width: `${list.length > 0 ? (running / list.length) * 100 : 0}%` }}
            />
          </div>
          {stateCounts.length > 0 && (
            <div className="mt-3 flex flex-wrap gap-1.5">
              {stateCounts.map(([state, n]) => (
                <Badge key={state} tone={containerStateTone[state] ?? "neutral"}>
                  {t("docker.unified.stateCount", { n, state: containerStateLabel(t, state) })}
                </Badge>
              ))}
            </div>
          )}
        </div>
      )}

      <div className="border-edge mt-4 flex items-center gap-1 border-t px-2 py-1.5">
        <Link
          to="/docker/$instanceId"
          params={{ instanceId: host.id }}
          className="text-fg-muted hover:text-fg hover:bg-surface-hover flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-[13px] font-medium transition-colors"
        >
          <ArrowUpRight className="size-3.5" aria-hidden />
          {t("docker.unified.open")}
        </Link>
      </div>
    </Card>
  );
}

function StatTile({
  icon: Icon,
  label,
  value,
  tone = "accent",
}: {
  icon: LucideIcon;
  label: string;
  value: ReactNode;
  tone?: "accent" | "danger";
}) {
  return (
    <Card className="flex items-center gap-3.5 p-4">
      <span
        className={cn(
          "flex size-9 shrink-0 items-center justify-center rounded-lg",
          tone === "danger" ? "bg-danger-soft text-danger-soft-fg" : "bg-accent-soft text-accent-soft-fg",
        )}
      >
        <Icon className="size-4" aria-hidden />
      </span>
      <div className="min-w-0 flex-1">
        <p className="text-fg-subtle text-[11px] font-semibold tracking-wide uppercase">{label}</p>
        <div className="text-fg mt-0.5 truncate text-lg font-semibold tracking-tight">{value}</div>
      </div>
    </Card>
  );
}
