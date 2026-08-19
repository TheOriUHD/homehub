import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  Boxes,
  CheckCircle2,
  ChevronRight,
  MoreVertical,
  Pencil,
  Plug,
  Power,
  RefreshCw,
  Trash2,
  XCircle,
} from "lucide-react";
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
import { Badge, StatusDot } from "@/ui/badge";
import { ConfirmDialog } from "@/ui/confirm";
import {
  Dropdown,
  DropdownContent,
  DropdownItem,
  DropdownSeparator,
  DropdownTrigger,
} from "@/ui/dropdown";
import { EmptyState, ErrorState } from "@/ui/states";
import { SkeletonRows } from "@/ui/skeleton";
import { useToast } from "@/ui/toast";
import { AddSystemWizard, EditSystemDialog } from "./editor";

/**
 * The Systems page owns viewing, adding, editing, and removing every system
 * HomeHub is connected to. It's a compact status board: a health summary up
 * top, then collapsible per-type groups — a healthy group stays a single row
 * (with at-a-glance status dots) and only expands to its dense tiles when it
 * has an issue or the operator opens it, so the page never becomes a long
 * scroll. Per-system actions live in each tile's menu so it stays scannable.
 * Visibility is server-filtered; mutations are gated on system/integrations.
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
  // Per-type collapse override; unset = default (open only when the group has an
  // issue) so a healthy fleet collapses to one row per type instead of scrolling.
  const [openOverride, setOpenOverride] = useState<Record<string, boolean>>({});

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
  const healthy = rows.filter((r) => r.enabled && r.status === "connected").length;
  const issues = rows.filter((r) => r.enabled && r.status !== "connected").length;
  const disabledCount = rows.filter((r) => !r.enabled).length;

  // In-group sort: problems first, then healthy, then disabled; name within.
  const rank = (r: Integration) => (!r.enabled ? 2 : r.status === "connected" ? 1 : 0);

  // Group cards by type, known types first in catalog order.
  const typesPresent = [
    ...SYSTEM_TYPES.filter((type) => rows.some((row) => row.type === type)),
    ...[...new Set(rows.map((row) => row.type))].filter(
      (type) => !(SYSTEM_TYPES as readonly string[]).includes(type),
    ),
  ];

  return (
    <div className="space-y-5">
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
        <>
          <div className="flex flex-wrap gap-2">
            <Stat label={t("systems.summary.total")} value={rows.length} />
            <Stat label={t("systems.summary.healthy")} value={healthy} dot="bg-success" />
            <Stat label={t("systems.summary.issues")} value={issues} dot="bg-danger" alert={issues > 0} />
            <Stat label={t("systems.summary.disabled")} value={disabledCount} dot="bg-fg-subtle" />
          </div>

          <div className="space-y-1.5">
            {typesPresent.map((type) => {
              const TypeIcon = systemTypeIcon(type);
              const group = rows
                .filter((row) => row.type === type)
                .sort((a, b) => rank(a) - rank(b) || a.name.localeCompare(b.name));
              const groupIssues = group.filter((r) => r.enabled && r.status !== "connected").length;
              const groupDisabled = group.filter((r) => !r.enabled).length;
              const open = openOverride[type] ?? groupIssues > 0;
              return (
                <section key={type} className="border-edge overflow-hidden rounded-lg border">
                  <button
                    type="button"
                    onClick={() => setOpenOverride((o) => ({ ...o, [type]: !open }))}
                    className="hover:bg-bg-subtle flex w-full items-center gap-2.5 px-3 py-2 text-left transition-colors"
                  >
                    <ChevronRight className={cn("text-fg-subtle size-4 shrink-0 transition-transform", open && "rotate-90")} />
                    <TypeIcon className="text-fg-subtle size-4 shrink-0" />
                    <span className="text-fg text-[13.5px] font-semibold">{systemTypeLabel(t, type)}</span>
                    <span className="text-fg-subtle text-[12.5px]">{group.length}</span>
                    <div className="ml-auto flex items-center gap-2.5">
                      {groupIssues > 0 && (
                        <Badge tone="danger">
                          {groupIssues} {t("systems.summary.issues")}
                        </Badge>
                      )}
                      {groupDisabled > 0 && (
                        <span className="text-fg-subtle text-[12px]">
                          {groupDisabled} {t("systems.summary.disabled")}
                        </span>
                      )}
                      {/* health-at-a-glance dots (collapsed rows still show state) */}
                      <div className="hidden items-center gap-1 sm:flex">
                        {group.slice(0, 10).map((r) => (
                          <StatusDot
                            key={r.id}
                            tone={r.enabled ? statusTone[r.status] : "neutral"}
                            pulse={r.status === "connecting" || r.status === "unreachable"}
                          />
                        ))}
                        {group.length > 10 && <span className="text-fg-subtle text-[11px]">+{group.length - 10}</span>}
                      </div>
                    </div>
                  </button>
                  {open && (
                    <div className="border-edge grid gap-2.5 border-t p-2.5 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
                      {group.map((row) => (
                        <SystemTile
                          key={row.id}
                          row={row}
                          canConfigure={canConfigure}
                          canDelete={canDelete}
                          testResult={testResults[row.id]}
                          onTest={() => test.mutate(row.id)}
                          onEdit={() => setEditing(row)}
                          onDelete={() => setDeleting(row)}
                          onToggle={() => toggle.mutate(row)}
                        />
                      ))}
                    </div>
                  )}
                </section>
              );
            })}
          </div>
        </>
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

