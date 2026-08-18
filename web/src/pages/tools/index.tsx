import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ArrowLeftRight, FileCode2, FolderOpen, Wrench, type LucideIcon } from "lucide-react";
import { api, unwrap } from "@/lib/api";
import { fileLanguage, formatBytes, timeAgo } from "@/lib/file-lang";
import { Card } from "@/ui/card";
import { CATEGORY_LABEL, CATEGORY_ORDER, TOOLS } from "./registry";

/**
 * The utilities hub: the built-in apps (editor, files, converter) plus every
 * self-contained tool from the registry, grouped by category, and the
 * workspace's most recent files for one-click re-entry.
 */
export function ToolsPage() {
  const { t } = useTranslation();
  const files = useQuery({
    queryKey: ["files"],
    queryFn: async () => unwrap(await api.GET("/files")),
  });
  const recent = (files.data ?? []).slice(0, 6);

  return (
    <div className="space-y-7">
      <div className="flex items-center gap-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
          <Wrench className="size-5" />
        </span>
        <div>
          <h1 className="text-fg text-xl font-semibold tracking-tight">{t("tools.title")}</h1>
          <p className="text-fg-muted text-sm">{t("tools.subtitle")}</p>
        </div>
      </div>

      {/* Built-in apps */}
      <section className="space-y-3">
        <h2 className="text-fg-subtle text-[11px] font-semibold uppercase tracking-wide">{t("tools.apps")}</h2>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          <AppTile to="/tools/editor" icon={FileCode2} title={t("tools.editor.title")} desc={t("tools.editor.tile")} search={{ file: undefined }} />
          <AppTile
            to="/tools/files"
            icon={FolderOpen}
            title={t("tools.files.title")}
            desc={t("tools.files.tile")}
            foot={t("tools.editor.fileCount", { count: files.data?.length ?? 0 })}
          />
          <AppTile to="/tools/convert" icon={ArrowLeftRight} title={t("tools.converter.title")} desc={t("tools.converter.tile")} />
        </div>
      </section>

      {/* Registry tools by category */}
      {CATEGORY_ORDER.map((cat) => {
        const tools = TOOLS.filter((x) => x.category === cat);
        if (tools.length === 0) return null;
        return (
          <section key={cat} className="space-y-3">
            <h2 className="text-fg-subtle text-[11px] font-semibold uppercase tracking-wide">{CATEGORY_LABEL[cat]}</h2>
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
              {tools.map((tool) => (
                <ToolTile key={tool.id} id={tool.id} icon={tool.icon} title={tool.title} desc={tool.desc} />
              ))}
            </div>
          </section>
        );
      })}

      {recent.length > 0 && (
        <section className="space-y-2.5">
          <h2 className="text-fg-subtle text-[11px] font-semibold uppercase tracking-wide">{t("tools.recent")}</h2>
          <Card className="divide-edge divide-y">
            {recent.map((f) => (
              <Link
                key={f.id}
                to="/tools/editor"
                search={{ file: f.id }}
                className="hover:bg-bg-subtle/60 flex items-center gap-3 px-4 py-2.5 transition-colors"
              >
                <FileCode2 className="text-fg-subtle size-4 shrink-0" />
                <span className="text-fg min-w-0 flex-1 truncate font-mono text-[13px]">{f.name}</span>
                <span className="text-fg-subtle shrink-0 text-[11.5px]">
                  {fileLanguage(f.name)} · {formatBytes(f.size)} · {timeAgo(f.updatedAt)}
                </span>
              </Link>
            ))}
          </Card>
        </section>
      )}
    </div>
  );
}

function AppTile({
  to,
  search,
  icon: Icon,
  title,
  desc,
  foot,
}: {
  to: string;
  search?: Record<string, unknown>;
  icon: LucideIcon;
  title: string;
  desc: string;
  foot?: string;
}) {
  return (
    <Link to={to} search={search as never} className="group">
      <Card className="group-hover:border-accent/50 h-full p-5 transition-colors">
        <span className="bg-accent-soft text-accent-soft-fg flex size-11 items-center justify-center rounded-xl">
          <Icon className="size-5" />
        </span>
        <p className="text-fg mt-3 text-[15px] font-semibold">{title}</p>
        <p className="text-fg-muted mt-1 text-[13px]">{desc}</p>
        {foot && <p className="text-fg-subtle mt-3 text-[12px] tabular-nums">{foot}</p>}
      </Card>
    </Link>
  );
}

function ToolTile({ id, icon: Icon, title, desc }: { id: string; icon: LucideIcon; title: string; desc: string }) {
  return (
    <Link to="/tools/$toolId" params={{ toolId: id }} className="group">
      <Card className="group-hover:border-accent/50 flex h-full flex-col p-4 transition-colors">
        <span className="bg-bg-subtle text-fg-muted group-hover:bg-accent-soft group-hover:text-accent-soft-fg flex size-9 items-center justify-center rounded-lg transition-colors">
          <Icon className="size-4.5" />
        </span>
        <p className="text-fg mt-2.5 text-[13.5px] font-semibold">{title}</p>
        <p className="text-fg-muted mt-0.5 text-[12px] leading-snug">{desc}</p>
      </Card>
    </Link>
  );
}
