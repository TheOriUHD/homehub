import { lazy, Suspense, useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useSearch } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  ArrowLeft,
  BookOpenText,
  ChevronRight,
  Code2,
  Download,
  ExternalLink,
  FileCode2,
  FilePlus2,
  Folder,
  FolderOpen,
  Loader2,
  Redo2,
  Save,
  Search,
  Undo2,
  WrapText,
  X,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import { api, isApiError, unwrap } from "@/lib/api";
import { fileLanguage, formatBytes, timeAgo } from "@/lib/file-lang";
import { cn } from "@/lib/utils";
import { toolsBackTarget } from "./workbench";
import { Button } from "@/ui/button";
import { Card } from "@/ui/card";
import { Dialog, DialogContent } from "@/ui/dialog";
import {
  Dropdown,
  DropdownContent,
  DropdownItem,
  DropdownSeparator,
  DropdownSub,
  DropdownSubContent,
  DropdownSubTrigger,
  DropdownTrigger,
} from "@/ui/dropdown";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { PageLoader } from "@/ui/misc";
import { EmptyState } from "@/ui/states";
import { useToast } from "@/ui/toast";
import type { CodeEditorHandle } from "@/components/code-editor";

// CodeMirror and its language packs are heavy — split into their own chunk,
// loaded the first time the editor page mounts (same pattern as the consoles).
const CodeEditor = lazy(() => import("@/components/code-editor").then((m) => ({ default: m.CodeEditor })));
// Milkdown (WYSIWYG markdown) is heavier still — only .md files in rich mode
// ever load it.
const MilkdownEditor = lazy(() => import("@/components/milkdown-editor"));

const isMdName = (name: string) => /\.(md|markdown)$/i.test(name.trim());

/**
 * The utilities text editor, laid out like a desktop editor: a File menu
 * (New / Open… / Open Recent / Save / Save As… / Download), tabs, and the
 * full page width for editing. THE BUFFER OWNS THE TRUTH: a file's content
 * is fetched exactly once when its tab opens and lives in local state from
 * then on — no query-cache sync, so a background refetch can never wipe or
 * revert what the user typed. Saving PUTs the buffer and moves the baseline
 * to the server's echo.
 */

type Buf = {
  name: string;
  content: string;
  baseName: string;
  baseContent: string;
  savedAt?: string;
  /** A scratch buffer that has never been saved — no server file behind it. */
  draft?: boolean;
};

// A draft is worth keeping only once it has content; a saved file is dirty on
// any divergence from the server baseline.
const isDirty = (b: Buf) => (b.draft ? b.content !== "" : b.name !== b.baseName || b.content !== b.baseContent);

// ---- recent files (localStorage, survives sessions) -------------------------

type Recent = { id: string; name: string };
const RECENTS_KEY = "hh-editor-recents";

function readRecents(): Recent[] {
  try {
    const raw = JSON.parse(localStorage.getItem(RECENTS_KEY) ?? "[]") as Recent[];
    return Array.isArray(raw) ? raw.filter((r) => r && typeof r.id === "string") : [];
  } catch {
    return [];
  }
}

function pushRecent(entry: Recent) {
  const next = [entry, ...readRecents().filter((r) => r.id !== entry.id)].slice(0, 10);
  localStorage.setItem(RECENTS_KEY, JSON.stringify(next));
}

function dropRecent(id: string) {
  localStorage.setItem(RECENTS_KEY, JSON.stringify(readRecents().filter((r) => r.id !== id)));
}

