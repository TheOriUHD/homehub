import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Boxes, Play, RotateCcw, Square } from "lucide-react";
import { api, unwrap, isApiError, type DockerContainer } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useLiveInvalidate } from "@/lib/live";
import { formatRelative } from "@/lib/utils";
import { Card } from "@/ui/card";
import { Button } from "@/ui/button";
import { Badge } from "@/ui/badge";
import { Input } from "@/ui/input";
import { Checkbox } from "@/ui/misc";
import { ConfirmDialog } from "@/ui/confirm";
import { EmptyState, ErrorState } from "@/ui/states";
import { SkeletonRows } from "@/ui/skeleton";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { useToast } from "@/ui/toast";

export type DockerContainerAction = "start" | "stop" | "restart" | "pause" | "unpause" | "kill";

export const containerStateTone = {
  running: "success",
  exited: "neutral",
  created: "neutral",
  paused: "warning",
  restarting: "info",
  dead: "danger",
} as const;

export function containerStateLabel(t: (k: string) => string, state: string): string {
  switch (state) {
    case "running":
    case "exited":
    case "created":
    case "paused":
    case "restarting":
    case "dead":
      return t(`docker.containers.state.${state}`);
    default:
      return state;
  }
}

/** formatPorts renders published ports as "8080→80/tcp", deduplicating v4/v6 doubles. */
export function formatPorts(ports: DockerContainer["ports"]): string {
  if (!ports?.length) return "—";
  const rendered = new Set<string>();
  for (const p of ports) {
    rendered.add(p.public != null ? `${p.public}→${p.private}/${p.type}` : `${p.private}/${p.type}`);
  }
  return [...rendered].join(", ");
}

