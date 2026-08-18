import { useState, type ReactNode } from "react";
import { Check, Copy } from "lucide-react";
import { cn } from "@/lib/utils";
import { Card } from "@/ui/card";

/**
 * Small shared primitives so every utility looks like the same app. Tools
 * compose these with the design system (Input, Field, Button, Select,
 * Switch, useToast) — no tool should hand-roll card chrome or copy buttons.
 */

/** A titled panel. Use one or two per tool to group inputs / outputs. */
export function ToolCard({
  title,
  right,
  children,
  className,
}: {
  title?: ReactNode;
  right?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <Card className={cn("p-4", className)}>
      {(title || right) && (
        <div className="mb-3 flex items-center gap-2">
          {title && <h2 className="text-fg text-[13px] font-semibold">{title}</h2>}
          <span className="flex-1" />
          {right}
        </div>
      )}
      {children}
    </Card>
  );
}

/** Copy-to-clipboard button with a transient check. Works on insecure LAN
 *  origins too (falls back to a textarea + execCommand). */
export function CopyIconButton({ value, label = "Copy", className }: { value: string; label?: string; className?: string }) {
  const [done, setDone] = useState(false);
  const copy = async () => {
    try {
      if (navigator.clipboard?.writeText) await navigator.clipboard.writeText(value);
      else {
        const ta = document.createElement("textarea");
        ta.value = value;
        ta.style.position = "fixed";
        ta.style.opacity = "0";
        document.body.appendChild(ta);
        ta.select();
        document.execCommand("copy");
        document.body.removeChild(ta);
      }
      setDone(true);
      setTimeout(() => setDone(false), 1200);
    } catch {
      /* ignore */
    }
  };
  return (
    <button
      type="button"
      onClick={copy}
      aria-label={label}
      title={label}
      className={cn(
        "text-fg-subtle hover:text-fg hover:bg-bg-subtle inline-flex items-center gap-1 rounded-md p-1.5 transition-colors",
        className,
      )}
    >
      {done ? <Check className="size-3.5 text-emerald-500" /> : <Copy className="size-3.5" />}
    </button>
  );
}

/** A read-only labeled result value with a copy affordance. */
export function ResultRow({ label, value, mono = true }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-center gap-2 py-1">
      <span className="text-fg-subtle w-40 shrink-0 text-[12.5px]">{label}</span>
      <span className={cn("text-fg min-w-0 flex-1 truncate text-[13px]", mono && "font-mono")}>{value || "—"}</span>
      {value && <CopyIconButton value={value} />}
    </div>
  );
}
