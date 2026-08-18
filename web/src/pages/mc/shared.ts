import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap, type McOverview, type McPlayer, type McServer, type McStatus } from "@/lib/api";
import { live } from "@/lib/ws";

/** RBAC resource for a network, e.g. "minecraft/<id>". */
export function mcResource(instanceId: string): string {
  return `minecraft/${instanceId}`;
}

/** RBAC/metrics resource for one server within a network. */
export function mcServerResource(instanceId: string, serverId: string): string {
  return `minecraft/${instanceId}/server/${serverId}`;
}

/** WS topic carrying console/status/players/companion events for the whole
 * network; event payloads carry the serverId they belong to. */
export function mcTopic(instanceId: string): string {
  return `integration:minecraft/${instanceId}`;
}

export const mcOverviewKey = (instanceId: string) => ["mc", instanceId, "overview"] as const;

/**
 * useMcOverview is the module's shared network query — the header, the
 * server grid and every server tab read the same key, and the live hook
 * patches it in place.
 */
export function useMcOverview(instanceId: string) {
  return useQuery({
    queryKey: mcOverviewKey(instanceId),
    queryFn: async () =>
      unwrap(await api.GET("/mc/{id}/overview", { params: { path: { id: instanceId } } })),
    staleTime: 15_000,
  });
}

/** The one server this page cares about, out of the shared overview. */
export function useMcServer(instanceId: string, serverId: string): McServer | undefined {
  const overview = useMcOverview(instanceId);
  return overview.data?.servers.find((s) => s.id === serverId);
}

function patchServer(
  overview: McOverview | undefined,
  serverId: string,
  patch: (server: McServer) => McServer,
): McOverview | undefined {
  if (!overview) return overview;
  return {
    ...overview,
    servers: overview.servers.map((s) => (s.id === serverId ? patch(s) : s)),
  };
}

/**
 * useMcLive subscribes once per module and patches the overview cache for
 * high-frequency pushes ("status", "players-changed") instead of
 * refetching — each event names its server. "companion-changed"
 * (connect/disconnect) invalidates. "console" events are consumed by the
 * console tab's own buffer, never here.
 */
export function useMcLive(instanceId: string) {
  const queryClient = useQueryClient();
  useEffect(() => {
    return live.subscribe(mcTopic(instanceId), (payload) => {
      const frame = payload as { event?: string; data?: unknown } | undefined;
      if (!frame?.event) return;
      const data = frame.data as { serverId?: string; status?: McStatus; players?: McPlayer[] } | undefined;
      if (frame.event === "status" && data?.serverId && data.status) {
        queryClient.setQueryData<McOverview>(mcOverviewKey(instanceId), (prev) =>
          patchServer(prev, data.serverId!, (s) => ({ ...s, connected: true, status: data.status })),
        );
      } else if (frame.event === "players-changed" && data?.serverId) {
        queryClient.setQueryData<McOverview>(mcOverviewKey(instanceId), (prev) =>
          patchServer(prev, data.serverId!, (s) => ({ ...s, players: data.players ?? [] })),
        );
      } else if (frame.event === "companion-changed") {
        void queryClient.invalidateQueries({ queryKey: mcOverviewKey(instanceId) });
        void queryClient.invalidateQueries({ queryKey: ["integrations"] });
      }
    });
  }, [instanceId, queryClient]);
}

/** TPS badge tone: ≥19 healthy, ≥15 struggling, below that in trouble. */
export function tpsTone(tps: number): "success" | "warning" | "danger" {
  if (tps >= 19) return "success";
  if (tps >= 15) return "warning";
  return "danger";
}

/** Loader product names are not translated, only capitalized properly. */
const LOADER_LABELS: Record<string, string> = {
  paper: "Paper",
  spigot: "Spigot",
  velocity: "Velocity",
  fabric: "Fabric",
  forge: "Forge",
};

export function loaderLabel(loader: string | undefined): string | null {
  if (!loader) return null;
  return LOADER_LABELS[loader] ?? loader.charAt(0).toUpperCase() + loader.slice(1);
}

/** Proxy loaders front subservers and have no tick loop of their own. */
export const PROXY_LOADERS = ["velocity"];

/**
 * Network-wide player totals without double counting: players connect
 * THROUGH the proxy and sit ON a subserver, so a connected proxy's count
 * is the network's count; subservers are only summed when no proxy is up.
 */
export function networkPlayers(servers: McServer[]): { online: number; max: number } {
  const up = servers.filter((s) => s.connected && s.status);
  const proxies = up.filter((s) => PROXY_LOADERS.includes(s.loader ?? ""));
  const pool = proxies.length > 0 ? proxies : up;
  return {
    online: pool.reduce((sum, s) => sum + (s.status?.playersOnline ?? 0), 0),
    max: pool.reduce((sum, s) => sum + (s.status?.playersMax ?? 0), 0),
  };
}

/** Loaders whose companion drops into mods/ instead of plugins/. */
export const MOD_LOADERS = ["fabric", "forge"];
