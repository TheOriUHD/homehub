import { useCallback, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowRight,
  Aperture,
  CheckCircle2,
  Clock,
  Download,
  File as FileIcon,
  FileText,
  Film,
  Image as ImageIcon,
  Loader2,
  Music,
  Package,
  Table2,
  Trash2,
  Upload,
  Video,
  X,
  XCircle,
  type LucideIcon,
} from "lucide-react";
import {
  api,
  unwrap,
  type ConvertBatch,
  type ConvertCatalog,
  type ConvertFormat,
  type ConvertItem,
  type ConvertOption,
  type ConvertOutput,
} from "@/lib/api";
import { cn } from "@/lib/utils";
import { WorkbenchLayout } from "./workbench";
import { formatBytes } from "@/lib/file-lang";
import { useLiveInvalidate } from "@/lib/live";
import { Button } from "@/ui/button";
import { Card } from "@/ui/card";
import { Badge } from "@/ui/badge";
import { Slider } from "@/ui/slider";
import { ConfirmDialog } from "@/ui/confirm";
import { useToast } from "@/ui/toast";

const CATEGORY_ICON: Record<string, LucideIcon> = {
  image: ImageIcon,
  raw: Aperture,
  video: Video,
  audio: Music,
  animation: Film,
  table: Table2,
  document: FileText,
};

const CATEGORY_LABEL: Record<string, string> = {
  image: "Images",
  raw: "Camera RAW",
  video: "Video",
  audio: "Audio",
  animation: "Animation",
  table: "Tables",
  document: "Documents",
};

const CATEGORY_ORDER = ["image", "raw", "video", "audio", "animation", "table", "document"];

/** A file the user has staged, with a client-side format guess (the server is
 *  authoritative once uploaded). */
type Picked = { key: string; file: File; format: ConvertFormat | null };

let pickCounter = 0;

/** The universal file converter — its own page under /tools, not a registry
 *  tool. Two phases: stage files + pick a target, then watch the batch run. */
