import { useQuery } from "@tanstack/react-query";
import { Outlet, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { AlertTriangle, Gamepad2 } from "lucide-react";
import { api, unwrap } from "@/lib/api";
import { useLiveInvalidate } from "@/lib/live";
import { cn } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { Card } from "@/ui/card";
import { statusTone } from "@/lib/systems";
import { OpenSystemLink } from "@/components/open-system";

export function AmpModulePage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/amp/$instanceId" });

  const integrations = useQuery({
    queryKey: ["integrations"],
    queryFn: async () => unwrap(await api.GET("/integrations")),
  });
  useLiveInvalidate(`integration:amp/${instanceId}`, [["integrations"]]);

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

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
          <Gamepad2 className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="text-fg flex items-center gap-2.5 text-xl font-semibold tracking-tight">
            {instance.name}
            <Badge tone={statusTone[instance.status]}>
              <StatusDot tone={statusTone[instance.status]} pulse={instance.status !== "connected"} />
              {t(`integrations.status.${instance.status}`)}
            </Badge>
          </h1>
          <p className="text-fg-muted text-sm">{t("amp.subtitle")}</p>
        </div>
        <OpenSystemLink instanceId={instanceId} />
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
          {t("amp.unreachableBanner", { message: instance.statusMessage })}
        </div>
      )}

      <Outlet />
    </div>
  );
}
