import { lazy, Suspense, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  ArrowLeft,
  Container,
  Eye,
  EyeOff,
  Pause,
  Play,
  RefreshCw,
  RotateCcw,
  Square,
  Trash2,
  Zap,
} from "lucide-react";
import {
  api,
  unwrap,
  isApiError,
  type DockerContainerDetail,
  type TermSession,
} from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { SHELL } from "@/lib/perms";
import { useLiveInvalidate } from "@/lib/live";
import { useMetricSeries } from "@/lib/metrics";
import { cn, formatRelative } from "@/lib/utils";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { Button } from "@/ui/button";
import { Badge } from "@/ui/badge";
import { Dialog, DialogContent } from "@/ui/dialog";
import { ConfirmDialog } from "@/ui/confirm";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { Checkbox, PageLoader } from "@/ui/misc";
import { ErrorState } from "@/ui/states";
import { useToast } from "@/ui/toast";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { TimeSeriesChart } from "@/components/time-series-chart";
import { containerStateLabel, containerStateTone, formatPorts, type DockerContainerAction } from "./containers";

// The terminal pulls in xterm.js — loaded only when a session opens.
const TerminalPane = lazy(() =>
  import("@/components/terminal").then((m) => ({ default: m.TerminalPane })),
);

type TabKey = "overview" | "inspect" | "logs" | "shell";

