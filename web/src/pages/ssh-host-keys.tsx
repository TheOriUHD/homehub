import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { CircleCheck, Fingerprint, ShieldAlert, ShieldQuestion, TriangleAlert } from "lucide-react";
import { api, isApiError, unwrap, type SSHHostKeyCheck, type SSHKnownHost } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { cn } from "@/lib/utils";
import { Button } from "@/ui/button";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { Dialog, DialogContent } from "@/ui/dialog";
import { PageLoader } from "@/ui/misc";
import { EmptyState } from "@/ui/states";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { useToast } from "@/ui/toast";

/**
 * Getting back in after a machine's SSH host key changes.
 *
 * Pinning is trust-on-first-use, so a rebuilt machine locks HomeHub out of
 * itself: the terminal, anything dialling it over SSH, and the assistant all
 * refuse at once. That is the correct default — a changed key means a rebuild
 * OR someone in the middle, and nothing can tell those apart from here. Only a
 * person can, which is what this screen is for: it shows both fingerprints and
 * lets them say which one is the machine they think it is.
 *
 * The pin is keyed by address, so one decision restores every consumer.
 */

// Same resource path as the registry page. Kept local rather than imported,
// because that page imports this one and a cycle for one string is a poor trade.
const SSH_RES = "system/ssh";

export function useKnownHosts(enabled = true) {
  return useQuery({
    queryKey: ["ssh-known-hosts"],
    queryFn: async () => unwrap(await api.GET("/ssh-known-hosts")),
    enabled,
  });
}

/** hostKeyChanged spots the pin failure in a connection error, so the fix can
 *  be offered where the error is read rather than somewhere else. */
export function hostKeyChanged(message: string | undefined): boolean {
  return /host key (has )?changed/i.test(message ?? "");
}

const TONES: Record<string, { icon: typeof CircleCheck; className: string }> = {
  match: { icon: CircleCheck, className: "text-success" },
  changed: { icon: ShieldAlert, className: "text-danger" },
  unpinned: { icon: ShieldQuestion, className: "text-fg-muted" },
  unreachable: { icon: TriangleAlert, className: "text-warning" },
};

/**
 * The dialog: check on open, then offer what the answer allows. Trusting names
 * the exact fingerprint being accepted, and the server re-reads the key before
 * pinning it, so a screen left open overnight cannot approve a key that has
 * changed again since.
 */
