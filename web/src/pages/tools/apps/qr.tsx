import { useEffect, useMemo, useState } from "react";
import QRCode from "qrcode";
import { CalendarClock, Contact, FileCode2, Image as ImageIcon, Link2, QrCode, Wifi } from "lucide-react";
import { cn } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { Switch } from "@/ui/switch";
import { useToast } from "@/ui/toast";
import { CopyIconButton, ToolCard } from "../kit";

/**
 * QR code generator. Four payload builders (plain text/URL, Wi-Fi join,
 * vCard 3.0 contact, iCalendar VEVENT) feed a live SVG preview and SVG/PNG
 * exports via the `qrcode` library. Everything is client-side and updates
 * as you type; oversized payloads surface a friendly inline error instead
 * of crashing.
 */

type Kind = "text" | "wifi" | "vcard" | "event";
type Ecl = "L" | "M" | "Q" | "H";
type WifiAuth = "WPA" | "WEP" | "nopass";

const TYPES: { id: Kind; label: string; icon: typeof QrCode }[] = [
  { id: "text", label: "Text / URL", icon: Link2 },
  { id: "wifi", label: "Wi-Fi", icon: Wifi },
  { id: "vcard", label: "vCard", icon: Contact },
  { id: "event", label: "Calendar", icon: CalendarClock },
];

const BASE_NAME: Record<Kind, string> = {
  text: "qrcode",
  wifi: "wifi-qr",
  vcard: "contact-qr",
  event: "event-qr",
};

