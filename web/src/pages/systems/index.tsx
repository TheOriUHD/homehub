import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Boxes, CheckCircle2, Pencil, Plug, Trash2, XCircle } from "lucide-react";
import { api, unwrap, isApiError, type Integration, type TestConnectionResult } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useLiveInvalidate } from "@/lib/live";
import {
  SYSTEM_TYPES,
  SYSTEMS_RES,
  statusTone,
  systemTypeIcon,
  systemTypeLabel,
  useIntegrations,
} from "@/lib/systems";
import { cn, formatRelative } from "@/lib/utils";
import { Card } from "@/ui/card";
import { Button } from "@/ui/button";
import { StatusDot } from "@/ui/badge";
import { ConfirmDialog } from "@/ui/confirm";
import { Switch } from "@/ui/switch";
import { EmptyState, ErrorState } from "@/ui/states";
import { SkeletonRows } from "@/ui/skeleton";
import { useToast } from "@/ui/toast";
import { Tooltip } from "@/ui/tooltip";
import { AddSystemWizard, EditSystemDialog } from "./editor";

/**
 * The Systems page owns viewing, adding, editing, and removing every system
 * HomeHub is connected to. Visibility is server-filtered; mutations are gated
 * on the system/integrations grants.
 */
export function SystemsPage() {
  const { t } = useTranslation();
  const { can } = useAuth();
  const toast = useToast();
  const queryClient = useQueryClient();

  const integrations = useIntegrations();
  useLiveInvalidate("integrations", [["integrations"]]);

  const canConfigure = can(SYSTEMS_RES, "configure");
  const canDelete = can(SYSTEMS_RES, "delete");

  const [addOpen, setAddOpen] = useState(false);
  const [editing, setEditing] = useState<Integration | null>(null);
  const [deleting, setDeleting] = useState<Integration | null>(null);
  const [testResults, setTestResults] = useState<Record<string, TestConnectionResult>>({});

  const remove = useMutation({
    mutationFn: async (id: string) =>
      unwrap(await api.DELETE("/integrations/{id}", { params: { path: { id } } })),
    onSuccess: () => {
      setDeleting(null);
      toast("success", t("systems.removed"));
      void queryClient.invalidateQueries({ queryKey: ["integrations"] });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const toggle = useMutation({
    mutationFn: async (row: Integration) =>
      unwrap(
        await api.PATCH("/integrations/{id}", {
          params: { path: { id: row.id } },
          body: { enabled: !row.enabled },
        }),
      ),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["integrations"] }),
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const test = useMutation({
    mutationFn: async (id: string) =>
      unwrap(await api.POST("/integrations/{id}/test", { params: { path: { id } } })),
    onSuccess: (result, id) => {
      setTestResults((prev) => ({ ...prev, [id]: result }));
      void queryClient.invalidateQueries({ queryKey: ["integrations"] });
    },
    onError: (err, id) =>
      setTestResults((prev) => ({
        ...prev,
        [id]: { ok: false, message: isApiError(err) ? err.message : t("common.unknownError") },
      })),
  });

  const rows = integrations.data ?? [];
  // Group cards by type, known types first in catalog order.
  const typesPresent = [
    ...SYSTEM_TYPES.filter((type) => rows.some((row) => row.type === type)),
    ...[...new Set(rows.map((row) => row.type))].filter(
      (type) => !(SYSTEM_TYPES as readonly string[]).includes(type),
    ),
  ];

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-fg text-xl font-semibold tracking-tight">{t("systems.title")}</h1>
          <p className="text-fg-muted mt-1 text-sm">{t("systems.subtitle")}</p>
        </div>
        {canConfigure && (
          <Button variant="primary" onClick={() => setAddOpen(true)}>
            <Plug className="size-4" />
            {t("systems.add")}
          </Button>
        )}
      </div>

      {integrations.isLoading ? (
        <SkeletonRows rows={3} />
      ) : integrations.isError ? (
        <Card>
          <ErrorState error={integrations.error} onRetry={() => void integrations.refetch()} />
        </Card>
      ) : rows.length === 0 ? (
        <Card>
          <EmptyState
            icon={Boxes}
            title={t("systems.empty.title")}
            body={t("systems.empty.body")}
            action={
              canConfigure ? (
                <Button variant="primary" onClick={() => setAddOpen(true)}>
                  {t("systems.add")}
                </Button>
              ) : undefined
            }
          />
        </Card>
      ) : (
        typesPresent.map((type) => {
          const TypeIcon = systemTypeIcon(type);
          const group = rows.filter((row) => row.type === type);
          return (
            <section key={type} className="space-y-2.5">
              <h2 className="text-fg flex items-center gap-2 text-[15px] font-semibold">
                <TypeIcon className="text-fg-subtle size-4" />
                {systemTypeLabel(t, type)}
                <span className="text-fg-subtle text-[13px] font-normal">({group.length})</span>
              </h2>
              <div className="grid gap-3 md:grid-cols-2">
                {group.map((row) => (
                  <SystemCard
                    key={row.id}
                    row={row}
                    canConfigure={canConfigure}
                    canDelete={canDelete}
                    testResult={testResults[row.id]}
                    testing={test.isPending && test.variables === row.id}
                    toggling={toggle.isPending && toggle.variables?.id === row.id}
                    onTest={() => test.mutate(row.id)}
                    onEdit={() => setEditing(row)}
                    onDelete={() => setDeleting(row)}
                    onToggle={() => toggle.mutate(row)}
                  />
                ))}
              </div>
            </section>
          );
        })
      )}

      {addOpen && <AddSystemWizard onClose={() => setAddOpen(false)} />}
      {editing && <EditSystemDialog row={editing} onClose={() => setEditing(null)} />}
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={deleting ? t("systems.deleteConfirmTitle", { name: deleting.name }) : ""}
        body={t("systems.deleteConfirmBody")}
        confirmLabel={t("common.delete")}
        loading={remove.isPending}
        onConfirm={() => deleting && remove.mutate(deleting.id)}
      />
    </div>
  );
}

/** OpenModuleLink renders the typed link into a system's own module. */
function OpenModuleLink({ row }: { row: Integration }) {
  const { t } = useTranslation();
  const className = "text-accent text-[13px] font-medium hover:underline";
  const label = <>{t("systems.open")} →</>;
  switch (row.type) {
    case "proxmox":
      return (
        <Link to="/pve/$instanceId" params={{ instanceId: row.id }} className={className}>
          {label}
        </Link>
      );
    case "docker":
      return (
        <Link to="/docker/$instanceId" params={{ instanceId: row.id }} className={className}>
          {label}
        </Link>
      );
    case "homeassistant":
      return (
        <Link to="/ha/$instanceId" params={{ instanceId: row.id }} className={className}>
          {label}
        </Link>
      );
    case "minecraft":
      return (
        <Link to="/mc/$instanceId" params={{ instanceId: row.id }} className={className}>
          {label}
        </Link>
      );
    case "pihole":
    case "adguard":
      return (
        <Link to="/dns" className={className}>
          {label}
        </Link>
      );
    case "mysql":
    case "mongodb":
      return (
        <Link to="/db/$instanceId" params={{ instanceId: row.id }} className={className}>
          {label}
        </Link>
      );
    default:
      return null;
  }
}

function SystemCard({
  row,
  canConfigure,
  canDelete,
  testResult,
  testing,
  toggling,
  onTest,
  onEdit,
  onDelete,
  onToggle,
}: {
  row: Integration;
  canConfigure: boolean;
  canDelete: boolean;
  testResult?: TestConnectionResult;
  testing: boolean;
  toggling: boolean;
  onTest: () => void;
  onEdit: () => void;
  onDelete: () => void;
  onToggle: () => void;
}) {
  const { t } = useTranslation();
  return (
    <Card className="flex flex-col p-4">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="text-fg flex items-center gap-2 text-sm font-semibold">
            <StatusDot
              tone={statusTone[row.status]}
              pulse={row.status === "connecting" || row.status === "unreachable"}
            />
            <span className="truncate">{row.name}</span>
          </p>
          <p className="text-fg-subtle mt-0.5 text-xs">
            {systemTypeLabel(t, row.type)} · {t(`integrations.status.${row.status}`)} ·{" "}
            {formatRelative(row.statusChangedAt)}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {canConfigure && (
            <>
              <Tooltip label={row.enabled ? t("systems.disable") : t("systems.enable")}>
                <Switch
                  checked={row.enabled}
                  disabled={toggling}
                  onCheckedChange={onToggle}
                  aria-label={row.enabled ? t("systems.disable") : t("systems.enable")}
                  className="mr-1"
                />
              </Tooltip>
              <Tooltip label={t("common.edit")}>
                <Button variant="ghost" size="icon" aria-label={t("common.edit")} onClick={onEdit}>
                  <Pencil className="size-4" />
                </Button>
              </Tooltip>
            </>
          )}
          {canDelete && (
            <Tooltip label={t("systems.remove")}>
              <Button variant="ghost" size="icon" aria-label={t("systems.remove")} onClick={onDelete}>
                <Trash2 className="text-danger size-4" />
              </Button>
            </Tooltip>
          )}
        </div>
      </div>

      {row.statusMessage && (
        <p className="text-danger-soft-fg bg-danger-soft mt-3 rounded-md px-3 py-2 text-[13px]">
          {row.statusMessage}
        </p>
      )}

      <div className="mt-3 flex flex-wrap items-center gap-2">
        {row.enabled && <OpenModuleLink row={row} />}
        {canConfigure && (
          <Button size="sm" variant="ghost" className="ml-auto" loading={testing} onClick={onTest}>
            {testing ? t("systems.testing") : t("systems.test")}
          </Button>
        )}
      </div>

      {testResult && (
        <p
          className={cn(
            "mt-2 flex items-center gap-1.5 text-[13px]",
            testResult.ok ? "text-success-soft-fg" : "text-danger-soft-fg",
          )}
          role="status"
        >
          {testResult.ok ? (
            <CheckCircle2 className="size-3.5 shrink-0" />
          ) : (
            <XCircle className="size-3.5 shrink-0" />
          )}
          {testResult.message}
        </p>
      )}
    </Card>
  );
}
