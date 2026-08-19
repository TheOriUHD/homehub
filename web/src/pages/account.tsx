import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { LanguagePicker } from "@/components/language";
import { startRegistration } from "@simplewebauthn/browser";
import QRCode from "qrcode";
import { Bell, ChevronDown, Fingerprint, Laptop, ShieldCheck, Smartphone } from "lucide-react";
import { api, unwrap, isApiError, type NotificationPrefs, type SessionInfo } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { formatRelative } from "@/lib/utils";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { Button } from "@/ui/button";
import { Input, PasswordInput } from "@/ui/input";
import { Field } from "@/ui/field";
import { Badge } from "@/ui/badge";
import { Dialog, DialogContent } from "@/ui/dialog";
import { ConfirmDialog } from "@/ui/confirm";
import { Checkbox, Secret } from "@/ui/misc";
import { Popover, PopoverContent, PopoverTrigger } from "@/ui/popover";
import { Switch } from "@/ui/switch";
import { ErrorState } from "@/ui/states";
import { SkeletonRows } from "@/ui/skeleton";
import { Tooltip } from "@/ui/tooltip";
import { useToast } from "@/ui/toast";

export function AccountPage() {
  const { t } = useTranslation();
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-fg text-xl font-semibold tracking-tight">{t("account.title")}</h1>
        <p className="text-fg-muted mt-1 text-sm">{t("account.subtitle")}</p>
      </div>
      <div className="grid items-start gap-4 lg:grid-cols-2">
        <div className="space-y-4">
          <LanguageCard />
          <PasswordCard />
          <TotpCard />
          <NotificationPrefsCard />
        </div>
        <div className="space-y-4">
          <PasskeysCard />
          <SessionsCard />
        </div>
      </div>
    </div>
  );
}

// ---- language ---------------------------------------------------------------

function LanguageCard() {
  const { t } = useTranslation();
  return (
    <Card>
      <CardHeader title={t("account.language.title")} description={t("account.language.subtitle")} />
      <CardBody>
        <LanguagePicker />
      </CardBody>
    </Card>
  );
}

// ---- password --------------------------------------------------------------

function PasswordCard() {
  const { t } = useTranslation();
  const toast = useToast();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);

  const change = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/auth/password", { body: { currentPassword: current, newPassword: next } })),
    onSuccess: () => {
      setCurrent("");
      setNext("");
      setConfirm("");
      toast("success", t("account.password"), t("account.passwordIntro"));
    },
  });

  const apiError = change.error && isApiError(change.error) ? change.error.message : null;

  return (
    <Card>
      <CardHeader title={t("account.password")} description={t("account.passwordIntro")} />
      <CardBody>
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
              <PasswordInput {...p} required autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} />
            )}
          </Field>
          <Field label={t("changePassword.new")} hint={t("setup.passwordHint")}>
            {(p) => (
              <PasswordInput {...p} required minLength={12} autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} />
            )}
          </Field>
          <Field label={t("changePassword.confirm")} error={localError ?? apiError}>
            {(p) => (
              <PasswordInput {...p} required autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} />
            )}
          </Field>
          <div className="flex justify-end">
            <Button type="submit" variant="primary" loading={change.isPending}>
              {t("account.changePassword")}
            </Button>
          </div>
        </form>
      </CardBody>
    </Card>
  );
}

// ---- TOTP ------------------------------------------------------------------

