import { useRef, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { CloudDownload, FolderSync, Globe, HardDrive, Pencil, Plus, RefreshCw, Trash2, Recycle, Rss } from "lucide-react";
import { api, isApiError, unwrap } from "@/lib/api";
import { cn } from "@/lib/utils";
import { Button } from "@/ui/button";
import { CopyBlock } from "@/ui/misc";
import { ConfirmDialog } from "@/ui/confirm";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Field } from "@/ui/field";
import { Input, PasswordInput } from "@/ui/input";
import { useToast } from "@/ui/toast";
import { useInvalidateCalendar, type Calendar } from "./shared";

const COLORS = ["#4f46e5", "#0d9488", "#d97706", "#db2777", "#2563eb", "#65a30d", "#9333ea", "#dc2626"];

const KIND_ICONS = { local: HardDrive, caldav: FolderSync, ics: Globe, trash: Recycle } as const;

/**
 * The calendars manager: every source with its color, kind, and sync state;
 * add local calendars, CalDAV accounts (with discovery), ICS feeds, and
 * trash-collection schedules with staged reminders.
 */
export function ManageCalendarsDialog({
  calendars,
  onClose,
}: {
  calendars: Calendar[];
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const invalidate = useInvalidateCalendar();
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<Calendar | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<Calendar | null>(null);
  const [sharing, setSharing] = useState<Calendar | null>(null);

  const sync = useMutation({
    mutationFn: async (id: string) =>
      unwrap(await api.POST("/calendars/{id}/sync", { params: { path: { id } } })),
    onSuccess: (cal) => {
      invalidate();
      if (cal.syncError) toast("error", cal.syncError);
      else toast("success", t("calendar.manage.synced", { name: cal.name }));
    },
    onError: (e) => toast("error", isApiError(e) ? e.message : t("common.unknownError")),
  });

  const remove = useMutation({
    mutationFn: async (id: string) =>
      unwrap(await api.DELETE("/calendars/{id}", { params: { path: { id } } })),
    onSuccess: () => {
      setConfirmDelete(null);
      invalidate();
    },
    onError: (e) => toast("error", isApiError(e) ? e.message : t("common.unknownError")),
  });

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent
        title={t("calendar.manage.title")}
        className="max-w-lg"
        footer={
          <>
            <Button variant="secondary" size="sm" onClick={() => setAdding(true)}>
              <Plus className="size-3.5" />
              {t("calendar.manage.add")}
            </Button>
            <span className="flex-1" />
            <Button variant="primary" onClick={onClose}>
              {t("common.done")}
            </Button>
          </>
        }
      >
        {calendars.length === 0 ? (
          <p className="text-fg-subtle py-6 text-center text-[13px]">{t("calendar.manage.empty")}</p>
        ) : (
          <div className="divide-edge border-edge divide-y rounded-lg border">
            {calendars.map((c) => {
              const Icon = KIND_ICONS[c.kind] ?? HardDrive;
              return (
                <div key={c.id} className="flex items-center gap-3 px-3.5 py-2.5">
                  <span className="size-3 shrink-0 rounded-full" style={{ background: c.color }} />
                  <div className="min-w-0 flex-1">
                    <p className="text-fg flex items-center gap-1.5 text-[13px] font-medium">
                      {c.name}
                      <Icon className="text-fg-subtle size-3.5" />
                    </p>
                    <p className={cn("text-[11.5px]", c.syncError ? "text-danger" : "text-fg-subtle")}>
                      {c.syncError
                        ? c.syncError
                        : t(`calendar.kinds.${c.kind}`)}
                    </p>
                  </div>
                  {c.kind !== "local" && (
                    <button
                      type="button"
                      onClick={() => sync.mutate(c.id)}
                      className="text-fg-subtle hover:text-fg rounded-md p-1.5"
                      title={t("calendar.manage.syncNow")}
                    >
                      <RefreshCw className={cn("size-4", sync.isPending && "animate-spin")} />
                    </button>
                  )}
                  <button
                    type="button"
                    onClick={() => setSharing(c)}
                    className="text-fg-subtle hover:text-fg rounded-md p-1.5"
                    title={t("calendar.manage.feedTitle")}
                  >
                    <Rss className="size-4" />
                  </button>
                  <button
                    type="button"
                    onClick={() => setEditing(c)}
                    className="text-fg-subtle hover:text-fg rounded-md p-1.5"
                    title={t("common.edit")}
                  >
                    <Pencil className="size-4" />
                  </button>
                  <button
                    type="button"
                    onClick={() => setConfirmDelete(c)}
                    className="text-fg-subtle hover:text-danger rounded-md p-1.5"
                    title={t("common.delete")}
                  >
                    <Trash2 className="size-4" />
                  </button>
                </div>
              );
            })}
          </div>
        )}

        {adding && <AddCalendarDialog onClose={() => setAdding(false)} />}
        {editing && <EditCalendarDialog calendar={editing} onClose={() => setEditing(null)} />}
        {sharing && <FeedDialog calendar={sharing} onClose={() => setSharing(null)} />}
        <ConfirmDialog
          open={confirmDelete !== null}
          onOpenChange={(o) => !o && setConfirmDelete(null)}
          title={t("calendar.manage.deleteTitle", { name: confirmDelete?.name ?? "" })}
          body={t("calendar.manage.deleteBody")}
          confirmLabel={t("common.delete")}
          loading={remove.isPending}
          onConfirm={() => confirmDelete && remove.mutate(confirmDelete.id)}
        />
      </DialogContent>
    </Dialog>
  );
}

