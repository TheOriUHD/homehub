import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { BellRing, Pencil, Trash2 } from "lucide-react";
import {
  api,
  unwrap,
  isApiError,
  type AlertRule,
  type AlertRuleInput,
  type MetricSeriesInfo,
} from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { formatMetricValue } from "@/lib/metrics";
import { Card } from "@/ui/card";
import { Badge, StatusDot } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Input } from "@/ui/input";
import { Field } from "@/ui/field";
import { Switch } from "@/ui/switch";
import { Dialog, DialogContent } from "@/ui/dialog";
import { ConfirmDialog } from "@/ui/confirm";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { EmptyState, ErrorState } from "@/ui/states";
import { SkeletonRows } from "@/ui/skeleton";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { useToast } from "@/ui/toast";

const severityTone = { info: "info", warning: "warning", error: "danger" } as const;

const SUSTAINED_PRESETS = [60, 120, 300, 900, 3600];

/** "300" → "5m" — locale-neutral shorthand used in conditions and pickers. */
function sustainedLabel(seconds: number): string {
  if (seconds % 3600 === 0) return `${seconds / 3600}h`;
  if (seconds % 60 === 0) return `${seconds / 60}m`;
  return `${seconds}s`;
}

/** The editable fields of a rule, for PATCHing a single flipped flag. */
function ruleInput(rule: AlertRule): AlertRuleInput {
  return {
    name: rule.name,
    resource: rule.resource,
    metric: rule.metric,
    operator: rule.operator,
    threshold: rule.threshold,
    sustainedSeconds: rule.sustainedSeconds,
    severity: rule.severity,
    enabled: rule.enabled,
  };
}

