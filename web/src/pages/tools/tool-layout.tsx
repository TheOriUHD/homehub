import { Suspense } from "react";
import { Link, useParams } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";
import { PageLoader } from "@/ui/misc";
import { EmptyState } from "@/ui/states";
import { FileQuestion } from "lucide-react";
import { toolById } from "./registry";

/**
 * The uniform chrome every client-side utility renders inside: a back link,
 * the tool's icon/title/description from the registry, then the tool body.
 * Tools own only their body — the header is central, so every utility looks
 * like it belongs to the same app.
 */
export function ToolAppPage() {
  const { toolId } = useParams({ from: "/shell/tools/$toolId" });
  const tool = toolById(toolId);

  if (!tool) {
    return (
      <EmptyState
        icon={FileQuestion}
        title="Unknown tool"
        body="This utility doesn't exist."
        action={
          <Link to="/tools" className="text-accent text-sm hover:underline">
            Back to Utilities
          </Link>
        }
      />
    );
  }

  const Icon = tool.icon;
  const Body = tool.Component;

  return (
    <div className="mx-auto max-w-5xl space-y-5">
      <div>
        <Link
          to="/tools"
          className="text-fg-subtle hover:text-fg mb-3 inline-flex items-center gap-1.5 text-[13px]"
        >
          <ArrowLeft className="size-3.5" />
          Utilities
        </Link>
        <div className="flex items-start gap-3">
          <span className="bg-accent-soft text-accent-soft-fg flex size-11 shrink-0 items-center justify-center rounded-xl">
            <Icon className="size-5" />
          </span>
          <div className="min-w-0">
            <h1 className="text-fg text-xl font-semibold tracking-tight">{tool.title}</h1>
            <p className="text-fg-muted mt-0.5 text-sm">{tool.desc}</p>
          </div>
        </div>
      </div>

      <Suspense fallback={<PageLoader className="py-16" />}>
        <Body />
      </Suspense>
    </div>
  );
}
