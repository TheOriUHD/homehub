import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ArrowLeftRight, FileCode2, Wrench } from "lucide-react";
import { api, unwrap } from "@/lib/api";
import { Card } from "@/ui/card";

/**
 * The utilities hub: small self-contained apps that live inside HomeHub.
 * Each tile is one app; the grid grows as utilities are added. The text
 * editor's workspace files double as assistant context.
 */
export function ToolsPage() {
  const { t } = useTranslation();
  const files = useQuery({
    queryKey: ["files"],
    queryFn: async () => unwrap(await api.GET("/files")),
  });

  return (
    <div className="space-y-5">
      <div className="flex items-center gap-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
          <Wrench className="size-5" />
        </span>
        <div>
          <h1 className="text-fg text-xl font-semibold tracking-tight">{t("tools.title")}</h1>
          <p className="text-fg-muted text-sm">{t("tools.subtitle")}</p>
        </div>
      </div>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <Link to="/tools/editor" className="group">
          <Card className="group-hover:border-accent/50 h-full p-5 transition-colors">
            <span className="bg-accent-soft text-accent-soft-fg flex size-11 items-center justify-center rounded-xl">
              <FileCode2 className="size-5" />
            </span>
            <p className="text-fg mt-3 text-[15px] font-semibold">{t("tools.editor.title")}</p>
            <p className="text-fg-muted mt-1 text-[13px]">{t("tools.editor.tile")}</p>
            <p className="text-fg-subtle mt-3 text-[12px] tabular-nums">
              {t("tools.editor.fileCount", { count: files.data?.length ?? 0 })}
            </p>
          </Card>
        </Link>

        {/* Next up: the universal converter. Ghost tile until it ships. */}
        <Card className="border-dashed p-5 opacity-60">
          <span className="bg-bg-subtle text-fg-subtle flex size-11 items-center justify-center rounded-xl">
            <ArrowLeftRight className="size-5" />
          </span>
          <p className="text-fg mt-3 text-[15px] font-semibold">{t("tools.converter.title")}</p>
          <p className="text-fg-muted mt-1 text-[13px]">{t("tools.converter.tile")}</p>
          <p className="text-fg-subtle mt-3 text-[12px]">{t("tools.comingSoon")}</p>
        </Card>
      </div>
    </div>
  );
}
