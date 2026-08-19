import { memo } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";
import { cn } from "@/lib/utils";
import "./markdown-doc.css";

/**
 * Document-grade markdown rendering: real heading hierarchy, GFM tables and
 * task lists, syntax-highlighted code blocks, and stable heading ids so a
 * table of contents (and the URL hash) can deep-link into sections. This is
 * the reading view — the assistant chat keeps its own compact renderer.
 */

/** GitHub-style slug: lowercase, alphanumerics and hyphens only. */
export function headingSlug(text: string): string {
  return text
    .toLowerCase()
    .trim()
    .replace(/[^\p{L}\p{N}\s-]/gu, "")
    .replace(/\s+/g, "-");
}

export type TocEntry = { level: number; text: string; id: string };

/** Headings from markdown source (outside code fences) for a TOC. */
export function extractHeadings(md: string): TocEntry[] {
  const out: TocEntry[] = [];
  const seen = new Map<string, number>();
  let inFence = false;
  for (const line of md.split("\n")) {
    if (/^\s*(```|~~~)/.test(line)) {
      inFence = !inFence;
      continue;
    }
    if (inFence) continue;
    const m = /^(#{1,4})\s+(.+?)\s*#*\s*$/.exec(line);
    if (!m) continue;
    const text = m[2].replace(/[*_`~[\]]/g, "");
    let id = headingSlug(text);
    const n = seen.get(id) ?? 0;
    seen.set(id, n + 1);
    if (n > 0) id = `${id}-${n}`;
    out.push({ level: m[1].length, text, id });
  }
  return out;
}

function textOf(node: React.ReactNode): string {
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(textOf).join("");
  if (node && typeof node === "object" && "props" in node) {
    return textOf((node as { props: { children?: React.ReactNode } }).props.children);
  }
  return "";
}

export const MarkdownDoc = memo(function MarkdownDoc({
  content,
  className,
}: {
  content: string;
  className?: string;
}) {
  // Duplicate headings get -1/-2 suffixes exactly like extractHeadings, so
  // the TOC ids and the rendered ids always agree. The counter map MUST be
  // fresh per render pass — a memoized map would carry counts into the next
  // render and every heading would silently drift to -1/-2/-3 ids.
  const slugCounts = new Map<string, number>();

  const heading = (Tag: "h1" | "h2" | "h3" | "h4") =>
    function Heading({ children }: { children?: React.ReactNode }) {
      let id = headingSlug(textOf(children));
      const n = slugCounts.get(id) ?? 0;
      slugCounts.set(id, n + 1);
      if (n > 0) id = `${id}-${n}`;
      return (
        <Tag id={id} className="group scroll-mt-20">
          <a
            href={`#${id}`}
            className="no-underline"
            onClick={(e) => {
              // The SPA router swallows native anchor scrolling — do it by hand.
              e.preventDefault();
              document.getElementById(id)?.scrollIntoView({ behavior: "smooth", block: "start" });
              history.replaceState(history.state, "", `#${id}`);
            }}
          >
            {children}
          </a>
        </Tag>
      );
    };

  return (
    <div className={cn("hh-mddoc", className)}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[[rehypeHighlight, { detect: false, ignoreMissing: true }]]}
        components={{
          h1: heading("h1"),
          h2: heading("h2"),
          h3: heading("h3"),
          h4: heading("h4"),
          a: ({ href, children }) => (
            <a
              href={href}
              target={href?.startsWith("#") ? undefined : "_blank"}
              rel="noreferrer"
              onClick={
                href?.startsWith("#")
                  ? (e) => {
                      e.preventDefault();
                      document
                        .getElementById(decodeURIComponent(href.slice(1)))
                        ?.scrollIntoView({ behavior: "smooth", block: "start" });
                      history.replaceState(history.state, "", href);
                    }
                  : undefined
              }
            >
              {children}
            </a>
          ),
          // Bare inline code vs fenced block is decided by the pre wrapper —
          // rehype-highlight has already tokenized block contents.
          input: (props) => <input {...props} disabled className="accent-(--hh-accent)" />,
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
});

export default MarkdownDoc;
