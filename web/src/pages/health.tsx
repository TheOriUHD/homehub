import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Boxes, Database, ListChecks, Radio } from "lucide-react";
import { api, unwrap } from "@/lib/api";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { Badge, StatusDot } from "@/ui/badge";
import { EmptyState, ErrorState } from "@/ui/states";
import { Skeleton } from "@/ui/skeleton";
import { formatBytes, formatDuration } from "@/lib/utils";

export function HealthPage() {
  const { t } = useTranslation();

  const health = useQuery({
    queryKey: ["health"],
    queryFn: async () => unwrap(await api.GET("/health")),
    refetchInterval: 15_000,
  });

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-fg text-xl font-semibold tracking-tight">{t("health.title")}</h1>
        <p className="text-fg-muted mt-1 text-sm">{t("health.subtitle")}</p>
      </div>

      {health.isLoading ? (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {Array.from({ length: 4 }, (_, i) => (
            <Card key={i} className="p-5">
              <Skeleton className="h-4 w-20" />
              <Skeleton className="mt-3 h-7 w-28" />
            </Card>
          ))}
        </div>
      ) : health.isError ? (
        <Card>
          <ErrorState error={health.error} onRetry={() => void health.refetch()} />
        </Card>
      ) : health.data ? (
        <>
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <StatCard label={t("health.version")} value={`v${health.data.version}`} />
            <StatCard label={t("health.uptime")} value={formatDuration(health.data.uptimeSeconds)} />
            <StatCard label={t("health.databaseSize")} value={formatBytes(health.data.database.sizeBytes)} />
            <StatCard label={t("health.wsClients")} value={String(health.data.websocketClients)} />
          </div>

          <div className="grid gap-4 lg:grid-cols-2">
            <Card>
              <CardHeader
                title={
                  <span className="flex items-center gap-2">
                    <Database className="text-fg-subtle size-4" />
                    {t("health.database")}
                  </span>
                }
                actions={
                  health.data.database.reachable ? (
                    <Badge tone="success">
                      <StatusDot tone="success" /> {t("health.reachable")}
                    </Badge>
                  ) : (
                    <Badge tone="danger">
                      <StatusDot tone="danger" pulse /> {t("health.unreachable")}
                    </Badge>
                  )
                }
              />
              <CardBody>
                <dl className="text-fg-muted space-y-2 text-[13px]">
                  <Row label={t("health.databaseSize")} value={formatBytes(health.data.database.sizeBytes)} />
                  <Row label={t("health.databaseLatency")} value={`${health.data.database.latencyMs.toFixed(1)} ms`} />
                </dl>
              </CardBody>
            </Card>

            <Card>
              <CardHeader
                title={
                  <span className="flex items-center gap-2">
                    <ListChecks className="text-fg-subtle size-4" />
                    {t("health.jobs")}
                  </span>
                }
                actions={
                  health.data.jobs.failedRecent > 0 ? (
                    <Badge tone="warning">{health.data.jobs.failedRecent} {t("health.jobsFailed")}</Badge>
                  ) : undefined
                }
              />
              <CardBody>
                <dl className="text-fg-muted space-y-2 text-[13px]">
                  <Row label={t("health.jobsWorkers")} value={String(health.data.jobs.workers)} />
                  <Row label={t("health.jobsPending")} value={String(health.data.jobs.pending)} />
                  <Row label={t("health.jobsRunning")} value={String(health.data.jobs.running)} />
                  <Row label={t("health.jobsFailed")} value={String(health.data.jobs.failedRecent)} />
                </dl>
              </CardBody>
            </Card>
          </div>

          <Card>
            <CardHeader
              title={
                <span className="flex items-center gap-2">
                  <Boxes className="text-fg-subtle size-4" />
                  {t("health.integrations")}
                </span>
              }
            />
            {health.data.integrations.length === 0 ? (
              <EmptyState
                icon={Radio}
                title={t("health.noIntegrations.title")}
                body={t("health.noIntegrations.body")}
                className="py-10"
              />
            ) : (
              <CardBody>
                <ul className="space-y-2">
                  {health.data.integrations.map((integration) => (
                    <li key={integration.id} className="flex items-center gap-3 text-sm">
                      <StatusDot
                        tone={
                          integration.status === "connected"
                            ? "success"
                            : integration.status === "degraded"
                              ? "warning"
                              : "danger"
                        }
                      />
                      <span className="text-fg font-medium">{integration.name}</span>
                      <span className="text-fg-subtle text-xs">{integration.type}</span>
                      {integration.lastError && (
                        <span className="text-danger ml-auto truncate text-xs">{integration.lastError}</span>
                      )}
                    </li>
                  ))}
                </ul>
              </CardBody>
            )}
          </Card>
        </>
      ) : null}
    </div>
  );
}

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <Card className="p-5">
      <p className="text-fg-subtle text-xs font-semibold tracking-wide uppercase">{label}</p>
      <p className="text-fg mt-1.5 text-2xl font-semibold tracking-tight">{value}</p>
    </Card>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between">
      <dt>{label}</dt>
      <dd className="text-fg font-medium">{value}</dd>
    </div>
  );
}
