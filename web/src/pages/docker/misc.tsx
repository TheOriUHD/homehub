import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Database, Download, KeyRound, Layers, Network, Package, RefreshCw, Trash2 } from "lucide-react";
import {
  api,
  unwrap,
  isApiError,
  type DockerImage,
  type DockerNetwork,
  type DockerPruneResult,
  type DockerRegistryCred,
  type DockerStack,
  type DockerVolume,
} from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useLiveInvalidate } from "@/lib/live";
import { formatBytes, formatRelative } from "@/lib/utils";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { Button } from "@/ui/button";
import { Badge, StatusDot } from "@/ui/badge";
import { Dialog, DialogContent } from "@/ui/dialog";
import { ConfirmDialog } from "@/ui/confirm";
import { Field } from "@/ui/field";
import { Input, PasswordInput } from "@/ui/input";
import { Checkbox } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { SkeletonRows } from "@/ui/skeleton";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { useToast } from "@/ui/toast";
import { Tooltip } from "@/ui/tooltip";
import { containerStateTone } from "./containers";

// ---- stacks ----------------------------------------------------------------

export function DockerStacksPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/docker/$instanceId/stacks" });
  const { can } = useAuth();
  const canControl = can(`docker/${instanceId}`, "control");

  const stacks = useQuery({
    queryKey: ["docker", instanceId, "stacks"],
    queryFn: async () =>
      unwrap(await api.GET("/docker/{id}/stacks", { params: { path: { id: instanceId } } })),
  });
  useLiveInvalidate(`integration:docker/${instanceId}`, [["docker", instanceId, "stacks"]], ["containers-changed"]);

  const [redeploying, setRedeploying] = useState<DockerStack | null>(null);

  if (stacks.isLoading) return <SkeletonRows rows={4} />;
  if (stacks.isError) {
    return (
      <Card>
        <ErrorState error={stacks.error} onRetry={() => void stacks.refetch()} />
      </Card>
    );
  }

  if (stacks.data!.length === 0) {
    return (
      <Card>
        <EmptyState icon={Layers} title={t("docker.stacks.empty.title")} body={t("docker.stacks.empty.body")} />
      </Card>
    );
  }

  return (
    <div className="grid items-start gap-4 lg:grid-cols-2">
      {stacks.data!.map((stack) => {
        const containerCount = stack.services.reduce((sum, s) => sum + s.containers.length, 0);
        return (
          <Card key={stack.project}>
            <CardHeader
              title={
                <span className="flex items-center gap-2">
                  <Layers className="text-fg-subtle size-4" />
                  {stack.project}
                </span>
              }
              description={t("docker.stacks.summary", { services: stack.services.length, containers: containerCount })}
              actions={
                canControl ? (
                  <Button variant="secondary" size="sm" onClick={() => setRedeploying(stack)}>
                    <RefreshCw className="size-3.5" />
                    {t("docker.stacks.redeploy")}
                  </Button>
                ) : undefined
              }
            />
            <CardBody className="space-y-3">
              {stack.services.map((service) => (
                <div key={service.service}>
                  <p className="text-fg-subtle mb-1.5 text-xs font-semibold tracking-wide uppercase">{service.service}</p>
                  <div className="flex flex-wrap gap-1.5">
                    {service.containers.map((container) => (
                      <Link
                        key={container.id}
                        to="/docker/$instanceId/containers/$name"
                        params={{ instanceId, name: container.name }}
                        className="border-edge text-fg hover:border-accent hover:bg-surface-hover flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium transition-colors"
                      >
                        <StatusDot
                          tone={containerStateTone[container.state as keyof typeof containerStateTone] ?? "neutral"}
                        />
                        {container.name}
                      </Link>
                    ))}
                  </div>
                </div>
              ))}
            </CardBody>
          </Card>
        );
      })}

      {redeploying && (
        <RedeployDialog instanceId={instanceId} project={redeploying.project} onClose={() => setRedeploying(null)} />
      )}
    </div>
  );
}

