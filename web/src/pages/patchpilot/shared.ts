import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap } from "@/lib/api";
import type {
  PatchpilotDetectResult,
  PatchpilotHost,
  PatchpilotHostRun,
  PatchpilotRunDetail,
  PatchpilotSchedule,
  PatchpilotScheduleInput,
} from "@/lib/api";

/** RBAC/metrics resource prefix for a fleet, e.g. "patchpilot/<id>". */
export function ppResource(instanceId: string): string {
  return `patchpilot/${instanceId}`;
}

/** WS topic carrying bootstrap steps, run events, and log lines. */
export function ppTopic(instanceId: string): string {
  return `integration:patchpilot/${instanceId}`;
}

/** Tone for a host's last-run / run status. */
export function runStatusTone(status: string): "success" | "warning" | "danger" | "neutral" | "info" {
  switch (status) {
    case "success":
      return "success";
    case "running":
      return "info";
    case "partial":
      return "warning";
    case "failed":
      return "danger";
    default:
      return "neutral";
  }
}

/** Human label for a package manager id. */
export function managerLabel(mgr: string): string {
  switch (mgr) {
    case "apt":
      return "APT";
    case "apt_proxmox":
      return "APT · Proxmox";
    case "dnf":
      return "DNF";
    case "pacman":
      return "pacman";
    case "zypper":
      return "zypper";
    case "apk":
      return "apk";
    default:
      return mgr || "—";
  }
}

export function useFleetInstances() {
  return useQuery({
    queryKey: ["integrations"],
    queryFn: async () => unwrap(await api.GET("/integrations")),
    staleTime: 30_000,
  });
}

export function usePatchpilotHosts(instanceId: string) {
  return useQuery({
    queryKey: ["patchpilot", instanceId, "hosts"],
    queryFn: async () =>
      unwrap(await api.GET("/patchpilot/{id}/hosts", { params: { path: { id: instanceId } } })),
    refetchInterval: 30_000,
  });
}

export function usePatchpilotHost(instanceId: string, hostId: string) {
  return useQuery({
    queryKey: ["patchpilot", instanceId, "host", hostId],
    queryFn: async () =>
      unwrap(
        await api.GET("/patchpilot/{id}/hosts/{hostId}", {
          params: { path: { id: instanceId, hostId } },
        }),
      ),
    refetchInterval: 30_000,
  });
}

export function usePatchpilotHostRuns(instanceId: string, hostId: string) {
  return useQuery({
    queryKey: ["patchpilot", instanceId, "host", hostId, "runs"],
    queryFn: async (): Promise<PatchpilotHostRun[]> =>
      unwrap(
        await api.GET("/patchpilot/{id}/hosts/{hostId}/runs", {
          params: { path: { id: instanceId, hostId } },
        }),
      ),
    refetchInterval: 15_000,
  });
}

export function usePatchpilotSchedules(instanceId: string) {
  return useQuery({
    queryKey: ["patchpilot", instanceId, "schedules"],
    queryFn: async (): Promise<PatchpilotSchedule[]> =>
      unwrap(await api.GET("/patchpilot/{id}/schedules", { params: { path: { id: instanceId } } })),
    staleTime: 20_000,
  });
}

export function usePatchpilotRuns(instanceId: string) {
  return useQuery({
    queryKey: ["patchpilot", instanceId, "runs"],
    queryFn: async () =>
      unwrap(await api.GET("/patchpilot/{id}/runs", { params: { path: { id: instanceId } } })),
    refetchInterval: 10_000,
  });
}

export function usePatchpilotRunDetail(instanceId: string, runId: string, live: boolean) {
  return useQuery({
    queryKey: ["patchpilot", instanceId, "run", runId],
    queryFn: async (): Promise<PatchpilotRunDetail> =>
      unwrap(
        await api.GET("/patchpilot/{id}/runs/{runId}", {
          params: { path: { id: instanceId, runId } },
        }),
      ),
    refetchInterval: live ? 3_000 : false,
  });
}

export function usePatchpilotPublicKey(instanceId: string) {
  return useQuery({
    queryKey: ["patchpilot", instanceId, "publickey"],
    queryFn: async () =>
      unwrap(await api.GET("/patchpilot/{id}/publickey", { params: { path: { id: instanceId } } })),
    staleTime: Infinity,
  });
}

// ---- mutations --------------------------------------------------------------

export function useDetectHost(instanceId: string) {
  return useMutation({
    mutationFn: async (body: {
      sshHostId?: string;
      hostname?: string;
      sshPort?: number;
      sshUser?: string;
      password?: string;
      rootPassword?: string;
    }): Promise<PatchpilotDetectResult> =>
      unwrap(await api.POST("/patchpilot/{id}/hosts/detect", { params: { path: { id: instanceId } }, body })),
  });
}