export function ToolsConvertPage() {
  const toast = useToast();
  const queryClient = useQueryClient();

  const catalogQuery = useQuery({
    queryKey: ["convert", "formats"],
    queryFn: async () => unwrap(await api.GET("/convert/formats")),
    staleTime: 60_000,
  });
  const catalog = catalogQuery.data;

  const index = useMemo(() => buildIndex(catalog), [catalog]);

  const [picked, setPicked] = useState<Picked[]>([]);
  const [target, setTarget] = useState<string | null>(null);
  const [options, setOptions] = useState<Record<string, number>>({});
  const [browseCategory, setBrowseCategory] = useState<string | null>(null);
  const [activeBatchId, setActiveBatchId] = useState<string | null>(null);
  const [progress, setProgress] = useState<number | null>(null);

  const addFiles = useCallback(
    (files: FileList | File[]) => {
      const next: Picked[] = [];
      for (const file of Array.from(files)) {
        next.push({ key: `p${++pickCounter}`, file, format: index.guess(file.name) });
      }
      if (next.length === 0) return;
      setPicked((prev) => [...prev, ...next].slice(0, 50));
      setBrowseCategory(null);
    },
    [index],
  );

  const removeFile = (key: string) => setPicked((prev) => prev.filter((p) => p.key !== key));
  const clearAll = () => {
    setPicked([]);
    setTarget(null);
    setOptions({});
  };

  const supported = picked.filter((p) => p.format);
  const targets = useMemo(() => commonTargets(supported, index), [supported, index]);
  const chosen = targets.find((t) => t.output.format === target) ?? null;
  const activeOptions: ConvertOption[] = chosen?.output.options ?? [];

  // Keep the selected target valid as the file set changes.
  if (target && !targets.some((t) => t.output.format === target)) {
    setTarget(null);
  }

  const upload = useMutation({
    mutationFn: async () => {
      if (!target) throw new Error("pick a target format");
      const opts: Record<string, number> = {};
      for (const opt of activeOptions) {
        opts[opt.name] = options[opt.name] ?? opt.default;
      }
      return uploadConversion(picked.map((p) => p.file), target, opts, setProgress);
    },
    onSuccess: (batch) => {
      setProgress(null);
      setActiveBatchId(batch.id);
      queryClient.setQueryData(["convert", "batch", batch.id], batch);
      queryClient.invalidateQueries({ queryKey: ["convert", "list"] });
      clearAll();
    },
    onError: (err: Error) => {
      setProgress(null);
      toast("error", "Upload failed", err.message);
    },
  });

  const totalBytes = picked.reduce((n, p) => n + p.file.size, 0);
  const overCap = picked.length > 50 || totalBytes > 2 * 1024 ** 3;

  return (
    <WorkbenchLayout>
      <div className="space-y-6">
      <div className="space-y-3">
        <div className="flex items-center gap-3">
          <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
            <ArrowRight className="size-5" />
          </span>
          <div>
            <h1 className="text-fg text-xl font-semibold tracking-tight">Converter</h1>
            <p className="text-fg-muted text-sm">
              Convert images, camera RAW, video, audio, animations, tables and documents — in batches, on this server.
            </p>
          </div>
        </div>
        {catalog && <ToolBar catalog={catalog} />}
      </div>

      {/* INPUT: drop zone + staged files */}
      <Card className="p-5">
        <DropZone onFiles={addFiles} count={picked.length} />

        {picked.length > 0 && (
          <div className="mt-4 space-y-1.5">
            <div className="mb-1 flex items-center justify-between">
              <span className="text-fg-subtle text-[11px] font-semibold uppercase tracking-wide">
                {picked.length} file{picked.length === 1 ? "" : "s"} · {formatBytes(totalBytes)}
              </span>
              <button onClick={clearAll} className="text-fg-subtle hover:text-fg text-[12px]">
                Clear all
              </button>
            </div>
            {picked.map((p) => (
              <FileRow key={p.key} picked={p} onRemove={() => removeFile(p.key)} />
            ))}
            {overCap && (
              <p className="text-danger pt-1 text-[12.5px]">
                Batches are limited to 50 files and 2 GiB total. Remove some files to continue.
              </p>
            )}
          </div>
        )}

        {picked.length === 0 && catalog && (
          <CategoryBrowser
            catalog={catalog}
            active={browseCategory}
            onPick={(c) => setBrowseCategory((cur) => (cur === c ? null : c))}
          />
        )}
      </Card>

      {/* OUTPUT: target grid + options + convert */}
      {supported.length > 0 && (
        <Card className="p-5">
          <h2 className="text-fg text-[13px] font-semibold">Convert to</h2>
          <p className="text-fg-muted mt-0.5 mb-3 text-[12.5px]">
            Formats every selected file can convert to.{" "}
            {picked.length > supported.length && (
              <span className="text-warning">
                {picked.length - supported.length} unsupported file{picked.length - supported.length === 1 ? "" : "s"} will be skipped.
              </span>
            )}
          </p>

          {targets.length === 0 ? (
            <p className="text-fg-muted text-[13px]">
              The selected files don’t share an output format. Convert them in separate batches.
            </p>
          ) : (
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4">
              {targets.map((t) => (
                <TargetTile
                  key={t.output.format}
                  target={t}
                  selected={target === t.output.format}
                  onSelect={() => setTarget(t.output.format)}
                />
              ))}
            </div>
          )}

          {chosen && activeOptions.length > 0 && (
            <div className="border-edge mt-4 space-y-4 border-t pt-4">
              {activeOptions.map((opt) => (
                <OptionSlider
                  key={opt.name}
                  option={opt}
                  value={options[opt.name] ?? opt.default}
                  onChange={(v) => setOptions((o) => ({ ...o, [opt.name]: v }))}
                />
              ))}
            </div>
          )}

          <div className="mt-5 flex items-center gap-3">
            <Button
              variant="primary"
              size="lg"
              disabled={!target || overCap || upload.isPending}
              loading={upload.isPending}
              onClick={() => upload.mutate()}
            >
              <Upload className="size-4" />
              Convert {supported.length} file{supported.length === 1 ? "" : "s"}
            </Button>
            {progress !== null && (
              <div className="flex flex-1 items-center gap-2">
                <div className="bg-bg-subtle h-1.5 flex-1 overflow-hidden rounded-full">
                  <div className="bg-accent h-full transition-[width] duration-150" style={{ width: `${progress}%` }} />
                </div>
                <span className="text-fg-subtle w-10 text-right text-[12px] tabular-nums">{progress}%</span>
              </div>
            )}
          </div>
        </Card>
      )}

      {/* Active batch */}
      {activeBatchId && (
        <ActiveBatch id={activeBatchId} onDismiss={() => setActiveBatchId(null)} />
      )}

      {/* Recent conversions */}
      <RecentConversions activeId={activeBatchId} onOpen={setActiveBatchId} />
      </div>
    </WorkbenchLayout>
  );
}

// ---- tool availability bar -------------------------------------------------