function RedeployDialog({ instanceId, project, onClose }: { instanceId: string; project: string; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const [pull, setPull] = useState(true);

  const redeploy = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/docker/{id}/stacks/{project}/redeploy", {
          params: { path: { id: instanceId, project } },
          body: { pull },
        }),
      ),
    onSuccess: () => {
      toast("success", t("docker.actions.queuedTitle"), t("docker.actions.queuedBody"));
      onClose();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={t("docker.stacks.redeployTitle", { project })}
        description={t("docker.stacks.redeployBody")}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button variant="primary" loading={redeploy.isPending} onClick={() => redeploy.mutate()}>
              {t("docker.stacks.redeploy")}
            </Button>
          </>
        }
      >
        <Checkbox checked={pull} onCheckedChange={setPull} label={t("docker.stacks.redeployPull")} />
      </DialogContent>
    </Dialog>
  );
}

// ---- images ----------------------------------------------------------------

type UpdateResult = { reference: string; updateAvailable: boolean; message?: string | null };

export function DockerImagesPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/docker/$instanceId/images" });
  const { can } = useAuth();
  const toast = useToast();
  const queryClient = useQueryClient();
  const instanceRes = `docker/${instanceId}`;
  const canControl = can(instanceRes, "control");
  const canDelete = can(instanceRes, "delete");
  const canConfigure = can(instanceRes, "configure");

  const images = useQuery({
    queryKey: ["docker", instanceId, "images"],
    queryFn: async () =>
      unwrap(await api.GET("/docker/{id}/images", { params: { path: { id: instanceId } } })),
  });

  const [checks, setChecks] = useState<Record<string, UpdateResult> | null>(null);
  const check = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/docker/{id}/images/check-updates", { params: { path: { id: instanceId } } })),
    onSuccess: (result) => {
      const map: Record<string, UpdateResult> = {};
      for (const entry of result.results) map[entry.reference] = entry;
      setChecks(map);
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const [pullOpen, setPullOpen] = useState(false);
  const [confirmPrune, setConfirmPrune] = useState(false);
  const [deleting, setDeleting] = useState<DockerImage | null>(null);
  const [deleteForce, setDeleteForce] = useState(false);

  const refetch = () => void queryClient.invalidateQueries({ queryKey: ["docker", instanceId, "images"] });

  const prune = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/docker/{id}/images/prune", { params: { path: { id: instanceId } } })),
    onSuccess: (result: DockerPruneResult) => {
      toast("success", t("docker.prune.result", { bytes: formatBytes(result.reclaimedBytes), count: result.deleted }));
      setConfirmPrune(false);
      refetch();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const remove = useMutation({
    mutationFn: async (image: DockerImage) =>
      unwrap(
        await api.DELETE("/docker/{id}/images/{imageId}", {
          params: { path: { id: instanceId, imageId: image.id }, query: { force: deleteForce } },
        }),
      ),
    onSuccess: () => {
      toast("success", t("docker.images.deleted"));
      setDeleting(null);
      setDeleteForce(false);
      refetch();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  if (images.isLoading) return <SkeletonRows rows={6} />;
  if (images.isError) {
    return (
      <Card>
        <ErrorState error={images.error} onRetry={() => void images.refetch()} />
      </Card>
    );
  }

  const rowResult = (image: DockerImage): UpdateResult | undefined =>
    checks ? image.tags.map((tag) => checks[tag]).find((entry) => entry !== undefined) : undefined;

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader
          title={t("docker.images.title")}
          actions={
            <>
              <Button variant="secondary" size="sm" loading={check.isPending} onClick={() => check.mutate()}>
                <RefreshCw className="size-3.5" />
                {t("docker.images.checkUpdates")}
              </Button>
              {canControl && (
                <Button variant="primary" size="sm" onClick={() => setPullOpen(true)}>
                  <Download className="size-3.5" />
                  {t("docker.images.pull")}
                </Button>
              )}
              {canDelete && (
                <Button variant="danger-ghost" size="sm" onClick={() => setConfirmPrune(true)}>
                  <Trash2 className="size-3.5" />
                  {t("docker.images.prune")}
                </Button>
              )}
            </>
          }
        />
        <CardBody className="px-0 py-0">
          {images.data!.length === 0 ? (
            <EmptyState icon={Package} title={t("docker.images.empty.title")} body={t("docker.images.empty.body")} />
          ) : (
            <Table>
              <THead>
                <tr>
                  <TH>{t("docker.images.columns.image")}</TH>
                  <TH>{t("docker.images.columns.size")}</TH>
                  <TH className="hidden md:table-cell">{t("docker.images.columns.created")}</TH>
                  <TH>{t("docker.images.columns.status")}</TH>
                  {checks && <TH>{t("docker.images.columns.updates")}</TH>}
                  {canDelete && <TH className="w-10" />}
                </tr>
              </THead>
              <TBody>
                {images.data!.map((image) => {
                  const shortId = image.id.replace(/^sha256:/, "").slice(0, 12);
                  const result = rowResult(image);
                  return (
                    <TR key={image.id}>
                      <TD>
                        <div className="min-w-0">
                          <p className="text-fg max-w-72 truncate font-medium">
                            {image.tags[0] ?? t("docker.images.untagged")}
                          </p>
                          <p className="text-fg-subtle font-mono text-xs">
                            {shortId}
                            {image.tags.length > 1 && (
                              <span className="ml-1.5 font-sans">
                                {t("docker.images.moreTags", { count: image.tags.length - 1 })}
                              </span>
                            )}
                          </p>
                        </div>
                      </TD>
                      <TD className="text-fg-muted whitespace-nowrap">{formatBytes(image.sizeBytes)}</TD>
                      <TD className="text-fg-muted hidden whitespace-nowrap md:table-cell">
                        {formatRelative(image.created)}
                      </TD>
                      <TD>
                        <span className="flex gap-1.5">
                          {image.inUse && <Badge tone="accent">{t("docker.images.inUse")}</Badge>}
                          {image.dangling && <Badge tone="warning">{t("docker.images.dangling")}</Badge>}
                        </span>
                      </TD>
                      {checks && (
                        <TD>
                          {result &&
                            (result.message ? (
                              <Tooltip label={result.message}>
                                <span className="inline-flex">
                                  <Badge tone="warning">{t("docker.images.checkFailed")}</Badge>
                                </span>
                              </Tooltip>
                            ) : result.updateAvailable ? (
                              <Badge tone="info">{t("docker.images.updateAvailable")}</Badge>
                            ) : (
                              <Badge tone="success">{t("docker.images.upToDate")}</Badge>
                            ))}
                        </TD>
                      )}
                      {canDelete && (
                        <TD>
                          <Tooltip label={t("common.delete")}>
                            <Button
                              variant="ghost"
                              size="icon"
                              aria-label={t("common.delete")}
                              onClick={() => setDeleting(image)}
                            >
                              <Trash2 className="text-danger size-4" />
                            </Button>
                          </Tooltip>
                        </TD>
                      )}
                    </TR>
                  );
                })}
              </TBody>
            </Table>
          )}
        </CardBody>
      </Card>

      {canConfigure && <RegistriesCard instanceId={instanceId} />}

      {pullOpen && <PullImageDialog instanceId={instanceId} onClose={() => setPullOpen(false)} />}
      <ConfirmDialog
        open={confirmPrune}
        onOpenChange={setConfirmPrune}
        title={t("docker.images.pruneConfirmTitle")}
        body={t("docker.images.pruneConfirmBody")}
        confirmLabel={t("docker.images.prune")}
        loading={prune.isPending}
        onConfirm={() => prune.mutate()}
      />
      <Dialog open={deleting !== null} onOpenChange={(open) => !open && setDeleting(null)}>
        <DialogContent
          title={t("docker.images.deleteConfirmTitle", {
            name: deleting?.tags[0] ?? deleting?.id.replace(/^sha256:/, "").slice(0, 12) ?? "",
          })}
          description={t("docker.images.deleteConfirmBody")}
          footer={
            <>
              <Button variant="ghost" onClick={() => setDeleting(null)}>
                {t("common.cancel")}
              </Button>
              <Button variant="danger" loading={remove.isPending} onClick={() => deleting && remove.mutate(deleting)}>
                {t("common.delete")}
              </Button>
            </>
          }
        >
          <Checkbox
            checked={deleteForce}
            onCheckedChange={setDeleteForce}
            label={t("docker.images.deleteForce")}
            hint={t("docker.images.deleteForceHint")}
          />
        </DialogContent>
      </Dialog>
    </div>
  );
}

function PullImageDialog({ instanceId, onClose }: { instanceId: string; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const [reference, setReference] = useState("");

  const pull = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/docker/{id}/images/pull", {
          params: { path: { id: instanceId } },
          body: { reference: reference.trim() },
        }),
      ),
    onSuccess: () => {
      toast("success", t("docker.actions.queuedTitle"), t("docker.actions.queuedBody"));
      onClose();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={t("docker.images.pullTitle")}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button variant="primary" disabled={!reference.trim()} loading={pull.isPending} onClick={() => pull.mutate()}>
              {t("docker.images.pull")}
            </Button>
          </>
        }
      >
        <Field label={t("docker.images.pullReference")} hint={t("docker.images.pullReferenceHint")}>
          {(p) => (
            <Input
              {...p}
              autoFocus
              className="font-mono text-[13px]"
              placeholder="nginx:latest"
              value={reference}
              onChange={(e) => setReference(e.target.value)}
            />
          )}
        </Field>
      </DialogContent>
    </Dialog>
  );
}