export function useOnboardHost(instanceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: {
      name?: string;
      sshHostId?: string;
      hostname?: string;
      sshPort?: number;
      sshUser?: string;
      password?: string;
      rootPassword?: string;
      notes?: string;
      autoReboot?: boolean;
    }): Promise<PatchpilotHost> =>
      unwrap(await api.POST("/patchpilot/{id}/hosts/onboard", { params: { path: { id: instanceId } }, body })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["patchpilot", instanceId, "hosts"] });
      void qc.invalidateQueries({ queryKey: ["integrations"] });
    },
  });
}

export function useHostAction(instanceId: string) {
  const qc = useQueryClient();
  const invalidate = () => {
    void qc.invalidateQueries({ queryKey: ["patchpilot", instanceId] });
  };
  return {
    dryRun: (hostId: string) =>
      api.POST("/patchpilot/{id}/hosts/{hostId}/dry-run", { params: { path: { id: instanceId, hostId } } }),
    run: (hostId: string) =>
      api.POST("/patchpilot/{id}/hosts/{hostId}/run", { params: { path: { id: instanceId, hostId } } }),
    reboot: (hostId: string) =>
      api.POST("/patchpilot/{id}/hosts/{hostId}/reboot", { params: { path: { id: instanceId, hostId } } }),
    repairSudoers: (hostId: string) =>
      api.POST("/patchpilot/{id}/hosts/{hostId}/repair-sudoers", { params: { path: { id: instanceId, hostId } } }),
    invalidate,
  };
}

export function useUpdateHost(instanceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { hostId: string; body: { name?: string; enabled?: boolean; autoReboot?: boolean; critical?: boolean; notes?: string } }) =>
      unwrap(
        await api.PUT("/patchpilot/{id}/hosts/{hostId}", {
          params: { path: { id: instanceId, hostId: vars.hostId } },
          body: vars.body,
        }),
      ),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["patchpilot", instanceId] }),
  });
}

/** Bind a host to a registry entry (null unbinds); the address follows it from then on. */
export function useBindHost(instanceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { hostId: string; sshHostId: string | null }) =>
      unwrap(
        await api.PUT("/patchpilot/{id}/hosts/{hostId}/binding", {
          params: { path: { id: instanceId, hostId: vars.hostId } },
          body: { sshHostId: vars.sshHostId },
        }),
      ),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["patchpilot", instanceId] }),
  });
}

/** Run the bootstrap again on a known host; steps stream on the fleet topic meanwhile. */
export function useRebootstrapHost(instanceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: {
      hostId: string;
      body: { sshUser?: string; password?: string; rootPassword?: string };
    }): Promise<PatchpilotHost> =>
      unwrap(
        await api.POST("/patchpilot/{id}/hosts/{hostId}/rebootstrap", {
          params: { path: { id: instanceId, hostId: vars.hostId } },
          body: vars.body,
        }),
      ),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["patchpilot", instanceId] }),
  });
}

export function useDeleteHost(instanceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (hostId: string) =>
      unwrap(await api.DELETE("/patchpilot/{id}/hosts/{hostId}", { params: { path: { id: instanceId, hostId } } })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["patchpilot", instanceId, "hosts"] });
      void qc.invalidateQueries({ queryKey: ["integrations"] });
    },
  });
}

export function useRunFleet(instanceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: { hostIds?: string[]; suppressReboot?: boolean }) =>
      unwrap(await api.POST("/patchpilot/{id}/run", { params: { path: { id: instanceId } }, body })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["patchpilot", instanceId, "runs"] }),
  });
}

export function useAbortRun(instanceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (runId: string) =>
      unwrap(
        await api.POST("/patchpilot/{id}/runs/{runId}/abort", {
          params: { path: { id: instanceId, runId } },
        }),
      ),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["patchpilot", instanceId, "runs"] }),
  });
}

export function useSaveSchedule(instanceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { id?: string; body: PatchpilotScheduleInput }) => {
      if (vars.id) {
        return unwrap(
          await api.PUT("/patchpilot/{id}/schedules/{scheduleId}", {
            params: { path: { id: instanceId, scheduleId: vars.id } },
            body: vars.body,
          }),
        );
      }
      return unwrap(
        await api.POST("/patchpilot/{id}/schedules", { params: { path: { id: instanceId } }, body: vars.body }),
      );
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["patchpilot", instanceId, "schedules"] }),
  });
}

export function useDeleteSchedule(instanceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (scheduleId: string) =>
      unwrap(
        await api.DELETE("/patchpilot/{id}/schedules/{scheduleId}", {
          params: { path: { id: instanceId, scheduleId } },
        }),
      ),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["patchpilot", instanceId, "schedules"] }),
  });
}

export function useRunSchedule(instanceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (scheduleId: string) =>
      unwrap(
        await api.POST("/patchpilot/{id}/schedules/{scheduleId}/run", {
          params: { path: { id: instanceId, scheduleId } },
        }),
      ),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["patchpilot", instanceId, "runs"] }),
  });
}

export type { PatchpilotHost, PatchpilotSchedule, PatchpilotRun, PatchpilotHostRun } from "@/lib/api";
