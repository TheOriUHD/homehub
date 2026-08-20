import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { api, isApiError, unwrap, type CaddyDnsRecordInput } from "@/lib/api";
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
        void queryClient.invalidateQueries({ queryKey: caddyDnsKey(instanceId) });
      }
    });
  }, [instanceId, queryClient]);
}

// ---- Cloudflare link + DNS ----------------------------------------------------

export const caddyDnsKey = (instanceId: string) => ["caddy", instanceId, "dns"] as const;

/**
 * useCaddyDns reads the linked zone's records (annotated with Caddy routes).
 * A 400 "not_linked" is not an error — it renders the setup card — so it's
 * surfaced as data.
 */
export function useCaddyDns(instanceId: string) {
  return useQuery({
    queryKey: caddyDnsKey(instanceId),
    queryFn: async () => {
      try {
        return { linked: true as const, state: unwrap(await api.GET("/caddy/{id}/dns", { params: { path: { id: instanceId } } })) };
      } catch (err) {
        if (isApiError(err, "not_linked")) return { linked: false as const, state: null };
        throw err;
      }
    },
    refetchInterval: 60_000,
  });
}

export function useCfLink(instanceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: { token?: string; zoneId?: string; recordContent?: string; proxied?: boolean }) =>
      unwrap(await api.POST("/caddy/{id}/cloudflare/link", { params: { path: { id: instanceId } }, body })),
    onSuccess: (res) => {
      if (res.linked) {
        void qc.invalidateQueries({ queryKey: caddyDnsKey(instanceId) });
        // The link lives in the integration's config — refresh the cached row
        // so the systems editor's config-preserving spread stays current.
        void qc.invalidateQueries({ queryKey: ["integrations"] });
      }
    },
  });
}

export function useCfUnlink(instanceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () =>
      unwrap(await api.DELETE("/caddy/{id}/cloudflare/link", { params: { path: { id: instanceId } } })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: caddyDnsKey(instanceId) });
      void qc.invalidateQueries({ queryKey: ["integrations"] });
    },
  });
}

export function useDnsCreate(instanceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: CaddyDnsRecordInput) =>
      unwrap(await api.POST("/caddy/{id}/dns/records", { params: { path: { id: instanceId } }, body })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: caddyDnsKey(instanceId) }),
  });
}

export function useDnsUpdate(instanceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { recordId: string; body: CaddyDnsRecordInput }) =>
      unwrap(
        await api.PUT("/caddy/{id}/dns/records/{recordId}", {
          params: { path: { id: instanceId, recordId: vars.recordId } },
          body: vars.body,
        }),
      ),
    onSuccess: () => void qc.invalidateQueries({ queryKey: caddyDnsKey(instanceId) }),
  });
}

export function useDnsDelete(instanceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (recordId: string) =>
      unwrap(await api.DELETE("/caddy/{id}/dns/records/{recordId}", { params: { path: { id: instanceId, recordId } } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: caddyDnsKey(instanceId) }),
  });
}

export function useCaddyPublish(instanceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: {
      name: string;
      upstreamHost: string;
      upstreamPort?: string;
      upstreamScheme?: string;
      createDns?: boolean;
      dnsContent?: string;
      proxied?: boolean;
    }) => unwrap(await api.POST("/caddy/{id}/publish", { params: { path: { id: instanceId } }, body })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: caddyDnsKey(instanceId) });
      void qc.invalidateQueries({ queryKey: caddySitesKey(instanceId) });
    },
  });
}
