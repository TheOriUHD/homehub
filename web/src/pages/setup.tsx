import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Navigate, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { api, unwrap, isApiError } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { AuthLayout } from "@/components/auth-layout";
import { Button } from "@/ui/button";
import { Input, PasswordInput } from "@/ui/input";
import { Field } from "@/ui/field";
import { PageLoader } from "@/ui/misc";

export function SetupPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { refresh } = useAuth();

  const status = useQuery({
    queryKey: ["setup-status"],
    queryFn: async () => unwrap(await api.GET("/setup/status")),
    staleTime: 0,
  });

  const [setupToken, setSetupToken] = useState("");
  const [username, setUsername] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);

  const submit = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/setup", {
          body: {
            setupToken: setupToken.trim(),
            username,
            displayName,
            password,
          },
        }),
      ),
    onSuccess: async () => {
      await refresh();
      void navigate({ to: "/" });
    },
  });

  if (status.isLoading) {
    return (
      <AuthLayout>
        <PageLoader className="py-10" />
      </AuthLayout>
    );
  }
  if (status.data && !status.data.needsSetup) {
    return <Navigate to="/login" />;
  }

  const tokenError = isApiError(submit.error, "invalid_setup_token")
    ? t("setup.tokenWrong")
    : null;
  const apiError =
    submit.error && isApiError(submit.error) && !tokenError
      ? submit.error.message
      : null;

  return (
    <AuthLayout footer={status.data ? `v${status.data.version}` : undefined}>
      <h1 className="text-fg text-lg font-semibold">{t("setup.welcome")}</h1>
      <p className="text-fg-muted mt-1.5 mb-5 text-[13px]">
        {t("setup.intro")}
      </p>

      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          setLocalError(null);
          if (password !== confirm) {
            setLocalError(t("setup.passwordMismatch"));
            return;
          }
          submit.mutate();
        }}
      >
        <Field
          label={t("setup.token")}
          hint={
            status.data?.setupTokenPath
              ? t("setup.tokenHint", { path: status.data.setupTokenPath })
              : t("setup.tokenHintNoPath")
          }
          error={tokenError}
        >
          {(p) => (
            <Input
              {...p}
              autoFocus
              autoComplete="off"
              spellCheck={false}
              required
              className="font-mono"
              value={setupToken}
              onChange={(e) => setSetupToken(e.target.value)}
            />
          )}
        </Field>
        <Field label={t("setup.username")} hint={t("setup.usernameHint")}>
          {(p) => (
            <Input
              {...p}
              autoComplete="username"
              required
              minLength={3}
              value={username}
              onChange={(e) => setUsername(e.target.value)}
            />
          )}
        </Field>
        <Field label={t("setup.displayName")}>
          {(p) => (
            <Input
              {...p}
              autoComplete="name"
              required
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
            />
          )}
        </Field>
        <Field label={t("setup.password")} hint={t("setup.passwordHint")}>
          {(p) => (
            <PasswordInput
              {...p}
              autoComplete="new-password"
              required
              minLength={12}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          )}
        </Field>
        <Field
          label={t("setup.confirmPassword")}
          error={localError ?? apiError}
        >
          {(p) => (
            <PasswordInput
              {...p}
              autoComplete="new-password"
              required
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
            />
          )}
        </Field>
        <Button
          type="submit"
          variant="primary"
          size="lg"
          className="w-full"
          loading={submit.isPending}
        >
          {submit.isPending ? t("setup.creating") : t("setup.createAccount")}
        </Button>
      </form>
    </AuthLayout>
  );
}
