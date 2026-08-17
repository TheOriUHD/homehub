import { useQuery } from "@tanstack/react-query";
import { Link, Outlet, useRouterState } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ShieldCheck } from "lucide-react";
import { api, unwrap, type DnsInstance } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useLiveInvalidate } from "@/lib/live";
import { cn } from "@/lib/utils";
import { Card } from "@/ui/card";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";

/** Product names are not translated, matching "Proxmox VE" / "Docker". */
export const dnsTypeLabel: Record<DnsInstance["type"], string> = {
  pihole: "Pi-hole",
  adguard: "AdGuard Home",
};

/** dnsResource builds the RBAC/metrics path for an instance, e.g. "pihole/<id>". */
export function dnsResource(instance: Pick<DnsInstance, "type" | "id">): string {
  return `${instance.type}/${instance.id}`;
}

/**
 * useDnsInstances is the module's shared instance list — every tab reads the
 * same query key, so the module gate and the tabs never disagree.
 */
export function useDnsInstances() {
  return useQuery({
    queryKey: ["dns", "instances"],
    queryFn: async () => unwrap(await api.GET("/dns/instances")),
    refetchInterval: 30_000,
  });
}

export function DnsModulePage() {
  const { t } = useTranslation();
  const { can } = useAuth();
  const pathname = useRouterState({ select: (s) => s.location.pathname });

  const instances = useDnsInstances();
  useLiveInvalidate("integrations", [["dns", "instances"]]);

  if (instances.isLoading) return <PageLoader />;
  if (instances.isError) {
    return (
      <Card>
        <ErrorState error={instances.error} onRetry={() => void instances.refetch()} />
      </Card>
    );
  }

  const list = instances.data!;
  if (list.length === 0) {
    return (
      <Card>
        <EmptyState
          icon={ShieldCheck}
          title={t("dns.empty.title")}
          body={t("dns.empty.body")}
          action={
            can("system/integrations", "configure") ? (
              <Link to="/systems" className="text-accent text-[13px] font-medium hover:underline">
                {t("dns.empty.action")} →
              </Link>
            ) : undefined
          }
        />
      </Card>
    );
  }

  const online = list.filter((i) => i.status === "connected").length;
  const syncVisible = list.length >= 2 && list.some((i) => can(dnsResource(i), "configure"));

  const tabs: { to: string; label: string; exact?: boolean }[] = [
    { to: "/dns", label: t("dns.tabs.overview"), exact: true },
    { to: "/dns/queries", label: t("dns.tabs.queryLog") },
    { to: "/dns/lists", label: t("dns.tabs.lists") },
    { to: "/dns/clients", label: t("dns.tabs.clients") },
    ...(syncVisible ? [{ to: "/dns/sync", label: t("dns.tabs.sync") }] : []),
  ];

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
          <ShieldCheck className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="text-fg text-xl font-semibold tracking-tight">{t("dns.title")}</h1>
          <p className="text-fg-muted text-sm">
            {t("dns.instancesSummary", { online, total: list.length })}
          </p>
        </div>
      </div>

      <nav className="border-edge no-scrollbar flex gap-1 overflow-x-auto border-b">
        {tabs.map((tab) => {
          const active = tab.exact ? pathname === tab.to : pathname.startsWith(tab.to);
          return (
            <Link
              key={tab.to}
              to={tab.to}
              className={cn(
                "-mb-px shrink-0 border-b-2 px-3.5 py-2 text-[13px] font-medium transition-colors",
                active
                  ? "border-accent text-fg"
                  : "text-fg-muted hover:text-fg border-transparent",
              )}
            >
              {tab.label}
            </Link>
          );
        })}
      </nav>

      <Outlet />
    </div>
  );
}