/** A compact health-summary pill. */
function Stat({
  label,
  value,
  dot,
  alert,
}: {
  label: string;
  value: number;
  dot?: string;
  alert?: boolean;
}) {
  return (
    <div
      className={cn(
        "border-edge bg-bg flex items-center gap-2 rounded-lg border px-3 py-1.5",
        alert && "border-danger/40 bg-danger-soft",
      )}
    >
      {dot && <span className={cn("size-2 rounded-full", dot)} />}
      <span className="text-fg text-sm font-semibold tabular-nums">{value}</span>
      <span className="text-fg-muted text-[12.5px]">{label}</span>
    </div>
  );
}

/** OpenModuleLink renders the typed link into a system's own module. */
function OpenModuleLink({ row }: { row: Integration }) {
  const { t } = useTranslation();
  const className = "text-accent text-[12.5px] font-medium hover:underline";
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
    case "truenas":
      return (
        <Link to="/truenas/$instanceId" params={{ instanceId: row.id }} className={className}>
          {label}
        </Link>
      );
    case "patchpilot":
      return (
        <Link to="/patchpilot/$instanceId" params={{ instanceId: row.id }} className={className}>
          {label}
        </Link>
      );
    default:
      return null;
  }
}

function SystemTile({
  row,
  canConfigure,
  canDelete,
  testResult,
  onTest,
  onEdit,
  onDelete,
  onToggle,
}: {
  row: Integration;
  canConfigure: boolean;
  canDelete: boolean;
  testResult?: TestConnectionResult;
  onTest: () => void;
  onEdit: () => void;
  onDelete: () => void;
  onToggle: () => void;
}) {
  const { t } = useTranslation();
  const unhealthy = row.enabled && row.status !== "connected";
  return (
    <Card className={cn("flex flex-col gap-1.5 p-3", unhealthy && "border-danger/30")}>
      <div className="flex items-start gap-2">
        <span className="mt-1 shrink-0">
          <StatusDot
            tone={statusTone[row.status]}
            pulse={row.status === "connecting" || row.status === "unreachable"}
          />
        </span>
        <div className="min-w-0 flex-1">
          <p className="text-fg truncate text-[13.5px] font-semibold">{row.name}</p>
          <p className="text-fg-subtle mt-0.5 truncate text-[11.5px]">
            {row.enabled ? t(`integrations.status.${row.status}`) : t("systems.disabledLabel")}
            {" · "}
            {formatRelative(row.statusChangedAt)}
          </p>
        </div>
        {(canConfigure || canDelete) && (
          <Dropdown>
            <DropdownTrigger asChild>
              <button
                type="button"
                aria-label={t("common.actions")}
                className="text-fg-subtle hover:text-fg hover:bg-bg-subtle -mt-0.5 -mr-1 rounded-md p-1"
              >
                <MoreVertical className="size-4" />
              </button>
            </DropdownTrigger>
            <DropdownContent align="end" className="w-44">
              {canConfigure && (
                <>
                  <DropdownItem onSelect={onTest}>
                    <RefreshCw className="size-4" />
                    {t("systems.test")}
                  </DropdownItem>
                  <DropdownItem onSelect={onEdit}>
                    <Pencil className="size-4" />
                    {t("common.edit")}
                  </DropdownItem>
                  <DropdownItem onSelect={onToggle}>
                    <Power className="size-4" />
                    {row.enabled ? t("systems.disable") : t("systems.enable")}
                  </DropdownItem>
                </>
              )}
              {canDelete && (
                <>
                  {canConfigure && <DropdownSeparator />}
                  <DropdownItem destructive onSelect={onDelete}>
                    <Trash2 className="size-4" />
                    {t("systems.remove")}
                  </DropdownItem>
                </>
              )}
            </DropdownContent>
          </Dropdown>
        )}
      </div>

      {unhealthy && row.statusMessage && (
        <p
          className="text-danger-soft-fg bg-danger-soft truncate rounded px-2 py-1 text-[11.5px]"
          title={row.statusMessage}
        >
          {row.statusMessage}
        </p>
      )}

      {testResult && (
        <p
          className={cn(
            "flex items-center gap-1 text-[11.5px]",
            testResult.ok ? "text-success-soft-fg" : "text-danger-soft-fg",
          )}
          role="status"
        >
          {testResult.ok ? (
            <CheckCircle2 className="size-3.5 shrink-0" />
          ) : (
            <XCircle className="size-3.5 shrink-0" />
          )}
          <span className="truncate" title={testResult.message}>
            {testResult.message}
          </span>
        </p>
      )}

      {row.enabled && <OpenModuleLink row={row} />}
    </Card>
  );
}
