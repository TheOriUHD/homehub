import { lazy, Suspense, useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useSearch } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  ArrowLeft,
  Download,
  FileCode2,
  FilePlus2,
  Loader2,
  Redo2,
  Save,
  Search,
  Trash2,
  Undo2,
  WrapText,
  X,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import { api, isApiError, unwrap } from "@/lib/api";
import { fileLanguage, formatBytes, timeAgo } from "@/lib/file-lang";
import { cn } from "@/lib/utils";
import { Button } from "@/ui/button";
import { Card } from "@/ui/card";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { PageLoader } from "@/ui/misc";
import { EmptyState } from "@/ui/states";
import { useToast } from "@/ui/toast";
import type { CodeEditorHandle } from "@/components/code-editor";

// CodeMirror and its language packs are heavy — split into their own chunk,
// loaded the first time the editor page mounts (same pattern as the consoles).
const CodeEditor = lazy(() => import("@/components/code-editor").then((m) => ({ default: m.CodeEditor })));

/**
 * The utilities text editor. THE BUFFER OWNS THE TRUTH: a file's content is
 * fetched exactly once when its tab opens and lives in local state from then
 * on — no query-cache sync, so a background refetch can never wipe or revert
 * what the user typed (the failure mode of the first version). Saving PUTs
 * the buffer and moves the baseline to the server's echo; the file LIST is
 * the only thing the query cache manages.
 */

type Buf = {
  name: string;
  content: string;
  baseName: string;
  baseContent: string;
  savedAt?: string;
};

const isDirty = (b: Buf) => b.name !== b.baseName || b.content !== b.baseContent;

