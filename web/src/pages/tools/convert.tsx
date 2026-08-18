import { ArrowLeftRight } from "lucide-react";
import { Link } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";
import { EmptyState } from "@/ui/states";

/** Placeholder until the converter ships. */
export function ToolsConvertPage() {
  return (
    <div className="mx-auto max-w-5xl space-y-5">
      <Link to="/tools" className="text-fg-subtle hover:text-fg inline-flex items-center gap-1.5 text-[13px]">
        <ArrowLeft className="size-3.5" /> Utilities
      </Link>
      <EmptyState icon={ArrowLeftRight} title="Converter" body="Under construction." />
    </div>
  );
}