export function DockerContainersPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/docker/$instanceId/" });
  const navigate = useNavigate();
  const { can } = useAuth();
  const toast = useToast();
  const queryClient = useQueryClient();

  const containers = useQuery({
    queryKey: ["docker", instanceId, "containers"],
    queryFn: async () =>
      unwrap(await api.GET("/docker/{id}/containers", { params: { path: { id: instanceId } } })),
  });
  useLiveInvalidate(`integration:docker/${instanceId}`, [["docker", instanceId, "containers"]], ["containers-changed"]);

  const [filter, setFilter] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [confirmStop, setConfirmStop] = useState(false);

  const filtered = useMemo(() => {
    const list = containers.data ?? [];
    const needle = filter.trim().toLowerCase();
    if (!needle) return list;
    return list.filter(
      (c) =>
        c.name.toLowerCase().includes(needle) ||
        c.image.toLowerCase().includes(needle) ||
        c.state.toLowerCase().includes(needle) ||
        (c.composeProject ?? "").toLowerCase().includes(needle),
    );
  }, [containers.data, filter]);

  const controllable = (c: DockerContainer) => can(`docker/${instanceId}/container/${c.name}`, "control");

  // There is no bulk endpoint — apply the action per container and report
  // each failure individually so nothing disappears into a summary.
  const bulk = useMutation({
    mutationFn: async (action: DockerContainerAction) => {
      let ok = 0;
      const failures: { name: string; message: string }[] = [];
      for (const name of selected) {
        try {
          unwrap(
            await api.POST("/docker/{id}/containers/{name}/action", {
              params: { path: { id: instanceId, name } },
              body: { action },
            }),
          );
          ok++;
        } catch (err) {
          failures.push({ name, message: isApiError(err) ? err.message : t("common.unknownError") });
        }
      }
      return { action, ok, failures };
    },
    onSuccess: ({ action, ok, failures }) => {
      for (const failure of failures) {
        toast("error", t("docker.actions.failedFor", { name: failure.name }), failure.message);
      }
      if (ok > 0) {
        toast("success", t("docker.actions.bulkDone", { action: t(`docker.actions.${action}`), count: ok }));
      }
      setSelected(new Set());
      setConfirmStop(false);
      void queryClient.invalidateQueries({ queryKey: ["docker", instanceId, "containers"] });
    },
  });

  if (containers.isLoading) return <SkeletonRows rows={6} />;
  if (containers.isError) {
    return (
      <Card>
        <ErrorState error={containers.error} onRetry={() => void containers.refetch()} />
      </Card>
    );
  }

  const selectable = filtered.filter(controllable);
  const allSelected = selectable.length > 0 && selectable.every((c) => selected.has(c.name));

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          placeholder={t("docker.containers.searchPlaceholder")}
          className="max-w-72"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
        {selected.size > 0 && (
          <div className="bg-accent-soft text-accent-soft-fg flex items-center gap-2 rounded-lg px-3 py-1.5 text-[13px] font-medium">
            {t("docker.containers.selected", { count: selected.size })}
            <span className="flex gap-1">
              <Button size="sm" variant="secondary" onClick={() => bulk.mutate("start")} loading={bulk.isPending}>
                <Play className="size-3.5" /> {t("docker.actions.start")}
              </Button>
              <Button size="sm" variant="secondary" onClick={() => bulk.mutate("restart")} loading={bulk.isPending}>
                <RotateCcw className="size-3.5" /> {t("docker.actions.restart")}
              </Button>
              <Button size="sm" variant="danger-ghost" onClick={() => setConfirmStop(true)} loading={bulk.isPending}>
                <Square className="size-3.5" /> {t("docker.actions.stop")}
              </Button>
            </span>
          </div>
        )}
      </div>

      <Card>
        {filtered.length === 0 ? (
          <EmptyState icon={Boxes} title={t("docker.containers.empty.title")} body={t("docker.containers.empty.body")} />
        ) : (
          <Table>
            <THead>
              <tr>
                <TH className="w-10">
                  <Checkbox
                    checked={allSelected}
                    onCheckedChange={(checked) =>
                      setSelected(checked ? new Set(selectable.map((c) => c.name)) : new Set())
                    }
                    label=""
                  />
                </TH>
                <TH>{t("docker.containers.columns.container")}</TH>
                <TH>{t("docker.containers.columns.state")}</TH>
                <TH className="hidden md:table-cell">{t("docker.containers.columns.ports")}</TH>
                <TH className="hidden lg:table-cell">{t("docker.containers.columns.created")}</TH>
              </tr>
            </THead>
            <TBody>
              {filtered.map((container) => (
                <TR
                  key={container.id}
                  interactive
                  onClick={() =>
                    void navigate({
                      to: "/docker/$instanceId/containers/$name",
                      params: { instanceId, name: container.name },
                    })
                  }
                >
                  <TD onClick={(e) => e.stopPropagation()}>
                    {controllable(container) && (
                      <Checkbox
                        checked={selected.has(container.name)}
                        onCheckedChange={(checked) =>
                          setSelected((prev) => {
                            const next = new Set(prev);
                            if (checked) next.add(container.name);
                            else next.delete(container.name);
                            return next;
                          })
                        }
                        label=""
                      />
                    )}
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
                  <TD className="text-fg-muted hidden max-w-56 truncate font-mono text-xs md:table-cell">
                    {formatPorts(container.ports)}
                  </TD>
                  <TD className="text-fg-muted hidden whitespace-nowrap lg:table-cell">
                    {formatRelative(container.created)}
                  </TD>
                </TR>
              ))}
            </TBody>
          </Table>
        )}
      </Card>

      <ConfirmDialog
        open={confirmStop}
        onOpenChange={setConfirmStop}
        title={t("docker.actions.bulkStopConfirmTitle", { count: selected.size })}
        body={t("docker.actions.bulkStopConfirmBody")}
        confirmLabel={t("docker.actions.stop")}
        loading={bulk.isPending}
        onConfirm={() => bulk.mutate("stop")}
      />
    </div>
  );
}
