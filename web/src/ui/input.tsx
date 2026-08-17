import { forwardRef, useState, type InputHTMLAttributes, type TextareaHTMLAttributes } from "react";
import { Eye, EyeOff } from "lucide-react";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";

const base =
  "w-full h-8.5 rounded-md border border-edge bg-surface px-3 text-sm text-fg " +
  "placeholder:text-fg-subtle shadow-1 transition-colors duration-150 " +
  "hover:border-edge-strong focus:border-accent focus:outline-none focus:ring-2 focus:ring-ring/40 " +
  "disabled:pointer-events-none disabled:opacity-55 " +
  "aria-invalid:border-danger aria-invalid:focus:ring-danger/30";

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(
  ({ className, ...props }, ref) => (
    <input ref={ref} className={cn(base, className)} {...props} />
  ),
);
Input.displayName = "Input";

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaHTMLAttributes<HTMLTextAreaElement>>(
  ({ className, ...props }, ref) => (
    <textarea ref={ref} className={cn(base, "h-auto min-h-20 py-2", className)} {...props} />
  ),
);
Textarea.displayName = "Textarea";

/** PasswordInput adds a reveal toggle; reveal state never leaves the field. */
export const PasswordInput = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(
  ({ className, ...props }, ref) => {
    const [visible, setVisible] = useState(false);
    const { t } = useTranslation();
    return (
      <div className="relative">
        <Input
          ref={ref}
          type={visible ? "text" : "password"}
          className={cn("pr-9", className)}
          {...props}
        />
        <button
          type="button"
          tabIndex={-1}
          aria-label={visible ? t("common.hide") : t("common.show")}
          onClick={() => setVisible((v) => !v)}
          className="text-fg-subtle hover:text-fg absolute inset-y-0 right-0 flex w-9 cursor-pointer items-center justify-center transition-colors"
        >
          {visible ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
        </button>
      </div>
    );
  },
);
PasswordInput.displayName = "PasswordInput";
