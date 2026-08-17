import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { CalendarClock, Database, Share2 } from "lucide-react";
import { api, unwrap, isApiError, type PveBackupJob, type PveStorage, type PveTask } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useLiveInvalidate } from "@/lib/live";
import { cn, formatBytes, formatDateTime } from "@/lib/utils";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { Button } from "@/ui/button";
import { Badge, StatusDot } from "@/ui/badge";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { Checkbox } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { SkeletonRows } from "@/ui/skeleton";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { useToast } from "@/ui/toast";

// ---- storage ---------------------------------------------------------------

/**
 * Storage types that are cluster-wide by nature. Name-based detection is
 * wrong here — "local" exists on every node but is a different disk each
 * time — so the type decides: these get deduplicated into one "Shared
 * storage" section, everything else lists under the node that owns it.
 */
const SHARED_STORAGE_TYPES = new Set([
  "nfs",
  "cifs",
  "pbs",
  "cephfs",
  "rbd",
  "glusterfs",
  "iscsi",
  "iscsidirect",
  "zfs",
]);

export function PveStoragePage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/pve/$instanceId/storage" });

  const storage = useQuery({
    queryKey: ["pve", instanceId, "storage"],
    queryFn: async () =>
      unwrap(await api.GET("/proxmox/{id}/storage", { params: { path: { id: instanceId } } })),
    refetchInterval: 60_000,
  });
  // Node status for the section heading dots; shares the summary tab's cache.
  const summary = useQuery({
    queryKey: ["pve", instanceId, "summary"],
    queryFn: async () =>
      unwrap(await api.GET("/proxmox/{id}/summary", { params: { path: { id: instanceId } } })),
  });

  if (storage.isLoading) return <SkeletonRows rows={4} />;
  if (storage.isError) {
    return (
      <Card>
        <ErrorState error={storage.error} onRetry={() => void storage.refetch()} />
      </Card>
    );
  }

  const rows = storage.data!;
  const nodeStatus = new Map((summary.data?.nodes ?? []).map((n) => [n.name, n.status]));

  const byNode = new Map<string, PveStorage[]>();
  const sharedByName = new Map<string, PveStorage[]>();
  for (const pool of rows) {
    if (SHARED_STORAGE_TYPES.has(pool.type)) {
      const list = sharedByName.get(pool.storage) ?? [];
      list.push(pool);
      sharedByName.set(pool.storage, list);
    } else {
      const list = byNode.get(pool.node) ?? [];
      list.push(pool);
      byNode.set(pool.node, list);
    }
  }
  const nodes = [...byNode.keys()].sort();
  // One row per shared pool: capacity is a single backing store, so repeating
  // it under every node would just double-count. Prefer an active mount.
  const shared = [...sharedByName.entries()]
    .map(([, mounts]) => ({
      pool: mounts.find((m) => m.active) ?? mounts[0],
      nodeCount: new Set(mounts.map((m) => m.node)).size,
    }))
    .sort((a, b) => a.pool.storage.localeCompare(b.pool.storage));

  return (
    <div className="space-y-4">
      {nodes.map((node) => (
        <Card key={node}>
          <div className="border-edge flex items-center gap-2.5 border-b px-5 py-3">
            <StatusDot
              tone={
                nodeStatus.get(node) === "online" ? "success" : nodeStatus.has(node) ? "danger" : "neutral"
              }
            />
            <h2 className="text-fg text-sm font-semibold">{node}</h2>
            <span className="text-fg-subtle text-xs">
              {t("pve.storage.poolCount", { count: byNode.get(node)!.length })}
            </span>
          </div>
          <StorageTable pools={byNode.get(node)!} />
        </Card>
      ))}

      {shared.length > 0 && (
        <Card>
          <div className="border-edge flex items-center gap-2.5 border-b px-5 py-3">
            <Share2 className="text-fg-subtle size-4" />
            <h2 className="text-fg text-sm font-semibold">{t("pve.storage.sharedSection")}</h2>
            <span className="text-fg-subtle text-xs">
              {t("pve.storage.poolCount", { count: shared.length })}
            </span>
          </div>
          <StorageTable pools={shared.map((s) => s.pool)} sharedNodeCounts={new Map(shared.map((s) => [s.pool.storage, s.nodeCount]))} />
        </Card>
      )}
    </div>
  );
}

