import { useId, type ReactNode } from "react";
import { cn } from "@/lib/utils";

/**
 * Field wires a label, control, hint, and error message together with the
 * right ARIA links. Pass a render-prop so the control receives id + aria
 * attributes.
 */
export function Field({
  label,
  hint,
  error,
  optional,
  className,
  children,
}: {
  label: string;
  hint?: string;
  error?: string | null;
  optional?: string;
  className?: string;
  children: (props: {
    id: string;
    "aria-invalid": boolean | undefined;
    "aria-describedby": string | undefined;
  }) => ReactNode;
}) {
  const id = useId();
  const describedBy = error ? `${id}-error` : hint ? `${id}-hint` : undefined;
  return (
    <div className={cn("space-y-1.5", className)}>
      <label htmlFor={id} className="text-fg flex items-baseline gap-2 text-[13px] font-medium">
        {label}
        {optional && <span className="text-fg-subtle text-xs font-normal">{optional}</span>}
      </label>
      {children({ id, "aria-invalid": error ? true : undefined, "aria-describedby": describedBy })}
      {error ? (
        <p id={`${id}-error`} role="alert" className="text-danger text-[13px]">
          {error}
        </p>
      ) : hint ? (
        <p id={`${id}-hint`} className="text-fg-subtle text-[13px]">
          {hint}
        </p>
      ) : null}
    </div>
  );
}
