import { useState } from "react";
import { useParams } from "@tanstack/react-router";
import { useTranslation, Trans } from "react-i18next";
import {
  Cloud,
  CloudOff,
  ExternalLink,
  Globe,
  MoreHorizontal,
  Pencil,
  Plus,
  Rocket,
  Trash2,
  Unlink,
} from "lucide-react";
import { type CaddyDnsRecord, type CaddyDnsState, type CaddyPublishResult } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { cn } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Card, CardBody } from "@/ui/card";
import { ConfirmDialog } from "@/ui/confirm";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Dropdown, DropdownContent, DropdownItem, DropdownSeparator, DropdownTrigger } from "@/ui/dropdown";
import { Field } from "@/ui/field";
import { Input, PasswordInput } from "@/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { Switch } from "@/ui/switch";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { Tooltip } from "@/ui/tooltip";
import { useToast } from "@/ui/toast";
import {
  caddyResource,
  useCaddyDns,
  useCaddyLive,
  useCaddyPublish,
  useCfLink,
  useCfUnlink,
  useDnsCreate,
  useDnsDelete,
  useDnsUpdate,
} from "./shared";

/**
 * CaddyDnsPage is the Cloudflare half of the reverse-proxy workflow: the
 * linked zone's records matched against Caddy sites, record management, and
 * the one-shot "publish" (DNS record + Caddy site together). Unlinked, it
 * renders the guided linking card.
 */
export function CaddyDnsPage() {
  const { instanceId } = useParams({ from: "/shell/caddy/$instanceId/dns" });
  const { can } = useAuth();
  const dns = useCaddyDns(instanceId);
  useCaddyLive(instanceId);

  const canControl = can(caddyResource(instanceId), "control");
  const canConfigure = can(caddyResource(instanceId), "configure");

  if (dns.isLoading) return <PageLoader />;
  if (dns.isError) {
    return (
      <Card>
        <ErrorState error={dns.error} onRetry={() => void dns.refetch()} />
      </Card>
    );
  }
  if (!dns.data!.linked) {
    return <CfLinkCard instanceId={instanceId} canConfigure={canConfigure} />;
  }
  return <DnsBoard instanceId={instanceId} state={dns.data!.state!} canControl={canControl} canConfigure={canConfigure} />;
}

// ---- linking -------------------------------------------------------------------

function CfLinkCard({ instanceId, canConfigure }: { instanceId: string; canConfigure: boolean }) {
  const { t } = useTranslation();
  return (
    <Card>
      <CardBody className="mx-auto max-w-xl space-y-4 py-8 text-center">
        <span className="bg-accent-soft text-accent-soft-fg mx-auto flex size-12 items-center justify-center rounded-2xl">
          <Cloud className="size-6" />
        </span>
        <div>
          <h2 className="text-fg text-lg font-semibold">{t("caddy.dns.link.title")}</h2>
          <p className="text-fg-muted mt-1 text-sm">{t("caddy.dns.link.body")}</p>
        </div>
        {canConfigure ? (
          <CfLinkForm instanceId={instanceId} />
        ) : (
          <p className="text-fg-subtle text-[13px]">{t("caddy.dns.link.noPermission")}</p>
        )}
      </CardBody>
    </Card>
  );
}

/** The link flow: paste token → verify (zones come back) → pick zone, set the
 * default target and proxied mode → link. Also reused for re-configuration. */
