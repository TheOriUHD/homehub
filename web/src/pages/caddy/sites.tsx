import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { FileWarning, Lock, Pencil, Plus, Trash2, Waypoints } from "lucide-react";
import {
  api,
  isApiError,
  unwrap,
  type CaddyHeaderUp,
  type CaddySite,
  type CaddySiteInput,
  type CaddySitesState,
} from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { ConfirmDialog } from "@/ui/confirm";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { Checkbox, PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { useToast } from "@/ui/toast";
import { caddyResource, caddySitesKey, useCaddyLive, useCaddySites } from "./shared";

export function CaddySitesPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/caddy/$instanceId/" });
  const { can } = useAuth();
  const sites = useCaddySites(instanceId);
  useCaddyLive(instanceId);

  const [editing, setEditing] = useState<CaddySite | null>(null);
  const [creating, setCreating] = useState(false);
  const [removing, setRemoving] = useState<string | null>(null);

  if (sites.isLoading) return <PageLoader />;
  if (sites.isError) {
    return (
      <Card>
        <ErrorState error={sites.error} onRetry={() => void sites.refetch()} />
      </Card>
    );
  }

  const state = sites.data!;
  const editable = state.writable && !state.refusal && can(caddyResource(instanceId), "control");

  // When the file was refused wholesale, the refusal IS the panel.
  if (state.refusal) {
    return (
      <Card>
        <EmptyState icon={FileWarning} title={t("caddy.sites.refusedTitle")} body={state.refusal} />
      </Card>
    );
  }

  return (
    <div className="space-y-5">
      {!state.writable && (
        <div className="bg-warning-soft text-warning-soft-fg flex items-start gap-2.5 rounded-lg px-4 py-3 text-[13px]" role="note">
          <Lock className="mt-0.5 size-4 shrink-0" />
          {t("caddy.sites.readOnlyBanner")}
        </div>
      )}

      <div className="flex items-center justify-between gap-3">
        <p className="text-fg-muted text-[13px]">
          {t("caddy.sites.fileLabel")} <code className="font-mono">{state.path}</code>
        </p>
        {editable && (
          <Button size="sm" onClick={() => setCreating(true)}>
            <Plus className="size-3.5" />
            {t("caddy.sites.add")}
          </Button>
        )}
      </div>

      {state.entries.length === 0 ? (
        <Card>
          <EmptyState icon={Waypoints} title={t("caddy.sites.emptyTitle")} body={t("caddy.sites.emptyBody")} />
        </Card>
      ) : (
        <Card>
          <div className="overflow-x-auto">
            <Table>
              <THead>
                <TR>
                  <TH>{t("caddy.sites.domain")}</TH>
                  <TH>{t("caddy.sites.upstream")}</TH>
                  <TH>{t("caddy.sites.options")}</TH>
                  <TH aria-label={t("common.actions")} />
                </TR>
              </THead>
              <TBody>
                {state.entries.map((site) => (
                  <TR key={site.domain}>
                    <TD>
                      <p className="text-fg font-medium">{site.domain}</p>
                      {site.extraDomains && site.extraDomains.length > 0 && (
                        <p className="text-fg-muted truncate text-[12px]">{site.extraDomains.join(", ")}</p>
                      )}
                    </TD>
                    <TD>
                      <code className="font-mono text-[12.5px]">
                        {(site.upstreamScheme ? site.upstreamScheme + "://" : "") +
                          site.upstreamHost +
                          (site.upstreamPort ? ":" + site.upstreamPort : "")}
                      </code>
                    </TD>
                    <TD>
                      <span className="flex flex-wrap gap-1">
                        {site.dnsProvider && <Badge tone="info">{t("caddy.sites.dns", { provider: site.dnsProvider })}</Badge>}
                        {site.encode && <Badge tone="neutral">encode {site.encode}</Badge>}
                        {site.insecureUpstream && <Badge tone="warning">{t("caddy.sites.insecure")}</Badge>}
                        {site.headerUp && site.headerUp.length > 0 && (
                          <Badge tone="neutral">{t("caddy.sites.headers", { count: site.headerUp.length })}</Badge>
                        )}
                        {site.unknownDirectives.length > 0 && (
                          <Badge tone="neutral">{t("caddy.sites.extraDirectives", { count: site.unknownDirectives.length })}</Badge>
                        )}
                      </span>
                    </TD>
                    <TD>
                      {editable && (
                        <span className="flex justify-end gap-1">
                          <Button size="sm" variant="ghost" aria-label={t("common.edit")} onClick={() => setEditing(site)}>
                            <Pencil className="size-4" />
                          </Button>
                          <Button size="sm" variant="ghost" aria-label={t("caddy.sites.remove")} onClick={() => setRemoving(site.domain)}>
                            <Trash2 className="size-4" />
                          </Button>
                        </span>
                      )}
                    </TD>
                  </TR>
                ))}
              </TBody>
            </Table>
          </div>
        </Card>
      )}

      {state.rawSites.length > 0 && (
        <Card>
          <CardHeader title={t("caddy.sites.advancedTitle")} description={t("caddy.sites.advancedBody")} />
          <CardBody className="space-y-3">
            {state.rawSites.map((raw, i) => (
              <div key={i} className="border-edge rounded-lg border">
                <div className="border-edge flex flex-wrap items-center gap-2 border-b px-3 py-2">
                  <span className="text-fg text-[13px] font-medium">{raw.addresses.join(", ")}</span>
                  <Badge tone="neutral">{t(`caddy.reasons.${reasonKey(raw.reason)}`, { defaultValue: raw.reason })}</Badge>
                </div>
                <pre className="overflow-x-auto px-3 py-2 font-mono text-[12px] leading-5">{raw.raw.trim()}</pre>
              </div>
            ))}
          </CardBody>
        </Card>
      )}

      {state.global && (
        <Card>
          <CardHeader title={t("caddy.sites.globalTitle")} description={t("caddy.sites.globalBody")} />
          <CardBody>
            <pre className="overflow-x-auto font-mono text-[12px] leading-5">{state.global.trim()}</pre>
          </CardBody>
        </Card>
      )}

      {(creating || editing) && (
        <SiteDialog
          instanceId={instanceId}
          site={editing}
          defaults={state.siteDefaults ?? null}
          onClose={() => {
            setCreating(false);
            setEditing(null);
          }}
        />
      )}
      {removing && <RemoveDialog instanceId={instanceId} domain={removing} onClose={() => setRemoving(null)} />}
    </div>
  );
}