// ---- registries ------------------------------------------------------------

function RegistriesCard({ instanceId }: { instanceId: string }) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();

  const registries = useQuery({
    queryKey: ["docker", instanceId, "registries"],
    queryFn: async () =>
      unwrap(await api.GET("/docker/{id}/registries", { params: { path: { id: instanceId } } })),
  });

  const [addOpen, setAddOpen] = useState(false);
  const [removing, setRemoving] = useState<DockerRegistryCred | null>(null);

  const remove = useMutation({
    mutationFn: async (registry: string) =>
      unwrap(
        await api.DELETE("/docker/{id}/registries/{registry}", {
          params: { path: { id: instanceId, registry } },
        }),
      ),
    onSuccess: () => {
      setRemoving(null);
      void queryClient.invalidateQueries({ queryKey: ["docker", instanceId, "registries"] });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <Card>
      <CardHeader
        title={t("docker.registries.title")}
        description={t("docker.registries.intro")}
        actions={
          <Button variant="secondary" size="sm" onClick={() => setAddOpen(true)}>
            <KeyRound className="size-3.5" />
            {t("docker.registries.add")}
          </Button>
        }
      />
      <CardBody className="px-0 py-0">
        {registries.isLoading ? (
          <SkeletonRows rows={2} className="px-5 py-4" />
        ) : registries.isError ? (
          <ErrorState error={registries.error} onRetry={() => void registries.refetch()} className="py-8" />
        ) : registries.data!.length === 0 ? (
          <p className="text-fg-subtle px-5 py-8 text-center text-[13px]">{t("docker.registries.empty")}</p>
        ) : (
          <ul>
            {registries.data!.map((cred) => (
              <li key={cred.registry} className="border-edge flex items-center gap-3 border-b px-5 py-3 last:border-b-0">
                <KeyRound className="text-fg-subtle size-4 shrink-0" />
                <div className="min-w-0 flex-1">
                  <p className="text-fg truncate font-mono text-[13px] font-medium">{cred.registry}</p>
                  <p className="text-fg-subtle text-xs">{cred.username}</p>
                </div>
                <Button size="sm" variant="danger-ghost" onClick={() => setRemoving(cred)}>
                  {t("docker.registries.remove")}
                </Button>
              </li>
            ))}
          </ul>
        )}
      </CardBody>

      {addOpen && <AddRegistryDialog instanceId={instanceId} onClose={() => setAddOpen(false)} />}
      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(open) => !open && setRemoving(null)}
        title={removing ? t("docker.registries.removeConfirmTitle", { registry: removing.registry }) : ""}
        body={t("docker.registries.removeConfirmBody")}
        confirmLabel={t("docker.registries.remove")}
        loading={remove.isPending}
        onConfirm={() => removing && remove.mutate(removing.registry)}
      />
    </Card>
  );
}

