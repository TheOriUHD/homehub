import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Archive, Lock, TriangleAlert } from "lucide-react";
import {
  api,
  unwrap,
  isApiError,
  type BackupArchive,
  type BackupManifest,
  type BackupTarget,
} from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { formatBytes, formatDateTime } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Input } from "@/ui/input";
import { Field } from "@/ui/field";
import { Dialog, DialogContent } from "@/ui/dialog";
import { EmptyState, ErrorState } from "@/ui/states";
import { SkeletonRows } from "@/ui/skeleton";
import { useToast } from "@/ui/toast";
import { BACKUPS_RES } from "./shared";

const baseName = (path: string) => path.split("/").pop() || path;

/** ArchivesDialog lists the restore points stored on one target. */
export function ArchivesDialog({ target, onClose }: { target: BackupTarget; onClose: () => void }) {
  const { t } = useTranslation();
  const { can } = useAuth();

  const canRestore = can(BACKUPS_RES, "configure");

  const archives = useQuery({
    queryKey: ["backups", "archives", target.id],
    queryFn: async () =>
      unwrap(await api.GET("/backups/targets/{id}/archives", { params: { path: { id: target.id } } })),
  });

  const [manifests, setManifests] = useState<Record<string, BackupManifest>>({});
  const [expanded, setExpanded] = useState<string | null>(null);
  const [restoring, setRestoring] = useState<BackupArchive | null>(null);

  const inspect = useMutation({
    mutationFn: async (path: string) =>
      unwrap(
        await api.POST("/backups/targets/{id}/archives/inspect", {
          params: { path: { id: target.id } },
          body: { path },
        }),
      ),
    onSuccess: (manifest, path) => {
      setManifests((prev) => ({ ...prev, [path]: manifest }));
      setExpanded(path);
    },
  });

  const inspectError =
    inspect.error && isApiError(inspect.error) ? inspect.error.message : inspect.error ? t("common.unknownError") : null;

  const sorted = [...(archives.data ?? [])].sort(
    (a, b) => new Date(b.modTime).getTime() - new Date(a.modTime).getTime(),
  );

  const toggleInspect = (archive: BackupArchive) => {
    if (expanded === archive.path) {
      setExpanded(null);
      return;
    }
    if (manifests[archive.path]) {
      setExpanded(archive.path);
      return;
    }
    inspect.mutate(archive.path);
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        wide
        title={t("backups.archives.title", { name: target.name })}
        description={t("backups.archives.intro")}
      >
        {archives.isLoading ? (
          <SkeletonRows rows={3} />
        ) : archives.isError ? (
          <ErrorState error={archives.error} onRetry={() => void archives.refetch()} />
        ) : sorted.length === 0 ? (
          <EmptyState
            icon={Archive}
            title={t("backups.archives.empty.title")}
            body={t("backups.archives.empty.body")}
          />
        ) : (
          <ul className="border-edge divide-edge divide-y rounded-lg border">
            {sorted.map((archive) => {
              const manifest = manifests[archive.path];
              const isExpanded = expanded === archive.path;
              const inspecting = inspect.isPending && inspect.variables === archive.path;
              return (
                <li key={archive.path} className="px-4 py-3">
                  <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
                    <div className="min-w-0 flex-1">
                      <p className="text-fg truncate font-mono text-[13px] font-medium" title={archive.path}>
                        {baseName(archive.path)}
                      </p>
                      <p className="text-fg-subtle mt-0.5 text-xs">
                        {formatBytes(archive.sizeBytes)} · {formatDateTime(archive.modTime)}
                      </p>
                    </div>
                    {archive.encrypted && (
                      <Badge tone="info">
                        <Lock className="size-3" />
                        {t("backups.archives.encrypted")}
                      </Badge>
                    )}
                    <Button size="sm" variant="ghost" loading={inspecting} onClick={() => toggleInspect(archive)}>
                      {t("backups.archives.inspect")}
                    </Button>
                    {canRestore && (
                      <Button size="sm" variant="secondary" onClick={() => setRestoring(archive)}>
                        {t("backups.archives.restore")}
                      </Button>
                    )}
                  </div>
                  {isExpanded && manifest && (
                    <dl className="bg-bg-subtle mt-2.5 grid grid-cols-2 gap-x-4 gap-y-2 rounded-md px-3.5 py-3 text-[13px] sm:grid-cols-4">
                      <div>
                        <dt className="text-fg-subtle text-xs">{t("backups.archives.manifest.version")}</dt>
                        <dd className="text-fg mt-0.5 font-medium">{manifest.version}</dd>
                      </div>
                      <div>
                        <dt className="text-fg-subtle text-xs">{t("backups.archives.manifest.createdAt")}</dt>
                        <dd className="text-fg mt-0.5 font-medium">{formatDateTime(manifest.createdAt)}</dd>
                      </div>
                      <div>
                        <dt className="text-fg-subtle text-xs">{t("backups.archives.manifest.dumpFormat")}</dt>
                        <dd className="text-fg mt-0.5 font-mono">{manifest.dumpFormat}</dd>
                      </div>
                      <div>
                        <dt className="text-fg-subtle text-xs">{t("backups.archives.manifest.encrypted")}</dt>
                        <dd className="text-fg mt-0.5 font-medium">
                          {manifest.encrypted ? t("backups.archives.manifest.yes") : t("backups.archives.manifest.no")}
                        </dd>
                      </div>
                    </dl>
                  )}
                </li>
              );
            })}
          </ul>
        )}

        {inspectError && (
          <p role="alert" className="text-danger mt-3 text-[13px]">
            {inspectError}
          </p>
        )}

        {restoring && (
          <RestoreDialog target={target} archive={restoring} onClose={() => setRestoring(null)} />
        )}
      </DialogContent>
    </Dialog>
  );
}

