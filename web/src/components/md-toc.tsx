import { useEffect, useRef, useState } from "react";
import { AlignLeft } from "lucide-react";
import type { TocEntry } from "@/components/markdown-doc";
import { cn } from "@/lib/utils";

/**
 * Scroll tracking for a rendered markdown document's table of contents.
 *
 * Deliberately NOT IntersectionObserver-based: the doc renders inside a lazy
 * chunk (headings don't exist when the effect first runs), and observers only
 * report band crossings — scrolling long sections leaves the highlight stale.
 * Instead a capture-phase scroll listener recomputes "last heading above the
 * reading line" on every scroll tick, catching window scrolling and inner
 * scroll containers alike; a short retry loop covers the lazy mount.
 */
export function useScrollSpy(toc: TocEntry[]): string | null {
  const [active, setActive] = useState<string | null>(null);
  const raf = useRef(0);

  useEffect(() => {
    if (toc.length === 0) {
      setActive(null);
      return;
    }
    const compute = () => {
      raf.current = 0;
      let current: string | null = null;
      for (const h of toc) {
        const el = document.getElementById(h.id);
        if (!el) continue;
        // 140px reading line: matches scroll-mt-20 plus breathing room.
        if (el.getBoundingClientRect().top <= 140) current = h.id;
      }
      // Above the first heading nothing has crossed the line — the first
      // entry is the honest highlight.
      setActive(current ?? toc[0].id);
    };
    const onScroll = () => {
      if (!raf.current) raf.current = requestAnimationFrame(compute);
    };
    // The lazy doc chunk may not have mounted yet — retry until headings exist.
    let cancelled = false;
    let tries = 0;
    const prime = () => {
      if (cancelled) return;
      if (!document.getElementById(toc[0].id) && tries++ < 40) {
        setTimeout(prime, 150);
        return;
      }
      compute();
    };
    prime();
    window.addEventListener("scroll", onScroll, { passive: true, capture: true });
    window.addEventListener("resize", onScroll);
    return () => {
      cancelled = true;
      if (raf.current) cancelAnimationFrame(raf.current);
      window.removeEventListener("scroll", onScroll, { capture: true } as EventListenerOptions);
      window.removeEventListener("resize", onScroll);
    };
  }, [toc]);

  return active;
}

/** Jump to a heading. The router swallows native #hash anchor behavior, so
 * scroll explicitly and record the hash without a navigation. */
export function scrollToHeading(id: string) {
  const el = document.getElementById(id);
  if (!el) return;
  el.scrollIntoView({ behavior: "smooth", block: "start" });
  history.replaceState(history.state, "", `#${id}`);
}

export function TocNav({
  toc,
  active,
  title,
  className,
}: {
  toc: TocEntry[];
  active: string | null;
  title: string;
  className?: string;
}) {
  return (
    <nav className={className}>
      <p className="text-fg-subtle mb-2 flex items-center gap-1.5 text-[11px] font-semibold tracking-wide uppercase">
        <AlignLeft className="size-3.5" />
        {title}
      </p>
      <div className="space-y-0.5">
        {toc.map((h) => (
          <a
            key={h.id}
            href={`#${h.id}`}
            onClick={(e) => {
              e.preventDefault();
              scrollToHeading(h.id);
            }}
            className={cn(
              "block truncate rounded px-2 py-1 text-[12.5px] transition-colors",
              h.level === 2 && "pl-4",
              h.level === 3 && "pl-6",
              active === h.id
                ? "bg-accent-soft text-accent-soft-fg"
                : "text-fg-muted hover:text-fg hover:bg-bg-subtle",
            )}
          >
            {h.text}
          </a>
        ))}
      </div>
    </nav>
  );
}
