import { Link, Navigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { useAuth } from "@/lib/auth";
import { useLiveInvalidate } from "@/lib/live";
import {
  SYSTEMS_RES,
  statusTone,
  systemTypeIcon,
  systemTypeLabel,
  useIntegrations,
} from "@/lib/systems";
import type { Integration } from "@/lib/api";
import { Card } from "@/ui/card";
import { Badge, StatusDot } from "@/ui/badge";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";

type LandingType = "proxmox" | "homeassistant";

/** InstanceLink wraps a card in the typed route for its module. */
function InstanceLink({ row, children }: { row: Integration; children: React.ReactNode }) {
  const className = "block";
  return row.type === "proxmox" ? (
    <Link to="/pve/$instanceId" params={{ instanceId: row.id }} className={className}>
      {children}
    </Link>
  ) : (
    <Link to="/ha/$instanceId" params={{ instanceId: row.id }} className={className}>
      {children}
    </Link>
  );
}

/**
 * SystemTypeLanding is the minimal landing for types without a unified module
 * (Proxmox, Home Assistant): one card per instance linking into its module.
 * With exactly one instance it forwards straight to that module.
 */
export function SystemTypeLanding({ type }: { type: LandingType }) {
  const { t } = useTranslation();
  const { can } = useAuth();

  const integrations = useIntegrations();
  useLiveInvalidate("integrations", [["integrations"]]);

  if (integrations.isLoading) return <PageLoader />;
  if (integrations.isError) {
    return (
      <Card>
        <ErrorState error={integrations.error} onRetry={() => void integrations.refetch()} />
      </Card>
    );
  }

  const rows = (integrations.data ?? []).filter((row) => row.type === type && row.enabled);
  const Icon = systemTypeIcon(type);

  if (rows.length === 0) {
    return (
      <Card>
        <EmptyState
          icon={Icon}
          title={t("systems.landing.empty.title", { type: systemTypeLabel(t, type) })}
          body={t("systems.landing.empty.body")}
          action={
            can(SYSTEMS_RES, "configure") ? (
              <Link to="/systems" className="text-accent text-[13px] font-medium hover:underline">
                {t("systems.landing.manage")} →
              </Link>
            ) : undefined
          }
        />
      </Card>
    );
  }

  if (rows.length === 1) {
    const only = rows[0];
    return only.type === "proxmox" ? (
      <Navigate to="/pve/$instanceId" params={{ instanceId: only.id }} replace />
    ) : (
      <Navigate to="/ha/$instanceId" params={{ instanceId: only.id }} replace />
    );
  }

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
          <Icon className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="text-fg text-xl font-semibold tracking-tight">{systemTypeLabel(t, type)}</h1>
          <p className="text-fg-muted text-sm">{t("systems.landing.pickInstance")}</p>
        </div>
      </div>

      <div className="grid gap-3 md:grid-cols-2">
        {rows.map((row) => (
          <InstanceLink key={row.id} row={row}>
            <Card className="hover:border-accent p-4 transition-colors">
              <p className="text-fg flex items-center gap-2 text-sm font-semibold">
                <StatusDot tone={statusTone[row.status]} pulse={row.status !== "connected"} />
                <span className="truncate">{row.name}</span>
                <Badge tone={statusTone[row.status]} className="ml-auto">
                  {t(`integrations.status.${row.status}`)}
                </Badge>
              </p>
              {row.statusMessage && (
                <p className="text-danger-soft-fg mt-2 text-[13px]">{row.statusMessage}</p>
              )}
            </Card>
          </InstanceLink>
        ))}
      </div>
    </div>
  );
}
