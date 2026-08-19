import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, Navigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  ArrowDownUp,
  ArrowRight,
  Check,
  CheckCheck,
  ChevronDown,
  Layers,
  Radio,
  X,
} from "lucide-react";
import { api, isApiError, unwrap, type Signal } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { live } from "@/lib/ws";
import { cn, formatRelative } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Card } from "@/ui/card";
import { ConfirmDialog } from "@/ui/confirm";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { useToast } from "@/ui/toast";

/**
 * Signals: the global stream of everything HomeHub's monitors currently see
 * across every system — failures, warnings, notable info, and green
 * "recovered on its own" notes. Color-coded, dismissable (with a
 * confirmation once anything worse than info is involved), and deep: a
 * failed update run carries its per-host errors and log tails right here.
 */

export type Severity = "info" | "warning" | "critical" | "recovered";

export const SEV_ORDER: Severity[] = ["critical", "warning", "info", "recovered"];

export const sevTone: Record<Severity, { dot: string; text: string; ring: string; label: string }> = {
  critical: { dot: "bg-danger", text: "text-danger", ring: "border-danger/40", label: "critical" },
  warning: { dot: "bg-warning", text: "text-warning-soft-fg", ring: "border-warning/40", label: "warning" },
  info: { dot: "bg-accent", text: "text-accent", ring: "border-accent/30", label: "info" },
  recovered: { dot: "bg-success", text: "text-success-soft-fg", ring: "border-success/40", label: "recovered" },
};

type SignalGroup = { key: string; label: string; dot: string; worst?: number; signals: Signal[] };

export function useSignals(limit = 200) {
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: ["signals"],
    queryFn: async () => unwrap(await api.GET("/signals", { params: { query: { limit } } })),
    refetchInterval: 60_000,
  });
  // Live: any change re-fetches for everyone.
  useEffect(() => {
    return live.subscribe("signals", () => {
      void qc.invalidateQueries({ queryKey: ["signals"] });
    });
  }, [qc]);
  return q;
}

