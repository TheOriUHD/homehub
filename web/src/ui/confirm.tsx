import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Dialog, DialogContent } from "./dialog";
import { Button } from "./button";
import { PasswordInput } from "./input";
import { Field } from "./field";

/**
 * ConfirmDialog is the destructive-action gate: explicit title, consequence
 * text, and (optionally) password re-authentication.
 */
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  body,
  confirmLabel,
  tone = "danger",
  requirePassword,
  loading,
  error,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  body: ReactNode;
  confirmLabel: string;
  tone?: "danger" | "primary";
  requirePassword?: boolean;
  loading?: boolean;
  error?: string | null;
  onConfirm: (password: string) => void;
}) {
  const { t } = useTranslation();
  const [password, setPassword] = useState("");

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) setPassword("");
        onOpenChange(next);
      }}
    >
      <DialogContent
        title={title}
        description={body}
        footer={
          <>
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              {t("common.cancel")}
            </Button>
            <Button
              variant={tone === "danger" ? "danger" : "primary"}
              loading={loading}
              disabled={requirePassword && password.length === 0}
              onClick={() => onConfirm(password)}
            >
              {confirmLabel}
            </Button>
          </>
        }
      >
        {requirePassword ? (
          <Field label={t("login.password")} error={error}>
            {(fieldProps) => (
              <PasswordInput
                {...fieldProps}
                autoFocus
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && password) onConfirm(password);
                }}
              />
            )}
          </Field>
        ) : error ? (
          <p role="alert" className="text-danger text-[13px]">{error}</p>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
