import { useEffect, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Braces, ChevronDown, ChevronRight, Database, Eye, HardDriveDownload, Table2 } from "lucide-react";
import {
  api,
  unwrap,
  isApiError,
  type BackupTarget,
  type DbDatabase,
  type DbInstance,
} from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { formatBytes } from "@/lib/utils";
import { backupTargetsQuery } from "@/pages/backups/shared";
import { Card } from "@/ui/card";
import { Button } from "@/ui/button";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Field } from "@/ui/field";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { EmptyState, ErrorState } from "@/ui/states";
import { SkeletonRows } from "@/ui/skeleton";
import { useToast } from "@/ui/toast";
import { dbDatabaseResource, useDbDatabases, useDbInstances } from "./shared";

/**
 * The Databases tab: every database the server lets this user see, with
 * Browse (inline objects view), Peek (MongoDB collections), and Dump (to a
 * backup target). Dump hides entirely when the targets probe answers 403 —
 * the one client-side probe this module makes; everything else mirrors grants.
 */
export function DbDatabasesPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/db/$instanceId/" });
  const { can } = useAuth();

  const instances = useDbInstances();
  const instance = instances.data?.find((row) => row.id === instanceId);
  const databases = useDbDatabases(instanceId);

  // Shares the shell's targets query; a 403 hides the dump action entirely.
  const targets = useQuery(backupTargetsQuery());
  const targetsVisible = !(
    targets.isError && isApiError(targets.error) && targets.error.status === 403
  );

  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [dumping, setDumping] = useState<string | null>(null);
  const [peeking, setPeeking] = useState<{ database: string; collection: string } | null>(null);

  if (!instance) return null;

  if (databases.isLoading) {
    return (
      <Card className="p-5">
        <SkeletonRows rows={3} />
      </Card>
    );
  }
  if (databases.isError) {
    return (
      <Card>
        <ErrorState error={databases.error} onRetry={() => void databases.refetch()} />
      </Card>
    );
  }

  const list = databases.data!;
  if (list.length === 0) {
    return (
      <Card>
        <EmptyState
          icon={Database}
          title={t("db.databases.empty.title")}
          body={t("db.databases.empty.body")}
        />
      </Card>
    );
  }

  const toggle = (name: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  };

  return (
    <>
      <Card className="overflow-hidden">
        <Table>
          <THead>
            <TR>
              <TH>{t("db.databases.columns.name")}</TH>
              <TH>{t("db.databases.columns.size")}</TH>
              <TH>{t("db.databases.columns.objects")}</TH>
              <TH className="text-right">{t("common.actions")}</TH>
            </TR>
          </THead>
          <TBody>
            {list.map((db) => (
              <DatabaseRow
                key={db.name}
                instance={instance}
                db={db}
                expanded={expanded.has(db.name)}
                canControlDb={can(dbDatabaseResource(instance, db.name), "control")}
                dumpVisible={targetsVisible}
                onToggle={() => toggle(db.name)}
                onDump={() => setDumping(db.name)}
                onPeek={(collection) => setPeeking({ database: db.name, collection })}
              />
            ))}
          </TBody>
        </Table>
      </Card>

      {dumping !== null && (
        <DumpDialog
          instanceId={instanceId}
          database={dumping}
          targets={(targets.data ?? []).filter((row) => row.enabled)}
          onClose={() => setDumping(null)}
        />
      )}
      {peeking !== null && (
        <PeekDialog
          instanceId={instanceId}
          database={peeking.database}
          collection={peeking.collection}
          onClose={() => setPeeking(null)}
        />
      )}
    </>
  );
}

