import { useQuery } from "@tanstack/react-query";

/**
 * QEMU guest agent info — GET /api/proxmox/{id}/guests/{vmid}/agent.
 *
 * The generated OpenAPI schema (lib/schema.d.ts) predates this endpoint, so
 * this module types it locally and calls it with a plain fetch. When the
 * schema is regenerated, replace this module's fetcher with the typed client
 * and delete the local types — everything agent-related lives here on purpose.
 */

export type PveAgentAddress = {
  address: string;
  type: "ipv4" | "ipv6";
  prefix?: number;
};

export type PveAgentInterface = {
  name: string;
  mac?: string;
  addresses: PveAgentAddress[];
};

export type PveAgentInfo = {
  available: boolean;
  reason?: string;
  hostname?: string;
  os?: string;
  interfaces: PveAgentInterface[];
};

const UNAVAILABLE: PveAgentInfo = { available: false, interfaces: [] };

export async function fetchGuestAgent(instanceId: string, vmid: number): Promise<PveAgentInfo> {
  const res = await fetch(
    `/api/proxmox/${encodeURIComponent(instanceId)}/guests/${vmid}/agent`,
    { headers: { Accept: "application/json" } },
  );
  // A server that predates the endpoint 404s — or answers the SPA fallback
  // (200 + text/html). Treat both as "no agent", silently, so older
  // deployments degrade to the pre-agent UI.
  if (res.status === 404) return UNAVAILABLE;
  if (!res.headers.get("content-type")?.includes("application/json")) return UNAVAILABLE;
  if (!res.ok) throw new Error(`agent info failed (${res.status})`);
  const body = (await res.json()) as Partial<PveAgentInfo>;
  return {
    available: body.available === true,
    reason: body.reason,
    hostname: body.hostname,
    os: body.os,
    interfaces: body.interfaces ?? [],
  };
}

/**
 * useGuestAgent caches per guest and never retries: agent info is decoration,
 * not critical data. Pass enabled=false to defer (e.g. off-screen list rows).
 */
export function useGuestAgent(instanceId: string, vmid: number, enabled: boolean) {
  return useQuery({
    queryKey: ["pve", instanceId, "guest", vmid, "agent"],
    queryFn: () => fetchGuestAgent(instanceId, vmid),
    enabled,
    staleTime: 60_000,
    retry: false,
    refetchOnWindowFocus: false,
  });
}

function isLoopback(addr: PveAgentAddress): boolean {
  return addr.address.startsWith("127.") || addr.address === "::1";
}

function isLinkLocal(addr: PveAgentAddress): boolean {
  return addr.address.startsWith("169.254.") || addr.address.toLowerCase().startsWith("fe80:");
}

/** agentAddresses flattens interfaces to real addresses (loopback dropped). */
export function agentAddresses(
  info: PveAgentInfo,
): { iface: string; addr: PveAgentAddress }[] {
  const out: { iface: string; addr: PveAgentAddress }[] = [];
  for (const iface of info.interfaces) {
    if (iface.name === "lo") continue;
    for (const addr of iface.addresses) {
      if (isLoopback(addr)) continue;
      out.push({ iface: iface.name, addr });
    }
  }
  return out;
}

/** primaryIpv4 picks the address worth showing inline: first routable IPv4. */
export function primaryIpv4(info: PveAgentInfo): string | null {
  const all = agentAddresses(info);
  const v4 = all.find(({ addr }) => addr.type === "ipv4" && !isLinkLocal(addr));
  return v4 ? v4.addr.address : null;
}
