import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap, type McOverview, type McPlayer, type McStatus } from "@/lib/api";
import { live } from "@/lib/ws";

/** RBAC/metrics resource for an instance, e.g. "minecraft/<id>". */
export function mcResource(instanceId: string): string {
  return `minecraft/${instanceId}`;
}

/** WS topic carrying console/status/players/companion events. */
export function mcTopic(instanceId: string): string {
  return `integration:minecraft/${instanceId}`;
}

export const mcOverviewKey = (instanceId: string) => ["mc", instanceId, "overview"] as const;

/**
 * useMcOverview is the module's shared overview query — the header and every
 * tab read the same key, and the live hook patches it in place.
 */
export function useMcOverview(instanceId: string) {
  return useQuery({
    queryKey: mcOverviewKey(instanceId),
    queryFn: async () =>
      unwrap(await api.GET("/mc/{id}/overview", { params: { path: { id: instanceId } } })),
    staleTime: 15_000,
  });
}

/**
 * useMcLive subscribes once per module and patches the overview cache for
 * high-frequency pushes ("status", "players-changed") instead of refetching.
 * "companion-changed" (connect/disconnect) invalidates — connection state,
 * versions, and pairing may all have changed. "console" events are consumed by
 * the console tab's own buffer, never here.
 */
export function useMcLive(instanceId: string) {
  const queryClient = useQueryClient();
  useEffect(() => {
    return live.subscribe(mcTopic(instanceId), (payload) => {
      const frame = payload as { event?: string; data?: unknown } | undefined;
      if (!frame?.event) return;
      if (frame.event === "status" && frame.data) {
        const status = frame.data as McStatus;
        queryClient.setQueryData<McOverview>(mcOverviewKey(instanceId), (prev) =>
          prev ? { ...prev, connected: true, status } : prev,
        );
      } else if (frame.event === "players-changed" && frame.data) {
        const { players } = frame.data as { players?: McPlayer[] };
        queryClient.setQueryData<McOverview>(mcOverviewKey(instanceId), (prev) =>
          prev ? { ...prev, players: players ?? [] } : prev,
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

/** Loaders whose companion drops into mods/ instead of plugins/. */
export const MOD_LOADERS = ["fabric", "forge"];
