import { useState, type HTMLAttributes, type ReactNode } from "react";
import { Check, Copy, Loader2 } from "lucide-react";
import * as CheckboxPrimitive from "@radix-ui/react-checkbox";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";
import { Tooltip } from "./tooltip";

export function Kbd({ className, ...props }: HTMLAttributes<HTMLElement>) {
  return (
    <kbd
      className={cn(
        "bg-bg-subtle border-edge text-fg-subtle inline-flex h-5 min-w-5 items-center justify-center rounded border px-1 font-sans text-[11px] font-medium",
        className,
      )}
      {...props}
    />
  );
}

/** CopyButton copies a value and flashes confirmation. */
export function CopyButton({ value, className }: { value: string; className?: string }) {
  const [copied, setCopied] = useState(false);
  const { t } = useTranslation();
  return (
    <Tooltip label={copied ? t("common.copied") : t("common.copy")}>
      <button
        type="button"
        onClick={async () => {
          await navigator.clipboard.writeText(value);
          setCopied(true);
          window.setTimeout(() => setCopied(false), 1500);
        }}
        className={cn("interactive-muted flex size-7 shrink-0 cursor-pointer items-center justify-center", className)}
        aria-label={t("common.copy")}
      >
        {copied ? <Check className="text-success size-3.5" /> : <Copy className="size-3.5" />}
      </button>
    </Tooltip>
  );
}

/** Secret renders a sensitive one-time value with copy affordance. */
export function Secret({ value }: { value: string }) {
  return (
    <div className="bg-bg-subtle border-edge flex items-center justify-between gap-2 rounded-md border px-3 py-2">
      <code className="text-fg font-mono text-[13px] break-all select-all">{value}</code>
      <CopyButton value={value} />
    </div>
  );
}

export function Checkbox({
  checked,
  onCheckedChange,
  label,
  hint,
  disabled,
}: {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  label: ReactNode;
  hint?: string;
  disabled?: boolean;
}) {
  return (
    <label className={cn("flex cursor-pointer items-start gap-2.5", disabled && "cursor-not-allowed opacity-55")}>
      <CheckboxPrimitive.Root
        checked={checked}
        disabled={disabled}
        onCheckedChange={(v) => onCheckedChange(v === true)}
        className={cn(
          "border-edge-strong bg-surface mt-0.5 flex size-4 shrink-0 cursor-pointer items-center justify-center rounded border transition-colors",
          "data-[state=checked]:bg-accent data-[state=checked]:border-accent",
        )}
      >
        <CheckboxPrimitive.Indicator>
          <Check className="text-accent-fg size-3" strokeWidth={3} />
        </CheckboxPrimitive.Indicator>
      </CheckboxPrimitive.Root>
      <span className="text-sm">
        <span className="text-fg">{label}</span>
        {hint && <span className="text-fg-subtle block text-[13px]">{hint}</span>}
      </span>
    </label>
  );
}

/** PageLoader fills a region while a route's primary query loads. */
export function PageLoader({ className }: { className?: string }) {
  return (
    <div className={cn("flex items-center justify-center py-20", className)}>
      <Loader2 className="text-fg-subtle size-5 animate-spin" />
    </div>
  );
}