function ToolBar({ catalog }: { catalog: ConvertCatalog }) {
  const tools: { key: keyof ConvertCatalog["tools"]; label: string }[] = [
    { key: "imagemagick", label: "ImageMagick" },
    { key: "ffmpeg", label: "ffmpeg" },
    { key: "libraw", label: "libraw" },
    { key: "pandoc", label: "pandoc" },
  ];
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {tools.map((tool) => {
        const ok = catalog.tools[tool.key];
        return (
          <Badge key={tool.key} tone={ok ? "success" : "neutral"} title={ok ? `${tool.label} available` : `${tool.label} not installed`}>
            <span className={cn("size-1.5 rounded-full", ok ? "bg-success" : "bg-fg-subtle")} />
            {tool.label}
          </Badge>
        );
      })}
      <span className="text-fg-subtle ml-1 text-[11.5px]">Tables always work — no tool required.</span>
    </div>
  );
}

// ---- drop zone -------------------------------------------------------------

function DropZone({ onFiles, count }: { onFiles: (f: FileList) => void; count: number }) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [over, setOver] = useState(false);
  return (
    <div
      onDragOver={(e) => {
        e.preventDefault();
        setOver(true);
      }}
      onDragLeave={() => setOver(false)}
      onDrop={(e) => {
        e.preventDefault();
        setOver(false);
        if (e.dataTransfer.files.length) onFiles(e.dataTransfer.files);
      }}
      onClick={() => inputRef.current?.click()}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") inputRef.current?.click();
      }}
      className={cn(
        "flex cursor-pointer flex-col items-center justify-center gap-2 rounded-xl border-2 border-dashed px-6 py-10 text-center transition-colors",
        over ? "border-accent bg-accent-soft/30" : "border-edge hover:border-accent/60 hover:bg-bg-subtle/40",
      )}
    >
      <span className="bg-bg-subtle text-fg-muted flex size-11 items-center justify-center rounded-xl">
        <Upload className="size-5" />
      </span>
      <p className="text-fg text-[14px] font-medium">
        {count > 0 ? "Add more files" : "Drop files here, or click to browse"}
      </p>
      <p className="text-fg-subtle text-[12px]">Up to 50 files · 1 GiB each · 2 GiB per batch</p>
      <input
        ref={inputRef}
        type="file"
        multiple
        className="hidden"
        onChange={(e) => {
          if (e.target.files?.length) onFiles(e.target.files);
          e.target.value = "";
        }}
      />
    </div>
  );
}

// ---- category browser (shown before any files are staged) ------------------

function CategoryBrowser({
  catalog,
  active,
  onPick,
}: {
  catalog: ConvertCatalog;
  active: string | null;
  onPick: (c: string) => void;
}) {
  const cats = CATEGORY_ORDER.filter((c) => catalog.categories.some((cc) => cc.id === c));
  const activeFormats = active ? catalog.formats.filter((f) => f.category === active && f.outputs.length > 0) : [];
  return (
    <div className="mt-5">
      <p className="text-fg-subtle mb-1.5 text-[11px] font-semibold uppercase tracking-wide">Or see what you can convert</p>
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4">
        {cats.map((c) => {
          const Icon = CATEGORY_ICON[c] ?? FileIcon;
          return (
            <button
              key={c}
              onClick={() => onPick(c)}
              className={cn(
                "flex cursor-pointer items-center gap-2.5 rounded-lg border px-3 py-2.5 text-left transition-colors",
                active === c ? "border-accent bg-accent-soft/30" : "border-edge hover:border-accent hover:bg-accent-soft/30",
              )}
            >
              <Icon className="text-fg-subtle size-4 shrink-0" />
              <span className="text-fg truncate text-[13.5px] font-medium">{CATEGORY_LABEL[c] ?? c}</span>
            </button>
          );
        })}
      </div>
      {active && (
        <div className="text-fg-muted mt-3 text-[12.5px]">
          <span className="text-fg-subtle">{CATEGORY_LABEL[active]} in:</span>{" "}
          {activeFormats.map((f) => f.extensions[0]).join(", ")} — drop any of these above to start.
        </div>
      )}
    </div>
  );
}

// ---- staged file row -------------------------------------------------------