/**
 * Publishing a calendar outward: one subscription URL any iCalendar client
 * can poll. The URL is the credential, so the dialog treats it like one —
 * rotate replaces it, stop kills it at once.
 */
function FeedDialog({ calendar, onClose }: { calendar: Calendar; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const feed = useQuery({
    queryKey: ["calendar", "feed", calendar.id],
    queryFn: async () => unwrap(await api.GET("/calendars/{id}/feed", { params: { path: { id: calendar.id } } })),
  });
  const publish = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/calendars/{id}/feed", { params: { path: { id: calendar.id } } })),
    onSuccess: () => feed.refetch(),
    onError: (e) => toast("error", isApiError(e) ? e.message : t("common.unknownError")),
  });
  const unpublish = useMutation({
    mutationFn: async () =>
      unwrap(await api.DELETE("/calendars/{id}/feed", { params: { path: { id: calendar.id } } })),
    onSuccess: () => feed.refetch(),
    onError: (e) => toast("error", isApiError(e) ? e.message : t("common.unknownError")),
  });
  // A path-only URL means no app URL is configured; the browser's origin is
  // the best guess at where subscribers will reach this HomeHub.
  const url = feed.data?.url ? (feed.data.url.startsWith("/") ? window.location.origin + feed.data.url : feed.data.url) : "";
  const busy = publish.isPending || unpublish.isPending;

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent
        title={t("calendar.manage.feedTitle")}
        className="max-w-lg"
        footer={
          <>
            {feed.data?.published ? (
              <>
                <Button variant="secondary" size="sm" disabled={busy} onClick={() => publish.mutate()}>
                  {t("calendar.manage.feedRotate")}
                </Button>
                <Button variant="danger" size="sm" disabled={busy} onClick={() => unpublish.mutate()}>
                  {t("calendar.manage.feedStop")}
                </Button>
              </>
            ) : (
              <Button variant="primary" size="sm" disabled={busy || feed.isLoading} onClick={() => publish.mutate()}>
                {t("calendar.manage.feedPublish")}
              </Button>
            )}
            <span className="flex-1" />
            <Button variant="secondary" onClick={onClose}>
              {t("common.done")}
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <p className="text-fg-muted text-[13px]">{t("calendar.manage.feedIntro", { name: calendar.name })}</p>
          {feed.data?.published && url ? (
            <>
              <CopyBlock label={t("calendar.manage.feedUrl")} value={url} />
              <p className="text-fg-subtle text-[12px]">{t("calendar.manage.feedHint")}</p>
            </>
          ) : (
            <p className="text-fg-subtle text-[12px]">{t("calendar.manage.feedOff")}</p>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function ColorPicker({ value, onChange }: { value: string; onChange: (c: string) => void }) {
  return (
    <div className="flex flex-wrap gap-1.5">
      {COLORS.map((c) => (
        <button
          key={c}
          type="button"
          onClick={() => onChange(c)}
          className={cn(
            "size-6 rounded-full transition-transform",
            value === c && "ring-accent scale-110 ring-2 ring-offset-2",
          )}
          style={{ background: c }}
          aria-label={c}
        />
      ))}
    </div>
  );
}

function EditCalendarDialog({ calendar, onClose }: { calendar: Calendar; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const invalidate = useInvalidateCalendar();
  const [name, setName] = useState(calendar.name);
  const [color, setColor] = useState(calendar.color);
  const settings = (calendar.settings ?? {}) as { leadHours?: number[] };
  const [leadHours, setLeadHours] = useState<number[]>(settings.leadHours ?? [24]);

  const save = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.PUT("/calendars/{id}", {
          params: { path: { id: calendar.id } },
          body: {
            name: name.trim(),
            color,
            settings: calendar.kind === "trash" ? { leadHours } : calendar.settings,
          },
        }),
      ),
    onSuccess: () => {
      invalidate();
      onClose();
    },
    onError: (e) => toast("error", isApiError(e) ? e.message : t("common.unknownError")),
  });

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent
        title={t("calendar.manage.editTitle", { name: calendar.name })}
        className="max-w-sm"
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button variant="primary" disabled={save.isPending || !name.trim()} onClick={() => save.mutate()}>
              {t("common.save")}
            </Button>
          </>
        }
      >
        <div className="space-y-3.5">
          <Field label={t("calendar.manage.name")}>
            {(p) => <Input {...p} value={name} onChange={(e) => setName(e.target.value)} />}
          </Field>
          <Field label={t("calendar.manage.color")}>
            {() => <ColorPicker value={color} onChange={setColor} />}
          </Field>
          {calendar.kind === "trash" && (
            <ReminderStages value={leadHours} onChange={setLeadHours} />
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

/** Editable list of lead-time stages ("3 days before", "24 hours before"). */
function ReminderStages({ value, onChange }: { value: number[]; onChange: (v: number[]) => void }) {
  const { t } = useTranslation();
  const OPTIONS = [2, 6, 12, 24, 48, 72, 168];
  return (
    <Field label={t("calendar.manage.reminders")} hint={t("calendar.manage.remindersHint")}>
      {() => (
        <div className="flex flex-wrap gap-1.5">
          {OPTIONS.map((h) => {
            const active = value.includes(h);
            return (
              <button
                key={h}
                type="button"
                onClick={() =>
                  onChange(active ? value.filter((x) => x !== h) : [...value, h].sort((a, b) => a - b))
                }
                className={cn(
                  "rounded-full border px-2.5 py-1 text-[12px] transition-colors",
                  active
                    ? "border-accent bg-accent-soft text-accent-soft-fg font-medium"
                    : "border-edge text-fg-muted hover:text-fg",
                )}
              >
                {h >= 24 ? t("calendar.manage.daysBefore", { count: h / 24 }) : t("calendar.manage.hoursBefore", { count: h })}
              </button>
            );
          })}
        </div>
      )}
    </Field>
  );
}

/** The add flow: pick a kind, then the kind-specific form. */
function AddCalendarDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const invalidate = useInvalidateCalendar();
  const fileRef = useRef<HTMLInputElement>(null);

  const [kind, setKind] = useState<Calendar["kind"]>("local");
  const [name, setName] = useState("");
  const [color, setColor] = useState(COLORS[0]);
  const [url, setUrl] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [icsContent, setIcsContent] = useState("");
  const [icsFileName, setIcsFileName] = useState("");
  const [leadHours, setLeadHours] = useState<number[]>([72, 24]);
  const [discovered, setDiscovered] = useState<{ path: string; name: string }[] | null>(null);

  const discover = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/calendars/discover", { body: { url: url.trim(), username, password } })),
    onSuccess: (found) => {
      setDiscovered(found.map((f) => ({ path: f.path, name: f.name || f.path })));
      if (found.length === 0) toast("error", t("calendar.manage.noCollections"));
    },
    onError: (e) => toast("error", isApiError(e) ? e.message : t("common.unknownError")),
  });

  const create = useMutation({
    mutationFn: async (vars: { finalUrl: string; finalName: string }) =>
      unwrap(
        await api.POST("/calendars", {
          body: {
            name: vars.finalName,
            color,
            kind,
            url: vars.finalUrl || undefined,
            username: username || undefined,
            password: password || undefined,
            icsContent: icsContent || undefined,
            settings: kind === "trash" ? { leadHours } : undefined,
          },
        }),
      ),
    onSuccess: (cal) => {
      invalidate();
      if (cal.syncError) toast("error", t("calendar.manage.addedWithError", { error: cal.syncError }));
      else toast("success", t("calendar.manage.added", { name: cal.name }));
      onClose();
    },
    onError: (e) => toast("error", isApiError(e) ? e.message : t("common.unknownError")),
  });

  const canSubmit = (() => {
    if (!name.trim() && kind !== "caldav") return false;
    switch (kind) {
      case "local":
        return true;
      case "caldav":
        return url.trim() !== "";
      case "ics":
      case "trash":
        return url.trim() !== "" || icsContent !== "";
      default:
        return false;
    }
  })();

  const submit = (collectionPath?: string, collectionName?: string) => {
    const finalUrl = collectionPath
      ? new URL(collectionPath, url.trim()).toString()
      : kind === "caldav"
        ? url.trim()
        : url.trim();
    create.mutate({ finalUrl, finalName: name.trim() || collectionName || "Calendar" });
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent
        title={t("calendar.manage.addTitle")}
        className="max-w-md"
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            {kind === "caldav" && discovered === null ? (
              <Button
                variant="primary"
                disabled={discover.isPending || !url.trim()}
                onClick={() => discover.mutate()}
              >
                <CloudDownload className="size-3.5" />
                {t("calendar.manage.discover")}
              </Button>
            ) : (
              <Button
                variant="primary"
                disabled={create.isPending || !canSubmit}
                onClick={() => submit()}
              >
                {t("common.create")}
              </Button>
            )}
          </>
        }
      >
        <div className="space-y-3.5">
          {/* kind picker */}
          <div className="grid grid-cols-4 gap-1.5">
            {(["local", "caldav", "ics", "trash"] as const).map((k) => {
              const Icon = KIND_ICONS[k];
              return (
                <button
                  key={k}
                  type="button"
                  onClick={() => {
                    setKind(k);
                    setDiscovered(null);
                  }}
                  className={cn(
                    "flex flex-col items-center gap-1 rounded-lg border px-2 py-2.5 text-[11.5px] transition-colors",
                    kind === k
                      ? "border-accent bg-accent-soft text-accent-soft-fg font-medium"
                      : "border-edge text-fg-muted hover:text-fg",
                  )}
                >
                  <Icon className="size-4" />
                  {t(`calendar.kinds.${k}`)}
                </button>
              );
            })}
          </div>

          <Field label={t("calendar.manage.name")} hint={kind === "caldav" ? t("calendar.manage.nameCaldavHint") : undefined}>
            {(p) => <Input {...p} value={name} onChange={(e) => setName(e.target.value)} />}
          </Field>
          <Field label={t("calendar.manage.color")}>
            {() => <ColorPicker value={color} onChange={setColor} />}
          </Field>

          {kind === "caldav" && (
            <>
              <Field label={t("calendar.manage.caldavUrl")} hint={t("calendar.manage.caldavUrlHint")}>
                {(p) => (
                  <Input {...p} value={url} onChange={(e) => { setUrl(e.target.value); setDiscovered(null); }} placeholder="https://cloud.example.com/remote.php/dav" />
                )}
              </Field>
              <div className="grid gap-3 sm:grid-cols-2">
                <Field label={t("calendar.manage.username")}>
                  {(p) => <Input {...p} value={username} onChange={(e) => setUsername(e.target.value)} />}
                </Field>
                <Field label={t("calendar.manage.password")}>
                  {(p) => <PasswordInput {...p} value={password} onChange={(e) => setPassword(e.target.value)} />}
                </Field>
              </div>
              {discovered !== null && discovered.length > 0 && (
                <Field label={t("calendar.manage.pickCollection")}>
                  {() => (
                    <div className="divide-edge border-edge max-h-44 divide-y overflow-y-auto rounded-lg border">
                      {discovered.map((d) => (
                        <button
                          key={d.path}
                          type="button"
                          disabled={create.isPending}
                          onClick={() => submit(d.path, d.name)}
                          className="hover:bg-bg-subtle block w-full px-3 py-2 text-left text-[13px]"
                        >
                          {d.name}
                          <span className="text-fg-subtle block truncate text-[11px]">{d.path}</span>
                        </button>
                      ))}
                    </div>
                  )}
                </Field>
              )}
            </>
          )}

          {(kind === "ics" || kind === "trash") && (
            <>
              <Field
                label={kind === "trash" ? t("calendar.manage.trashUrl") : t("calendar.manage.icsUrl")}
                hint={t("calendar.manage.icsUrlHint")}
              >
                {(p) => (
                  <Input {...p} value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://…/calendar.ics" />
                )}
              </Field>
              <div className="flex items-center gap-2">
                <Button size="sm" variant="secondary" onClick={() => fileRef.current?.click()}>
                  {t("calendar.manage.uploadIcs")}
                </Button>
                {icsFileName && <span className="text-fg-subtle text-[12px]">{icsFileName}</span>}
                <input
                  ref={fileRef}
                  type="file"
                  accept=".ics,text/calendar"
                  className="hidden"
                  onChange={async (e) => {
                    const f = e.target.files?.[0];
                    if (f) {
                      setIcsContent(await f.text());
                      setIcsFileName(f.name);
                      setUrl("");
                    }
                    e.target.value = "";
                  }}
                />
              </div>
              {kind === "trash" && <ReminderStages value={leadHours} onChange={setLeadHours} />}
            </>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
