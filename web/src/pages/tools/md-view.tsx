import { lazy, Suspense, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ArrowLeft, BookOpenText, Code2, ExternalLink, SquarePen } from "lucide-react";
import { api, unwrap } from "@/lib/api";
import { extractHeadings } from "@/components/markdown-doc";
import { TocNav, useScrollSpy } from "@/components/md-toc";
import { cn } from "@/lib/utils";
import { Button } from "@/ui/button";
import { PageLoader } from "@/ui/misc";
import { ErrorState } from "@/ui/states";

const MarkdownDoc = lazy(() => import("@/components/markdown-doc"));

const isMdName = (name: string) => /\.(md|markdown)$/i.test(name);

/**
 * The full-page markdown reader: one workspace file rendered as a document at
 * its own link, with a table of contents that tracks scrolling. This is what
 * the editor's "open full page" and the file manager's .md rows point at —
 * write a doc once, then read and share it as a page.
 */
export function ToolsMdViewPage() {
  const { t } = useTranslation();
  const { fileId } = useParams({ from: "/shell/tools/md/$fileId" });
  const [plain, setPlain] = useState(false);

  const file = useQuery({
    queryKey: ["files", fileId],
    queryFn: async () => unwrap(await api.GET("/files/{id}", { params: { path: { id: fileId } } })),
  });

  const content = file.data?.content ?? "";
  const isMd = file.data ? isMdName(file.data.name) : true;
  const rendered = isMd && !plain;
  const toc = useMemo(() => (rendered ? extractHeadings(content).filter((h) => h.level <= 3) : []), [rendered, content]);
  const activeHeading = useScrollSpy(toc);

  if (file.isLoading) return <PageLoader className="py-20" />;
  if (file.isError || !file.data) {
    return <ErrorState error={file.error ?? new Error("not found")} onRetry={() => void file.refetch()} />;
  }

  return (
    <div className="mx-auto max-w-6xl">
      {/* reader chrome */}
      <div className="mb-5 flex flex-wrap items-center gap-2">
        <Link
          to="/tools/files"
          className="text-fg-subtle hover:text-fg inline-flex items-center gap-1.5 text-[13px]"
        >
          <ArrowLeft className="size-3.5" />
          {t("tools.files.title")}
        </Link>
        <span className="text-edge-strong">/</span>
        <span className="text-fg min-w-0 flex-1 truncate font-mono text-[13px]">{file.data.name}</span>
        {isMd && (
          <div className="border-edge flex overflow-hidden rounded-lg border">
            <ModeButton active={!plain} onClick={() => setPlain(false)} icon={BookOpenText} label={t("tools.mdView.rendered")} />
            <ModeButton active={plain} onClick={() => setPlain(true)} icon={Code2} label={t("tools.mdView.plain")} />
          </div>
        )}
        <Link to="/tools/editor" search={{ file: fileId }}>
          <Button size="sm" variant="secondary">
            <SquarePen className="size-3.5" />
            {t("tools.files.open")}
          </Button>
        </Link>
        <a href={`/md/${fileId}`} target="_blank" rel="noreferrer" title={t("tools.editor.fullPage")}>
          <Button size="sm" variant="ghost">
            <ExternalLink className="size-3.5" />
          </Button>
        </a>
      </div>

      <div className="flex items-start gap-8">
        {/* the document */}
        <div className="border-edge bg-surface min-w-0 flex-1 rounded-xl border px-8 py-8 sm:px-12 sm:py-10">
          {rendered ? (
            <Suspense fallback={<PageLoader className="py-16" />}>
              <MarkdownDoc content={content} />
            </Suspense>
          ) : (
            <pre className="text-fg overflow-x-auto font-mono text-[13px] leading-relaxed whitespace-pre-wrap">
              {content}
            </pre>
          )}
        </div>

        {/* table of contents */}
        {rendered && toc.length > 1 && (
          <TocNav
            toc={toc}
            active={activeHeading}
            title={t("tools.mdView.contents")}
            className="sticky top-20 hidden w-56 shrink-0 xl:block"
          />
        )}
      </div>
    </div>
  );
}

function ModeButton({
  active,
  onClick,
  icon: Icon,
  label,
}: {
  active: boolean;
  onClick: () => void;
  icon: React.ComponentType<{ className?: string }>;
  label: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex items-center gap-1.5 px-2.5 py-1.5 text-[12.5px] transition-colors",
        active ? "bg-accent-soft text-accent-soft-fg font-medium" : "text-fg-muted hover:text-fg hover:bg-bg-subtle",
      )}
    >
      <Icon className="size-3.5" />
      {label}
    </button>
  );
}