function FileRow({ picked, onRemove }: { picked: Picked; onRemove: () => void }) {
  const Icon = picked.format ? CATEGORY_ICON[picked.format.category] ?? FileIcon : FileIcon;
  return (
    <div className="border-edge flex items-center gap-3 rounded-lg border px-3 py-2">
      <Icon className={cn("size-4 shrink-0", picked.format ? "text-fg-muted" : "text-warning")} />
      <span className="text-fg min-w-0 flex-1 truncate text-[13px]">{picked.file.name}</span>
      <span className="text-fg-subtle shrink-0 text-[12px] tabular-nums">{formatBytes(picked.file.size)}</span>
      {picked.format ? (
        <Badge tone="neutral">{picked.format.label}</Badge>
      ) : (
        <Badge tone="warning" title="This file type isn’t supported and will be skipped">
          Unsupported
        </Badge>
      )}
      <button onClick={onRemove} aria-label="Remove" className="text-fg-subtle hover:text-danger shrink-0 p-0.5">
        <X className="size-3.5" />
      </button>
    </div>
  );
}

// ---- target tile -----------------------------------------------------------

type TargetChoice = { output: ConvertOutput; available: boolean; reason?: string };

function TargetTile({ target, selected, onSelect }: { target: TargetChoice; selected: boolean; onSelect: () => void }) {
  const { output, available, reason } = target;
  return (
    <button
      disabled={!available}
      onClick={onSelect}
      title={!available ? reason : undefined}
      className={cn(
        "flex flex-col items-start gap-0.5 rounded-lg border px-3 py-2.5 text-left transition-colors",
        !available && "cursor-not-allowed opacity-50",
        available && !selected && "border-edge hover:border-accent hover:bg-accent-soft/30 cursor-pointer",
        selected && "border-accent bg-accent-soft/40 cursor-pointer",
      )}
    >
      <span className="text-fg text-[13.5px] font-semibold uppercase">{output.format}</span>
      <span className="text-fg-subtle truncate text-[11.5px]">
        {output.note ? output.note : output.label}
      </span>
    </button>
  );
}

// ---- option slider ---------------------------------------------------------

function OptionSlider({ option, value, onChange }: { option: ConvertOption; value: number; onChange: (v: number) => void }) {
  const [live, setLive] = useState(value);
  return (
    <div>
      <div className="mb-1.5 flex items-center justify-between">
        <label className="text-fg text-[13px] font-medium">{option.label}</label>
        <span className="text-fg-subtle text-[12px] tabular-nums">{live}</span>
      </div>
      <Slider
        value={value}
        min={option.min}
        max={option.max}
        aria-label={option.label}
        onValueChange={setLive}
        onValueCommit={onChange}
      />
    </div>
  );
}

// ---- active batch ----------------------------------------------------------

function ActiveBatch({ id, onDismiss }: { id: string; onDismiss: () => void }) {
  useLiveInvalidate("convert", [["convert", "batch", id], ["convert", "list"]]);

  const query = useQuery({
    queryKey: ["convert", "batch", id],
    queryFn: async () => unwrap(await api.GET("/convert/{id}", { params: { path: { id } } })),
    refetchInterval: (q) => {
      const status = q.state.data?.status;
      return status === "queued" || status === "running" ? 2000 : false;
    },
  });
  const batch = query.data;
  if (!batch) return null;

  const done = batch.items.filter((it) => it.status === "done");
  return (
    <Card className="p-5">
      <div className="mb-3 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <h2 className="text-fg text-[13px] font-semibold">This conversion</h2>
          <StatusBadge status={batch.status} />
        </div>
        <div className="flex items-center gap-2">
          {done.length >= 2 && (
            <a
              href={`/api/convert/${batch.id}/download`}
              className="bg-accent text-accent-fg hover:bg-accent-hover shadow-1 inline-flex h-8.5 items-center gap-2 rounded-md px-3.5 text-sm font-medium"
            >
              <Package className="size-4" /> Download all (.zip)
            </a>
          )}
          <button onClick={onDismiss} aria-label="Dismiss" className="text-fg-subtle hover:text-fg p-1">
            <X className="size-4" />
          </button>
        </div>
      </div>
      <div className="space-y-1.5">
        {batch.items.map((it) => (
          <ItemRow key={it.id} batchId={batch.id} item={it} />
        ))}
      </div>
    </Card>
  );
}

