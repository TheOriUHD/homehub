import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { UserPlus, Users as UsersIcon } from "lucide-react";
import {
  api,
  unwrap,
  isApiError,
  type CreateUserResult,
  type Role,
  type User,
} from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { RES } from "@/lib/perms";
import { formatRelative } from "@/lib/utils";
import { Card } from "@/ui/card";
import { Button } from "@/ui/button";
import { Badge } from "@/ui/badge";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { Checkbox, Secret } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { SkeletonRows } from "@/ui/skeleton";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { formatDateTime } from "@/lib/utils";

export function UsersPage() {
  const { t } = useTranslation();
  const { can } = useAuth();
  const navigate = useNavigate();

  const users = useQuery({
    queryKey: ["users"],
    queryFn: async () => unwrap(await api.GET("/users")),
  });
  const roles = useQuery({
    queryKey: ["roles"],
    queryFn: async () => unwrap(await api.GET("/roles")),
  });

  const roleName = useMemo(() => {
    const map = new Map<string, string>();
    for (const role of roles.data ?? []) map.set(role.id, role.name);
    return map;
  }, [roles.data]);

  const [createOpen, setCreateOpen] = useState(false);
  const canConfigure = can(RES.users, "configure");

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-fg text-xl font-semibold tracking-tight">{t("users.title")}</h1>
          <p className="text-fg-muted mt-1 text-sm">{t("users.subtitle")}</p>
        </div>
        {canConfigure && (
          <Button variant="primary" onClick={() => setCreateOpen(true)}>
            <UserPlus className="size-4" />
            {t("users.add")}
          </Button>
        )}
      </div>

      <Card>
        {users.isLoading ? (
          <SkeletonRows rows={4} className="p-5" />
        ) : users.isError ? (
          <ErrorState error={users.error} onRetry={() => void users.refetch()} />
        ) : users.data!.length <= 1 && canConfigure ? (
          <>
            <UserTable users={users.data!} roleName={roleName} onOpen={(id) => void navigate({ to: "/admin/users/$userId", params: { userId: id } })} />
            <EmptyState
              icon={UsersIcon}
              title={t("users.empty.title")}
              body={t("users.empty.body")}
              className="border-edge border-t py-8"
              action={
                <Button variant="secondary" onClick={() => setCreateOpen(true)}>
                  {t("users.add")}
                </Button>
              }
            />
          </>
        ) : (
          <UserTable users={users.data!} roleName={roleName} onOpen={(id) => void navigate({ to: "/admin/users/$userId", params: { userId: id } })} />
        )}
      </Card>

      {createOpen && <CreateUserDialog roles={roles.data ?? []} onClose={() => setCreateOpen(false)} />}
    </div>
  );
}

function UserTable({
  users,
  roleName,
  onOpen,
}: {
  users: User[];
  roleName: Map<string, string>;
  onOpen: (id: string) => void;
}) {
  const { t } = useTranslation();
  return (
    <Table>
      <THead>
        <tr>
          <TH>{t("users.columns.user")}</TH>
          <TH className="hidden md:table-cell">{t("users.columns.roles")}</TH>
          <TH className="hidden sm:table-cell">{t("users.columns.security")}</TH>
          <TH className="hidden lg:table-cell">{t("users.columns.lastLogin")}</TH>
          <TH>{t("users.columns.status")}</TH>
        </tr>
      </THead>
      <TBody>
        {users.map((user) => (
          <TR
            key={user.id}
            interactive
            onClick={() => onOpen(user.id)}
            tabIndex={0}
            onKeyDown={(e) => {
              if (e.key === "Enter") onOpen(user.id);
            }}
          >
            <TD>
              <div className="flex items-center gap-2.5">
                <span className="bg-accent-soft text-accent-soft-fg flex size-7 shrink-0 items-center justify-center rounded-full text-xs font-semibold">
                  {user.displayName.slice(0, 1).toUpperCase()}
                </span>
                <div className="min-w-0">
                  <p className="text-fg truncate font-medium">{user.displayName}</p>
                  <p className="text-fg-subtle truncate text-xs">{user.username}</p>
                </div>
              </div>
            </TD>
            <TD className="hidden md:table-cell">
              <div className="flex flex-wrap gap-1">
                {user.roleIds.length === 0 ? (
                  <span className="text-fg-subtle text-xs">{t("common.none")}</span>
                ) : (
                  user.roleIds.map((id) => (
                    <Badge key={id} tone="neutral">
                      {roleName.get(id) ?? "…"}
                    </Badge>
                  ))
                )}
              </div>
            </TD>
            <TD className="hidden sm:table-cell">
              <div className="flex flex-wrap gap-1">
                {user.totpEnabled && <Badge tone="success">{t("users.totpOn")}</Badge>}
                {user.passkeyCount > 0 && (
                  <Badge tone="accent">{t("users.passkeys", { count: user.passkeyCount })}</Badge>
                )}
                {user.mustChangePassword && <Badge tone="warning">{t("users.mustChange")}</Badge>}
              </div>
            </TD>
            <TD className="text-fg-muted hidden lg:table-cell">{formatRelative(user.lastLoginAt)}</TD>
            <TD>
              <Badge tone={user.status === "active" ? "success" : "neutral"}>
                {user.status === "active" ? t("users.statusActive") : t("users.statusDisabled")}
              </Badge>
            </TD>
          </TR>
        ))}
      </TBody>
    </Table>
  );
}

