import { useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Plus, Trash2 } from "lucide-react";
import { api, unwrap, isApiError } from "@/lib/api";
import { Button } from "@/ui/button";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { Checkbox } from "@/ui/misc";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { useToast } from "@/ui/toast";
import { PANE } from "./shared";

/*
 * Visual creation dialogs: a new database (SQL engines), a new table with a
 * proper column builder and live SQL preview (SQL), and a new collection
 * (MongoDB). Buttons rendering these are control-gated by the callers; the
 * server re-checks and audits every call, and upstream engine errors are
 * shown verbatim inside the dialog.
 */

/** Identifier rule mirrored client-side purely as guidance; the server decides. */
const NAME_RE = /^[A-Za-z0-9_]+$/;

/* ------------------------------ New database ------------------------------ */

export function CreateDatabaseDialog({
  instanceId,
  instanceName,
  onClose,
}: {
  instanceId: string;
  instanceName: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const valid = NAME_RE.test(name);

  const create = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/db/{id}/databases", {
          params: { path: { id: instanceId } },
          body: { name },
        }),
      ),
    onSuccess: () => {
      toast("success", t("db.create.database.created", { name }));
      void queryClient.invalidateQueries({ queryKey: ["db", instanceId, "databases"] });
      onClose();
    },
    onError: (err) => {
      if (isApiError(err) && err.status === 403) {
        toast("error", t("db.create.database.failed"), err.message);
        return;
      }
      // 400/502 carry the engine's or validator's real message — verbatim.
      setErrorMsg(isApiError(err) ? err.message : t("common.unknownError"));
    },
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={t("db.create.database.title")}
        description={t("db.create.database.description", { name: instanceName })}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button
              variant="primary"
              loading={create.isPending}
              disabled={!valid}
              onClick={() => create.mutate()}
            >
              {t("db.create.database.confirm")}
            </Button>
          </>
        }
      >
        <Field label={t("db.create.database.name")} hint={t("db.create.nameHint")} error={errorMsg}>
          {(fieldProps) => (
            <Input
              {...fieldProps}
              autoFocus
              value={name}
              onChange={(e) => {
                setName(e.target.value);
                setErrorMsg(null);
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter" && valid && !create.isPending) create.mutate();
              }}
              placeholder={t("db.create.database.placeholder")}
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

/* ----------------------------- New collection ----------------------------- */

export function CreateCollectionDialog({
  instanceId,
  database,
  onClose,
}: {
  instanceId: string;
  database: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const valid = name.trim() !== "";

  const create = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/db/{id}/databases/{database}/objects/create", {
          params: { path: { id: instanceId, database } },
          body: { name: name.trim() },
        }),
      ),
    onSuccess: () => {
      toast("success", t("db.create.collection.created", { name: name.trim() }));
      void queryClient.invalidateQueries({ queryKey: ["db", instanceId, "databases"] });
      onClose();
    },
    onError: (err) => {
      if (isApiError(err) && err.status === 403) {
        toast("error", t("db.create.collection.failed"), err.message);
        return;
      }
      setErrorMsg(isApiError(err) ? err.message : t("common.unknownError"));
    },
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={t("db.create.collection.title")}
        description={t("db.create.collection.description", { database })}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button
              variant="primary"
              loading={create.isPending}
              disabled={!valid}
              onClick={() => create.mutate()}
            >
              {t("db.create.collection.confirm")}
            </Button>
          </>
        }
      >
        <Field label={t("db.create.collection.name")} error={errorMsg}>
          {(fieldProps) => (
            <Input
              {...fieldProps}
              autoFocus
              value={name}
              onChange={(e) => {
                setName(e.target.value);
                setErrorMsg(null);
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter" && valid && !create.isPending) create.mutate();
              }}
              placeholder={t("db.create.collection.placeholder")}
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

/* ------------------------------- New table -------------------------------- */

/**
 * The offered column types. `param` marks types taking a length/precision the
 * user can edit inline; its value seeds the length input.
 */
const TYPE_DEFS = [
  { base: "INT" },
  { base: "BIGINT" },
  { base: "VARCHAR", param: "255" },
  { base: "TEXT" },
  { base: "BOOLEAN" },
  { base: "DATE" },
  { base: "DATETIME" },
  { base: "TIMESTAMP" },
  { base: "DECIMAL", param: "10,2" },
  { base: "FLOAT" },
  { base: "DOUBLE" },
  { base: "JSON" },
  { base: "BLOB" },
  { base: "CHAR", param: "36" },
  { base: "UUID" },
] as const;

type TypeBase = (typeof TYPE_DEFS)[number]["base"];

const AUTO_INC_TYPES: TypeBase[] = ["INT", "BIGINT"];

function typeParam(base: TypeBase): string | undefined {
  const def = TYPE_DEFS.find((entry) => entry.base === base);
  return def !== undefined && "param" in def ? def.param : undefined;
}

type ColumnDraft = {
  name: string;
  base: TypeBase;
  length: string;
  nullable: boolean;
  default: string;
  pk: boolean;
  ai: boolean;
};

function newColumn(partial?: Partial<ColumnDraft>): ColumnDraft {
  return {
    name: "",
    base: "VARCHAR",
    length: "255",
    nullable: false,
    default: "",
    pk: false,
    ai: false,
    ...partial,
  };
}

/** The full engine type string, e.g. "VARCHAR(255)" or "INT". */
function fullType(col: ColumnDraft): string {
  const param = typeParam(col.base);
  if (param === undefined) return col.base;
  return `${col.base}(${col.length.replace(/\s+/g, "")})`;
}

function lengthValid(col: ColumnDraft): boolean {
  if (typeParam(col.base) === undefined) return true;
  return col.base === "DECIMAL"
    ? /^\d+(\s*,\s*\d+)?$/.test(col.length)
    : /^\d+$/.test(col.length);
}

/** Bare keywords/numbers stay unquoted in the preview; everything else quotes. */
function renderDefault(value: string): string {
  const bare = /^(NULL|TRUE|FALSE)$/i.test(value) || /^CURRENT_TIMESTAMP(\(\d*\))?$/i.test(value);
  if (bare || /^-?\d+(\.\d+)?$/.test(value)) return value;
  return `'${value.replaceAll("'", "''")}'`;
}

/** Client-side CREATE TABLE approximation — the server builds the real one. */
function previewSql(database: string, table: string, columns: ColumnDraft[]): string {
  const q = (name: string) => `\`${name.replaceAll("`", "") || "…"}\``;
  const lines = columns.map((col) => {
    let sql = `${q(col.name)} ${fullType(col)}`;
    sql += col.nullable ? " NULL" : " NOT NULL";
    if (col.default.trim() !== "") sql += ` DEFAULT ${renderDefault(col.default.trim())}`;
    if (col.ai) sql += " AUTO_INCREMENT";
    return sql;
  });
  const pk = columns.filter((col) => col.pk).map((col) => q(col.name));
  if (pk.length > 0) lines.push(`PRIMARY KEY (${pk.join(", ")})`);
  return `CREATE TABLE ${q(database)}.${q(table)} (\n  ${lines.join(",\n  ")}\n);`;
}

export function CreateTableDialog({
  instanceId,
  database,
  onClose,
}: {
  instanceId: string;
  database: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();

  const [name, setName] = useState("");
  // Seeded with the conventional surrogate key; fully editable/removable.
  const [columns, setColumns] = useState<ColumnDraft[]>([
    newColumn({ name: "id", base: "INT", length: "", pk: true, ai: true }),
  ]);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  const patch = (index: number, changes: Partial<ColumnDraft>) => {
    setColumns((prev) => prev.map((col, i) => (i === index ? { ...col, ...changes } : col)));
    setErrorMsg(null);
  };

  const setBase = (index: number, base: TypeBase) => {
    // Switching type reseeds the length and drops auto-inc where it no longer applies.
    patch(index, {
      base,
      length: typeParam(base) ?? "",
      ...(AUTO_INC_TYPES.includes(base) ? {} : { ai: false }),
    });
  };

  const valid =
    NAME_RE.test(name) &&
    columns.length > 0 &&
    columns.every((col) => col.name.trim() !== "" && lengthValid(col));

  const sql = useMemo(() => previewSql(database, name, columns), [database, name, columns]);

  const create = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/db/{id}/databases/{database}/objects/create", {
          params: { path: { id: instanceId, database } },
          body: {
            name,
            columns: columns.map((col) => ({
              name: col.name.trim(),
              type: fullType(col),
              nullable: col.nullable,
              ...(col.default.trim() !== "" ? { default: col.default.trim() } : {}),
              primaryKey: col.pk,
              autoIncrement: col.ai,
            })),
          },
        }),
      ),
    onSuccess: () => {
      toast("success", t("db.create.table.created", { name }));
      void queryClient.invalidateQueries({ queryKey: ["db", instanceId, "databases"] });
      onClose();
    },
    onError: (err) => {
      if (isApiError(err) && err.status === 403) {
        toast("error", t("db.create.table.failed"), err.message);
        return;
      }
      // 502 carries the engine's own message — shown verbatim in the dialog.
      setErrorMsg(isApiError(err) ? err.message : t("common.unknownError"));
    },
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        wide
        title={t("db.create.table.title")}
        description={t("db.create.table.description", { database })}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button
              variant="primary"
              loading={create.isPending}
              disabled={!valid}
              onClick={() => create.mutate()}
            >
              {t("db.create.table.confirm")}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label={t("db.create.table.name")} hint={t("db.create.nameHint")}>
            {(fieldProps) => (
              <Input
                {...fieldProps}
                autoFocus
                value={name}
                onChange={(e) => {
                  setName(e.target.value);
                  setErrorMsg(null);
                }}
                placeholder={t("db.create.table.placeholder")}
                autoComplete="off"
                spellCheck={false}
                className="font-mono"
              />
            )}
          </Field>

          <div className="space-y-2">
            <p className="text-fg text-[13px] font-medium">{t("db.create.table.columns")}</p>
            {columns.map((col, index) => (
              <ColumnRow
                key={index}
                col={col}
                removable={columns.length > 1}
                onPatch={(changes) => patch(index, changes)}
                onBase={(base) => setBase(index, base)}
                onRemove={() => {
                  setColumns((prev) => prev.filter((_, i) => i !== index));
                  setErrorMsg(null);
                }}
              />
            ))}
            <Button
              size="sm"
              variant="ghost"
              onClick={() => setColumns((prev) => [...prev, newColumn()])}
            >
              <Plus className="size-3.5" aria-hidden />
              {t("db.create.table.addColumn")}
            </Button>
          </div>

          <div>
            <p className="text-fg-subtle mb-1.5 text-xs font-semibold tracking-wide uppercase">
              {t("db.create.table.preview")}
            </p>
            <pre
              className="overflow-x-auto rounded-lg border px-3.5 py-3 font-mono text-xs leading-relaxed"
              style={{ background: PANE.bg, borderColor: PANE.edge, color: PANE.text }}
            >
              {sql}
            </pre>
            <p className="text-fg-subtle mt-1 text-xs">{t("db.create.table.previewHint")}</p>
          </div>

          {errorMsg !== null && (
            <p role="alert" className="text-danger font-mono text-[12.5px] break-words">
              {errorMsg}
            </p>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

/** One column's builder row: name, type (+length), then flags and default. */
function ColumnRow({
  col,
  removable,
  onPatch,
  onBase,
  onRemove,
}: {
  col: ColumnDraft;
  removable: boolean;
  onPatch: (changes: Partial<ColumnDraft>) => void;
  onBase: (base: TypeBase) => void;
  onRemove: () => void;
}) {
  const { t } = useTranslation();
  const hasParam = typeParam(col.base) !== undefined;
  const canAutoInc = AUTO_INC_TYPES.includes(col.base);

  return (
    <div className="border-edge bg-bg-subtle/50 space-y-2 rounded-lg border p-2.5">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          value={col.name}
          onChange={(e) => onPatch({ name: e.target.value })}
          placeholder={t("db.create.table.colNamePlaceholder")}
          aria-label={t("db.create.table.colName")}
          autoComplete="off"
          spellCheck={false}
          className="h-8 min-w-28 flex-1 font-mono text-[13px]"
        />
        <Select value={col.base} onValueChange={(base) => onBase(base as TypeBase)}>
          <SelectTrigger className="h-8 w-32 font-mono text-[12.5px]" aria-label={t("db.create.table.colType")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {TYPE_DEFS.map((def) => (
              <SelectItem key={def.base} value={def.base}>
                <span className="font-mono text-[12.5px]">
                  {"param" in def ? `${def.base}(…)` : def.base}
                </span>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {hasParam && (
          <Input
            value={col.length}
            onChange={(e) => onPatch({ length: e.target.value })}
            aria-label={t("db.create.table.colLength")}
            aria-invalid={lengthValid(col) ? undefined : true}
            autoComplete="off"
            spellCheck={false}
            className="h-8 w-18 font-mono text-[13px]"
          />
        )}
        {removable && (
          <Button
            size="icon"
            variant="ghost"
            className="ml-auto size-8 shrink-0"
            aria-label={t("db.create.table.removeColumn", { name: col.name || "?" })}
            onClick={onRemove}
          >
            <Trash2 className="size-3.5" aria-hidden />
          </Button>
        )}
      </div>
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <Input
          value={col.default}
          onChange={(e) => onPatch({ default: e.target.value })}
          placeholder={t("db.create.table.defaultPlaceholder")}
          aria-label={t("db.create.table.colDefault")}
          autoComplete="off"
          spellCheck={false}
          className="h-8 w-40 font-mono text-[13px]"
        />
        <Checkbox
          checked={col.nullable}
          onCheckedChange={(nullable) => onPatch({ nullable })}
          label={<span className="text-[13px]">{t("db.create.table.nullable")}</span>}
        />
        <Checkbox
          checked={col.pk}
          onCheckedChange={(pk) => onPatch({ pk })}
          label={<span className="text-[13px]">{t("db.create.table.primaryKey")}</span>}
        />
        <Checkbox
          checked={col.ai}
          disabled={!canAutoInc}
          onCheckedChange={(ai) => onPatch({ ai })}
          label={<span className="text-[13px]">{t("db.create.table.autoIncrement")}</span>}
        />
      </div>
    </div>
  );
}