// ---- restore ---------------------------------------------------------------

/**
 * RestoreDialog is the hard gate before a destructive restore: it spells out
 * the consequences and only arms the button once the user types the confirm
 * word.
 */
function RestoreDialog({
  target,
  archive,
  onClose,
}: {
  target: BackupTarget;
  archive: BackupArchive;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();

  const [confirmText, setConfirmText] = useState("");
  const word = t("backups.archives.restoreWord");
  const armed = confirmText.trim() === word;

  const restore = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/backups/restore", {
          body: { targetId: target.id, path: archive.path, confirm: true },
        }),
      ),
    onSuccess: () => {
      toast("info", t("backups.archives.restoreQueuedTitle"), t("backups.archives.restoreQueuedBody"));
      onClose();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={t("backups.archives.restoreTitle")}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button
              variant="danger"
              disabled={!armed}
              loading={restore.isPending}
              onClick={() => armed && restore.mutate()}
            >
              {t("backups.archives.restoreConfirm")}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <div className="bg-danger-soft text-danger-soft-fg space-y-2 rounded-lg px-4 py-3 text-[13px]">
            <p className="flex items-center gap-2 font-semibold">
              <TriangleAlert className="size-4 shrink-0" />
              {t("backups.archives.restoreWarningTitle")}
            </p>
            <p>{t("backups.archives.restoreWarningReplace")}</p>
            <p>{t("backups.archives.restoreWarningLost", { date: formatDateTime(archive.modTime) })}</p>
            <p>{t("backups.archives.restoreWarningRestart")}</p>
          </div>

          <div className="bg-bg-subtle rounded-md px-3.5 py-2.5">
            <p className="text-fg-subtle text-xs">{t("backups.archives.restoreArchiveLabel")}</p>
            <p className="text-fg mt-0.5 truncate font-mono text-[13px]" title={archive.path}>
              {baseName(archive.path)}
            </p>
            <p className="text-fg-subtle mt-0.5 text-xs">
              {formatBytes(archive.sizeBytes)} · {formatDateTime(archive.modTime)}
            </p>
          </div>

          <Field label={t("backups.archives.restoreTypeToConfirm", { word })}>
            {(p) => (
              <Input
                {...p}
                autoFocus
                autoComplete="off"
                spellCheck={false}
                placeholder={word}
                value={confirmText}
                onChange={(e) => setConfirmText(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && armed) restore.mutate();
                }}
              />
            )}
          </Field>
        </div>
      </DialogContent>
    </Dialog>
  );
}
