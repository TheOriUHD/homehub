import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useNavigate, useParams } from "@tanstack/react-router";
import { Trans, useTranslation } from "react-i18next";
import { api, unwrap, isApiError } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { AuthLayout } from "@/components/auth-layout";
import { Button } from "@/ui/button";
import { PasswordInput } from "@/ui/input";
import { Field } from "@/ui/field";
import { PageLoader } from "@/ui/misc";

export function InvitePage() {
  const { t } = useTranslation();
  const { token } = useParams({ from: "/invite/$token" });
  const navigate = useNavigate();
  const { refresh } = useAuth();

  const invite = useQuery({
    queryKey: ["invite", token],
    queryFn: async () => unwrap(await api.GET("/invites/{token}", { params: { path: { token } } })),
    retry: false,
  });

  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);

  const accept = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/invites/{token}", {
          params: { path: { token } },
          body: { password },
        }),
      ),
    onSuccess: async () => {
      await refresh();
      void navigate({ to: "/" });
    },
  });

  if (invite.isLoading) {
    return (
      <AuthLayout>
        <PageLoader className="py-10" />
      </AuthLayout>
    );
  }

  if (invite.isError) {
    return (
      <AuthLayout>
        <p role="alert" className="text-fg py-4 text-center text-sm">
          {t("invite.invalid")}
        </p>
      </AuthLayout>
    );
  }

  const apiError = accept.error && isApiError(accept.error) ? accept.error.message : null;

  return (
    <AuthLayout>
      <h1 className="text-fg text-lg font-semibold">{t("invite.title")}</h1>
      <p className="text-fg-muted mt-1.5 mb-5 text-[13px]">
        <Trans
          i18nKey="invite.intro"
          values={{ username: invite.data?.username }}
          components={{ strong: <strong className="text-fg font-medium" /> }}
        />
      </p>
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          setLocalError(null);
          if (password !== confirm) {
            setLocalError(t("changePassword.mismatch"));
            return;
          }
          accept.mutate();
        }}
      >
        <Field label={t("invite.password")} hint={t("setup.passwordHint")}>
          {(p) => (
            <PasswordInput
              {...p}
              autoFocus
              required
              minLength={12}
              autoComplete="new-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          )}
        </Field>
        <Field label={t("setup.confirmPassword")} error={localError ?? apiError}>
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
        <Button type="submit" variant="primary" size="lg" className="w-full" loading={accept.isPending}>
          {accept.isPending ? t("invite.activating") : t("invite.activate")}
        </Button>
      </form>
    </AuthLayout>
  );
}
