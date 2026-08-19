import type { ReactNode } from "react";
import { Link, useNavigate, useRouterState } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ArrowLeftRight, FileCode2, FolderOpen, LayoutGrid } from "lucide-react";
import { cn } from "@/lib/utils";
import { CATEGORY_LABEL, CATEGORY_ORDER, TOOLS } from "./registry";

/**
 * The Utilities workbench: a persistent left sidebar (the big three apps
 * pinned on top, then the categorized tools) beside the open utility. Every
 * page under /tools/* — big apps and small tools alike — renders inside this
 * frame; the grid at /tools is the landing that folds into it. The frame is
 * fixed to the viewport height: the sidebar scrolls on its own and ONLY the
 * right pane scrolls with content — the page itself never does.
 */

const BIG_APPS = [
  { to: "/tools/editor", seg: "editor", icon: FileCode2, key: "editor" },
  { to: "/tools/files", seg: "files", icon: FolderOpen, key: "files" },
  { to: "/tools/convert", seg: "convert", icon: ArrowLeftRight, key: "converter" },
] as const;

/** The active tool/app id from the path, e.g. /tools/subnet → "subnet". */
function activeSegment(pathname: string): string {
  if (!pathname.startsWith("/tools/")) return "";
  return pathname.slice("/tools/".length).split("/")[0];
}

export function WorkbenchLayout({ children }: { children: ReactNode }) {
  return (
    <div className="flex h-[calc(100dvh-6.5rem)] gap-4 overflow-hidden">
      <WorkbenchSidebar />
      <div className="min-w-0 flex-1 overflow-y-auto">{children}</div>
    </div>
  );
}

function WorkbenchSidebar() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const active = activeSegment(pathname);

  return (
    <aside className="border-edge bg-surface flex h-full w-60 shrink-0 flex-col overflow-y-auto rounded-xl border">
      {/* back to the tile grid */}
      <button
        type="button"
        onClick={() => void navigate({ to: "/tools" })}
        className="text-fg-muted hover:text-fg hover:bg-bg-subtle border-edge flex shrink-0 items-center gap-2 border-b px-3.5 py-2.5 text-[13px] font-medium"
      >
        <LayoutGrid className="size-4" />
        {t("tools.workbench.allUtilities")}
      </button>

      {/* the big three, pinned on top */}
      <div className="border-edge shrink-0 space-y-0.5 border-b p-1.5">
        {BIG_APPS.map((app) => {
          const on = active === app.seg;
          return (
            <Link
              key={app.key}
              to={app.to}
              className={cn(
                "flex items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-[13px] transition-colors",
                on
                  ? "bg-accent-soft text-accent-soft-fg font-medium"
                  : "text-fg-muted hover:text-fg hover:bg-bg-subtle",
              )}
            >
              <app.icon className="size-4" />
              {t(`tools.${app.key}.title`)}
            </Link>
          );
        })}
      </div>

      {/* categories */}
      <div className="p-1.5">
        {CATEGORY_ORDER.map((cat) => {
          const tools = TOOLS.filter((x) => x.category === cat);
          if (tools.length === 0) return null;
          return (
            <div key={cat} className="mb-2">
              <p className="text-fg-subtle px-2.5 pt-1.5 pb-1 text-[10.5px] font-semibold tracking-wide uppercase">
                {CATEGORY_LABEL[cat]}
              </p>
              <div className="space-y-0.5">
                {tools.map((tool) => (
                  <Link
                    key={tool.id}
                    to="/tools/$toolId"
                    params={{ toolId: tool.id }}
                    className={cn(
                      "flex items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-[12.5px] transition-colors",
                      active === tool.id
                        ? "bg-accent-soft text-accent-soft-fg font-medium"
                        : "text-fg-muted hover:text-fg hover:bg-bg-subtle",
                    )}
                  >
                    <tool.icon className="size-3.5" />
                    <span className="truncate">{tool.title}</span>
                  </Link>
                ))}
              </div>
            </div>
          );
        })}
      </div>
    </aside>
  );
}
