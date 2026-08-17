import * as DialogPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";

export const Dialog = DialogPrimitive.Root;
export const DialogTrigger = DialogPrimitive.Trigger;
export const DialogClose = DialogPrimitive.Close;

export function DialogContent({
  title,
  description,
  children,
  footer,
  wide,
  className,
}: {
  title: string;
  description?: ReactNode;
  children?: ReactNode;
  footer?: ReactNode;
  wide?: boolean;
  className?: string;
}) {
  const { t } = useTranslation();
  return (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay className="fixed inset-0 z-40 bg-black/45 backdrop-blur-[2px] animate-[hh-fade-in_150ms_var(--ease-out-quick)]" />
      <DialogPrimitive.Content
        className={cn(
          "bg-overlay border-edge fixed top-1/2 left-1/2 z-50 w-[calc(100vw-2rem)] -translate-x-1/2 -translate-y-1/2 rounded-xl border shadow-3",
          "animate-[hh-pop-in_170ms_var(--ease-out-quick)] focus:outline-none",
          "max-h-[85vh] overflow-y-auto",
          wide ? "max-w-2xl" : "max-w-md",
          className,
        )}
      >
        <div className="flex items-start justify-between gap-4 px-5 pt-4 pb-1">
          <div>
            <DialogPrimitive.Title className="text-fg text-[15px] font-semibold">
              {title}
            </DialogPrimitive.Title>
            {description && (
              <DialogPrimitive.Description className="text-fg-muted mt-1 text-[13px]">
                {description}
              </DialogPrimitive.Description>
            )}
          </div>
          <DialogPrimitive.Close
            aria-label={t("common.close")}
            className="interactive-muted -mr-1 flex size-7 shrink-0 cursor-pointer items-center justify-center"
          >
            <X className="size-4" />
          </DialogPrimitive.Close>
        </div>
        {children && <div className="px-5 py-4">{children}</div>}
        {footer && (
          <div className="border-edge flex justify-end gap-2 border-t px-5 py-3.5">{footer}</div>
        )}
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  );
}