function DatabaseRow({
  instance,
  db,
  expanded,
  canControlDb,
  dumpVisible,
  onToggle,
  onDump,
  onPeek,
}: {
  instance: DbInstance;
  db: DbDatabase;
  expanded: boolean;
  canControlDb: boolean;
  dumpVisible: boolean;
  onToggle: () => void;
  onDump: () => void;
  onPeek: (collection: string) => void;
}) {
  const { t } = useTranslation();
  const Chevron = expanded ? ChevronDown : ChevronRight;
  return (
    <>
      <TR interactive onClick={onToggle}>
        <TD>
          <span className="text-fg flex items-center gap-2 font-mono text-[13px] font-medium">
            <Chevron className="text-fg-subtle size-4 shrink-0" aria-hidden />
            {db.name}
          </span>
        </TD>
        <TD className="text-fg-muted whitespace-nowrap">{formatBytes(db.sizeBytes)}</TD>
        <TD className="text-fg-muted">{db.objectCount.toLocaleString()}</TD>
        <TD className="text-right">
          <div
            className="flex items-center justify-end gap-1"
            onClick={(e) => e.stopPropagation()}
          >
            <Button size="sm" variant="ghost" onClick={onToggle}>
              {expanded ? t("db.databases.collapse") : t("db.databases.browse")}
            </Button>
            {dumpVisible && canControlDb && (
              <Button size="sm" variant="ghost" onClick={onDump}>
                <HardDriveDownload className="size-3.5" aria-hidden />
                {t("db.databases.dump")}
              </Button>
            )}
          </div>
        </TD>
      </TR>
      {expanded && (
        <TR>
          <TD colSpan={4} className="bg-bg-subtle/50 px-4 py-3">
            <ObjectsView
              instance={instance}
              database={db.name}
              peekVisible={instance.engine === "mongodb" && canControlDb}
              onPeek={onPeek}
            />
          </TD>
        </TR>
      )}
    </>
  );
}

/** ObjectsView lists a database's tables or collections inline. */
function ObjectsView({
  instance,
  database,
  peekVisible,
  onPeek,
}: {
  instance: DbInstance;
  database: string;
  peekVisible: boolean;
  onPeek: (collection: string) => void;
}) {
  const { t } = useTranslation();
  const isSql = instance.engine === "mysql";

  const objects = useQuery({
    queryKey: ["db", instance.id, "databases", database, "objects"],
    queryFn: async () =>
      unwrap(
        await api.GET("/db/{id}/databases/{database}/objects", {
          params: { path: { id: instance.id, database } },
        }),
      ),
    staleTime: 30_000,
  });

  if (objects.isLoading) return <SkeletonRows rows={2} />;
  if (objects.isError) {
    return <ErrorState className="py-6" error={objects.error} onRetry={() => void objects.refetch()} />;
  }

  const rows = objects.data!;
  if (rows.length === 0) {
    return <p className="text-fg-subtle py-2 text-[13px]">{t("db.databases.objects.empty")}</p>;
  }

  return (
    <Table>
      <THead>
        <TR>
          <TH className="pl-2">{t("db.databases.objects.columns.name")}</TH>
          <TH>{t("db.databases.objects.columns.kind")}</TH>
          {isSql && <TH>{t("db.databases.objects.columns.engine")}</TH>}
          <TH>{t("db.databases.objects.columns.rows")}</TH>
          <TH>{t("db.databases.objects.columns.size")}</TH>
          {peekVisible && <TH />}
        </TR>
      </THead>
      <TBody>
        {rows.map((obj) => {
          const KindIcon = obj.kind === "table" ? Table2 : Braces;
          return (
            <TR key={obj.name}>
              <TD className="pl-2">
                <span className="text-fg flex items-center gap-2 font-mono text-[13px]">
                  <KindIcon className="text-fg-subtle size-3.5 shrink-0" aria-hidden />
                  {obj.name}
                </span>
              </TD>
              <TD className="text-fg-muted">{t(`db.databases.objects.kind.${obj.kind}`)}</TD>
              {isSql && <TD className="text-fg-muted">{obj.engine ?? "—"}</TD>}
              <TD className="text-fg-muted">{obj.rows.toLocaleString()}</TD>
              <TD className="text-fg-muted whitespace-nowrap">{formatBytes(obj.sizeBytes)}</TD>
              {peekVisible && (
                <TD className="text-right">
                  {obj.kind === "collection" && (
                    <Button size="sm" variant="ghost" onClick={() => onPeek(obj.name)}>
                      <Eye className="size-3.5" aria-hidden />
                      {t("db.databases.objects.peek")}
                    </Button>
                  )}
                </TD>
              )}
            </TR>
          );
        })}
      </TBody>
    </Table>
  );
}

