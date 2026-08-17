import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Navigate, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { api, unwrap, isApiError } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { AuthLayout } from "@/components/auth-layout";
import { Button } from "@/ui/button";
import { PasswordInput } from "@/ui/input";
import { Field } from "@/ui/field";

/** Forced password change after a temporary password sign-in. */
export function ChangePasswordPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { me, loading, refresh } = useAuth();

  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);

  const change = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/auth/password", {
          body: { currentPassword: current, newPassword: next },
        }),
      ),
    onSuccess: async () => {
      await refresh();
      void navigate({ to: "/" });
    },
  });

  if (!loading && !me) return <Navigate to="/login" />;
  if (!loading && me && !me.user.mustChangePassword) return <Navigate to="/" />;

  const apiError = change.error && isApiError(change.error) ? change.error.message : null;

  return (
    <AuthLayout>
      <h1 className="text-fg text-lg font-semibold">{t("changePassword.title")}</h1>
      <p className="text-fg-muted mt-1.5 mb-5 text-[13px]">{t("changePassword.intro")}</p>
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          setLocalError(null);
          if (next !== confirm) {
            setLocalError(t("changePassword.mismatch"));
            return;
          }
          change.mutate();
        }}
      >
        <Field label={t("changePassword.current")}>
          {(p) => (
            <PasswordInput
              {...p}
              autoFocus
              required
              autoComplete="current-password"
              value={current}
              onChange={(e) => setCurrent(e.target.value)}
            />
          )}
        </Field>
        <Field label={t("changePassword.new")} hint={t("setup.passwordHint")}>
          {(p) => (
            <PasswordInput
              {...p}
              required
              minLength={12}
              autoComplete="new-password"
              value={next}
              onChange={(e) => setNext(e.target.value)}
            />
          )}
        </Field>
        <Field label={t("changePassword.confirm")} error={localError ?? apiError}>
          {(p) => (
            <PasswordInput
              {...p}
              required
              autoComplete="new-password"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
            />
          )}
        </Field>
        <Button type="submit" variant="primary" size="lg" className="w-full" loading={change.isPending}>
          {change.isPending ? t("changePassword.changing") : t("changePassword.change")}
        </Button>
      </form>
    </AuthLayout>
  );
}
