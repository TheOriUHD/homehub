import { useEffect, useId, useMemo, useState, type KeyboardEvent as ReactKeyboardEvent } from "react";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  ArrowDown,
  ArrowLeft,
  ArrowUp,
  Braces,
  ChevronLeft,
  ChevronRight,
  Loader2,
  Pencil,
  Plus,
  Table2,
  Trash2,
} from "lucide-react";
import {
  api,
  unwrap,
  isApiError,
  type DbColumn,
  type DbInstance,
  type DbObjectSchema,
} from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { cn } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Card, CardHeader } from "@/ui/card";
import { ConfirmDialog } from "@/ui/confirm";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Field } from "@/ui/field";
import { Input, Textarea } from "@/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { Switch } from "@/ui/switch";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { ErrorState } from "@/ui/states";
import { SkeletonRows } from "@/ui/skeleton";
import { useToast } from "@/ui/toast";
import { CreateCollectionDialog, CreateTableDialog } from "./create";
import { PANE, dbDatabaseResource, prettyDocument, useDbInstances, useDbObjectSchema } from "./shared";

/*
 * The Adminer-style browser for one table or collection: a real data grid
 * (sort, per-column filters, pagination) with full row/document editing, a
 * structure view, and the destructive operations behind typed confirms.
 * Everything mutating mirrors the control grant on the database; the server
 * still authorizes and audits every call.
 */

const PAGE_SIZES = [25, 50, 100, 250];

type FilterOp = "eq" | "ne" | "lt" | "lte" | "gt" | "gte" | "like" | "is_null" | "not_null";
type DraftOp = "auto" | FilterOp;
const FILTER_OPS: DraftOp[] = ["auto", "eq", "ne", "lt", "lte", "gt", "gte", "like", "is_null", "not_null"];

type FilterDraft = { op: DraftOp; value: string };

/** buildTriple turns one column's filter draft into a `col:op:value` param. */
function buildTriple(column: string, draft: FilterDraft): string | null {
  if (draft.op === "is_null" || draft.op === "not_null") return `${column}:${draft.op}:`;
  const value = draft.value;
  if (value === "") return null;
  if (draft.op === "auto") {
    // Bare numbers filter exact; anything else becomes a contains-match.
    return /^-?\d+(\.\d+)?$/.test(value.trim())
      ? `${column}:eq:${value.trim()}`
      : `${column}:like:%${value}%`;
  }
  return `${column}:${draft.op}:${value}`;
}

/** The identity payload: primary-key values as loaded, or every column when keyless. */
function rowIdentity(
  dataColumns: string[],
  row: (string | null)[],
  primaryKey: string[],
): Record<string, string | null> {
  const keys = primaryKey.length > 0 ? primaryKey : dataColumns;
  const identity: Record<string, string | null> = {};
  for (const key of keys) {
    const idx = dataColumns.indexOf(key);
    if (idx >= 0) identity[key] = row[idx];
  }
  return identity;
}

function identitySummary(identity: Record<string, string | null>): string {
  const parts = Object.entries(identity).map(([k, v]) => `${k} = ${v ?? "NULL"}`);
  return parts.length > 3 ? `${parts.slice(0, 3).join(", ")}, …` : parts.join(", ");
}

/** extractId pulls a document's _id out as extended JSON, or null when absent. */
function extractId(doc: string): string | null {
  try {
    const parsed: unknown = JSON.parse(doc);
    if (parsed !== null && typeof parsed === "object" && "_id" in parsed) {
      return JSON.stringify((parsed as Record<string, unknown>)._id);
    }
  } catch {
    /* unparseable documents just lose their row actions */
  }
  return null;
}

export function DbBrowsePage() {
  const { instanceId, database, object } = useParams({
    from: "/shell/db/$instanceId/browse/$database/$object",
  });
  const instances = useDbInstances();
  const instance = instances.data?.find((row) => row.id === instanceId);
  if (!instance) return null;
  // Keyed so grid state (sort, filters, page) never leaks across objects.
  return (
    <BrowseView
      key={`${database}/${object}`}
      instance={instance}
      database={database}
      object={object}
    />
  );
}