/** DumpDialog queues a full dump of one database to a backup target. */
function DumpDialog({
  instanceId,
  database,
  targets,
  onClose,
}: {
  instanceId: string;
  database: string;
  targets: BackupTarget[];
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const [targetId, setTargetId] = useState<string>(targets.length === 1 ? targets[0].id : "");

  const dump = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/db/{id}/dump", {
          params: { path: { id: instanceId } },
          body: { database, targetId },
        }),
      ),
    onSuccess: () => {
      toast("success", t("db.dump.queued", { database }));
      onClose();
    },
    onError: (err) =>
      toast("error", t("db.dump.failed"), isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={t("db.dump.title", { database })}
        description={t("db.dump.description", { database })}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button
              variant="primary"
              loading={dump.isPending}
              disabled={targetId === ""}
              onClick={() => dump.mutate()}
            >
              {t("db.dump.confirm")}
            </Button>
          </>
        }
      >
        <Field label={t("db.dump.target")}>
          {(fieldProps) => (
            <Select value={targetId} onValueChange={setTargetId}>
              <SelectTrigger id={fieldProps.id} aria-describedby={fieldProps["aria-describedby"]}>
                <SelectValue placeholder={t("db.dump.targetPlaceholder")} />
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
      </DialogContent>
    </Dialog>
  );
}

function prettyDocument(doc: string): string {
  try {
    return JSON.stringify(JSON.parse(doc), null, 2);
  } catch {
    return doc;
  }
}

/** PeekDialog renders a collection's newest documents as extended JSON. */
function PeekDialog({
  instanceId,
  database,
  collection,
  onClose,
}: {
  instanceId: string;
  database: string;
  collection: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const [limit, setLimit] = useState("25");

  const peek = useQuery({
    queryKey: ["db", instanceId, "peek", database, collection, limit],
    queryFn: async () =>
      unwrap(
        await api.GET("/db/{id}/databases/{database}/peek", {
          params: {
            path: { id: instanceId, database },
            query: { collection, limit: Number(limit) },
          },
        }),
      ),
    retry: (count, err) => !(isApiError(err) && err.status === 403) && count < 2,
  });

  // Grant mirrors can drift; if the server still says no, toast and close.
  const denied = peek.isError && isApiError(peek.error) && peek.error.status === 403;
  useEffect(() => {
    if (denied) {
      toast("error", t("db.peek.denied"));
      onClose();
    }
  }, [denied, toast, t, onClose]);

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        wide
        title={t("db.peek.title", { collection })}
        description={t("db.peek.description", { database, collection })}
      >
        <div className="space-y-3">
          <div className="flex items-center gap-2">
            <label htmlFor="db-peek-limit" className="text-fg text-[13px] font-medium">
              {t("db.peek.limit")}
            </label>
            <Select value={limit} onValueChange={setLimit}>
              <SelectTrigger id="db-peek-limit" className="w-24">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {["10", "25", "50"].map((value) => (
                  <SelectItem key={value} value={value}>
                    {value}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {peek.isLoading ? (
            <SkeletonRows rows={3} />
          ) : peek.isError ? (
            denied ? null : (
              <ErrorState className="py-6" error={peek.error} onRetry={() => void peek.refetch()} />
            )
          ) : peek.data!.documents.length === 0 ? (
            <p className="text-fg-subtle py-4 text-center text-[13px]">{t("db.peek.empty")}</p>
          ) : (
            <div className="max-h-[55vh] space-y-2 overflow-y-auto">
              {peek.data!.documents.map((doc, i) => (
                <pre
                  key={i}
                  className="bg-bg-subtle border-edge text-fg overflow-x-auto rounded-md border p-3 font-mono text-xs leading-relaxed"
                >
                  {prettyDocument(doc)}
                </pre>
              ))}
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