function ItemRow({ batchId, item }: { batchId: string; item: ConvertItem }) {
  return (
    <div className="border-edge flex items-center gap-3 rounded-lg border px-3 py-2">
      <ItemStatusIcon status={item.status} />
      <div className="min-w-0 flex-1">
        <div className="text-fg truncate text-[13px]">{item.outputName ?? item.sourceName}</div>
        {item.error && <div className="text-danger truncate text-[11.5px]">{item.error}</div>}
      </div>
      <span className="text-fg-subtle shrink-0 text-[12px] tabular-nums">
        {item.status === "done" && item.outputBytes ? formatBytes(item.outputBytes) : formatBytes(item.sourceBytes)}
      </span>
      {item.status === "done" ? (
        <a
          href={`/api/convert/${batchId}/items/${item.id}/download`}
          className="text-accent hover:text-accent-hover inline-flex shrink-0 items-center gap-1 text-[12.5px] font-medium"
        >
          <Download className="size-3.5" /> Download
        </a>
      ) : (
        <span className="text-fg-subtle w-16 shrink-0 text-right text-[11.5px]">
          {item.sourceFormat ? `${item.sourceFormat} →` : ""}
        </span>
      )}
    </div>
  );
}

// ---- recent conversions ----------------------------------------------------

function RecentConversions({ activeId, onOpen }: { activeId: string | null; onOpen: (id: string) => void }) {
  const queryClient = useQueryClient();
  const toast = useToast();
  const [confirmId, setConfirmId] = useState<string | null>(null);
  useLiveInvalidate("convert", [["convert", "list"]]);

  const query = useQuery({
    queryKey: ["convert", "list"],
    queryFn: async () => unwrap(await api.GET("/convert")),
    refetchInterval: (q) => (q.state.data?.some((b) => b.status === "queued" || b.status === "running") ? 3000 : false),
  });

  const del = useMutation({
    mutationFn: async (id: string) => unwrap(await api.DELETE("/convert/{id}", { params: { path: { id } } })),
    onSuccess: () => {
      setConfirmId(null);
      queryClient.invalidateQueries({ queryKey: ["convert", "list"] });
      toast("success", "Conversion deleted");
    },
    onError: (e: Error) => toast("error", "Delete failed", e.message),
  });

  const batches = (query.data ?? []).filter((b) => b.id !== activeId);
  if (batches.length === 0) return null;

  return (
    <section className="space-y-2.5">
      <h2 className="text-fg-subtle text-[11px] font-semibold uppercase tracking-wide">Recent conversions</h2>
      <Card className="divide-edge divide-y">
        {batches.map((b) => (
          <RecentRow key={b.id} batch={b} onOpen={() => onOpen(b.id)} onDelete={() => setConfirmId(b.id)} />
        ))}
      </Card>
      <ConfirmDialog
        open={confirmId !== null}
        onOpenChange={(open) => !open && setConfirmId(null)}
        title="Delete this conversion?"
        body="The converted files will be removed from the server. This cannot be undone."
        confirmLabel="Delete"
        loading={del.isPending}
        onConfirm={() => confirmId && del.mutate(confirmId)}
      />
    </section>
  );
}

function RecentRow({ batch, onOpen, onDelete }: { batch: ConvertBatch; onOpen: () => void; onDelete: () => void }) {
  const done = batch.items.filter((it) => it.status === "done").length;
  return (
    <div className="hover:bg-bg-subtle/50 flex items-center gap-3 px-4 py-2.5 transition-colors">
      <button onClick={onOpen} className="flex min-w-0 flex-1 items-center gap-3 text-left">
        <StatusBadge status={batch.status} />
        <span className="text-fg min-w-0 flex-1 truncate text-[13px]">
          {batch.items.length} file{batch.items.length === 1 ? "" : "s"} → <span className="uppercase">{batch.targetFormat}</span>
          {done > 0 && <span className="text-fg-subtle"> · {done} ready</span>}
        </span>
      </button>
      <span className="text-fg-subtle shrink-0 text-[11.5px]">{expiryLabel(batch.expiresAt)}</span>
      {batch.items.some((it) => it.status === "done") && (
        <a
          href={`/api/convert/${batch.id}/download`}
          className="text-fg-subtle hover:text-accent shrink-0 p-1"
          title="Download all (.zip)"
          aria-label="Download all"
        >
          <Download className="size-4" />
        </a>
      )}
      <button onClick={onDelete} aria-label="Delete" className="text-fg-subtle hover:text-danger shrink-0 p-1">
        <Trash2 className="size-4" />
      </button>
    </div>
  );
}

// ---- small pieces ----------------------------------------------------------

