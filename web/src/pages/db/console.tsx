import { useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from "react";
import { useMutation } from "@tanstack/react-query";
import { Navigate, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { AlertTriangle, Loader2, Play } from "lucide-react";
import { api, unwrap, isApiError, type DbQueryResult } from "@/lib/api";
import { Button } from "@/ui/button";
import { Textarea } from "@/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { useToast } from "@/ui/toast";
import { useDbDatabases, useDbInstances } from "./shared";

// The result area is a fixed dark surface in BOTH themes — like a terminal,
// not a themed card — mirroring the Minecraft console's palette.
const PANE = {
  bg: "#101319",
  chrome: "#161a22",
  edge: "#262b36",
  text: "#ccd2de",
  dim: "#5b6272",
  warn: "#fbbf24",
  error: "#f87171",
};

/** Radix Select items can't carry an empty value; NUL can't be a DB name. */
const NO_SCOPE = "\u0000";

/**
 * The SQL console: one statement at a time against the instance, optionally
 * scoped to a database. The tab only renders for SQL engines with a control
 * grant; the server still authorizes and audits every run.
 */
export function DbConsolePage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/db/$instanceId/console" });
  const toast = useToast();

  const instances = useDbInstances();
  const instance = instances.data?.find((row) => row.id === instanceId);
  const databases = useDbDatabases(instanceId);

  const [scope, setScope] = useState<string>(NO_SCOPE);
  const [statement, setStatement] = useState("");
  const [result, setResult] = useState<DbQueryResult | null>(null);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  // Session-local statement history, recalled with ↑ in an empty editor.
  const historyRef = useRef<string[]>([]);
  const indexRef = useRef<number | null>(null);

  const run = useMutation({
    mutationFn: async (input: { statement: string; database?: string }) =>
      unwrap(
        await api.POST("/db/{id}/query", {
          params: { path: { id: instanceId } },
          body: input,
        }),
      ),
    onSuccess: (res) => {
      setResult(res);
      setErrorMsg(null);
    },
    onError: (err) => {
      if (isApiError(err) && err.status === 403) {
        toast("error", t("db.console.runFailed"), err.message);
        return;
      }
      // 502 carries the engine's own message — shown verbatim.
      setErrorMsg(isApiError(err) ? err.message : t("common.unknownError"));
      setResult(null);
    },
  });

  if (instance && instance.engine !== "mysql") {
    return <Navigate to="/db/$instanceId" params={{ instanceId }} replace />;
  }

  const submit = () => {
    const trimmed = statement.trim();
    if (!trimmed || run.isPending) return;
    const history = historyRef.current;
    if (history[history.length - 1] !== trimmed) history.push(trimmed);
    indexRef.current = null;
    run.mutate({
      statement: trimmed,
      ...(scope !== NO_SCOPE ? { database: scope } : {}),
    });
  };

  const onKeyDown = (e: ReactKeyboardEvent<HTMLTextAreaElement>) => {
    if ((e.ctrlKey || e.metaKey) && e.key === "Enter") {
      e.preventDefault();
      submit();
      return;
    }
    const history = historyRef.current;
    if (e.key === "ArrowUp" && (statement === "" || indexRef.current !== null)) {
      if (history.length === 0) return;
      e.preventDefault();
      indexRef.current =
        indexRef.current === null ? history.length - 1 : Math.max(0, indexRef.current - 1);
      setStatement(history[indexRef.current]);
    } else if (e.key === "ArrowDown" && indexRef.current !== null) {
      e.preventDefault();
      if (indexRef.current < history.length - 1) {
        indexRef.current += 1;
        setStatement(history[indexRef.current]);
      } else {
        indexRef.current = null;
        setStatement("");
      }
    } else if (e.key !== "ArrowUp" && e.key !== "ArrowDown") {
      indexRef.current = null;
    }
  };

  return (
    <div className="space-y-3">
      <div
        className="text-warning-soft-fg bg-warning-soft flex items-start gap-2.5 rounded-md px-3.5 py-2.5 text-[13px]"
        role="note"
      >
        <AlertTriangle className="mt-0.5 size-3.5 shrink-0" aria-hidden />
        {t("db.console.warning")}
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <Select value={scope} onValueChange={setScope}>
          <SelectTrigger className="w-60" aria-label={t("db.console.scope")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={NO_SCOPE}>{t("db.console.noScope")}</SelectItem>
            {(databases.data ?? []).map((db) => (
              <SelectItem key={db.name} value={db.name}>
                {db.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button
          variant="primary"
          className="ml-auto"
          loading={run.isPending}
          disabled={statement.trim() === ""}
          onClick={submit}
        >
          {!run.isPending && <Play className="size-3.5" aria-hidden />}
          {t("db.console.run")}
        </Button>
      </div>

      <Textarea
        value={statement}
        onChange={(e) => setStatement(e.target.value)}
        onKeyDown={onKeyDown}
        placeholder={t("db.console.placeholder")}
        aria-label={t("db.console.statementAria")}
        autoComplete="off"
        autoCorrect="off"
        autoCapitalize="off"
        spellCheck={false}
        className="min-h-28 font-mono text-[13px]"
      />
      <p className="text-fg-subtle text-xs">
        {t("db.console.runHint")} · {t("db.console.historyHint")}
      </p>

      <ResultPane result={result} errorMsg={errorMsg} pending={run.isPending} />
    </div>
  );
}

function ResultPane({
  result,
  errorMsg,
  pending,
}: {
  result: DbQueryResult | null;
  errorMsg: string | null;
  pending: boolean;
}) {
  const { t } = useTranslation();
  const hasColumns = result !== null && result.columns.length > 0;

  return (
    <div
      className="overflow-hidden rounded-lg border"
      style={{ background: PANE.bg, borderColor: PANE.edge }}
    >
      {pending ? (
        <div className="flex items-center justify-center py-14">
          <Loader2 aria-hidden className="size-5 animate-spin" style={{ color: PANE.dim }} />
          <span className="sr-only">{t("common.loading")}</span>
        </div>
      ) : errorMsg !== null ? (
        <pre
          role="alert"
          className="overflow-x-auto px-4 py-3.5 font-mono text-[12.5px] whitespace-pre-wrap"
          style={{ color: PANE.error }}
        >
          {errorMsg}
        </pre>
      ) : result === null ? (
        <p className="px-4 py-10 text-center text-[13px]" style={{ color: PANE.dim }}>
          {t("db.console.idle")}
        </p>
      ) : (
        <>
          {result.truncated && (
            <div
              className="border-b px-3.5 py-2 text-[12.5px]"
              style={{ borderColor: PANE.edge, background: PANE.chrome, color: PANE.warn }}
              role="status"
            >
              {t("db.console.truncatedBanner", { count: result.rows.length })}
            </div>
          )}

          {hasColumns ? (
            <div className="max-h-96 overflow-auto">
              <table className="w-full border-collapse font-mono text-[12.5px]">
                <thead>
                  <tr>
                    {result.columns.map((col, i) => (
                      <th
                        key={i}
                        className="sticky top-0 px-3 py-1.5 text-left font-semibold whitespace-nowrap"
                        style={{ background: PANE.chrome, color: PANE.dim }}
                      >
                        {col}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {result.rows.map((row, i) => (
                    <tr key={i} style={{ borderTop: `1px solid ${PANE.edge}` }}>
                      {row.map((cell, j) => (
                        <td
                          key={j}
                          className="px-3 py-1 align-top whitespace-pre"
                          style={
                            cell === "NULL"
                              ? { color: PANE.dim, fontStyle: "italic" }
                              : { color: PANE.text }
                          }
                        >
                          {cell}
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <p className="px-4 py-6 text-[13px]" style={{ color: PANE.text }}>
              {t("db.console.rowsAffected", { count: result.rowsAffected })}
            </p>
          )}

          <div
            className="flex items-center justify-between gap-3 border-t px-3.5 py-1.5 text-xs"
            style={{ borderColor: PANE.edge, background: PANE.chrome, color: PANE.dim }}
          >
            <span>
              {hasColumns
                ? t("db.console.rows", { count: result.rows.length })
                : t("db.console.rowsAffected", { count: result.rowsAffected })}
            </span>
            <span>{t("db.console.duration", { ms: result.durationMs.toFixed(1) })}</span>
          </div>
        </>
      )}
    </div>
  );
}