function StorageTable({
  pools,
  sharedNodeCounts,
}: {
  pools: PveStorage[];
  sharedNodeCounts?: Map<string, number>;
}) {
  const { t } = useTranslation();
  return (
    <Table>
      <THead>
        <tr>
          <TH>{t("pve.storage.columns.pool")}</TH>
          <TH className="hidden sm:table-cell">{t("pve.storage.columns.type")}</TH>
          <TH className="hidden md:table-cell">{t("pve.storage.columns.content")}</TH>
          <TH className="w-64">{t("pve.storage.columns.usage")}</TH>
        </tr>
      </THead>
      <TBody>
        {pools
          .slice()
          .sort((a, b) => a.storage.localeCompare(b.storage))
          .map((pool) => {
            const fraction = pool.totalBytes > 0 ? pool.usedBytes / pool.totalBytes : 0;
            const nodeCount = sharedNodeCounts?.get(pool.storage);
            return (
              <TR key={pool.storage + pool.node}>
                <TD>
                  <span className="text-fg flex items-center gap-2 font-medium">
                    <Database className="text-fg-subtle size-4" />
                    {pool.storage}
                    {nodeCount !== undefined && (
                      <Badge tone="info">
                        {t("pve.storage.shared")}
                        {nodeCount > 1 ? ` · ${t("pve.storage.sharedNodes", { count: nodeCount })}` : ""}
                      </Badge>
                    )}
                    {!pool.active && <Badge tone="neutral">{t("pve.storage.inactive")}</Badge>}
                  </span>
                </TD>
                <TD className="text-fg-muted hidden sm:table-cell">{pool.type}</TD>
                <TD className="text-fg-subtle hidden max-w-44 truncate text-xs md:table-cell">{pool.content}</TD>
                <TD>
                  <div className="flex items-center gap-2.5">
                    <div className="bg-bg-subtle h-1.5 min-w-24 flex-1 overflow-hidden rounded-full">
                      <div
                        className={cn(
                          "h-full rounded-full",
                          fraction > 0.9 ? "bg-danger" : fraction > 0.75 ? "bg-warning" : "bg-accent",
                        )}
                        style={{ width: `${Math.min(fraction * 100, 100)}%` }}
                      />
                    </div>
                    <span className="text-fg-muted text-xs whitespace-nowrap">
                      {formatBytes(pool.usedBytes)} / {formatBytes(pool.totalBytes)}
                    </span>
                  </div>
                </TD>
              </TR>
            );
          })}
      </TBody>
    </Table>
  );
}

// ---- tasks -----------------------------------------------------------------

