import * as DialogPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import { createContext, useCallback, useContext, useEffect } from "react";
import type { ComponentPropsWithoutRef, ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";

/**
 * Dialog runs Radix in NON-modal mode on purpose. A modal dialog sets
 * pointer-events:none on <body> and re-enables it only inside the content, so
 * anything floating above it — the assistant widget — becomes unclickable:
 * the click lands on nothing, and Radix's dismiss layer closes the dialog.
 * Non-modal keeps clicks flowing; DialogContent below re-implements the modal
 * behaviour we actually want (dim, click-away, Escape, scroll lock) while
 * letting the assistant stay usable.
 */
const DialogCloseContext = createContext<(() => void) | null>(null);

export function Dialog({ onOpenChange, ...props }: ComponentPropsWithoutRef<typeof DialogPrimitive.Root>) {
  // Context carries the closer to DialogContent's overlay, so the 70+ call
  // sites keep their existing <Dialog open onOpenChange=...> shape.
  const close = useCallback(() => onOpenChange?.(false), [onOpenChange]);
  return (
    <DialogCloseContext.Provider value={close}>
      <DialogPrimitive.Root {...props} onOpenChange={onOpenChange} modal={false} />
    </DialogCloseContext.Provider>
  );
}
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
  const close = useContext(DialogCloseContext);
  // Non-modal Radix doesn't lock background scroll; do it ourselves so the
  // page behind an open dialog still can't scroll away under it.
  useEffect(() => {
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = prev;
    };
  }, []);
  return (
    <DialogPrimitive.Portal>
      {/* Non-modal Radix doesn't dismiss on overlay clicks, so the overlay
          carries it — click-away still closes, while clicks on the assistant
          widget (which floats above, outside this overlay) pass through to it. */}
      <DialogPrimitive.Overlay
        onClick={() => close?.()}
        className="fixed inset-0 z-40 bg-black/45 backdrop-blur-[2px] animate-[hh-fade-in_150ms_var(--ease-out-quick)]"
      />
      <DialogPrimitive.Content
        // Dismissal is driven by the overlay click above; ignore Radix's own
        // outside-interaction events so a click on the assistant widget (or
        // any other floating surface) can never close the dialog.
        onPointerDownOutside={(e) => e.preventDefault()}
        onInteractOutside={(e) => e.preventDefault()}
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