function AddRegistryDialog({ instanceId, onClose }: { instanceId: string; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();

  const [registry, setRegistry] = useState("");
  const [username, setUsername] = useState("");
  const [secret, setSecret] = useState("");

  const save = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.PUT("/docker/{id}/registries", {
          params: { path: { id: instanceId } },
          body: { registry: registry.trim(), username: username.trim(), secret },
        }),
      ),
    onSuccess: () => {
      toast("success", t("docker.registries.saved"));
      void queryClient.invalidateQueries({ queryKey: ["docker", instanceId, "registries"] });
      onClose();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const complete = registry.trim() && username.trim() && secret;

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={t("docker.registries.add")}
        description={t("docker.registries.addIntro")}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button variant="primary" disabled={!complete} loading={save.isPending} onClick={() => save.mutate()}>
              {t("common.save")}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label={t("docker.registries.registry")} hint={t("docker.registries.registryHint")}>
            {(p) => (
              <Input {...p} autoFocus className="font-mono text-[13px]" placeholder="ghcr.io" value={registry} onChange={(e) => setRegistry(e.target.value)} />
            )}
          </Field>
          <Field label={t("docker.registries.username")}>
            {(p) => <Input {...p} value={username} onChange={(e) => setUsername(e.target.value)} />}
          </Field>
          <Field label={t("docker.registries.secret")} hint={t("docker.registries.secretHint")}>
            {(p) => <PasswordInput {...p} value={secret} onChange={(e) => setSecret(e.target.value)} />}
          </Field>
        </div>
      </DialogContent>
    </Dialog>
  );
}

