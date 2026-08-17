// Shared helpers for connected systems (integrations): status presentation,
// per-type icons/labels, and the instance list every systems surface reads.
import { useQuery } from "@tanstack/react-query";
import { Boxes, Container, Database, Gamepad2, HardDrive, House, Pickaxe, Server, ShieldCheck, type LucideIcon } from "lucide-react";
import { api, unwrap, type Integration } from "./api";

/** RBAC resource guarding add/edit/remove/toggle of systems. */
export const SYSTEMS_RES = "system/integrations";

/** statusTone maps an integration status to a badge/dot tone. */
export const statusTone = {
  pending: "neutral",
  connecting: "info",
  connected: "success",
  degraded: "warning",
  unreachable: "danger",
  disabled: "neutral",
} as const;

/** Known system types, in display order. */
export const SYSTEM_TYPES = ["proxmox", "docker", "pihole", "adguard", "homeassistant", "minecraft", "amp", "mysql", "mongodb", "truenas"] as const;

const typeIcons: Record<string, LucideIcon> = {
  proxmox: Server,
  docker: Container,
  pihole: ShieldCheck,
  adguard: ShieldCheck,
  homeassistant: House,
  minecraft: Pickaxe,
  amp: Gamepad2,
  mysql: Database,
  mongodb: Database,
  truenas: HardDrive,
};

export function systemTypeIcon(type: string): LucideIcon {
  return typeIcons[type] ?? Boxes;
}

/** Product names are not translated ("Proxmox VE", "Docker", …). */
export function systemTypeLabel(t: (key: string) => string, type: string): string {
  return (SYSTEM_TYPES as readonly string[]).includes(type) ? t(`systems.types.${type}`) : type;
}

/**
 * worstStatus aggregates instance statuses for a type's nav entry:
 * unreachable > degraded/connecting/pending > connected.
 */
export function worstStatus(rows: Integration[]): "connected" | "degraded" | "unreachable" {
  if (rows.some((r) => r.status === "unreachable")) return "unreachable";
  if (rows.some((r) => r.status === "degraded" || r.status === "connecting" || r.status === "pending")) {
    return "degraded";
  }
  return "connected";
}

/**
 * useIntegrations is the shared systems list — the server filters it to what
 * the caller may see, so no client-side grant check gates visibility.
 */
export function useIntegrations(enabled = true) {
  return useQuery({
    queryKey: ["integrations"],
    queryFn: async () => unwrap(await api.GET("/integrations")),
    staleTime: 30_000,
    enabled,
  });
}