export function PveTasksPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/pve/$instanceId/tasks" });

  const tasks = useQuery({
    queryKey: ["pve", instanceId, "tasks"],
    queryFn: async () =>
      unwrap(await api.GET("/proxmox/{id}/tasks", { params: { path: { id: instanceId }, query: { limit: 50 } } })),
  });
  useLiveInvalidate(`integration:proxmox/${instanceId}`, [["pve", instanceId, "tasks"]], ["tasks-changed"]);

  const [selected, setSelected] = useState<PveTask | null>(null);

  if (tasks.isLoading) return <SkeletonRows rows={6} />;
  if (tasks.isError) {
    return (
      <Card>
        <ErrorState error={tasks.error} onRetry={() => void tasks.refetch()} />
      </Card>
    );
  }

  return (
    <>
      <Card>
        {tasks.data!.length === 0 ? (
          <p className="text-fg-subtle px-5 py-8 text-center text-[13px]">{t("pve.tasks.empty")}</p>
        ) : (
          <Table>
            <THead>
              <tr>
                <TH>{t("pve.tasks.columns.time")}</TH>
                <TH>{t("pve.tasks.columns.task")}</TH>
                <TH className="hidden sm:table-cell">{t("pve.tasks.columns.node")}</TH>
                <TH className="hidden md:table-cell">{t("pve.tasks.columns.user")}</TH>
                <TH>{t("pve.tasks.columns.status")}</TH>
              </tr>
            </THead>
            <TBody>
              {tasks.data!.map((task) => (
                <TR key={task.upid} interactive onClick={() => setSelected(task)}>
                  <TD className="text-fg-muted whitespace-nowrap">{formatDateTime(task.startTime)}</TD>
                  <TD>
                    <code className="text-fg font-mono text-xs">{task.type}</code>
                    {task.id && <span className="text-fg-subtle ml-1.5 text-xs">{task.id}</span>}
                  </TD>
                  <TD className="text-fg-muted hidden sm:table-cell">{task.node}</TD>
                  <TD className="text-fg-subtle hidden text-xs md:table-cell">{task.user}</TD>
                  <TD>
                    {task.running ? (
                      <Badge tone="info">{t("pve.tasks.running")}</Badge>
                    ) : task.exitStatus === "OK" ? (
                      <Badge tone="success">{t("pve.tasks.ok")}</Badge>
                    ) : (
                      <Badge tone="danger">{task.exitStatus ?? "?"}</Badge>
                    )}
                  </TD>
                </TR>
              ))}
            </TBody>
          </Table>
        )}
      </Card>

      {selected && (
        <TaskLogDialog instanceId={instanceId} task={selected} onClose={() => setSelected(null)} />
      )}
    </>
  );
}