function StatusBadge({ status }: { status: ConvertBatch["status"] }) {
  const map: Record<ConvertBatch["status"], { tone: "neutral" | "info" | "success" | "warning" | "danger"; label: string }> = {
    queued: { tone: "neutral", label: "Queued" },
    running: { tone: "info", label: "Converting" },
    done: { tone: "success", label: "Done" },
    partial: { tone: "warning", label: "Partial" },
    failed: { tone: "danger", label: "Failed" },
  };
  const { tone, label } = map[status];
  return (
    <Badge tone={tone}>
      {(status === "running" || status === "queued") && <Loader2 className="size-3 animate-spin" />}
      {label}
    </Badge>
  );
}

function ItemStatusIcon({ status }: { status: ConvertItem["status"] }) {
  switch (status) {
    case "done":
      return <CheckCircle2 className="text-success size-4 shrink-0" />;
    case "failed":
      return <XCircle className="text-danger size-4 shrink-0" />;
    case "converting":
      return <Loader2 className="text-info size-4 shrink-0 animate-spin" />;
    default:
      return <Clock className="text-fg-subtle size-4 shrink-0" />;
  }
}

// ---- helpers ---------------------------------------------------------------

type CatalogIndex = {
  byId: Map<string, ConvertFormat>;
  guess: (name: string) => ConvertFormat | null;
};

function buildIndex(catalog: ConvertCatalog | undefined): CatalogIndex {
  const byId = new Map<string, ConvertFormat>();
  const byExt = new Map<string, ConvertFormat>();
  for (const f of catalog?.formats ?? []) {
    byId.set(f.id, f);
    for (const ext of f.extensions) if (!byExt.has(ext)) byExt.set(ext, f);
  }
  return {
    byId,
    guess: (name: string) => {
      const dot = name.lastIndexOf(".");
      if (dot < 0) return null;
      return byExt.get(name.slice(dot + 1).toLowerCase()) ?? null;
    },
  };
}

/** The intersection of allowed outputs across the staged (supported) files. A
 *  target is available only if every file's edge to it is available. */
function commonTargets(supported: Picked[], index: CatalogIndex): TargetChoice[] {
  if (supported.length === 0) return [];
  const sourceIds = Array.from(new Set(supported.map((p) => p.format!.id)));
  const outputMaps = sourceIds.map((id) => {
    const m = new Map<string, ConvertOutput>();
    for (const o of index.byId.get(id)?.outputs ?? []) m.set(o.format, o);
    return m;
  });

  const first = outputMaps[0];
  const result: TargetChoice[] = [];
  for (const [toId, firstOut] of first) {
    if (!outputMaps.every((m) => m.has(toId))) continue; // not shared by all
    let available = true;
    let reason: string | undefined;
    for (const m of outputMaps) {
      const o = m.get(toId)!;
      if (!o.available) {
        available = false;
        reason = o.unavailableReason ?? "This tool isn’t installed";
        break;
      }
    }
    result.push({ output: firstOut, available, reason });
  }
  result.sort((a, b) => {
    if (a.available !== b.available) return a.available ? -1 : 1;
    return a.output.format.localeCompare(b.output.format);
  });
  return result;
}

/** Raw XHR upload so we get real progress; the session cookie rides along
 *  (same-origin). openapi-fetch is awkward for multipart, so this is by hand. */
function uploadConversion(
  files: File[],
  target: string,
  options: Record<string, number>,
  onProgress: (pct: number) => void,
): Promise<ConvertBatch> {
  return new Promise((resolve, reject) => {
    const form = new FormData();
    form.append("target", target);
    form.append("options", JSON.stringify(options));
    for (const file of files) form.append("files", file, file.name);

    const xhr = new XMLHttpRequest();
    xhr.open("POST", "/api/convert");
    xhr.withCredentials = true;
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) onProgress(Math.round((e.loaded / e.total) * 100));
    };
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          resolve(JSON.parse(xhr.responseText) as ConvertBatch);
        } catch {
          reject(new Error("could not parse the server response"));
        }
        return;
      }
      let message = `upload failed (${xhr.status})`;
      try {
        const body = JSON.parse(xhr.responseText) as { message?: string };
        if (body.message) message = body.message;
      } catch {
        /* keep default */
      }
      reject(new Error(message));
    };
    xhr.onerror = () => reject(new Error("network error during upload"));
    xhr.send(form);
  });
}

function expiryLabel(iso: string): string {
  const ms = new Date(iso).getTime() - Date.now();
  if (ms <= 0) return "expired";
  const hours = Math.floor(ms / 3_600_000);
  if (hours >= 1) return `expires in ${hours}h`;
  const mins = Math.max(1, Math.floor(ms / 60_000));
  return `expires in ${mins}m`;
}