function CfLinkForm({
  instanceId,
  initial,
  onDone,
}: {
  instanceId: string;
  initial?: { zoneName: string; recordContent: string; proxied: boolean };
  onDone?: () => void;
}) {
  const { t } = useTranslation();
  const linkMut = useCfLink(instanceId);
  const toast = useToast();
  const [token, setToken] = useState("");
  const [zones, setZones] = useState<{ id: string; name: string }[] | null>(null);
  const [zoneId, setZoneId] = useState("");
  const [content, setContent] = useState(initial?.recordContent ?? "");
  const [proxied, setProxied] = useState(initial?.proxied ?? false);
  const reconfiguring = Boolean(initial);

  const submit = () => {
    linkMut.mutate(
      {
        token: token.trim() || undefined,
        zoneId: zoneId || undefined,
        recordContent: content.trim(),
        proxied,
      },
      {
        onSuccess: (res) => {
          if (!res.linked && res.zones) {
            setZones(res.zones);
            if (res.zones.length > 0) setZoneId(res.zones[0].id);
            return;
          }
          toast("success", t("caddy.dns.link.linked", { zone: res.zoneName ?? "" }));
          onDone?.();
        },
        onError: (e) => toast("error", t("caddy.dns.link.failed"), String(e)),
      },
    );
  };

  return (
    <div className="space-y-4 text-left">
      <Field
        label={t("caddy.dns.link.token")}
        hint={reconfiguring ? t("caddy.dns.link.tokenKeep") : t("caddy.dns.link.tokenHint")}
      >
        {(p) => (
          <PasswordInput
            {...p}
            className="font-mono text-[13px]"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder={reconfiguring ? "••••••••" : ""}
          />
        )}
      </Field>
      {!reconfiguring && (
        <p className="text-fg-subtle text-[12px]">
          <Trans
            i18nKey="caddy.dns.link.tokenHowTo"
            components={{
              a: (
                <a
                  href="https://dash.cloudflare.com/profile/api-tokens"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-accent inline-flex items-center gap-0.5 hover:underline"
                />
              ),
            }}
          />
          <ExternalLink className="ml-0.5 inline size-3 align-[-1px]" />
        </p>
      )}
      {zones && zones.length > 0 && (
        <Field label={t("caddy.dns.link.zone")} hint={t("caddy.dns.link.zoneHint")}>
          {(p) => (
            <Select value={zoneId} onValueChange={setZoneId}>
              <SelectTrigger id={p.id}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {zones.map((z) => (
                  <SelectItem key={z.id} value={z.id}>
                    {z.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        </Field>
      )}
      <Field label={t("caddy.dns.link.content")} hint={t("caddy.dns.link.contentHint")}>
        {(p) => <Input {...p} placeholder="192.168.1.10" value={content} onChange={(e) => setContent(e.target.value)} />}
      </Field>
      <label className="flex items-center justify-between gap-3">
        <span>
          <span className="text-fg block text-[13.5px] font-medium">{t("caddy.dns.link.proxied")}</span>
          <span className="text-fg-subtle block text-[12px]">{t("caddy.dns.link.proxiedHint")}</span>
        </span>
        <Switch checked={proxied} onCheckedChange={setProxied} />
      </label>
      <Button variant="primary" className="w-full" loading={linkMut.isPending} onClick={submit} disabled={!reconfiguring && !token.trim() && !zones}>
        {zones ? t("caddy.dns.link.confirm") : reconfiguring ? t("common.save") : t("caddy.dns.link.verify")}
      </Button>
    </div>
  );
}

// ---- the linked board ------------------------------------------------------------

function DnsBoard({
  instanceId,
  state,
  canControl,
  canConfigure,
}: {
  instanceId: string;
  state: CaddyDnsState;
  canControl: boolean;
  canConfigure: boolean;
}) {
  const { t } = useTranslation();
  const unlink = useCfUnlink(instanceId);
  const del = useDnsDelete(instanceId);
  const toast = useToast();
  const [publish, setPublish] = useState<{ name?: string; createDns?: boolean } | null>(null);
  const [record, setRecord] = useState<{ edit?: CaddyDnsRecord; presetName?: string } | null>(null);
  const [deleting, setDeleting] = useState<CaddyDnsRecord | null>(null);
  const [confirmUnlink, setConfirmUnlink] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2.5">
        <Badge tone="accent">
          <Globe className="size-3" />
          {state.zoneName}
        </Badge>
        {state.recordContent && (
          <span className="text-fg-subtle text-[12.5px]">
            {t("caddy.dns.defaultTarget")}: <span className="text-fg-muted font-mono">{state.recordContent}</span>
          </span>
        )}
        <div className="ml-auto flex items-center gap-2">
          {canControl && (
            <>
              <Button variant="secondary" onClick={() => setRecord({})}>
                <Plus className="size-4" />
                {t("caddy.dns.addRecord")}
              </Button>
              <Button variant="primary" onClick={() => setPublish({})}>
                <Rocket className="size-4" />
                {t("caddy.dns.publish")}
              </Button>
            </>
          )}
          {canConfigure && (
            <Dropdown>
              <DropdownTrigger asChild>
                <Button size="sm" variant="ghost" aria-label={t("common.actions")}>
                  <MoreHorizontal className="size-4" />
                </Button>
              </DropdownTrigger>
              <DropdownContent align="end">
                <DropdownItem onSelect={() => setSettingsOpen(true)}>
                  <Pencil className="size-3.5" />
                  {t("caddy.dns.settings")}
                </DropdownItem>
                <DropdownSeparator />
                <DropdownItem destructive onSelect={() => setConfirmUnlink(true)}>
                  <Unlink className="size-3.5" />
                  {t("caddy.dns.unlink")}
                </DropdownItem>
              </DropdownContent>
            </Dropdown>
          )}
        </div>
      </div>

      {state.missingDns.length > 0 && canControl && (
        <div className="bg-warning-soft text-warning-soft-fg flex flex-wrap items-center gap-2 rounded-lg px-4 py-2.5 text-[13px]">
          <span>{t("caddy.dns.missing", { count: state.missingDns.length })}</span>
          {state.missingDns.map((d) => (
            <button
              key={d}
              onClick={() => setRecord({ presetName: d })}
              className="border-warning/40 hover:bg-warning/10 rounded border px-1.5 py-0.5 font-mono text-[12px] transition-colors"
              title={t("caddy.dns.missingAdd", { domain: d })}
            >
              {d} +
            </button>
          ))}
        </div>
      )}

      <Card>
        {state.records.length === 0 ? (
          <EmptyState icon={Globe} title={t("caddy.dns.empty.title")} body={t("caddy.dns.empty.body")} />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-[13px]">
              <thead>
                <tr className="text-fg-subtle border-edge border-b text-left text-[11px] font-semibold uppercase tracking-wide">
                  <th className="px-4 py-2.5">{t("caddy.dns.cols.type")}</th>
                  <th className="px-3 py-2.5">{t("caddy.dns.cols.name")}</th>
                  <th className="px-3 py-2.5">{t("caddy.dns.cols.content")}</th>
                  <th className="px-3 py-2.5">{t("caddy.dns.cols.proxy")}</th>
                  <th className="px-3 py-2.5">{t("caddy.dns.cols.route")}</th>
                  <th className="px-4 py-2.5 text-right" />
                </tr>
              </thead>
              <tbody className="divide-edge/60 divide-y">
                {state.records.map((rec) => (
                  <tr key={rec.id} className="hover:bg-bg-subtle/50">
                    <td className="px-4 py-2">
                      <Badge tone={rec.type === "A" || rec.type === "AAAA" ? "info" : rec.type === "CNAME" ? "accent" : "neutral"}>{rec.type}</Badge>
                    </td>
                    <td className="text-fg max-w-56 truncate px-3 py-2 font-mono text-[12.5px]" title={rec.name}>
                      {rec.name}
                    </td>
                    <td className="text-fg-muted max-w-64 truncate px-3 py-2 font-mono text-[12.5px]" title={rec.content}>
                      {rec.content}
                    </td>
                    <td className="px-3 py-2">
                      <Tooltip label={rec.proxied ? t("caddy.dns.proxied") : t("caddy.dns.dnsOnly")}>
                        {rec.proxied ? <Cloud className="size-4 text-orange-400" /> : <CloudOff className="text-fg-subtle size-4" />}
                      </Tooltip>
                    </td>
                    <td className="px-3 py-2">
                      {rec.routeDomain ? (
                        <span className="text-success inline-flex items-center gap-1.5 text-[12.5px]">
                          ✓ <span className="text-fg-muted font-mono text-[12px]">{rec.routeUpstream}</span>
                        </span>
                      ) : ["A", "AAAA", "CNAME"].includes(rec.type) ? (
                        canControl ? (
                          <button
                            onClick={() => setPublish({ name: rec.name, createDns: false })}
                            className="text-accent text-[12.5px] hover:underline"
                          >
                            {t("caddy.dns.createRoute")}
                          </button>
                        ) : (
                          <span className="text-fg-subtle text-[12.5px]">{t("caddy.dns.noRoute")}</span>
                        )
                      ) : (
                        <span className="text-fg-subtle">—</span>
                      )}
                    </td>
                    <td className="px-4 py-2 text-right">
                      {canControl && (
                        <div className="flex items-center justify-end gap-1">
                          <Button size="sm" variant="ghost" aria-label={t("common.edit")} onClick={() => setRecord({ edit: rec })}>
                            <Pencil className="size-3.5" />
                          </Button>
                          <Button size="sm" variant="ghost" aria-label={t("common.delete")} onClick={() => setDeleting(rec)}>
                            <Trash2 className="text-danger size-3.5" />
                          </Button>
                        </div>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      {publish && (
        <PublishDialog
          instanceId={instanceId}
          state={state}
          preset={publish}
          onClose={() => setPublish(null)}
        />
      )}
      {record && (
        <RecordDialog instanceId={instanceId} state={state} edit={record.edit} presetName={record.presetName} onClose={() => setRecord(null)} />
      )}
      <ConfirmDialog
        open={Boolean(deleting)}
        onOpenChange={(o) => !o && setDeleting(null)}
        tone="danger"
        title={t("caddy.dns.deleteConfirm.title")}
        body={t("caddy.dns.deleteConfirm.body", { name: deleting?.name ?? "" })}
        confirmLabel={t("common.delete")}
        loading={del.isPending}
        onConfirm={() =>
          deleting &&
          del.mutate(deleting.id, {
            onSuccess: () => setDeleting(null),
            onError: (e) => {
              setDeleting(null);
              toast("error", t("caddy.dns.toast.failed"), String(e));
            },
          })
        }
      />
      <ConfirmDialog
        open={confirmUnlink}
        onOpenChange={setConfirmUnlink}
        tone="danger"
        title={t("caddy.dns.unlinkConfirm.title")}
        body={t("caddy.dns.unlinkConfirm.body", { zone: state.zoneName })}
        confirmLabel={t("caddy.dns.unlink")}
        loading={unlink.isPending}
        onConfirm={() =>
          unlink.mutate(undefined, {
            onSuccess: () => setConfirmUnlink(false),
            onError: (e) => toast("error", t("caddy.dns.toast.failed"), String(e)),
          })
        }
      />
      {settingsOpen && (
        <Dialog open onOpenChange={(o) => !o && setSettingsOpen(false)}>
          <DialogContent title={t("caddy.dns.settings")}>
            <CfLinkForm
              instanceId={instanceId}
              initial={{ zoneName: state.zoneName, recordContent: state.recordContent, proxied: state.proxiedDefault }}
              onDone={() => setSettingsOpen(false)}
            />
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}

// ---- publish dialog ---------------------------------------------------------------

function PublishDialog({
  instanceId,
  state,
  preset,
  onClose,
}: {
  instanceId: string;
  state: CaddyDnsState;
  preset: { name?: string; createDns?: boolean };
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const publish = useCaddyPublish(instanceId);
  const toast = useToast();
  // Preset name arrives as a FQDN from the route column; show the short form.
  const short = (fqdn: string) =>
    fqdn === state.zoneName ? "@" : fqdn.endsWith("." + state.zoneName) ? fqdn.slice(0, -(state.zoneName.length + 1)) : fqdn;
  const [name, setName] = useState(preset.name ? short(preset.name) : "");
  const [host, setHost] = useState("");
  const [port, setPort] = useState("");
  const [scheme, setScheme] = useState("http");
  const [createDns, setCreateDns] = useState(preset.createDns ?? true);
  const [proxied, setProxied] = useState(state.proxiedDefault);
  const [result, setResult] = useState<CaddyPublishResult | null>(null);

  const fqdn = name.trim() === "" || name.trim() === "@" ? state.zoneName : name.includes(".") && name.endsWith(state.zoneName) ? name : `${name.trim()}.${state.zoneName}`;

  const submit = () => {
    publish.mutate(
      {
        name: name.trim() || "@",
        upstreamHost: host.trim(),
        upstreamPort: port.trim() || undefined,
        upstreamScheme: scheme !== "http" ? scheme : undefined,
        createDns,
        proxied: createDns ? proxied : undefined,
      },
      {
        onSuccess: (res) => {
          setResult(res);
          if (res.siteStatus === "created" && (res.dnsStatus === "created" || res.dnsStatus === "exists" || res.dnsStatus === "skipped" || res.dnsStatus === "unlinked")) {
            toast("success", t("caddy.dns.toast.published", { domain: res.domain }));
          }
        },
        onError: (e) => toast("error", t("caddy.dns.toast.failed"), String(e)),
      },
    );
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={t("caddy.dns.publishTitle")} description={t("caddy.dns.publishBody")}>
        {result ? (
          <div className="space-y-3">
            <p className="text-fg font-mono text-[14px] font-semibold">{result.domain}</p>
            <OutcomeRow ok={result.dnsStatus === "created" || result.dnsStatus === "exists" || result.dnsStatus === "skipped" || result.dnsStatus === "unlinked"} label={t("caddy.dns.outcome.dns")} status={t(`caddy.dns.dnsStatus.${result.dnsStatus}`)} message={result.dnsMessage} />
            <OutcomeRow ok={result.siteStatus === "created"} label={t("caddy.dns.outcome.site")} status={t(`caddy.dns.siteStatus.${result.siteStatus}`)} message={result.siteMessage} />
            <div className="flex justify-end">
              <Button variant="secondary" onClick={onClose}>
                {t("common.close")}
              </Button>
            </div>
          </div>
        ) : (
          <div className="space-y-4">
            <Field label={t("caddy.dns.form.name")} hint={t("caddy.dns.form.nameHint", { zone: state.zoneName })}>
              {(p) => (
                <div className="flex items-center gap-2">
                  <Input {...p} autoFocus placeholder="@" value={name} onChange={(e) => setName(e.target.value)} />
                  <span className="text-fg-subtle shrink-0 font-mono text-[12px]">→ {fqdn}</span>
                </div>
              )}
            </Field>
            <div className="grid grid-cols-[110px_1fr_110px] gap-2">
              <Field label={t("caddy.sites.form.scheme", "Scheme")}>
                {(p) => (
                  <Select value={scheme} onValueChange={setScheme}>
                    <SelectTrigger id={p.id}>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="http">http</SelectItem>
                      <SelectItem value="https">https</SelectItem>
                    </SelectContent>
                  </Select>
                )}
              </Field>
              <Field label={t("caddy.dns.form.upstreamHost")}>
                {(p) => <Input {...p} placeholder="192.168.1.20" value={host} onChange={(e) => setHost(e.target.value)} />}
              </Field>
              <Field label={t("caddy.dns.form.upstreamPort")}>
                {(p) => <Input {...p} placeholder="8080" value={port} onChange={(e) => setPort(e.target.value)} />}
              </Field>
            </div>
            <label className="flex items-center justify-between gap-3">
              <span>
                <span className="text-fg block text-[13.5px] font-medium">{t("caddy.dns.form.createDns")}</span>
                <span className="text-fg-subtle block text-[12px]">
                  {t("caddy.dns.form.createDnsHint", { content: state.recordContent || "—" })}
                </span>
              </span>
              <Switch checked={createDns} onCheckedChange={setCreateDns} />
            </label>
            {createDns && (
              <label className="flex items-center justify-between gap-3">
                <span>
                  <span className="text-fg block text-[13.5px] font-medium">{t("caddy.dns.link.proxied")}</span>
                  <span className="text-fg-subtle block text-[12px]">{t("caddy.dns.link.proxiedHint")}</span>
                </span>
                <Switch checked={proxied} onCheckedChange={setProxied} />
              </label>
            )}
            <div className="flex justify-end gap-2">
              <Button variant="ghost" onClick={onClose}>
                {t("common.cancel")}
              </Button>
              <Button variant="primary" loading={publish.isPending} disabled={!host.trim()} onClick={submit}>
                <Rocket className="size-4" />
                {t("caddy.dns.publish")}
              </Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

function OutcomeRow({ ok, label, status, message }: { ok: boolean; label: string; status: string; message: string }) {
  return (
    <div className={cn("rounded-lg border px-3 py-2", ok ? "border-success/30 bg-success-soft/40" : "border-danger/30 bg-danger-soft/40")}>
      <div className="flex items-center justify-between gap-2">
        <span className="text-fg text-[13px] font-semibold">{label}</span>
        <Badge tone={ok ? "success" : "danger"}>{status}</Badge>
      </div>
      {message && <p className="text-fg-muted mt-0.5 text-[12.5px]">{message}</p>}
    </div>
  );
}

// ---- record create/edit dialog ------------------------------------------------------

const RECORD_TYPES = ["A", "AAAA", "CNAME", "TXT"] as const;

function RecordDialog({
  instanceId,
  state,
  edit,
  presetName,
  onClose,
}: {
  instanceId: string;
  state: CaddyDnsState;
  edit?: CaddyDnsRecord;
  presetName?: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const create = useDnsCreate(instanceId);
  const update = useDnsUpdate(instanceId);
  const toast = useToast();
  const [name, setName] = useState(edit?.name ?? presetName ?? "");
  const [type, setType] = useState(edit?.type ?? "A");
  const [content, setContent] = useState(edit?.content ?? state.recordContent);
  const [proxied, setProxied] = useState(edit?.proxied ?? state.proxiedDefault);
  const busy = create.isPending || update.isPending;

  const submit = () => {
    const body = { name: name.trim(), type, content: content.trim(), proxied };
    const opts = {
      onSuccess: () => onClose(),
      onError: (e: unknown) => toast("error", t("caddy.dns.toast.failed"), String(e)),
    };
    if (edit) update.mutate({ recordId: edit.id, body }, opts);
    else create.mutate(body, opts);
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={edit ? t("caddy.dns.editRecord") : t("caddy.dns.addRecord")}>
        <div className="space-y-4">
          <div className="grid grid-cols-[110px_1fr] gap-2">
            <Field label={t("caddy.dns.cols.type")}>
              {(p) => (
                <Select value={type} onValueChange={setType}>
                  <SelectTrigger id={p.id}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {RECORD_TYPES.map((rt) => (
                      <SelectItem key={rt} value={rt}>
                        {rt}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            </Field>
            <Field label={t("caddy.dns.cols.name")} hint={t("caddy.dns.form.nameHint", { zone: state.zoneName })}>
              {(p) => <Input {...p} autoFocus={!edit} placeholder="@" value={name} onChange={(e) => setName(e.target.value)} />}
            </Field>
          </div>
          <Field label={t("caddy.dns.cols.content")}>
            {(p) => <Input {...p} className="font-mono text-[13px]" value={content} onChange={(e) => setContent(e.target.value)} />}
          </Field>
          {type !== "TXT" && (
            <label className="flex items-center justify-between gap-3">
              <span>
                <span className="text-fg block text-[13.5px] font-medium">{t("caddy.dns.link.proxied")}</span>
                <span className="text-fg-subtle block text-[12px]">{t("caddy.dns.link.proxiedHint")}</span>
              </span>
              <Switch checked={proxied} onCheckedChange={setProxied} />
            </label>
          )}
          <div className="flex justify-end gap-2">
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button variant="primary" loading={busy} disabled={!name.trim() || !content.trim()} onClick={submit}>
              {edit ? t("common.save") : t("caddy.dns.addRecord")}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