export function AlertsPage() {
  const { t } = useTranslation();
  const toast = useToast();
  const { can } = useAuth();
  const queryClient = useQueryClient();

  const rules = useQuery({
    queryKey: ["alerts"],
    queryFn: async () => unwrap(await api.GET("/alerts")),
    // Rule state flips server-side as metrics cross thresholds.
    refetchInterval: 30_000,
  });
  const catalog = useQuery({
    queryKey: ["metrics", "series"],
    queryFn: async () => unwrap(await api.GET("/metrics/series")),
    staleTime: 60_000,
  });

  const unitByKey = useMemo(() => {
    const map = new Map<string, string>();
    for (const s of catalog.data ?? []) map.set(`${s.resource}|${s.metric}`, s.unit);
    return map;
  }, [catalog.data]);

  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<AlertRule | null>(null);
  const [deleting, setDeleting] = useState<AlertRule | null>(null);

  const canCreate = (catalog.data ?? []).some((s) => can(s.resource, "configure"));

  const toggle = useMutation({
    mutationFn: async ({ rule, enabled }: { rule: AlertRule; enabled: boolean }) =>
      unwrap(
        await api.PATCH("/alerts/{id}", {
          params: { path: { id: rule.id } },
          body: { ...ruleInput(rule), enabled },
        }),
      ),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["alerts"] }),
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const remove = useMutation({
    mutationFn: async (id: string) =>
      unwrap(await api.DELETE("/alerts/{id}", { params: { path: { id } } })),
    onSuccess: () => {
      setDeleting(null);
      toast("success", t("alerts.deleted"));
      void queryClient.invalidateQueries({ queryKey: ["alerts"] });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const conditionText = (rule: AlertRule): string => {
    const unit = unitByKey.get(`${rule.resource}|${rule.metric}`) ?? "";
    return t(rule.operator === "gt" ? "alerts.conditionAbove" : "alerts.conditionBelow", {
      metric: rule.metric,
      value: formatMetricValue(unit, rule.threshold),
      duration: sustainedLabel(rule.sustainedSeconds),
    });
  };

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-fg text-xl font-semibold tracking-tight">{t("alerts.title")}</h1>
          <p className="text-fg-muted mt-1 text-sm">{t("alerts.subtitle")}</p>
        </div>
        {canCreate && (
          <Button
            variant="primary"
            onClick={() => {
              setEditing(null);
              setEditorOpen(true);
            }}
          >
            {t("alerts.add")}
          </Button>
        )}
      </div>

      <Card>
        {rules.isLoading ? (
          <SkeletonRows rows={4} className="p-5" />
        ) : rules.isError ? (
          <ErrorState error={rules.error} onRetry={() => void rules.refetch()} />
        ) : (rules.data ?? []).length === 0 ? (
          <EmptyState
            icon={BellRing}
            title={t("alerts.empty.title")}
            body={t("alerts.empty.body")}
            action={
              <Link to="/history" className="text-accent text-[13px] font-medium hover:underline">
                {t("alerts.empty.action")} →
              </Link>
            }
          />
        ) : (
          <Table>
            <THead>
              <tr>
                <TH>{t("alerts.columns.state")}</TH>
                <TH>{t("common.name")}</TH>
                <TH className="hidden lg:table-cell">{t("alerts.columns.series")}</TH>
                <TH>{t("alerts.columns.condition")}</TH>
                <TH className="hidden md:table-cell">{t("alerts.columns.severity")}</TH>
                <TH>{t("common.enabled")}</TH>
                <TH className="w-0" />
              </tr>
            </THead>
            <TBody>
              {rules.data!.map((rule) => {
                const canConfigure = can(rule.resource, "configure");
                return (
                  <TR key={rule.id}>
                    <TD>
                      {rule.state === "firing" ? (
                        <Badge tone="danger" title={stateChangedTitle(rule.stateChangedAt)}>
                          <StatusDot tone="danger" pulse />
                          {t("alerts.state.firing")}
                        </Badge>
                      ) : rule.state === "ok" ? (
                        <Badge tone="success" title={stateChangedTitle(rule.stateChangedAt)}>
                          {t("alerts.state.ok")}
                        </Badge>
                      ) : (
                        <Badge tone="neutral" title={stateChangedTitle(rule.stateChangedAt)}>
                          {t("alerts.state.unknown")}
                        </Badge>
                      )}
                    </TD>
                    <TD>
                      <span className="text-fg font-medium">{rule.name}</span>
                    </TD>
                    <TD className="hidden max-w-56 lg:table-cell">
                      <code className="text-fg-muted block truncate font-mono text-xs">
                        {rule.resource} · {rule.metric}
                      </code>
                    </TD>
                    <TD className="text-fg-muted whitespace-nowrap">{conditionText(rule)}</TD>
                    <TD className="hidden md:table-cell">
                      <Badge tone={severityTone[rule.severity]}>
                        {t(`alerts.severity.${rule.severity}`)}
                      </Badge>
                    </TD>
                    <TD>
                      <Switch
                        checked={rule.enabled}
                        disabled={!canConfigure || toggle.isPending}
                        aria-label={t("alerts.toggleAria", { name: rule.name })}
                        onCheckedChange={(enabled) => toggle.mutate({ rule, enabled })}
                      />
                    </TD>
                    <TD>
                      {canConfigure && (
                        <span className="flex justify-end gap-1">
                          <Button
                            size="icon"
                            variant="ghost"
                            aria-label={t("common.edit")}
                            onClick={() => {
                              setEditing(rule);
                              setEditorOpen(true);
                            }}
                          >
                            <Pencil className="size-3.5" />
                          </Button>
                          <Button
                            size="icon"
                            variant="danger-ghost"
                            aria-label={t("common.delete")}
                            onClick={() => setDeleting(rule)}
                          >
                            <Trash2 className="size-3.5" />
                          </Button>
                        </span>
                      )}
                    </TD>
                  </TR>
                );
              })}
            </TBody>
          </Table>
        )}
      </Card>

      <AlertRuleEditor
        open={editorOpen}
        onOpenChange={setEditorOpen}
        rule={editing}
        catalog={catalog.data ?? []}
      />

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={t("alerts.deleteConfirmTitle", { name: deleting?.name ?? "" })}
        body={t("alerts.deleteConfirmBody")}
        confirmLabel={t("common.delete")}
        loading={remove.isPending}
        onConfirm={() => deleting && remove.mutate(deleting.id)}
      />
    </div>
  );
}

/** Absolute state-change time, surfaced as a hover title on the state badge. */
function stateChangedTitle(iso: string): string {
  return new Date(iso).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

// ---- editor ----------------------------------------------------------------

/**
 * AlertRuleEditor creates or edits a rule. Also opened from History with a
 * pre-filled series ("Add alert from this series").
 */
export function AlertRuleEditor({
  open,
  onOpenChange,
  rule,
  prefill,
  catalog,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Existing rule to edit, or null to create. */
  rule: AlertRule | null;
  /** Pre-selected series when creating from History. */
  prefill?: { resource: string; metric: string };
  catalog: MetricSeriesInfo[];
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const { can } = useAuth();
  const queryClient = useQueryClient();

  const [name, setName] = useState("");
  const [resource, setResource] = useState("");
  const [metric, setMetric] = useState("");
  const [operator, setOperator] = useState<"gt" | "lt">("gt");
  const [threshold, setThreshold] = useState("");
  const [sustained, setSustained] = useState("300");
  const [severity, setSeverity] = useState<"info" | "warning" | "error">("warning");

  // Rules can only target resources the user may configure; keep the current
  // rule's resource visible even if grants changed since it was created.
  const resources = useMemo(() => {
    const list = [...new Set(catalog.map((s) => s.resource))].filter((r) => can(r, "configure"));
    if (rule && !list.includes(rule.resource)) list.unshift(rule.resource);
    return list;
  }, [catalog, rule, can]);

  const metrics = useMemo(
    () => catalog.filter((s) => s.resource === resource).map((s) => s.metric),
    [catalog, resource],
  );

  const unit = catalog.find((s) => s.resource === resource && s.metric === metric)?.unit ?? "";

  useEffect(() => {
    if (!open) return;
    if (rule) {
      const ruleUnit =
        catalog.find((s) => s.resource === rule.resource && s.metric === rule.metric)?.unit ?? "";
      setName(rule.name);
      setResource(rule.resource);
      setMetric(rule.metric);
      setOperator(rule.operator);
      setThreshold(
        ruleUnit === "fraction"
          ? String(Math.round(rule.threshold * 100 * 100) / 100)
          : String(rule.threshold),
      );
      setSustained(String(rule.sustainedSeconds));
      setSeverity(rule.severity);
    } else {
      setName("");
      setResource(prefill?.resource ?? "");
      setMetric(prefill?.metric ?? "");
      setOperator("gt");
      setThreshold("");
      setSustained("300");
      setSeverity("warning");
    }
    // Re-seed the form each time the dialog opens for a (possibly different) rule.
  }, [open, rule, prefill, catalog]);

  const save = useMutation({
    mutationFn: async (input: AlertRuleInput) =>
      rule
        ? unwrap(
            await api.PATCH("/alerts/{id}", { params: { path: { id: rule.id } }, body: input }),
          )
        : unwrap(await api.POST("/alerts", { body: input })),
    onSuccess: () => {
      toast("success", rule ? t("alerts.updated") : t("alerts.created"));
      void queryClient.invalidateQueries({ queryKey: ["alerts"] });
      onOpenChange(false);
    },
  });

  const thresholdNumber = Number(threshold);
  const valid =
    name.trim().length > 0 && resource !== "" && metric !== "" && threshold.trim() !== "" && Number.isFinite(thresholdNumber);

  const submit = () => {
    if (!valid) return;
    save.mutate({
      name: name.trim(),
      resource,
      metric,
      operator,
      threshold: unit === "fraction" ? thresholdNumber / 100 : thresholdNumber,
      sustainedSeconds: Number(sustained),
      severity,
      enabled: rule?.enabled ?? true,
    });
  };

  const sustainedOptions = useMemo(() => {
    const opts = [...SUSTAINED_PRESETS];
    const current = Number(sustained);
    if (Number.isFinite(current) && current > 0 && !opts.includes(current)) {
      opts.push(current);
      opts.sort((a, b) => a - b);
    }
    return opts;
  }, [sustained]);

  const apiError = save.error && isApiError(save.error) ? save.error.message : null;
  const unitHint = unit === "fraction" ? "%" : unit === "bytes" ? "bytes" : unit === "per_min" ? "/min" : null;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={rule ? t("alerts.editor.editTitle") : t("alerts.editor.createTitle")}
        description={t("alerts.editor.intro")}
        footer={
          <>
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              {t("common.cancel")}
            </Button>
            <Button variant="primary" disabled={!valid} loading={save.isPending} onClick={submit}>
              {rule ? t("common.save") : t("common.create")}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label={t("common.name")}>
            {(p) => (
              <Input
                {...p}
                autoFocus
                placeholder={t("alerts.editor.namePlaceholder")}
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            )}
          </Field>

          <Field label={t("alerts.editor.resource")}>
            {(p) => (
              <Select
                value={resource}
                onValueChange={(v) => {
                  setResource(v);
                  if (!catalog.some((s) => s.resource === v && s.metric === metric)) setMetric("");
                }}
              >
                <SelectTrigger id={p.id} aria-describedby={p["aria-describedby"]}>
                  <SelectValue placeholder={t("alerts.editor.resourcePlaceholder")} />
                </SelectTrigger>
                <SelectContent>
                  {resources.map((r) => (
                    <SelectItem key={r} value={r} className="font-mono text-xs">
                      {r}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          </Field>

          <Field label={t("alerts.editor.metric")}>
            {(p) => (
              <Select value={metric} onValueChange={setMetric} disabled={resource === ""}>
                <SelectTrigger id={p.id} aria-describedby={p["aria-describedby"]}>
                  <SelectValue placeholder={t("alerts.editor.metricPlaceholder")} />
                </SelectTrigger>
                <SelectContent>
                  {metrics.map((m) => (
                    <SelectItem key={m} value={m} className="font-mono text-xs">
                      {m}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          </Field>

          <div className="grid grid-cols-2 gap-3">
            <Field label={t("alerts.editor.operator")}>
              {(p) => (
                <Select value={operator} onValueChange={(v) => setOperator(v as "gt" | "lt")}>
                  <SelectTrigger id={p.id}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="gt">{t("alerts.editor.above")}</SelectItem>
                    <SelectItem value="lt">{t("alerts.editor.below")}</SelectItem>
                  </SelectContent>
                </Select>
              )}
            </Field>
            <Field label={t("alerts.editor.threshold")}>
              {(p) => (
                <div className="flex items-center gap-2">
                  <Input
                    {...p}
                    type="number"
                    step="any"
                    min={unit === "fraction" ? 0 : undefined}
                    max={unit === "fraction" ? 100 : undefined}
                    value={threshold}
                    onChange={(e) => setThreshold(e.target.value)}
                  />
                  {unitHint && (
                    <span className="text-fg-subtle shrink-0 text-[13px]">{unitHint}</span>
                  )}
                </div>
              )}
            </Field>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <Field label={t("alerts.editor.sustained")} hint={t("alerts.editor.sustainedHint")}>
              {(p) => (
                <Select value={sustained} onValueChange={setSustained}>
                  <SelectTrigger id={p.id} aria-describedby={p["aria-describedby"]}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {sustainedOptions.map((s) => (
                      <SelectItem key={s} value={String(s)}>
                        {sustainedLabel(s)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            </Field>
            <Field label={t("alerts.columns.severity")} error={apiError}>
              {(p) => (
                <Select
                  value={severity}
                  onValueChange={(v) => setSeverity(v as typeof severity)}
                >
                  <SelectTrigger id={p.id} aria-describedby={p["aria-describedby"]}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="info">{t("alerts.severity.info")}</SelectItem>
                    <SelectItem value="warning">{t("alerts.severity.warning")}</SelectItem>
                    <SelectItem value="error">{t("alerts.severity.error")}</SelectItem>
                  </SelectContent>
                </Select>
              )}
            </Field>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
