import { cn } from "@/lib/utils";

/** The HomeHub mark: a hub node with radiating links, in the accent color. */
export function LogoMark({ className }: { className?: string }) {
  return (
    <div
      className={cn(
        "bg-accent text-accent-fg flex size-8 shrink-0 items-center justify-center rounded-lg shadow-1",
        className,
      )}
      aria-hidden
    >
      <svg viewBox="0 0 24 24" fill="none" className="size-5">
        <circle cx="12" cy="12" r="2.6" fill="currentColor" />
        <circle cx="12" cy="4.5" r="1.7" fill="currentColor" opacity="0.85" />
        <circle cx="19" cy="16" r="1.7" fill="currentColor" opacity="0.85" />
        <circle cx="5" cy="16" r="1.7" fill="currentColor" opacity="0.85" />
        <path
          d="M12 9.4V6.2M13.9 13.4l3.4 1.9M10.1 13.4l-3.4 1.9"
          stroke="currentColor"
          strokeWidth="1.5"
          strokeLinecap="round"
          opacity="0.7"
        />
      </svg>
    </div>
  );
}

export function Wordmark({ className }: { className?: string }) {
  return (
    <span className={cn("text-fg text-[15px] font-semibold tracking-tight", className)}>
      Home<span className="text-accent">Hub</span>
    </span>
  );
}
