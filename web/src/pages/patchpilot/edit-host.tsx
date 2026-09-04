import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { PatchpilotHost } from "@/lib/api";
import { SSHHostPicker, useSSHHosts } from "@/pages/ssh-hosts";
import { Button } from "@/ui/button";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { Switch } from "@/ui/switch";
import { useToast } from "@/ui/toast";
import { useBindHost, useUpdateHost } from "./shared";

/**
 * EditHostDialog is the add-host form again, for a host that already exists:
 * where it lives (a registry entry, or an address typed by hand), what it is
 * called, and whether it may reboot itself. Saving applies only what changed,
 * binding first — an address typed for a host that is being bound is
 * meaningless, the entry's address wins from that moment on.
 */
export function EditHostDialog({
  instanceId,
  host,
  open,
  onOpenChange,
}: {
  instanceId: string;
  host: PatchpilotHost;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const update = useUpdateHost(instanceId);
  const bind = useBindHost(instanceId);
  const registryHosts = useSSHHosts(open);

  const [sshHostId, setSshHostId] = useState<string | null>(host.sshHostId ?? null);
  const [hostname, setHostname] = useState(host.hostname);
  const [sshPort, setSshPort] = useState(String(host.sshPort));
  const [name, setName] = useState(host.name);
  const [autoReboot, setAutoReboot] = useState(host.autoReboot);
  const [saving, setSaving] = useState(false);

  // Reopening shows the host as it is now, not as the last edit left the form.
  useEffect(() => {
    if (!open) return;
    setSshHostId(host.sshHostId ?? null);
    setHostname(host.hostname);
    setSshPort(String(host.sshPort));
    setName(host.name);
    setAutoReboot(host.autoReboot);
  }, [open, host]);

  const registryHost = registryHosts.data?.find((h) => h.id === sshHostId) ?? null;
  const port = Number(sshPort);
  const manualValid = Boolean(sshHostId) || (hostname.trim() !== "" && port >= 1 && port <= 65535);
  const canSave = manualValid && name.trim() !== "" && !saving;

  const errMsg = (e: unknown): string => {
    const m = (e as { message?: unknown } | null | undefined)?.message;
    return typeof m === "string" && m ? m : t("common.unknownError");
  };

  const save = async () => {
    setSaving(true);
    try {
      const bindingChanged = sshHostId !== (host.sshHostId ?? null);
      if (bindingChanged) await bind.mutateAsync({ hostId: host.id, sshHostId });
      const body: { name?: string; hostname?: string; sshPort?: number; autoReboot?: boolean } = {};
      if (name.trim() !== host.name) body.name = name.trim();
      if (autoReboot !== host.autoReboot) body.autoReboot = autoReboot;
      if (!sshHostId && (hostname.trim() !== host.hostname || port !== host.sshPort || bindingChanged)) {
        body.hostname = hostname.trim();
        body.sshPort = port;
      }
      if (Object.keys(body).length > 0) await update.mutateAsync({ hostId: host.id, body });
      toast("success", t("patchpilot.editHost.saved"));
      onOpenChange(false);
    } catch (e) {
      toast("error", errMsg(e));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={t("patchpilot.editHost.title", { name: host.name })}
        description={t("patchpilot.editHost.intro")}
        footer={
          <>
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              {t("common.cancel")}
            </Button>
            <Button variant="primary" disabled={!canSave} loading={saving} onClick={() => void save()}>
              {t("common.save")}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label={t("patchpilot.addHost.source")} hint={t("patchpilot.addHost.sourceHint")}>
            {() => (
              <SSHHostPicker
                value={sshHostId}
                onChange={(id) => {
                  const picked = registryHosts.data?.find((h) => h.id === id) ?? null;
                  if (picked && (name.trim() === "" || name === registryHost?.name)) setName(picked.name);
                  setSshHostId(id);
                }}
              />
            )}
          </Field>

          {registryHost ? (
            <div className="space-y-1">
              <p className="text-fg-muted font-mono text-[12.5px]">
                {registryHost.username}@{registryHost.hostname}:{registryHost.port}
              </p>
              <p className="text-fg-subtle text-[12px]">{t("patchpilot.editHost.addressFromRegistry")}</p>
            </div>
          ) : (
            <div className="grid gap-4 sm:grid-cols-[1fr_7rem]">
              <Field label={t("patchpilot.addHost.hostname")} hint={t("patchpilot.editHost.addressHint")}>
                {(p) => (
                  <Input
                    {...p}
                    spellCheck={false}
                    autoComplete="off"
                    className="font-mono text-[13px]"
                    value={hostname}
                    onChange={(e) => setHostname(e.target.value)}
                  />
                )}
              </Field>
              <Field label={t("patchpilot.addHost.port")}>
                {(p) => <Input {...p} type="number" value={sshPort} onChange={(e) => setSshPort(e.target.value)} />}
              </Field>
            </div>
          )}

          <Field label={t("patchpilot.addHost.name")}>
            {(p) => <Input {...p} value={name} onChange={(e) => setName(e.target.value)} />}
          </Field>

          <div className="border-edge flex items-start gap-3 rounded-lg border px-3.5 py-2.5">
            <div className="min-w-0 flex-1">
              <p className="text-fg text-[13px] font-medium">{t("patchpilot.addHost.autoReboot")}</p>
              <p className="text-fg-subtle text-[12px]">{t("patchpilot.addHost.autoRebootHint")}</p>
            </div>
            <Switch checked={autoReboot} onCheckedChange={setAutoReboot} />
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
