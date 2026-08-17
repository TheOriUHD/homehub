import * as SwitchPrimitive from "@radix-ui/react-switch";
import type { ComponentPropsWithoutRef } from "react";
import { cn } from "@/lib/utils";

export function Switch({ className, ...props }: ComponentPropsWithoutRef<typeof SwitchPrimitive.Root>) {
  return (
    <SwitchPrimitive.Root
      className={cn(
        // aria-checked, not data-state: wrapping a Switch in a Tooltip lets
        // Tooltip.Trigger stamp its own data-state ("closed") over the
        // Switch's, which left checked tracks grey. aria-checked is the
        // Switch's alone.
        "bg-edge-strong aria-checked:bg-accent relative h-5 w-9 shrink-0 cursor-pointer rounded-full transition-colors duration-150",
        "disabled:cursor-not-allowed disabled:opacity-50",
        className,
      )}
      {...props}
    >
      <SwitchPrimitive.Thumb className="bg-surface block size-4 translate-x-0.5 rounded-full shadow-1 transition-transform duration-150 data-[state=checked]:translate-x-[18px]" />
    </SwitchPrimitive.Root>
  );
}