export function ToolsEditorPage() {
  const { t } = useTranslation();
  const toast = useToast();
  const qc = useQueryClient();
  const search = useSearch({ from: "/shell/tools/editor" }) as { file?: string };

  const files = useQuery({
    queryKey: ["files"],
    queryFn: async () => unwrap(await api.GET("/files")),
  });

  // Open buffers (tab order) + the active one. Buffers are the only owner
  // of content once open.
  const [bufs, setBufs] = useState<Record<string, Buf>>({});
  const [openIds, setOpenIds] = useState<string[]>([]);
  const [activeId, setActiveId] = useState<string | null>(null);
  const [opening, setOpening] = useState<string | null>(null);
  const [filter, setFilter] = useState("");
  const [cursor, setCursor] = useState({ line: 1, col: 1, selected: 0 });
  const [wrap, setWrap] = useState(true);
  const [fontSize, setFontSize] = useState(13.5);
  const [createOpen, setCreateOpen] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);
  const [confirmClose, setConfirmClose] = useState<string | null>(null);
  const editorRef = useRef<CodeEditorHandle>(null);

  const active = activeId ? bufs[activeId] : undefined;

  const openFile = async (id: string) => {
    if (bufs[id]) {
      setActiveId(id);
      return;
    }
    setOpening(id);
    try {
      const f = unwrap(await api.GET("/files/{id}", { params: { path: { id } } }));
      const content = f.content ?? "";
      setBufs((b) => ({
        ...b,
        [id]: { name: f.name, content, baseName: f.name, baseContent: content },
      }));
      setOpenIds((ids) => (ids.includes(id) ? ids : [...ids, id]));
      setActiveId(id);
    } catch (err) {
      toast("error", isApiError(err) ? err.message : t("common.unknownError"));
    } finally {
      setOpening(null);
    }
  };

  // Deep link: /tools/editor?file=<id> opens that file (used by the manager).
  useEffect(() => {
    if (search.file) void openFile(search.file);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [search.file]);

  const closeTab = (id: string, force = false) => {
    const b = bufs[id];
    if (b && isDirty(b) && !force) {
      setConfirmClose(id);
      return;
    }
    setConfirmClose(null);
    setOpenIds((ids) => {
      const next = ids.filter((x) => x !== id);
      if (activeId === id) setActiveId(next[next.length - 1] ?? null);
      return next;
    });
    setBufs((b2) => {
      const next = { ...b2 };
      delete next[id];
      return next;
    });
  };

  const patchActive = (patch: Partial<Pick<Buf, "name" | "content">>) => {
    if (!activeId) return;
    setBufs((b) => (b[activeId] ? { ...b, [activeId]: { ...b[activeId], ...patch } } : b));
  };

  const create = useMutation({
    mutationFn: async (newName: string) =>
      unwrap(await api.POST("/files", { body: { name: newName, content: "" } })),
    onSuccess: (f) => {
      void qc.invalidateQueries({ queryKey: ["files"], exact: true });
      // Seed the buffer straight from the response — no extra GET, no races.
      setBufs((b) => ({
        ...b,
        [f.id]: { name: f.name, content: f.content ?? "", baseName: f.name, baseContent: f.content ?? "" },
      }));
      setOpenIds((ids) => [...ids, f.id]);
      setActiveId(f.id);
      setCreateOpen(false);
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const save = useMutation({
    mutationFn: async (id: string) => {
      const b = bufs[id];
      return unwrap(
        await api.PUT("/files/{id}", {
          params: { path: { id } },
          body: { name: b.name.trim(), content: b.content },
        }),
      );
    },
    onSuccess: (f) => {
      // Baseline moves to what the server confirmed; the buffer itself is
      // untouched — nothing can revert or flash.
      setBufs((b) =>
        b[f.id]
          ? {
              ...b,
              [f.id]: {
                ...b[f.id],
                name: f.name,
                baseName: f.name,
                baseContent: f.content ?? b[f.id].content,
                savedAt: f.updatedAt,
              },
            }
          : b,
      );
      void qc.invalidateQueries({ queryKey: ["files"], exact: true });
      toast("success", t("tools.editor.saved", { name: f.name }));
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const remove = useMutation({
    mutationFn: async (id: string) => {
      unwrap(await api.DELETE("/files/{id}", { params: { path: { id } } }));
      return id;
    },
    onSuccess: (id) => {
      closeTab(id, true);
      setConfirmDelete(null);
      void qc.invalidateQueries({ queryKey: ["files"], exact: true });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  // Download the current buffer under the current name — always exactly what
  // is on screen, saved or not.
  const download = () => {
    if (!active) return;
    const blob = new Blob([active.content], { type: "text/plain;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = active.name.trim() || "untitled.txt";
    a.click();
    URL.revokeObjectURL(url);
  };

  const filtered = (files.data ?? []).filter((f) => f.name.toLowerCase().includes(filter.toLowerCase()));
  const activeDirty = active ? isDirty(active) : false;

  return (
    <div className="flex h-[calc(100dvh-6.5rem)] min-h-0 flex-col space-y-4">
      <div className="flex items-center gap-3">
        <Link
          to="/tools"
          className="text-fg-subtle hover:text-fg hover:bg-surface-hover -ml-1 flex size-8 shrink-0 items-center justify-center rounded-lg"
          title={t("tools.backToUtilities")}
        >
          <ArrowLeft className="size-4" />
        </Link>
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
        {/* workspace file list */}
        <Card className="flex w-72 shrink-0 flex-col overflow-hidden">
          <div className="border-edge border-b p-2">
            <Input
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder={t("tools.editor.filter")}
              className="h-8 text-[13px]"
            />
          </div>
          {files.isLoading ? (
            <PageLoader className="py-10" />
          ) : filtered.length === 0 ? (
            <EmptyState
              icon={FileCode2}
              title={filter ? t("tools.editor.noMatch") : t("tools.editor.empty.title")}
              body={filter ? "" : t("tools.editor.empty.body")}
            />
          ) : (
            <div className="min-h-0 flex-1 overflow-y-auto py-1">
              {filtered.map((f) => {
                const open = !!bufs[f.id];
                const fDirty = open && isDirty(bufs[f.id]);
                return (
                  <div
                    key={f.id}
                    className={cn(
                      "group flex w-full items-center gap-2 px-3 py-2 transition-colors",
                      f.id === activeId ? "bg-accent-soft/60" : "hover:bg-bg-subtle",
                    )}
                  >
                    <button
                      type="button"
                      onClick={() => void openFile(f.id)}
                      className="flex min-w-0 flex-1 items-center gap-2 text-left"
                    >
                      <FileCode2 className={cn("size-3.5 shrink-0", open ? "text-accent" : "text-fg-subtle")} />
                      <span className="min-w-0 flex-1">
                        <span className="text-fg block truncate font-mono text-[12.5px]">
                          {bufs[f.id]?.name ?? f.name}
                        </span>
                        <span className="text-fg-subtle block text-[11px]">
                          {formatBytes(f.size)} · {timeAgo(f.updatedAt)}
                        </span>
                      </span>
                      {fDirty && <span className="bg-accent size-1.5 shrink-0 rounded-full" />}
                    </button>
                    <button
                      type="button"
                      onClick={() => setConfirmDelete(f.id)}
                      className="text-fg-subtle hover:text-danger p-1 opacity-0 transition-opacity group-hover:opacity-100"
                      aria-label={t("common.delete")}
                    >
                      <Trash2 className="size-3.5" />
                    </button>
                  </div>
                );
              })}
            </div>
          )}
        </Card>

        {/* editor area */}
        <Card className="flex min-w-0 flex-1 flex-col overflow-hidden">
          {/* tabs */}
          {openIds.length > 0 && (
            <div className="border-edge flex items-center gap-0.5 overflow-x-auto border-b px-1.5 pt-1.5">
              {openIds.map((id) => {
                const b = bufs[id];
                if (!b) return null;
                const bDirty = isDirty(b);
                return (
                  <div
                    key={id}
                    className={cn(
                      "flex shrink-0 cursor-pointer items-center gap-1.5 rounded-t-lg border border-b-0 px-3 py-1.5 text-[12.5px]",
                      id === activeId
                        ? "border-edge bg-bg-subtle text-fg"
                        : "text-fg-muted hover:text-fg border-transparent",
                    )}
                    onClick={() => setActiveId(id)}
                  >
                    <span className="max-w-44 truncate font-mono">{b.name}</span>
                    {bDirty ? (
                      <span className="bg-accent size-1.5 rounded-full" />
                    ) : (
                      <span className="size-1.5" />
                    )}
                    <button
                      type="button"
                      onClick={(e) => {
                        e.stopPropagation();
                        closeTab(id);
                      }}
                      className="text-fg-subtle hover:text-fg -mr-1 rounded p-0.5"
                      aria-label={t("common.close")}
                    >
                      <X className="size-3" />
                    </button>
                  </div>
                );
              })}
            </div>
          )}

          {!active ? (
            opening ? (
              <PageLoader className="py-10" />
            ) : (
              <EmptyState
                icon={FileCode2}
                title={t("tools.editor.pick.title")}
                body={t("tools.editor.pick.body")}
              />
            )
          ) : (
            <>
              {/* toolbar */}
              <div className="border-edge flex items-center gap-1.5 border-b px-2.5 py-1.5">
                <Input
                  value={active.name}
                  onChange={(e) => patchActive({ name: e.target.value })}
                  className="h-7.5 max-w-56 font-mono text-[12.5px]"
                  aria-label={t("tools.editor.fileName")}
                />
                <span className="bg-bg-subtle text-fg-muted rounded-md px-1.5 py-0.5 text-[11px]">
                  {fileLanguage(active.name)}
                </span>
                <span className="bg-edge mx-1 h-4 w-px" />
                <ToolButton label={t("tools.editor.undo")} onClick={() => editorRef.current?.undo()}>
                  <Undo2 className="size-3.5" />
                </ToolButton>
                <ToolButton label={t("tools.editor.redo")} onClick={() => editorRef.current?.redo()}>
                  <Redo2 className="size-3.5" />
                </ToolButton>
                <ToolButton label={t("tools.editor.find")} onClick={() => editorRef.current?.find()}>
                  <Search className="size-3.5" />
                </ToolButton>
                <ToolButton
                  label={t("tools.editor.wrapToggle")}
                  active={wrap}
                  onClick={() => {
                    const next = !wrap;
                    setWrap(next);
                    editorRef.current?.setWrap(next);
                  }}
                >
                  <WrapText className="size-3.5" />
                </ToolButton>
                <ToolButton
                  label={t("tools.editor.fontSmaller")}
                  onClick={() => {
                    const next = Math.max(10, fontSize - 1.5);
                    setFontSize(next);
                    editorRef.current?.setFontSize(next);
                  }}
                >
                  <ZoomOut className="size-3.5" />
                </ToolButton>
                <ToolButton
                  label={t("tools.editor.fontBigger")}
                  onClick={() => {
                    const next = Math.min(22, fontSize + 1.5);
                    setFontSize(next);
                    editorRef.current?.setFontSize(next);
                  }}
                >
                  <ZoomIn className="size-3.5" />
                </ToolButton>
                <span className="flex-1" />
                <Button
                  size="sm"
                  variant="primary"
                  disabled={!activeDirty || save.isPending}
                  onClick={() => activeId && save.mutate(activeId)}
                >
                  {save.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
                  {t("common.save")}
                </Button>
                <Button size="sm" variant="ghost" onClick={download} title={t("tools.editor.downloadHint")}>
                  <Download className="size-3.5" />
                  {t("tools.editor.download")}
                </Button>
              </div>

              <div className="min-h-0 flex-1">
                <Suspense fallback={<PageLoader className="py-10" />}>
                  <CodeEditor
                    key={activeId}
                    ref={editorRef}
                    fileName={active.name}
                    initialValue={active.content}
                    onChange={(next) => patchActive({ content: next })}
                    onSave={() => {
                      if (activeId && isDirty(bufs[activeId])) save.mutate(activeId);
                    }}
                    onCursor={(line, col, selected) => setCursor({ line, col, selected })}
                    autoFocus
                  />
                </Suspense>
              </div>

              {/* status bar */}
              <div className="border-edge text-fg-subtle flex items-center gap-3 border-t px-3 py-1.5 text-[11.5px] tabular-nums">
                <span>{t("tools.editor.cursor", { line: cursor.line, col: cursor.col })}</span>
                {cursor.selected > 0 && <span>{t("tools.editor.selected", { count: cursor.selected })}</span>}
                <span>{t("tools.editor.chars", { count: active.content.length })}</span>
                <span className="flex-1" />
                {activeDirty ? (
                  <span className="text-accent">{t("tools.editor.unsaved")}</span>
                ) : active.savedAt ? (
                  <span>{t("tools.editor.savedAt", { time: timeAgo(active.savedAt) })}</span>
                ) : null}
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

      {confirmDelete && (
        <Dialog open onOpenChange={(o) => !o && setConfirmDelete(null)}>
          <DialogContent
            title={t("tools.editor.deleteTitle", {
              name: files.data?.find((f) => f.id === confirmDelete)?.name ?? "",
            })}
            className="max-w-sm"
          >
            <p className="text-fg-muted text-sm">{t("tools.editor.deleteBody")}</p>
            <div className="mt-4 flex justify-end gap-2">
              <Button variant="ghost" onClick={() => setConfirmDelete(null)}>
                {t("common.cancel")}
              </Button>
              <Button variant="danger" disabled={remove.isPending} onClick={() => remove.mutate(confirmDelete)}>
                {t("common.delete")}
              </Button>
            </div>
          </DialogContent>
        </Dialog>
      )}

      {confirmClose && (
        <Dialog open onOpenChange={(o) => !o && setConfirmClose(null)}>
          <DialogContent title={t("tools.editor.closeTitle", { name: bufs[confirmClose]?.name ?? "" })} className="max-w-sm">
            <p className="text-fg-muted text-sm">{t("tools.editor.closeBody")}</p>
            <div className="mt-4 flex justify-end gap-2">
              <Button variant="ghost" onClick={() => setConfirmClose(null)}>
                {t("common.cancel")}
              </Button>
              <Button
                variant="ghost"
                onClick={() => {
                  const id = confirmClose;
                  setConfirmClose(null);
                  if (id) closeTab(id, true);
                }}
              >
                {t("tools.editor.closeDiscard")}
              </Button>
              <Button
                variant="primary"
                disabled={save.isPending}
                onClick={async () => {
                  const id = confirmClose;
                  if (!id) return;
                  await save.mutateAsync(id);
                  setConfirmClose(null);
                  closeTab(id, true);
                }}
              >
                {t("tools.editor.closeSave")}
              </Button>
            </div>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}

function ToolButton({
  label,
  active,
  onClick,
  children,
}: {
  label: string;
  active?: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      onClick={onClick}
      className={cn(
        "rounded-md p-1.5 transition-colors",
        active ? "bg-accent-soft text-accent-soft-fg" : "text-fg-subtle hover:bg-bg-subtle hover:text-fg",
      )}
    >
      {children}
    </button>
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