export function DockerContainerDetailPage() {
  const { t } = useTranslation();
  const { instanceId, name } = useParams({ from: "/shell/docker/$instanceId/containers/$name" });
  const { tab: initialTab } = useSearch({ from: "/shell/docker/$instanceId/containers/$name" });
  const navigate = useNavigate();
  const { can } = useAuth();
  const queryClient = useQueryClient();
  const toast = useToast();

  const containerKeys = [["docker", instanceId, "container", name], ["docker", instanceId, "containers"]];
  const detail = useQuery({
    queryKey: ["docker", instanceId, "container", name],
    queryFn: async () =>
      unwrap(await api.GET("/docker/{id}/containers/{name}", { params: { path: { id: instanceId, name } } })),
  });
  useLiveInvalidate(`integration:docker/${instanceId}`, containerKeys, ["containers-changed"]);

  const [tab, setTab] = useState<TabKey>(initialTab ?? "overview");
  const containerRes = `docker/${instanceId}/container/${name}`;
  const canControl = can(containerRes, "control");
  const canDelete = can(containerRes, "delete");

  const action = useMutation({
    mutationFn: async (a: DockerContainerAction) =>
      unwrap(
        await api.POST("/docker/{id}/containers/{name}/action", {
          params: { path: { id: instanceId, name } },
          body: { action: a },
        }),
      ),
    onSuccess: (_data, a) => {
      toast("success", t("docker.actions.done", { action: t(`docker.actions.${a}`) }));
      setConfirmKill(false);
      void queryClient.invalidateQueries({ queryKey: ["docker", instanceId] });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const [confirmKill, setConfirmKill] = useState(false);
  const [recreateOpen, setRecreateOpen] = useState(false);
  const [removeOpen, setRemoveOpen] = useState(false);
  const [removeForce, setRemoveForce] = useState(false);
  const [removeVolumes, setRemoveVolumes] = useState(false);

  const remove = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.DELETE("/docker/{id}/containers/{name}", {
          params: { path: { id: instanceId, name }, query: { force: removeForce, volumes: removeVolumes } },
        }),
      ),
    onSuccess: () => {
      toast("success", t("docker.container.removed", { name }));
      void queryClient.invalidateQueries({ queryKey: ["docker", instanceId, "containers"] });
      void navigate({ to: "/docker/$instanceId", params: { instanceId } });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  if (detail.isLoading) return <PageLoader />;
  if (detail.isError) {
    return (
      <Card>
        <ErrorState error={detail.error} onRetry={() => void detail.refetch()} />
      </Card>
    );
  }
  const data = detail.data!;
  const container = data.container;
  const state = container.state;
  const killable = state === "running" || state === "paused" || state === "restarting";

  const tabs: { key: TabKey; label: string; show: boolean }[] = [
    { key: "overview", label: t("docker.container.overview"), show: true },
    { key: "inspect", label: t("docker.container.inspect"), show: true },
    { key: "logs", label: t("docker.container.logs"), show: true },
    { key: "shell", label: t("docker.container.shell"), show: can(containerRes, SHELL) },
  ];

  return (
    <div className="space-y-5">
      <div>
        <Link
          to="/docker/$instanceId"
          params={{ instanceId }}
          className="text-fg-subtle hover:text-fg mb-3 inline-flex items-center gap-1.5 text-[13px] transition-colors"
        >
          <ArrowLeft className="size-3.5" />
          {t("docker.tabs.containers")}
        </Link>
        <div className="flex flex-wrap items-center gap-3">
          <span className="bg-accent-soft text-accent-soft-fg flex size-10 shrink-0 items-center justify-center rounded-xl">
            <Container className="size-5" />
          </span>
          <div className="min-w-0 flex-1">
            <h1 className="text-fg flex flex-wrap items-center gap-2 text-xl font-semibold tracking-tight">
              {container.name}
              <Badge tone={containerStateTone[state as keyof typeof containerStateTone] ?? "neutral"}>
                {containerStateLabel(t, state)}
              </Badge>
              {container.composeProject && <Badge tone="accent">{container.composeProject}</Badge>}
            </h1>
            <p className="text-fg-muted truncate text-sm">{container.image}</p>
          </div>

          {canControl && (
            <div className="flex flex-wrap gap-1.5">
              {state === "running" ? (
                <>
                  <Button variant="secondary" size="sm" loading={action.isPending} onClick={() => action.mutate("stop")}>
                    <Square className="size-3.5" /> {t("docker.actions.stop")}
                  </Button>
                  <Button variant="secondary" size="sm" loading={action.isPending} onClick={() => action.mutate("restart")}>
                    <RotateCcw className="size-3.5" /> {t("docker.actions.restart")}
                  </Button>
                  <Button variant="secondary" size="sm" loading={action.isPending} onClick={() => action.mutate("pause")}>
                    <Pause className="size-3.5" /> {t("docker.actions.pause")}
                  </Button>
                </>
              ) : state === "paused" ? (
                <Button variant="primary" size="sm" loading={action.isPending} onClick={() => action.mutate("unpause")}>
                  <Play className="size-3.5" /> {t("docker.actions.unpause")}
                </Button>
              ) : state !== "restarting" ? (
                <Button variant="primary" size="sm" loading={action.isPending} onClick={() => action.mutate("start")}>
                  <Play className="size-3.5" /> {t("docker.actions.start")}
                </Button>
              ) : null}
              {killable && (
                <Button variant="danger-ghost" size="sm" onClick={() => setConfirmKill(true)}>
                  <Zap className="size-3.5" /> {t("docker.actions.kill")}
                </Button>
              )}
              <Button variant="secondary" size="sm" onClick={() => setRecreateOpen(true)}>
                <RefreshCw className="size-3.5" /> {t("docker.actions.recreate")}
              </Button>
              {canDelete && (
                <Button variant="danger-ghost" size="sm" onClick={() => setRemoveOpen(true)}>
                  <Trash2 className="size-3.5" /> {t("docker.actions.remove")}
                </Button>
              )}
            </div>
          )}
        </div>
      </div>

      <nav className="border-edge flex gap-1 overflow-x-auto overflow-y-hidden border-b">
        {tabs.filter((entry) => entry.show).map((entry) => (
          <button
            key={entry.key}
            onClick={() => setTab(entry.key)}
            className={cn(
              "-mb-px shrink-0 cursor-pointer border-b-2 px-3.5 py-2 text-[13px] font-medium transition-colors",
              tab === entry.key ? "border-accent text-fg" : "text-fg-muted hover:text-fg border-transparent",
            )}
          >
            {entry.label}
          </button>
        ))}
      </nav>

      {tab === "overview" && <OverviewTab instanceId={instanceId} name={name} detail={data} />}
      {tab === "inspect" && <InspectTab detail={data} />}
      {tab === "logs" && <LogsTab instanceId={instanceId} name={name} />}
      {tab === "shell" && can(containerRes, SHELL) && <ShellTab instanceId={instanceId} name={name} />}

      <ConfirmDialog
        open={confirmKill}
        onOpenChange={setConfirmKill}
        title={t("docker.actions.killConfirmTitle", { name: container.name })}
        body={t("docker.actions.killConfirmBody")}
        confirmLabel={t("docker.actions.kill")}
        loading={action.isPending}
        onConfirm={() => action.mutate("kill")}
      />
      {recreateOpen && (
        <RecreateDialog instanceId={instanceId} name={name} onClose={() => setRecreateOpen(false)} />
      )}
      <Dialog open={removeOpen} onOpenChange={setRemoveOpen}>
        <DialogContent
          title={t("docker.actions.removeConfirmTitle", { name: container.name })}
          description={t("docker.actions.removeConfirmBody")}
          footer={
            <>
              <Button variant="ghost" onClick={() => setRemoveOpen(false)}>
                {t("common.cancel")}
              </Button>
              <Button variant="danger" loading={remove.isPending} onClick={() => remove.mutate()}>
                {t("docker.actions.remove")}
              </Button>
            </>
          }
        >
          <div className="space-y-3">
            <Checkbox
              checked={removeForce}
              onCheckedChange={setRemoveForce}
              label={t("docker.actions.removeForce")}
              hint={t("docker.actions.removeForceHint")}
            />
            <Checkbox
              checked={removeVolumes}
              onCheckedChange={setRemoveVolumes}
              label={t("docker.actions.removeVolumes")}
              hint={t("docker.actions.removeVolumesHint")}
            />
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}

// ---- overview --------------------------------------------------------------

function OverviewTab({
  instanceId,
  name,
  detail,
}: {
  instanceId: string;
  name: string;
  detail: DockerContainerDetail;
}) {
  const { t } = useTranslation();
  const resource = `docker/${instanceId}/container/${name}`;
  const topic = `metrics:docker/${instanceId}`;
  const cpu = useMetricSeries(topic, resource, "cpu");
  const memUsed = useMetricSeries(topic, resource, "mem_used");
  const memTotal = useMetricSeries(topic, resource, "mem_total");

  const container = detail.container;
  const running = container.state === "running";

  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <MiniStat label={t("docker.container.statStatus")} value={container.status || "—"} />
        <MiniStat
          label={running ? t("docker.container.statStarted") : t("docker.container.statExitCode")}
          value={
            running
              ? formatRelative(detail.startedAt)
              : detail.exitCode != null
                ? String(detail.exitCode)
                : "—"
          }
        />
        <MiniStat label={t("docker.container.statRestartPolicy")} value={detail.restartPolicy || "—"} />
        <MiniStat label={t("docker.container.statPorts")} value={formatPorts(container.ports)} />
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        <Card className="p-4">
          <p className="text-fg-subtle mb-1 text-xs font-semibold uppercase">{t("docker.container.chartCpu")}</p>
          <TimeSeriesChart points={cpu} kind="percent" height={150} />
        </Card>
        <Card className="p-4">
          <p className="text-fg-subtle mb-1 text-xs font-semibold uppercase">{t("docker.container.chartMem")}</p>
          <TimeSeriesChart points={memUsed} capacity={memTotal} kind="bytes" height={150} />
        </Card>
      </div>
    </div>
  );
}

function MiniStat({ label, value }: { label: string; value: string }) {
  return (
    <Card className="p-4">
      <p className="text-fg-subtle text-xs font-semibold tracking-wide uppercase">{label}</p>
      <p className="text-fg mt-1 truncate text-lg font-semibold">{value}</p>
    </Card>
  );
}

// ---- inspect ---------------------------------------------------------------

function InspectTab({ detail }: { detail: DockerContainerDetail }) {
  const { t } = useTranslation();
  const labels = Object.entries(detail.labels).sort(([a], [b]) => a.localeCompare(b));

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader title={t("docker.container.command")} />
        <CardBody className="space-y-3">
          <InspectRow label={t("docker.container.cmd")} value={detail.cmd || "—"} mono />
          <InspectRow label={t("docker.container.entrypoint")} value={detail.entrypoint || "—"} mono />
          <InspectRow label={t("docker.container.statRestartPolicy")} value={detail.restartPolicy || "—"} />
        </CardBody>
      </Card>

      <Card>
        <CardHeader title={t("docker.container.env")} description={t("docker.container.envIntro")} />
        <CardBody className="px-0 py-0">
          {detail.env.length === 0 ? (
            <p className="text-fg-subtle px-5 py-8 text-center text-[13px]">{t("docker.container.envEmpty")}</p>
          ) : (
            <ul>
              {detail.env.map((entry, i) => (
                <EnvRow key={i} entry={entry} />
              ))}
            </ul>
          )}
        </CardBody>
      </Card>

      <Card>
        <CardHeader title={t("docker.container.mounts")} />
        <CardBody className="px-0 py-0">
          {detail.mounts.length === 0 ? (
            <p className="text-fg-subtle px-5 py-8 text-center text-[13px]">{t("docker.container.mountsEmpty")}</p>
          ) : (
            <Table>
              <THead>
                <tr>
                  <TH>{t("docker.container.mountType")}</TH>
                  <TH className="hidden sm:table-cell">{t("docker.container.mountSource")}</TH>
                  <TH>{t("docker.container.mountDestination")}</TH>
                  <TH>{t("docker.container.mountMode")}</TH>
                </tr>
              </THead>
              <TBody>
                {detail.mounts.map((mount, i) => (
                  <TR key={i}>
                    <TD className="text-fg-muted">{mount.type}</TD>
                    <TD className="text-fg-muted hidden max-w-56 truncate font-mono text-xs sm:table-cell">
                      {mount.source || "—"}
                    </TD>
                    <TD className="text-fg max-w-56 truncate font-mono text-xs">{mount.destination}</TD>
                    <TD>
                      <Badge tone={mount.rw ? "neutral" : "warning"}>
                        {mount.rw ? t("docker.container.mountRw") : t("docker.container.mountRo")}
                      </Badge>
                    </TD>
                  </TR>
                ))}
              </TBody>
            </Table>
          )}
        </CardBody>
      </Card>

      <Card>
        <CardHeader title={t("docker.container.networks")} />
        <CardBody className="px-0 py-0">
          {detail.networks.length === 0 ? (
            <p className="text-fg-subtle px-5 py-8 text-center text-[13px]">{t("docker.container.networksEmpty")}</p>
          ) : (
            <ul>
              {detail.networks.map((network) => (
                <li key={network.name} className="border-edge flex items-center gap-3 border-b px-5 py-3 last:border-b-0">
                  <span className="text-fg flex-1 text-sm font-medium">{network.name}</span>
                  <code className="text-fg-muted font-mono text-xs">{network.ipAddress || "—"}</code>
                </li>
              ))}
            </ul>
          )}
        </CardBody>
      </Card>

      <Card>
        <CardHeader title={t("docker.container.labels")} />
        <CardBody className="px-0 py-0">
          {labels.length === 0 ? (
            <p className="text-fg-subtle px-5 py-8 text-center text-[13px]">{t("docker.container.labelsEmpty")}</p>
          ) : (
            <Table>
              <THead>
                <tr>
                  <TH className="w-64">{t("docker.container.labelKey")}</TH>
                  <TH>{t("docker.container.labelValue")}</TH>
                </tr>
              </THead>
              <TBody>
                {labels.map(([key, value]) => (
                  <TR key={key}>
                    <TD>
                      <code className="text-fg font-mono text-xs break-all">{key}</code>
                    </TD>
                    <TD>
                      <code className="text-fg-muted font-mono text-xs break-all">{value || "—"}</code>
                    </TD>
                  </TR>
                ))}
              </TBody>
            </Table>
          )}
        </CardBody>
      </Card>
    </div>
  );
}

function InspectRow({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1">
      <span className="text-fg-subtle w-32 shrink-0 text-xs font-semibold tracking-wide uppercase">{label}</span>
      <span className={cn("text-fg min-w-0 flex-1 text-sm break-all", mono && "font-mono text-xs")}>{value}</span>
    </div>
  );
}

/** EnvRow keeps values masked until deliberately revealed — env is where secrets live. */
function EnvRow({ entry }: { entry: string }) {
  const { t } = useTranslation();
  const [revealed, setRevealed] = useState(false);
  const eq = entry.indexOf("=");
  const key = eq === -1 ? entry : entry.slice(0, eq);
  const value = eq === -1 ? "" : entry.slice(eq + 1);

  return (
    <li className="border-edge flex items-center gap-3 border-b px-5 py-2.5 last:border-b-0">
      <code className="text-fg w-56 shrink-0 truncate font-mono text-xs font-semibold">{key}</code>
      <code className="text-fg-muted min-w-0 flex-1 truncate font-mono text-xs">
        {revealed ? value || "—" : "••••••••"}
      </code>
      <button
        type="button"
        onClick={() => setRevealed((v) => !v)}
        aria-label={revealed ? t("common.hide") : t("common.show")}
        className="interactive-muted flex size-7 shrink-0 cursor-pointer items-center justify-center"
      >
        {revealed ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
      </button>
    </li>
  );
}

// ---- logs ------------------------------------------------------------------

function wsUrl(path: string): string {
  return `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}${path}`;
}

function LogsTab({ instanceId, name }: { instanceId: string; name: string }) {
  const { t } = useTranslation();
  const toast = useToast();
  const [tail, setTail] = useState("500");
  const [session, setSession] = useState<TermSession | null>(null);

  const open = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/docker/{id}/containers/{name}/logs", {
          params: { path: { id: instanceId, name } },
          body: { tail: Number(tail) || 500 },
        }),
      ),
    onSuccess: setSession,
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  if (session) {
    return (
      <div className="border-edge overflow-hidden rounded-lg border" style={{ height: 520 }}>
        <Suspense fallback={<PageLoader className="h-full" />}>
          <TerminalPane
            wsUrl={wsUrl(session.wsPath)}
            renew={async () => {
              // Session tickets are single-use — reconnect mints a fresh one.
              const fresh = await open.mutateAsync();
              return wsUrl(fresh.wsPath);
            }}
            className="h-full"
            onExit={() => setSession(null)}
          />
        </Suspense>
      </div>
    );
  }

  return (
    <Card className="max-w-xl">
      <CardHeader title={t("docker.container.logs")} description={t("docker.container.logsIntro")} />
      <CardBody className="space-y-4">
        <Field label={t("docker.container.logsTail")} hint={t("docker.container.logsTailHint")}>
          {(p) => <Input {...p} type="number" className="max-w-32" value={tail} onChange={(e) => setTail(e.target.value)} />}
        </Field>
        <div className="flex justify-end">
          <Button variant="primary" loading={open.isPending} onClick={() => open.mutate()}>
            {t("docker.container.logsOpen")}
          </Button>
        </div>
      </CardBody>
    </Card>
  );
}

