import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { api, unwrap } from "@/lib/api";
import { live } from "@/lib/ws";

/** RBAC/metrics resource for a Caddy system, e.g. "caddy/<id>". */
export function caddyResource(instanceId: string): string {
  return `caddy/${instanceId}`;
}

export const caddySitesKey = (instanceId: string) => ["caddy", instanceId, "sites"] as const;

/**
 * useCaddySites reads the manager state live. The Caddyfile is the source
 * of truth and someone may edit it by hand on the box, so it also polls.
 */
export function useCaddySites(instanceId: string) {
  return useQuery({
    queryKey: caddySitesKey(instanceId),
    queryFn: async () =>
      unwrap(await api.GET("/caddy/{id}/sites", { params: { path: { id: instanceId } } })),
    refetchInterval: 30_000,
  });
}

/** Refetch on our own writes landing (any tab, any user). */
export function useCaddyLive(instanceId: string) {
  const queryClient = useQueryClient();
  useEffect(() => {
    return live.subscribe(`integration:caddy/${instanceId}`, (payload) => {
      const frame = payload as { event?: string } | undefined;
      if (frame?.event === "sites-changed") {
        void queryClient.invalidateQueries({ queryKey: caddySitesKey(instanceId) });
      }
    });
  }, [instanceId, queryClient]);
}