export function ToolsEditorPage() {
  const { t } = useTranslation();
  const toast = useToast();
  const qc = useQueryClient();
  const search = useSearch({ from: "/shell/tools/editor" }) as { file?: string };

  // Open buffers (tab order) + the active one. Buffers are the only owner
  // of content once open.
  const [bufs, setBufs] = useState<Record<string, Buf>>({});
  const [openIds, setOpenIds] = useState<string[]>([]);
  const [activeId, setActiveId] = useState<string | null>(null);
  const [opening, setOpening] = useState<string | null>(null);
  const [cursor, setCursor] = useState({ line: 1, col: 1, selected: 0 });
  const [wrap, setWrap] = useState(true);
  const [fontSize, setFontSize] = useState(13.5);
  // First save of a draft (or Save As) prompts for name+extension; closeAfter
  // closes the tab once the save lands.
  const [saveAs, setSaveAs] = useState<{ id: string; closeAfter: boolean } | null>(null);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [confirmClose, setConfirmClose] = useState<string | null>(null);
  // Per-buffer editing mode for .md files: source (CodeMirror) or rich
  // (rendered markdown, edited in place).
  const [richMode, setRichMode] = useState<Record<string, boolean>>({});
  const editorRef = useRef<CodeEditorHandle>(null);
  const draftSeq = useRef(0);
  const initRef = useRef(false);

  const active = activeId ? bufs[activeId] : undefined;
  const activeIsMd = !!active && isMdName(active.name);
  const activeRich = activeIsMd && !!activeId && !!richMode[activeId];

  // Open a fresh scratch buffer — like any editor: "untitled", no extension,
  // named for real on first save.
  const newDraft = () => {
    const n = ++draftSeq.current;
    const id = `draft:${n}`;
    const name = n === 1 ? "untitled" : `untitled ${n}`;
    setBufs((b) => ({ ...b, [id]: { name, content: "", baseName: name, baseContent: "", draft: true } }));
    setOpenIds((ids) => [...ids, id]);
    setActiveId(id);
  };

  const openFile = async (id: string) => {
    if (bufs[id]) {
      setActiveId(id);
      return;
    }
    setOpening(id);
    try {
      const f = unwrap(await api.GET("/files/{id}", { params: { path: { id } } }));
      if (f.isBinary) {
        toast("error", t("tools.editor.binaryRefused"));
        return;
      }
      const content = f.content ?? "";
      setBufs((b) => ({
        ...b,
        [id]: { name: f.name, content, baseName: f.name, baseContent: content },
      }));
      setOpenIds((ids) => (ids.includes(id) ? ids : [...ids, id]));
      setActiveId(id);
      pushRecent({ id, name: f.name });
    } catch (err) {
      dropRecent(id); // deleted or inaccessible — stop suggesting it
      toast("error", isApiError(err) ? err.message : t("common.unknownError"));
    } finally {
      setOpening(null);
    }
  };

  // Deep link: /tools/editor?file=<id> opens that file (used by the manager).
  // Otherwise the editor opens with a scratch buffer ready to paste into.
  useEffect(() => {
    if (search.file) {
      initRef.current = true;
      void openFile(search.file);
    } else if (!initRef.current) {
      initRef.current = true;
      newDraft();
    }
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

  // First save of a draft / Save As: create the server file with the buffer's
  // content, then swap the buffer over to the new file id in place (same tab).
  const createAs = useMutation({
    mutationFn: async (vars: { draftId: string; name: string; closeAfter: boolean }) =>
      unwrap(await api.POST("/files", { body: { name: vars.name, content: bufs[vars.draftId]?.content ?? "" } })),
    onSuccess: (f, vars) => {
      void qc.invalidateQueries({ queryKey: ["files"], exact: true });
      setSaveAs(null);
      pushRecent({ id: f.id, name: f.name });
      if (vars.closeAfter) {
        closeTab(vars.draftId, true);
      } else {
        setBufs((b) => {
          const { [vars.draftId]: old, ...rest } = b;
          return {
            ...rest,
            [f.id]: {
              name: f.name,
              content: old?.content ?? f.content ?? "",
              baseName: f.name,
              baseContent: f.content ?? "",
              savedAt: f.updatedAt,
            },
          };
        });
        setOpenIds((ids) => ids.map((x) => (x === vars.draftId ? f.id : x)));
        setActiveId((prev) => (prev === vars.draftId ? f.id : prev));
        setRichMode((m) => (m[vars.draftId] ? { ...m, [f.id]: true } : m));
      }
      toast("success", t("tools.editor.saved", { name: f.name }));
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
      pushRecent({ id: f.id, name: f.name });
      void qc.invalidateQueries({ queryKey: ["files"], exact: true });
      toast("success", t("tools.editor.saved", { name: f.name }));
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  // Save routes by buffer kind: drafts prompt for name+extension (save-as),
  // saved files PUT straight back.
  const saveBuffer = (id: string) => {
    const b = bufs[id];
    if (!b || !isDirty(b)) return;
    if (b.draft) setSaveAs({ id, closeAfter: false });
    else save.mutate(id);
  };

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

  const activeDirty = active ? isDirty(active) : false;
  const saving = save.isPending || createAs.isPending;
  const recents = readRecents();

  return (
    <div className="flex h-[calc(100dvh-6.5rem)] min-h-0 flex-col space-y-3">
      <div className="flex items-center gap-3">
        <Link
          to={toolsBackTarget()}
          className="text-fg-subtle hover:text-fg hover:bg-surface-hover -ml-1 flex size-8 shrink-0 items-center justify-center rounded-lg"
          title={t("tools.backToUtilities")}
        >
          <ArrowLeft className="size-4" />
        </Link>
        <span className="bg-accent-soft text-accent-soft-fg flex size-9 items-center justify-center rounded-xl">
          <FileCode2 className="size-4.5" />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="text-fg text-lg font-semibold tracking-tight">{t("tools.editor.title")}</h1>
        </div>
      </div>

      <Card className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
        {/* menu + tab strip */}
        <div className="border-edge flex items-center gap-1 border-b px-1.5 py-1">
          <Dropdown>
            <DropdownTrigger asChild>
              <button
                type="button"
                className="text-fg hover:bg-surface-hover data-[state=open]:bg-surface-hover rounded-md px-2.5 py-1 text-[13px] font-medium"
              >
                {t("tools.editor.menu.file")}
              </button>
            </DropdownTrigger>
            <DropdownContent align="start">
              <DropdownItem onSelect={newDraft}>
                <FilePlus2 className="size-3.5" />
                {t("tools.editor.menu.new")}
              </DropdownItem>
              <DropdownItem onSelect={() => setPickerOpen(true)}>
                <FolderOpen className="size-3.5" />
                {t("tools.editor.menu.open")}
              </DropdownItem>
              <DropdownSub>
                <DropdownSubTrigger disabled={recents.length === 0}>
                  <BookOpenText className="size-3.5" />
                  {t("tools.editor.menu.openRecent")}
                  <ChevronRight className="text-fg-subtle ml-auto size-3.5" />
                </DropdownSubTrigger>
                <DropdownSubContent>
                  {recents.map((r) => (
                    <DropdownItem key={r.id} onSelect={() => void openFile(r.id)}>
                      <span className="truncate font-mono text-[12.5px]">{r.name}</span>
                    </DropdownItem>
                  ))}
                </DropdownSubContent>
              </DropdownSub>
              <DropdownSeparator />
              <DropdownItem disabled={!active || !activeDirty || saving} onSelect={() => activeId && saveBuffer(activeId)}>
                <Save className="size-3.5" />
                {t("tools.editor.menu.save")}
              </DropdownItem>
              <DropdownItem
                disabled={!active || saving}
                onSelect={() => activeId && setSaveAs({ id: activeId, closeAfter: false })}
              >
                <Save className="size-3.5" />
                {t("tools.editor.menu.saveAs")}
              </DropdownItem>
              <DropdownSeparator />
              <DropdownItem disabled={!active} onSelect={download}>
                <Download className="size-3.5" />
                {t("tools.editor.download")}
              </DropdownItem>
            </DropdownContent>
          </Dropdown>

          <span className="bg-edge h-4 w-px" />

          <div className="flex min-w-0 flex-1 items-center gap-0.5 overflow-x-auto">
            {openIds.map((id) => {
              const b = bufs[id];
              if (!b) return null;
              const bDirty = isDirty(b);
              return (
                <div
                  key={id}
                  className={cn(
                    "flex shrink-0 cursor-pointer items-center gap-1.5 rounded-md px-2.5 py-1 text-[12.5px]",
                    id === activeId ? "bg-bg-subtle text-fg" : "text-fg-muted hover:text-fg",
                  )}
                  onClick={() => setActiveId(id)}
                >
                  <span className="max-w-44 truncate font-mono">{b.name}</span>
                  {bDirty ? <span className="bg-accent size-1.5 rounded-full" /> : <span className="size-1.5" />}
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
        </div>

        {!active ? (
          opening ? (
            <PageLoader className="py-10" />
          ) : (
            <EmptyState
              icon={FileCode2}
              title={t("tools.editor.pick.title")}
              body={t("tools.editor.pick.body")}
              action={
                <div className="flex gap-2">
                  <Button size="sm" variant="primary" onClick={newDraft}>
                    <FilePlus2 className="size-3.5" />
                    {t("tools.editor.menu.new")}
                  </Button>
                  <Button size="sm" variant="secondary" onClick={() => setPickerOpen(true)}>
                    <FolderOpen className="size-3.5" />
                    {t("tools.editor.menu.open")}
                  </Button>
                </div>
              }
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
              {activeIsMd && (
                <>
                  <span className="bg-edge mx-1 h-4 w-px" />
                  <div className="border-edge flex overflow-hidden rounded-lg border">
                    <button
                      type="button"
                      onClick={() => activeId && setRichMode((m) => ({ ...m, [activeId]: false }))}
                      className={cn(
                        "flex items-center gap-1 px-2 py-1 text-[11.5px] transition-colors",
                        !activeRich
                          ? "bg-accent-soft text-accent-soft-fg font-medium"
                          : "text-fg-muted hover:text-fg hover:bg-bg-subtle",
                      )}
                    >
                      <Code2 className="size-3" />
                      {t("tools.editor.modeText")}
                    </button>
                    <button
                      type="button"
                      onClick={() => activeId && setRichMode((m) => ({ ...m, [activeId]: true }))}
                      className={cn(
                        "flex items-center gap-1 px-2 py-1 text-[11.5px] transition-colors",
                        activeRich
                          ? "bg-accent-soft text-accent-soft-fg font-medium"
                          : "text-fg-muted hover:text-fg hover:bg-bg-subtle",
                      )}
                    >
                      <BookOpenText className="size-3" />
                      {t("tools.editor.modeRich")}
                    </button>
                  </div>
                  {!active.draft && activeId && (
                    <a
                      href={`/md/${activeId}`}
                      target="_blank"
                      rel="noreferrer"
                      title={t("tools.editor.fullPage")}
                      className="text-fg-subtle hover:bg-bg-subtle hover:text-fg rounded-md p-1.5 transition-colors"
                    >
                      <ExternalLink className="size-3.5" />
                    </a>
                  )}
                </>
              )}
              {!activeRich && (
                <>
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
                </>
              )}
              <span className="flex-1" />
              <Button
                size="sm"
                variant="primary"
                disabled={!activeDirty || saving}
                onClick={() => activeId && saveBuffer(activeId)}
              >
                {saving ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
                {t("common.save")}
              </Button>
            </div>

            <div
              className="min-h-0 flex-1"
              onKeyDownCapture={(e) => {
                // Milkdown owns the keymap in rich mode — intercept Ctrl+S
                // before it reaches the document.
                if (activeRich && (e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "s") {
                  e.preventDefault();
                  if (activeId) saveBuffer(activeId);
                }
              }}
            >
              <Suspense fallback={<PageLoader className="py-10" />}>
                {activeRich ? (
                  <MilkdownEditor
                    key={`${activeId}:rich`}
                    initialValue={active.content}
                    onChange={(md) => patchActive({ content: md })}
                  />
                ) : (
                  <CodeEditor
                    key={activeId}
                    ref={editorRef}
                    fileName={active.name}
                    initialValue={active.content}
                    onChange={(next) => patchActive({ content: next })}
                    onSave={() => {
                      if (activeId) saveBuffer(activeId);
                    }}
                    onCursor={(line, col, selected) => setCursor({ line, col, selected })}
                    autoFocus
                  />
                )}
              </Suspense>
            </div>

            {/* status bar */}
            <div className="border-edge text-fg-subtle flex items-center gap-3 border-t px-3 py-1.5 text-[11.5px] tabular-nums">
              {!activeRich && (
                <span>{t("tools.editor.cursor", { line: cursor.line, col: cursor.col })}</span>
              )}
              {!activeRich && cursor.selected > 0 && (
                <span>{t("tools.editor.selected", { count: cursor.selected })}</span>
              )}
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

      {saveAs && bufs[saveAs.id] && (
        <SaveFileDialog
          initialName={bufs[saveAs.id].name}
          onSave={(n) => createAs.mutate({ draftId: saveAs.id, name: n, closeAfter: saveAs.closeAfter })}
          pending={createAs.isPending}
          onClose={() => setSaveAs(null)}
        />
      )}

      {pickerOpen && (
        <FilePickerDialog
          onPick={(id) => {
            setPickerOpen(false);
            void openFile(id);
          }}
          onClose={() => setPickerOpen(false)}
        />
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
                  if (bufs[id]?.draft) {
                    // Never-saved buffer: name it first; the tab closes once
                    // the save lands.
                    setConfirmClose(null);
                    setSaveAs({ id, closeAfter: true });
                    return;
                  }
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

/** The windowed file manager behind File → Open…: navigate folders, pick a
 * text file. Binaries stay visible but inert — this is a text editor. */
function FilePickerDialog({ onPick, onClose }: { onPick: (id: string) => void; onClose: () => void }) {
  const { t } = useTranslation();
  const [path, setPath] = useState("");

  const files = useQuery({
    queryKey: ["files"],
    queryFn: async () => unwrap(await api.GET("/files")),
  });
  const folders = useQuery({
    queryKey: ["folders"],
    queryFn: async () => unwrap(await api.GET("/folders")),
  });

  const subfolders = (folders.data ?? []).filter((f) =>
    !path ? !f.path.includes("/") : f.path.startsWith(path + "/") && !f.path.slice(path.length + 1).includes("/"),
  );
  const rows = (files.data ?? []).filter((f) => f.folder === path);
  const crumbs = path ? path.split("/") : [];

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={t("tools.editor.picker.title")} className="max-w-lg">
        <div className="text-fg-muted mb-2 flex flex-wrap items-center gap-1 text-[12.5px]">
          <button
            type="button"
            onClick={() => setPath("")}
            className={cn("hover:text-fg rounded px-1 py-0.5", !path && "text-fg font-medium")}
          >
            {t("tools.files.root")}
          </button>
          {crumbs.map((seg, i) => (
            <span key={i} className="flex items-center gap-1">
              <ChevronRight className="text-fg-subtle size-3" />
              <button
                type="button"
                onClick={() => setPath(crumbs.slice(0, i + 1).join("/"))}
                className={cn("hover:text-fg rounded px-1 py-0.5", i === crumbs.length - 1 && "text-fg font-medium")}
              >
                {seg}
              </button>
            </span>
          ))}
        </div>
        <div className="border-edge max-h-80 overflow-y-auto rounded-lg border">
          {files.isLoading || folders.isLoading ? (
            <PageLoader className="py-8" />
          ) : subfolders.length === 0 && rows.length === 0 ? (
            <p className="text-fg-subtle px-4 py-8 text-center text-[13px]">{t("tools.editor.picker.empty")}</p>
          ) : (
            <div className="divide-edge divide-y">
              {subfolders.map((f) => (
                <button
                  key={f.id}
                  type="button"
                  onClick={() => setPath(f.path)}
                  className="hover:bg-bg-subtle flex w-full items-center gap-2.5 px-3.5 py-2 text-left"
                >
                  <Folder className="text-accent size-4 shrink-0" />
                  <span className="text-fg truncate text-[13px] font-medium">{f.path.split("/").pop()}</span>
                </button>
              ))}
              {rows.map((f) => (
                <button
                  key={f.id}
                  type="button"
                  disabled={f.isBinary}
                  onClick={() => onPick(f.id)}
                  className={cn(
                    "flex w-full items-center gap-2.5 px-3.5 py-2 text-left",
                    f.isBinary ? "cursor-not-allowed opacity-45" : "hover:bg-bg-subtle",
                  )}
                >
                  <FileCode2 className="text-fg-subtle size-4 shrink-0" />
                  <span className="text-fg min-w-0 flex-1 truncate font-mono text-[12.5px]">{f.name}</span>
                  <span className="text-fg-subtle shrink-0 text-[11px]">
                    {formatBytes(f.size)} · {timeAgo(f.updatedAt)}
                  </span>
                </button>
              ))}
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function SaveFileDialog({
  initialName,
  onSave,
  pending,
  onClose,
}: {
  initialName: string;
  onSave: (name: string) => void;
  pending: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState(initialName);
  const inputRef = useRef<HTMLInputElement>(null);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent
        title={t("tools.editor.saveAsTitle")}
        description={t("tools.editor.saveAsBody")}
        className="max-w-sm"
      >
        <Field label={t("tools.editor.fileName")}>
          {(p) => (
            <Input
              {...p}
              ref={inputRef}
              value={name}
              onChange={(e) => setName(e.target.value)}
              onFocus={(e) => {
                // Preselect the stem so typing replaces "untitled" but keeps
                // an extension, if there is one.
                const dot = e.target.value.lastIndexOf(".");
                e.target.setSelectionRange(0, dot > 0 ? dot : e.target.value.length);
              }}
              autoFocus
              className="font-mono"
              onKeyDown={(e) => {
                if (e.key === "Enter" && name.trim()) onSave(name.trim());
              }}
            />
          )}
        </Field>
        <div className="mt-4 flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button variant="primary" disabled={pending || !name.trim()} onClick={() => onSave(name.trim())}>
            {t("common.save")}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