// ---- shell -----------------------------------------------------------------

function ShellTab({ instanceId, name }: { instanceId: string; name: string }) {
  const { t } = useTranslation();
  const toast = useToast();
  const [command, setCommand] = useState("");
  const [session, setSession] = useState<TermSession | null>(null);

  const open = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/docker/{id}/containers/{name}/exec", {
          params: { path: { id: instanceId, name } },
          body: command.trim() ? { command: command.trim() } : {},
        }),
      ),
    onSuccess: setSession,
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  if (session) {
    return (
      <div className="border-edge overflow-hidden rounded-lg border" style={{ height: 520 }}>
        <Suspense fallback={<PageLoader className="h-full" />}>
          <TerminalPane
            wsUrl={wsUrl(session.wsPath)}
            renew={async () => {
              // Session tickets are single-use — reconnect mints a fresh one.
              const fresh = await open.mutateAsync();
              return wsUrl(fresh.wsPath);
            }}
            className="h-full"
            onExit={() => setSession(null)}
          />
        </Suspense>
      </div>
    );
  }

  return (
    <Card className="max-w-xl">
      <CardHeader title={t("docker.container.shell")} description={t("docker.container.shellIntro")} />
      <CardBody className="space-y-4">
        <Field label={t("docker.container.shellCommand")} hint={t("docker.container.shellCommandHint")} optional={t("common.optional")}>
          {(p) => (
            <Input {...p} className="font-mono text-[13px]" placeholder="bash" value={command} onChange={(e) => setCommand(e.target.value)} />
          )}
        </Field>
        <div className="flex justify-end">
          <Button variant="primary" loading={open.isPending} onClick={() => open.mutate()}>
            {t("docker.container.shellOpen")}
          </Button>
        </div>
      </CardBody>
    </Card>
  );
}

// ---- recreate --------------------------------------------------------------

function RecreateDialog({ instanceId, name, onClose }: { instanceId: string; name: string; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const [pull, setPull] = useState(true);

  const recreate = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/docker/{id}/containers/{name}/recreate", {
          params: { path: { id: instanceId, name } },
          body: { pull },
        }),
      ),
    onSuccess: () => {
      toast("success", t("docker.actions.queuedTitle"), t("docker.actions.queuedBody"));
      onClose();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={t("docker.actions.recreateTitle", { name })}
        description={t("docker.actions.recreateBody")}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button variant="primary" loading={recreate.isPending} onClick={() => recreate.mutate()}>
              {t("docker.actions.recreate")}
            </Button>
          </>
        }
      >
        <Checkbox checked={pull} onCheckedChange={setPull} label={t("docker.actions.recreatePull")} />
      </DialogContent>
    </Dialog>
  );
}
