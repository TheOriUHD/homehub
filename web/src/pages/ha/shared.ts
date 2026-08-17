import { useEffect } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, unwrap, isApiError, type HaArea, type HaEntity } from "@/lib/api";
import { live } from "@/lib/ws";
import { useToast } from "@/ui/toast";

export type HaEntitiesResponse = { entities: HaEntity[]; areas: HaArea[] };

/** RBAC/metrics resource prefix for an instance, e.g. "homeassistant/<id>". */
export function haResource(instanceId: string): string {
  return `homeassistant/${instanceId}`;
}

/** Metrics resource for one entity, e.g. "homeassistant/<id>/sensor/<objectId>". */
export function haEntityResource(instanceId: string, entity: Pick<HaEntity, "domain" | "objectId">): string {
  return `${haResource(instanceId)}/${entity.domain}/${entity.objectId}`;
}

export const haEntitiesKey = (instanceId: string) => ["ha", instanceId, "entities"] as const;

/**
 * useHaInstances is the module's shared instance list — the module gate and
 * both tabs read the same query key, so they never disagree.
 */
export function useHaInstances() {
  return useQuery({
    queryKey: ["ha", "instances"],
    queryFn: async () => unwrap(await api.GET("/ha/instances")),
    staleTime: 15_000,
  });
}

/** The one entities query per instance; the WS hook patches it in place. */
export function useHaEntities(instanceId: string) {
  return useQuery({
    queryKey: haEntitiesKey(instanceId),
    queryFn: async () =>
      unwrap(await api.GET("/ha/{id}/entities", { params: { path: { id: instanceId } } })),
  });
}

export const haDashboardKey = (instanceId: string) => ["ha", instanceId, "dashboard"] as const;

/**
 * useHaDashboard is the caller's OWN board layout for this instance — the
 * server scopes it per user, so no user id appears in the key. Live entity
 * state never lives here; cards only reference entity ids and resolve them
 * against the entities query, which useHaLiveStates keeps patched.
 */
export function useHaDashboard(instanceId: string) {
  return useQuery({
    queryKey: haDashboardKey(instanceId),
    queryFn: async () =>
      unwrap(await api.GET("/ha/{id}/dashboard", { params: { path: { id: instanceId } } })),
  });
}

/**
 * useHaLiveStates subscribes to the instance topic and patches the TanStack
 * cache directly on every "state" push — no refetch storms on a busy HA.
 * Registry changes ("entities-changed") invalidate the list instead.
 *
 * RBAC: the topic is authorized at instance level, so a "state" event may
 * name an entity the caller cannot view. The entities list is already
 * server-filtered — only entities already present in the cache are patched,
 * never inserted.
 */
export function useHaLiveStates(instanceId: string) {
  const queryClient = useQueryClient();
  useEffect(() => {
    return live.subscribe(`integration:homeassistant/${instanceId}`, (payload) => {
      const frame = payload as { event?: string; data?: unknown } | undefined;
      if (frame?.event === "state" && frame.data) {
        const entity = frame.data as HaEntity;
        queryClient.setQueryData<HaEntitiesResponse>(haEntitiesKey(instanceId), (prev) => {
          if (!prev) return prev;
          const idx = prev.entities.findIndex((e) => e.entityId === entity.entityId);
          if (idx === -1) return prev; // unknown to this user — never insert
          const entities = prev.entities.slice();
          entities[idx] = entity;
          return { ...prev, entities };
        });
        // Patch an open detail query only if it is already cached.
        queryClient.setQueryData<HaEntity>(
          ["ha", instanceId, "entity", entity.entityId],
          (prev) => (prev ? entity : prev),
        );
      } else if (frame?.event === "entities-changed") {
        void queryClient.invalidateQueries({ queryKey: haEntitiesKey(instanceId) });
        void queryClient.invalidateQueries({ queryKey: ["ha", "instances"] });
      }
    });
  }, [instanceId, queryClient]);
}

/**
 * useHaService is the one mutation for entity service calls. No optimistic
 * update — the WS "state" push lands well under a second and refreshes the
 * card; the control just shows a subtle pending state meanwhile. Errors
 * (403 lacking control, 502 upstream) surface the server's message verbatim.
 */
export function useHaService(instanceId: string) {
  const { t } = useTranslation();
  const toast = useToast();
  return useMutation({
    mutationFn: async (input: { entityId: string; service: string; data?: Record<string, unknown> }) =>
      unwrap(
        await api.POST("/ha/{id}/entities/{entityId}/service", {
          params: { path: { id: instanceId, entityId: input.entityId } },
          body: { service: input.service, data: input.data },
        }),
      ),
    onError: (err) =>
      toast("error", t("ha.serviceError"), isApiError(err) ? err.message : t("common.unknownError")),
  });
}

// ---- attribute access ------------------------------------------------------

export function attrNum(entity: HaEntity, key: string): number | undefined {
  const v = entity.attributes[key];
  return typeof v === "number" && Number.isFinite(v) ? v : undefined;
}

export function attrStr(entity: HaEntity, key: string): string | undefined {
  const v = entity.attributes[key];
  return typeof v === "string" ? v : undefined;
}

export function attrStrArr(entity: HaEntity, key: string): string[] {
  const v = entity.attributes[key];
  return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
}

/** Color modes that imply a brightness channel (everything but on/off). */
const BRIGHTNESS_MODES = ["brightness", "color_temp", "hs", "rgb", "rgbw", "rgbww", "xy", "white"];

/** Color modes that mean the light renders an actual color. */
const COLOR_MODES = ["hs", "rgb", "rgbw", "rgbww", "xy"];

export function lightSupportsBrightness(entity: HaEntity): boolean {
  if ("brightness" in entity.attributes) return true;
  return attrStrArr(entity, "supported_color_modes").some((m) => BRIGHTNESS_MODES.includes(m));
}

export function lightSupportsColor(entity: HaEntity): boolean {
  return attrStrArr(entity, "supported_color_modes").some((m) => COLOR_MODES.includes(m));
}

export function lightSupportsColorTemp(entity: HaEntity): boolean {
  return attrStrArr(entity, "supported_color_modes").includes("color_temp");
}

/** States HA reports when it has no usable value. */
export function isUnknownState(state: string): boolean {
  return state === "unknown" || state === "unavailable";
}
