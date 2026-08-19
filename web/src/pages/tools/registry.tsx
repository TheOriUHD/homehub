import { lazy, type ComponentType, type LazyExoticComponent } from "react";
import {
  Binary,
  Braces,
  CalendarClock,
  Globe,
  Radar,
  SendHorizontal,
  ShieldCheck,
  Zap,
  Clapperboard,
  Clock,
  FileImage,
  GitCompareArrows,
  Gauge,
  Hash,
  KeyRound,
  Network,
  Palette,
  QrCode,
  Regex,
  ScanSearch,
  type LucideIcon,
} from "lucide-react";

export type ToolCategory = "network" | "dev" | "media" | "qol";

export type ToolDef = {
  id: string;
  category: ToolCategory;
  icon: LucideIcon;
  title: string;
  desc: string;
  Component: LazyExoticComponent<ComponentType>;
};

export const CATEGORY_LABEL: Record<ToolCategory, string> = {
  network: "Network",
  dev: "Dev & Data",
  media: "Media",
  qol: "Quick tools",
};

export const CATEGORY_ORDER: ToolCategory[] = ["network", "dev", "media", "qol"];

/**
 * Every self-contained (client-side) utility. A tool is one file under
 * ./apps that default-exports its body component; the dynamic /tools/$toolId
 * route wraps it in the shared ToolLayout chrome. Adding a tool is a single
 * entry here plus its file — nothing else in the app changes.
 */
export const TOOLS: ToolDef[] = [
  // ---- Network ----
  {
    id: "qr",
    category: "network",
    icon: QrCode,
    title: "QR code generator",
    desc: "Encode links, Wi-Fi, vCards and calendar events — export SVG or PNG.",
    Component: lazy(() => import("./apps/qr")),
  },
  {
    id: "subnet",
    category: "network",
    icon: Network,
    title: "Subnet calculator",
    desc: "CIDR ranges, usable hosts, netmask and broadcast address.",
    Component: lazy(() => import("./apps/subnet")),
  },
  {
    id: "ssl",
    category: "network",
    icon: ShieldCheck,
    title: "SSL certificate checker",
    desc: "Inspect a domain's TLS cert: expiry, issuer, chain, validity.",
    Component: lazy(() => import("./apps/ssl")),
  },
  {
    id: "dns",
    category: "network",
    icon: Globe,
    title: "DNS lookup",
    desc: "A/AAAA/CNAME/TXT/MX/NS queries against any resolver.",
    Component: lazy(() => import("./apps/dns")),
  },
  {
    id: "reachability",
    category: "network",
    icon: Radar,
    title: "Port check & ping",
    desc: "Test a host's reachability — TCP port and ICMP ping.",
    Component: lazy(() => import("./apps/reachability")),
  },
  {
    id: "ipscan",
    category: "network",
    icon: ScanSearch,
    title: "IP scanner",
    desc: "Sweep your subnet — alive hosts, hostnames, MAC/vendor and open ports.",
    Component: lazy(() => import("./apps/ipscan")),
  },
  {
    id: "wol",
    category: "network",
    icon: Zap,
    title: "Wake-on-LAN",
    desc: "Wake saved machines with a magic packet.",
    Component: lazy(() => import("./apps/wol")),
  },
  // ---- Dev & Data ----
  {
    id: "json-yaml",
    category: "dev",
    icon: Braces,
    title: "JSON ↔ YAML",
    desc: "Format, validate and convert both ways with precise errors.",
    Component: lazy(() => import("./apps/json-yaml")),
  },
  {
    id: "cron",
    category: "dev",
    icon: CalendarClock,
    title: "Cron builder",
    desc: "Build expressions visually with a plain-English preview and next runs.",
    Component: lazy(() => import("./apps/cron")),
  },
  {
    id: "timestamp",
    category: "dev",
    icon: Clock,
    title: "Timestamp converter",
    desc: "Unix epoch ↔ human time, timezone-aware.",
    Component: lazy(() => import("./apps/timestamp")),
  },
  {
    id: "diff",
    category: "dev",
    icon: GitCompareArrows,
    title: "Diff tool",
    desc: "Side-by-side comparison of two texts or configs.",
    Component: lazy(() => import("./apps/diff")),
  },
  {
    id: "hash",
    category: "dev",
    icon: Hash,
    title: "Hash & checksum",
    desc: "SHA-256/1, MD5 and more for text and file verification.",
    Component: lazy(() => import("./apps/hash")),
  },
  {
    id: "http",
    category: "dev",
    icon: SendHorizontal,
    title: "HTTP request tester",
    desc: "A mini API client: method, headers, body, pretty response.",
    Component: lazy(() => import("./apps/http")),
  },
  {
    id: "regex",
    category: "dev",
    icon: Regex,
    title: "Regex tester",
    desc: "Live match highlighting and capture-group extraction.",
    Component: lazy(() => import("./apps/regex")),
  },
  // ---- Media ----
  {
    id: "bitrate",
    category: "media",
    icon: Gauge,
    title: "Bitrate calculator",
    desc: "Target size + duration ↔ required bitrate for exports.",
    Component: lazy(() => import("./apps/bitrate")),
  },
  {
    id: "timecode",
    category: "media",
    icon: Clapperboard,
    title: "Timecode calculator",
    desc: "Frames ↔ timecode at any fps; add and subtract timecodes.",
    Component: lazy(() => import("./apps/timecode")),
  },
  {
    id: "exif",
    category: "media",
    icon: FileImage,
    title: "EXIF viewer & stripper",
    desc: "Inspect photo metadata and strip GPS / serial numbers.",
    Component: lazy(() => import("./apps/exif")),
  },
  // ---- Quick tools ----
  {
    id: "password",
    category: "qol",
    icon: KeyRound,
    title: "Password generator",
    desc: "Configurable passwords and passphrases, generated in-browser.",
    Component: lazy(() => import("./apps/password")),
  },
  {
    id: "base64",
    category: "qol",
    icon: Binary,
    title: "Base64 & URL",
    desc: "Encode and decode Base64 and URL components, both ways.",
    Component: lazy(() => import("./apps/base64")),
  },
  {
    id: "color",
    category: "qol",
    icon: Palette,
    title: "Color converter",
    desc: "HEX / RGB / HSL with a live picker.",
    Component: lazy(() => import("./apps/color")),
  },
];

export function toolById(id: string | undefined): ToolDef | undefined {
  return TOOLS.find((t) => t.id === id);
}