function TotpCard() {
  const { t } = useTranslation();
  const toast = useToast();
  const { me, refresh } = useAuth();
  const enabled = me?.user.totpEnabled ?? false;

  const [setupOpen, setSetupOpen] = useState(false);
  const [disableOpen, setDisableOpen] = useState(false);
  const [regenOpen, setRegenOpen] = useState(false);
  const [enrollment, setEnrollment] = useState<{ secret: string; otpauthUrl: string; qr: string } | null>(null);
  const [code, setCode] = useState("");
  const [recoveryCodes, setRecoveryCodes] = useState<string[] | null>(null);
  const [flowError, setFlowError] = useState<string | null>(null);

  const enroll = useMutation({
    mutationFn: async () => unwrap(await api.POST("/auth/totp/enroll")),
    onSuccess: async (data) => {
      const qr = await QRCode.toDataURL(data.otpauthUrl, { margin: 1, width: 180 });
      setEnrollment({ ...data, qr });
      setSetupOpen(true);
      setFlowError(null);
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const confirm = useMutation({
    mutationFn: async () => unwrap(await api.POST("/auth/totp/confirm", { body: { code } })),
    onSuccess: async (data) => {
      setRecoveryCodes(data.recoveryCodes);
      setCode("");
      setFlowError(null);
      await refresh();
    },
    onError: (err) => setFlowError(isApiError(err) ? err.message : t("common.unknownError")),
  });

  const disable = useMutation({
    mutationFn: async (password: string) =>
      unwrap(await api.POST("/auth/totp/disable", { body: { password } })),
    onSuccess: async () => {
      setDisableOpen(false);
      setFlowError(null);
      toast("success", t("account.twoFactor"), t("common.disabled"));
      await refresh();
    },
    onError: (err) => setFlowError(isApiError(err) ? err.message : t("common.unknownError")),
  });

  const regen = useMutation({
    mutationFn: async (password: string) =>
      unwrap(await api.POST("/auth/recovery-codes", { body: { password } })),
    onSuccess: (data) => {
      setRegenOpen(false);
      setFlowError(null);
      setRecoveryCodes(data.recoveryCodes);
      setSetupOpen(true);
    },
    onError: (err) => setFlowError(isApiError(err) ? err.message : t("common.unknownError")),
  });

  const closeSetup = () => {
    setSetupOpen(false);
    setEnrollment(null);
    setRecoveryCodes(null);
    setCode("");
    setFlowError(null);
  };

  return (
    <Card>
      <CardHeader
        title={
          <span className="flex items-center gap-2">
            <ShieldCheck className="text-fg-subtle size-4" />
            {t("account.twoFactor")}
          </span>
        }
        description={enabled ? t("account.totpOn") : t("account.totpOff")}
        actions={
          enabled ? (
            <div className="flex gap-2">
              <Button size="sm" variant="secondary" onClick={() => setRegenOpen(true)}>
                {t("account.regenerate")}
              </Button>
              <Button size="sm" variant="danger-ghost" onClick={() => setDisableOpen(true)}>
                {t("account.disableTotp")}
              </Button>
            </div>
          ) : (
            <Button size="sm" variant="primary" loading={enroll.isPending} onClick={() => enroll.mutate()}>
              {t("account.enableTotp")}
            </Button>
          )
        }
      />
      {enabled && (
        <CardBody>
          <Badge tone="success">{t("common.enabled")}</Badge>
        </CardBody>
      )}

      <Dialog open={setupOpen} onOpenChange={(open) => !open && closeSetup()}>
        {recoveryCodes ? (
          <DialogContent
            title={t("account.totpSetup.recoveryTitle")}
            description={t("account.totpSetup.recoveryIntro")}
            footer={
              <Button variant="primary" onClick={closeSetup}>
                {t("account.totpSetup.downloaded")}
              </Button>
            }
          >
            <div className="grid grid-cols-2 gap-2">
              {recoveryCodes.map((rc) => (
                <code key={rc} className="bg-bg-subtle border-edge rounded-md border px-2.5 py-1.5 text-center font-mono text-[13px] select-all">
                  {rc}
                </code>
              ))}
            </div>
          </DialogContent>
        ) : enrollment ? (
          <DialogContent
            title={t("account.totpSetup.title")}
            description={t("account.totpSetup.scan")}
            footer={
              <>
                <Button variant="ghost" onClick={closeSetup}>
                  {t("common.cancel")}
                </Button>
                <Button variant="primary" loading={confirm.isPending} disabled={code.length < 6} onClick={() => confirm.mutate()}>
                  {t("account.totpSetup.verify")}
                </Button>
              </>
            }
          >
            <div className="space-y-4">
              <div className="flex justify-center">
                <img src={enrollment.qr} alt="TOTP QR code" className="border-edge rounded-lg border bg-white p-2" width={180} height={180} />
              </div>
              <div>
                <p className="text-fg-subtle mb-1 text-xs font-medium uppercase">{t("account.totpSetup.secret")}</p>
                <Secret value={enrollment.secret} />
              </div>
              <Field label={t("account.totpSetup.enterCode")} error={flowError}>
                {(p) => (
                  <Input
                    {...p}
                    autoFocus
                    inputMode="numeric"
                    autoComplete="one-time-code"
                    placeholder="000000"
                    className="text-center font-mono tracking-[0.2em]"
                    value={code}
                    onChange={(e) => setCode(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" && code.length >= 6) confirm.mutate();
                    }}
                  />
                )}
              </Field>
            </div>
          </DialogContent>
        ) : (
          <DialogContent title={t("account.totpSetup.title")} />
        )}
      </Dialog>

      <ConfirmDialog
        open={disableOpen}
        onOpenChange={(open) => {
          setDisableOpen(open);
          if (!open) setFlowError(null);
        }}
        title={t("account.disableTotpConfirm.title")}
        body={t("account.disableTotpConfirm.body")}
        confirmLabel={t("account.disableTotp")}
        requirePassword
        loading={disable.isPending}
        error={flowError}
        onConfirm={(password) => disable.mutate(password)}
      />

      <ConfirmDialog
        open={regenOpen}
        onOpenChange={(open) => {
          setRegenOpen(open);
          if (!open) setFlowError(null);
        }}
        title={t("account.regenerate")}
        body={t("account.regenerateConfirm")}
        confirmLabel={t("account.regenerate")}
        tone="primary"
        requirePassword
        loading={regen.isPending}
        error={flowError}
        onConfirm={(password) => regen.mutate(password)}
      />
    </Card>
  );
}

// ---- passkeys --------------------------------------------------------------

function PasskeysCard() {
  const { t } = useTranslation();
  const toast = useToast();
  const { refresh } = useAuth();
  const queryClient = useQueryClient();

  const passkeys = useQuery({
    queryKey: ["passkeys"],
    queryFn: async () => unwrap(await api.GET("/auth/passkeys")),
  });

  const [nameOpen, setNameOpen] = useState(false);
  const [name, setName] = useState("");
  const [removing, setRemoving] = useState<{ id: string; name: string } | null>(null);

  const register = useMutation({
    mutationFn: async () => {
      const challenge = unwrap(await api.POST("/auth/passkeys/options"));
      const credential = await startRegistration({ optionsJSON: challenge.publicKey as never });
      return unwrap(
        await api.POST("/auth/passkeys", {
          body: { challengeToken: challenge.challengeToken, name, credential: credential as unknown as Record<string, unknown> },
        }),
      );
    },
    onSuccess: async () => {
      setNameOpen(false);
      setName("");
      toast("success", t("account.passkeyAdded"));
      void queryClient.invalidateQueries({ queryKey: ["passkeys"] });
      await refresh();
    },
    onError: (err) => {
      if (err instanceof DOMException && err.name === "NotAllowedError") return;
      toast("error", isApiError(err) ? err.message : t("login.passkeyFailed"));
    },
  });

  const remove = useMutation({
    mutationFn: async (id: string) =>
      unwrap(await api.DELETE("/auth/passkeys/{id}", { params: { path: { id } } })),
    onSuccess: async () => {
      setRemoving(null);
      void queryClient.invalidateQueries({ queryKey: ["passkeys"] });
      await refresh();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <Card>
      <CardHeader
        title={
          <span className="flex items-center gap-2">
            <Fingerprint className="text-fg-subtle size-4" />
            {t("account.passkeysTitle")}
          </span>
        }
        description={
          window.isSecureContext ? t("account.passkeysIntro") : t("account.passkeysInsecureContext")
        }
        actions={
          window.isSecureContext ? (
            <Button size="sm" variant="primary" onClick={() => setNameOpen(true)}>
              {t("account.addPasskey")}
            </Button>
          ) : undefined
        }
      />
      <CardBody className="px-0 py-0">
        {passkeys.isLoading ? (
          <SkeletonRows rows={2} className="p-5" />
        ) : !passkeys.data || passkeys.data.length === 0 ? (
          <p className="text-fg-subtle px-5 py-6 text-center text-[13px]">{t("account.noPasskeys")}</p>
        ) : (
          <ul>
            {passkeys.data.map((pk) => (
              <li key={pk.id} className="border-edge flex items-center gap-3 border-b px-5 py-3 last:border-b-0">
                <Fingerprint className="text-fg-subtle size-4 shrink-0" />
                <div className="min-w-0 flex-1">
                  <p className="text-fg truncate text-sm font-medium">{pk.name}</p>
                  <p className="text-fg-subtle text-xs">
                    {pk.lastUsedAt
                      ? t("account.passkeyLastUsed", { when: formatRelative(pk.lastUsedAt) })
                      : t("account.passkeyNever")}
                  </p>
                </div>
                <Button size="sm" variant="danger-ghost" onClick={() => setRemoving({ id: pk.id, name: pk.name })}>
                  {t("account.removePasskey")}
                </Button>
              </li>
            ))}
          </ul>
        )}
      </CardBody>

      <Dialog open={nameOpen} onOpenChange={setNameOpen}>
        <DialogContent
          title={t("account.addPasskey")}
          footer={
            <>
              <Button variant="ghost" onClick={() => setNameOpen(false)}>
                {t("common.cancel")}
              </Button>
              <Button variant="primary" disabled={!name.trim()} loading={register.isPending} onClick={() => register.mutate()}>
                {t("common.continue")}
              </Button>
            </>
          }
        >
          <Field label={t("account.passkeyName")} hint={t("account.passkeyNameHint")}>
            {(p) => (
              <Input
                {...p}
                autoFocus
                value={name}
                onChange={(e) => setName(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && name.trim()) register.mutate();
                }}
              />
            )}
          </Field>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(open) => !open && setRemoving(null)}
        title={t("account.removePasskey")}
        body={removing ? t("account.removePasskeyConfirm", { name: removing.name }) : ""}
        confirmLabel={t("account.removePasskey")}
        loading={remove.isPending}
        onConfirm={() => removing && remove.mutate(removing.id)}
      />
    </Card>
  );
}

// ---- notification preferences ----------------------------------------------

/**
 * NotificationPrefsCard lets the user choose, per event type, whether it lands
 * in-app and through which outbound channels. Saves are optimistic — the UI
 * flips immediately and rolls back with a toast if the server disagrees.
 */
function NotificationPrefsCard() {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();

  const prefs = useQuery({
    queryKey: ["notification-prefs"],
    queryFn: async () => unwrap(await api.GET("/notification-prefs")),
  });

  const put = useMutation({
    mutationFn: async (body: { eventType: string; inapp: boolean; channelIds: string[] }) =>
      unwrap(await api.PUT("/notification-prefs", { body })),
    onMutate: async (body) => {
      await queryClient.cancelQueries({ queryKey: ["notification-prefs"] });
      const prev = queryClient.getQueryData<NotificationPrefs>(["notification-prefs"]);
      queryClient.setQueryData<NotificationPrefs>(["notification-prefs"], (cur) =>
        cur
          ? { ...cur, prefs: [...cur.prefs.filter((p) => p.eventType !== body.eventType), body] }
          : cur,
      );
      return { prev };
    },
    onError: (err, _body, ctx) => {
      if (ctx?.prev) queryClient.setQueryData(["notification-prefs"], ctx.prev);
      toast("error", isApiError(err) ? err.message : t("common.unknownError"));
    },
    onSettled: () => void queryClient.invalidateQueries({ queryKey: ["notification-prefs"] }),
  });

  const data = prefs.data;

  return (
    <Card>
      <CardHeader
        title={
          <span className="flex items-center gap-2">
            <Bell className="text-fg-subtle size-4" />
            {t("notifPrefs.title")}
          </span>
        }
        description={t("notifPrefs.intro")}
      />
      <CardBody className="px-0 py-0">
        {prefs.isLoading ? (
          <SkeletonRows rows={3} className="p-5" />
        ) : prefs.isError ? (
          <ErrorState error={prefs.error} onRetry={() => void prefs.refetch()} />
        ) : (
          <ul>
            {(data?.eventTypes ?? []).map((eventType) => {
              const pref = data?.prefs.find((p) => p.eventType === eventType) ?? {
                eventType,
                inapp: true,
                channelIds: [],
              };
              const channels = data?.channels ?? [];
              return (
                <li
                  key={eventType}
                  className="border-edge flex items-center gap-3 border-b px-5 py-3 last:border-b-0"
                >
                  <p className="text-fg min-w-0 flex-1 text-sm">
                    {t(`notifPrefs.events.${eventType}`, { defaultValue: eventType })}
                  </p>
                  {channels.length > 0 && (
                    <Popover>
                      <PopoverTrigger asChild>
                        <Button size="sm" variant="ghost">
                          {pref.channelIds.length > 0
                            ? t("notifPrefs.channelCount", { count: pref.channelIds.length })
                            : t("notifPrefs.channels")}
                          <ChevronDown className="size-3.5" />
                        </Button>
                      </PopoverTrigger>
                      <PopoverContent className="space-y-2.5">
                        <p className="text-fg-subtle text-xs font-semibold uppercase">
                          {t("notifPrefs.channels")}
                        </p>
                        {channels.map((channel) => (
                          <Checkbox
                            key={channel.id}
                            checked={pref.channelIds.includes(channel.id)}
                            onCheckedChange={(checked) => {
                              const channelIds = checked
                                ? [...pref.channelIds, channel.id]
                                : pref.channelIds.filter((id) => id !== channel.id);
                              put.mutate({ eventType, inapp: pref.inapp, channelIds });
                            }}
                            label={channel.name}
                            hint={t(`channels.types.${channel.type}`, { defaultValue: channel.type })}
                          />
                        ))}
                      </PopoverContent>
                    </Popover>
                  )}
                  <Tooltip label={t("notifPrefs.inapp")}>
                    <Switch
                      checked={pref.inapp}
                      aria-label={t("notifPrefs.inappAria", {
                        event: t(`notifPrefs.events.${eventType}`, { defaultValue: eventType }),
                      })}
                      onCheckedChange={(inapp) =>
                        put.mutate({ eventType, inapp, channelIds: pref.channelIds })
                      }
                    />
                  </Tooltip>
                </li>
              );
            })}
          </ul>
        )}
      </CardBody>
    </Card>
  );
}

// ---- sessions --------------------------------------------------------------

function SessionsCard() {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();

  const sessions = useQuery({
    queryKey: ["sessions"],
    queryFn: async () => unwrap(await api.GET("/auth/sessions")),
  });

  const revoke = useMutation({
    mutationFn: async (id: string) =>
      unwrap(await api.DELETE("/auth/sessions/{id}", { params: { path: { id } } })),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["sessions"] }),
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const revokeOthers = useMutation({
    mutationFn: async () => unwrap(await api.POST("/auth/sessions/revoke-others")),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["sessions"] }),
  });

  return (
    <Card>
      <CardHeader
        title={t("account.sessions")}
        description={t("account.sessionsIntro")}
        actions={
          (sessions.data?.length ?? 0) > 1 ? (
            <Button size="sm" variant="secondary" loading={revokeOthers.isPending} onClick={() => revokeOthers.mutate()}>
              {t("account.revokeOthers")}
            </Button>
          ) : undefined
        }
      />
      <CardBody className="px-0 py-0">
        {sessions.isLoading ? (
          <SkeletonRows rows={2} className="p-5" />
        ) : (
          <ul>
            {(sessions.data ?? []).map((sess) => (
              <SessionRow key={sess.id} session={sess} onRevoke={() => revoke.mutate(sess.id)} revoking={revoke.isPending} />
            ))}
          </ul>
        )}
      </CardBody>
    </Card>
  );
}

function SessionRow({
  session,
  onRevoke,
  revoking,
}: {
  session: SessionInfo;
  onRevoke: () => void;
  revoking: boolean;
}) {
  const { t } = useTranslation();
  const isMobile = /mobile|android|iphone|ipad/i.test(session.userAgent);
  const Icon = isMobile ? Smartphone : Laptop;
  return (
    <li className="border-edge flex items-center gap-3 border-b px-5 py-3 last:border-b-0">
      <Icon className="text-fg-subtle size-4 shrink-0" />
      <div className="min-w-0 flex-1">
        <p className="text-fg flex items-center gap-2 text-sm font-medium">
          {browserLabel(session.userAgent)}
          {session.current && <Badge tone="accent">{t("account.currentSession")}</Badge>}
        </p>
        <p className="text-fg-subtle text-xs">
          {session.ip} · {t("account.sessionSeen", { when: formatRelative(session.lastSeenAt) })}
        </p>
      </div>
      {!session.current && (
        <Button size="sm" variant="danger-ghost" loading={revoking} onClick={onRevoke}>
          {t("account.revoke")}
        </Button>
      )}
    </li>
  );
}

function browserLabel(userAgent: string): string {
  if (/edg\//i.test(userAgent)) return "Edge";
  if (/firefox/i.test(userAgent)) return "Firefox";
  if (/chrome/i.test(userAgent)) return "Chrome";
  if (/safari/i.test(userAgent)) return "Safari";
  if (/curl|wget|python|go-http/i.test(userAgent)) return "API client";
  return userAgent.slice(0, 40) || "Unknown client";
}
