import { useQuery } from "@tanstack/react-query";
import { api, unwrap, isApiError } from "@/lib/api";

/** RBAC resource for everything on the Backups page. */
export const BACKUPS_RES = "system/backups";

/**
 * The one targets query, shared between the shell's visibility probe and the
 * page so they never fetch twice.
 */
export function backupTargetsQuery() {
  return {
    queryKey: ["backups", "targets"] as const,
    queryFn: async () => unwrap(await api.GET("/backups/targets")),
    staleTime: 30_000,
    retry: (count: number, err: unknown) => !(isApiError(err) && err.status === 403) && count < 2,
  };
}

/**
 * useBackupsVisible probes GET /backups/targets; a 403 means the user can't
 * see backups at all, so navigation hides the entry entirely.
 */
export function useBackupsVisible(): boolean {
  const targets = useQuery(backupTargetsQuery());
  return !(targets.isError && isApiError(targets.error) && targets.error.status === 403);
}
