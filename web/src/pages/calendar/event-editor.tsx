import { useState } from "react";
import { useTranslation } from "react-i18next";
import { AlignLeft, Clock, Link2, MapPin, Repeat, Trash2 } from "lucide-react";
import { isApiError } from "@/lib/api";
import { cn } from "@/lib/utils";
import { Button } from "@/ui/button";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { Switch } from "@/ui/switch";
import { useToast } from "@/ui/toast";
import {
  fromInputs,
  presetRRule,
  rruleToPreset,
  toLocalDateInput,
  toLocalTimeInput,
  useCreateEvent,
  useDeleteEvent,
  useUpdateEvent,
  type Calendar,
  type CalendarInstance,
  type EditScope,
} from "./shared";

/**
 * The Apple-grade event editor: title, calendar, all-day, start/end,
 * repeat, location, URL, and notes. Edits to recurring events ask how far
 * they reach (this event / this and following / all).
 */
export function EventEditor({
  calendars,
  instance,
  defaults,
  onClose,
}: {
  calendars: Calendar[];
  /** Editing an existing occurrence; null = creating. */
  instance: CalendarInstance | null;
  /** Prefill for creation (e.g. the clicked day). */
  defaults?: { start: Date; allDay?: boolean };
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const create = useCreateEvent();
  const update = useUpdateEvent();
  const remove = useDeleteEvent();

  const writable = calendars.filter((c) => !c.readOnly);
  const editing = instance !== null;

  const initStart = instance ? new Date(instance.start) : (defaults?.start ?? new Date());
  // All-day ends are exclusive on the wire; show the inclusive last day.
  const initEnd = instance
    ? instance.allDay
      ? new Date(new Date(instance.end).getTime() - 86_400_000)
      : new Date(instance.end)
    : new Date((defaults?.start ?? new Date()).getTime() + 3600_000);

  const [calendarId, setCalendarId] = useState(
    instance?.calendarId ?? writable[0]?.id ?? "",
  );
  const [title, setTitle] = useState(instance?.title ?? "");
  const [allDay, setAllDay] = useState(instance?.allDay ?? defaults?.allDay ?? false);
  const [startDate, setStartDate] = useState(toLocalDateInput(initStart));
  const [startTime, setStartTime] = useState(toLocalTimeInput(initStart));
  const [endDate, setEndDate] = useState(toLocalDateInput(initEnd));
  const [endTime, setEndTime] = useState(toLocalTimeInput(initEnd));
  const [repeat, setRepeat] = useState(() => rruleToPreset(instance?.rrule, initStart));
  const [customRule, setCustomRule] = useState(instance?.rrule ?? "");
  const [location, setLocation] = useState(instance?.location ?? "");
  const [url, setUrl] = useState(instance?.url ?? "");
  const [notes, setNotes] = useState(instance?.description ?? "");
  // When a recurring event is saved/deleted, ask how far the change goes.
  const [askScope, setAskScope] = useState<null | "save" | "delete">(null);

  const cal = calendars.find((c) => c.id === calendarId);
  const readOnly = editing && (cal?.readOnly ?? false);
  const pending = create.isPending || update.isPending || remove.isPending;

  const buildInput = () => {
    const start = allDay
      ? fromInputs(startDate, "00:00")
      : fromInputs(startDate, startTime);
    let end = allDay ? fromInputs(endDate, "00:00") : fromInputs(endDate, endTime);
    if (allDay) end = new Date(end.getTime() + 86_400_000); // exclusive end
    if (end <= start) end = new Date(start.getTime() + (allDay ? 86_400_000 : 1800_000));
    const rule = repeat === "custom" ? customRule.trim() : presetRRule(repeat, start);
    return {
      title: title.trim(),
      location: location.trim() || undefined,
      description: notes.trim() || undefined,
      url: url.trim() || undefined,
      startsAt: start.toISOString(),
      endsAt: end.toISOString(),
      allDay,
      rrule: rule || undefined,
    };
  };

  const err = (e: unknown) => toast("error", isApiError(e) ? e.message : t("common.unknownError"));

  const doSave = (scope?: EditScope) => {
    if (!editing) {
      create.mutate(
        { calendarId, event: buildInput() },
        { onSuccess: onClose, onError: err },
      );
      return;
    }
    update.mutate(
      {
        id: instance.eventId,
        scope,
        occurrenceStart: instance.occurrenceStart,
        event: buildInput(),
      },
      { onSuccess: onClose, onError: err },
    );
  };

  const doDelete = (scope?: EditScope) => {
    if (!editing) return;
    remove.mutate(
      { id: instance.eventId, scope, occurrenceStart: instance.occurrenceStart },
      { onSuccess: onClose, onError: err },
    );
  };

  const requestSave = () => {
    if (editing && instance.recurring) setAskScope("save");
    else doSave();
  };
  const requestDelete = () => {
    if (editing && instance.recurring) setAskScope("delete");
    else doDelete();
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent
        title={editing ? t("calendar.editor.editTitle") : t("calendar.editor.newTitle")}
        className="max-w-xl"
        footer={
          <div className="flex w-full items-center gap-2">
            {editing && !readOnly && (
              <Button variant="danger" size="sm" disabled={pending} onClick={requestDelete}>
                <Trash2 className="size-3.5" />
                {t("common.delete")}
              </Button>
            )}
            <span className="flex-1" />
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            {!readOnly && (
              <Button
                variant="primary"
                disabled={pending || !title.trim() || !calendarId}
                onClick={requestSave}
              >
                {t("common.save")}
              </Button>
            )}
          </div>
        }
      >
        <div className="space-y-3.5">
          <Input
            autoFocus={!editing}
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder={t("calendar.editor.titlePlaceholder")}
            disabled={readOnly}
            className="text-[15px] font-medium"
          />

          <div className="grid gap-3 sm:grid-cols-2">
            <Field label={t("calendar.editor.calendar")}>
              {() => (
                <Select
                  value={calendarId}
                  onValueChange={setCalendarId}
                  disabled={editing}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {(editing ? calendars : writable).map((c) => (
                      <SelectItem key={c.id} value={c.id}>
                        <span className="flex items-center gap-2">
                          <span className="size-2.5 rounded-full" style={{ background: c.color }} />
                          {c.name}
                        </span>
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            </Field>
            <div className="border-edge flex items-center justify-between rounded-lg border px-3.5 py-2">
              <span className="text-fg text-[13px] font-medium">{t("calendar.editor.allDay")}</span>
              <Switch checked={allDay} onCheckedChange={setAllDay} disabled={readOnly} />
            </div>
          </div>

          <div className="space-y-2">
            <TimeRow
              icon={<Clock className="size-3.5" />}
              label={t("calendar.editor.starts")}
              date={startDate}
              time={startTime}
              allDay={allDay}
              disabled={readOnly}
              onDate={(v) => {
                setStartDate(v);
                if (endDate < v) setEndDate(v);
              }}
              onTime={setStartTime}
            />
            <TimeRow
              icon={<Clock className="size-3.5 opacity-0" />}
              label={t("calendar.editor.ends")}
              date={endDate}
              time={endTime}
              allDay={allDay}
              disabled={readOnly}
              onDate={setEndDate}
              onTime={setEndTime}
            />
          </div>

          <Field label={t("calendar.editor.repeat")}>
            {() => (
              <div className="flex items-center gap-2">
                <Repeat className="text-fg-subtle size-3.5 shrink-0" />
                <Select value={repeat} onValueChange={setRepeat} disabled={readOnly}>
                  <SelectTrigger className="flex-1">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {["none", "daily", "weekly", "biweekly", "monthly", "yearly", "custom"].map(
                      (p) => (
                        <SelectItem key={p} value={p}>
                          {t(`calendar.repeat.${p}`)}
                        </SelectItem>
                      ),
                    )}
                  </SelectContent>
                </Select>
              </div>
            )}
          </Field>
          {repeat === "custom" && (
            <Input
              value={customRule}
              onChange={(e) => setCustomRule(e.target.value)}
              placeholder="FREQ=WEEKLY;BYDAY=MO,WE,FR"
              className="font-mono text-[12.5px]"
              disabled={readOnly}
            />
          )}

          <IconInput
            icon={<MapPin className="size-3.5" />}
            value={location}
            onChange={setLocation}
            placeholder={t("calendar.editor.location")}
            disabled={readOnly}
          />
          <IconInput
            icon={<Link2 className="size-3.5" />}
            value={url}
            onChange={setUrl}
            placeholder={t("calendar.editor.url")}
            disabled={readOnly}
          />
          <div className="flex items-start gap-2">
            <AlignLeft className="text-fg-subtle mt-2.5 size-3.5 shrink-0" />
            <textarea
              value={notes}
              onChange={(e) => setNotes(e.target.value)}
              placeholder={t("calendar.editor.notes")}
              disabled={readOnly}
              rows={3}
              className={cn(
                "border-edge bg-surface text-fg placeholder:text-fg-subtle w-full rounded-lg border px-3 py-2 text-[13px]",
                "focus:border-accent focus:outline-none",
              )}
            />
          </div>
          {readOnly && (
            <p className="text-fg-subtle text-[12px]">{t("calendar.editor.readOnly")}</p>
          )}
        </div>

        {askScope && (
          <ScopeDialog
            mode={askScope}
            pending={pending}
            onPick={(scope) => {
              setAskScope(null);
              if (askScope === "save") doSave(scope);
              else doDelete(scope);
            }}
            onClose={() => setAskScope(null)}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

function TimeRow({
  icon,
  label,
  date,
  time,
  allDay,
  disabled,
  onDate,
  onTime,
}: {
  icon: React.ReactNode;
  label: string;
  date: string;
  time: string;
  allDay: boolean;
  disabled?: boolean;
  onDate: (v: string) => void;
  onTime: (v: string) => void;
}) {
  return (
    <div className="flex items-center gap-2">
      <span className="text-fg-subtle shrink-0">{icon}</span>
      <span className="text-fg-muted w-12 shrink-0 text-[12.5px]">{label}</span>
      <Input
        type="date"
        value={date}
        onChange={(e) => onDate(e.target.value)}
        disabled={disabled}
        className="h-8.5 flex-1 text-[13px]"
      />
      {!allDay && (
        <Input
          type="time"
          value={time}
          onChange={(e) => onTime(e.target.value)}
          disabled={disabled}
          className="h-8.5 w-28 text-[13px]"
        />
      )}
    </div>
  );
}

function IconInput({
  icon,
  value,
  onChange,
  placeholder,
  disabled,
}: {
  icon: React.ReactNode;
  value: string;
  onChange: (v: string) => void;
  placeholder: string;
  disabled?: boolean;
}) {
  return (
    <div className="flex items-center gap-2">
      <span className="text-fg-subtle shrink-0">{icon}</span>
      <Input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        disabled={disabled}
        className="h-8.5 flex-1 text-[13px]"
      />
    </div>
  );
}

/** "How far does this change reach?" — the recurring-event scope prompt. */
function ScopeDialog({
  mode,
  pending,
  onPick,
  onClose,
}: {
  mode: "save" | "delete";
  pending: boolean;
  onPick: (scope: EditScope) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent
        title={mode === "save" ? t("calendar.scope.saveTitle") : t("calendar.scope.deleteTitle")}
        className="max-w-sm"
      >
        <p className="text-fg-muted mb-3 text-[13px]">{t("calendar.scope.body")}</p>
        <div className="space-y-1.5">
          {(["this", "following", "all"] as EditScope[]).map((scope) => (
            <button
              key={scope}
              type="button"
              disabled={pending}
              onClick={() => onPick(scope)}
              className="border-edge hover:bg-bg-subtle hover:border-accent/50 block w-full rounded-lg border px-3.5 py-2.5 text-left transition-colors"
            >
              <span className="text-fg text-[13px] font-medium">
                {t(`calendar.scope.${scope}`)}
              </span>
              <span className="text-fg-subtle block text-[11.5px]">
                {t(`calendar.scope.${scope}Hint`)}
              </span>
            </button>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  );
}
