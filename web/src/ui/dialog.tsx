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

// ---- reference-counted background scroll lock --------------------------------
let scrollLocks = 0;

/** scrollTargets are every element a lock applies to: the app's scroll
 * container (the shell's <main>) plus body, so it works on standalone pages
 * that have no shell. */
function scrollTargets(): HTMLElement[] {
  const out: HTMLElement[] = [document.body];
  const main = document.querySelector<HTMLElement>("[data-app-scroll]");
  if (main) out.push(main);
  return out;
}

/**
 * ScrollLock freezes the background for as long as it is mounted. It lives
 * INSIDE the portal on purpose: DialogContent itself mounts even while the
 * dialog is closed (only the portal's children are gated on open), so locking
 * from DialogContent's own effect froze every page that merely renders a
 * closed dialog — permanently, and surviving reloads.
 */
function ScrollLock() {
  useEffect(() => {
    lockBodyScroll();
    return releaseBodyScroll;
  }, []);
  return null;
}

function lockBodyScroll() {
  if (scrollLocks === 0) {
    for (const el of scrollTargets()) el.style.overflow = "hidden";
  }
  scrollLocks++;
}

function releaseBodyScroll() {
  scrollLocks = Math.max(0, scrollLocks - 1);
  if (scrollLocks === 0) unlockBodyScrollHard();
}

/** unlockBodyScrollHard is the escape hatch: whatever went wrong, the app
 * must never be left unscrollable. Also called on every route change.
 * Clearing the inline value restores the stylesheet's own rule rather than
 * a snapshot, so overlapping dialogs can't strand a stale value. */
export function unlockBodyScrollHard() {
  scrollLocks = 0;
  document.body.style.overflow = "";
  const main = document.querySelector<HTMLElement>("[data-app-scroll]");
  if (main) main.style.overflow = "";
}

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
  icon,
  centered,
  children,
  footer,
  wide,
  className,
}: {
  title: string;
  description?: ReactNode;
  /** A badge above the title. Only rendered in the centered layout. */
  icon?: ReactNode;
  /**
   * Centered layout: icon, title and description stack down the middle and the
   * footer buttons share the width evenly. For short, high-stakes decisions
   * (confirm a cost, confirm a deletion) where the whole dialog is the
   * message; ordinary form dialogs keep the default left-aligned header.
   */
  centered?: boolean;
  children?: ReactNode;
  footer?: ReactNode;
  wide?: boolean;
  className?: string;
}) {
  const { t } = useTranslation();
  const close = useContext(DialogCloseContext);

  return (
    <DialogPrimitive.Portal>
      <ScrollLock />
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
        {centered ? (
          <>
            {/* Out of the flow so the title stays optically centred. */}
            <DialogPrimitive.Close
              aria-label={t("common.close")}
              className="interactive-muted absolute top-3 right-3 flex size-7 cursor-pointer items-center justify-center"
            >
              <X className="size-4" />
            </DialogPrimitive.Close>
            <div className="flex flex-col items-center gap-2 px-6 pt-7 text-center">
              {icon}
              <DialogPrimitive.Title className="text-fg text-base font-semibold">
                {title}
              </DialogPrimitive.Title>
              {description && (
                <DialogPrimitive.Description className="text-fg-muted max-w-xs text-[13px] leading-relaxed">
                  {description}
                </DialogPrimitive.Description>
              )}
            </div>
          </>
        ) : (
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
        )}
        {children && <div className={cn("px-5 py-4", centered && "px-6 pt-4 pb-0")}>{children}</div>}
        {footer && (
          <div
            className={cn(
              "border-edge flex justify-end gap-2 border-t px-5 py-3.5",
              // No divider: the centered layout reads as one card, and the
              // buttons split the width so neither is the obvious default.
              centered && "grid grid-cols-2 border-t-0 px-6 pt-4 pb-5 [&>*]:w-full",
            )}
          >
            {footer}
          </div>
        )}
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  );
}
