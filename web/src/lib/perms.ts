// Client-side mirror of the server's grant evaluation, used ONLY to decide
// what to render. The server re-checks every request; hiding here is
// presentation, never the security boundary.
import type { Grant } from "./api";

export type Level = "view" | "control" | "configure" | "delete";

// System resource paths, kept in sync with internal/api/server.go.
export const RES = {
  users: "system/users",
  roles: "system/roles",
  settings: "system/settings",
  audit: "system/audit",
  health: "system/health",
} as const;

/** matches reports whether a selector covers a resource (node or subtree). */
export function matches(selector: string, resource: string): boolean {
  if (!selector || !resource) return false;
  const sel = selector.split("/");
  const res = resource.split("/");
  if (res.length < sel.length) return false;
  for (let i = 0; i < sel.length; i++) {
    if (sel[i] !== "*" && sel[i] !== res[i]) return false;
  }
  return true;
}

/** evaluate applies deny-by-default rules over a grant set. */
export function evaluate(grants: Grant[], resource: string, level: Level): boolean {
  let allowed = false;
  for (const g of grants) {
    if (!matches(g.selector, resource)) continue;
    if (g.effect === "deny") {
      if (g.levels.includes("view") || g.levels.includes(level)) return false;
    } else if (level === "view" || g.levels.includes(level)) {
      allowed = true;
    }
  }
  return allowed;
}
