import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Navigate, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { startAuthentication } from "@simplewebauthn/browser";
import { Fingerprint } from "lucide-react";
import { api, unwrap, isApiError } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { AuthLayout } from "@/components/auth-layout";
import { Button } from "@/ui/button";
import { Input, PasswordInput } from "@/ui/input";
import { Field } from "@/ui/field";

type Stage =
  | { kind: "password" }
  | { kind: "totp"; mfaToken: string; methods: string[] }
  | { kind: "recovery"; mfaToken: string; methods: string[] };

export function LoginPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { me, loading, refresh } = useAuth();

  const setup = useQuery({
    queryKey: ["setup-status"],
    queryFn: async () => unwrap(await api.GET("/setup/status")),
    staleTime: 0,
  });

  const [stage, setStage] = useState<Stage>({ kind: "password" });
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [error, setError] = useState<string | null>(null);

  const finish = async () => {
    await refresh();
    void navigate({ to: "/" });
  };

  const login = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/auth/login", { body: { username, password } })),
    onSuccess: async (result) => {
      setError(null);
      if (result.status === "mfa_required" && result.mfaToken) {
        setStage({ kind: "totp", mfaToken: result.mfaToken, methods: result.methods ?? [] });
        return;
      }
      await finish();
    },
    onError: (err) => setError(isApiError(err) ? err.message : t("common.unknownError")),
  });

  const totp = useMutation({
    mutationFn: async (args: { mfaToken: string; code: string }) =>
      unwrap(await api.POST("/auth/login/totp", { body: args })),
    onSuccess: finish,
    onError: (err) => setError(isApiError(err) ? err.message : t("common.unknownError")),
  });

  const recovery = useMutation({
    mutationFn: async (args: { mfaToken: string; code: string }) =>
      unwrap(await api.POST("/auth/login/recovery", { body: args })),
    onSuccess: finish,
    onError: (err) => setError(isApiError(err) ? err.message : t("common.unknownError")),
  });

  const passkeyLogin = useMutation({
    mutationFn: async () => {
      const challenge = unwrap(await api.POST("/auth/login/passkey/options"));
      const credential = await startAuthentication({
        optionsJSON: challenge.publicKey as never,
      });
      return unwrap(
        await api.POST("/auth/login/passkey/verify", {
          body: { challengeToken: challenge.challengeToken, credential: credential as unknown as Record<string, unknown> },
        }),
      );
    },
    onSuccess: finish,
    onError: (err) => {
      // A dismissed browser prompt is not an error worth shouting about.
      if (err instanceof DOMException && err.name === "NotAllowedError") return;
      setError(isApiError(err) ? err.message : t("login.passkeyFailed"));
    },
  });

  const passkeyMfa = useMutation({
    mutationFn: async (mfaToken: string) => {
      const challenge = unwrap(
        await api.POST("/auth/login/mfa-passkey/options", { body: { mfaToken } }),
      );
      const credential = await startAuthentication({
        optionsJSON: challenge.publicKey as never,
      });
      return unwrap(
        await api.POST("/auth/login/mfa-passkey/verify", {
          body: { mfaToken, challengeToken: challenge.challengeToken, credential: credential as unknown as Record<string, unknown> },
        }),
      );
    },
    onSuccess: finish,
    onError: (err) => {
      if (err instanceof DOMException && err.name === "NotAllowedError") return;
      setError(isApiError(err) ? err.message : t("login.passkeyFailed"));
    },
  });

  if (!loading && me) return <Navigate to="/" />;
  if (setup.data?.needsSetup) return <Navigate to="/setup" />;

  if (stage.kind !== "password") {
    const isRecovery = stage.kind === "recovery";
    const pending = totp.isPending || recovery.isPending || passkeyMfa.isPending;
    return (
      <AuthLayout>
        <h1 className="text-fg text-lg font-semibold">
          {isRecovery ? t("login.recoveryTitle") : t("login.mfaTitle")}
        </h1>
        <p className="text-fg-muted mt-1.5 mb-5 text-[13px]">
          {isRecovery ? t("login.recoveryIntro") : t("login.mfaIntro")}
        </p>
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            setError(null);
            if (isRecovery) recovery.mutate({ mfaToken: stage.mfaToken, code });
            else totp.mutate({ mfaToken: stage.mfaToken, code });
          }}
        >
          <Field label={isRecovery ? t("login.recoveryCode") : t("login.mfaCode")} error={error}>
            {(p) => (
              <Input
                {...p}
                autoFocus
                required
                inputMode={isRecovery ? "text" : "numeric"}
                autoComplete="one-time-code"
                placeholder={isRecovery ? "xxxx-xxxx-xxxx-xxxx" : "000000"}
                className="text-center font-mono text-base tracking-[0.2em]"
                value={code}
                onChange={(e) => setCode(e.target.value)}
              />
            )}
          </Field>
          <Button type="submit" variant="primary" size="lg" className="w-full" loading={pending}>
            {pending ? t("login.verifying") : t("login.verify")}
          </Button>
        </form>
        <div className="mt-4 flex flex-col gap-1.5 text-center">
          {stage.methods.includes("passkey") && window.isSecureContext && (
            <button
              className="text-accent cursor-pointer text-[13px] hover:underline"
              onClick={() => passkeyMfa.mutate(stage.mfaToken)}
            >
              {t("login.usePasskeyMfa")}
            </button>
          )}
          {isRecovery ? (
            <button
              className="text-accent cursor-pointer text-[13px] hover:underline"
              onClick={() => {
                setCode("");
                setError(null);
                setStage({ ...stage, kind: "totp" });
              }}
            >
              {t("login.useTotpCode")}
            </button>
          ) : (
            stage.methods.includes("recovery") && (
              <button
                className="text-accent cursor-pointer text-[13px] hover:underline"
                onClick={() => {
                  setCode("");
                  setError(null);
                  setStage({ ...stage, kind: "recovery" });
                }}
              >
                {t("login.useRecoveryCode")}
              </button>
            )
          )}
          <button
            className="text-fg-subtle cursor-pointer text-[13px] hover:underline"
            onClick={() => {
              setStage({ kind: "password" });
              setCode("");
              setError(null);
            }}
          >
            {t("login.startOver")}
          </button>
        </div>
      </AuthLayout>
    );
  }

  return (
    <AuthLayout>
      <h1 className="text-fg mb-5 text-lg font-semibold">{t("login.title")}</h1>
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          setError(null);
          login.mutate();
        }}
      >
        <Field label={t("login.username")}>
          {(p) => (
            <Input
              {...p}
              autoFocus
              required
              autoComplete="username webauthn"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
            />
          )}
        </Field>
        <Field label={t("login.password")} error={error}>
          {(p) => (
            <PasswordInput
              {...p}
              required
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          )}
        </Field>
        <Button type="submit" variant="primary" size="lg" className="w-full" loading={login.isPending}>
          {login.isPending ? t("login.signingIn") : t("login.signIn")}
        </Button>
      </form>

      {window.isSecureContext && (
        <div className="border-edge mt-5 border-t pt-4">
          <Button
            variant="secondary"
            size="md"
            className="w-full"
            loading={passkeyLogin.isPending}
            onClick={() => {
              setError(null);
              passkeyLogin.mutate();
            }}
          >
            <Fingerprint className="size-4" />
            {t("login.usePasskey")}
          </Button>
        </div>
      )}
    </AuthLayout>
  );
}