// Wi-Fi / MECARD escaping: backslash-escape the reserved delimiters.
const wifiEsc = (s: string) => s.replace(/([\\;,:"])/g, "\\$1");

// vCard 3.0 / iCalendar text escaping: backslash first, then delimiters and newlines.
const icsEsc = (s: string) =>
  s
    .replace(/\\/g, "\\\\")
    .replace(/;/g, "\\;")
    .replace(/,/g, "\\,")
    .replace(/\r?\n/g, "\\n");

const pad = (n: number) => String(n).padStart(2, "0");

/** Format a Date as an iCalendar UTC stamp: yyyymmddThhmmssZ. */
const fmtICS = (d: Date) =>
  `${d.getUTCFullYear()}${pad(d.getUTCMonth() + 1)}${pad(d.getUTCDate())}T${pad(d.getUTCHours())}${pad(
    d.getUTCMinutes(),
  )}${pad(d.getUTCSeconds())}Z`;

/** Parse a <input type="datetime-local"> value (local time) into a UTC iCal stamp. */
const localToICS = (local: string) => {
  if (!local) return "";
  const d = new Date(local);
  return Number.isNaN(d.getTime()) ? "" : fmtICS(d);
};

/** Random hex (crypto.randomUUID is unavailable on insecure LAN origins). */
function randHex(bytes = 8) {
  const a = new Uint8Array(bytes);
  crypto.getRandomValues(a);
  return Array.from(a, (b) => b.toString(16).padStart(2, "0")).join("");
}

function friendlyError(e: unknown): string {
  const msg = e instanceof Error ? e.message : String(e);
  if (/too big/i.test(msg))
    return "Too much data for one QR code at this level. Shorten the content or drop the error-correction level to L.";
  if (/no input/i.test(msg)) return "Nothing to encode yet.";
  return msg || "Could not generate a QR code from this input.";
}

const textareaCls =
  "border-edge bg-bg-subtle w-full rounded-lg border p-2.5 font-mono text-[13px] text-fg " +
  "placeholder:text-fg-subtle focus:border-accent focus:outline-none";

export default function Tool() {
  const toast = useToast();

  const [kind, setKind] = useState<Kind>("text");
  const [ecl, setEcl] = useState<Ecl>("M");

  // Text / URL
  const [text, setText] = useState("");

  // Wi-Fi
  const [wSsid, setWSsid] = useState("");
  const [wPass, setWPass] = useState("");
  const [wAuth, setWAuth] = useState<WifiAuth>("WPA");
  const [wHidden, setWHidden] = useState(false);

  // vCard
  const [vFirst, setVFirst] = useState("");
  const [vLast, setVLast] = useState("");
  const [vOrg, setVOrg] = useState("");
  const [vTitle, setVTitle] = useState("");
  const [vPhone, setVPhone] = useState("");
  const [vEmail, setVEmail] = useState("");
  const [vUrl, setVUrl] = useState("");

  // Calendar event
  const [evSummary, setEvSummary] = useState("");
  const [evLocation, setEvLocation] = useState("");
  const [evDesc, setEvDesc] = useState("");
  const [evStart, setEvStart] = useState("");
  const [evEnd, setEvEnd] = useState("");

  // Stable identifiers for the VEVENT, generated once per mount.
  const uid = useMemo(() => `${randHex(8)}@homehub`, []);
  const stamp = useMemo(() => fmtICS(new Date()), []);

  const payload = useMemo(() => {
    if (kind === "text") return text;

    if (kind === "wifi") {
      if (!wSsid.trim()) return "";
      const p = wAuth === "nopass" ? "" : wifiEsc(wPass);
      return `WIFI:T:${wAuth};S:${wifiEsc(wSsid)};P:${p};${wHidden ? "H:true;" : ""};`;
    }

    if (kind === "vcard") {
      const fn = `${vFirst} ${vLast}`.trim().replace(/\s+/g, " ");
      const any = fn || vOrg.trim() || vTitle.trim() || vPhone.trim() || vEmail.trim() || vUrl.trim();
      if (!any) return "";
      const lines = [
        "BEGIN:VCARD",
        "VERSION:3.0",
        `N:${icsEsc(vLast.trim())};${icsEsc(vFirst.trim())};;;`,
        `FN:${icsEsc(fn || vOrg.trim())}`,
      ];
      if (vOrg.trim()) lines.push(`ORG:${icsEsc(vOrg.trim())}`);
      if (vTitle.trim()) lines.push(`TITLE:${icsEsc(vTitle.trim())}`);
      if (vPhone.trim()) lines.push(`TEL;TYPE=CELL:${icsEsc(vPhone.trim())}`);
      if (vEmail.trim()) lines.push(`EMAIL;TYPE=INTERNET:${icsEsc(vEmail.trim())}`);
      if (vUrl.trim()) lines.push(`URL:${icsEsc(vUrl.trim())}`);
      lines.push("END:VCARD");
      return lines.join("\r\n");
    }

    // Calendar event
    const start = localToICS(evStart);
    let end = localToICS(evEnd);
    // Default to a one-hour block when only the start is given.
    if (start && !evEnd) {
      const d = new Date(evStart);
      if (!Number.isNaN(d.getTime())) end = fmtICS(new Date(d.getTime() + 3_600_000));
    }
    if (!start) return "";
    const lines = [
      "BEGIN:VCALENDAR",
      "VERSION:2.0",
      "PRODID:-//HomeHub//Utilities//EN",
      "BEGIN:VEVENT",
      `UID:${uid}`,
      `DTSTAMP:${stamp}`,
    ];
    if (evSummary.trim()) lines.push(`SUMMARY:${icsEsc(evSummary.trim())}`);
    if (start) lines.push(`DTSTART:${start}`);
    if (end && (!start || end > start)) lines.push(`DTEND:${end}`);
    if (evLocation.trim()) lines.push(`LOCATION:${icsEsc(evLocation.trim())}`);
    if (evDesc.trim()) lines.push(`DESCRIPTION:${icsEsc(evDesc.trim())}`);
    lines.push("END:VEVENT", "END:VCALENDAR");
    return lines.join("\r\n");
  }, [
    kind, text, wSsid, wPass, wAuth, wHidden,
    vFirst, vLast, vOrg, vTitle, vPhone, vEmail, vUrl,
    evSummary, evLocation, evDesc, evStart, evEnd, uid, stamp,
  ]);

  const [svg, setSvg] = useState("");
  const [pngUrl, setPngUrl] = useState("");
  const [meta, setMeta] = useState<{ version: number; size: number } | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!payload) {
      setSvg("");
      setPngUrl("");
      setMeta(null);
      setError(null);
      return;
    }
    let alive = true;
    (async () => {
      try {
        const info = QRCode.create(payload, { errorCorrectionLevel: ecl });
        const [s, u] = await Promise.all([
          QRCode.toString(payload, { type: "svg", errorCorrectionLevel: ecl, margin: 2 }),
          QRCode.toDataURL(payload, { errorCorrectionLevel: ecl, margin: 2, width: 1024 }),
        ]);
        if (!alive) return;
        setSvg(s);
        setPngUrl(u);
        setMeta({ version: info.version, size: info.modules.size });
        setError(null);
      } catch (e) {
        if (!alive) return;
        setSvg("");
        setPngUrl("");
        setMeta(null);
        setError(friendlyError(e));
      }
    })();
    return () => {
      alive = false;
    };
  }, [payload, ecl]);

  const triggerDownload = (href: string, name: string) => {
    const a = document.createElement("a");
    a.href = href;
    a.download = name;
    document.body.appendChild(a);
    a.click();
    a.remove();
    toast("success", `Saved ${name}`);
  };

  const downloadSvg = () => {
    if (!svg) return;
    const url = URL.createObjectURL(new Blob([svg], { type: "image/svg+xml;charset=utf-8" }));
    triggerDownload(url, `${BASE_NAME[kind]}.svg`);
    URL.revokeObjectURL(url);
  };

  const downloadPng = () => {
    if (!pngUrl) return;
    triggerDownload(pngUrl, `${BASE_NAME[kind]}.png`);
  };

  return (
    <div className="grid gap-4 lg:grid-cols-2">
      {/* ---- Inputs ---- */}
      <div className="space-y-4">
        <ToolCard title="Content">
          <div className="space-y-4">
            <div className="border-edge bg-bg-subtle grid grid-cols-2 gap-1 rounded-lg border p-1 sm:grid-cols-4">
              {TYPES.map((tp) => {
                const Icon = tp.icon;
                const active = kind === tp.id;
                return (
                  <button
                    key={tp.id}
                    type="button"
                    onClick={() => setKind(tp.id)}
                    aria-pressed={active}
                    className={cn(
                      "flex cursor-pointer items-center justify-center gap-1.5 rounded-md px-2 py-1.5 text-[12.5px] font-medium transition-colors",
                      active ? "bg-surface text-fg shadow-1" : "text-fg-muted hover:text-fg",
                    )}
                  >
                    <Icon className="size-3.5 shrink-0" />
                    {tp.label}
                  </button>
                );
              })}
            </div>

            {kind === "text" && (
              <Field label="Text or URL" hint="A link, a note, a phone number — anything.">
                {(p) => (
                  <textarea
                    {...p}
                    value={text}
                    onChange={(e) => setText(e.target.value)}
                    rows={5}
                    spellCheck={false}
                    placeholder="https://example.com"
                    className={textareaCls}
                  />
                )}
              </Field>
            )}

            {kind === "wifi" && (
              <div className="space-y-3">
                <div className="grid gap-3 sm:grid-cols-2">
                  <Field label="Network name (SSID)">
                    {(p) => (
                      <Input {...p} value={wSsid} onChange={(e) => setWSsid(e.target.value)} placeholder="MyNetwork" />
                    )}
                  </Field>
                  <Field label="Security">
                    {(p) => (
                      <Select value={wAuth} onValueChange={(v) => setWAuth(v as WifiAuth)}>
                        <SelectTrigger {...p}>
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="WPA">WPA / WPA2 / WPA3</SelectItem>
                          <SelectItem value="WEP">WEP</SelectItem>
                          <SelectItem value="nopass">None (open)</SelectItem>
                        </SelectContent>
                      </Select>
                    )}
                  </Field>
                </div>
                {wAuth !== "nopass" && (
                  <Field label="Password">
                    {(p) => (
                      <Input {...p} value={wPass} onChange={(e) => setWPass(e.target.value)} placeholder="••••••••" />
                    )}
                  </Field>
                )}
                <div className="border-edge bg-bg-subtle flex items-center justify-between rounded-lg border px-3 py-2">
                  <div className="min-w-0">
                    <p className="text-fg text-[13px] font-medium">Hidden network</p>
                    <p className="text-fg-subtle text-[12px]">Turn on if the SSID is not broadcast.</p>
                  </div>
                  <Switch checked={wHidden} onCheckedChange={setWHidden} />
                </div>
              </div>
            )}

            {kind === "vcard" && (
              <div className="grid gap-3 sm:grid-cols-2">
                <Field label="First name">
                  {(p) => <Input {...p} value={vFirst} onChange={(e) => setVFirst(e.target.value)} placeholder="Ada" />}
                </Field>
                <Field label="Last name">
                  {(p) => (
                    <Input {...p} value={vLast} onChange={(e) => setVLast(e.target.value)} placeholder="Lovelace" />
                  )}
                </Field>
                <Field label="Organization">
                  {(p) => (
                    <Input {...p} value={vOrg} onChange={(e) => setVOrg(e.target.value)} placeholder="Analytical Engines" />
                  )}
                </Field>
                <Field label="Job title">
                  {(p) => (
                    <Input {...p} value={vTitle} onChange={(e) => setVTitle(e.target.value)} placeholder="Mathematician" />
                  )}
                </Field>
                <Field label="Phone">
                  {(p) => (
                    <Input
                      {...p}
                      type="tel"
                      value={vPhone}
                      onChange={(e) => setVPhone(e.target.value)}
                      placeholder="+1 555 010 1842"
                    />
                  )}
                </Field>
                <Field label="Email">
                  {(p) => (
                    <Input
                      {...p}
                      type="email"
                      value={vEmail}
                      onChange={(e) => setVEmail(e.target.value)}
                      placeholder="ada@example.com"
                    />
                  )}
                </Field>
                <Field label="Website" className="sm:col-span-2">
                  {(p) => (
                    <Input {...p} value={vUrl} onChange={(e) => setVUrl(e.target.value)} placeholder="https://example.com" />
                  )}
                </Field>
              </div>
            )}

            {kind === "event" && (
              <div className="space-y-3">
                <Field label="Title">
                  {(p) => (
                    <Input
                      {...p}
                      value={evSummary}
                      onChange={(e) => setEvSummary(e.target.value)}
                      placeholder="Team standup"
                    />
                  )}
                </Field>
                <div className="grid gap-3 sm:grid-cols-2">
                  <Field label="Starts">
                    {(p) => (
                      <Input {...p} type="datetime-local" value={evStart} onChange={(e) => setEvStart(e.target.value)} />
                    )}
                  </Field>
                  <Field label="Ends" hint="Defaults to one hour after the start.">
                    {(p) => (
                      <Input {...p} type="datetime-local" value={evEnd} onChange={(e) => setEvEnd(e.target.value)} />
                    )}
                  </Field>
                </div>
                <Field label="Location">
                  {(p) => (
                    <Input
                      {...p}
                      value={evLocation}
                      onChange={(e) => setEvLocation(e.target.value)}
                      placeholder="Conference room B"
                    />
                  )}
                </Field>
                <Field label="Description">
                  {(p) => (
                    <textarea
                      {...p}
                      value={evDesc}
                      onChange={(e) => setEvDesc(e.target.value)}
                      rows={3}
                      placeholder="Notes, agenda, dial-in…"
                      className={textareaCls}
                    />
                  )}
                </Field>
                <p className="text-fg-subtle text-[12px]">
                  Times are encoded in UTC. The event opens in the scanner&rsquo;s calendar app.
                </p>
              </div>
            )}
          </div>
        </ToolCard>

        <ToolCard title="Options">
          <Field
            label="Error correction"
            hint="Higher levels stay scannable when the code is smudged or partly covered, but hold less data."
          >
            {(p) => (
              <Select value={ecl} onValueChange={(v) => setEcl(v as Ecl)}>
                <SelectTrigger {...p}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="L">Low — 7% recovery</SelectItem>
                  <SelectItem value="M">Medium — 15% recovery</SelectItem>
                  <SelectItem value="Q">Quartile — 25% recovery</SelectItem>
                  <SelectItem value="H">High — 30% recovery</SelectItem>
                </SelectContent>
              </Select>
            )}
          </Field>
        </ToolCard>
      </div>

      {/* ---- Output ---- */}
      <div className="space-y-4">
        <ToolCard
          title="Preview"
          right={
            meta ? (
              <Badge tone="neutral">
                v{meta.version} · {meta.size}×{meta.size} · EC {ecl}
              </Badge>
            ) : undefined
          }
        >
          <div className="space-y-3">
            <div className="mx-auto flex aspect-square w-full max-w-[280px] items-center justify-center overflow-hidden rounded-xl bg-white p-4 ring-1 ring-black/10">
              {svg ? (
                <div
                  className="h-full w-full [&_svg]:block [&_svg]:h-full [&_svg]:w-full"
                  dangerouslySetInnerHTML={{ __html: svg }}
                />
              ) : (
                <div className="flex flex-col items-center gap-2 text-neutral-300">
                  <QrCode className="size-12" />
                  <span className="text-xs text-neutral-400">
                    {error ? "Can’t generate" : "Your QR code appears here"}
                  </span>
                </div>
              )}
            </div>

            {error && (
              <div className="bg-danger-soft text-danger-soft-fg rounded-lg px-3 py-2 text-[13px]">{error}</div>
            )}

            <div className="flex gap-2">
              <Button variant="secondary" size="sm" className="flex-1" onClick={downloadSvg} disabled={!svg}>
                <FileCode2 className="size-3.5" />
                Download SVG
              </Button>
              <Button variant="secondary" size="sm" className="flex-1" onClick={downloadPng} disabled={!pngUrl}>
                <ImageIcon className="size-3.5" />
                Download PNG
              </Button>
            </div>
          </div>
        </ToolCard>

        <ToolCard
          title="Encoded data"
          right={payload ? <CopyIconButton value={payload} label="Copy encoded string" /> : undefined}
        >
          <textarea
            readOnly
            value={payload}
            rows={6}
            spellCheck={false}
            placeholder="Fill in the fields to build the encoded string."
            className={cn(textareaCls, "resize-none")}
          />
          <p className="text-fg-subtle mt-2 text-[12px]">
            {payload ? `${payload.length} characters` : "Nothing to encode yet."}
          </p>
        </ToolCard>
      </div>
    </div>
  );
}
