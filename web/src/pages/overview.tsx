import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Activity, FileClock, Layers, Shield, UserPlus } from "lucide-react";
import { api, unwrap } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { RES } from "@/lib/perms";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { Badge } from "@/ui/badge";
import { EmptyState } from "@/ui/states";
import { Skeleton } from "@/ui/skeleton";
import { formatRelative } from "@/lib/utils";

export function OverviewPage() {
  const { t } = useTranslation();
  const { me, can } = useAuth();

  const canHealth = can(RES.health);
  const canAudit = can(RES.audit);
  const canUsers = can(RES.users, "configure");
  const canRoles = can(RES.roles, "configure");

  const health = useQuery({
    queryKey: ["health"],
    queryFn: async () => unwrap(await api.GET("/health")),
    enabled: canHealth,
    refetchInterval: 30_000,
  });

  const audit = useQuery({
    queryKey: ["audit", "recent"],
    queryFn: async () => unwrap(await api.GET("/audit", { params: { query: { limit: 8 } } })),
    enabled: canAudit,
  });

  const anyCards = canHealth || canAudit || canUsers || canRoles;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-fg text-xl font-semibold tracking-tight">
          {t("overview.greeting", { name: me?.user.displayName.split(" ")[0] })}
        </h1>
        <p className="text-fg-muted mt-1 text-sm">{t("overview.subtitle")}</p>
      </div>

      {!anyCards ? (
        <Card>
          <EmptyState
            icon={Layers}
            title={t("overview.empty.title")}
            body={t("overview.empty.body")}
          />
        </Card>
      ) : (
        <div className="grid gap-4 lg:grid-cols-3">
          {canHealth && (
            <Card className="lg:col-span-1">
              <CardHeader
                title={t("overview.healthCard")}
                actions={
                  <Link to="/health" className="text-accent text-[13px] font-medium hover:underline">
                    {t("nav.health")} →
                  </Link>
                }
              />
              <CardBody>
                {health.isLoading ? (
                  <div className="space-y-2.5">
                    <Skeleton className="h-5 w-32" />
                    <Skeleton className="h-4 w-44" />
                  </div>
                ) : health.data ? (
                  <div className="space-y-3">
                    <div className="flex items-center gap-2">
                      {health.data.database.reachable && health.data.jobs.failedRecent === 0 ? (
                        <Badge tone="success">{t("overview.healthOk")}</Badge>
                      ) : (
                        <Badge tone="warning">{t("overview.healthIssues")}</Badge>
                      )}
                      <span className="text-fg-subtle text-xs">v{health.data.version}</span>
                    </div>
                    <dl className="text-fg-muted space-y-1.5 text-[13px]">
                      <div className="flex justify-between">
                        <dt>{t("health.jobsPending")}</dt>
                        <dd className="text-fg font-medium">{health.data.jobs.pending}</dd>
                      </div>
                      <div className="flex justify-between">
                        <dt>{t("health.jobsFailed")}</dt>
                        <dd className="text-fg font-medium">{health.data.jobs.failedRecent}</dd>
                      </div>
                      <div className="flex justify-between">
                        <dt>{t("health.wsClients")}</dt>
                        <dd className="text-fg font-medium">{health.data.websocketClients}</dd>
                      </div>
                    </dl>
                  </div>
                ) : null}
              </CardBody>
            </Card>
          )}

          {canAudit && (
            <Card className="lg:col-span-2">
              <CardHeader
                title={t("overview.recentActivity")}
                actions={
                  <Link to="/admin/audit" className="text-accent text-[13px] font-medium hover:underline">
                    {t("nav.audit")} →
                  </Link>
                }
              />
              <CardBody className="px-0 py-0">
                {audit.isLoading ? (
                  <div className="space-y-2.5 p-5">
                    <Skeleton className="h-4 w-full" />
                    <Skeleton className="h-4 w-5/6" />
                    <Skeleton className="h-4 w-4/6" />
                  </div>
                ) : !audit.data || audit.data.entries.length === 0 ? (
                  <p className="text-fg-subtle px-5 py-8 text-center text-[13px]">{t("overview.noActivity")}</p>
                ) : (
                  <ul>
                    {audit.data.entries.map((entry) => (
                      <li
                        key={entry.id}
                        className="border-edge flex items-center gap-3 border-b px-5 py-2.5 text-[13px] last:border-b-0"
                      >
                        <span className="text-fg min-w-0 flex-1 truncate">
                          <span className="font-medium">{entry.actorName}</span>{" "}
                          <span className="text-fg-muted font-mono text-xs">{entry.action}</span>
                        </span>
                        {entry.outcome !== "success" && (
                          <Badge tone={entry.outcome === "denied" ? "warning" : "danger"}>
                            {t(`audit.outcome.${entry.outcome}`)}
                          </Badge>
                        )}
                        <span className="text-fg-subtle shrink-0 text-xs">{formatRelative(entry.ts)}</span>
                      </li>
                    ))}
                  </ul>
                )}
              </CardBody>
            </Card>
          )}

          {(canUsers || canRoles) && (
            <Card className={canHealth && canAudit ? "lg:col-span-3" : "lg:col-span-2"}>
              <CardHeader title={t("overview.quickActions")} />
              <CardBody className="flex flex-wrap gap-3">
                {canUsers && (
                  <QuickAction to="/admin/users" icon={<UserPlus className="size-4" />} label={t("overview.addUser")} />
                )}
                {canRoles && (
                  <QuickAction to="/admin/roles" icon={<Shield className="size-4" />} label={t("overview.createRole")} />
                )}
                {canAudit && (
                  <QuickAction to="/admin/audit" icon={<FileClock className="size-4" />} label={t("overview.viewAudit")} />
                )}
                {canHealth && (
                  <QuickAction to="/health" icon={<Activity className="size-4" />} label={t("nav.health")} />
                )}
              </CardBody>
            </Card>
          )}
        </div>
      )}
    </div>
  );
}

function QuickAction({ to, icon, label }: { to: string; icon: React.ReactNode; label: string }) {
  return (
    <Link
      to={to}
      className="border-edge bg-surface hover:bg-surface-hover hover:border-edge-strong flex items-center gap-2 rounded-lg border px-3.5 py-2.5 text-[13px] font-medium shadow-1 transition-colors"
    >
      <span className="text-accent">{icon}</span>
      {label}
    </Link>
  );
}
