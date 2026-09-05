import * as TooltipPrimitive from "@radix-ui/react-tooltip";
import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

export const TooltipProvider = TooltipPrimitive.Provider;

export function Tooltip({
  label,
  className,
  side = "top",
  children,
}: {
  label: ReactNode;
  /** Which side of the trigger the bubble opens on. */
  side?: "top" | "bottom" | "left" | "right";
  /** Overrides for the bubble — width caps, wrapping, or a themed surface
   *  instead of the default inverted chip. */
  className?: string;
  children: ReactNode;
}) {
  return (
    <TooltipPrimitive.Root delayDuration={350}>
      <TooltipPrimitive.Trigger asChild>{children}</TooltipPrimitive.Trigger>
      <TooltipPrimitive.Portal>
        <TooltipPrimitive.Content
          side={side}
          sideOffset={6}
          className={cn(
            "bg-fg text-bg z-[70] max-w-72 rounded-md px-2.5 py-1.5 text-xs font-medium shadow-2",
            "animate-[hh-fade-in_120ms_var(--ease-out-quick)]",
            className,
          )}
        >
          {label}
        </TooltipPrimitive.Content>
      </TooltipPrimitive.Portal>
    </TooltipPrimitive.Root>
  );
}
