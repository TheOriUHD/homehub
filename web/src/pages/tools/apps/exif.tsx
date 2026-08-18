import { useEffect, useMemo, useRef, useState } from "react";
import exifr from "exifr";
import {
  Aperture,
  Calendar,
  Camera,
  Download,
  Eraser,
  ExternalLink,
  FileImage,
  ImageUp,
  Info,
  Loader2,
  MapPin,
  Search,
  ShieldAlert,
  ShieldCheck,
  Table2,
  X,
} from "lucide-react";
import { formatBytes } from "@/lib/file-lang";
import { cn } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { useToast } from "@/ui/toast";
import { CopyIconButton, ToolCard } from "../kit";

/**
 * EXIF viewer & stripper. `exifr.parse(file, true)` reads every metadata
 * segment (TIFF/EXIF, GPS, XMP, IPTC, ICC…) into one flat object; the viewer
 * groups the interesting fields (camera, exposure, dates, GPS) and lists the
 * raw tags. Privacy-sensitive fields — GPS coordinates and serial numbers —
 * are flagged. The stripper redraws the pixels through a <canvas> and
 * re-encodes with `canvas.toBlob`, which by construction drops ALL metadata,
 * then offers the clean copy for download. Everything runs in the browser;
 * the image never leaves the device.
 */

type Tags = Record<string, unknown>;
type Row = { label: string; value: string; sensitive?: boolean };
type Fmt = "image/jpeg" | "image/png" | "image/webp";

const FORMATS: { id: Fmt; label: string; ext: string; lossy: boolean }[] = [
  { id: "image/jpeg", label: "JPEG", ext: "jpg", lossy: true },
  { id: "image/png", label: "PNG (lossless)", ext: "png", lossy: false },
  { id: "image/webp", label: "WebP", ext: "webp", lossy: true },
];

/** Keys whose presence leaks who/where — highlighted and summarized. */
const isSensitiveKey = (k: string) =>
  /^gps/i.test(k) || k === "latitude" || k === "longitude" || /serial/i.test(k);

// ---- value formatters -----------------------------------------------------

/** Best-effort human string for an arbitrary tag value (raw-table + copy). */
function fmtRaw(v: unknown): string {
  if (v == null) return "";
  if (v instanceof Date) return Number.isNaN(v.getTime()) ? String(v) : v.toLocaleString();
  if (v instanceof Uint8Array) return `[binary · ${v.length} bytes]`;
  if (Array.isArray(v)) return v.map(fmtRaw).join(", ");
  if (typeof v === "number") return Number.isInteger(v) ? String(v) : String(Math.round(v * 1e6) / 1e6);
  if (typeof v === "object") {
    try {
      return JSON.stringify(v);
    } catch {
      return String(v);
    }
  }
  return String(v);
}

const num = (v: unknown): number | undefined => (typeof v === "number" && Number.isFinite(v) ? v : undefined);

const shutter = (t: unknown) => {
  const s = num(t);
  if (s == null || s <= 0) return undefined;
  return s >= 1 || Math.round(1 / s) < 2 ? `${Math.round(s * 10) / 10} s` : `1/${Math.round(1 / s)} s`;
};
const aperture = (f: unknown) => {
  const n = num(f);
  return n == null ? undefined : `ƒ/${Math.round(n * 10) / 10}`;
};
const focal = (mm: unknown, eq: unknown) => {
  const n = num(mm);
  if (n == null) return undefined;
  const e = num(eq);
  const base = `${Math.round(n * 10) / 10} mm`;
  return e != null && Math.round(e) !== Math.round(n) ? `${base} (${Math.round(e)} mm eq.)` : base;
};
const isoStr = (v: unknown) => {
  const n = Array.isArray(v) ? num(v[0]) : num(v);
  return n == null ? undefined : `ISO ${n}`;
};
const evComp = (v: unknown) => {
  const n = num(v);
  if (n == null) return undefined;
  const r = Math.round(n * 100) / 100;
  return `${r > 0 ? "+" : ""}${r} EV`;
};

