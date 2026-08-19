import { useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  BookOpenText,
  ChevronRight,
  Download,
  File,
  FileArchive,
  FileAudio,
  FileCode2,
  FileImage,
  FileVideo,
  Folder,
  FolderOpen,
  FolderPlus,
  Pencil,
  SquarePen,
  Trash2,
  Upload,
} from "lucide-react";
import { api, isApiError, unwrap } from "@/lib/api";
import { fileLanguage, formatBytes, timeAgo } from "@/lib/file-lang";
import { cn } from "@/lib/utils";
import { WorkbenchLayout } from "./workbench";
import { Button } from "@/ui/button";
import { Card } from "@/ui/card";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { PageLoader } from "@/ui/misc";
import { EmptyState } from "@/ui/states";
import { useToast } from "@/ui/toast";

export const isMdName = (name: string) => /\.(md|markdown)$/i.test(name);

function fileIcon(name: string, isBinary: boolean, mime: string) {
  if (isMdName(name)) return BookOpenText;
  if (!isBinary) return FileCode2;
  if (mime.startsWith("image/")) return FileImage;
  if (mime.startsWith("video/")) return FileVideo;
  if (mime.startsWith("audio/")) return FileAudio;
  if (/zip|tar|compress|7z|rar/.test(mime)) return FileArchive;
  return File;
}

/**
 * The workspace: a simple personal cloud. Folders, uploads of any file type
 * from any device, downloads back out — plus the text-file workflow the
 * editor and assistant already use. Text files open in the editor, .md files
 * open as documents, binaries download.
 */
