import { useQuery } from "@tanstack/react-query";
import { Link, Outlet, useParams, useRouterState } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { AlertTriangle, Container } from "lucide-react";
import { api, unwrap } from "@/lib/api";
import { useLiveInvalidate } from "@/lib/live";
import { cn } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { PageLoader } from "@/ui/misc";
import { ErrorState } from "@/ui/states";
import { Card } from "@/ui/card";
import { statusTone } from "@/pages/integrations";

export function DockerModulePage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/docker/$instanceId" });
  const pathname = useRouterState({ select: (s) => s.location.pathname });

  const integrations = useQuery({
    queryKey: ["integrations"],
    queryFn: async () => unwrap(await api.GET("/integrations")),
  });
  useLiveInvalidate(`integration:docker/${instanceId}`, [["integrations"]]);

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
        <ErrorState error={new Error(t("errors.notFound.body"))} />
      </Card>
    );
  }

  const base = `/docker/${instanceId}`;
  // The Containers tab is the index route; container detail lives beneath it.
  const tabs = [
    { to: base, label: t("docker.tabs.containers"), also: `${base}/containers` },
    { to: `${base}/stacks`, label: t("docker.tabs.stacks") },
    { to: `${base}/images`, label: t("docker.tabs.images") },
    { to: `${base}/volumes`, label: t("docker.tabs.volumes") },
    { to: `${base}/networks`, label: t("docker.tabs.networks") },
  ];

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
          <Container className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="text-fg flex items-center gap-2.5 text-xl font-semibold tracking-tight">
            {instance.name}
            <Badge tone={statusTone[instance.status]}>
              <StatusDot tone={statusTone[instance.status]} pulse={instance.status !== "connected"} />
              {t(`integrations.status.${instance.status}`)}
            </Badge>
          </h1>
          <p className="text-fg-muted text-sm">Docker</p>
        </div>
      </div>

      {(instance.status === "unreachable" || instance.status === "degraded") && (
        <div
          className={cn(
            "flex items-start gap-2.5 rounded-lg px-4 py-3 text-[13px]",
            instance.status === "unreachable"
              ? "bg-danger-soft text-danger-soft-fg"
              : "bg-warning-soft text-warning-soft-fg",
          )}
          role="alert"
        >
          <AlertTriangle className="mt-0.5 size-4 shrink-0" />
          {instance.status === "unreachable"
            ? t("docker.unreachableBanner", { message: instance.statusMessage })
            : t("docker.degradedBanner", { message: instance.statusMessage })}
        </div>
      )}

      <nav className="border-edge flex gap-1 overflow-x-auto border-b">
        {tabs.map((tab) => {
          const active = tab.also
            ? pathname === tab.to || pathname.startsWith(tab.also)
            : pathname.startsWith(tab.to);
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