function TaskLogDialog({ instanceId, task, onClose }: { instanceId: string; task: PveTask; onClose: () => void }) {
  const { t } = useTranslation();
  const log = useQuery({
    queryKey: ["pve", instanceId, "task-log", task.upid],
    queryFn: async () =>
      unwrap(
        await api.GET("/proxmox/{id}/tasks/{node}/{upid}/log", {
          params: { path: { id: instanceId, node: task.node, upid: task.upid } },
        }),
      ),
    refetchInterval: task.running ? 2000 : false,
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent wide title={t("pve.tasks.logTitle")} description={<code className="font-mono text-xs">{task.upid}</code>}>
        <pre className="bg-bg-subtle border-edge text-fg-muted max-h-96 overflow-auto rounded-md border p-3 font-mono text-xs leading-relaxed">
          {log.data ? log.data.lines.join("\n") || "—" : t("common.loading")}
        </pre>
      </DialogContent>
    </Dialog>
  );
}

// ---- backup schedules ------------------------------------------------------

export function PveBackupsPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/pve/$instanceId/backups" });
  const { can } = useAuth();
  const canConfigure = can(`proxmox/${instanceId}`, "configure");

  const jobs = useQuery({
    queryKey: ["pve", instanceId, "backup-jobs"],
    queryFn: async () =>
      unwrap(await api.GET("/proxmox/{id}/backup-jobs", { params: { path: { id: instanceId } } })),
  });

  const [editing, setEditing] = useState<PveBackupJob | "new" | null>(null);

  if (jobs.isLoading) return <SkeletonRows rows={3} />;
  if (jobs.isError) {
    return (
      <Card>
        <ErrorState error={jobs.error} onRetry={() => void jobs.refetch()} />
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader
        title={t("pve.backups.jobs")}
        actions={
          canConfigure ? (
            <Button variant="primary" size="sm" onClick={() => setEditing("new")}>
              <CalendarClock className="size-3.5" />
              {t("pve.backups.addJob")}
            </Button>
          ) : undefined
        }
      />
      <CardBody className="px-0 py-0">
        {jobs.data!.length === 0 ? (
          <EmptyState
            icon={CalendarClock}
            title={t("pve.backups.empty.title")}
            body={t("pve.backups.empty.body")}
            className="py-10"
          />
        ) : (
          <ul>
            {jobs.data!.map((job) => (
              <li key={job.id} className="border-edge flex items-center gap-3 border-b px-5 py-3 last:border-b-0">
                <CalendarClock className="text-fg-subtle size-4 shrink-0" />
                <div className="min-w-0 flex-1">
                  <p className="text-fg flex items-center gap-2 text-sm font-medium">
                    <code className="font-mono">{job.schedule}</code>
                    <span className="text-fg-subtle">→ {job.storage}</span>
                    {!job.enabled && <Badge tone="neutral">{t("common.disabled")}</Badge>}
                  </p>
                  <p className="text-fg-subtle text-xs">
                    {job.vmids ? `VMIDs ${job.vmids}` : t("pve.backups.allGuests")} · {job.mode}
                  </p>
                </div>
                {canConfigure && (
                  <Button size="sm" variant="secondary" onClick={() => setEditing(job)}>
                    {t("common.edit")}
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
      </CardBody>

      {editing && (
        <BackupJobDialog
          instanceId={instanceId}
          job={editing === "new" ? null : editing}
          onClose={() => setEditing(null)}
        />
      )}
    </Card>
  );
}

function BackupJobDialog({
  instanceId,
  job,
  onClose,
}: {
  instanceId: string;
  job: PveBackupJob | null;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();

  const storage = useQuery({
    queryKey: ["pve", instanceId, "storage"],
    queryFn: async () =>
      unwrap(await api.GET("/proxmox/{id}/storage", { params: { path: { id: instanceId } } })),
  });
  const pools = [...new Set((storage.data ?? []).map((p) => p.storage))];

  const [schedule, setSchedule] = useState(job?.schedule ?? "02:30");
  const [target, setTarget] = useState(job?.storage ?? "");
  const [mode, setMode] = useState(job?.mode || "snapshot");
  const [vmids, setVmids] = useState(job?.vmids ?? "");
  const [enabled, setEnabled] = useState(job?.enabled ?? true);

  const save = useMutation({
    mutationFn: async () => {
      const body = {
        schedule,
        storage: target || pools[0],
        mode: mode as "snapshot" | "suspend" | "stop",
        vmids,
        enabled,
      };
      if (job) {
        return unwrap(
          await api.PUT("/proxmox/{id}/backup-jobs/{jobId}", {
            params: { path: { id: instanceId, jobId: job.id } },
            body,
          }),
        );
      }
      return unwrap(
        await api.POST("/proxmox/{id}/backup-jobs", { params: { path: { id: instanceId } }, body }),
      );
    },
    onSuccess: () => {
      toast("success", t("pve.backups.jobSaved"));
      void queryClient.invalidateQueries({ queryKey: ["pve", instanceId, "backup-jobs"] });
      onClose();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={job ? t("pve.backups.editJob") : t("pve.backups.addJob")}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button
              variant="primary"
              disabled={!schedule.trim() || (!target && pools.length === 0)}
              loading={save.isPending}
              onClick={() => save.mutate()}
            >
              {t("common.save")}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <Field label={t("pve.backups.schedule")} hint={t("pve.backups.scheduleHint")}>
              {(p) => <Input {...p} autoFocus className="font-mono text-[13px]" value={schedule} onChange={(e) => setSchedule(e.target.value)} />}
            </Field>
            <Field label={t("pve.backups.storage")}>
              {() => (
                <Select value={target || pools[0] || ""} onValueChange={setTarget}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {pools.map((pool) => (
                      <SelectItem key={pool} value={pool}>
                        {pool}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            </Field>
          </div>
          <Field label={t("pve.backups.guests")} hint={t("pve.backups.guestsHint")}>
            {(p) => <Input {...p} placeholder="101,102" className="font-mono text-[13px]" value={vmids} onChange={(e) => setVmids(e.target.value)} />}
          </Field>
          <Field label={t("pve.guest.backupMode")}>
            {() => (
              <Select value={mode} onValueChange={setMode}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="snapshot">snapshot</SelectItem>
                  <SelectItem value="suspend">suspend</SelectItem>
                  <SelectItem value="stop">stop</SelectItem>
                </SelectContent>
              </Select>
            )}
          </Field>
          <Checkbox checked={enabled} onCheckedChange={setEnabled} label={t("common.enabled")} />
        </div>
      </DialogContent>
    </Dialog>
  );
}