export function ToolsFilesPage() {
  const { t } = useTranslation();
  const toast = useToast();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const inputRef = useRef<HTMLInputElement>(null);
  const [path, setPath] = useState("");
  const [dragOver, setDragOver] = useState(false);
  const [renaming, setRenaming] = useState<{ id: string; name: string } | null>(null);
  const [newFolder, setNewFolder] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<{ kind: "file" | "folder"; id: string; name: string } | null>(null);
  const [uploading, setUploading] = useState(false);
  const [filter, setFilter] = useState("");

  const files = useQuery({
    queryKey: ["files"],
    queryFn: async () => unwrap(await api.GET("/files")),
  });
  const folders = useQuery({
    queryKey: ["folders"],
    queryFn: async () => unwrap(await api.GET("/folders")),
  });

  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ["files"], exact: true });
    void qc.invalidateQueries({ queryKey: ["folders"], exact: true });
  };

  // Multipart upload to the raw endpoint — any file type, straight into the
  // current folder.
  const uploadFiles = async (list: FileList | File[]) => {
    const form = new FormData();
    form.append("folder", path);
    for (const file of Array.from(list)) form.append("file", file, file.name);
    setUploading(true);
    try {
      const res = await fetch("/api/files/upload", { method: "POST", body: form, credentials: "same-origin" });
      if (!res.ok) {
        let msg = t("common.unknownError");
        try {
          msg = ((await res.json()) as { message?: string }).message ?? msg;
        } catch {
          /* not json */
        }
        toast("error", msg);
        return;
      }
      const uploaded = (await res.json()) as unknown[];
      toast("success", t("tools.files.uploaded", { count: uploaded.length }));
      refresh();
    } catch {
      toast("error", t("common.unknownError"));
    } finally {
      setUploading(false);
    }
  };

  const createFolder = useMutation({
    mutationFn: async (name: string) => {
      const full = path ? `${path}/${name}` : name;
      return unwrap(await api.POST("/folders", { body: { path: full } }));
    },
    onSuccess: () => {
      setNewFolder(null);
      refresh();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const rename = useMutation({
    mutationFn: async ({ id, name }: { id: string; name: string }) => {
      // Renames go through the full PUT: fetch the content once, write it
      // back under the new name (binary files rename in place server-side).
      const f = unwrap(await api.GET("/files/{id}", { params: { path: { id } } }));
      return unwrap(
        await api.PUT("/files/{id}", { params: { path: { id } }, body: { name, content: f.content ?? "" } }),
      );
    },
    onSuccess: () => {
      setRenaming(null);
      refresh();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const removeFile = useMutation({
    mutationFn: async (id: string) => {
      unwrap(await api.DELETE("/files/{id}", { params: { path: { id } } }));
    },
    onSuccess: () => {
      setConfirmDelete(null);
      refresh();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const removeFolder = useMutation({
    mutationFn: async (id: string) => {
      unwrap(await api.DELETE("/folders/{id}", { params: { path: { id } } }));
    },
    onSuccess: () => {
      setConfirmDelete(null);
      refresh();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const download = (id: string) => {
    // The raw endpoint streams any file with its stored name.
    const a = document.createElement("a");
    a.href = `/api/files/${id}/download`;
    a.click();
  };

  const allFiles = files.data ?? [];
  const allFolders = folders.data ?? [];

  // Current level: direct subfolders of `path` and files whose folder is
  // exactly `path`. The filter searches the whole tree by name.
  const subfolders = allFolders.filter((f) => {
    if (filter) return f.path.toLowerCase().includes(filter.toLowerCase());
    if (!path) return !f.path.includes("/");
    return f.path.startsWith(path + "/") && !f.path.slice(path.length + 1).includes("/");
  });
  const rows = allFiles.filter((f) =>
    filter ? f.name.toLowerCase().includes(filter.toLowerCase()) : f.folder === path,
  );

  const crumbs = path ? path.split("/") : [];

  return (
    <WorkbenchLayout>
      <div className="min-h-0 flex-1 space-y-4 overflow-y-auto pr-0.5">
      <div className="flex flex-wrap items-center gap-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
          <FolderOpen className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="text-fg text-xl font-semibold tracking-tight">{t("tools.files.title")}</h1>
          <p className="text-fg-muted text-sm">{t("tools.files.subtitle")}</p>
        </div>
        <Input
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder={t("tools.editor.filter")}
          className="h-9 w-44"
        />
        <Button size="sm" variant="secondary" onClick={() => setNewFolder("")}>
          <FolderPlus className="size-3.5" />
          {t("tools.files.newFolder")}
        </Button>
        <Button size="sm" variant="primary" loading={uploading} onClick={() => inputRef.current?.click()}>
          <Upload className="size-3.5" />
          {t("tools.files.upload")}
        </Button>
        <input
          ref={inputRef}
          type="file"
          multiple
          className="hidden"
          onChange={(e) => {
            if (e.target.files?.length) void uploadFiles(e.target.files);
            e.target.value = "";
          }}
        />
      </div>

      {/* breadcrumbs */}
      <div className="text-fg-muted flex flex-wrap items-center gap-1 text-[13px]">
        <button
          type="button"
          onClick={() => setPath("")}
          className={cn("hover:text-fg rounded px-1 py-0.5", !path && "text-fg font-medium")}
        >
          {t("tools.files.root")}
        </button>
        {crumbs.map((seg, i) => {
          const target = crumbs.slice(0, i + 1).join("/");
          return (
            <span key={target} className="flex items-center gap-1">
              <ChevronRight className="text-fg-subtle size-3.5" />
              <button
                type="button"
                onClick={() => setPath(target)}
                className={cn("hover:text-fg rounded px-1 py-0.5", i === crumbs.length - 1 && "text-fg font-medium")}
              >
                {seg}
              </button>
            </span>
          );
        })}
      </div>

      <Card
        className={cn("overflow-hidden transition-colors", dragOver && "border-accent bg-accent-soft/20")}
        onDragOver={(e) => {
          e.preventDefault();
          setDragOver(true);
        }}
        onDragLeave={() => setDragOver(false)}
        onDrop={(e) => {
          e.preventDefault();
          setDragOver(false);
          if (e.dataTransfer.files.length) void uploadFiles(e.dataTransfer.files);
        }}
      >
        {files.isLoading || folders.isLoading ? (
          <PageLoader className="py-12" />
        ) : subfolders.length === 0 && rows.length === 0 ? (
          <EmptyState
            icon={FolderOpen}
            title={filter ? t("tools.editor.noMatch") : t("tools.files.empty.title")}
            body={filter ? "" : t("tools.files.empty.body")}
          />
        ) : (
          <div className="divide-edge divide-y">
            {subfolders.map((f) => {
              const label = filter ? f.path : f.path.split("/").pop();
              return (
                <div key={f.id} className="group hover:bg-bg-subtle/60 flex items-center gap-3 px-4 py-2.5">
                  <Folder className="text-accent size-4 shrink-0" />
                  <button
                    type="button"
                    onClick={() => {
                      setFilter("");
                      setPath(f.path);
                    }}
                    className="text-fg hover:text-accent min-w-0 flex-1 truncate text-left font-medium text-[13px]"
                  >
                    {label}
                  </button>
                  <div className="flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100">
                    <button
                      type="button"
                      onClick={() => setConfirmDelete({ kind: "folder", id: f.id, name: f.path })}
                      className="text-fg-subtle hover:text-danger rounded-md p-1.5"
                      title={t("common.delete")}
                    >
                      <Trash2 className="size-4" />
                    </button>
                  </div>
                </div>
              );
            })}
            {rows.map((f) => {
              const md = isMdName(f.name);
              const Icon = fileIcon(f.name, f.isBinary, f.mime);
              const openRow = () => {
                if (md) void navigate({ to: "/tools/md/$fileId", params: { fileId: f.id } });
                else if (!f.isBinary) void navigate({ to: "/tools/editor", search: { file: f.id } });
                else download(f.id);
              };
              return (
                <div key={f.id} className="group hover:bg-bg-subtle/60 flex items-center gap-3 px-4 py-2.5">
                  <Icon className="text-fg-subtle size-4 shrink-0" />
                  <div className="min-w-0 flex-1">
                    <button
                      type="button"
                      onClick={openRow}
                      className="text-fg hover:text-accent block max-w-full truncate text-left font-mono text-[13px]"
                    >
                      {f.name}
                    </button>
                    <p className="text-fg-subtle text-[11.5px]">
                      {f.isBinary ? f.mime || t("tools.files.binaryFile") : fileLanguage(f.name)} ·{" "}
                      {formatBytes(f.size)} · {timeAgo(f.updatedAt)}
                      {filter && f.folder ? ` · ${f.folder}` : ""}
                    </p>
                  </div>
                  <div className="flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100">
                    {md && (
                      <Link
                        to="/tools/md/$fileId"
                        params={{ fileId: f.id }}
                        className="text-fg-subtle hover:text-fg rounded-md p-1.5"
                        title={t("tools.files.view")}
                      >
                        <BookOpenText className="size-4" />
                      </Link>
                    )}
                    {!f.isBinary && (
                      <Link
                        to="/tools/editor"
                        search={{ file: f.id }}
                        className="text-fg-subtle hover:text-fg rounded-md p-1.5"
                        title={t("tools.files.open")}
                      >
                        <SquarePen className="size-4" />
                      </Link>
                    )}
                    <button
                      type="button"
                      onClick={() => download(f.id)}
                      className="text-fg-subtle hover:text-fg rounded-md p-1.5"
                      title={t("tools.editor.download")}
                    >
                      <Download className="size-4" />
                    </button>
                    <button
                      type="button"
                      onClick={() => setRenaming({ id: f.id, name: f.name })}
                      className="text-fg-subtle hover:text-fg rounded-md p-1.5"
                      title={t("tools.files.rename")}
                    >
                      <Pencil className="size-4" />
                    </button>
                    <button
                      type="button"
                      onClick={() => setConfirmDelete({ kind: "file", id: f.id, name: f.name })}
                      className="text-fg-subtle hover:text-danger rounded-md p-1.5"
                      title={t("common.delete")}
                    >
                      <Trash2 className="size-4" />
                    </button>
                  </div>
                </div>
              );
            })}
          </div>
        )}
        <div className="border-edge text-fg-subtle border-t px-4 py-2 text-center text-[12px]">
          {t("tools.files.dropHint")}
        </div>
      </Card>

      {newFolder !== null && (
        <Dialog open onOpenChange={(o) => !o && setNewFolder(null)}>
          <DialogContent title={t("tools.files.newFolder")} className="max-w-sm">
            <Field label={t("tools.files.folderName")}>
              {(p) => (
                <Input
                  {...p}
                  autoFocus
                  value={newFolder}
                  onChange={(e) => setNewFolder(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && newFolder.trim()) createFolder.mutate(newFolder.trim());
                  }}
                />
              )}
            </Field>
            <div className="mt-4 flex justify-end gap-2">
              <Button variant="ghost" onClick={() => setNewFolder(null)}>
                {t("common.cancel")}
              </Button>
              <Button
                variant="primary"
                disabled={createFolder.isPending || !newFolder.trim()}
                onClick={() => createFolder.mutate(newFolder.trim())}
              >
                {t("common.create")}
              </Button>
            </div>
          </DialogContent>
        </Dialog>
      )}

      {renaming && (
        <Dialog open onOpenChange={(o) => !o && setRenaming(null)}>
          <DialogContent title={t("tools.files.rename")} className="max-w-sm">
            <Field label={t("tools.editor.fileName")}>
              {(p) => (
                <Input
                  {...p}
                  autoFocus
                  className="font-mono"
                  value={renaming.name}
                  onChange={(e) => setRenaming({ ...renaming, name: e.target.value })}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && renaming.name.trim())
                      rename.mutate({ id: renaming.id, name: renaming.name.trim() });
                  }}
                />
              )}
            </Field>
            <div className="mt-4 flex justify-end gap-2">
              <Button variant="ghost" onClick={() => setRenaming(null)}>
                {t("common.cancel")}
              </Button>
              <Button
                variant="primary"
                disabled={rename.isPending || !renaming.name.trim()}
                onClick={() => rename.mutate({ id: renaming.id, name: renaming.name.trim() })}
              >
                {t("common.save")}
              </Button>
            </div>
          </DialogContent>
        </Dialog>
      )}

      {confirmDelete && (
        <Dialog open onOpenChange={(o) => !o && setConfirmDelete(null)}>
          <DialogContent
            title={t("tools.editor.deleteTitle", { name: confirmDelete.name })}
            className="max-w-sm"
          >
            <p className="text-fg-muted text-sm">
              {confirmDelete.kind === "folder" ? t("tools.files.deleteFolderBody") : t("tools.editor.deleteBody")}
            </p>
            <div className="mt-4 flex justify-end gap-2">
              <Button variant="ghost" onClick={() => setConfirmDelete(null)}>
                {t("common.cancel")}
              </Button>
              <Button
                variant="danger"
                disabled={removeFile.isPending || removeFolder.isPending}
                onClick={() =>
                  confirmDelete.kind === "folder"
                    ? removeFolder.mutate(confirmDelete.id)
                    : removeFile.mutate(confirmDelete.id)
                }
              >
                {t("common.delete")}
              </Button>
            </div>
          </DialogContent>
        </Dialog>
      )}
    </div>
    </WorkbenchLayout>
  );
}
