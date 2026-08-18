import { lazy, Suspense, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import {
  Download,
  FileCode2,
  FilePlus2,
  Loader2,
  Save,
  Trash2,
} from "lucide-react";
import { api, isApiError, unwrap } from "@/lib/api";
import { Button } from "@/ui/button";
import { Card } from "@/ui/card";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { PageLoader } from "@/ui/misc";
import { EmptyState } from "@/ui/states";
import { useToast } from "@/ui/toast";
import { cn } from "@/lib/utils";

// CodeMirror and its language packs are heavy — split into their own chunk,
// loaded the first time the editor page mounts (same pattern as the consoles).
const CodeEditor = lazy(() => import("@/components/code-editor").then((m) => ({ default: m.CodeEditor })));
const languageForP = import("@/components/code-editor").then((m) => m.languageFor);

/**
 * The utilities text editor: per-user workspace files stored server-side,
 * syntax highlighting by extension, and downloads under any name — the name
 * field IS the download name, extension and all. Edits are kept as drafts
 * per file, so switching files never loses work; the assistant can read
 * saved files as context.
 */
export function ToolsEditorPage() {
  const { t } = useTranslation();
  const toast = useToast();
  const qc = useQueryClient();

  const files = useQuery({
    queryKey: ["files"],
    queryFn: async () => unwrap(await api.GET("/files")),
  });

  const [selectedId, setSelectedId] = useState<string | null>(null);
  // Drafts: unsaved edits per file id — the sidebar shows a dirty dot and
  // switching files round-trips through here instead of losing changes.
  const [drafts, setDrafts] = useState<Record<string, { name: string; content: string }>>({});
  const [cursor, setCursor] = useState({ line: 1, col: 1 });
  const [createOpen, setCreateOpen] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [langName, setLangName] = useState("");

  const active = files.data?.find((f) => f.id === selectedId) ?? null;

  const fileQuery = useQuery({
    queryKey: ["files", selectedId],
    queryFn: async () =>
      unwrap(await api.GET("/files/{id}", { params: { path: { id: selectedId! } } })),
    enabled: !!selectedId,
  });

  const draft = selectedId ? drafts[selectedId] : undefined;
  const savedName = active?.name ?? "";
  const savedContent = fileQuery.data?.content ?? "";
  const name = draft?.name ?? savedName;
  const content = draft?.content ?? savedContent;
  const dirty = !!draft && (draft.name !== savedName || draft.content !== savedContent);

  // Language label needs the lazy chunk; resolve once it's in.
  useMemo(() => {
    void languageForP.then((fn) => setLangName(fn(name)));
  }, [name]);

  const setDraft = (patch: Partial<{ name: string; content: string }>) => {
    if (!selectedId) return;
    setDrafts((d) => {
      const cur = d[selectedId] ?? { name, content };
      const next = { ...cur, ...patch };
      // Editing a file back to its saved state clears the draft entirely, so
      // the sidebar dot and the Save button agree there's nothing to save.
      if (next.name === savedName && next.content === savedContent) {
        const { [selectedId]: _drop, ...rest } = d;
        return rest;
      }
      return { ...d, [selectedId]: next };
    });
  };

  const create = useMutation({
    mutationFn: async (newName: string) =>
      unwrap(await api.POST("/files", { body: { name: newName, content: "" } })),
    onSuccess: (f) => {
      void qc.invalidateQueries({ queryKey: ["files"] });
      setSelectedId(f.id);
      setCreateOpen(false);
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const save = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.PUT("/files/{id}", {
          params: { path: { id: selectedId! } },
          body: { name: name.trim(), content },
        }),
      ),
    onSuccess: (f) => {
      // Seed the single-file cache with the saved payload BEFORE dropping the
      // draft, so `content` never transiently falls back to the stale server
      // copy (which would flash old text and jump the caret on every save).
      qc.setQueryData(["files", f.id], f);
      setDrafts((d) => {
        const next = { ...d };
        delete next[f.id];
        return next;
      });
      void qc.invalidateQueries({ queryKey: ["files"] });
      toast("success", t("tools.editor.saved", { name: f.name }));
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const remove = useMutation({
    mutationFn: async () =>
      unwrap(await api.DELETE("/files/{id}", { params: { path: { id: selectedId! } } })),
    onSuccess: () => {
      setDrafts((d) => {
        const next = { ...d };
        if (selectedId) delete next[selectedId];
        return next;
      });
      setSelectedId(null);
      setConfirmDelete(false);
      void qc.invalidateQueries({ queryKey: ["files"] });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  // Download is entirely client-side: the current buffer under the current
  // name — whatever name and extension the user typed.
  const download = () => {
    const blob = new Blob([content], { type: "text/plain;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = name.trim() || "untitled.txt";
    a.click();
    URL.revokeObjectURL(url);
  };

  // Require a successful load before editing: a blank buffer from a failed
  // GET must never become a Save that wipes the real server file.
  const editorReady = !!selectedId && (fileQuery.isSuccess || !!draft);

  return (
    <div className="flex h-[calc(100dvh-6.5rem)] min-h-0 flex-col space-y-4">
      <div className="flex items-center gap-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
          <FileCode2 className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="text-fg text-xl font-semibold tracking-tight">{t("tools.editor.title")}</h1>
          <p className="text-fg-muted text-sm">{t("tools.editor.subtitle")}</p>
        </div>
        <Button size="sm" variant="primary" onClick={() => setCreateOpen(true)}>
          <FilePlus2 className="size-3.5" />
          {t("tools.editor.newFile")}
        </Button>
      </div>

      <div className="flex min-h-0 flex-1 gap-4">
        {/* file list */}
        <Card className="flex w-64 shrink-0 flex-col overflow-hidden">
          {files.isLoading ? (
            <PageLoader className="py-10" />
          ) : (files.data?.length ?? 0) === 0 ? (
            <EmptyState
              icon={FileCode2}
              title={t("tools.editor.empty.title")}
              body={t("tools.editor.empty.body")}
            />
          ) : (
            <div className="min-h-0 flex-1 overflow-y-auto py-1.5">
              {files.data!.map((f) => {
                const fDirty = !!drafts[f.id];
                return (
                  <button
                    key={f.id}
                    type="button"
                    onClick={() => setSelectedId(f.id)}
                    className={cn(
                      "flex w-full items-center gap-2 px-3 py-2 text-left text-[13px] transition-colors",
                      f.id === selectedId ? "bg-accent-soft/60 text-fg" : "text-fg-muted hover:bg-bg-subtle hover:text-fg",
                    )}
                  >
                    <FileCode2 className="text-fg-subtle size-3.5 shrink-0" />
                    <span className="min-w-0 flex-1 truncate font-mono text-[12.5px]">
                      {drafts[f.id]?.name ?? f.name}
                    </span>
                    {fDirty && <span className="bg-accent size-1.5 shrink-0 rounded-full" />}
                    <span className="text-fg-subtle shrink-0 text-[11px] tabular-nums">{fmtSize(f.size)}</span>
                  </button>
                );
              })}
            </div>
          )}
        </Card>

        {/* editor */}
        <Card className="flex min-w-0 flex-1 flex-col overflow-hidden">
          {!selectedId ? (
            <EmptyState
              icon={FileCode2}
              title={t("tools.editor.pick.title")}
              body={t("tools.editor.pick.body")}
            />
          ) : fileQuery.isError && !draft ? (
            <EmptyState
              icon={FileCode2}
              title={t("tools.editor.loadError.title")}
              body={t("tools.editor.loadError.body")}
              action={
                <Button variant="ghost" onClick={() => void fileQuery.refetch()}>
                  {t("common.retry")}
                </Button>
              }
            />
          ) : !editorReady ? (
            <PageLoader className="py-10" />
          ) : (
            <>
              <div className="border-edge flex items-center gap-2 border-b px-3 py-2">
                <Input
                  value={name}
                  onChange={(e) => setDraft({ name: e.target.value })}
                  className="h-8 max-w-64 font-mono text-[13px]"
                  aria-label={t("tools.editor.fileName")}
                />
                <span className="bg-bg-subtle text-fg-muted rounded-md px-1.5 py-0.5 text-[11px]">{langName}</span>
                {dirty && <span className="bg-accent size-2 rounded-full" title={t("tools.editor.unsaved")} />}
                <span className="flex-1" />
                <Button size="sm" variant="primary" disabled={!dirty || save.isPending} onClick={() => save.mutate()}>
                  {save.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
                  {t("common.save")}
                </Button>
                <Button size="sm" variant="ghost" onClick={download} title={t("tools.editor.downloadHint")}>
                  <Download className="size-3.5" />
                  {t("tools.editor.download")}
                </Button>
                <button
                  type="button"
                  onClick={() => setConfirmDelete(true)}
                  className="text-fg-subtle hover:text-danger p-1.5"
                  aria-label={t("common.delete")}
                >
                  <Trash2 className="size-4" />
                </button>
              </div>

              <div className="min-h-0 flex-1">
                <Suspense fallback={<PageLoader className="py-10" />}>
                  <CodeEditor
                    key={selectedId}
                    fileName={name}
                    value={content}
                    onChange={(next) => setDraft({ content: next })}
                    onSave={() => {
                      if (dirty) save.mutate();
                    }}
                    onCursor={(line, col) => setCursor({ line, col })}
                    autoFocus
                  />
                </Suspense>
              </div>

              <div className="border-edge text-fg-subtle flex items-center gap-3 border-t px-3 py-1.5 text-[11.5px] tabular-nums">
                <span>
                  {t("tools.editor.cursor", { line: cursor.line, col: cursor.col })}
                </span>
                <span>{t("tools.editor.chars", { count: content.length })}</span>
                <span className="flex-1" />
                <span>{t("tools.editor.saveHint")}</span>
              </div>
            </>
          )}
        </Card>
      </div>

      {createOpen && (
        <CreateFileDialog
          onCreate={(n) => create.mutate(n)}
          pending={create.isPending}
          onClose={() => setCreateOpen(false)}
        />
      )}

      {confirmDelete && active && (
        <Dialog open onOpenChange={(o) => !o && setConfirmDelete(false)}>
          <DialogContent title={t("tools.editor.deleteTitle", { name: active.name })} className="max-w-sm">
            <p className="text-fg-muted text-sm">{t("tools.editor.deleteBody")}</p>
            <div className="mt-4 flex justify-end gap-2">
              <Button variant="ghost" onClick={() => setConfirmDelete(false)}>
                {t("common.cancel")}
              </Button>
              <Button variant="danger" disabled={remove.isPending} onClick={() => remove.mutate()}>
                {t("common.delete")}
              </Button>
            </div>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}

function CreateFileDialog({
  onCreate,
  pending,
  onClose,
}: {
  onCreate: (name: string) => void;
  pending: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState("untitled.txt");
  const inputRef = useRef<HTMLInputElement>(null);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={t("tools.editor.newFile")} description={t("tools.editor.newFileBody")} className="max-w-sm">
        <Field label={t("tools.editor.fileName")}>
          {(p) => (
            <Input
              {...p}
              ref={inputRef}
              value={name}
              onChange={(e) => setName(e.target.value)}
              onFocus={(e) => {
                // Preselect the stem so typing replaces "untitled" but keeps ".txt".
                const dot = e.target.value.lastIndexOf(".");
                e.target.setSelectionRange(0, dot > 0 ? dot : e.target.value.length);
              }}
              autoFocus
              className="font-mono"
              onKeyDown={(e) => {
                if (e.key === "Enter" && name.trim()) onCreate(name.trim());
              }}
            />
          )}
        </Field>
        <div className="mt-4 flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button variant="primary" disabled={pending || !name.trim()} onClick={() => onCreate(name.trim())}>
            {t("common.create")}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function fmtSize(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}
