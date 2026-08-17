import { Fragment, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Activity, ChevronDown, ChevronRight } from "lucide-react";
import { api, unwrap, isApiError, type DbSession } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { formatDuration } from "@/lib/utils";
import { Card } from "@/ui/card";
import { Button } from "@/ui/button";
import { ConfirmDialog } from "@/ui/confirm";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { EmptyState, ErrorState } from "@/ui/states";
import { SkeletonRows } from "@/ui/skeleton";
import { useToast } from "@/ui/toast";
import { dbResource, useDbInstances } from "./shared";

function fmtTime(seconds: number): string {
  return seconds < 60 ? `${seconds}s` : formatDuration(seconds);
}

/**
 * The Sessions tab: live connections on the instance, refreshed every 10s.
 * Kill mirrors the control grant client-side; the server re-checks and a
 * stray 403 still just toasts.
 */
export function DbSessionsPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/db/$instanceId/sessions" });
  const { can } = useAuth();
  const toast = useToast();
  const queryClient = useQueryClient();

  const instances = useDbInstances();
  const instance = instances.data?.find((row) => row.id === instanceId);
  const canControl = instance ? can(dbResource(instance), "control") : false;

  const sessions = useQuery({
    queryKey: ["db", instanceId, "sessions"],
    queryFn: async () =>
      unwrap(await api.GET("/db/{id}/sessions", { params: { path: { id: instanceId } } })),
    refetchInterval: 10_000,
  });

  const [expandedId, setExpandedId] = useState<number | null>(null);
  const [killing, setKilling] = useState<DbSession | null>(null);

  const kill = useMutation({
    mutationFn: async (sessionId: number) =>
      unwrap(
        await api.POST("/db/{id}/sessions/{sessionId}/kill", {
          params: { path: { id: instanceId, sessionId } },
        }),
      ),
    onSuccess: (_res, sessionId) => {
      setKilling(null);
      toast("success", t("db.sessions.killed", { id: sessionId }));
      void queryClient.invalidateQueries({ queryKey: ["db", instanceId, "sessions"] });
    },
    onError: (err) =>
      toast(
        "error",
        t("db.sessions.killFailed"),
        isApiError(err) ? err.message : t("common.unknownError"),
      ),
  });

  if (sessions.isLoading) {
    return (
      <Card className="p-5">
        <SkeletonRows rows={3} />
      </Card>
    );
  }
  if (sessions.isError) {
    return (
      <Card>
        <ErrorState error={sessions.error} onRetry={() => void sessions.refetch()} />
      </Card>
    );
  }

  const list = sessions.data!;
  if (list.length === 0) {
    return (
      <Card>
        <EmptyState
          icon={Activity}
          title={t("db.sessions.empty.title")}
          body={t("db.sessions.empty.body")}
        />
      </Card>
    );
  }

  const columnCount = canControl ? 8 : 7;

  return (
    <>
      <Card className="overflow-hidden">
        <Table>
          <THead>
            <TR>
              <TH>{t("db.sessions.columns.id")}</TH>
              <TH>{t("db.sessions.columns.user")}</TH>
              <TH>{t("db.sessions.columns.database")}</TH>
              <TH>{t("db.sessions.columns.command")}</TH>
              <TH>{t("db.sessions.columns.time")}</TH>
              <TH>{t("db.sessions.columns.state")}</TH>
              <TH>{t("db.sessions.columns.query")}</TH>
              {canControl && <TH className="text-right">{t("common.actions")}</TH>}
            </TR>
          </THead>
          <TBody>
            {list.map((session) => {
              const expanded = expandedId === session.id;
              const Chevron = expanded ? ChevronDown : ChevronRight;
              return (
                <Fragment key={session.id}>
                  <TR>
                    <TD className="text-fg-muted font-mono text-[13px]">{session.id}</TD>
                    <TD>
                      {session.user && (
                        <span className="text-fg block text-[13px] font-medium">{session.user}</span>
                      )}
                      <span className="text-fg-subtle block font-mono text-xs">{session.client}</span>
                    </TD>
                    <TD className="text-fg-muted font-mono text-[13px]">{session.database || "—"}</TD>
                    <TD className="text-fg-muted text-[13px]">{session.command}</TD>
                    <TD className="text-fg-muted text-[13px] whitespace-nowrap tabular-nums">
                      {fmtTime(session.timeSec)}
                    </TD>
                    <TD className="text-fg-muted text-[13px]">{session.state || "—"}</TD>
                    <TD className="max-w-56">
                      {session.query ? (
                        <button
                          type="button"
                          onClick={() => setExpandedId(expanded ? null : session.id)}
                          aria-expanded={expanded}
                          aria-label={expanded ? t("db.sessions.hideQuery") : t("db.sessions.showQuery")}
                          className="text-fg-muted hover:text-fg flex w-full cursor-pointer items-center gap-1.5 text-left"
                        >
                          <Chevron className="text-fg-subtle size-3.5 shrink-0" aria-hidden />
                          <span className="truncate font-mono text-xs">{session.query}</span>
                        </button>
                      ) : (
                        <span className="text-fg-subtle">—</span>
                      )}
                    </TD>
                    {canControl && (
                      <TD className="text-right">
                        <Button size="sm" variant="danger-ghost" onClick={() => setKilling(session)}>
                          {t("db.sessions.kill")}
                        </Button>
                      </TD>
                    )}
                  </TR>
                  {expanded && session.query && (
                    <TR>
                      <TD colSpan={columnCount} className="bg-bg-subtle/50">
                        <pre className="text-fg overflow-x-auto font-mono text-xs whitespace-pre-wrap">
                          {session.query}
                        </pre>
                      </TD>
                    </TR>
                  )}
                </Fragment>
              );
            })}
          </TBody>
        </Table>
      </Card>

      <ConfirmDialog
        open={killing !== null}
        onOpenChange={(open) => !open && setKilling(null)}
        title={killing ? t("db.sessions.killConfirmTitle", { id: killing.id }) : ""}
        body={t("db.sessions.killConfirmBody")}
        confirmLabel={t("db.sessions.kill")}
        loading={kill.isPending}
        onConfirm={() => killing && kill.mutate(killing.id)}
      />
    </>
  );
}
