import { useEffect, useRef, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, Navigate, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ArrowLeft, ArrowRightLeft, CheckCircle2, Download, RefreshCw, Server, Terminal, Trash2 } from "lucide-react";
import {
  api,
  unwrap,
  isApiError,
  type McOverview,
  type McPairingCode,
} from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { cn, formatBytes, formatRelative } from "@/lib/utils";
import { useIntegrations } from "@/lib/systems";
import { Button } from "@/ui/button";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { ConfirmDialog } from "@/ui/confirm";
import { Dropdown, DropdownContent, DropdownItem, DropdownTrigger } from "@/ui/dropdown";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { CopyButton, PageLoader } from "@/ui/misc";
import { Badge, StatusDot } from "@/ui/badge";
import { ErrorState } from "@/ui/states";
import { useToast } from "@/ui/toast";
import { loaderLabel, mcOverviewKey, mcResource, useMcOverview } from "./shared";

/**
 * Pairing and companion installation. Configure-gated: the module hides the
 * tab, and a direct URL bounces back to the console.
 */
type DownloadEntry = { loader: string; file: string; sizeBytes: number; mcVersions: string; minecraft?: string };

/** The manifest lists one row per jar; the UI wants one card per loader. */
function groupDownloads(entries: DownloadEntry[]): { loader: string; entries: DownloadEntry[] }[] {
  const groups: { loader: string; entries: DownloadEntry[] }[] = [];
  for (const d of entries) {
    const g = groups.find((x) => x.loader === d.loader);
    if (g) g.entries.push(d);
    else groups.push({ loader: d.loader, entries: [d] });
  }
  return groups;
}

