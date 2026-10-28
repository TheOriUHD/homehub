import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, isApiError, unwrap } from "@/lib/api";
import { usePreferredOrigin } from "@/lib/origin";
import { Button } from "@/ui/button";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { CopyButton } from "@/ui/misc";
import { useToast } from "@/ui/toast";

/**
 * Helper setup for an existing Caddy system: the install one-liner (served
 * from this HomeHub's own origin, no internet required), the setup command,
 * and re-pairing with a fresh six-digit code. Pairing is redeemed
 * server-side — the helper token never reaches the browser.
 */
export function CaddySetupDialog({ instanceId, onClose }: { instanceId: string; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const [code, setCode] = useState("");

  const origin = usePreferredOrigin();
  const installCmd = `curl -fsSL ${origin}/helper/install.sh | sudo bash`;

  const repair = useMutation({
    mutationFn: async () => {
      unwrap(
        await api.POST("/caddy/{id}/repair", {
          params: { path: { id: instanceId } },
          body: { code: code.replace(/\s/g, "") },
        }),
      );
    },
    onSuccess: () => {
      toast("success", t("caddy.setup.repairSuccess"));
      setCode("");
      onClose();
    },
    onError: (err) =>
      toast("error", t("caddy.setup.repairFailed"), isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={t("caddy.setup.title")}
        footer={
          <Button variant="ghost" onClick={onClose}>
            {t("common.close")}
          </Button>
        }
      >
        <div className="space-y-5">
          <SetupStep n={1} title={t("caddy.setup.installStep")}>
            <CopyBlock value={installCmd} />
          </SetupStep>
          <SetupStep n={2} title={t("caddy.setup.setupStep")}>
            <CopyBlock value="sudo caddy-reader setup" />
            <p className="text-fg-muted mt-2 text-[13px]">{t("caddy.setup.setupNote")}</p>
          </SetupStep>
          <SetupStep n={3} title={t("caddy.setup.repairStep")}>
            <p className="text-fg-muted mb-2 text-[13px]">{t("caddy.setup.repairHint")}</p>
            <div className="flex items-end gap-2">
              <div className="flex-1">
                <Field label={t("integrations.caddy.code")}>
                  {(p) => (
                    <Input
                      {...p}
                      className="font-mono text-[15px] tracking-widest"
                      placeholder="481 502"
                      value={code}
                      onChange={(e) => setCode(e.target.value)}
                    />
                  )}
                </Field>
              </div>
              <Button
                variant="primary"
                loading={repair.isPending}
                disabled={code.replace(/\s/g, "").length < 6}
                onClick={() => repair.mutate()}
              >
                {t("caddy.setup.repairButton")}
              </Button>
            </div>
          </SetupStep>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function SetupStep({ n, title, children }: { n: number; title: string; children: React.ReactNode }) {
  return (
    <div className="flex gap-3">
      <span className="bg-accent-soft text-accent-soft-fg flex size-6 shrink-0 items-center justify-center rounded-full text-[12px] font-semibold">
        {n}
      </span>
      <div className="min-w-0 flex-1">
        <p className="text-fg text-sm font-medium">{title}</p>
        <div className="mt-2">{children}</div>
      </div>
    </div>
  );
}

function CopyBlock({ value }: { value: string }) {
  return (
    <div className="bg-bg-subtle border-edge flex items-start justify-between gap-2 rounded-md border px-3 py-2">
      <pre className="text-fg min-w-0 flex-1 overflow-x-auto font-mono text-[12.5px] whitespace-pre select-all">
        {value}
      </pre>
      <CopyButton value={value} />
    </div>
  );
}