export function SignalsPage() {
  const { t } = useTranslation();
  const { me } = useAuth();
  const toast = useToast();
  const qc = useQueryClient();
  const signals = useSignals(500);
  const [filter, setFilter] = useState<Severity | "all">("all");
  const [groupBy, setGroupBy] = useState<"severity" | "system">("severity");
  const [sortBy, setSortBy] = useState<"recent" | "severity">("recent");
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [expanded, setExpanded] = useState<Set<number>>(new Set());
  // Pending dismissal awaiting confirmation (bulk or single with severity
  // above info).
  const [confirming, setConfirming] = useState<number[] | null>(null);

  const dismiss = useMutation({
    mutationFn: async (ids: number[]) =>
      unwrap(await api.POST("/signals/dismiss", { body: { ids } })),
    onSuccess: (_res, ids) => {
      setSelected((s) => {
        const next = new Set(s);
        ids.forEach((id) => next.delete(id));
        return next;
      });
      setConfirming(null);
      void qc.invalidateQueries({ queryKey: ["signals"] });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const items = signals.data?.items ?? [];
  const counts = signals.data?.counts;
  const visible = filter === "all" ? items : items.filter((s) => s.severity === filter);

  // Dismissing anything worse than info (warning/critical) asks first;
  // info and recovered go straight away.
  const needsConfirm = (ids: number[]) =>
    items.some((s) => ids.includes(s.id) && (s.severity === "warning" || s.severity === "critical"));

  const requestDismiss = (ids: number[]) => {
    if (ids.length === 0) return;
    if (needsConfirm(ids)) setConfirming(ids);
    else dismiss.mutate(ids);
  };

  const toggleSelect = (id: number) =>
    setSelected((s) => {
      const next = new Set(s);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const allVisibleSelected = visible.length > 0 && visible.every((s) => selected.has(s.id));

  // Organize the visible signals into sections — grouped by severity or by
  // the system that raised them — with a chosen sort inside each section.
  const groups = useMemo<SignalGroup[]>(() => {
    const sevRank = (s: Signal) => SEV_ORDER.indexOf((s.severity as Severity) ?? "info");
    const sortSignals = (arr: Signal[]) =>
      [...arr].sort((a, b) => {
        const byTime = +new Date(b.lastSeen) - +new Date(a.lastSeen);
        return sortBy === "severity" ? sevRank(a) - sevRank(b) || byTime : byTime;
      });

    if (groupBy === "severity") {
      return SEV_ORDER.map((sev) => {
        const sigs = sortSignals(visible.filter((s) => s.severity === sev));
        return { key: sev, label: t(`signals.sev.${sev}`), dot: sevTone[sev].dot, signals: sigs };
      }).filter((g) => g.signals.length > 0);
    }

    const bySystem = new Map<string, Signal[]>();
    for (const s of visible) {
      const label = s.sourceName || s.sourceType || t("signals.system");
      const arr = bySystem.get(label);
      if (arr) arr.push(s);
      else bySystem.set(label, [s]);
    }
    return [...bySystem.entries()]
      .map(([label, sigs]) => {
        const sorted = sortSignals(sigs);
        const worst = Math.min(...sorted.map(sevRank));
        return { key: label, label, dot: sevTone[SEV_ORDER[worst]].dot, worst, signals: sorted };
      })
      .sort((a, b) => (a.worst ?? 3) - (b.worst ?? 3) || b.signals.length - a.signals.length);
  }, [visible, groupBy, sortBy, t]);

  // Signals are hidden for this account (per-user or by role) — send them home.
  if (me?.signalsHidden) return <Navigate to="/" replace />;
  if (signals.isLoading) return <PageLoader />;
  if (signals.isError) {
    return (
      <Card>
        <ErrorState error={signals.error} onRetry={() => void signals.refetch()} />
      </Card>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <div className="min-w-0 flex-1">
          <h1 className="text-fg text-xl font-semibold tracking-tight">{t("signals.title")}</h1>
          <p className="text-fg-muted mt-0.5 text-sm">{t("signals.subtitle")}</p>
        </div>
        {selected.size > 0 && (
          <Button size="sm" variant="secondary" onClick={() => requestDismiss([...selected])}>
            <CheckCheck className="size-3.5" />
            {t("signals.dismissSelected", { count: selected.size })}
          </Button>
        )}
      </div>

      {/* severity filter chips */}
      <div className="flex flex-wrap items-center gap-1.5">
        <FilterChip
          active={filter === "all"}
          onClick={() => setFilter("all")}
          label={t("signals.all", { count: items.length })}
        />
        {SEV_ORDER.map((sev) => (
          <FilterChip
            key={sev}
            active={filter === sev}
            onClick={() => setFilter(filter === sev ? "all" : sev)}
            label={`${counts?.[sev] ?? 0} ${t(`signals.sev.${sev}`)}`}
            dotClass={sevTone[sev].dot}
          />
        ))}
      </div>

      {/* organize: group + sort */}
      <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
        <div className="flex items-center gap-2">
          <Layers className="text-fg-subtle size-3.5" />
          <Segmented
            value={groupBy}
            onChange={setGroupBy}
            options={[
              { value: "severity", label: t("signals.bySeverity") },
              { value: "system", label: t("signals.bySystem") },
            ]}
          />
        </div>
        <div className="flex items-center gap-2">
          <ArrowDownUp className="text-fg-subtle size-3.5" />
          <Segmented
            value={sortBy}
            onChange={setSortBy}
            options={[
              { value: "recent", label: t("signals.sortRecent") },
              { value: "severity", label: t("signals.sortSeverity") },
            ]}
          />
        </div>
        <span className="flex-1" />
        {visible.length > 0 && (
          <button
            type="button"
            onClick={() =>
              setSelected(allVisibleSelected ? new Set() : new Set(visible.map((s) => s.id)))
            }
            className="text-fg-subtle hover:text-fg text-[12.5px]"
          >
            {allVisibleSelected ? t("signals.selectNone") : t("signals.selectAll")}
          </button>
        )}
      </div>

      {visible.length === 0 ? (
        <Card>
          <EmptyState
            icon={Radio}
            title={t("signals.empty.title")}
            body={t("signals.empty.body")}
          />
        </Card>
      ) : (
        <div className="space-y-4">
          {groups.map((g) => {
            const ids = g.signals.map((s) => s.id);
            const allSel = ids.every((id) => selected.has(id));
            return (
              <section key={g.key}>
                <div className="mb-1.5 flex items-center gap-2 px-1">
                  <span className={cn("size-2 shrink-0 rounded-full", g.dot)} />
                  <h2 className="text-fg text-[13px] font-semibold">{g.label}</h2>
                  <span className="text-fg-subtle text-[12px]">{g.signals.length}</span>
                  <button
                    type="button"
                    onClick={() =>
                      setSelected((prev) => {
                        const next = new Set(prev);
                        if (allSel) ids.forEach((id) => next.delete(id));
                        else ids.forEach((id) => next.add(id));
                        return next;
                      })
                    }
                    className="text-fg-subtle hover:text-fg ml-1.5 text-[11.5px]"
                  >
                    {allSel ? t("signals.selectNone") : t("signals.selectGroup")}
                  </button>
                </div>
                <Card className="overflow-hidden p-0">
                  <div className="divide-edge divide-y">
                    {g.signals.map((s) => (
                      <SignalRow
                        key={s.id}
                        signal={s}
                        selected={selected.has(s.id)}
                        expanded={expanded.has(s.id)}
                        onSelect={() => toggleSelect(s.id)}
                        onExpand={() =>
                          setExpanded((e) => {
                            const next = new Set(e);
                            if (next.has(s.id)) next.delete(s.id);
                            else next.add(s.id);
                            return next;
                          })
                        }
                        onDismiss={() => requestDismiss([s.id])}
                      />
                    ))}
                  </div>
                </Card>
              </section>
            );
          })}
        </div>
      )}

      <ConfirmDialog
        open={confirming !== null}
        onOpenChange={(o) => !o && setConfirming(null)}
        title={t("signals.confirmTitle", { count: confirming?.length ?? 0 })}
        body={t("signals.confirmBody")}
        confirmLabel={t("signals.dismiss")}
        loading={dismiss.isPending}
        onConfirm={() => confirming && dismiss.mutate(confirming)}
      />
    </div>
  );
}

function Segmented<T extends string>({
  value,
  onChange,
  options,
}: {
  value: T;
  onChange: (v: T) => void;
  options: { value: T; label: string }[];
}) {
  return (
    <div className="border-edge bg-bg-subtle inline-flex rounded-lg border p-0.5">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          onClick={() => onChange(o.value)}
          className={cn(
            "rounded-md px-2.5 py-1 text-[12px] font-medium transition-colors",
            value === o.value ? "bg-bg text-fg shadow-sm" : "text-fg-subtle hover:text-fg",
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

function FilterChip({
  active,
  onClick,
  label,
  dotClass,
}: {
  active: boolean;
  onClick: () => void;
  label: string;
  dotClass?: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-[12.5px] transition-colors",
        active
          ? "border-accent bg-accent-soft text-accent-soft-fg font-medium"
          : "border-edge text-fg-muted hover:text-fg hover:bg-bg-subtle",
      )}
    >
      {dotClass && <span className={cn("size-2 rounded-full", dotClass)} />}
      {label}
    </button>
  );
}

function SignalRow({
  signal: s,
  selected,
  expanded,
  onSelect,
  onExpand,
  onDismiss,
}: {
  signal: Signal;
  selected: boolean;
  expanded: boolean;
  onSelect: () => void;
  onExpand: () => void;
  onDismiss: () => void;
}) {
  const { t } = useTranslation();
  const sev = (s.severity as Severity) ?? "info";
  const tone = sevTone[sev];
  const hasDetail = s.detail !== undefined && s.detail !== null;

  return (
    <div className={cn("group", selected && "bg-accent-soft/20")}>
      <div className="flex items-start gap-3 px-4 py-3">
        <input
          type="checkbox"
          checked={selected}
          onChange={onSelect}
          className="accent-(--hh-accent) mt-1 size-3.5 shrink-0 cursor-pointer"
          aria-label={t("signals.select")}
        />
        <span className={cn("mt-1.5 size-2.5 shrink-0 rounded-full", tone.dot)} />
        <button
          type="button"
          onClick={hasDetail ? onExpand : undefined}
          className={cn("min-w-0 flex-1 text-left", hasDetail && "cursor-pointer")}
        >
          <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
            <span className="text-fg text-[13.5px] font-medium">{s.title}</span>
            {s.count > 1 && (
              <Badge tone="neutral">{t("signals.times", { count: s.count })}</Badge>
            )}
          </div>
          {s.body && <p className="text-fg-muted mt-0.5 text-[12.5px]">{s.body}</p>}
          <p className="text-fg-subtle mt-1 text-[11.5px]">
            {s.sourceName || s.sourceType}
            {" · "}
            {formatRelative(s.lastSeen)}
            {s.resolvedAt && (
              <span className="text-success-soft-fg"> · {t("signals.resolvedNote")}</span>
            )}
          </p>
        </button>
        <div className="flex shrink-0 items-center gap-1">
          {hasDetail && (
            <button
              type="button"
              onClick={onExpand}
              className="text-fg-subtle hover:text-fg rounded-md p-1.5"
              aria-label={t("signals.detail")}
            >
              <ChevronDown className={cn("size-4 transition-transform", expanded && "rotate-180")} />
            </button>
          )}
          <button
            type="button"
            onClick={onDismiss}
            className="text-fg-subtle hover:text-fg rounded-md p-1.5 opacity-0 transition-opacity group-hover:opacity-100"
            title={t("signals.dismiss")}
          >
            {sev === "recovered" || sev === "info" ? <Check className="size-4" /> : <X className="size-4" />}
          </button>
        </div>
      </div>
      {expanded && hasDetail && (
        <div className="border-edge bg-bg-subtle/50 border-t px-4 py-3 pl-12">
          <DetailView value={s.detail} />
        </div>
      )}
    </div>
  );
}

/**
 * DetailView renders a signal's deep payload generically: scalars as
 * key/value rows, "log" arrays as a console block, arrays of objects (e.g.
 * per-host failures) as nested blocks.
 */
function DetailView({ value, depth = 0 }: { value: unknown; depth?: number }) {
  if (value === null || value === undefined) return null;
  if (Array.isArray(value)) {
    // Array of strings → log/console block.
    if (value.every((v) => typeof v === "string")) {
      return (
        <pre className="border-edge bg-bg text-fg max-h-72 overflow-auto rounded-lg border px-3 py-2 font-mono text-[11.5px] leading-relaxed whitespace-pre-wrap">
          {(value as string[]).join("\n")}
        </pre>
      );
    }
    return (
      <div className="space-y-2">
        {value.map((v, i) => (
          <div key={i} className="border-edge rounded-lg border p-2.5">
            <DetailView value={v} depth={depth + 1} />
          </div>
        ))}
      </div>
    );
  }
  if (typeof value === "object") {
    const entries = Object.entries(value as Record<string, unknown>).filter(
      ([, v]) => v !== null && v !== undefined && v !== "",
    );
    return (
      <div className="space-y-1.5">
        {entries.map(([k, v]) => (
          <div key={k} className="text-[12.5px]">
            <span className="text-fg-subtle mr-2 font-medium">{k}</span>
            {typeof v === "object" ? (
              <div className="mt-1">
                <DetailView value={v} depth={depth + 1} />
              </div>
            ) : (
              <span className="text-fg break-all">{String(v)}</span>
            )}
          </div>
        ))}
      </div>
    );
  }
  return <span className="text-fg text-[12.5px]">{String(value)}</span>;
}

/* ---- overview panel --------------------------------------------------------- */

/**
 * The dashboard panel: the handful of most recent signals with severity
 * dots, count chips, and the little arrow that leads to the full page.
 */
export function SignalsPanel() {
  const { t } = useTranslation();
  const { me } = useAuth();
  const signals = useSignals(50);
  const items = signals.data?.items ?? [];
  const counts = signals.data?.counts;
  const top = items.slice(0, 5);

  // Hidden for this account — don't render the overview card at all.
  if (me?.signalsHidden) return null;

  return (
    <Card className="p-0">
      <div className="flex items-center gap-2 px-4 pt-3 pb-1.5">
        <Radio className="text-accent size-4" />
        <span className="text-fg text-[13.5px] font-semibold">{t("signals.title")}</span>
        <span className="flex items-center gap-1.5 pl-1">
          {SEV_ORDER.map((sev) =>
            (counts?.[sev] ?? 0) > 0 ? (
              <span key={sev} className="flex items-center gap-1 text-[11.5px]">
                <span className={cn("size-2 rounded-full", sevTone[sev].dot)} />
                <span className="text-fg-muted tabular-nums">{counts?.[sev]}</span>
              </span>
            ) : null,
          )}
        </span>
        <span className="flex-1" />
        <Link
          to="/signals"
          className="text-fg-subtle hover:text-fg hover:bg-bg-subtle rounded-md p-1.5"
          title={t("signals.openAll")}
        >
          <ArrowRight className="size-4" />
        </Link>
      </div>
      {top.length === 0 ? (
        <p className="text-fg-subtle px-4 pt-1 pb-3.5 text-[12.5px]">{t("signals.empty.title")}</p>
      ) : (
        <div className="divide-edge divide-y">
          {top.map((s) => {
            const tone = sevTone[(s.severity as Severity) ?? "info"];
            return (
              <Link
                key={s.id}
                to="/signals"
                className="hover:bg-bg-subtle/60 flex items-center gap-2.5 px-4 py-2 transition-colors"
              >
                <span className={cn("size-2 shrink-0 rounded-full", tone.dot)} />
                <span className="text-fg min-w-0 flex-1 truncate text-[12.5px]">{s.title}</span>
                <span className="text-fg-subtle shrink-0 text-[11px]">
                  {s.sourceName} · {formatRelative(s.lastSeen)}
                </span>
              </Link>
            );
          })}
        </div>
      )}
    </Card>
  );
}
