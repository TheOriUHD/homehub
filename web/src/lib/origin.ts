import { useQuery } from "@tanstack/react-query";
import { api, unwrap } from "./api";
import { useAuth } from "./auth";
import { RES } from "./perms";

/**
 * The address to put in setup snippets another machine will run: the
 * companion's pairing URL (http becomes ws, https becomes wss) and the
 * caddy-reader installer piped into sudo. When this page was opened over
 * plain http but HomeHub has an https external URL configured (Settings),
 * the snippet uses that, so the token exchange and the root-run download
 * travel over TLS. Otherwise it is the address the page is open on.
 */
export function usePreferredOrigin(): string {
  const { can } = useAuth();
  const here = window.location.origin;
  const onHttp = window.location.protocol === "http:";
  const settings = useQuery({
    queryKey: ["settings"],
    queryFn: async () => unwrap(await api.GET("/settings")),
    enabled: onHttp && can(RES.settings),
    retry: false,
  });
  const configured = settings.data?.appUrl ?? "";
  if (onHttp && configured.startsWith("https://")) return configured.replace(/\/+$/, "");
  return here;
}
