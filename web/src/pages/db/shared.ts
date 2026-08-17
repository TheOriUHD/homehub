import { useQuery } from "@tanstack/react-query";
import { api, unwrap, type DbInstance } from "@/lib/api";

/** RBAC/metrics resource for an instance, e.g. "mysql/<id>" or "mongodb/<id>". */
export function dbResource(instance: Pick<DbInstance, "engine" | "id">): string {
  return `${instance.engine}/${instance.id}`;
}

/** Per-database RBAC resource, e.g. "mysql/<id>/database/<name>". */
export function dbDatabaseResource(
  instance: Pick<DbInstance, "engine" | "id">,
  database: string,
): string {
  return `${dbResource(instance)}/database/${database}`;
}

/** Product names are not translated ("MySQL / MariaDB", "MongoDB"). */
export const engineLabel: Record<DbInstance["engine"], string> = {
  mysql: "MySQL / MariaDB",
  mongodb: "MongoDB",
};

/** shortVersion trims a server version to major.minor, e.g. "11.8.8-MariaDB…" → "11.8". */
export function shortVersion(version: string): string {
  const m = version.match(/^(\d+)(?:\.(\d+))?/);
  if (!m) return version;
  return m[2] !== undefined ? `${m[1]}.${m[2]}` : m[1];
}

/** Engine badge text: flavor + short version when known ("MariaDB 11.8"). */
export function engineBadgeLabel(instance: DbInstance): string {
  const stats = instance.stats;
  const product =
    stats?.flavor || (instance.engine === "mongodb" ? "MongoDB" : engineLabel.mysql);
  return stats?.version ? `${product} ${shortVersion(stats.version)}` : product;
}

/**
 * useDbInstances is the module's shared instance list — the landing, the
 * module gate, and every tab read the same query key so they never disagree.
 */
export function useDbInstances() {
  return useQuery({
    queryKey: ["db", "instances"],
    queryFn: async () => unwrap(await api.GET("/db/instances")),
    refetchInterval: 30_000,
  });
}

/** useDbDatabases lists an instance's databases (already RBAC-filtered). */
export function useDbDatabases(instanceId: string) {
  return useQuery({
    queryKey: ["db", instanceId, "databases"],
    queryFn: async () =>
      unwrap(await api.GET("/db/{id}/databases", { params: { path: { id: instanceId } } })),
    staleTime: 30_000,
  });
}

/** useDbObjectSchema loads one table's or collection's structure. */
export function useDbObjectSchema(instanceId: string, database: string, object: string) {
  return useQuery({
    queryKey: ["db", instanceId, "browse", database, object, "schema"],
    queryFn: async () =>
      unwrap(
        await api.GET("/db/{id}/databases/{database}/objects/{object}/schema", {
          params: { path: { id: instanceId, database, object } },
        }),
      ),
    staleTime: 30_000,
  });
}

/**
 * Data panes (console results, the browse grid, document lists) are a fixed
 * dark surface in BOTH themes — like a terminal, not a themed card —
 * mirroring the Minecraft console's palette.
 */
export const PANE = {
  bg: "#101319",
  chrome: "#161a22",
  edge: "#262b36",
  text: "#ccd2de",
  dim: "#5b6272",
  warn: "#fbbf24",
  error: "#f87171",
};

/** prettyDocument re-indents an extended-JSON document for display. */
export function prettyDocument(doc: string): string {
  try {
    return JSON.stringify(JSON.parse(doc), null, 2);
  } catch {
    return doc;
  }
}
