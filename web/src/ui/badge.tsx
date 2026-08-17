import type { HTMLAttributes } from "react";
import { cn } from "@/lib/utils";

type Tone = "neutral" | "accent" | "success" | "warning" | "danger" | "info";

const tones: Record<Tone, string> = {
  neutral: "bg-bg-subtle text-fg-muted border-edge",
  accent: "bg-accent-soft text-accent-soft-fg border-transparent",
  success: "bg-success-soft text-success-soft-fg border-transparent",
  warning: "bg-warning-soft text-warning-soft-fg border-transparent",
  danger: "bg-danger-soft text-danger-soft-fg border-transparent",
  info: "bg-info-soft text-info-soft-fg border-transparent",
};

export function Badge({
  tone = "neutral",
  className,
  ...props
}: HTMLAttributes<HTMLSpanElement> & { tone?: Tone }) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-medium whitespace-nowrap",
        tones[tone],
        className,
      )}
      {...props}
    />
  );
}

/** StatusDot is the tiny live-state indicator used beside names. */
export function StatusDot({ tone, pulse }: { tone: Tone; pulse?: boolean }) {
  const colors: Record<Tone, string> = {
    neutral: "bg-fg-subtle",
    accent: "bg-accent",
    success: "bg-success",
    warning: "bg-warning",
    danger: "bg-danger",
    info: "bg-info",
  };
  return (
    <span className="relative inline-flex size-2">
      {pulse && (
        <span className={cn("absolute inline-flex size-full animate-ping rounded-full opacity-40", colors[tone])} />
      )}
      <span className={cn("relative inline-flex size-2 rounded-full", colors[tone])} />
    </span>
  );
}
