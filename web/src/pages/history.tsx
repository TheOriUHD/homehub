import { useCallback, useMemo, useState } from "react";
import { useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import {
  AlertTriangle,
  BellPlus,
  ChartSpline,
  ChevronDown,
  ChevronRight,
  X,
} from "lucide-react";
import { api, unwrap, isApiError, type MetricSeriesInfo } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { formatMetricValue } from "@/lib/metrics";
import { cn } from "@/lib/utils";
import {
  MultiSeriesChart,
  type ChartSeries,
  type EventMarker,
  type MultiSeriesPoint,
} from "@/components/multi-series-chart";
import { AlertRuleEditor } from "@/pages/alerts";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { Button } from "@/ui/button";
import { Checkbox } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { Skeleton, SkeletonRows } from "@/ui/skeleton";
import { Tooltip } from "@/ui/tooltip";
import { useToast } from "@/ui/toast";

const MAX_SERIES = 6;

const TIMEFRAMES = ["1h", "6h", "24h", "7d", "30d"] as const;
type Timeframe = (typeof TIMEFRAMES)[number];

const SPAN_MS: Record<Timeframe, number> = {
  "1h": 3_600_000,
  "6h": 6 * 3_600_000,
  "24h": 24 * 3_600_000,
  "7d": 7 * 86_400_000,
  "30d": 30 * 86_400_000,
};

type SelectedSeries = {
  resource: string;
  metric: string;
  unit: string;
  /** Palette slot 1..6 — assigned in pick order, kept until deselected. */
  colorIdx: number;
};

const seriesKey = (resource: string, metric: string) => `${resource}|${metric}`;

/** Series group = first two resource path segments, e.g. "proxmox/<id>". */
function groupPrefix(resource: string): string {
  return resource.split("/").slice(0, 2).join("/");
}

function subResource(resource: string): string {
  return resource.split("/").slice(2).join("/");
}

export function HistoryPage() {
  const { t } = useTranslation();
  const toast = useToast();
  const { can } = useAuth();
  const queryClient = useQueryClient();

  const catalog = useQuery({
    queryKey: ["metrics", "series"],
    queryFn: async () => unwrap(await api.GET("/metrics/series")),
    staleTime: 60_000,
  });
  const integrations = useQuery({
    queryKey: ["integrations"],
    queryFn: async () => unwrap(await api.GET("/integrations")),
    staleTime: 30_000,
  });

  const [selected, setSelected] = useState<SelectedSeries[]>([]);
  const [timeframe, setTimeframe] = useState<Timeframe>("24h");
  const [pickerOpen, setPickerOpen] = useState(false);
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());
  const [scrub, setScrub] = useState<{ ts: number; pinned: boolean } | null>(null);
  const [alertPrefill, setAlertPrefill] = useState<{ resource: string; metric: string } | null>(
    null,
  );

  const range = useMemo(() => {
    const to = Date.now();
    return { from: new Date(to - SPAN_MS[timeframe]).toISOString(), to: new Date(to).toISOString() };
  }, [timeframe]);

  // Groups by integration prefix, integration display names resolved where possible.
  const groups = useMemo(() => {
    const byPrefix = new Map<string, MetricSeriesInfo[]>();
    for (const info of catalog.data ?? []) {
      const prefix = groupPrefix(info.resource);
      if (!byPrefix.has(prefix)) byPrefix.set(prefix, []);
      byPrefix.get(prefix)!.push(info);
    }
    return [...byPrefix.entries()]
      .map(([prefix, entries]) => {
        const id = prefix.split("/")[1];
        const integration = (integrations.data ?? []).find((i) => i.id === id);
        return { prefix, label: integration?.name ?? prefix, entries };
      })
      .sort((a, b) => a.label.localeCompare(b.label));
  }, [catalog.data, integrations.data]);

  const groupLabelByPrefix = useMemo(
    () => new Map(groups.map((g) => [g.prefix, g.label])),
    [groups],
  );

  // One /metrics/query per picked series. `combine` returns only structurally
  // shareable data so chart identities stay stable across unrelated renders.
  const seriesResults = useQueries({
    queries: selected.map((sel) => ({
      queryKey: ["metrics", "history", sel.resource, sel.metric, range.from, range.to],
      queryFn: async () =>
        unwrap(
          await api.GET("/metrics/query", {
            params: {
              query: { resource: sel.resource, metric: sel.metric, from: range.from, to: range.to },
            },
          }),
        ),
      staleTime: 60_000,
    })),
    combine: (results) => ({
      points: results.map((r) => r.data?.points ?? null),
      loading: results.map((r) => r.isLoading),
      failed: results.map((r) => r.isError),
    }),
  });

  // Audit entries under the selected series' integration prefixes become
  // event markers. Users without audit access simply see no markers.
  const prefixes = useMemo(
    () => [...new Set(selected.map((s) => groupPrefix(s.resource)))],
    [selected],
  );
  const auditResults = useQueries({
    queries: prefixes.map((prefix) => ({
      queryKey: ["history", "audit", prefix, range.from, range.to],
      queryFn: async () => {
        try {
          const res = unwrap(
            await api.GET("/audit", {
              params: { query: { resource: prefix, from: range.from, to: range.to, limit: 100 } },
            }),
          );
          return res.entries;
        } catch (err) {
          if (isApiError(err) && err.status === 403) return [];
          throw err;
        }
      },
      retry: false,
      staleTime: 60_000,
    })),
    combine: (results) => results.map((r) => r.data ?? null),
  });

  const eventMarkers = useMemo((): EventMarker[] => {
    const markers = auditResults
      .flatMap((entries) => entries ?? [])
      .map((e) => ({
        ts: new Date(e.ts).getTime(),
        label: e.actorName ? `${e.action} · ${e.actorName}` : e.action,
      }))
      .sort((a, b) => a.ts - b.ts);
    return markers.slice(-100);
  }, [auditResults]);

  const chartSeries = useMemo(
    (): ChartSeries[] =>
      selected.map((sel, i) => {
        const rest = subResource(sel.resource);
        const group = groupLabelByPrefix.get(groupPrefix(sel.resource)) ?? groupPrefix(sel.resource);
        return {
          key: seriesKey(sel.resource, sel.metric),
          label: `${group} · ${rest ? `${rest} · ` : ""}${sel.metric}`,
          unit: sel.unit,
          color: `--hh-series-${sel.colorIdx}`,
          points: (seriesResults.points[i] ?? []).map((p) => ({
            ts: new Date(p.ts).getTime(),
            value: p.avg,
          })),
        };
      }),
    [selected, seriesResults, groupLabelByPrefix],
  );

  /** How long ago a series stopped recording, or "" while it is still live. */
  const staleSince = (lastSeen: string | null | undefined): string => {
    if (!lastSeen) return t("history.staleNever");
    const age = Date.now() - new Date(lastSeen).getTime();
    if (age < 6 * 60 * 60 * 1000) return "";
    const days = Math.floor(age / 86_400_000);
    if (days >= 1) return t("history.staleDays", { count: days });
    return t("history.staleHours", { count: Math.floor(age / 3_600_000) });
  };

  const toggleSeries = (info: MetricSeriesInfo) => {
    const key = seriesKey(info.resource, info.metric);
    const exists = selected.some((s) => seriesKey(s.resource, s.metric) === key);
    if (!exists && selected.length >= MAX_SERIES) {
      toast("info", t("history.maxSeriesTitle"), t("history.maxSeriesBody", { max: MAX_SERIES }));
      return;
    }
    setSelected((prev) => {
      if (exists) return prev.filter((s) => seriesKey(s.resource, s.metric) !== key);
      const used = new Set(prev.map((s) => s.colorIdx));
      let colorIdx = 1;
      while (used.has(colorIdx)) colorIdx++;
      return [...prev, { resource: info.resource, metric: info.metric, unit: info.unit, colorIdx }];
    });
  };

  const handleScrub = useCallback((ts: number | null) => {
    setScrub((prev) => {
      if (prev?.pinned) return prev;
      if (ts == null) return prev == null ? prev : null;
      return prev?.ts === ts ? prev : { ts, pinned: false };
    });
  }, []);

  const handlePin = useCallback((ts: number) => {
    setScrub((prev) => (prev?.pinned && prev.ts === ts ? { ts, pinned: false } : { ts, pinned: true }));
  }, []);

  const anyLoading = seriesResults.loading.some(Boolean);
  const anyFailed = seriesResults.failed.some(Boolean);
  const anyData = chartSeries.some((s) => s.points.length > 0);

  const picker = (
    <Card className={cn(!pickerOpen && "hidden lg:block")}>
      <CardHeader title={t("history.picker.title")} description={t("history.picker.hint", { max: MAX_SERIES })} />
      <CardBody className="max-h-[28rem] space-y-1 overflow-y-auto px-2 py-2 lg:max-h-[60vh]">
        {groups.map((group) => {
          const isCollapsed = collapsed.has(group.prefix);
          const Chevron = isCollapsed ? ChevronRight : ChevronDown;
          return (
            <div key={group.prefix}>
              <button
                type="button"
                className="text-fg hover:bg-surface-hover flex w-full cursor-pointer items-center gap-1.5 rounded-md px-2 py-1.5 text-[13px] font-medium transition-colors"
                aria-expanded={!isCollapsed}
                onClick={() =>
                  setCollapsed((prev) => {
                    const next = new Set(prev);
                    if (!next.delete(group.prefix)) next.add(group.prefix);
                    return next;
                  })
                }
              >
                <Chevron className="text-fg-subtle size-3.5 shrink-0" />
                <span className="min-w-0 flex-1 truncate text-left">{group.label}</span>
                <span className="text-fg-subtle text-xs">{group.entries.length}</span>
              </button>
              {!isCollapsed && (
                <div className="space-y-1 py-1 pr-2 pl-7">
                  {group.entries.map((info) => {
                    const key = seriesKey(info.resource, info.metric);
                    const sel = selected.find((s) => seriesKey(s.resource, s.metric) === key);
                    const rest = subResource(info.resource);
                    const stale = staleSince(info.lastSeen);
                    return (
                      <div key={key} className="flex items-center gap-2">
                        <Checkbox
                          checked={sel !== undefined}
                          onCheckedChange={() => toggleSeries(info)}
                          label={
                            <span className={cn("font-mono text-xs", stale && "text-fg-subtle")}>
                              {rest ? `${rest} · ` : ""}
                              {info.metric}
                              {stale && (
                                // A removed integration keeps its catalog rows
                                // and stops recording. Without this the picker
                                // offers dead series as if they were live, and
                                // choosing one just draws an empty chart.
                                <span className="text-fg-subtle ml-1.5 font-sans not-italic">
                                  · {t("history.stale", { since: stale })}
                                </span>
                              )}
                            </span>
                          }
                        />
                        {sel && (
                          <span
                            aria-hidden
                            className="ml-auto size-2 shrink-0 rounded-full"
                            style={{ background: `var(--hh-series-${sel.colorIdx})` }}
                          />
                        )}
                      </div>
                    );
                  })}
                </div>
              )}
            </div>
          );
        })}
      </CardBody>
    </Card>
  );

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-fg text-xl font-semibold tracking-tight">{t("history.title")}</h1>
        <p className="text-fg-muted mt-1 text-sm">{t("history.subtitle")}</p>
      </div>

      {catalog.isLoading ? (
        <Card>
          <SkeletonRows rows={5} className="p-5" />
        </Card>
      ) : catalog.isError ? (
        <Card>
          <ErrorState error={catalog.error} onRetry={() => void catalog.refetch()} />
        </Card>
      ) : (catalog.data ?? []).length === 0 ? (
        <Card>
          <EmptyState
            icon={ChartSpline}
            title={t("history.emptyCatalog.title")}
            body={t("history.emptyCatalog.body")}
          />
        </Card>
      ) : (
        <div className="flex flex-col gap-4 lg:flex-row lg:items-start">
          <div className="space-y-3 lg:w-72 lg:shrink-0">
            <Button
              variant="secondary"
              className="w-full justify-between lg:hidden"
              aria-expanded={pickerOpen}
              onClick={() => setPickerOpen((v) => !v)}
            >
              {t("history.picker.toggle", { count: selected.length })}
              <ChevronDown className={cn("size-4 transition-transform", pickerOpen && "rotate-180")} />
            </Button>
            {picker}
          </div>

          <div className="min-w-0 flex-1 space-y-4">
            <div className="border-edge bg-surface inline-flex rounded-md border p-0.5 shadow-1">
              {TIMEFRAMES.map((tf) => (
                <button
                  key={tf}
                  type="button"
                  aria-pressed={timeframe === tf}
                  onClick={() => setTimeframe(tf)}
                  className={cn(
                    "cursor-pointer rounded-[5px] px-2.5 py-1 text-[13px] font-medium transition-colors",
                    timeframe === tf
                      ? "bg-accent-soft text-accent-soft-fg"
                      : "text-fg-muted hover:text-fg",
                  )}
                >
                  {t(`history.timeframe.${tf}`)}
                </button>
              ))}
            </div>

            {anyFailed && (
              <div
                className="bg-danger-soft text-danger-soft-fg flex items-center gap-2.5 rounded-lg px-4 py-3 text-[13px]"
                role="alert"
              >
                <AlertTriangle className="size-4 shrink-0" />
                <span className="flex-1">{t("history.partialError")}</span>
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={() =>
                    void queryClient.invalidateQueries({ queryKey: ["metrics", "history"] })
                  }
                >
                  {t("common.retry")}
                </Button>
              </div>
            )}

            <Card>
              <CardBody>
                {selected.length === 0 ? (
                  <EmptyState
                    icon={ChartSpline}
                    title={t("history.noSelection.title")}
                    body={t("history.noSelection.body")}
                  />
                ) : anyLoading && !anyData ? (
                  <Skeleton style={{ height: 260 }} />
                ) : !anyData ? (
                  <EmptyState
                    icon={ChartSpline}
                    title={t("history.noData.title")}
                    body={t("history.noData.body")}
                  />
                ) : (
                  <MultiSeriesChart
                    series={chartSeries}
                    eventMarkers={eventMarkers}
                    range={{ from: new Date(range.from).getTime(), to: new Date(range.to).getTime() }}
                    height={280}
                    onScrub={handleScrub}
                    onScrubPin={handlePin}
                  />
                )}
              </CardBody>
            </Card>

            {selected.length > 0 && anyData && (
              <ScrubPanel
                scrub={scrub}
                timeframe={timeframe}
                series={chartSeries}
                selected={selected}
                canConfigure={(resource) => can(resource, "configure")}
                onUnpin={() => setScrub(null)}
                onAddAlert={(resource, metric) => setAlertPrefill({ resource, metric })}
              />
            )}
          </div>
        </div>
      )}

      <AlertRuleEditor
        open={alertPrefill !== null}
        onOpenChange={(open) => !open && setAlertPrefill(null)}
        rule={null}
        prefill={alertPrefill ?? undefined}
        catalog={catalog.data ?? []}
      />
    </div>
  );
}

