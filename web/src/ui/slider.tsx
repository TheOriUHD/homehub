import { useState, type CSSProperties } from "react";
import { cn } from "@/lib/utils";

/**
 * Slider is a styled native range input. While dragging it renders the local
 * value; `onValueCommit` fires once when the drag (or keyboard adjustment)
 * ends, so callers can send one request instead of one per tick.
 */
export function Slider({
  value,
  min = 0,
  max = 100,
  step = 1,
  onValueChange,
  onValueCommit,
  disabled,
  className,
  "aria-label": ariaLabel,
}: {
  value: number;
  min?: number;
  max?: number;
  step?: number;
  /** Fires on every tick while dragging. */
  onValueChange?: (value: number) => void;
  /** Fires once when the interaction ends. */
  onValueCommit?: (value: number) => void;
  disabled?: boolean;
  className?: string;
  "aria-label"?: string;
}) {
  // Local value during a drag; null when idle (render the prop).
  const [draft, setDraft] = useState<number | null>(null);
  const shown = draft ?? value;
  const pct = max > min ? ((Math.min(Math.max(shown, min), max) - min) / (max - min)) * 100 : 0;

  const commit = () => {
    if (draft == null) return;
    setDraft(null);
    if (draft !== value) onValueCommit?.(draft);
  };

  return (
    <input
      type="range"
      min={min}
      max={max}
      step={step}
      value={shown}
      disabled={disabled}
      aria-label={ariaLabel}
      onChange={(e) => {
        const v = Number(e.target.value);
        setDraft(v);
        onValueChange?.(v);
      }}
      onPointerUp={commit}
      onKeyUp={(e) => {
        if (e.key !== "Tab") commit();
      }}
      onBlur={commit}
      style={
        {
          background: `linear-gradient(to right, var(--hh-accent) ${pct}%, var(--hh-edge-strong) ${pct}%)`,
        } as CSSProperties
      }
      className={cn(
        "h-1.5 w-full cursor-pointer appearance-none rounded-full transition-opacity outline-none",
        "focus-visible:ring-ring/40 focus-visible:ring-2",
        "disabled:cursor-not-allowed disabled:opacity-50",
        "[&::-webkit-slider-thumb]:bg-surface [&::-webkit-slider-thumb]:border-edge-strong [&::-webkit-slider-thumb]:size-4 [&::-webkit-slider-thumb]:cursor-pointer [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:rounded-full [&::-webkit-slider-thumb]:border [&::-webkit-slider-thumb]:shadow-1",
        "[&::-moz-range-thumb]:bg-surface [&::-moz-range-thumb]:border-edge-strong [&::-moz-range-thumb]:size-3.5 [&::-moz-range-thumb]:cursor-pointer [&::-moz-range-thumb]:rounded-full [&::-moz-range-thumb]:border [&::-moz-range-thumb]:shadow-1",
        className,
      )}
    />
  );
}