export function HostKeyDialog({
  host,
  port,
  label,
  onClose,
}: {
  host: string;
  port: number;
  label: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const { can } = useAuth();
  const toast = useToast();
  const queryClient = useQueryClient();
  const [result, setResult] = useState<SSHHostKeyCheck | null>(null);
  const canConfigure = can(SSH_RES, "configure");

  const check = useMutation({
    mutationFn: async () => unwrap(await api.POST("/ssh-known-hosts/check", { body: { host, port } })),
    onSuccess: setResult,
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const trust = useMutation({
    mutationFn: async (fingerprint: string) =>
      unwrap(await api.POST("/ssh-known-hosts/trust", { body: { host, port, fingerprint } })),
    onSuccess: (data) => {
      setResult(data);
      void queryClient.invalidateQueries({ queryKey: ["ssh-known-hosts"] });
      toast("success", t("ssh.hostKey.trusted", { name: label }));
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const forget = useMutation({
    mutationFn: async () => unwrap(await api.POST("/ssh-known-hosts/forget", { body: { host, port } })),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["ssh-known-hosts"] });
      toast("success", t("ssh.hostKey.forgot", { name: label }));
      onClose();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  // Checking is the whole point of opening this, so it happens on open rather
  // than behind a button nobody would decline to press.
  const run = check.mutate;
  useEffect(() => {
    run();
  }, [run]);

  const tone = result ? TONES[result.status] : null;
  const Icon = tone?.icon;

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent
        title={t("ssh.hostKey.dialogTitle", { name: label })}
        description={`${host}:${port}`}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.close")}
            </Button>
            {canConfigure && result?.status === "changed" && result.observed && (
              <Button variant="primary" loading={trust.isPending} onClick={() => trust.mutate(result.observed!)}>
                {t("ssh.hostKey.trust")}
              </Button>
            )}
          </>
        }
      >
        <div className="space-y-3">
          {check.isPending && !result && <PageLoader className="py-6" />}

          {result && (
            <>
              <p className={cn("flex items-start gap-2 text-[13px] leading-relaxed", tone?.className)}>
                {Icon && <Icon className="mt-0.5 size-4 shrink-0" />}
                <span>
                  <span className="font-medium">{t(`ssh.hostKey.status.${result.status}`)}</span>
                  {result.message ? ` ${result.message}` : ""}
                </span>
              </p>

              <div className="space-y-2">
                <Fingerprints
                  label={t("ssh.hostKey.pinnedLabel")}
                  value={result.pinned}
                  empty={t("ssh.hostKey.nonePinned")}
                  stale={result.status === "changed"}
                />
                <Fingerprints
                  label={t("ssh.hostKey.observedLabel")}
                  value={result.observed}
                  empty={t("ssh.hostKey.noAnswer")}
                />
              </div>

              {result.alsoStale && result.alsoStale.length > 0 && (
                <p className="text-fg-muted text-[12.5px] leading-relaxed">
                  {t("ssh.hostKey.alsoStale", { hosts: result.alsoStale.join(", ") })}
                </p>
              )}

              {result.status === "changed" && (
                <p className="bg-danger-soft text-danger-soft-fg rounded-lg px-3 py-2.5 text-[12.5px] leading-relaxed">
                  {t("ssh.hostKey.caution")}
                </p>
              )}

              {result.status === "unreachable" && (
                <p className="text-fg-muted text-[12.5px] leading-relaxed">{t("ssh.hostKey.unreachableHint")}</p>
              )}
            </>
          )}

          <div className="flex flex-wrap items-center gap-2 pt-1">
            <Button size="sm" variant="secondary" loading={check.isPending} onClick={() => check.mutate()}>
              <Fingerprint className="size-3.5" />
              {t("ssh.hostKey.recheck")}
            </Button>
            {canConfigure && result && result.status !== "unpinned" && (
              <Button size="sm" variant="ghost" loading={forget.isPending} onClick={() => forget.mutate()}>
                {t("ssh.hostKey.forget")}
              </Button>
            )}
          </div>
          {canConfigure && <p className="text-fg-subtle text-[12px]">{t("ssh.hostKey.forgetHint")}</p>}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function Fingerprints({
  label,
  value,
  empty,
  stale,
}: {
  label: string;
  value?: string;
  empty: string;
  stale?: boolean;
}) {
  return (
    <div className="border-edge rounded-lg border px-3 py-2">
      <p className="text-fg-subtle text-[11px] font-medium tracking-wide uppercase">{label}</p>
      {value ? (
        // Wraps rather than truncates: a fingerprint is only useful in full,
        // and the whole decision is comparing two of them character by character.
        <code className={cn("text-fg mt-0.5 block font-mono text-[12px] break-all", stale && "line-through opacity-60")}>
          {value}
        </code>
      ) : (
        <p className="text-fg-muted mt-0.5 text-[12.5px]">{empty}</p>
      )}
    </div>
  );
}

/**
 * Every pin HomeHub holds, including addresses with no registry entry — a
 * Docker daemon over SSH or a Minecraft host pins a key without being a
 * registry row, and when one of those breaks this is the only place it shows.
 */
export function KnownHostsCard() {
  const { t } = useTranslation();
  const known = useKnownHosts();
  const [inspecting, setInspecting] = useState<SSHKnownHost | null>(null);

  const rows = known.data?.items ?? [];

  return (
    <>
      <Card>
        <CardHeader title={t("ssh.hostKey.knownTitle")} description={t("ssh.hostKey.knownSubtitle")} />
        <CardBody className="p-0">
          {known.isLoading ? (
            <PageLoader className="py-8" />
          ) : rows.length === 0 ? (
            <EmptyState icon={Fingerprint} title={t("ssh.hostKey.knownEmpty")} body={t("ssh.hostKey.knownEmptyBody")} />
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <THead>
                  <TR>
                    <TH>{t("ssh.hostKey.cols.address")}</TH>
                    <TH>{t("ssh.hostKey.cols.usedBy")}</TH>
                    <TH>{t("ssh.hostKey.cols.fingerprint")}</TH>
                    <TH aria-label={t("common.actions")} />
                  </TR>
                </THead>
                <TBody>
                  {rows.map((row) => (
                    <TR key={`${row.host}:${row.port}`}>
                      <TD>
                        <code className="font-mono text-[12.5px]">
                          {row.host}
                          {row.port !== 22 ? `:${row.port}` : ""}
                        </code>
                      </TD>
                      <TD>
                        {row.usedBy.length > 0 ? (
                          <span className="text-fg-muted text-[12.5px]">{row.usedBy.join(", ")}</span>
                        ) : (
                          <span className="text-fg-subtle text-[12.5px]">—</span>
                        )}
                      </TD>
                      <TD>
                        <code className="text-fg-muted font-mono text-[11.5px] break-all">{row.fingerprint}</code>
                      </TD>
                      <TD>
                        <span className="flex justify-end">
                          <Button size="sm" variant="ghost" onClick={() => setInspecting(row)}>
                            <Fingerprint className="size-4" />
                            {t("ssh.hostKey.check")}
                          </Button>
                        </span>
                      </TD>
                    </TR>
                  ))}
                </TBody>
              </Table>
            </div>
          )}
        </CardBody>
      </Card>

      {inspecting && (
        <HostKeyDialog
          host={inspecting.host}
          port={inspecting.port}
          label={inspecting.usedBy[0] ?? inspecting.host}
          onClose={() => setInspecting(null)}
        />
      )}
    </>
  );
}
