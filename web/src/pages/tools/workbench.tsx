import { Link, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ArrowLeftRight, FileCode2, FolderOpen, LayoutGrid } from "lucide-react";
import { cn } from "@/lib/utils";
import { CATEGORY_LABEL, CATEGORY_ORDER, TOOLS } from "./registry";

/**
 * Workbench mode: once any small utility is opened, the tiles fold into
 * this left sidebar (big three apps pinned on top, then the categories)
 * and the utility fills the remaining width. The grid stays the landing
 * experience — the sidebar only exists while you're inside a utility.
 */

const WORKBENCH_KEY = "hh-tools-workbench";

/** Remember that the workbench is active and which utility it shows. */
export function rememberWorkbench(toolId: string) {
  try {
    sessionStorage.setItem(WORKBENCH_KEY, toolId);
  } catch {
    /* private mode */
  }
}

/** Leaving to the grid ends workbench mode. */
export function clearWorkbench() {
  try {
    sessionStorage.removeItem(WORKBENCH_KEY);
  } catch {
    /* private mode */
  }
}

/** Where a big app's back button should land: the workbench utility that
 * launched it, or the grid when it was entered from the grid. */
export function toolsBackTarget(): string {
  try {
    const id = sessionStorage.getItem(WORKBENCH_KEY);
    return id ? `/tools/${id}` : "/tools";
  } catch {
    return "/tools";
  }
}

const BIG_APPS = [
  { to: "/tools/editor", icon: FileCode2, key: "editor" },
  { to: "/tools/files", icon: FolderOpen, key: "files" },
  { to: "/tools/convert", icon: ArrowLeftRight, key: "converter" },
] as const;

export function WorkbenchSidebar({ activeToolId }: { activeToolId: string }) {
  const { t } = useTranslation();
  const navigate = useNavigate();

  return (
    <aside className="border-edge bg-surface flex w-60 shrink-0 flex-col overflow-y-auto rounded-xl border">
      {/* back to the tile grid — ends workbench mode */}
      <button
        type="button"
        onClick={() => {
          clearWorkbench();
          void navigate({ to: "/tools" });
        }}
        className="text-fg-muted hover:text-fg hover:bg-bg-subtle border-edge flex items-center gap-2 border-b px-3.5 py-2.5 text-[13px] font-medium"
      >
        <LayoutGrid className="size-4" />
        {t("tools.workbench.allUtilities")}
      </button>

      {/* the big three, pinned on top */}
      <div className="border-edge space-y-0.5 border-b p-1.5">
        {BIG_APPS.map((app) => (
          <Link
            key={app.key}
            to={app.to}
            className="text-fg-muted hover:text-fg hover:bg-bg-subtle flex items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-[13px]"
          >
            <app.icon className="size-4" />
            {t(`tools.${app.key}.title`)}
          </Link>
        ))}
      </div>

      {/* categories */}
      <div className="flex-1 p-1.5">
        {CATEGORY_ORDER.map((cat) => {
          const tools = TOOLS.filter((x) => x.category === cat);
          if (tools.length === 0) return null;
          return (
            <div key={cat} className="mb-2">
              <p className="text-fg-subtle px-2.5 pt-1.5 pb-1 text-[10.5px] font-semibold tracking-wide uppercase">
                {CATEGORY_LABEL[cat]}
              </p>
              <div className="space-y-0.5">
                {tools.map((tool) => {
                  const active = tool.id === activeToolId;
                  return (
                    <Link
                      key={tool.id}
                      to="/tools/$toolId"
                      params={{ toolId: tool.id }}
                      className={cn(
                        "flex items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-[12.5px] transition-colors",
                        active
                          ? "bg-accent-soft text-accent-soft-fg font-medium"
                          : "text-fg-muted hover:text-fg hover:bg-bg-subtle",
                      )}
                    >
                      <tool.icon className="size-3.5" />
                      <span className="truncate">{tool.title}</span>
                    </Link>
                  );
                })}
              </div>
            </div>
          );
        })}
      </div>
    </aside>
  );
}