function CreateUserDialog({ roles, onClose }: { roles: Role[]; onClose: () => void }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();

  const [username, setUsername] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [email, setEmail] = useState("");
  const [roleIds, setRoleIds] = useState<string[]>([]);
  const [onboarding, setOnboarding] = useState<"invite" | "temp_password">("invite");
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<CreateUserResult | null>(null);

  const create = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/users", {
          body: {
            username,
            displayName,
            email: email.trim() || undefined,
            roleIds,
            onboarding,
          },
        }),
      ),
    onSuccess: (data) => {
      setResult(data);
      void queryClient.invalidateQueries({ queryKey: ["users"] });
    },
    onError: (err) => setError(isApiError(err) ? err.message : t("common.unknownError")),
  });

  if (result) {
    return (
      <Dialog open onOpenChange={(open) => !open && onClose()}>
        <DialogContent
          title={t("users.created.title", { username: result.user.username })}
          description={
            result.inviteUrl
              ? t("users.created.inviteIntro", {
                  expires: result.inviteExpiresAt ? formatDateTime(result.inviteExpiresAt) : "",
                })
              : t("users.created.tempIntro")
          }
          footer={
            <Button variant="primary" onClick={onClose}>
              {t("users.created.understood")}
            </Button>
          }
        >
          <Secret value={result.inviteUrl ?? result.tempPassword ?? ""} />
        </DialogContent>
      </Dialog>
    );
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={t("users.create.title")}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button
              variant="primary"
              disabled={!username.trim() || !displayName.trim()}
              loading={create.isPending}
              onClick={() => {
                setError(null);
                create.mutate();
              }}
            >
              {t("users.create.submit")}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t("users.create.username")} error={error}>
              {(p) => <Input {...p} autoFocus required minLength={3} value={username} onChange={(e) => setUsername(e.target.value)} />}
            </Field>
            <Field label={t("users.create.displayName")}>
              {(p) => <Input {...p} required value={displayName} onChange={(e) => setDisplayName(e.target.value)} />}
            </Field>
          </div>
          <Field label={t("users.create.email")} optional={t("common.optional")}>
            {(p) => <Input {...p} type="email" value={email} onChange={(e) => setEmail(e.target.value)} />}
          </Field>

          <div>
            <p className="text-fg mb-1.5 text-[13px] font-medium">{t("users.create.roles")}</p>
            <p className="text-fg-subtle mb-2 text-xs">{t("users.create.rolesHint")}</p>
            <div className="space-y-2">
              {roles.map((role) => (
                <Checkbox
                  key={role.id}
                  checked={roleIds.includes(role.id)}
                  onCheckedChange={(checked) =>
                    setRoleIds((prev) => (checked ? [...prev, role.id] : prev.filter((id) => id !== role.id)))
                  }
                  label={role.name}
                  hint={role.description || undefined}
                />
              ))}
            </div>
          </div>

          <div>
            <p className="text-fg mb-2 text-[13px] font-medium">{t("users.create.onboarding")}</p>
            <div className="space-y-2">
              <OnboardingOption
                selected={onboarding === "invite"}
                onSelect={() => setOnboarding("invite")}
                label={t("users.create.invite")}
                hint={t("users.create.inviteHint")}
              />
              <OnboardingOption
                selected={onboarding === "temp_password"}
                onSelect={() => setOnboarding("temp_password")}
                label={t("users.create.tempPassword")}
                hint={t("users.create.tempPasswordHint")}
              />
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function OnboardingOption({
  selected,
  onSelect,
  label,
  hint,
}: {
  selected: boolean;
  onSelect: () => void;
  label: string;
  hint: string;
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      className={
        "w-full cursor-pointer rounded-lg border p-3 text-left transition-colors " +
        (selected
          ? "border-accent bg-accent-soft/50"
          : "border-edge hover:border-edge-strong bg-transparent")
      }
    >
      <p className="text-fg text-sm font-medium">{label}</p>
      <p className="text-fg-subtle mt-0.5 text-xs">{hint}</p>
    </button>
  );
}
