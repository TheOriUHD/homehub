import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ArrowLeft, KeyRound, Laptop } from "lucide-react";
import {
  api,
  unwrap,
  isApiError,
  type CreateUserResult,
  type GrantInput,
} from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { RES } from "@/lib/perms";
import { formatDateTime, formatRelative } from "@/lib/utils";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { Button } from "@/ui/button";
import { Badge } from "@/ui/badge";
import { Dialog, DialogContent } from "@/ui/dialog";
import { ConfirmDialog } from "@/ui/confirm";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { Checkbox, PageLoader, Secret } from "@/ui/misc";
import { ErrorState } from "@/ui/states";
import { GrantEditor } from "@/components/grant-editor";
import { useToast } from "@/ui/toast";

export function UserDetailPage() {
  const { t } = useTranslation();
  const { userId } = useParams({ from: "/shell/admin/users/$userId" });
  const { me, can } = useAuth();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const toast = useToast();

  const user = useQuery({
    queryKey: ["users", userId],
    queryFn: async () => unwrap(await api.GET("/users/{id}", { params: { path: { id: userId } } })),
  });
  const roles = useQuery({
    queryKey: ["roles"],
    queryFn: async () => unwrap(await api.GET("/roles")),
  });
  const sessions = useQuery({
    queryKey: ["users", userId, "sessions"],
    queryFn: async () => unwrap(await api.GET("/users/{id}/sessions", { params: { path: { id: userId } } })),
  });

  const canConfigure = can(RES.users, "configure");
  const canDelete = can(RES.users, "delete");
  const isSelf = me?.user.id === userId;

  // Editable state, hydrated from the query.
  const [username, setUsername] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [email, setEmail] = useState("");
  const [roleIds, setRoleIds] = useState<string[]>([]);
  const [overrides, setOverrides] = useState<GrantInput[]>([]);
  useEffect(() => {
    if (user.data) {
      setUsername(user.data.username);
      setDisplayName(user.data.displayName);
      setEmail(user.data.email ?? "");
      setRoleIds(user.data.roleIds);
      setOverrides(user.data.overrides.map((g) => ({ selector: g.selector, levels: [...g.levels], effect: g.effect })));
    }
  }, [user.data]);

  const dirty = useMemo(() => {
    if (!user.data) return false;
    const rolesChanged =
      roleIds.length !== user.data.roleIds.length || roleIds.some((id) => !user.data.roleIds.includes(id));
    const overridesChanged = JSON.stringify(overrides) !==
      JSON.stringify(user.data.overrides.map((g) => ({ selector: g.selector, levels: [...g.levels], effect: g.effect })));
    return (
      username.trim() !== user.data.username ||
      displayName !== user.data.displayName ||
      email !== (user.data.email ?? "") ||
      rolesChanged ||
      overridesChanged
    );
  }, [user.data, username, displayName, email, roleIds, overrides]);

  const save = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.PATCH("/users/{id}", {
          params: { path: { id: userId } },
          body: {
            username: username.trim(),
            displayName,
            email: email.trim() === "" ? null : email.trim(),
            roleIds,
            overrides,
          },
        }),
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["users"] });
      toast("success", t("common.save"), undefined);
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const setStatus = useMutation({
    mutationFn: async (status: "active" | "disabled") =>
      unwrap(await api.PATCH("/users/{id}", { params: { path: { id: userId } }, body: { status } })),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["users"] }),
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const [resetMode, setResetMode] = useState<"invite" | "temp_password" | null>(null);
  const [revokeOnReset, setRevokeOnReset] = useState(true);
  const [resetResult, setResetResult] = useState<CreateUserResult | null>(null);
  const reset = useMutation({
    mutationFn: async (mode: "invite" | "temp_password") =>
      unwrap(
        await api.POST("/users/{id}/reset", {
          params: { path: { id: userId } },
          body: { mode, revokeSessions: revokeOnReset },
        }),
      ),
    onSuccess: (data) => {
      setResetMode(null);
      setResetResult(data);
      void queryClient.invalidateQueries({ queryKey: ["users"] });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const revokeSessions = useMutation({
    mutationFn: async () =>
      unwrap(await api.DELETE("/users/{id}/sessions", { params: { path: { id: userId } } })),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["users", userId, "sessions"] }),
  });

  const [deleteOpen, setDeleteOpen] = useState(false);
  const remove = useMutation({
    mutationFn: async () => unwrap(await api.DELETE("/users/{id}", { params: { path: { id: userId } } })),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["users"] });
      void navigate({ to: "/admin/users" });
    },
    onError: (err) => {
      setDeleteOpen(false);
      toast("error", isApiError(err) ? err.message : t("common.unknownError"));
    },
  });

  if (user.isLoading) return <PageLoader />;
  if (user.isError) {
    return (
      <Card>
        <ErrorState error={user.error} onRetry={() => void user.refetch()} />
      </Card>
    );
  }
  const u = user.data!;

  return (
    <div className="space-y-6">
      <div>
        <Link to="/admin/users" className="text-fg-subtle hover:text-fg mb-3 inline-flex items-center gap-1.5 text-[13px] transition-colors">
          <ArrowLeft className="size-3.5" />
          {t("users.title")}
        </Link>
        <div className="flex flex-wrap items-center gap-3">
          <span className="bg-accent-soft text-accent-soft-fg flex size-11 items-center justify-center rounded-full text-base font-semibold">
            {u.displayName.slice(0, 1).toUpperCase()}
          </span>
          <div>
            <h1 className="text-fg flex items-center gap-2 text-xl font-semibold tracking-tight">
              {u.displayName}
              {isSelf && <Badge tone="accent">{t("common.you")}</Badge>}
              <Badge tone={u.status === "active" ? "success" : "neutral"}>
                {u.status === "active" ? t("users.statusActive") : t("users.statusDisabled")}
              </Badge>
            </h1>
            <p className="text-fg-muted text-sm">
              {u.username} · {t("users.columns.lastLogin")}: {formatRelative(u.lastLoginAt)}
            </p>
          </div>
          {canConfigure && dirty && (
            <Button variant="primary" className="ml-auto" loading={save.isPending} onClick={() => save.mutate()}>
              {t("common.save")}
            </Button>
          )}
        </div>
      </div>

      <div className="grid items-start gap-4 lg:grid-cols-2">
        <div className="space-y-4">
          <Card>
            <CardHeader title={t("users.detail.profile")} />
            <CardBody className="space-y-4">
              <Field label={t("users.create.username")} hint={t("users.detail.usernameHint")}>
                {(p) => <Input {...p} disabled={!canConfigure} value={username} onChange={(e) => setUsername(e.target.value)} />}
              </Field>
              <Field label={t("users.create.displayName")}>
                {(p) => <Input {...p} disabled={!canConfigure} value={displayName} onChange={(e) => setDisplayName(e.target.value)} />}
              </Field>
              <Field label={t("users.create.email")} optional={t("common.optional")}>
                {(p) => <Input {...p} type="email" disabled={!canConfigure} value={email} onChange={(e) => setEmail(e.target.value)} />}
              </Field>
            </CardBody>
          </Card>

          <Card>
            <CardHeader title={t("users.detail.roles")} />
            <CardBody className="space-y-2">
              {(roles.data ?? []).map((role) => (
                <Checkbox
                  key={role.id}
                  disabled={!canConfigure}
                  checked={roleIds.includes(role.id)}
                  onCheckedChange={(checked) =>
                    setRoleIds((prev) => (checked ? [...prev, role.id] : prev.filter((id) => id !== role.id)))
                  }
                  label={role.name}
                  hint={role.description || undefined}
                />
              ))}
            </CardBody>
          </Card>

          {canConfigure && (
            <Card>
              <CardHeader title={t("users.detail.overrides")} description={t("users.detail.overridesHint")} />
              <CardBody>
                <GrantEditor grants={overrides} onChange={setOverrides} />
              </CardBody>
            </Card>
          )}
        </div>

        <div className="space-y-4">
          {canConfigure && (
            <Card>
              <CardHeader
                title={
                  <span className="flex items-center gap-2">
                    <KeyRound className="text-fg-subtle size-4" />
                    {t("users.detail.resetAccess")}
                  </span>
                }
                description={t("users.detail.resetIntro")}
              />
              <CardBody className="space-y-3">
                <div className="flex flex-wrap gap-2">
                  <Button
                    variant="secondary"
                    loading={reset.isPending && resetMode === "invite"}
                    onClick={() => {
                      setResetMode("invite");
                      reset.mutate("invite");
                    }}
                  >
                    {t("users.detail.newInvite")}
                  </Button>
                  <Button
                    variant="secondary"
                    loading={reset.isPending && resetMode === "temp_password"}
                    onClick={() => {
                      setResetMode("temp_password");
                      reset.mutate("temp_password");
                    }}
                  >
                    {t("users.detail.newTempPassword")}
                  </Button>
                </div>
                <Checkbox checked={revokeOnReset} onCheckedChange={setRevokeOnReset} label={t("users.detail.alsoRevoke")} />
              </CardBody>
            </Card>
          )}

          <Card>
            <CardHeader
              title={
                <span className="flex items-center gap-2">
                  <Laptop className="text-fg-subtle size-4" />
                  {t("users.detail.sessions")}
                </span>
              }
              actions={
                canConfigure && (sessions.data?.length ?? 0) > 0 ? (
                  <Button size="sm" variant="danger-ghost" loading={revokeSessions.isPending} onClick={() => revokeSessions.mutate()}>
                    {t("users.detail.revokeAll")}
                  </Button>
                ) : undefined
              }
            />
            <CardBody className="px-0 py-0">
              {(sessions.data ?? []).length === 0 ? (
                <p className="text-fg-subtle px-5 py-6 text-center text-[13px]">{t("common.none")}</p>
              ) : (
                <ul>
                  {(sessions.data ?? []).map((sess) => (
                    <li key={sess.id} className="border-edge border-b px-5 py-3 text-[13px] last:border-b-0">
                      <p className="text-fg font-medium">{sess.ip || "—"}</p>
                      <p className="text-fg-subtle truncate text-xs">{sess.userAgent || "—"}</p>
                      <p className="text-fg-subtle text-xs">{t("account.sessionSeen", { when: formatRelative(sess.lastSeenAt) })}</p>
                    </li>
                  ))}
                </ul>
              )}
            </CardBody>
          </Card>

          {(canConfigure || canDelete) && !isSelf && (
            <Card className="border-danger/30">
              <CardHeader title={t("users.detail.dangerZone")} />
              <CardBody className="flex flex-wrap gap-2">
                {canConfigure &&
                  (u.status === "active" ? (
                    <Button variant="secondary" loading={setStatus.isPending} onClick={() => setStatus.mutate("disabled")}>
                      {t("users.detail.disable")}
                    </Button>
                  ) : (
                    <Button variant="secondary" loading={setStatus.isPending} onClick={() => setStatus.mutate("active")}>
                      {t("users.detail.enable")}
                    </Button>
                  ))}
                {canDelete && (
                  <Button variant="danger" onClick={() => setDeleteOpen(true)}>
                    {t("users.detail.deleteUser")}
                  </Button>
                )}
              </CardBody>
            </Card>
          )}
        </div>
      </div>

      {resetResult && (
        <Dialog open onOpenChange={(open) => !open && setResetResult(null)}>
          <DialogContent
            title={t("users.created.title", { username: u.username })}
            description={
              resetResult.inviteUrl
                ? t("users.created.inviteIntro", {
                    expires: resetResult.inviteExpiresAt ? formatDateTime(resetResult.inviteExpiresAt) : "",
                  })
                : t("users.created.tempIntro")
            }
            footer={
              <Button variant="primary" onClick={() => setResetResult(null)}>
                {t("users.created.understood")}
              </Button>
            }
          >
            <Secret value={resetResult.inviteUrl ?? resetResult.tempPassword ?? ""} />
          </DialogContent>
        </Dialog>
      )}

      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={t("users.detail.deleteConfirmTitle", { username: u.username })}
        body={t("users.detail.deleteConfirmBody")}
        confirmLabel={t("common.delete")}
        loading={remove.isPending}
        onConfirm={() => remove.mutate()}
      />
    </div>
  );
}
