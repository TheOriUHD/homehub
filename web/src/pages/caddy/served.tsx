import { useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { AlertTriangle, Globe } from "lucide-react";
import { api, unwrap } from "@/lib/api";
import { Badge } from "@/ui/badge";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";

/**
 * CaddyServedPage shows what the RUNNING config is actually serving — read
 * through the helper's projected config, which is the truth even when the
 * Caddyfile on disk has drifted or came from somewhere else entirely.
 */
export function CaddyServedPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/caddy/$instanceId/served" });

  const served = useQuery({
    queryKey: ["caddy", instanceId, "served"],
    queryFn: async () =>
      unwrap(await api.GET("/caddy/{id}/served", { params: { path: { id: instanceId } } })),
    refetchInterval: 30_000,
  });

  if (served.isLoading) return <PageLoader />;
  if (served.isError) {
    return (
      <Card>
        <ErrorState error={served.error} onRetry={() => void served.refetch()} />
      </Card>
    );
  }

  const data = served.data!;

  return (
    <div className="space-y-5">
      {data.upstreams.length === 0 && data.unresolvable.length === 0 && data.domainsWithoutProxy.length === 0 ? (
        <Card>
          <EmptyState icon={Globe} title={t("caddy.served.emptyTitle")} body={t("caddy.served.emptyBody")} />
        </Card>
      ) : (
        <>
          {data.upstreams.length > 0 && (
            <Card>
              <CardHeader title={t("caddy.served.proxiedTitle")} description={t("caddy.served.proxiedBody")} />
              <div className="overflow-x-auto">
                <Table>
                  <THead>
                    <TR>
                      <TH>{t("caddy.sites.domain")}</TH>
                      <TH>{t("caddy.served.upstream")}</TH>
                    </TR>
                  </THead>
                  <TBody>
                    {data.upstreams.map((u) => (
                      <TR key={u.domain + u.dial}>
                        <TD>
                          <span className="text-fg font-medium">{u.domain}</span>
                        </TD>
                        <TD>
                          <code className="font-mono text-[12.5px]">{u.dial}</code>
                        </TD>
                      </TR>
                    ))}
                  </TBody>
                </Table>
              </div>
            </Card>
          )}

          {data.unresolvable.length > 0 && (
            <Card>
              <CardHeader title={t("caddy.served.unresolvableTitle")} description={t("caddy.served.unresolvableBody")} />
              <CardBody className="space-y-1.5">
                {data.unresolvable.map((u) => (
                  <p key={u.domain + u.reason} className="text-[13px]">
                    <span className="text-fg font-medium">{u.domain}</span>{" "}
                    <span className="text-fg-muted">— {u.reason}</span>
                  </p>
                ))}
              </CardBody>
            </Card>
          )}

          {data.domainsWithoutProxy.length > 0 && (
            <Card>
              <CardHeader title={t("caddy.served.noProxyTitle")} description={t("caddy.served.noProxyBody")} />
              <CardBody>
                <span className="flex flex-wrap gap-1.5">
                  {data.domainsWithoutProxy.map((d) => (
                    <Badge key={d} tone="neutral">
                      {d}
                    </Badge>
                  ))}
                </span>
              </CardBody>
            </Card>
          )}
        </>
      )}

      {data.warnings.length > 0 && (
        <div className="bg-warning-soft text-warning-soft-fg space-y-1 rounded-lg px-4 py-3 text-[13px]" role="note">
          {data.warnings.map((w) => (
            <p key={w} className="flex items-start gap-2">
              <AlertTriangle className="mt-0.5 size-4 shrink-0" />
              {w}
            </p>
          ))}
        </div>
      )}
    </div>
  );
}
