import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useRouterState } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Expand, Sparkles, X } from "lucide-react";
import { api, unwrap } from "@/lib/api";
import { cn } from "@/lib/utils";
import { AssistantChatPane } from "./assistant-chat";

/**
 * The always-there assistant: a sparkle button floating bottom-right on
 * every page, expanding into a compact support-style chat. Same
 * conversation as the /assistant page (shared query keys), so moving
 * between the two is seamless. Hidden while the assistant is disabled and
 * on the assistant page itself (no point doubling up).
 */
export function AssistantWidget() {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const pathname = useRouterState({ select: (s) => s.location.pathname });

  const state = useQuery({
    queryKey: ["assistant"],
    queryFn: async () => unwrap(await api.GET("/assistant")),
  });

  // Escape closes the panel from anywhere.
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open]);

  if (!state.data?.enabled || pathname.startsWith("/assistant")) return null;

  return (
    <>
      {open && (
        <div
          className={cn(
            "border-edge bg-surface fixed right-4 bottom-4 z-50 flex flex-col overflow-hidden rounded-2xl border shadow-2",
            "h-[min(600px,calc(100dvh-5rem))] w-[min(400px,calc(100vw-2rem))]",
            "origin-bottom-right animate-[hh-pop-in_160ms_var(--ease-out-quick)]",
          )}
          role="dialog"
          aria-label={t("assistant.title")}
        >
          <div className="border-edge flex items-center gap-2.5 border-b px-4 py-3">
            <span className="bg-accent-soft text-accent-soft-fg flex size-7 items-center justify-center rounded-lg">
              <Sparkles className="size-3.5" />
            </span>
            <div className="min-w-0 flex-1">
              <p className="text-fg text-[13.5px] leading-tight font-semibold">{t("assistant.title")}</p>
              <p className="text-fg-subtle text-[11px] leading-tight">{t("assistant.widgetSubtitle")}</p>
            </div>
            <Link
              to="/assistant"
              onClick={() => setOpen(false)}
              aria-label={t("assistant.openFull")}
              className="text-fg-subtle hover:text-fg rounded-md p-1.5 transition-colors"
            >
              <Expand className="size-4" />
            </Link>
            <button
              type="button"
              onClick={() => setOpen(false)}
              aria-label={t("common.close")}
              className="text-fg-subtle hover:text-fg rounded-md p-1.5 transition-colors"
            >
              <X className="size-4" />
            </button>
          </div>
          <div className="flex min-h-0 flex-1 flex-col p-3">
            <AssistantChatPane variant="widget" />
          </div>
        </div>
      )}

      {!open && (
        <button
          type="button"
          onClick={() => setOpen(true)}
          aria-label={t("assistant.openWidget")}
          className={cn(
            "bg-accent text-accent-fg fixed right-4 bottom-4 z-50 flex size-13 cursor-pointer items-center justify-center rounded-full shadow-2",
            "hover:bg-accent-hover transition-all hover:scale-105 active:scale-95",
            "animate-[hh-pop-in_200ms_var(--ease-out-quick)]",
          )}
        >
          <Sparkles className="size-5.5" />
        </button>
      )}
    </>
  );
}
