import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap } from "@/lib/api";
import { live } from "@/lib/ws";

/** RBAC/metrics resource for a panel, e.g. "amp/<id>". */
export function ampResource(instanceId: string): string {
  return `amp/${instanceId}`;
}

/** WS topic carrying console/status events for a panel. */
export function ampTopic(instanceId: string): string {
  return `integration:amp/${instanceId}`;
}

export const ampOverviewKey = (instanceId: string) => ["amp", instanceId, "overview"] as const;

export function useAmpOverview(instanceId: string) {
  return useQuery({
    queryKey: ampOverviewKey(instanceId),
    queryFn: async () =>
      unwrap(await api.GET("/amp/{id}/overview", { params: { path: { id: instanceId } } })),
    staleTime: 10_000,
    refetchInterval: 15_000,
  });
}

/**
 * useAmpLive refetches the overview when the driver reports any instance
 * changed state — cheap, and the payload is the whole inventory anyway.
 */
export function useAmpLive(instanceId: string) {
  const queryClient = useQueryClient();
  useEffect(() => {
    return live.subscribe(ampTopic(instanceId), (payload) => {
      const frame = payload as { event?: string } | undefined;
      if (frame?.event === "instances-changed") {
        void queryClient.invalidateQueries({ queryKey: ampOverviewKey(instanceId) });
      }
    });
  }, [instanceId, queryClient]);
}

/** Badge tone for an AMP application state slug. */
export function ampStateTone(
  state: string,
): "success" | "info" | "warning" | "danger" | "neutral" {
  switch (state) {
    case "running":
      return "success";
    case "starting":
    case "restarting":
    case "configuring":
    case "preStart":
    case "installing":
    case "updating":
      return "info";
    case "stopping":
    case "preparingForSleep":
    case "waiting":
    case "awaitingInput":
    case "maintenance":
      return "warning";
    case "failed":
    case "suspended":
      return "danger";
    default:
      return "neutral";
  }
}

/** States that flow into another state on their own — the badge pulses. */
export function ampStateBusy(state: string): boolean {
  return ["starting", "restarting", "stopping", "configuring", "preStart", "installing", "updating", "preparingForSleep"].includes(state);
}
