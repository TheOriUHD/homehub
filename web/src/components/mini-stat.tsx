import type { LucideIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import { Card } from "@/ui/card";

/**
 * MiniStat is the compact dashboard tile shared by the module landing pages:
 * icon, uppercase label, big value, optional sub-text and severity tone.
 */
export function MiniStat({
  icon: Icon,
  label,
  value,
  sub,
  tone,
}: {
  icon: LucideIcon;
  label: string;
  value: string | number;
  sub?: string;
  tone?: "danger" | "success" | "warning";
}) {
  return (
    <Card className="p-4">
      <div className="flex items-center gap-2">
        <Icon
          className={cn(
            "size-4",
            tone === "danger" && "text-danger",
            tone === "success" && "text-success",
            tone === "warning" && "text-warning",
            !tone && "text-fg-subtle",
          )}
          aria-hidden
        />
        <p className="text-fg-subtle truncate text-[11px] font-semibold tracking-wide uppercase">{label}</p>
      </div>
      <p className="text-fg mt-1.5 truncate text-xl font-semibold tracking-tight tabular-nums">
        {value}
        {sub && <span className="text-fg-subtle ml-1.5 text-[13px] font-normal">{sub}</span>}
      </p>
    </Card>
  );
}
