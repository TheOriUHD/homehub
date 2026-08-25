import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, unwrap, type HaEntity } from "@/lib/api";
import { useMetricSeries } from "@/lib/metrics";
import { formatRelative } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { TimeSeriesChart, type ChartKind } from "@/components/time-series-chart";
import { stateLabel } from "./entity-card";
import { haEntityResource, haResource, isUnknownState } from "./shared";

/** Domains whose recent history the metrics engine records. */
const CHARTED_DOMAINS = ["sensor", "binary_sensor", "climate"];

/**
 * EntityDetailDialog shows the full state of one entity: live state, history
 * charts where the metrics engine has series for it, and every attribute.
 */
export function EntityDetailDialog({
  instanceId,
  entity,
  onClose,
}: {
  instanceId: string;
  entity: HaEntity;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const unit = typeof entity.attributes.unit_of_measurement === "string"
    ? entity.attributes.unit_of_measurement
    : "";

  const attributes = Object.entries(entity.attributes).sort(([a], [b]) => a.localeCompare(b));

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent wide title={entity.name} description={<span className="font-mono">{entity.entityId}</span>}>
        <div className="space-y-5">
          <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
            <Badge tone={isUnknownState(entity.state) ? "neutral" : "accent"}>
              {stateLabel(t, entity.state)}
              {unit && ` ${unit}`}
            </Badge>
            <span className="text-fg-subtle text-xs">
              {t("ha.detail.lastChanged", { time: formatRelative(entity.lastChanged) })}
            </span>
            <span className="text-fg-subtle text-xs">
              {t("ha.detail.lastUpdated", { time: formatRelative(entity.lastUpdated) })}
            </span>
          </div>

          {CHARTED_DOMAINS.includes(entity.domain) && (
            <EntityHistory instanceId={instanceId} entity={entity} />
          )}

          <div>
            <p className="text-fg-subtle mb-2 text-xs font-semibold tracking-wide uppercase">
              {t("ha.detail.attributes")}
            </p>
            {attributes.length === 0 ? (
              <p className="text-fg-subtle text-[13px]">{t("ha.detail.noAttributes")}</p>
            ) : (
              <div className="border-edge overflow-hidden rounded-lg border">
                <Table>
                  <THead>
                    <tr>
                      <TH className="w-52">{t("ha.detail.attributeKey")}</TH>
                      <TH>{t("ha.detail.attributeValue")}</TH>
                    </tr>
                  </THead>
                  <TBody>
                    {attributes.map(([key, value]) => (
                      <TR key={key}>
                        <TD className="align-top">
                          <code className="text-fg font-mono text-xs break-all">{key}</code>
                        </TD>
                        <TD>
                          <AttributeValue value={value} />
                        </TD>
                      </TR>
                    ))}
                  </TBody>
                </Table>
              </div>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function AttributeValue({ value }: { value: unknown }) {
  if (value === null || value === undefined) {
    return <span className="text-fg-subtle text-xs">—</span>;
  }
  if (typeof value === "object") {
    return (
      <pre className="text-fg-muted max-h-40 overflow-auto font-mono text-xs whitespace-pre-wrap">
        {JSON.stringify(value, null, 2)}
      </pre>
    );
  }
  return <code className="text-fg-muted font-mono text-xs break-all">{String(value)}</code>;
}

/**
 * EntityHistory discovers which metrics the engine holds for this entity via
 * the series catalog, then charts each one (last hour + live tail), the same
 * treatment as the docker container detail page.
 */
function EntityHistory({ instanceId, entity }: { instanceId: string; entity: HaEntity }) {
  const { t } = useTranslation();
  const resource = haEntityResource(instanceId, entity);

  const catalog = useQuery({
    queryKey: ["metrics", "series"],
    queryFn: async () => unwrap(await api.GET("/metrics/series")),
    staleTime: 60_000,
  });
  const series = (catalog.data ?? []).filter((s) => s.resource === resource);

  if (series.length === 0) {
    return (
      <p className="text-fg-subtle text-[13px]">
        {catalog.isLoading ? t("common.loading") : t("ha.detail.noHistory")}
      </p>
    );
  }

  return (
    <div className="space-y-4">
      {series.map((s) => (
        <MetricChart
          key={s.metric}
          instanceId={instanceId}
          resource={resource}
          metric={s.metric}
          unit={s.unit}
        />
      ))}
    </div>
  );
}

// Everything Home Assistant reports that is not a share or a size is a
// reading — °C, V, W, hPa, lx — which keeps its decimals and its own band.
function chartKind(unit: string): ChartKind {
  if (unit === "fraction") return "percent";
  if (unit === "bytes") return "bytes";
  return "decimal";
}

function MetricChart({
  instanceId,
  resource,
  metric,
  unit,
}: {
  instanceId: string;
  resource: string;
  metric: string;
  unit: string;
}) {
  const points = useMetricSeries(`metrics:${haResource(instanceId)}`, resource, metric);
  return (
    <div>
      <p className="text-fg-subtle mb-1 text-xs font-semibold uppercase">
        {metric.replace(/_/g, " ")}
        {unit && <span className="text-fg-subtle font-normal normal-case"> · {unit}</span>}
      </p>
      <TimeSeriesChart points={points} kind={chartKind(unit)} height={140} />
    </div>
  );
}