// ---- volumes ---------------------------------------------------------------

export function DockerVolumesPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/docker/$instanceId/volumes" });
  const { can } = useAuth();
  const toast = useToast();
  const queryClient = useQueryClient();
  const canDelete = can(`docker/${instanceId}`, "delete");

  const volumes = useQuery({
    queryKey: ["docker", instanceId, "volumes"],
    queryFn: async () =>
      unwrap(await api.GET("/docker/{id}/volumes", { params: { path: { id: instanceId } } })),
  });

  const [confirmPrune, setConfirmPrune] = useState(false);
  const [deleting, setDeleting] = useState<DockerVolume | null>(null);

  const refetch = () => void queryClient.invalidateQueries({ queryKey: ["docker", instanceId, "volumes"] });

  const prune = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/docker/{id}/volumes/prune", { params: { path: { id: instanceId } } })),
    onSuccess: (result: DockerPruneResult) => {
      toast("success", t("docker.prune.result", { bytes: formatBytes(result.reclaimedBytes), count: result.deleted }));
      setConfirmPrune(false);
      refetch();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const remove = useMutation({
    mutationFn: async (name: string) =>
      unwrap(
        await api.DELETE("/docker/{id}/volumes/{name}", {
          params: { path: { id: instanceId, name } },
        }),
      ),
    onSuccess: () => {
      toast("success", t("docker.volumes.deleted"));
      setDeleting(null);
      refetch();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  if (volumes.isLoading) return <SkeletonRows rows={5} />;
  if (volumes.isError) {
    return (
      <Card>
        <ErrorState error={volumes.error} onRetry={() => void volumes.refetch()} />
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader
        title={t("docker.volumes.title")}
        actions={
          canDelete ? (
            <Button variant="danger-ghost" size="sm" onClick={() => setConfirmPrune(true)}>
              <Trash2 className="size-3.5" />
              {t("docker.volumes.prune")}
            </Button>
          ) : undefined
        }
      />
      <CardBody className="px-0 py-0">
        {volumes.data!.length === 0 ? (
          <EmptyState icon={Database} title={t("docker.volumes.empty.title")} body={t("docker.volumes.empty.body")} />
        ) : (
          <Table>
            <THead>
              <tr>
                <TH>{t("docker.volumes.columns.volume")}</TH>
                <TH className="hidden md:table-cell">{t("docker.volumes.columns.mountpoint")}</TH>
                <TH className="hidden lg:table-cell">{t("docker.volumes.columns.created")}</TH>
                <TH>{t("docker.volumes.columns.status")}</TH>
                {canDelete && <TH className="w-10" />}
              </tr>
            </THead>
            <TBody>
              {volumes.data!.map((volume) => (
                <TR key={volume.name}>
                  <TD>
                    <div className="min-w-0">
                      <p className="text-fg max-w-72 truncate font-medium">{volume.name}</p>
                      <p className="text-fg-subtle text-xs">{volume.driver}</p>
                    </div>
                  </TD>
                  <TD className="text-fg-muted hidden max-w-72 truncate font-mono text-xs md:table-cell">
                    {volume.mountpoint}
                  </TD>
                  <TD className="text-fg-muted hidden whitespace-nowrap lg:table-cell">
                    {formatRelative(volume.created)}
                  </TD>
                  <TD>
                    {volume.inUse ? (
                      <Badge tone="accent">{t("docker.volumes.inUse")}</Badge>
                    ) : (
                      <Badge tone="neutral">{t("docker.volumes.unused")}</Badge>
                    )}
                  </TD>
                  {canDelete && (
                    <TD>
                      {!volume.inUse && (
                        <Tooltip label={t("common.delete")}>
                          <Button
                            variant="ghost"
                            size="icon"
                            aria-label={t("common.delete")}
                            onClick={() => setDeleting(volume)}
                          >
                            <Trash2 className="text-danger size-4" />
                          </Button>
                        </Tooltip>
                      )}
                    </TD>
                  )}
                </TR>
              ))}
            </TBody>
          </Table>
        )}
      </CardBody>

      <ConfirmDialog
        open={confirmPrune}
        onOpenChange={setConfirmPrune}
        title={t("docker.volumes.pruneConfirmTitle")}
        body={t("docker.volumes.pruneConfirmBody")}
        confirmLabel={t("docker.volumes.prune")}
        loading={prune.isPending}
        onConfirm={() => prune.mutate()}
      />
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={deleting ? t("docker.volumes.deleteConfirmTitle", { name: deleting.name }) : ""}
        body={t("docker.volumes.deleteConfirmBody")}
        confirmLabel={t("common.delete")}
        loading={remove.isPending}
        onConfirm={() => deleting && remove.mutate(deleting.name)}
      />
    </Card>
  );
}

// ---- networks --------------------------------------------------------------

export function DockerNetworksPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/docker/$instanceId/networks" });
  const { can } = useAuth();
  const toast = useToast();
  const queryClient = useQueryClient();
  const canDelete = can(`docker/${instanceId}`, "delete");

  const networks = useQuery({
    queryKey: ["docker", instanceId, "networks"],
    queryFn: async () =>
      unwrap(await api.GET("/docker/{id}/networks", { params: { path: { id: instanceId } } })),
  });

  const [deleting, setDeleting] = useState<DockerNetwork | null>(null);

  const remove = useMutation({
    mutationFn: async (networkId: string) =>
      unwrap(
        await api.DELETE("/docker/{id}/networks/{networkId}", {
          params: { path: { id: instanceId, networkId } },
        }),
      ),
    onSuccess: () => {
      toast("success", t("docker.networks.deleted"));
      setDeleting(null);
      void queryClient.invalidateQueries({ queryKey: ["docker", instanceId, "networks"] });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  if (networks.isLoading) return <SkeletonRows rows={4} />;
  if (networks.isError) {
    return (
      <Card>
        <ErrorState error={networks.error} onRetry={() => void networks.refetch()} />
      </Card>
    );
  }

  return (
    <Card>
      {networks.data!.length === 0 ? (
        <EmptyState icon={Network} title={t("docker.networks.empty.title")} body={t("docker.networks.empty.body")} />
      ) : (
        <Table>
          <THead>
            <tr>
              <TH>{t("docker.networks.columns.network")}</TH>
              <TH>{t("docker.networks.columns.driver")}</TH>
              <TH className="hidden sm:table-cell">{t("docker.networks.columns.scope")}</TH>
              <TH className="hidden md:table-cell">{t("docker.networks.columns.containers")}</TH>
              {canDelete && <TH className="w-10" />}
            </tr>
          </THead>
          <TBody>
            {networks.data!.map((network) => (
              <TR key={network.id}>
                <TD>
                  <span className="text-fg flex items-center gap-2 font-medium">
                    <Network className="text-fg-subtle size-4" />
                    {network.name}
                    {network.builtin && <Badge tone="neutral">{t("docker.networks.builtin")}</Badge>}
                  </span>
                </TD>
                <TD className="text-fg-muted">{network.driver}</TD>
                <TD className="text-fg-muted hidden sm:table-cell">{network.scope}</TD>
                <TD className="text-fg-muted hidden md:table-cell">{network.containers}</TD>
                {canDelete && (
                  <TD>
                    {!network.builtin && (
                      <Tooltip label={t("common.delete")}>
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label={t("common.delete")}
                          onClick={() => setDeleting(network)}
                        >
                          <Trash2 className="text-danger size-4" />
                        </Button>
                      </Tooltip>
                    )}
                  </TD>
                )}
              </TR>
            ))}
          </TBody>
        </Table>
      )}

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={deleting ? t("docker.networks.deleteConfirmTitle", { name: deleting.name }) : ""}
        body={t("docker.networks.deleteConfirmBody")}
        confirmLabel={t("common.delete")}
        loading={remove.isPending}
        onConfirm={() => deleting && remove.mutate(deleting.id)}
      />
    </Card>
  );
}
