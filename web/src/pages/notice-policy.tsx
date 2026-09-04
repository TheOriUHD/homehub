import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Plus, Send, X } from "lucide-react";
import { api, isApiError, unwrap, type NoticePolicy } from "@/lib/api";
import { cn, formatRelative } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Input } from "@/ui/input";
import { Switch } from "@/ui/switch";
import { useToast } from "@/ui/toast";

const DAYS = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"] as const;

/**
 * NoticePolicyCard is the owner's rule for how monitored trouble reaches the
 * phone: which severities interrupt at once, when the house is quiet, and
 * when the digests of everything else go out — one list of times for
 * weekdays and one for weekends, or a list per day. Edits live in a draft
 * until Save; "Send digest now" flushes the queue regardless of the clock.
 */
export function NoticePolicyCard({ canConfigure }: { canConfigure: boolean }) {
  const { t } = useTranslation();
  const toast = useToast();
  const qc = useQueryClient();
  const state = useQuery({
    queryKey: ["notice-policy"],
    queryFn: async () => unwrap(await api.GET("/telegram/notice-policy")),
  });
  const [draft, setDraft] = useState<NoticePolicy | null>(null);
  const policy = draft ?? state.data?.policy ?? null;
  const edit = (fn: (p: NoticePolicy) => NoticePolicy) => {
    if (policy) setDraft(fn(structuredClone(policy)));
  };

  const save = useMutation({
    mutationFn: async () => unwrap(await api.PUT("/telegram/notice-policy", { body: policy! })),
    onSuccess: () => {
      setDraft(null);
      toast("success", t("assistant.telegram.policy.saved"));
      void qc.invalidateQueries({ queryKey: ["notice-policy"] });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : String(err)),
  });
  const sendNow = useMutation({
    mutationFn: async () => unwrap(await api.POST("/telegram/notice-policy/digest")),
    onSuccess: (r) => {
      toast("success", r.sent ? t("assistant.telegram.policy.digestSent", { count: r.items }) : t("assistant.telegram.policy.digestEmpty"));
      void qc.invalidateQueries({ queryKey: ["notice-policy"] });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : String(err)),
  });

  if (!policy) return null;
  const s = state.data;
  const ro = !canConfigure;

  return (
    <div className="border-edge space-y-4 rounded-lg border p-3.5">
      <div className="space-y-1">
        <p className="text-fg text-[13px] font-semibold">{t("assistant.telegram.policy.title")}</p>
        <p className="text-fg-subtle text-[12px]">{t("assistant.telegram.policy.intro")}</p>
        {s && (
          <div className="flex flex-wrap items-center gap-1.5 pt-1">
            <Badge tone={s.queued > 0 ? "warning" : "neutral"}>
              {t("assistant.telegram.policy.queued", { count: s.queued })}
            </Badge>
            {s.nextDigestAt && (
              <Badge tone="neutral">{t("assistant.telegram.policy.next", { when: formatRelative(s.nextDigestAt) })}</Badge>
            )}
            {s.quietNow && <Badge tone="info">{t("assistant.telegram.policy.quietNow")}</Badge>}
          </div>
        )}
      </div>

      {/* what interrupts */}
      <div className="space-y-2">
        <p className="text-fg text-[12.5px] font-medium">{t("assistant.telegram.policy.immediate")}</p>
        <div className="grid gap-2 sm:grid-cols-2">
          {(["critical", "warning", "info", "recovered"] as const).map((sev) => (
            <label key={sev} className="text-fg flex items-center justify-between gap-3 text-[13px]">
              <span>
                {t(`assistant.telegram.policy.${sev}`)}
                {sev === "recovered" && (
                  <span className="text-fg-subtle block text-[11.5px]">{t("assistant.telegram.policy.recoveredHint")}</span>
                )}
              </span>
              <Switch
                checked={policy.immediate[sev]}
                disabled={ro || sev === "critical"}
                onCheckedChange={(v) => edit((p) => ({ ...p, immediate: { ...p.immediate, [sev]: v } }))}
              />
            </label>
          ))}
        </div>
      </div>

      {/* quiet hours */}
      <div className="space-y-2">
        <label className="text-fg flex items-center justify-between gap-3 text-[12.5px] font-medium">
          {t("assistant.telegram.policy.quiet")}
          <Switch
            checked={policy.quietHours.enabled}
            disabled={ro}
            onCheckedChange={(v) => edit((p) => ({ ...p, quietHours: { ...p.quietHours, enabled: v } }))}
          />
        </label>
        {policy.quietHours.enabled && (
          <div className="flex flex-wrap items-center gap-3 text-[13px]">
            <span className="text-fg-muted">{t("assistant.telegram.policy.quietFrom")}</span>
            <Input
              type="time"
              className="w-28"
              disabled={ro}
              value={policy.quietHours.from}
              onChange={(e) => edit((p) => ({ ...p, quietHours: { ...p.quietHours, from: e.target.value } }))}
            />
            <span className="text-fg-muted">{t("assistant.telegram.policy.quietTo")}</span>
            <Input
              type="time"
              className="w-28"
              disabled={ro}
              value={policy.quietHours.to}
              onChange={(e) => edit((p) => ({ ...p, quietHours: { ...p.quietHours, to: e.target.value } }))}
            />
            <label className="text-fg ml-auto flex items-center gap-2">
              <Switch
                checked={policy.quietHours.criticalDuring}
                disabled={ro}
                onCheckedChange={(v) => edit((p) => ({ ...p, quietHours: { ...p.quietHours, criticalDuring: v } }))}
              />
              {t("assistant.telegram.policy.quietCritical")}
            </label>
          </div>
        )}
      </div>

      {/* digest schedule */}
      <div className="space-y-2">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <p className="text-fg text-[12.5px] font-medium">{t("assistant.telegram.policy.digest")}</p>
          <div className="bg-bg-subtle flex rounded-md p-0.5 text-[12px]">
            {(["weekdays-weekends", "per-day"] as const).map((mode) => (
              <button
                key={mode}
                type="button"
                disabled={ro}
                onClick={() => edit((p) => ({ ...p, digest: { ...p.digest, mode } }))}
                className={cn(
                  "rounded px-2 py-1 transition-colors",
                  policy.digest.mode === mode ? "bg-surface text-fg shadow-sm" : "text-fg-muted hover:text-fg",
                )}
              >
                {mode === "per-day" ? t("assistant.telegram.policy.modeDays") : t("assistant.telegram.policy.modeGroups")}
              </button>
            ))}
          </div>
        </div>
        {policy.digest.mode === "per-day" ? (
          DAYS.map((day) => (
            <TimeList
              key={day}
              label={t(`assistant.telegram.policy.${day}`)}
              times={policy.digest.days[day] ?? []}
              readOnly={ro}
              onChange={(times) => edit((p) => ({ ...p, digest: { ...p.digest, days: { ...p.digest.days, [day]: times } } }))}
            />
          ))
        ) : (
          <>
            <TimeList
              label={t("assistant.telegram.policy.weekdays")}
              times={policy.digest.weekdays}
              readOnly={ro}
              onChange={(times) => edit((p) => ({ ...p, digest: { ...p.digest, weekdays: times } }))}
            />
            <TimeList
              label={t("assistant.telegram.policy.weekends")}
              times={policy.digest.weekends}
              readOnly={ro}
              onChange={(times) => edit((p) => ({ ...p, digest: { ...p.digest, weekends: times } }))}
            />
          </>
        )}
      </div>

      {/* time zone */}
      <div className="space-y-1">
        <p className="text-fg text-[12.5px] font-medium">{t("assistant.telegram.policy.timezone")}</p>
        <Input
          disabled={ro}
          value={policy.timezone}
          placeholder={s?.timezoneDefault}
          spellCheck={false}
          onChange={(e) => edit((p) => ({ ...p, timezone: e.target.value }))}
        />
        <p className="text-fg-subtle text-[11.5px]">
          {t("assistant.telegram.policy.timezoneHint", { tz: s?.timezoneDefault ?? "UTC" })}
        </p>
      </div>

      {canConfigure && (
        <div className="flex items-center justify-between gap-2">
          <Button variant="secondary" size="sm" loading={sendNow.isPending} onClick={() => sendNow.mutate()}>
            <Send className="size-3.5" />
            {t("assistant.telegram.policy.sendNow")}
          </Button>
          <Button variant="primary" size="sm" disabled={!draft} loading={save.isPending} onClick={() => save.mutate()}>
            {t("common.save")}
          </Button>
        </div>
      )}
    </div>
  );
}