/** Latest sample at or before the scrub time — never a future value. */
function valueAt(points: MultiSeriesPoint[], ts: number): number | null {
  let best: number | null = null;
  for (const p of points) {
    if (p.ts > ts) break;
    best = p.value;
  }
  return best;
}

/**
 * ScrubPanel shows "state at <time>" for every picked series while the user
 * hovers the chart; clicking the chart pins it in place.
 */
function ScrubPanel({
  scrub,
  timeframe,
  series,
  selected,
  canConfigure,
  onUnpin,
  onAddAlert,
}: {
  scrub: { ts: number; pinned: boolean } | null;
  timeframe: Timeframe;
  series: ChartSeries[];
  selected: SelectedSeries[];
  canConfigure: (resource: string) => boolean;
  onUnpin: () => void;
  onAddAlert: (resource: string, metric: string) => void;
}) {
  const { t } = useTranslation();

  const time = scrub
    ? new Date(scrub.ts).toLocaleString(
        undefined,
        SPAN_MS[timeframe] > 86_400_000
          ? { dateStyle: "medium", timeStyle: "short" }
          : { timeStyle: "short" },
      )
    : null;

  return (
    <Card>
      <CardHeader
        title={time ? t("history.scrub.stateAt", { time }) : t("history.scrub.title")}
        description={scrub ? undefined : t("history.scrub.hint")}
        actions={
          scrub?.pinned ? (
            <Button size="sm" variant="ghost" onClick={onUnpin}>
              <X className="size-3.5" />
              {t("history.scrub.unpin")}
            </Button>
          ) : undefined
        }
      />
      {scrub && (
        <CardBody className="px-0 py-0">
          <ul>
            {series.map((s, i) => {
              const sel = selected[i];
              const value = valueAt(s.points, scrub.ts);
              return (
                <li
                  key={s.key}
                  className="border-edge flex items-center gap-3 border-b px-5 py-2.5 last:border-b-0"
                >
                  <span
                    aria-hidden
                    className="size-2.5 shrink-0 rounded-[3px]"
                    style={{ background: `var(${s.color})` }}
                  />
                  <span className="text-fg min-w-0 flex-1 truncate text-[13px]">{s.label}</span>
                  <span className="text-fg text-[13px] font-medium tabular-nums">
                    {value == null ? "—" : formatMetricValue(s.unit, value)}
                  </span>
                  {canConfigure(sel.resource) && (
                    <Tooltip label={t("history.scrub.addAlert")}>
                      <Button
                        size="icon"
                        variant="ghost"
                        aria-label={t("history.scrub.addAlert")}
                        onClick={() => onAddAlert(sel.resource, sel.metric)}
                      >
                        <BellPlus className="size-3.5" />
                      </Button>
                    </Tooltip>
                  )}
                </li>
              );
            })}
          </ul>
        </CardBody>
      )}
    </Card>
  );
}
