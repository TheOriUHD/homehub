import * as DropdownPrimitive from "@radix-ui/react-dropdown-menu";
import type { ComponentPropsWithoutRef } from "react";
import { cn } from "@/lib/utils";

export const Dropdown = DropdownPrimitive.Root;
export const DropdownTrigger = DropdownPrimitive.Trigger;

export function DropdownContent({
  className,
  sideOffset = 6,
  ...props
}: ComponentPropsWithoutRef<typeof DropdownPrimitive.Content>) {
  return (
    <DropdownPrimitive.Portal>
      <DropdownPrimitive.Content
        sideOffset={sideOffset}
        className={cn(
          "bg-overlay border-edge z-50 min-w-44 rounded-lg border p-1 shadow-2",
          "animate-[hh-pop-in_140ms_var(--ease-out-quick)]",
          className,
        )}
        {...props}
      />
    </DropdownPrimitive.Portal>
  );
}

export function DropdownItem({
  className,
  destructive,
  ...props
}: ComponentPropsWithoutRef<typeof DropdownPrimitive.Item> & { destructive?: boolean }) {
  return (
    <DropdownPrimitive.Item
      className={cn(
        "flex cursor-pointer items-center gap-2 rounded-md px-2.5 py-1.5 text-[13px] outline-none select-none",
        destructive
          ? "text-danger data-highlighted:bg-danger-soft"
          : "text-fg data-highlighted:bg-surface-hover",
        "data-disabled:pointer-events-none data-disabled:opacity-50",
        className,
      )}
      {...props}
    />
  );
}

export function DropdownSeparator() {
  return <DropdownPrimitive.Separator className="bg-edge mx-1 my-1 h-px" />;
}

export const DropdownSub = DropdownPrimitive.Sub;

export function DropdownSubTrigger({
  className,
  ...props
}: ComponentPropsWithoutRef<typeof DropdownPrimitive.SubTrigger>) {
  return (
    <DropdownPrimitive.SubTrigger
      className={cn(
        "text-fg flex cursor-pointer items-center gap-2 rounded-md px-2.5 py-1.5 text-[13px] outline-none select-none",
        "data-highlighted:bg-surface-hover data-[state=open]:bg-surface-hover",
        "data-disabled:pointer-events-none data-disabled:opacity-50",
        className,
      )}
      {...props}
    />
  );
}

export function DropdownSubContent({
  className,
  ...props
}: ComponentPropsWithoutRef<typeof DropdownPrimitive.SubContent>) {
  return (
    <DropdownPrimitive.Portal>
      <DropdownPrimitive.SubContent
        sideOffset={4}
        className={cn(
          "bg-overlay border-edge z-50 min-w-44 rounded-lg border p-1 shadow-2",
          "animate-[hh-pop-in_140ms_var(--ease-out-quick)]",
          className,
        )}
        {...props}
      />
    </DropdownPrimitive.Portal>
  );
}

export function DropdownLabel({ className, ...props }: ComponentPropsWithoutRef<typeof DropdownPrimitive.Label>) {
  return (
    <DropdownPrimitive.Label
      className={cn("text-fg-subtle px-2.5 py-1.5 text-xs font-medium", className)}
      {...props}
    />
  );
}
