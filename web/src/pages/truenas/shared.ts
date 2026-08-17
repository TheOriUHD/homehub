import { useQuery } from "@tanstack/react-query";
import { api, unwrap, type TruenasPool } from "@/lib/api";

/** RBAC/metrics resource prefix for an instance, e.g. "truenas/<id>". */
export function truenasResource(instanceId: string): string {
  return `truenas/${instanceId}`;
}

/** Metrics resource for one pool, e.g. "truenas/<id>/pool/tank". */
export function truenasPoolResource(instanceId: string, pool: string): string {
  return `${truenasResource(instanceId)}/pool/${pool}`;
}

/** Metrics resource for one disk, e.g. "truenas/<id>/disk/sda". */
export function truenasDiskResource(instanceId: string, disk: string): string {
  return `${truenasResource(instanceId)}/disk/${disk}`;
}

/** Badge/bar tone for a pool's fill level: <70% ok, <90% warning, ≥90% danger. */
export function capacityTone(usedPct: number): "success" | "warning" | "danger" {
  if (usedPct >= 90) return "danger";
  if (usedPct >= 70) return "warning";
  return "success";
}

/** Solid fill class for the capacity bar, matching {@link capacityTone}. */
export function capacityBarClass(usedPct: number): string {
  if (usedPct >= 90) return "bg-danger";
  if (usedPct >= 70) return "bg-warning";
  return "bg-success";
}

/**
 * Health tone for a pool. An unhealthy pool, or a status naming DEGRADED,
 * warns; FAULTED / OFFLINE / UNAVAIL / REMOVED reads as danger.
 */
export function poolHealthTone(pool: Pick<TruenasPool, "status" | "healthy">): "success" | "warning" | "danger" {
  const s = pool.status.toUpperCase();
  if (/FAULTED|OFFLINE|UNAVAIL|REMOVED/.test(s)) return "danger";
  if (!pool.healthy || s.includes("DEGRADED")) return "warning";
  return "success";
}

/** Disk temperatures at or above 45 °C are flagged. */
export function diskTempWarn(temperature: number): boolean {
  return temperature >= 45;
}

/** Badge tone for an alert level (INFO / NOTICE / WARNING / CRITICAL / …). */
export function alertTone(level: string): "info" | "warning" | "danger" | "neutral" {
  const s = level.toUpperCase();
  if (/CRITICAL|ERROR|ALERT|EMERGENCY/.test(s)) return "danger";
  if (/WARNING|WARN/.test(s)) return "warning";
  if (/INFO|NOTICE/.test(s)) return "info";
  return "neutral";
}

/** Root pool a dataset belongs to (the first "/"-separated segment). */
export function datasetRootPool(name: string): string {
  return name.split("/")[0];
}

/** Depth of a dataset within its pool — used to indent child datasets. */
export function datasetDepth(name: string): number {
  return name.split("/").length - 1;
}

/**
 * useTruenasInstances is the module's shared instance list — the landing, the
 * module gate, and every tab read the same query key so they never disagree.
 */
export function useTruenasInstances() {
  return useQuery({
    queryKey: ["truenas", "instances"],
    queryFn: async () => unwrap(await api.GET("/truenas/instances")),
    refetchInterval: 30_000,
  });
}

/** useTruenasOverview loads pools + disks (already RBAC-filtered). */
export function useTruenasOverview(instanceId: string) {
  return useQuery({
    queryKey: ["truenas", instanceId, "overview"],
    queryFn: async () =>
      unwrap(await api.GET("/truenas/{id}/overview", { params: { path: { id: instanceId } } })),
    refetchInterval: 30_000,
  });
}

/** useTruenasDatasets lists datasets/zvols across visible pools. */
export function useTruenasDatasets(instanceId: string) {
  return useQuery({
    queryKey: ["truenas", instanceId, "datasets"],
    queryFn: async () =>
      unwrap(await api.GET("/truenas/{id}/datasets", { params: { path: { id: instanceId } } })),
    staleTime: 30_000,
  });
}

/** useTruenasShares lists SMB/NFS/iSCSI shares. */
export function useTruenasShares(instanceId: string) {
  return useQuery({
    queryKey: ["truenas", instanceId, "shares"],
    queryFn: async () =>
      unwrap(await api.GET("/truenas/{id}/shares", { params: { path: { id: instanceId } } })),
    staleTime: 30_000,
  });
}

/** useTruenasSnapshots lists snapshots newest-first, capped by `limit`. */
export function useTruenasSnapshots(instanceId: string, limit = 200) {
  return useQuery({
    queryKey: ["truenas", instanceId, "snapshots", limit],
    queryFn: async () =>
      unwrap(
        await api.GET("/truenas/{id}/snapshots", {
          params: { path: { id: instanceId }, query: { limit } },
        }),
      ),
    staleTime: 30_000,
  });
}

/** useTruenasAlerts lists the instance's alerts. */
export function useTruenasAlerts(instanceId: string) {
  return useQuery({
    queryKey: ["truenas", instanceId, "alerts"],
    queryFn: async () =>
      unwrap(await api.GET("/truenas/{id}/alerts", { params: { path: { id: instanceId } } })),
    refetchInterval: 60_000,
  });
}
