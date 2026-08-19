import { Suspense } from "react";
import { Link, useParams } from "@tanstack/react-router";
import { FileQuestion } from "lucide-react";
import { PageLoader } from "@/ui/misc";
import { EmptyState } from "@/ui/states";
import { toolById } from "./registry";
import { WorkbenchLayout } from "./workbench";

/**
 * A small utility rendered inside the workbench frame: the categorized
 * sidebar on the left, this tool filling the (independently scrolling) right
 * pane. Tools own only their body — the header is central, so every utility
 * looks like it belongs to the same app.
 */
export function ToolAppPage() {
  const { toolId } = useParams({ from: "/shell/tools/$toolId" });
  const tool = toolById(toolId);

  if (!tool) {
    return (
      <WorkbenchLayout>
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
      </WorkbenchLayout>
    );
  }

  const Icon = tool.icon;
  const Body = tool.Component;

  return (
    <WorkbenchLayout>
      <div className="mb-5 flex shrink-0 items-start gap-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-11 shrink-0 items-center justify-center rounded-xl">
          <Icon className="size-5" />
        </span>
        <div className="min-w-0">
          <h1 className="text-fg text-xl font-semibold tracking-tight">{tool.title}</h1>
          <p className="text-fg-muted mt-0.5 text-sm">{tool.desc}</p>
        </div>
      </div>

      {/* A fill tool (e.g. the IP scanner) owns its own inner scroll, so this
          area must not scroll; every other tool scrolls here while the page
          stays locked. Either way there is exactly one content scrollbar. */}
      <div
        className={
          tool.fill ? "flex min-h-0 flex-1 flex-col overflow-hidden" : "min-h-0 flex-1 overflow-y-auto"
        }
      >
        <Suspense fallback={<PageLoader className="py-16" />}>
          <Body />
        </Suspense>
      </div>
    </WorkbenchLayout>
  );
}
