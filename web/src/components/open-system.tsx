import { useTranslation } from "react-i18next";
import { ExternalLink } from "lucide-react";
import type { Integration } from "@/lib/api";
import { useIntegrations } from "@/lib/systems";

/**
 * systemWebUrl derives the upstream's own web UI address from an instance's
 * config, mirroring each driver's host normalization (default scheme/port).
 * Returns null for types without a web interface.
 */
export function systemWebUrl(instance: Pick<Integration, "type" | "config">): string | null {
  const cfg = instance.config as Record<string, unknown>;
  const str = (key: string) => (typeof cfg[key] === "string" ? (cfg[key] as string).trim() : "");
  const withScheme = (raw: string, scheme: "http" | "https") =>
    raw.includes("://") ? raw : `${scheme}://${raw}`;

  switch (instance.type) {
    case "proxmox": {
      const host = str("host");
      if (!host) return null;
      const url = withScheme(host, "https");
      // The PVE web UI listens on 8006; only add it when no port was given.
      return /:\d+/.test(url.replace(/^https?:\/\//, "")) ? url : `${url}:8006`;
    }
    case "homeassistant": {
      const host = str("host");
      return host ? withScheme(host, "http") : null;
    }
    case "amp": {
      const host = str("host");
      return host ? withScheme(host, "http") : null;
    }
    case "truenas": {
      const host = str("host");
      return host ? withScheme(host, "https") : null;
    }
    case "adguard": {
      const host = str("host");
      return host ? withScheme(host, "http") : null;
    }
    case "pihole": {
      const host = str("host");
      return host ? `${withScheme(host, "http").replace(/\/$/, "")}/admin` : null;
    }
    case "tailscale":
      return "https://login.tailscale.com/admin";
    case "moonraker":
    case "aperture":
    case "caddy": {
      const base = str("baseUrl");
      // caddy's baseUrl is the helper, not a UI — only the first two link out.
      return instance.type === "caddy" ? null : base ? withScheme(base, "http") : null;
    }
    default:
      return null;
  }
}

/** Product names for the "Open X" label, keyed by type. */
const productName: Record<string, string> = {
  proxmox: "Proxmox",
  homeassistant: "Home Assistant",
  amp: "AMP",
  truenas: "TrueNAS",
  adguard: "AdGuard Home",
  pihole: "Pi-hole",
  tailscale: "Tailscale",
  moonraker: "Moonraker",
  aperture: "Aperture",
};

/**
 * OpenSystemLink renders the "Open <product>" header button for instances
 * whose upstream has its own web interface; renders nothing otherwise.
 * Self-sufficient: give it an instance id and it reads the (cached)
 * integrations list for the type + config.
 */
export function OpenSystemLink({ instanceId }: { instanceId: string }) {
  const { t } = useTranslation();
  const integrations = useIntegrations();
  const instance = integrations.data?.find((row) => row.id === instanceId);
  const url = instance ? systemWebUrl(instance) : null;
  if (!instance || !url) return null;
  return (
    <a
      href={url}
      target="_blank"
      rel="noopener noreferrer"
      className="border-edge bg-surface hover:bg-surface-hover text-fg flex shrink-0 items-center gap-1.5 rounded-md border px-3 py-1.5 text-[13px] font-medium shadow-1 transition-colors"
    >
      <ExternalLink className="size-3.5" aria-hidden />
      {t("systems.open", { product: productName[instance.type] ?? instance.type })}
    </a>
  );
}