export function McSetupPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/mc/$instanceId/setup" });
  const { can } = useAuth();
  const toast = useToast();
  const queryClient = useQueryClient();

  const overview = useMcOverview(instanceId);
  const downloads = useQuery({
    queryKey: ["mc", "downloads"],
    queryFn: async () => unwrap(await api.GET("/mc/downloads")),
    staleTime: 5 * 60_000,
  });

  const [label, setLabel] = useState("");
  const generate = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/mc/{id}/pairing-code", {
          params: { path: { id: instanceId } },
          body: label.trim() ? { label: label.trim() } : {},
        }),
      ),
    onSuccess: (code, _vars, _ctx) => {
      queryClient.setQueryData<McOverview>(mcOverviewKey(instanceId), (prev) =>
        prev ? { ...prev, pendingPairing: code } : prev,
      );
    },
    onError: (err) =>
      toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const allowed = can(mcResource(instanceId), "configure");

  // A code exists whenever this page is open, so the copyable command below
  // is always real — never a placeholder waiting for a click.
  const autoRequested = useRef(false);
  const hasPending = Boolean(overview.data?.pendingPairing);
  useEffect(() => {
    if (!allowed || overview.isLoading || hasPending || generate.isPending) return;
    if (autoRequested.current) return;
    autoRequested.current = true;
    generate.mutate();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [allowed, overview.isLoading, hasPending]);
  // When a code expires the flag flips back — allow one fresh auto-mint.
  useEffect(() => {
    if (hasPending) autoRequested.current = false;
  }, [hasPending]);

  const [cmdMode, setCmdMode] = useState<"ingame" | "console">("ingame");

  if (!allowed) {
    return <Navigate to="/mc/$instanceId" params={{ instanceId }} replace />;
  }

  if (overview.isLoading) return <PageLoader />;
  if (overview.isError) {
    return (
      <Card>
        <ErrorState error={overview.error} onRetry={() => void overview.refetch()} />
      </Card>
    );
  }

  const data = overview.data!;
  const pending = data.pendingPairing ?? null;
  const origin = window.location.origin;
  const code = pending?.code ?? t("mc.setup.codePlaceholder");
  const propertiesSnippet = `url=${origin}\ncode=${code}`;
  const pairCommand = `${cmdMode === "ingame" ? "/" : ""}homehub pair ${origin} ${code}`;
  const servers = data.servers ?? [];

  return (
    <div className="space-y-4">
      <Link
        to="/mc/$instanceId"
        params={{ instanceId }}
        className="text-fg-muted hover:text-fg flex w-fit items-center gap-1 text-[13px] transition-colors"
      >
        <ArrowLeft className="size-3.5" />
        {t("mc.setup.back")}
      </Link>
      <Card>
        <CardHeader
          title={t("mc.setup.serversTitle")}
          description={t("mc.setup.serversIntro")}
        />
        <CardBody className="space-y-2">
          {servers.length === 0 && (
            <p className="text-fg-muted text-[13px]">{t("mc.setup.noServers")}</p>
          )}
          {servers.map((server) => (
            <ServerRow key={server.id} instanceId={instanceId} server={server} />
          ))}
        </CardBody>
      </Card>

      <SshCard instanceId={instanceId} configured={data.sshConfigured} />

      <Card>
        <CardHeader
          title={t("mc.setup.pairingTitle")}
          description={t("mc.setup.pairingIntro")}
          actions={
            <Button
              variant="secondary"
              size="sm"
              loading={generate.isPending}
              onClick={() => generate.mutate()}
            >
              <RefreshCw className="size-3.5" />
              {t("mc.setup.generate")}
            </Button>
          }
        />
        <CardBody className="space-y-4">
          <Field label={t("mc.setup.labelField")} hint={t("mc.setup.labelHint")}>
            {(p) => (
              <Input
                {...p}
                placeholder={t("mc.setup.labelPlaceholder")}
                value={label}
                onChange={(e) => setLabel(e.target.value)}
              />
            )}
          </Field>
          {pending ? (
            <PendingCode
              pending={pending}
              onExpired={() =>
                void queryClient.invalidateQueries({ queryKey: mcOverviewKey(instanceId) })
              }
            />
          ) : (
            <p className="text-fg-muted text-[13px]">{t("mc.setup.noCode")}</p>
          )}

        </CardBody>
      </Card>

      <Card>
        <CardHeader
          title={t("mc.setup.instructionsTitle")}
          description={t("mc.setup.instructionsIntro")}
        />
        <CardBody className="space-y-5">
          <Step index={1} title={t("mc.setup.step1")}>
            {downloads.isLoading ? (
              <p className="text-fg-subtle text-[13px]">{t("common.loading")}</p>
            ) : downloads.isError ? (
              <p className="text-danger-soft-fg text-[13px]">
                {isApiError(downloads.error) ? downloads.error.message : t("common.unknownError")}
              </p>
            ) : (
              <div className="flex flex-wrap gap-2">
                {groupDownloads(downloads.data ?? []).map((group) => (
                  <div
                    key={group.loader}
                    className="border-edge bg-surface flex items-start gap-2.5 rounded-md border px-3 py-2 shadow-1"
                  >
                    <Download className="text-fg-subtle mt-0.5 size-4 shrink-0" />
                    <span>
                      <span className="text-fg block text-[13px] font-semibold">
                        {loaderLabel(group.loader)}
                      </span>
                      {group.entries.length === 1 ? (
                        <a
                          href={`/api/mc/downloads/${encodeURIComponent(group.entries[0].file)}`}
                          download
                          className="text-fg-subtle hover:text-fg block text-xs underline-offset-2 hover:underline"
                        >
                          {t("mc.setup.downloadMeta", {
                            versions: group.entries[0].mcVersions,
                            size: formatBytes(group.entries[0].sizeBytes),
                          })}
                        </a>
                      ) : (
                        // Loaders that need one jar per game version: the
                        // chips are the versions, newest first, so the
                        // choice reads like a version picker.
                        <>
                          <span className="text-fg-subtle block text-xs">{t("mc.setup.pickVersion")}</span>
                          <span className="mt-1.5 flex flex-wrap gap-1">
                            {group.entries.map((d) => (
                              <a
                                key={d.file}
                                href={`/api/mc/downloads/${encodeURIComponent(d.file)}`}
                                download
                                title={t("mc.setup.downloadMeta", {
                                  versions: d.mcVersions,
                                  size: formatBytes(d.sizeBytes),
                                })}
                                className="border-edge bg-bg hover:bg-surface-hover text-fg rounded border px-1.5 py-0.5 font-mono text-[11px] transition-colors"
                              >
                                {d.mcVersions}
                              </a>
                            ))}
                          </span>
                        </>
                      )}
                    </span>
                  </div>
                ))}
              </div>
            )}
          </Step>

          <Step index={2} title={t("mc.setup.step2")} />

          <Step index={3} title={t("mc.setup.step3")}>
            <p className="text-fg-muted mb-3 text-[13px]">{t("mc.setup.step3Body")}</p>
            <div className="space-y-3">
              <CopyBlock label={t("mc.setup.propertiesLabel")} value={propertiesSnippet} />
              <div>
                <div className="mb-1.5 flex items-center gap-2">
                  <p className="text-fg-subtle font-mono text-xs">{t("mc.setup.commandLabel")}</p>
                  <div className="border-edge flex overflow-hidden rounded-md border text-[12px]">
                    {(["ingame", "console"] as const).map((mode) => (
                      <button
                        key={mode}
                        onClick={() => setCmdMode(mode)}
                        className={cn(
                          "px-2.5 py-0.5 font-medium transition-colors",
                          cmdMode === mode
                            ? "bg-accent-soft text-accent-soft-fg"
                            : "text-fg-muted hover:text-fg",
                        )}
                      >
                        {t(mode === "ingame" ? "mc.setup.modeInGame" : "mc.setup.modeConsole")}
                      </button>
                    ))}
                  </div>
                </div>
                <CopyBlock value={pairCommand} />
              </div>
            </div>
          </Step>

          <Step index={4} title={t("mc.setup.step4")}>
            <p className="text-fg-muted text-[13px]">{t("mc.setup.step4Body")}</p>
          </Step>
        </CardBody>
      </Card>
    </div>
  );
}

