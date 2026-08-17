import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { api, unwrap } from "@/lib/api";
import { live } from "@/lib/ws";

/** RBAC/metrics resource for a tailnet, e.g. "tailscale/<id>". */
export function tsResource(instanceId: string): string {
  return `tailscale/${instanceId}`;
}

export const tsDevicesKey = (instanceId: string) => ["ts", instanceId, "devices"] as const;

export function useTsDevices(instanceId: string) {
  return useQuery({
    queryKey: tsDevicesKey(instanceId),
    queryFn: async () =>
      unwrap(await api.GET("/ts/{id}/devices", { params: { path: { id: instanceId } } })),
    staleTime: 10_000,
    refetchInterval: 30_000,
  });
}

/** Refetch devices when the driver reports connectivity changes. */
export function useTsLive(instanceId: string) {
  const queryClient = useQueryClient();
  useEffect(() => {
    return live.subscribe(`integration:tailscale/${instanceId}`, (payload) => {
      const frame = payload as { event?: string } | undefined;
      if (frame?.event === "devices-changed") {
        void queryClient.invalidateQueries({ queryKey: tsDevicesKey(instanceId) });
      }
    });
  }, [instanceId, queryClient]);
}

/** The exit-node advertisement is the 0.0.0.0/0 + ::/0 pair. */
export const EXIT_ROUTES = ["0.0.0.0/0", "::/0"];

export function isExitRoute(route: string): boolean {
  return EXIT_ROUTES.includes(route);
}
