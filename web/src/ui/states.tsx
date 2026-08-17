import type { LucideIcon } from "lucide-react";
import { AlertTriangle } from "lucide-react";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";
import { Button } from "./button";
import { isApiError } from "@/lib/api";

/** EmptyState explains why a view has no content and what to do about it. */
export function EmptyState({
  icon: Icon,
  title,
  body,
  action,
  className,
}: {
  icon?: LucideIcon;
  title: string;
  body?: string;
  action?: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex flex-col items-center justify-center px-6 py-12 text-center", className)}>
      {Icon && (
        <div className="bg-bg-subtle text-fg-subtle mb-3 flex size-11 items-center justify-center rounded-xl">
          <Icon className="size-5" />
        </div>
      )}
      <h3 className="text-fg text-sm font-semibold">{title}</h3>
      {body && <p className="text-fg-muted mt-1 max-w-sm text-[13px]">{body}</p>}
      {action && <div className="mt-4">{action}</div>}
    </div>
  );
}

/** ErrorState shows the actual failure and a way forward — never a dead end. */
export function ErrorState({
  error,
  onRetry,
  className,
}: {
  error: unknown;
  onRetry?: () => void;
  className?: string;
}) {
  const { t } = useTranslation();
  const message = isApiError(error) ? error.message : t("common.unknownError");
  return (
    <div className={cn("flex flex-col items-center justify-center px-6 py-12 text-center", className)} role="alert">
      <div className="bg-danger-soft text-danger-soft-fg mb-3 flex size-11 items-center justify-center rounded-xl">
        <AlertTriangle className="size-5" />
      </div>
      <p className="text-fg max-w-md text-sm font-medium">{message}</p>
      {onRetry && (
        <Button variant="secondary" size="sm" className="mt-4" onClick={onRetry}>
          {t("common.retry")}
        </Button>
      )}
    </div>
  );
}
