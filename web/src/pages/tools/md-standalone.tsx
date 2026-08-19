import { lazy, Suspense, useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate, useParams } from "@tanstack/react-router";
import { AlignLeft, Moon, Sun } from "lucide-react";
import { api, isApiError, unwrap } from "@/lib/api";
import { extractHeadings } from "@/components/markdown-doc";
import { cn } from "@/lib/utils";
import { PageLoader } from "@/ui/misc";

const MarkdownDoc = lazy(() => import("@/components/markdown-doc"));

// The page's own theme, independent of the app's setting. It drives the root
// data-theme attribute while this page is mounted (restored on leave), so the
// design tokens flip exactly like the app's own dark mode.
const THEME_KEY = "hh-mdpage-theme";

/**
 * The standalone document page: /md/<fileId> renders one markdown file and
 * NOTHING else — no shell, no menu, no back button. This is the link you hand
 * to someone (auth still applies): they see the document, a floating table of
 * contents, and a small light/dark toggle. That's it.
 */
export function MdStandalonePage() {
  const { fileId } = useParams({ from: "/md/$fileId" });
  const navigate = useNavigate();
  const [dark, setDark] = useState(() => localStorage.getItem(THEME_KEY) === "dark");
  const [activeHeading, setActiveHeading] = useState<string | null>(null);

  // Own the root theme attribute while mounted; put the app's choice back on
  // unmount.
  useEffect(() => {
    const root = document.documentElement;
    const prev = root.getAttribute("data-theme");
    root.setAttribute("data-theme", dark ? "dark" : "light");
    localStorage.setItem(THEME_KEY, dark ? "dark" : "light");
    return () => {
      if (prev) root.setAttribute("data-theme", prev);
      else root.removeAttribute("data-theme");
    };
  }, [dark]);

  const file = useQuery({
    queryKey: ["files", fileId],
    queryFn: async () => unwrap(await api.GET("/files/{id}", { params: { path: { id: fileId } } })),
    retry: false,
  });

  // Unauthenticated readers land on the login page and come back after.
  useEffect(() => {
    if (file.isError && isApiError(file.error) && file.error.status === 401) {
      void navigate({ to: "/login" });
    }
  }, [file.isError, file.error, navigate]);

  const content = file.data?.content ?? "";
  const toc = useMemo(() => extractHeadings(content).filter((h) => h.level <= 3), [content]);

  useEffect(() => {
    if (toc.length === 0) return;
    const els = toc.map((h) => document.getElementById(h.id)).filter((el): el is HTMLElement => el !== null);
    if (els.length === 0) return;
    const io = new IntersectionObserver(
      (entries) => {
        const hit = entries
          .filter((e) => e.isIntersecting)
          .sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)[0];
        if (hit) setActiveHeading(hit.target.id);
      },
      { rootMargin: "-10% 0px -70% 0px" },
    );
    els.forEach((el) => io.observe(el));
    return () => io.disconnect();
  }, [toc, content]);

  return (
    <div className="bg-bg min-h-dvh">
      {/* floating theme toggle — the page's only control */}
      <button
        type="button"
        onClick={() => setDark((d) => !d)}
        className="text-fg-subtle hover:text-fg bg-surface/80 border-edge fixed top-4 right-4 z-10 flex size-9 items-center justify-center rounded-full border shadow-1 backdrop-blur"
        aria-label="Toggle theme"
      >
        {dark ? <Sun className="size-4" /> : <Moon className="size-4" />}
      </button>

      {file.isLoading ? (
        <PageLoader className="py-24" />
      ) : file.isError || !file.data ? (
        <p className="text-fg-muted py-24 text-center text-sm">This document does not exist.</p>
      ) : (
        <div className="mx-auto flex max-w-6xl items-start gap-10 px-5 py-10 sm:px-8 sm:py-14">
          <article className="min-w-0 flex-1">
            <Suspense fallback={<PageLoader className="py-16" />}>
              <MarkdownDoc content={content} className="mx-auto max-w-3xl" />
            </Suspense>
          </article>
          {toc.length > 1 && (
            <nav className="sticky top-10 hidden w-56 shrink-0 lg:block">
              <p className="text-fg-subtle mb-2 flex items-center gap-1.5 text-[11px] font-semibold tracking-wide uppercase">
                <AlignLeft className="size-3.5" />
                Contents
              </p>
              <div className="space-y-0.5">
                {toc.map((h) => (
                  <a
                    key={h.id}
                    href={`#${h.id}`}
                    className={cn(
                      "block truncate rounded px-2 py-1 text-[12.5px] transition-colors",
                      h.level === 2 && "pl-4",
                      h.level === 3 && "pl-6",
                      activeHeading === h.id
                        ? "bg-accent-soft text-accent-soft-fg"
                        : "text-fg-muted hover:text-fg hover:bg-bg-subtle",
                    )}
                  >
                    {h.text}
                  </a>
                ))}
              </div>
            </nav>
          )}
        </div>
      )}
    </div>
  );
}
