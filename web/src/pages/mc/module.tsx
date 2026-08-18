import { Link, Outlet, useNavigate, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { AlertTriangle, MoreHorizontal, Pickaxe, Wrench } from "lucide-react";
import { Button } from "@/ui/button";
import { Dropdown, DropdownContent, DropdownItem, DropdownTrigger } from "@/ui/dropdown";
import { useAuth } from "@/lib/auth";
import { useLiveInvalidate } from "@/lib/live";
import { statusTone, useIntegrations } from "@/lib/systems";
import { Badge, StatusDot } from "@/ui/badge";
import { Card } from "@/ui/card";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { mcResource, networkPlayers, useMcLive, useMcOverview } from "./shared";

/**
 * McModulePage is the NETWORK shell: one Minecraft integration is a whole
 * network (proxy + subservers), and this frame carries its name, status
 * and setup entry while the index route shows the server grid and nested
 * routes show one server each.
 */
export function McModulePage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/mc/$instanceId" });
  const navigate = useNavigate();
  const { can } = useAuth();

  const integrations = useIntegrations();
  useLiveInvalidate("integrations", [["integrations"]]);
  const overview = useMcOverview(instanceId);
  // One subscription for the whole module: status/players patch the overview
  // cache in place; companion connects/disconnects invalidate it.
  useMcLive(instanceId);

  if (integrations.isLoading) return <PageLoader />;
  if (integrations.isError) {
    return (
      <Card>
        <ErrorState error={integrations.error} onRetry={() => void integrations.refetch()} />
      </Card>
    );
  }
  const instance = integrations.data?.find((row) => row.id === instanceId);
  if (!instance) {
    return (
      <Card>
        <EmptyState icon={AlertTriangle} title={t("errors.notFound.title")} body={t("errors.notFound.body")} />
      </Card>
    );
  }

  const canConfigure = can(mcResource(instanceId), "configure");
  const servers = overview.data?.servers ?? [];
  const connected = servers.filter((s) => s.connected).length;
  const players = networkPlayers(servers).online;

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
          <Pickaxe className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="text-fg flex items-center gap-2.5 text-xl font-semibold tracking-tight">
            <Link to="/mc/$instanceId" params={{ instanceId }} className="hover:underline">
              {instance.name}
            </Link>
            <Badge tone={statusTone[instance.status]}>
              <StatusDot tone={statusTone[instance.status]} pulse={instance.status !== "connected"} />
              {t(`integrations.status.${instance.status}`)}
            </Badge>
          </h1>
          <p className="text-fg-muted text-sm">
            {t("mc.networkSubtitle", { connected, total: servers.length, players })}
          </p>
        </div>

        {canConfigure && (
          <Dropdown>
            <DropdownTrigger asChild>
              <Button size="sm" variant="ghost" aria-label={t("common.actions")}>
                <MoreHorizontal className="size-4" />
              </Button>
            </DropdownTrigger>
            <DropdownContent align="end">
              <DropdownItem
                onSelect={() => void navigate({ to: "/mc/$instanceId/setup", params: { instanceId } })}
              >
                <Wrench className="size-3.5" />
                {t("mc.tabs.setup")}
              </DropdownItem>
            </DropdownContent>
          </Dropdown>
        )}
      </div>

      <Outlet />
    </div>
  );
}
