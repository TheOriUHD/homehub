import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Eye, Lock, Pencil, Shield, Trash2 } from "lucide-react";
import { api, unwrap, isApiError, type GrantInput, type Role } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { RES } from "@/lib/perms";
import { Card } from "@/ui/card";
import { Button } from "@/ui/button";
import { Badge } from "@/ui/badge";
import { Dialog, DialogContent } from "@/ui/dialog";
import { ConfirmDialog } from "@/ui/confirm";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { EmptyState, ErrorState } from "@/ui/states";
import { SkeletonRows } from "@/ui/skeleton";
import { GrantEditor } from "@/components/grant-editor";
import { useToast } from "@/ui/toast";
import { Tooltip } from "@/ui/tooltip";
import { Switch } from "@/ui/switch";

export function RolesPage() {
  const { t } = useTranslation();
  const { can, refresh } = useAuth();
  const toast = useToast();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const roles = useQuery({
    queryKey: ["roles"],
    queryFn: async () => unwrap(await api.GET("/roles")),
  });

  const canConfigure = can(RES.roles, "configure");
  const canDelete = can(RES.roles, "delete");

  const [editing, setEditing] = useState<Role | "new" | null>(null);
  const [deleting, setDeleting] = useState<Role | null>(null);

  const remove = useMutation({
    mutationFn: async (id: string) => unwrap(await api.DELETE("/roles/{id}", { params: { path: { id } } })),
    onSuccess: () => {
      setDeleting(null);
      void queryClient.invalidateQueries({ queryKey: ["roles"] });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const toggleSignals = useMutation({
    mutationFn: async (v: { id: string; hide: boolean }) =>
      unwrap(await api.PATCH("/roles/{id}", { params: { path: { id: v.id } }, body: { hideSignals: v.hide } })),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["roles"] }),
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const preview = useMutation({
    mutationFn: async (roleId: string) => unwrap(await api.PUT("/auth/view-as", { body: { roleId } })),
    onSuccess: async () => {
      await refresh();
      void navigate({ to: "/" });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-fg text-xl font-semibold tracking-tight">{t("roles.title")}</h1>
          <p className="text-fg-muted mt-1 text-sm">{t("roles.subtitle")}</p>
        </div>
        {canConfigure && (
          <Button variant="primary" onClick={() => setEditing("new")}>
            {t("roles.add")}
          </Button>
        )}
      </div>

      {roles.isLoading ? (
        <SkeletonRows rows={4} />
      ) : roles.isError ? (
        <Card>
          <ErrorState error={roles.error} onRetry={() => void roles.refetch()} />
        </Card>
      ) : (
        <div className="grid gap-3 md:grid-cols-2">
          {roles.data!.map((role) => (
            <Card key={role.id} className="flex flex-col p-4">
              <div className="flex items-start justify-between gap-2">
                <div className="flex min-w-0 items-center gap-2.5">
                  <span className="bg-accent-soft text-accent-soft-fg flex size-8 shrink-0 items-center justify-center rounded-lg">
                    {role.isSystem ? <Lock className="size-4" /> : <Shield className="size-4" />}
                  </span>
                  <div className="min-w-0">
                    <p className="text-fg flex items-center gap-2 truncate text-sm font-semibold">
                      {role.name}
                      {role.isSystem && <Badge tone="neutral">{t("roles.system")}</Badge>}
                    </p>
                    <p className="text-fg-subtle text-xs">
                      {t("roles.members", { count: role.memberCount })} ·{" "}
                      {role.grants.length === 0
                        ? t("roles.noGrants")
                        : t("roles.grantCount", { count: role.grants.length })}
                    </p>
                  </div>
                </div>
                <div className="flex shrink-0 gap-1">
                  {canConfigure && (
                    <Tooltip label={t("roles.viewAs")}>
                      <Button variant="ghost" size="icon" aria-label={t("roles.viewAs")} onClick={() => preview.mutate(role.id)}>
                        <Eye className="size-4" />
                      </Button>
                    </Tooltip>
                  )}
                  {canConfigure && !role.isSystem && (
                    <Tooltip label={t("common.edit")}>
                      <Button variant="ghost" size="icon" aria-label={t("common.edit")} onClick={() => setEditing(role)}>
                        <Pencil className="size-4" />
                      </Button>
                    </Tooltip>
                  )}
                  {canDelete && !role.isSystem && (
                    <Tooltip label={t("common.delete")}>
                      <Button variant="ghost" size="icon" aria-label={t("common.delete")} onClick={() => setDeleting(role)}>
                        <Trash2 className="text-danger size-4" />
                      </Button>
                    </Tooltip>
                  )}
                </div>
              </div>
              {role.description && <p className="text-fg-muted mt-2.5 text-[13px]">{role.description}</p>}
              {role.grants.length > 0 && (
                <div className="mt-3 flex flex-wrap gap-1.5">
                  {role.grants.slice(0, 4).map((grant, i) => (
                    <Badge key={i} tone={grant.effect === "deny" ? "danger" : "accent"} className="font-mono">
                      {grant.effect === "deny" && "¬"}
                      {grant.selector}
                    </Badge>
                  ))}
                  {role.grants.length > 4 && <Badge tone="neutral">+{role.grants.length - 4}</Badge>}
                </div>
              )}
              {canConfigure && (
                <label className="border-border/60 mt-3 flex items-center justify-between gap-2 border-t pt-3">
                  <span className="text-fg-muted text-[13px]">{t("roles.hideSignals")}</span>
                  <Switch
                    checked={role.hideSignals}
                    disabled={toggleSignals.isPending}
                    onCheckedChange={(v) => toggleSignals.mutate({ id: role.id, hide: v })}
                    aria-label={t("roles.hideSignals")}
                  />
                </label>
              )}
            </Card>
          ))}
          {roles.data!.every((r) => r.isSystem) && (
            <Card className="md:col-span-2">
              <EmptyState
                icon={Shield}
                title={t("roles.empty.title")}
                body={t("roles.empty.body")}
                action={
                  canConfigure ? (
                    <Button variant="primary" onClick={() => setEditing("new")}>
                      {t("roles.add")}
                    </Button>
                  ) : undefined
                }
              />
            </Card>
          )}
        </div>
      )}

      {editing !== null && (
        <RoleEditorDialog
          role={editing === "new" ? null : editing}
          onClose={() => setEditing(null)}
        />
      )}

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={deleting ? t("roles.deleteConfirmTitle", { name: deleting.name }) : ""}
        body={deleting ? t("roles.deleteConfirmBody", { count: deleting.memberCount }) : ""}
        confirmLabel={t("common.delete")}
        loading={remove.isPending}
        onConfirm={() => deleting && remove.mutate(deleting.id)}
      />
    </div>
  );
}

function RoleEditorDialog({ role, onClose }: { role: Role | null; onClose: () => void }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();

  const [name, setName] = useState(role?.name ?? "");
  const [description, setDescription] = useState(role?.description ?? "");
  const [grants, setGrants] = useState<GrantInput[]>(
    role?.grants.map((g) => ({ selector: g.selector, levels: [...g.levels], effect: g.effect })) ?? [],
  );
  const [error, setError] = useState<string | null>(null);

  const save = useMutation({
    mutationFn: async () => {
      const body = { name, description, grants };
      if (role) {
        return unwrap(await api.PATCH("/roles/{id}", { params: { path: { id: role.id } }, body }));
      }
      return unwrap(await api.POST("/roles", { body }));
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["roles"] });
      onClose();
    },
    onError: (err) => setError(isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        wide
        title={role ? t("roles.editor.editTitle") : t("roles.editor.createTitle")}
        description={t("roles.editor.grantsHint")}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button variant="primary" disabled={!name.trim()} loading={save.isPending} onClick={() => save.mutate()}>
              {role ? t("common.save") : t("common.create")}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t("roles.editor.name")} error={error}>
              {(p) => <Input {...p} autoFocus value={name} onChange={(e) => setName(e.target.value)} />}
            </Field>
            <Field label={t("roles.editor.description")} optional={t("common.optional")}>
              {(p) => <Input {...p} value={description} onChange={(e) => setDescription(e.target.value)} />}
            </Field>
          </div>
          <div>
            <p className="text-fg mb-2 text-[13px] font-medium">{t("roles.editor.grants")}</p>
            <GrantEditor grants={grants} onChange={setGrants} />
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