/** reasonKey turns the splitter's reason sentence into an i18n slug. */
function reasonKey(reason: string): string {
  if (reason.startsWith("unrecognised reverse_proxy option")) return "unknownOption";
  switch (reason) {
    case "no reverse_proxy":
      return "noProxy";
    case "more than one reverse_proxy":
      return "multiProxy";
    case "contains a nested block":
      return "nestedBlock";
    case "tls block does more than a DNS challenge":
      return "tlsAdvanced";
    case "unrecognised transport":
      return "transport";
    case "reverse_proxy has matchers or several upstreams":
      return "matchers";
    default:
      return "other";
  }
}

function useSiteMutationHandlers(instanceId: string, onDone: () => void) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();
  return {
    onSuccess: (state: CaddySitesState) => {
      queryClient.setQueryData(caddySitesKey(instanceId), state);
      toast("success", t("caddy.sites.saved"));
      onDone();
    },
    onError: (err: unknown) => {
      const message = isApiError(err) ? err.message : t("common.unknownError");
      toast("error", t("caddy.sites.saveFailed"), message);
      // A 409 means the file moved underneath us — reload it.
      void queryClient.invalidateQueries({ queryKey: caddySitesKey(instanceId) });
    },
  };
}

function SiteDialog({
  instanceId,
  site,
  defaults,
  onClose,
}: {
  instanceId: string;
  site: CaddySite | null; // null = create
  defaults?: { dnsProvider?: string; dnsArgs?: string; encode?: string } | null;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [domain, setDomain] = useState(site?.domain ?? "");
  const [extras, setExtras] = useState((site?.extraDomains ?? []).join(", "));
  const [scheme, setScheme] = useState(site?.upstreamScheme ?? "");
  const [host, setHost] = useState(site?.upstreamHost ?? "");
  const [port, setPort] = useState(site?.upstreamPort ?? "");
  // New sites start from the "site template" inferred from existing entries
  // (TLS DNS challenge + encode), so they come out looking like the rest.
  const [dnsProvider, setDnsProvider] = useState(site ? (site.dnsProvider ?? "") : (defaults?.dnsProvider ?? ""));
  const [dnsArgs, setDnsArgs] = useState(site ? (site.dnsArgs ?? "") : (defaults?.dnsArgs ?? ""));
  const [encode, setEncode] = useState(site ? (site.encode ?? "") : (defaults?.encode ?? ""));
  const [insecure, setInsecure] = useState(site?.insecureUpstream ?? false);
  const [headers, setHeaders] = useState<CaddyHeaderUp[]>(site?.headerUp ?? []);

  const handlers = useSiteMutationHandlers(instanceId, onClose);
  const save = useMutation({
    mutationFn: async () => {
      const body: CaddySiteInput = {
        domain: domain.trim(),
        upstreamHost: host.trim(),
      };
      const extraList = extras
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);
      if (extraList.length > 0) body.extraDomains = extraList;
      if (scheme.trim()) body.upstreamScheme = scheme.trim();
      if (port.trim()) body.upstreamPort = port.trim();
      if (dnsProvider.trim()) {
        body.dnsProvider = dnsProvider.trim();
        if (dnsArgs.trim()) body.dnsArgs = dnsArgs.trim();
      }
      if (encode.trim()) body.encode = encode.trim();
      if (insecure) body.insecureUpstream = true;
      if (headers.length > 0) body.headerUp = headers.filter((h) => h.name.trim());
      if (site) {
        return unwrap(
          await api.PUT("/caddy/{id}/sites/{domain}", {
            params: { path: { id: instanceId, domain: site.domain } },
            body,
          }),
        );
      }
      return unwrap(await api.POST("/caddy/{id}/sites", { params: { path: { id: instanceId } }, body }));
    },
    ...handlers,
  });

  const complete = domain.trim() !== "" && host.trim() !== "";

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        wide
        title={site ? t("caddy.editor.editTitle", { domain: site.domain }) : t("caddy.editor.addTitle")}
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
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t("caddy.editor.domain")} hint={t("caddy.editor.domainHint")}>
              {(p) => <Input {...p} autoFocus placeholder="app.example.com" value={domain} onChange={(e) => setDomain(e.target.value)} />}
            </Field>
            <Field label={t("caddy.editor.extraDomains")} hint={t("caddy.editor.extraDomainsHint")}>
              {(p) => <Input {...p} placeholder="www.app.example.com" value={extras} onChange={(e) => setExtras(e.target.value)} />}
            </Field>
          </div>

          <div className="grid gap-4 sm:grid-cols-3">
            <Field label={t("caddy.editor.scheme")} hint={t("caddy.editor.schemeHint")}>
              {(p) => <Input {...p} placeholder="https" value={scheme} onChange={(e) => setScheme(e.target.value)} />}
            </Field>
            <Field label={t("caddy.editor.host")}>
              {(p) => <Input {...p} placeholder="10.0.0.5" value={host} onChange={(e) => setHost(e.target.value)} />}
            </Field>
            <Field label={t("caddy.editor.port")}>
              {(p) => <Input {...p} placeholder="8080" value={port} onChange={(e) => setPort(e.target.value)} />}
            </Field>
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t("caddy.editor.dnsProvider")} hint={t("caddy.editor.dnsProviderHint")}>
              {(p) => <Input {...p} placeholder="cloudflare" value={dnsProvider} onChange={(e) => setDnsProvider(e.target.value)} />}
            </Field>
            <Field label={t("caddy.editor.dnsArgs")} hint={t("caddy.editor.dnsArgsHint")}>
              {(p) => (
                <Input {...p} className="font-mono text-[13px]" placeholder="{env.CLOUDFLARE_API_TOKEN}" value={dnsArgs} onChange={(e) => setDnsArgs(e.target.value)} />
              )}
            </Field>
          </div>

          <Field label={t("caddy.editor.encode")} hint={t("caddy.editor.encodeHint")}>
            {(p) => <Input {...p} placeholder="gzip zstd" value={encode} onChange={(e) => setEncode(e.target.value)} />}
          </Field>

          <Checkbox
            checked={insecure}
            onCheckedChange={setInsecure}
            label={t("caddy.editor.insecure")}
            hint={t("caddy.editor.insecureHint")}
          />

          <HeaderUpEditor headers={headers} onChange={setHeaders} />

          {site && site.unknownDirectives.length > 0 && (
            <div className="bg-surface border-edge rounded-lg border px-3 py-2">
              <p className="text-fg-muted text-[12px]">{t("caddy.editor.unknownKept")}</p>
              <pre className="mt-1 font-mono text-[12px] leading-5">{site.unknownDirectives.join("\n")}</pre>
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function HeaderUpEditor({ headers, onChange }: { headers: CaddyHeaderUp[]; onChange: (h: CaddyHeaderUp[]) => void }) {
  const { t } = useTranslation();
  return (
    <div className="space-y-2">
      <p className="text-fg text-[13px] font-medium">{t("caddy.editor.headerUp")}</p>
      {headers.map((h, i) => (
        <div key={i} className="flex gap-2">
          <Input
            className="font-mono text-[13px]"
            placeholder="Host"
            value={h.name}
            onChange={(e) => onChange(headers.map((x, j) => (j === i ? { ...x, name: e.target.value } : x)))}
          />
          <Input
            className="font-mono text-[13px]"
            placeholder="{host}"
            value={h.value}
            onChange={(e) => onChange(headers.map((x, j) => (j === i ? { ...x, value: e.target.value } : x)))}
          />
          <Button size="sm" variant="ghost" aria-label={t("common.remove")} onClick={() => onChange(headers.filter((_, j) => j !== i))}>
            <Trash2 className="size-4" />
          </Button>
        </div>
      ))}
      <Button size="sm" variant="secondary" onClick={() => onChange([...headers, { name: "", value: "" }])}>
        <Plus className="size-3.5" />
        {t("caddy.editor.addHeader")}
      </Button>
    </div>
  );
}

function RemoveDialog({ instanceId, domain, onClose }: { instanceId: string; domain: string; onClose: () => void }) {
  const { t } = useTranslation();
  const handlers = useSiteMutationHandlers(instanceId, onClose);
  const remove = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.DELETE("/caddy/{id}/sites/{domain}", {
          params: { path: { id: instanceId, domain } },
        }),
      ),
    ...handlers,
  });
  return (
    <ConfirmDialog
      open
      onOpenChange={(open) => !open && onClose()}
      title={t("caddy.sites.removeTitle")}
      body={t("caddy.sites.removeBody", { domain })}
      confirmLabel={t("caddy.sites.remove")}
      loading={remove.isPending}
      onConfirm={() => remove.mutate()}
    />
  );
}