/** One labelled row of digest times: chips with a remove, plus a time input to add. */
function TimeList({
  label,
  times,
  readOnly,
  onChange,
}: {
  label: string;
  times: string[];
  readOnly: boolean;
  onChange: (times: string[]) => void;
}) {
  const { t } = useTranslation();
  const [next, setNext] = useState("");
  const add = () => {
    if (!next || times.includes(next)) return;
    onChange([...times, next].sort());
    setNext("");
  };
  return (
    <div className="flex flex-wrap items-center gap-2 text-[13px]">
      <span className="text-fg-muted w-24 shrink-0">{label}</span>
      {times.length === 0 && <span className="text-fg-subtle text-[12px]">{t("assistant.telegram.policy.noTimes")}</span>}
      {times.map((time) => (
        <span key={time} className="bg-bg-subtle text-fg inline-flex items-center gap-1 rounded-md px-2 py-0.5 font-mono text-[12.5px]">
          {time}
          {!readOnly && (
            <button
              type="button"
              className="text-fg-subtle hover:text-danger"
              aria-label={t("common.delete")}
              onClick={() => onChange(times.filter((x) => x !== time))}
            >
              <X className="size-3" />
            </button>
          )}
        </span>
      ))}
      {!readOnly && (
        <span className="ml-auto flex items-center gap-1">
          <Input type="time" className="w-28" value={next} onChange={(e) => setNext(e.target.value)} />
          <Button variant="ghost" size="sm" disabled={!next} onClick={add}>
            <Plus className="size-3.5" />
            {t("assistant.telegram.policy.add")}
          </Button>
        </span>
      )}
    </div>
  );
}