/** Serialize tags to JSON, coercing Date / binary into readable placeholders. */
function tagsToJson(tags: Tags): string {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(tags)) {
    if (v instanceof Uint8Array) out[k] = `[binary · ${v.length} bytes]`;
    else if (v instanceof Date) out[k] = Number.isNaN(v.getTime()) ? null : v.toISOString();
    else out[k] = v;
  }
  return JSON.stringify(out, null, 2);
}

const cleanName = (orig: string, ext: string) => `${orig.replace(/\.[^.]+$/, "") || "image"}-clean.${ext}`;

// ---- small presentational bits --------------------------------------------

/** A labelled metadata value with an optional privacy flag and copy button. */
function DetailRow({ label, value, sensitive }: { label: string; value: string; sensitive?: boolean }) {
  return (
    <div className="flex items-center gap-2 py-1">
      <span className="text-fg-subtle flex w-36 shrink-0 items-center gap-1.5 text-[12.5px]">
        {sensitive && <ShieldAlert className="size-3 shrink-0 text-amber-500" />}
        {label}
      </span>
      <span className="text-fg min-w-0 flex-1 truncate font-mono text-[13px]">{value || "—"}</span>
      {value.length > 0 && <CopyIconButton value={value} />}
    </div>
  );
}

/** A metadata group card — renders nothing when it has no populated rows. */
function GroupCard({ icon: Icon, title, rows }: { icon: typeof Camera; title: string; rows: Row[] }) {
  if (rows.length === 0) return null;
  return (
    <ToolCard
      title={
        <span className="flex items-center gap-2">
          <Icon className="text-fg-subtle size-4" />
          {title}
        </span>
      }
    >
      <div className="divide-edge/60 divide-y">
        {rows.map((r) => (
          <DetailRow key={r.label} label={r.label} value={r.value} sensitive={r.sensitive} />
        ))}
      </div>
    </ToolCard>
  );
}

// ---- component ------------------------------------------------------------