function BrowseView({
  instance,
  database,
  object,
}: {
  instance: DbInstance;
  database: string;
  object: string;
}) {
  const { t } = useTranslation();
  const { can } = useAuth();
  const canControl = can(dbDatabaseResource(instance, database), "control");

  const schema = useDbObjectSchema(instance.id, database, object);
  const [tab, setTab] = useState<"data" | "structure">("data");
  const [lastDurationMs, setLastDurationMs] = useState<number | null>(null);
  // The sibling-object builder ("New table" / "New collection" in this database).
  const [creatingSibling, setCreatingSibling] = useState(false);

  if (schema.isLoading) {
    return (
      <Card className="p-5">
        <SkeletonRows rows={4} />
      </Card>
    );
  }
  if (schema.isError) {
    return (
      <Card>
        <ErrorState error={schema.error} onRetry={() => void schema.refetch()} />
      </Card>
    );
  }

  const info = schema.data!;
  const isTable = info.kind === "table";
  const KindIcon = isTable ? Table2 : Braces;
  const estimate = info.rowEstimate ?? 0;

  const tabs = [
    { key: "data" as const, label: t("db.browse.tabs.data") },
    { key: "structure" as const, label: t("db.browse.tabs.structure") },
  ];

  return (
    <div className="space-y-5">
      <div>
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
          <Link
            to="/db/$instanceId"
            params={{ instanceId: instance.id }}
            className="text-fg-subtle hover:text-fg inline-flex items-center gap-1.5 text-[13px] transition-colors"
          >
            <ArrowLeft className="size-3.5" aria-hidden />
            {t("db.tabs.databases")}
          </Link>
          {canControl && (
            <Button size="sm" variant="secondary" onClick={() => setCreatingSibling(true)}>
              <Plus className="size-3.5" aria-hidden />
              {t(instance.engine === "mysql" ? "db.create.table.button" : "db.create.collection.button")}
            </Button>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <span className="bg-accent-soft text-accent-soft-fg flex size-10 shrink-0 items-center justify-center rounded-xl">
            <KindIcon className="size-5" aria-hidden />
          </span>
          <div className="min-w-0 flex-1">
            <h1 className="text-fg flex flex-wrap items-center gap-2.5 text-xl font-semibold tracking-tight">
              <span className="min-w-0 truncate font-mono text-[19px]">
                <span className="text-fg-muted">{database}</span>
                <span className="text-fg-subtle">.</span>
                {object}
              </span>
              <Badge tone="neutral">{t(`db.databases.objects.kind.${info.kind}`)}</Badge>
            </h1>
            <p className="text-fg-muted text-sm">
              {t(isTable ? "db.browse.estRows" : "db.browse.estDocs", {
                count: estimate,
                formatted: estimate.toLocaleString(),
              })}
              {lastDurationMs !== null && (
                <> · {t("db.browse.lastFetch", { ms: lastDurationMs.toFixed(1) })}</>
              )}
            </p>
          </div>
        </div>
      </div>

      <nav className="border-edge flex gap-1 border-b">
        {tabs.map((entry) => (
          <button
            key={entry.key}
            onClick={() => setTab(entry.key)}
            className={cn(
              "-mb-px shrink-0 cursor-pointer border-b-2 px-3.5 py-2 text-[13px] font-medium transition-colors",
              tab === entry.key
                ? "border-accent text-fg"
                : "text-fg-muted hover:text-fg border-transparent",
            )}
          >
            {entry.label}
          </button>
        ))}
      </nav>

      {tab === "data" ? (
        isTable ? (
          <SqlDataTab
            instanceId={instance.id}
            database={database}
            object={object}
            info={info}
            canControl={canControl}
            onDuration={setLastDurationMs}
          />
        ) : (
          <MongoDataTab
            instanceId={instance.id}
            database={database}
            object={object}
            info={info}
            canControl={canControl}
            onDuration={setLastDurationMs}
          />
        )
      ) : (
        <StructureTab
          instanceId={instance.id}
          database={database}
          object={object}
          info={info}
          canDelete={can(dbDatabaseResource(instance, database), "delete")}
        />
      )}

      {creatingSibling &&
        (instance.engine === "mysql" ? (
          <CreateTableDialog
            instanceId={instance.id}
            database={database}
            onClose={() => setCreatingSibling(false)}
          />
        ) : (
          <CreateCollectionDialog
            instanceId={instance.id}
            database={database}
            onClose={() => setCreatingSibling(false)}
          />
        ))}
    </div>
  );
}

/* ------------------------------- SQL grid ------------------------------- */

function SqlDataTab({
  instanceId,
  database,
  object,
  info,
  canControl,
  onDuration,
}: {
  instanceId: string;
  database: string;
  object: string;
  info: DbObjectSchema;
  canControl: boolean;
  onDuration: (ms: number) => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();

  const schemaCols = useMemo(() => info.columns ?? [], [info.columns]);
  const primaryKey = useMemo(() => info.primaryKey ?? [], [info.primaryKey]);

  const [limit, setLimit] = useState(50);
  const [offset, setOffset] = useState(0);
  const [sort, setSort] = useState<{ column: string; dir: "asc" | "desc" } | null>(null);
  const [drafts, setDrafts] = useState<Record<string, FilterDraft>>({});
  const [applied, setApplied] = useState<string[]>([]);

  const data = useQuery({
    queryKey: [
      "db",
      instanceId,
      "browse",
      database,
      object,
      "data",
      { limit, offset, sort, filters: applied },
    ],
    queryFn: async () =>
      unwrap(
        await api.GET("/db/{id}/databases/{database}/objects/{object}/data", {
          params: {
            path: { id: instanceId, database, object },
            query: {
              limit,
              offset,
              ...(sort !== null ? { orderBy: sort.column, dir: sort.dir } : {}),
              ...(applied.length > 0 ? { filter: applied } : {}),
            },
          },
        }),
      ),
    placeholderData: keepPreviousData,
  });

  const durationMs = data.data?.durationMs;
  useEffect(() => {
    if (durationMs !== undefined) onDuration(durationMs);
  }, [durationMs, onDuration]);

  const columns = data.data?.columns ?? schemaCols.map((col) => col.name);
  const rows = data.data?.rows ?? [];
  const total = data.data?.total ?? 0;

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ["db", instanceId, "browse", database, object] });
    void queryClient.invalidateQueries({ queryKey: ["db", instanceId, "databases"] });
  };

  const getDraft = (column: string): FilterDraft => drafts[column] ?? { op: "auto", value: "" };

  const applyFrom = (next: Record<string, FilterDraft>) => {
    const triples: string[] = [];
    for (const column of columns) {
      const draft = next[column];
      if (!draft) continue;
      const triple = buildTriple(column, draft);
      if (triple !== null) triples.push(triple);
    }
    setApplied(triples);
    setOffset(0);
  };

  const clearFilters = () => {
    setDrafts({});
    setApplied([]);
    setOffset(0);
  };

  const cycleSort = (column: string) => {
    setOffset(0);
    setSort((prev) =>
      prev?.column === column
        ? prev.dir === "asc"
          ? { column, dir: "desc" }
          : null
        : { column, dir: "asc" },
    );
  };

  // Dialog state: null = closed; "add" = insert; a row = edit that row.
  const [editing, setEditing] = useState<"add" | { row: (string | null)[] } | null>(null);
  const [deleting, setDeleting] = useState<Record<string, string | null> | null>(null);

  const del = useMutation({
    mutationFn: async (identity: Record<string, string | null>) =>
      unwrap(
        await api.POST("/db/{id}/databases/{database}/objects/{object}/rows/delete", {
          params: { path: { id: instanceId, database, object } },
          body: { identity },
        }),
      ),
    onSuccess: (res) => {
      toast("success", t("db.browse.row.deleted", { count: res.affected }));
      setDeleting(null);
      invalidate();
    },
    onError: (err) =>
      toast("error", t("db.browse.row.failed"), isApiError(err) ? err.message : t("common.unknownError")),
  });

  const actionCol = canControl ? 1 : 0;

  return (
    <div className="space-y-3">
      {(canControl || applied.length > 0) && (
        <div className="flex flex-wrap items-center gap-2">
          {applied.length > 0 && (
            <>
              <Badge tone="accent">{t("db.browse.filter.active", { count: applied.length })}</Badge>
              <Button size="sm" variant="ghost" onClick={clearFilters}>
                {t("db.browse.filter.clear")}
              </Button>
            </>
          )}
          {canControl && (
            <Button size="sm" variant="secondary" className="ml-auto" onClick={() => setEditing("add")}>
              <Plus className="size-3.5" aria-hidden />
              {t("db.browse.addRow")}
            </Button>
          )}
        </div>
      )}

      <div
        className="overflow-hidden rounded-lg border"
        style={{ background: PANE.bg, borderColor: PANE.edge }}
      >
        <div
          className={cn(
            "max-h-[65vh] overflow-auto transition-opacity",
            data.isPlaceholderData && "opacity-60",
          )}
        >
          <table className="w-full border-collapse font-mono text-[12.5px]">
            <thead>
              <tr>
                {columns.map((column) => {
                  const active = sort?.column === column;
                  const SortIcon = sort?.dir === "desc" ? ArrowDown : ArrowUp;
                  return (
                    <th
                      key={column}
                      className="sticky top-0 z-10 h-8 p-0 text-left font-semibold whitespace-nowrap"
                      style={{ background: PANE.chrome }}
                    >
                      <button
                        onClick={() => cycleSort(column)}
                        aria-label={t("db.browse.sortAria", { column })}
                        className="flex h-full w-full cursor-pointer items-center gap-1 px-3 transition-opacity hover:opacity-80"
                        style={{ color: active ? PANE.text : PANE.dim }}
                      >
                        {column}
                        {active && <SortIcon className="size-3 shrink-0" aria-hidden />}
                      </button>
                    </th>
                  );
                })}
                {actionCol > 0 && (
                  <th
                    className="sticky top-0 z-10 h-8 p-0"
                    style={{ background: PANE.chrome }}
                    aria-hidden
                  />
                )}
              </tr>
              <tr>
                {columns.map((column) => (
                  <FilterCell
                    key={column}
                    column={column}
                    draft={getDraft(column)}
                    onChange={(draft, apply) => {
                      const next = { ...drafts, [column]: draft };
                      setDrafts(next);
                      if (apply) applyFrom(next);
                    }}
                  />
                ))}
                {actionCol > 0 && (
                  <th
                    className="sticky top-8 z-10 px-2 py-1"
                    style={{ background: PANE.chrome, boxShadow: `inset 0 -1px 0 ${PANE.edge}` }}
                    aria-hidden
                  />
                )}
              </tr>
            </thead>
            <tbody>
              {data.isLoading ? (
                <tr>
                  <td colSpan={columns.length + actionCol} className="py-14 text-center">
                    <Loader2 aria-hidden className="inline size-5 animate-spin" style={{ color: PANE.dim }} />
                    <span className="sr-only">{t("common.loading")}</span>
                  </td>
                </tr>
              ) : data.isError ? (
                <tr>
                  <td colSpan={columns.length + actionCol} className="px-4 py-6">
                    {/* 400/502 carry the engine's real message — verbatim. */}
                    <pre
                      role="alert"
                      className="font-mono text-[12.5px] whitespace-pre-wrap"
                      style={{ color: PANE.error }}
                    >
                      {isApiError(data.error) ? data.error.message : t("common.unknownError")}
                    </pre>
                    <p className="mt-1 text-xs" style={{ color: PANE.dim }}>
                      {t("db.browse.loadFailedHint")}
                    </p>
                  </td>
                </tr>
              ) : rows.length === 0 ? (
                <tr>
                  <td
                    colSpan={columns.length + actionCol}
                    className="px-4 py-10 text-center text-[13px]"
                    style={{ color: PANE.dim }}
                  >
                    {t("db.browse.emptyRows")}
                  </td>
                </tr>
              ) : (
                rows.map((row, i) => (
                  <tr key={i} style={i > 0 ? { borderTop: `1px solid ${PANE.edge}` } : undefined}>
                    {row.map((cell, j) => (
                      <td
                        key={j}
                        title={cell ?? undefined}
                        className="max-w-[24rem] overflow-hidden px-3 py-1 align-top text-ellipsis whitespace-pre"
                        style={
                          cell === null
                            ? { color: PANE.dim, fontStyle: "italic" }
                            : { color: PANE.text }
                        }
                      >
                        {cell ?? "NULL"}
                      </td>
                    ))}
                    {actionCol > 0 && (
                      <td className="px-2 py-0.5 text-right align-top whitespace-nowrap">
                        <button
                          aria-label={t("db.browse.row.editAria")}
                          onClick={() => setEditing({ row })}
                          className="cursor-pointer p-1 text-[#5b6272] transition-colors hover:text-[#ccd2de]"
                        >
                          <Pencil className="size-3.5" aria-hidden />
                        </button>
                        <button
                          aria-label={t("db.browse.row.deleteAria")}
                          onClick={() => setDeleting(rowIdentity(columns, row, primaryKey))}
                          className="cursor-pointer p-1 text-[#5b6272] transition-colors hover:text-[#f87171]"
                        >
                          <Trash2 className="size-3.5" aria-hidden />
                        </button>
                      </td>
                    )}
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>

        <PaneFooter
          offset={offset}
          limit={limit}
          count={rows.length}
          total={total}
          durationMs={data.data?.durationMs}
          onOffset={setOffset}
          onLimit={(next) => {
            setLimit(next);
            setOffset(0);
          }}
        />
      </div>

      {editing !== null && (
        <RowDialog
          instanceId={instanceId}
          database={database}
          object={object}
          columns={schemaCols}
          primaryKey={primaryKey}
          dataColumns={columns}
          row={editing === "add" ? undefined : editing.row}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null);
            invalidate();
          }}
        />
      )}

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={t("db.browse.row.deleteTitle")}
        body={deleting !== null ? t("db.browse.row.deleteBody", { identity: identitySummary(deleting) }) : ""}
        confirmLabel={t("common.delete")}
        loading={del.isPending}
        onConfirm={() => deleting !== null && del.mutate(deleting)}
      />
    </div>
  );
}

/** One column's filter controls: a compact op picker plus the value input. */
function FilterCell({
  column,
  draft,
  onChange,
}: {
  column: string;
  draft: FilterDraft;
  onChange: (draft: FilterDraft, apply: boolean) => void;
}) {
  const { t } = useTranslation();
  const nullOp = draft.op === "is_null" || draft.op === "not_null";
  return (
    <th
      className="sticky top-8 z-10 px-2 py-1 font-normal"
      style={{ background: PANE.chrome, boxShadow: `inset 0 -1px 0 ${PANE.edge}` }}
    >
      <div className="flex min-w-28 items-center gap-1">
        <Select
          value={draft.op}
          onValueChange={(op) => onChange({ ...draft, op: op as DraftOp }, true)}
        >
          <SelectTrigger
            aria-label={t("db.browse.filter.opAria", { column })}
            className="h-6 w-auto min-w-13 shrink-0 gap-0.5 rounded border px-1.5 text-[11px] shadow-none"
            style={{ background: PANE.bg, color: PANE.dim, borderColor: PANE.edge }}
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {FILTER_OPS.map((op) => (
              <SelectItem key={op} value={op}>
                {t(`db.browse.filter.ops.${op}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <input
          value={nullOp ? "" : draft.value}
          disabled={nullOp}
          onChange={(e) => onChange({ ...draft, value: e.target.value }, false)}
          onKeyDown={(e: ReactKeyboardEvent<HTMLInputElement>) => {
            if (e.key === "Enter") onChange(draft, true);
          }}
          placeholder={nullOp ? undefined : t("db.browse.filter.placeholder")}
          aria-label={t("db.browse.filter.valueAria", { column })}
          autoComplete="off"
          spellCheck={false}
          className="h-6 w-full min-w-16 rounded border px-1.5 font-mono text-[11.5px] transition-colors focus:outline-none disabled:opacity-40"
          style={{ background: PANE.bg, borderColor: PANE.edge, color: PANE.text }}
        />
      </div>
    </th>
  );
}

/** RowDialog inserts or edits one SQL row — one input per column, NULL toggles. */
function RowDialog({
  instanceId,
  database,
  object,
  columns,
  primaryKey,
  dataColumns,
  row,
  onClose,
  onSaved,
}: {
  instanceId: string;
  database: string;
  object: string;
  columns: DbColumn[];
  primaryKey: string[];
  dataColumns: string[];
  row?: (string | null)[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const baseId = useId();
  const isEdit = row !== undefined;

  // Insert mode leaves auto_increment out entirely — the engine assigns it.
  const visible = useMemo(
    () => (isEdit ? columns : columns.filter((col) => !(col.extra ?? "").includes("auto_increment"))),
    [columns, isEdit],
  );

  const [values, setValues] = useState<Record<string, { text: string; isNull: boolean }>>(() => {
    const initial: Record<string, { text: string; isNull: boolean }> = {};
    for (const col of visible) {
      if (isEdit) {
        const idx = dataColumns.indexOf(col.name);
        const cell = idx >= 0 ? row[idx] : null;
        initial[col.name] = cell === null ? { text: "", isNull: true } : { text: cell, isNull: false };
      } else {
        initial[col.name] = { text: "", isNull: col.nullable };
      }
    }
    return initial;
  });
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  const save = useMutation({
    mutationFn: async (): Promise<number | null> => {
      const payload: Record<string, string | null> = {};
      for (const col of visible) {
        const state = values[col.name];
        payload[col.name] = state.isNull ? null : state.text;
      }
      if (isEdit) {
        const res = unwrap(
          await api.PATCH("/db/{id}/databases/{database}/objects/{object}/rows", {
            params: { path: { id: instanceId, database, object } },
            body: { identity: rowIdentity(dataColumns, row, primaryKey), values: payload },
          }),
        );
        return res.affected;
      }
      unwrap(
        await api.POST("/db/{id}/databases/{database}/objects/{object}/rows", {
          params: { path: { id: instanceId, database, object } },
          body: { values: payload },
        }),
      );
      return null;
    },
    onSuccess: (affected) => {
      toast(
        "success",
        affected === null
          ? t("db.browse.row.added")
          : t("db.browse.row.updated", { count: affected }),
      );
      onSaved();
    },
    onError: (err) => {
      if (isApiError(err) && err.status === 403) {
        toast("error", t("db.browse.row.failed"), err.message);
        return;
      }
      // 400/502 carry a useful engine message — keep the form open, verbatim.
      setErrorMsg(isApiError(err) ? err.message : t("common.unknownError"));
    },
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        wide
        title={isEdit ? t("db.browse.row.editTitle") : t("db.browse.row.addTitle")}
        description={t(isEdit ? "db.browse.row.editDescription" : "db.browse.row.addDescription", {
          database,
          object,
        })}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>
              {isEdit ? t("common.save") : t("db.browse.row.insert")}
            </Button>
          </>
        }
      >
        <div className="space-y-3.5">
          {visible.map((col) => {
            const state = values[col.name];
            const id = `${baseId}-${col.name}`;
            const auto = (col.extra ?? "").includes("auto_increment");
            return (
              <div key={col.name} className="space-y-1.5">
                <label htmlFor={id} className="flex flex-wrap items-center gap-1.5">
                  <span className="text-fg font-mono text-[13px] font-medium">{col.name}</span>
                  <span className="text-fg-subtle font-mono text-[11px]">{col.type}</span>
                  {col.isPrimaryKey && (
                    <Badge tone="warning" className="px-1.5 py-0 text-[10px]">
                      {t("db.browse.row.primaryKeyChip")}
                    </Badge>
                  )}
                  {auto && (
                    <Badge tone="accent" className="px-1.5 py-0 text-[10px]">
                      {t("db.browse.row.autoIncrementChip")}
                    </Badge>
                  )}
                </label>
                <div className="flex items-center gap-2.5">
                  <Input
                    id={id}
                    value={state.isNull ? "" : state.text}
                    disabled={state.isNull}
                    placeholder={!isEdit && col.default != null ? col.default : undefined}
                    onChange={(e) =>
                      setValues((prev) => ({
                        ...prev,
                        [col.name]: { text: e.target.value, isNull: false },
                      }))
                    }
                    autoComplete="off"
                    spellCheck={false}
                    className="font-mono text-[13px]"
                  />
                  {col.nullable && (
                    <label className="flex shrink-0 cursor-pointer items-center gap-1.5">
                      <Switch
                        checked={state.isNull}
                        onCheckedChange={(checked) =>
                          setValues((prev) => ({
                            ...prev,
                            [col.name]: { ...prev[col.name], isNull: checked },
                          }))
                        }
                        aria-label={`${col.name} ${t("db.browse.row.null")}`}
                      />
                      <span className="text-fg-subtle font-mono text-[11px]">
                        {t("db.browse.row.null")}
                      </span>
                    </label>
                  )}
                </div>
              </div>
            );
          })}
          {errorMsg !== null && (
            <p role="alert" className="text-danger text-[13px] break-words">
              {errorMsg}
            </p>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

/* ----------------------------- Mongo documents ---------------------------- */

function MongoDataTab({
  instanceId,
  database,
  object,
  info,
  canControl,
  onDuration,
}: {
  instanceId: string;
  database: string;
  object: string;
  info: DbObjectSchema;
  canControl: boolean;
  onDuration: (ms: number) => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();

  const [limit, setLimit] = useState(50);
  const [offset, setOffset] = useState(0);
  const [filterDraft, setFilterDraft] = useState("");
  const [filterApplied, setFilterApplied] = useState<string | null>(null);
  const [filterError, setFilterError] = useState<string | null>(null);
  const [sortDraft, setSortDraft] = useState("");
  const [sortApplied, setSortApplied] = useState<string | null>(null);
  const [dir, setDir] = useState<"asc" | "desc">("asc");

  const apply = () => {
    const trimmed = filterDraft.trim();
    if (trimmed !== "") {
      try {
        JSON.parse(trimmed);
      } catch {
        setFilterError(t("db.browse.mongo.invalidFilter"));
        return;
      }
    }
    setFilterError(null);
    setFilterApplied(trimmed === "" ? null : trimmed);
    setSortApplied(sortDraft.trim() === "" ? null : sortDraft.trim());
    setOffset(0);
  };

  const data = useQuery({
    queryKey: [
      "db",
      instanceId,
      "browse",
      database,
      object,
      "data",
      { limit, offset, filter: filterApplied, sortField: sortApplied, dir },
    ],
    queryFn: async () =>
      unwrap(
        await api.GET("/db/{id}/databases/{database}/objects/{object}/data", {
          params: {
            path: { id: instanceId, database, object },
            query: {
              limit,
              offset,
              ...(filterApplied !== null ? { filter: [filterApplied] } : {}),
              ...(sortApplied !== null ? { sortField: sortApplied, dir } : {}),
            },
          },
        }),
      ),
    placeholderData: keepPreviousData,
  });

  const durationMs = data.data?.durationMs;
  useEffect(() => {
    if (durationMs !== undefined) onDuration(durationMs);
  }, [durationMs, onDuration]);

  const documents = data.data?.documents ?? [];
  const total = data.data?.total ?? 0;

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ["db", instanceId, "browse", database, object] });
    void queryClient.invalidateQueries({ queryKey: ["db", instanceId, "databases"] });
  };

  // Dialog state: "insert", or the original document being edited.
  const [editing, setEditing] = useState<"insert" | { doc: string; id: string } | null>(null);
  const [deleting, setDeleting] = useState<string | null>(null);

  const del = useMutation({
    mutationFn: async (id: string) =>
      unwrap(
        await api.POST("/db/{id}/databases/{database}/objects/{object}/rows/delete", {
          params: { path: { id: instanceId, database, object } },
          body: { id },
        }),
      ),
    onSuccess: (res) => {
      toast("success", t("db.browse.doc.deleted", { count: res.affected }));
      setDeleting(null);
      invalidate();
    },
    onError: (err) =>
      toast("error", t("db.browse.doc.failed"), isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          value={filterDraft}
          onChange={(e) => setFilterDraft(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && apply()}
          placeholder={t("db.browse.mongo.filterPlaceholder")}
          aria-label={t("db.browse.mongo.filterAria")}
          aria-invalid={filterError !== null ? true : undefined}
          autoComplete="off"
          spellCheck={false}
          className="min-w-52 flex-1 font-mono text-[13px]"
        />
        <Input
          value={sortDraft}
          onChange={(e) => setSortDraft(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && apply()}
          placeholder={t("db.browse.mongo.sortPlaceholder")}
          aria-label={t("db.browse.mongo.sortAria")}
          autoComplete="off"
          spellCheck={false}
          className="w-40 font-mono text-[13px]"
        />
        <Select
          value={dir}
          onValueChange={(next) => {
            setDir(next as "asc" | "desc");
            setOffset(0);
          }}
        >
          <SelectTrigger className="w-32" aria-label={t("db.browse.mongo.dirAria")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="asc">{t("db.browse.mongo.dirAsc")}</SelectItem>
            <SelectItem value="desc">{t("db.browse.mongo.dirDesc")}</SelectItem>
          </SelectContent>
        </Select>
        {canControl && (
          <Button size="sm" variant="secondary" onClick={() => setEditing("insert")}>
            <Plus className="size-3.5" aria-hidden />
            {t("db.browse.insertDocument")}
          </Button>
        )}
      </div>
      {filterError !== null && (
        <p role="alert" className="text-danger text-[13px]">
          {filterError}
        </p>
      )}

      <div
        className="overflow-hidden rounded-lg border"
        style={{ background: PANE.bg, borderColor: PANE.edge }}
      >
        <div
          className={cn(
            "max-h-[65vh] overflow-y-auto transition-opacity",
            data.isPlaceholderData && "opacity-60",
          )}
        >
          {data.isLoading ? (
            <div className="flex items-center justify-center py-14">
              <Loader2 aria-hidden className="size-5 animate-spin" style={{ color: PANE.dim }} />
              <span className="sr-only">{t("common.loading")}</span>
            </div>
          ) : data.isError ? (
            <pre
              role="alert"
              className="overflow-x-auto px-4 py-3.5 font-mono text-[12.5px] whitespace-pre-wrap"
              style={{ color: PANE.error }}
            >
              {isApiError(data.error) ? data.error.message : t("common.unknownError")}
            </pre>
          ) : documents.length === 0 ? (
            <p className="px-4 py-10 text-center text-[13px]" style={{ color: PANE.dim }}>
              {t("db.browse.emptyDocs")}
            </p>
          ) : (
            documents.map((doc, i) => {
              const id = extractId(doc);
              return (
                <div
                  key={i}
                  className="flex items-start gap-2 px-3.5 py-3"
                  style={i > 0 ? { borderTop: `1px solid ${PANE.edge}` } : undefined}
                >
                  <pre
                    className="min-w-0 flex-1 overflow-x-auto font-mono text-xs leading-relaxed"
                    style={{ color: PANE.text }}
                  >
                    {prettyDocument(doc)}
                  </pre>
                  {canControl && id !== null && (
                    <div className="flex shrink-0 gap-1">
                      <button
                        aria-label={t("db.browse.doc.editAria")}
                        onClick={() => setEditing({ doc, id })}
                        className="cursor-pointer p-1 text-[#5b6272] transition-colors hover:text-[#ccd2de]"
                      >
                        <Pencil className="size-3.5" aria-hidden />
                      </button>
                      <button
                        aria-label={t("db.browse.doc.deleteAria")}
                        onClick={() => setDeleting(id)}
                        className="cursor-pointer p-1 text-[#5b6272] transition-colors hover:text-[#f87171]"
                      >
                        <Trash2 className="size-3.5" aria-hidden />
                      </button>
                    </div>
                  )}
                </div>
              );
            })
          )}
        </div>

        <PaneFooter
          offset={offset}
          limit={limit}
          count={documents.length}
          total={total}
          durationMs={data.data?.durationMs}
          onOffset={setOffset}
          onLimit={(next) => {
            setLimit(next);
            setOffset(0);
          }}
        />
      </div>

      {editing !== null && (
        <DocumentDialog
          instanceId={instanceId}
          database={database}
          object={object}
          sampleKeys={info.sampleKeys ?? []}
          original={editing === "insert" ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null);
            invalidate();
          }}
        />
      )}

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={t("db.browse.doc.deleteTitle")}
        body={deleting !== null ? t("db.browse.doc.deleteBody", { id: deleting }) : ""}
        confirmLabel={t("common.delete")}
        loading={del.isPending}
        onConfirm={() => deleting !== null && del.mutate(deleting)}
      />
    </div>
  );
}

/** DocumentDialog inserts or fully replaces one MongoDB document as JSON. */
function DocumentDialog({
  instanceId,
  database,
  object,
  sampleKeys,
  original,
  onClose,
  onSaved,
}: {
  instanceId: string;
  database: string;
  object: string;
  sampleKeys: string[];
  original: { doc: string; id: string } | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const isEdit = original !== null;

  const [text, setText] = useState(() => {
    if (original !== null) return prettyDocument(original.doc);
    // Seed the skeleton from the collection's sampled keys.
    const skeleton: Record<string, string> = {};
    for (const key of sampleKeys) if (key !== "_id") skeleton[key] = "";
    return JSON.stringify(skeleton, null, 2);
  });
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  const save = useMutation({
    mutationFn: async (): Promise<number | null> => {
      if (isEdit) {
        const res = unwrap(
          await api.PATCH("/db/{id}/databases/{database}/objects/{object}/rows", {
            params: { path: { id: instanceId, database, object } },
            body: { id: original.id, document: text },
          }),
        );
        return res.affected;
      }
      unwrap(
        await api.POST("/db/{id}/databases/{database}/objects/{object}/rows", {
          params: { path: { id: instanceId, database, object } },
          body: { document: text },
        }),
      );
      return null;
    },
    onSuccess: (affected) => {
      toast(
        "success",
        affected === null
          ? t("db.browse.doc.inserted")
          : t("db.browse.doc.updated", { count: affected }),
      );
      onSaved();
    },
    onError: (err) => {
      if (isApiError(err) && err.status === 403) {
        toast("error", t("db.browse.doc.failed"), err.message);
        return;
      }
      setErrorMsg(isApiError(err) ? err.message : t("common.unknownError"));
    },
  });

  const submit = () => {
    try {
      JSON.parse(text);
    } catch {
      setErrorMsg(t("db.browse.doc.invalidJson"));
      return;
    }
    setErrorMsg(null);
    save.mutate();
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        wide
        title={isEdit ? t("db.browse.doc.editTitle") : t("db.browse.doc.insertTitle")}
        description={t(isEdit ? "db.browse.doc.editDescription" : "db.browse.doc.insertDescription", {
          database,
          object,
        })}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button variant="primary" loading={save.isPending} onClick={submit}>
              {isEdit ? t("common.save") : t("db.browse.row.insert")}
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <Textarea
            value={text}
            onChange={(e) => setText(e.target.value)}
            aria-label={t("db.browse.doc.jsonAria")}
            autoComplete="off"
            autoCorrect="off"
            autoCapitalize="off"
            spellCheck={false}
            className="min-h-56 font-mono text-xs leading-relaxed"
          />
          {errorMsg !== null && (
            <p role="alert" className="text-danger text-[13px] break-words">
              {errorMsg}
            </p>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

/* ------------------------------- Pagination ------------------------------- */

function PaneFooter({
  offset,
  limit,
  count,
  total,
  durationMs,
  onOffset,
  onLimit,
}: {
  offset: number;
  limit: number;
  count: number;
  total: number;
  durationMs?: number;
  onOffset: (offset: number) => void;
  onLimit: (limit: number) => void;
}) {
  const { t } = useTranslation();
  const from = total === 0 ? 0 : offset + 1;
  const to = offset + count;

  return (
    <div
      className="flex flex-wrap items-center gap-x-4 gap-y-2 border-t px-3.5 py-1.5 text-xs"
      style={{ borderColor: PANE.edge, background: PANE.chrome, color: PANE.dim }}
    >
      <span>
        {t("db.browse.pagination.range", {
          from: from.toLocaleString(),
          to: to.toLocaleString(),
          total: total.toLocaleString(),
        })}
      </span>
      {durationMs !== undefined && <span>{t("db.console.duration", { ms: durationMs.toFixed(1) })}</span>}
      <div className="ml-auto flex items-center gap-2">
        <Select value={String(limit)} onValueChange={(next) => onLimit(Number(next))}>
          <SelectTrigger
            aria-label={t("db.browse.pagination.pageSize")}
            className="h-6 w-auto gap-1 rounded border px-1.5 text-[11px] shadow-none"
            style={{ background: PANE.bg, color: PANE.dim, borderColor: PANE.edge }}
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {PAGE_SIZES.map((size) => (
              <SelectItem key={size} value={String(size)}>
                {size}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <button
          aria-label={t("db.browse.pagination.prev")}
          disabled={offset === 0}
          onClick={() => onOffset(Math.max(0, offset - limit))}
          className="cursor-pointer p-1 text-[#5b6272] transition-colors hover:text-[#ccd2de] disabled:pointer-events-none disabled:opacity-40"
        >
          <ChevronLeft className="size-4" aria-hidden />
        </button>
        <button
          aria-label={t("db.browse.pagination.next")}
          disabled={offset + limit >= total}
          onClick={() => onOffset(offset + limit)}
          className="cursor-pointer p-1 text-[#5b6272] transition-colors hover:text-[#ccd2de] disabled:pointer-events-none disabled:opacity-40"
        >
          <ChevronRight className="size-4" aria-hidden />
        </button>
      </div>
    </div>
  );
}

/* -------------------------------- Structure ------------------------------- */

function StructureTab({
  instanceId,
  database,
  object,
  info,
  canDelete,
}: {
  instanceId: string;
  database: string;
  object: string;
  info: DbObjectSchema;
  /** Emptying or dropping an object is a delete, not an edit. */
  canDelete: boolean;
}) {
  const { t } = useTranslation();
  const isTable = info.kind === "table";
  const indexes = info.indexes ?? [];

  return (
    <div className="space-y-5">
      {isTable ? (
        <Card className="overflow-hidden">
          <CardHeader title={t("db.browse.structure.columns")} />
          <Table>
            <THead>
              <TR>
                <TH>{t("db.browse.structure.colName")}</TH>
                <TH>{t("db.browse.structure.colType")}</TH>
                <TH>{t("db.browse.structure.colNullable")}</TH>
                <TH>{t("db.browse.structure.colDefault")}</TH>
                <TH>{t("db.browse.structure.colKeys")}</TH>
              </TR>
            </THead>
            <TBody>
              {(info.columns ?? []).map((col) => (
                <TR key={col.name}>
                  <TD className="text-fg font-mono text-[13px] font-medium">{col.name}</TD>
                  <TD className="text-fg-muted font-mono text-[13px]">{col.type}</TD>
                  <TD className="text-fg-muted">
                    {col.nullable
                      ? t("db.browse.structure.nullableYes")
                      : t("db.browse.structure.nullableNo")}
                  </TD>
                  <TD className="text-fg-muted font-mono text-[13px]">
                    {col.default != null ? col.default : "—"}
                  </TD>
                  <TD>
                    <span className="flex flex-wrap items-center gap-1.5">
                      {col.isPrimaryKey && (
                        <Badge tone="warning">{t("db.browse.row.primaryKeyChip")}</Badge>
                      )}
                      {(col.extra ?? "").includes("auto_increment") && (
                        <Badge tone="accent">{t("db.browse.row.autoIncrementChip")}</Badge>
                      )}
                      {col.key && col.key !== "PRI" && <Badge tone="neutral">{col.key}</Badge>}
                      {!col.isPrimaryKey && !col.key && !(col.extra ?? "").includes("auto_increment") && (
                        <span className="text-fg-subtle">—</span>
                      )}
                    </span>
                  </TD>
                </TR>
              ))}
            </TBody>
          </Table>
        </Card>
      ) : (
        <Card>
          <CardHeader title={t("db.browse.structure.overview")} />
          <div className="space-y-3 px-5 py-4">
            <p className="text-fg-muted text-sm">
              {t("db.browse.estDocs", {
                count: info.rowEstimate ?? 0,
                formatted: (info.rowEstimate ?? 0).toLocaleString(),
              })}
            </p>
            <div>
              <p className="text-fg mb-1.5 text-[13px] font-medium">
                {t("db.browse.structure.sampleKeys")}
              </p>
              <div className="flex flex-wrap gap-1.5">
                {(info.sampleKeys ?? []).map((key) => (
                  <Badge key={key} tone="neutral" className="font-mono">
                    {key}
                  </Badge>
                ))}
              </div>
              <p className="text-fg-subtle mt-1.5 text-xs">
                {t("db.browse.structure.sampleKeysHint")}
              </p>
            </div>
          </div>
        </Card>
      )}

      <Card className="overflow-hidden">
        <CardHeader title={t("db.browse.structure.indexes")} />
        {indexes.length === 0 ? (
          <p className="text-fg-subtle px-5 py-4 text-[13px]">
            {t("db.browse.structure.noIndexes")}
          </p>
        ) : (
          <Table>
            <THead>
              <TR>
                <TH>{t("db.browse.structure.idxName")}</TH>
                <TH>{t("db.browse.structure.idxUnique")}</TH>
                <TH>{t("db.browse.structure.idxColumns")}</TH>
              </TR>
            </THead>
            <TBody>
              {indexes.map((idx) => (
                <TR key={idx.name}>
                  <TD className="text-fg font-mono text-[13px] font-medium">{idx.name}</TD>
                  <TD>
                    {idx.unique ? (
                      <Badge tone="accent">{t("db.browse.structure.idxUnique")}</Badge>
                    ) : (
                      <span className="text-fg-subtle">—</span>
                    )}
                  </TD>
                  <TD className="text-fg-muted font-mono text-[13px]">{idx.columns.join(", ")}</TD>
                </TR>
              ))}
            </TBody>
          </Table>
        )}
      </Card>

      {canDelete && (
        <DangerZone instanceId={instanceId} database={database} object={object} info={info} />
      )}
    </div>
  );
}

/* ------------------------------- Danger zone ------------------------------ */

function DangerZone({
  instanceId,
  database,
  object,
  info,
}: {
  instanceId: string;
  database: string;
  object: string;
  info: DbObjectSchema;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  const isTable = info.kind === "table";
  const formatted = (info.rowEstimate ?? 0).toLocaleString();
  const [confirming, setConfirming] = useState<"truncate" | "drop" | null>(null);

  const truncate = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/db/{id}/databases/{database}/objects/{object}/truncate", {
          params: { path: { id: instanceId, database, object } },
          body: { confirm: true },
        }),
      ),
    onSuccess: () => {
      toast("success", t("db.browse.danger.truncated", { object }));
      setConfirming(null);
      void queryClient.invalidateQueries({ queryKey: ["db", instanceId, "browse", database, object] });
      void queryClient.invalidateQueries({ queryKey: ["db", instanceId, "databases"] });
    },
    onError: (err) =>
      toast("error", t("db.browse.danger.failed"), isApiError(err) ? err.message : t("common.unknownError")),
  });

  const drop = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.DELETE("/db/{id}/databases/{database}/objects/{object}", {
          params: { path: { id: instanceId, database, object }, query: { confirm: true } },
        }),
      ),
    onSuccess: () => {
      toast("success", t("db.browse.danger.dropped", { object }));
      queryClient.removeQueries({ queryKey: ["db", instanceId, "browse", database, object] });
      void queryClient.invalidateQueries({ queryKey: ["db", instanceId, "databases"] });
      void navigate({ to: "/db/$instanceId", params: { instanceId } });
    },
    onError: (err) =>
      toast("error", t("db.browse.danger.failed"), isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <>
      <Card className="border-danger/40 overflow-hidden">
        <CardHeader title={<span className="text-danger">{t("db.browse.danger.title")}</span>} />
        <div className="divide-edge divide-y">
          {isTable && (
            <div className="flex flex-wrap items-center justify-between gap-3 px-5 py-4">
              <div className="min-w-0">
                <p className="text-fg text-sm font-medium">{t("db.browse.danger.truncate")}</p>
                <p className="text-fg-muted text-[13px]">
                  {t("db.browse.danger.truncateDesc", { formatted })}
                </p>
              </div>
              <Button variant="danger-ghost" size="sm" onClick={() => setConfirming("truncate")}>
                {t("db.browse.danger.truncate")}
              </Button>
            </div>
          )}
          <div className="flex flex-wrap items-center justify-between gap-3 px-5 py-4">
            <div className="min-w-0">
              <p className="text-fg text-sm font-medium">
                {t(isTable ? "db.browse.danger.dropTable" : "db.browse.danger.dropCollection")}
              </p>
              <p className="text-fg-muted text-[13px]">{t("db.browse.danger.dropDesc", { object })}</p>
            </div>
            <Button variant="danger-ghost" size="sm" onClick={() => setConfirming("drop")}>
              {t(isTable ? "db.browse.danger.dropTable" : "db.browse.danger.dropCollection")}
            </Button>
          </div>
        </div>
      </Card>

      {confirming === "truncate" && (
        <TypedConfirmDialog
          title={t("db.browse.danger.truncateConfirmTitle", { object })}
          body={t("db.browse.danger.truncateConfirmBody", { database, object, formatted })}
          verifyText={object}
          confirmLabel={t("db.browse.danger.truncate")}
          loading={truncate.isPending}
          onConfirm={() => truncate.mutate()}
          onClose={() => setConfirming(null)}
        />
      )}
      {confirming === "drop" && (
        <TypedConfirmDialog
          title={t("db.browse.danger.dropConfirmTitle", { object })}
          body={t(
            isTable
              ? "db.browse.danger.dropConfirmBodyTable"
              : "db.browse.danger.dropConfirmBodyCollection",
            { database, object, formatted },
          )}
          verifyText={object}
          confirmLabel={t(isTable ? "db.browse.danger.dropTable" : "db.browse.danger.dropCollection")}
          loading={drop.isPending}
          onConfirm={() => drop.mutate()}
          onClose={() => setConfirming(null)}
        />
      )}
    </>
  );
}

/** TypedConfirmDialog gates truly destructive actions behind typing the name. */
function TypedConfirmDialog({
  title,
  body,
  verifyText,
  confirmLabel,
  loading,
  onConfirm,
  onClose,
}: {
  title: string;
  body: string;
  verifyText: string;
  confirmLabel: string;
  loading: boolean;
  onConfirm: () => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [typed, setTyped] = useState("");
  const matches = typed === verifyText;

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={title}
        description={body}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button variant="danger" loading={loading} disabled={!matches} onClick={onConfirm}>
              {confirmLabel}
            </Button>
          </>
        }
      >
        <Field label={t("db.browse.danger.typeToConfirm", { name: verifyText })}>
          {(fieldProps) => (
            <Input
              {...fieldProps}
              autoFocus
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && matches && !loading) onConfirm();
              }}
              placeholder={t("db.browse.danger.confirmPlaceholder", { name: verifyText })}
              autoComplete="off"
              spellCheck={false}
              className="font-mono"
            />
          )}
        </Field>
      </DialogContent>
    </Dialog>
  );
}
