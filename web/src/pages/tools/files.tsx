import { useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  ArrowLeft,
  Download,
  FileCode2,
  FolderOpen,
  Pencil,
  SquarePen,
  Trash2,
  Upload,
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

const MAX_BYTES = 1 << 20;

/**
 * The workspace file manager: everything the editor saves lives here, plus
 * uploads from disk (text files only — this workspace feeds the editor and
 * the assistant). Rows open in the editor, download verbatim, rename, or
 * delete.
 */
export function ToolsFilesPage() {
  const { t } = useTranslation();
  const toast = useToast();
  const qc = useQueryClient();
  const inputRef = useRef<HTMLInputElement>(null);
  const [dragOver, setDragOver] = useState(false);
  const [renaming, setRenaming] = useState<{ id: string; name: string } | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);
  const [filter, setFilter] = useState("");

  const files = useQuery({
    queryKey: ["files"],
    queryFn: async () => unwrap(await api.GET("/files")),
  });

  const refresh = () => void qc.invalidateQueries({ queryKey: ["files"], exact: true });

  // Uploads read the file as text in the browser and POST through the same
  // validated API the editor uses. Binaries are refused — the workspace is
  // text (the converter utility will be the home for binary work).
  const uploadFiles = async (list: FileList | File[]) => {
    let ok = 0;
    for (const file of Array.from(list)) {
      if (file.size > MAX_BYTES) {
        toast("error", t("tools.files.tooLarge", { name: file.name }));
        continue;
      }
      const text = await file.text();
      if (text.includes("\0")) {
        toast("error", t("tools.files.binary", { name: file.name }));
        continue;
      }
      const res = await api.POST("/files", { body: { name: file.name, content: text } });
      if (res.error) {
        toast("error", `${file.name}: ${(res.error as { message?: string }).message ?? t("common.unknownError")}`);
        continue;
      }
      ok++;
    }
    if (ok > 0) {
      toast("success", t("tools.files.uploaded", { count: ok }));
      refresh();
    }
  };

  const rename = useMutation({
    mutationFn: async ({ id, name }: { id: string; name: string }) => {
      // Renames go through the full PUT: fetch the content once, write it
      // back under the new name (content is authoritative server-side).
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

  const remove = useMutation({
    mutationFn: async (id: string) => {
      unwrap(await api.DELETE("/files/{id}", { params: { path: { id } } }));
    },
    onSuccess: () => {
      setConfirmDelete(null);
      refresh();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const download = async (id: string, name: string) => {
    try {
      const f = unwrap(await api.GET("/files/{id}", { params: { path: { id } } }));
      const blob = new Blob([f.content ?? ""], { type: "text/plain;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = name;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      toast("error", isApiError(err) ? err.message : t("common.unknownError"));
    }
  };

  const rows = (files.data ?? []).filter((f) => f.name.toLowerCase().includes(filter.toLowerCase()));

  return (
    <div className="space-y-4">
      <Link to="/tools" className="text-fg-subtle hover:text-fg inline-flex items-center gap-1.5 text-[13px]">
        <ArrowLeft className="size-3.5" /> {t("tools.backToUtilities")}
      </Link>
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
          className="h-9 w-48"
        />
        <Button size="sm" variant="primary" onClick={() => inputRef.current?.click()}>
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
        {files.isLoading ? (
          <PageLoader className="py-12" />
        ) : rows.length === 0 ? (
          <EmptyState
            icon={FolderOpen}
            title={filter ? t("tools.editor.noMatch") : t("tools.files.empty.title")}
            body={filter ? "" : t("tools.files.empty.body")}
          />
        ) : (
          <div className="divide-edge divide-y">
            {rows.map((f) => (
              <div key={f.id} className="group hover:bg-bg-subtle/60 flex items-center gap-3 px-4 py-2.5">
                <FileCode2 className="text-fg-subtle size-4 shrink-0" />
                <div className="min-w-0 flex-1">
                  <Link
                    to="/tools/editor"
                    search={{ file: f.id }}
                    className="text-fg hover:text-accent block truncate font-mono text-[13px]"
                  >
                    {f.name}
                  </Link>
                  <p className="text-fg-subtle text-[11.5px]">
                    {fileLanguage(f.name)} · {formatBytes(f.size)} · {timeAgo(f.updatedAt)}
                  </p>
                </div>
                <div className="flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100">
                  <Link
                    to="/tools/editor"
                    search={{ file: f.id }}
                    className="text-fg-subtle hover:text-fg rounded-md p-1.5"
                    title={t("tools.files.open")}
                  >
                    <SquarePen className="size-4" />
                  </Link>
                  <button
                    type="button"
                    onClick={() => void download(f.id, f.name)}
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
                    onClick={() => setConfirmDelete(f.id)}
                    className="text-fg-subtle hover:text-danger rounded-md p-1.5"
                    title={t("common.delete")}
                  >
                    <Trash2 className="size-4" />
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
        <div className="border-edge text-fg-subtle border-t px-4 py-2 text-center text-[12px]">
          {t("tools.files.dropHint")}
        </div>
      </Card>

      {renaming && (
        <Dialog open onOpenChange={(o) => !o && setRenaming(null)}>
          <DialogContent title={t("tools.files.rename")} className="max-w-sm">
            <Field label={t("tools.editor.fileName")}>
              {(p) => (
                <Input
                  {...p}
                  value={renaming.name}
                  onChange={(e) => setRenaming({ ...renaming, name: e.target.value })}
                  autoFocus
                  className="font-mono"
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && renaming.name.trim()) rename.mutate({ id: renaming.id, name: renaming.name.trim() });
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
            title={t("tools.editor.deleteTitle", { name: files.data?.find((f) => f.id === confirmDelete)?.name ?? "" })}
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
    </div>
  );
}