export default function Tool() {
  const toast = useToast();
  const inputRef = useRef<HTMLInputElement>(null);
  const stripRef = useRef<HTMLDivElement>(null);

  const [file, setFile] = useState<File | null>(null);
  const [imgUrl, setImgUrl] = useState<string | null>(null);
  const [img, setImg] = useState<HTMLImageElement | null>(null);
  const [dims, setDims] = useState<{ w: number; h: number } | null>(null);
  const [decodable, setDecodable] = useState<boolean | null>(null);
  const [tags, setTags] = useState<Tags | null>(null);
  const [parsing, setParsing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [dragOver, setDragOver] = useState(false);
  const [filter, setFilter] = useState("");

  // strip settings + result
  const [format, setFormat] = useState<Fmt>("image/jpeg");
  const [quality, setQuality] = useState(0.92);
  const [stripped, setStripped] = useState<{ url: string; size: number } | null>(null);
  const [stripError, setStripError] = useState<string | null>(null);

  // Revoke object URLs when they are replaced or the tool unmounts.
  useEffect(() => () => void (imgUrl && URL.revokeObjectURL(imgUrl)), [imgUrl]);
  useEffect(() => () => void (stripped && URL.revokeObjectURL(stripped.url)), [stripped]);

  const reset = () => {
    setFile(null);
    setImgUrl(null);
    setImg(null);
    setDims(null);
    setDecodable(null);
    setTags(null);
    setError(null);
    setStripped(null);
    setStripError(null);
    setFilter("");
  };

  async function load(f: File) {
    if (!f.type.startsWith("image/")) {
      toast("error", `“${f.name}” is not an image. Pick a JPEG, PNG, WebP, HEIC or TIFF file.`);
      return;
    }
    reset();
    setFile(f);
    setError(null);
    setParsing(true);

    // Default the output format to the source when it is canvas-encodable.
    setFormat(FORMATS.some((x) => x.id === f.type) ? (f.type as Fmt) : "image/jpeg");

    // Decode into an <img> for the preview + canvas source.
    const url = URL.createObjectURL(f);
    setImgUrl(url);
    const image = new Image();
    image.onload = () => {
      setImg(image);
      setDims({ w: image.naturalWidth, h: image.naturalHeight });
      setDecodable(true);
    };
    image.onerror = () => setDecodable(false);
    image.src = url;

    // Read every metadata segment. Missing/anonymous metadata is not an error.
    try {
      const parsed = (await exifr.parse(f, true)) as Tags | undefined;
      setTags(parsed && Object.keys(parsed).length ? parsed : null);
    } catch {
      setTags(null);
    } finally {
      setParsing(false);
    }
  }

  const onPick = (list: FileList | null) => {
    const f = list?.[0];
    if (f) void load(f);
  };

  // Re-encode through a canvas whenever the source or export settings change.
  // Drawing to a canvas and reading it back drops every metadata segment.
  useEffect(() => {
    if (!img || !dims) {
      setStripped(null);
      return;
    }
    let alive = true;
    setStripError(null);
    try {
      const canvas = document.createElement("canvas");
      canvas.width = dims.w;
      canvas.height = dims.h;
      const ctx = canvas.getContext("2d");
      if (!ctx) throw new Error("no 2d context");
      // JPEG has no alpha — flatten transparency onto white instead of black.
      if (format === "image/jpeg") {
        ctx.fillStyle = "#ffffff";
        ctx.fillRect(0, 0, dims.w, dims.h);
      }
      ctx.drawImage(img, 0, 0);
      const q = format === "image/png" ? undefined : quality;
      canvas.toBlob(
        (blob) => {
          if (!alive) return;
          if (!blob) {
            setStripError("The browser could not re-encode this image.");
            setStripped(null);
            return;
          }
          setStripped({ url: URL.createObjectURL(blob), size: blob.size });
        },
        format,
        q,
      );
    } catch {
      setStripError("This image is too large to re-encode in the browser.");
      setStripped(null);
    }
    return () => {
      alive = false;
    };
  }, [img, dims, format, quality]);

  // ---- derived metadata groups --------------------------------------------

  const groups = useMemo(() => {
    if (!tags) return null;
    const g = (arr: Row[], label: string, value: string | undefined, sensitive?: boolean) => {
      if (value) arr.push({ label, value, sensitive });
    };

    const camera: Row[] = [];
    g(camera, "Make", fmtRaw(tags.Make));
    g(camera, "Model", fmtRaw(tags.Model));
    g(camera, "Lens", fmtRaw(tags.LensModel ?? tags.Lens ?? tags.LensID));
    g(camera, "Lens maker", fmtRaw(tags.LensMake));
    g(camera, "Software", fmtRaw(tags.Software));
    g(
      camera,
      "Camera serial",
      fmtRaw(tags.SerialNumber ?? tags.BodySerialNumber ?? tags.InternalSerialNumber ?? tags.CameraSerialNumber),
      true,
    );
    g(camera, "Lens serial", fmtRaw(tags.LensSerialNumber), true);
    g(camera, "Owner", fmtRaw(tags.OwnerName ?? tags.CameraOwnerName), true);
    g(camera, "Artist", fmtRaw(tags.Artist));
    g(camera, "Copyright", fmtRaw(tags.Copyright));

    const exposure: Row[] = [];
    g(exposure, "ISO", isoStr(tags.ISO ?? tags.ISOSpeedRatings ?? tags.PhotographicSensitivity));
    g(exposure, "Aperture", aperture(tags.FNumber));
    g(exposure, "Shutter", shutter(tags.ExposureTime));
    g(exposure, "Focal length", focal(tags.FocalLength, tags.FocalLengthIn35mmFormat));
    g(exposure, "Exposure comp.", evComp(tags.ExposureCompensation ?? tags.ExposureBiasValue));
    g(exposure, "Program", fmtRaw(tags.ExposureProgram));
    g(exposure, "Metering", fmtRaw(tags.MeteringMode));
    g(exposure, "Flash", fmtRaw(tags.Flash));
    g(exposure, "White balance", fmtRaw(tags.WhiteBalance));

    const dates: Row[] = [];
    g(dates, "Taken", fmtRaw(tags.DateTimeOriginal));
    g(dates, "Digitized", fmtRaw(tags.CreateDate ?? tags.DateTimeDigitized));
    g(dates, "Modified", fmtRaw(tags.ModifyDate ?? tags.DateTime));

    const lat = num(tags.latitude);
    const lng = num(tags.longitude);
    const gps: Row[] = [];
    if (lat != null && lng != null) {
      g(gps, "Latitude", `${lat.toFixed(6)}°`, true);
      g(gps, "Longitude", `${lng.toFixed(6)}°`, true);
    }
    g(gps, "Altitude", num(tags.GPSAltitude) != null ? `${Math.round(num(tags.GPSAltitude)!)} m` : undefined, true);
    g(gps, "Heading", num(tags.GPSImgDirection) != null ? `${Math.round(num(tags.GPSImgDirection)!)}°` : undefined);

    return { camera, exposure, dates, gps, lat, lng };
  }, [tags]);

  const sensitiveHits = useMemo(() => {
    if (!tags) return [] as string[];
    const hits: string[] = [];
    if (num(tags.latitude) != null && num(tags.longitude) != null) hits.push("GPS location");
    if (tags.SerialNumber ?? tags.BodySerialNumber ?? tags.InternalSerialNumber ?? tags.CameraSerialNumber)
      hits.push("Camera serial number");
    if (tags.LensSerialNumber) hits.push("Lens serial number");
    if (tags.OwnerName ?? tags.CameraOwnerName) hits.push("Owner name");
    return hits;
  }, [tags]);

  const rawRows = useMemo(() => {
    if (!tags) return [] as Row[];
    const q = filter.trim().toLowerCase();
    return Object.entries(tags)
      .map(([k, v]) => ({ label: k, value: fmtRaw(v), sensitive: isSensitiveKey(k) }))
      .filter((r) => r.value !== "")
      .filter((r) => !q || r.label.toLowerCase().includes(q) || r.value.toLowerCase().includes(q))
      .sort((a, b) => a.label.localeCompare(b.label));
  }, [tags, filter]);

  const rawJson = useMemo(() => (tags ? tagsToJson(tags) : ""), [tags]);

  const activeFmt = FORMATS.find((f) => f.id === format)!;
  const delta = file && stripped ? stripped.size - file.size : 0;
  const deltaPct = file && stripped && file.size > 0 ? Math.round((delta / file.size) * 100) : 0;

  const download = () => {
    if (!stripped || !file) return;
    const a = document.createElement("a");
    a.href = stripped.url;
    a.download = cleanName(file.name, activeFmt.ext);
    document.body.appendChild(a);
    a.click();
    a.remove();
    toast("success", "Saved metadata-free copy");
  };

  // ---- render --------------------------------------------------------------

  return (
    <div className="space-y-4">
      <input
        ref={inputRef}
        type="file"
        accept="image/*"
        className="hidden"
        onChange={(e) => {
          onPick(e.target.files);
          e.target.value = "";
        }}
      />

      {/* Drop zone / loaded-file header */}
      {!file ? (
        <button
          type="button"
          onClick={() => inputRef.current?.click()}
          onDragOver={(e) => {
            e.preventDefault();
            setDragOver(true);
          }}
          onDragLeave={() => setDragOver(false)}
          onDrop={(e) => {
            e.preventDefault();
            setDragOver(false);
            onPick(e.dataTransfer.files);
          }}
          className={cn(
            "border-edge bg-surface hover:border-accent/60 hover:bg-accent-soft/10 flex w-full flex-col items-center justify-center gap-3 rounded-2xl border border-dashed px-6 py-14 text-center transition-colors",
            dragOver && "border-accent bg-accent-soft/20",
          )}
        >
          <span className="bg-accent-soft text-accent-soft-fg flex size-14 items-center justify-center rounded-2xl">
            <ImageUp className="size-7" />
          </span>
          <span className="text-fg text-[15px] font-semibold">Drop an image here, or click to browse</span>
          <span className="text-fg-subtle max-w-md text-[13px]">
            JPEG, PNG, WebP, HEIC and TIFF are supported. Everything happens in your browser — your photo never
            leaves this device.
          </span>
          {error && (
            <span className="bg-danger-soft text-danger-soft-fg mt-1 rounded-lg px-3 py-1.5 text-[13px]">{error}</span>
          )}
        </button>
      ) : (
        <ToolCard>
          <div className="flex flex-wrap items-center gap-3">
            <span className="bg-accent-soft text-accent-soft-fg flex size-11 shrink-0 items-center justify-center rounded-xl">
              <FileImage className="size-5" />
            </span>
            <div className="min-w-0 flex-1">
              <p className="text-fg truncate text-[14px] font-semibold">{file.name}</p>
              <p className="text-fg-subtle flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[12px]">
                <span>{file.type || "image"}</span>
                <span aria-hidden>·</span>
                <span>{formatBytes(file.size)}</span>
                {dims && (
                  <>
                    <span aria-hidden>·</span>
                    <span>
                      {dims.w} × {dims.h}
                    </span>
                  </>
                )}
                {parsing && (
                  <>
                    <span aria-hidden>·</span>
                    <span className="inline-flex items-center gap-1">
                      <Loader2 className="size-3 animate-spin" /> reading…
                    </span>
                  </>
                )}
              </p>
            </div>
            <Button variant="ghost" size="sm" onClick={() => inputRef.current?.click()}>
              <ImageUp className="size-3.5" />
              Replace
            </Button>
            <Button variant="ghost" size="sm" onClick={reset}>
              <X className="size-3.5" />
              Clear
            </Button>
          </div>
        </ToolCard>
      )}

      {file && (
        <>
          {/* Privacy summary */}
          {!parsing && tags && sensitiveHits.length > 0 && (
            <div className="flex flex-wrap items-center gap-3 rounded-xl border border-amber-500/30 bg-amber-500/10 px-4 py-3">
              <ShieldAlert className="size-5 shrink-0 text-amber-500" />
              <div className="min-w-0 flex-1">
                <p className="text-fg text-[13.5px] font-semibold">This photo carries privacy-sensitive metadata</p>
                <div className="mt-1.5 flex flex-wrap gap-1.5">
                  {sensitiveHits.map((h) => (
                    <Badge key={h} tone="warning">
                      {h}
                    </Badge>
                  ))}
                </div>
              </div>
              <Button
                variant="primary"
                size="sm"
                onClick={() => stripRef.current?.scrollIntoView({ behavior: "smooth", block: "center" })}
              >
                <Eraser className="size-3.5" />
                Strip it
              </Button>
            </div>
          )}
          {!parsing && tags && sensitiveHits.length === 0 && (
            <div className="text-fg-muted flex items-center gap-2 rounded-xl border border-emerald-500/25 bg-emerald-500/5 px-4 py-2.5 text-[13px]">
              <ShieldCheck className="size-4 shrink-0 text-emerald-500" />
              No location or serial-number data detected in this image.
            </div>
          )}

          <div className="grid gap-4 lg:grid-cols-2">
            {/* Left: grouped metadata */}
            <div className="space-y-4">
              {!parsing && !tags && (
                <ToolCard>
                  <div className="text-fg-muted flex items-start gap-2.5 text-[13px]">
                    <Info className="mt-0.5 size-4 shrink-0" />
                    <span>
                      No embedded metadata found. This image has already been stripped, or it never carried EXIF (many
                      screenshots and messaging-app exports don’t). You can still re-encode it below for a
                      guaranteed-clean copy.
                    </span>
                  </div>
                </ToolCard>
              )}
              {groups && (
                <>
                  <GroupCard icon={Camera} title="Camera & lens" rows={groups.camera} />
                  <GroupCard icon={Aperture} title="Exposure" rows={groups.exposure} />
                  <GroupCard icon={Calendar} title="Dates" rows={groups.dates} />
                  {groups.gps.length > 0 && (
                    <ToolCard
                      title={
                        <span className="flex items-center gap-2">
                          <MapPin className="text-fg-subtle size-4" />
                          Location
                          <Badge tone="warning">Sensitive</Badge>
                        </span>
                      }
                    >
                      <div className="divide-edge/60 divide-y">
                        {groups.gps.map((r) => (
                          <DetailRow key={r.label} label={r.label} value={r.value} sensitive={r.sensitive} />
                        ))}
                      </div>
                      {groups.lat != null && groups.lng != null && (
                        <a
                          href={`https://www.google.com/maps?q=${groups.lat},${groups.lng}`}
                          target="_blank"
                          rel="noreferrer"
                          className="border-edge bg-surface text-fg hover:bg-surface-hover shadow-1 mt-3 inline-flex h-7 items-center gap-1.5 rounded-md border px-2.5 text-[13px] font-medium"
                        >
                          <MapPin className="size-3.5" />
                          Open in Google Maps
                          <ExternalLink className="text-fg-subtle size-3" />
                        </a>
                      )}
                    </ToolCard>
                  )}
                </>
              )}
            </div>

            {/* Right: preview + stripper */}
            <div className="space-y-4">
              <ToolCard title="Preview">
                <div className="bg-bg-subtle border-edge flex items-center justify-center overflow-hidden rounded-lg border p-2">
                  {imgUrl && decodable !== false ? (
                    <img
                      src={imgUrl}
                      alt={file.name}
                      className="max-h-72 w-auto max-w-full rounded object-contain"
                    />
                  ) : (
                    <div className="text-fg-subtle flex flex-col items-center gap-2 py-10 text-center text-[13px]">
                      <FileImage className="size-8" />
                      This browser can’t render this format (e.g. HEIC) to preview or strip. Metadata above still
                      applies.
                    </div>
                  )}
                </div>
              </ToolCard>

              <div ref={stripRef}>
                <ToolCard
                  title={
                    <span className="flex items-center gap-2">
                      <Eraser className="text-fg-subtle size-4" />
                      Strip metadata
                    </span>
                  }
                >
                  <div className="space-y-3.5">
                    <p className="text-fg-subtle text-[12.5px]">
                      Re-encoding through a canvas rebuilds the file from pixels alone — EXIF, GPS, serial numbers
                      and embedded thumbnails are all dropped. Works for JPEG, PNG and WebP.
                    </p>

                    <div className="grid gap-3 sm:grid-cols-2">
                      <Field label="Output format">
                        {(p) => (
                          <Select value={format} onValueChange={(v) => setFormat(v as Fmt)}>
                            <SelectTrigger {...p}>
                              <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                              {FORMATS.map((f) => (
                                <SelectItem key={f.id} value={f.id}>
                                  {f.label}
                                </SelectItem>
                              ))}
                            </SelectContent>
                          </Select>
                        )}
                      </Field>
                      {activeFmt.lossy && (
                        <div className="space-y-1.5">
                          <div className="flex items-baseline justify-between">
                            <label htmlFor="exif-quality" className="text-fg text-[13px] font-medium">
                              Quality
                            </label>
                            <span className="text-fg-subtle font-mono text-[12px]">{Math.round(quality * 100)}%</span>
                          </div>
                          <input
                            id="exif-quality"
                            type="range"
                            min={0.4}
                            max={1}
                            step={0.01}
                            value={quality}
                            onChange={(e) => setQuality(Number(e.target.value))}
                            className="text-accent accent-current h-8.5 w-full cursor-pointer"
                          />
                        </div>
                      )}
                    </div>

                    {stripError ? (
                      <div className="bg-danger-soft text-danger-soft-fg rounded-lg px-3 py-2 text-[13px]">
                        {stripError}
                      </div>
                    ) : decodable === false ? (
                      <div className="bg-bg-subtle text-fg-muted rounded-lg px-3 py-2 text-[13px]">
                        Stripping needs a format the browser can render to a canvas.
                      </div>
                    ) : (
                      <>
                        <div className="border-edge bg-bg-subtle grid grid-cols-3 gap-2 rounded-lg border p-3 text-center">
                          <div>
                            <p className="text-fg-subtle text-[11px] tracking-wide uppercase">Original</p>
                            <p className="text-fg mt-0.5 font-mono text-[13px]">
                              {file ? formatBytes(file.size) : "—"}
                            </p>
                          </div>
                          <div>
                            <p className="text-fg-subtle text-[11px] tracking-wide uppercase">Cleaned</p>
                            <p className="text-fg mt-0.5 font-mono text-[13px]">
                              {stripped ? formatBytes(stripped.size) : "…"}
                            </p>
                          </div>
                          <div>
                            <p className="text-fg-subtle text-[11px] tracking-wide uppercase">Change</p>
                            <p
                              className={cn(
                                "mt-0.5 font-mono text-[13px]",
                                delta < 0 ? "text-emerald-500" : delta > 0 ? "text-amber-500" : "text-fg",
                              )}
                            >
                              {stripped ? `${deltaPct > 0 ? "+" : ""}${deltaPct}%` : "…"}
                            </p>
                          </div>
                        </div>

                        <Button
                          variant="primary"
                          size="sm"
                          className="w-full"
                          disabled={!stripped}
                          onClick={download}
                        >
                          <Download className="size-3.5" />
                          Download clean {activeFmt.ext.toUpperCase()}
                        </Button>
                      </>
                    )}
                  </div>
                </ToolCard>
              </div>
            </div>
          </div>

          {/* Raw tag table */}
          {tags && (
            <ToolCard
              title={
                <span className="flex items-center gap-2">
                  <Table2 className="text-fg-subtle size-4" />
                  All tags
                  <Badge tone="neutral">{Object.keys(tags).length}</Badge>
                </span>
              }
              right={<CopyIconButton value={rawJson} label="Copy all tags as JSON" />}
            >
              <div className="space-y-3">
                <div className="relative">
                  <Search className="text-fg-subtle pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2" />
                  <Input
                    value={filter}
                    onChange={(e) => setFilter(e.target.value)}
                    placeholder="Filter tags…"
                    className="h-8 pl-8"
                  />
                </div>
                <div className="border-edge max-h-96 overflow-auto rounded-lg border">
                  <table className="w-full text-[12.5px]">
                    <tbody className="divide-edge/60 divide-y">
                      {rawRows.map((r) => (
                        <tr key={r.label} className="hover:bg-bg-subtle/50 group">
                          <td className="text-fg-muted w-1/3 min-w-[9rem] px-3 py-1.5 align-top font-mono">
                            <span className="flex items-center gap-1.5">
                              {r.sensitive && <ShieldAlert className="size-3 shrink-0 text-amber-500" />}
                              {r.label}
                            </span>
                          </td>
                          <td className="text-fg px-3 py-1.5 align-top font-mono break-words">
                            <span className="flex items-start gap-1">
                              <span className="min-w-0 flex-1">{r.value}</span>
                              <span className="opacity-0 transition-opacity group-hover:opacity-100">
                                <CopyIconButton value={`${r.label}: ${r.value}`} />
                              </span>
                            </span>
                          </td>
                        </tr>
                      ))}
                      {rawRows.length === 0 && (
                        <tr>
                          <td colSpan={2} className="text-fg-subtle px-3 py-6 text-center text-[13px]">
                            No tags match “{filter}”.
                          </td>
                        </tr>
                      )}
                    </tbody>
                  </table>
                </div>
              </div>
            </ToolCard>
          )}
        </>
      )}
    </div>
  );
}
