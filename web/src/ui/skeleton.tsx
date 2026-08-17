import type { HTMLAttributes } from "react";
import { cn } from "@/lib/utils";

/** Skeleton is the shimmer placeholder shown while real data loads. */
export function Skeleton({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      aria-hidden
      className={cn(
        "rounded-md bg-[linear-gradient(100deg,var(--hh-bg-subtle)_40%,var(--hh-surface-hover)_50%,var(--hh-bg-subtle)_60%)] bg-[length:200%_100%]",
        "animate-[hh-shimmer_1.6s_ease-in-out_infinite]",
        className,
      )}
      {...props}
    />
  );
}

/** SkeletonRows renders a stack of line placeholders for tables/lists. */
export function SkeletonRows({ rows = 4, className }: { rows?: number; className?: string }) {
  return (
    <div className={cn("space-y-2.5", className)}>
      {Array.from({ length: rows }, (_, i) => (
        <Skeleton key={i} className="h-9 w-full" style={{ opacity: 1 - i * 0.15 }} />
      ))}
    </div>
  );
}
