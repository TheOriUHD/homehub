import { Suspense, useEffect } from "react";
import { Link, useParams } from "@tanstack/react-router";
import { FileQuestion } from "lucide-react";
import { PageLoader } from "@/ui/misc";
import { EmptyState } from "@/ui/states";
import { toolById } from "./registry";
import { rememberWorkbench, WorkbenchSidebar } from "./workbench";

/**
 * A small utility open = workbench mode: the categorized sidebar on the
 * left (big three apps pinned on top), the tool filling the rest of the
 * width. Tools own only their body — the chrome is central, so every
 * utility looks like it belongs to the same app.
 */
export function ToolAppPage() {
  const { toolId } = useParams({ from: "/shell/tools/$toolId" });
  const tool = toolById(toolId);

  // Opening any utility turns the hub into a workbench for the session;
  // big apps entered from the sidebar return here instead of the grid.
  useEffect(() => {
    if (tool) rememberWorkbench(toolId);
  }, [tool, toolId]);

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
    <div className="flex min-h-[calc(100dvh-6.5rem)] items-stretch gap-4">
      <WorkbenchSidebar activeToolId={toolId} />

      <div className="min-w-0 flex-1">
        <div className="mb-5 flex items-start gap-3">
          <span className="bg-accent-soft text-accent-soft-fg flex size-11 shrink-0 items-center justify-center rounded-xl">
            <Icon className="size-5" />
          </span>
          <div className="min-w-0">
            <h1 className="text-fg text-xl font-semibold tracking-tight">{tool.title}</h1>
            <p className="text-fg-muted mt-0.5 text-sm">{tool.desc}</p>
          </div>
        </div>

        <Suspense fallback={<PageLoader className="py-16" />}>
          <Body />
        </Suspense>
      </div>
    </div>
  );
}