/** The outstanding code, BIG, with a live countdown to its expiry. */
function PendingCode({
  pending,
  onExpired,
}: {
  pending: McPairingCode;
  onExpired: () => void;
}) {
  const { t } = useTranslation();
  const [remaining, setRemaining] = useState(() => msLeft(pending.expiresAt));

  useEffect(() => {
    setRemaining(msLeft(pending.expiresAt));
    const timer = window.setInterval(() => {
      const left = msLeft(pending.expiresAt);
      setRemaining(left);
      if (left <= 0) {
        window.clearInterval(timer);
        // The server forgets expired codes — refresh so the UI agrees.
        onExpired();
      }
    }, 1000);
    return () => window.clearInterval(timer);
    // onExpired is a stable-enough invalidation callback.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pending.expiresAt, pending.code]);

  const expired = remaining <= 0;

  return (
    <div className="bg-bg-subtle border-edge flex flex-wrap items-center justify-between gap-3 rounded-lg border px-4 py-3.5">
      <div>
        <p className="text-fg-subtle text-xs font-semibold tracking-wide uppercase">
          {t("mc.setup.currentCode")}
        </p>
        <p
          className={cn(
            "mt-0.5 font-mono text-3xl font-semibold tracking-[0.25em]",
            expired ? "text-fg-subtle line-through" : "text-fg",
          )}
        >
          {pending.code}
        </p>
        <p className={cn("mt-1 text-[13px]", expired ? "text-danger-soft-fg" : "text-fg-muted")}>
          {expired ? t("mc.setup.expired") : t("mc.setup.expiresIn", { time: fmtCountdown(remaining) })}
        </p>
      </div>
      {!expired && <CopyButton value={pending.code} />}
    </div>
  );
}

function msLeft(expiresAt: string): number {
  return new Date(expiresAt).getTime() - Date.now();
}

function fmtCountdown(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const minutes = Math.floor(total / 60);
  const seconds = total % 60;
  return `${minutes}:${String(seconds).padStart(2, "0")}`;
}

function Step({ index, title, children }: { index: number; title: string; children?: ReactNode }) {
  return (
    <div className="flex gap-3">
      <span className="bg-accent-soft text-accent-soft-fg mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full text-xs font-semibold">
        {index}
      </span>
      <div className="min-w-0 flex-1">
        <p className="text-fg text-sm font-medium">{title}</p>
        {children && <div className="mt-2">{children}</div>}
      </div>
    </div>
  );
}

function CopyBlock({ label, value }: { label?: string; value: string }) {
  return (
    <div>
      {label && <p className="text-fg-subtle mb-1 font-mono text-xs">{label}</p>}
      <div className="bg-bg-subtle border-edge flex items-start justify-between gap-2 rounded-md border px-3 py-2">
        <pre className="text-fg min-w-0 flex-1 overflow-x-auto font-mono text-[12.5px] whitespace-pre select-all">
          {value}
        </pre>
        <CopyButton value={value} />
      </div>
    </div>
  );
}


/** One member server: identity, live dot, move-to-network, and removal. */
function ServerRow({ instanceId, server }: { instanceId: string; server: import("@/lib/api").McServer }) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();
  const [confirm, setConfirm] = useState(false);
  const integrations = useIntegrations();
  const otherNetworks = (integrations.data ?? []).filter(
    (row) => row.type === "minecraft" && row.id !== instanceId,
  );

  const move = useMutation({
    mutationFn: async (networkId: string) => {
      unwrap(
        await api.PATCH("/mc/{id}/servers/{serverId}", {
          params: { path: { id: instanceId, serverId: server.id } },
          body: { networkId },
        }),
      );
    },
    onSuccess: () => {
      toast("success", t("mc.setup.moved", { name: server.label }));
      void queryClient.invalidateQueries({ queryKey: ["mc"] });
      void queryClient.invalidateQueries({ queryKey: ["integrations"] });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });
  const remove = useMutation({
    mutationFn: async () => {
      unwrap(
        await api.DELETE("/mc/{id}/servers/{serverId}", {
          params: { path: { id: instanceId, serverId: server.id } },
        }),
      );
    },
    onSuccess: () => {
      setConfirm(false);
      void queryClient.invalidateQueries({ queryKey: mcOverviewKey(instanceId) });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <div className="border-edge bg-surface flex items-center gap-3 rounded-lg border px-3 py-2.5">
      <Server className="text-fg-subtle size-4 shrink-0" />
      <div className="min-w-0 flex-1">
        <p className="text-fg flex items-center gap-2 text-[13.5px] font-medium">
          {server.label}
          <StatusDot tone={server.connected ? "success" : "neutral"} pulse={!server.connected} />
        </p>
        <p className="text-fg-subtle truncate text-[12px]">
          {[loaderLabel(server.loader), server.mcVersion, server.remoteIp,
            server.companionVersion ? t("mc.companionVersion", { version: server.companionVersion }) : null]
            .filter(Boolean)
            .join(" · ") || t("mc.setup.neverConnected")}
        </p>
      </div>
      {server.pairedAt && (
        <Badge tone="success">
          <CheckCircle2 className="size-3" />
          {formatRelative(server.pairedAt)}
        </Badge>
      )}
      {otherNetworks.length > 0 && (
        <Dropdown>
          <DropdownTrigger asChild>
            <Button size="sm" variant="ghost" aria-label={t("mc.setup.moveServer")} loading={move.isPending}>
              <ArrowRightLeft className="size-4" />
            </Button>
          </DropdownTrigger>
          <DropdownContent align="end">
            {otherNetworks.map((network) => (
              <DropdownItem key={network.id} onSelect={() => move.mutate(network.id)}>
                {t("mc.setup.moveTo", { name: network.name })}
              </DropdownItem>
            ))}
          </DropdownContent>
        </Dropdown>
      )}
      <Button size="sm" variant="ghost" aria-label={t("mc.setup.removeServer")} onClick={() => setConfirm(true)}>
        <Trash2 className="size-4" />
      </Button>
      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title={t("mc.setup.removeTitle", { name: server.label })}
        body={t("mc.setup.removeBody")}
        confirmLabel={t("mc.setup.removeServer")}
        loading={remove.isPending}
        onConfirm={() => remove.mutate()}
      />
    </div>
  );
}

/**
 * The network's SSH host access: configured through the integration's own
 * config/secrets (edit dialog), surfaced and testable here — this is the
 * credential full restarts will ride on.
 */
function SshCard({ instanceId, configured }: { instanceId: string; configured: boolean }) {
  const { t } = useTranslation();
  const test = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/mc/{id}/ssh/test", { params: { path: { id: instanceId } } })),
  });

  return (
    <Card>
      <CardHeader
        title={t("mc.setup.sshTitle")}
        description={t("mc.setup.sshIntro")}
        actions={
          configured ? (
            <Button size="sm" variant="secondary" loading={test.isPending} onClick={() => test.mutate()}>
              <Terminal className="size-3.5" />
              {t("mc.setup.sshTest")}
            </Button>
          ) : undefined
        }
      />
      <CardBody className="space-y-2">
        {configured ? (
          <p className="text-fg-muted text-[13px]">{t("mc.setup.sshConfigured")}</p>
        ) : (
          <p className="text-fg-muted text-[13px]">{t("mc.setup.sshNotConfigured")}</p>
        )}
        {test.isSuccess && (
          <p className={cn("text-[12.5px] whitespace-pre-wrap", test.data.ok ? "text-success" : "text-danger")}>
            {test.data.ok ? t("mc.setup.sshOk", { detail: test.data.message }) : test.data.message}
          </p>
        )}
        {test.isError && (
          <p className="text-danger text-[12.5px]">
            {isApiError(test.error) ? test.error.message : t("common.unknownError")}
          </p>
        )}
      </CardBody>
    </Card>
  );
}
