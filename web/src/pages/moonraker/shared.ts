import { useEffect } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap, type MoonrakerSnapshot } from "@/lib/api";
import { live } from "@/lib/ws";

/** WS topic carrying live snapshot pushes, G-code responses and klippy edges. */
export function mrTopic(instanceId: string): string {
  return `integration:moonraker/${instanceId}`;
}

export function mrKey(instanceId: string): (string | undefined)[] {
  return ["moonraker", instanceId, "snapshot"];
}

/**
 * useMoonrakerSnapshot loads the printer snapshot and keeps it live: the
 * backend streams a fresh snapshot over the WS topic (coalesced to a few per
 * second) and we replace the cached copy in place. A slow fallback poll covers
 * the window before the socket connects or if it drops.
 */
export function useMoonrakerSnapshot(instanceId: string) {
  const qc = useQueryClient();
  const query = useQuery({
    queryKey: mrKey(instanceId),
    queryFn: async (): Promise<MoonrakerSnapshot> =>
      unwrap(await api.GET("/moonraker/{id}/snapshot", { params: { path: { id: instanceId } } })),
    refetchInterval: 6000,
  });

  useEffect(() => {
    return live.subscribe(mrTopic(instanceId), (raw: unknown) => {
      const { event, data } = (raw ?? {}) as { event?: string; data?: unknown };
      if (event === "snapshot" && data) {
        qc.setQueryData<MoonrakerSnapshot>(mrKey(instanceId), (prev) => {
          const next = data as MoonrakerSnapshot;
          // Live pushes may omit REST-only header identity — keep the last.
          if (prev && (!next.printer || !next.printer.hostname)) {
            next.printer = prev.printer;
          }
          return next;
        });
      } else if (event === "klippy") {
        // A ready/shutdown/disconnect edge — pull a full authoritative snapshot.
        void qc.invalidateQueries({ queryKey: mrKey(instanceId) });
      }
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [instanceId]);

  return query;
}

function invalidate(qc: ReturnType<typeof useQueryClient>, instanceId: string) {
  void qc.invalidateQueries({ queryKey: mrKey(instanceId) });
}

/** RunGcode — the universal control (macros, homing, fan/LED commands, …). */
export function useMoonrakerGcode(instanceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (script: string) =>
      unwrap(
        await api.POST("/moonraker/{id}/gcode", {
          params: { path: { id: instanceId } },
          body: { script },
        }),
      ),
    onSuccess: () => invalidate(qc, instanceId),
  });
}

export function useMoonrakerSetTemp(instanceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { heater: string; target: number }) =>
      unwrap(
        await api.POST("/moonraker/{id}/temperature", {
          params: { path: { id: instanceId } },
          body: vars,
        }),
      ),
    onSuccess: () => invalidate(qc, instanceId),
  });
}

export function useMoonrakerPrint(instanceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (action: "pause" | "resume" | "cancel") =>
      unwrap(
        await api.POST("/moonraker/{id}/print", {
          params: { path: { id: instanceId } },
          body: { action },
        }),
      ),
    onSuccess: () => invalidate(qc, instanceId),
  });
}

export function useMoonrakerEstop(instanceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/moonraker/{id}/estop", { params: { path: { id: instanceId } } })),
    onSuccess: () => invalidate(qc, instanceId),
  });
}

export function useMoonrakerFirmwareRestart(instanceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/moonraker/{id}/firmware-restart", { params: { path: { id: instanceId } } }),
      ),
    onSuccess: () => invalidate(qc, instanceId),
  });
}
