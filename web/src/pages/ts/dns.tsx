import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Globe, Plus, X } from "lucide-react";
import { api, isApiError, unwrap } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { Button } from "@/ui/button";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { Input } from "@/ui/input";
import { PageLoader } from "@/ui/misc";
import { ErrorState } from "@/ui/states";
import { Switch } from "@/ui/switch";
import { useToast } from "@/ui/toast";
import { tsResource } from "./shared";

/**
 * TsDnsPage edits the tailnet's DNS as one form: global nameservers,
 * MagicDNS, and search domains, saved together.
 */
export function TsDnsPage() {
  const { instanceId } = useParams({ from: "/shell/ts/$instanceId/dns" });
  const { can } = useAuth();
  const configurable = can(tsResource(instanceId), "configure");

  const dns = useQuery({
    queryKey: ["ts", instanceId, "dns"],
    queryFn: async () => unwrap(await api.GET("/ts/{id}/dns", { params: { path: { id: instanceId } } })),
  });

  if (dns.isLoading) return <PageLoader />;
  if (dns.isError) {
    return (
      <Card>
        <ErrorState error={dns.error} onRetry={() => void dns.refetch()} />
      </Card>
    );
  }

  return <DnsForm key={JSON.stringify(dns.data)} instanceId={instanceId} initial={dns.data!} readOnly={!configurable} />;
}

function DnsForm({
  instanceId,
  initial,
  readOnly,
}: {
  instanceId: string;
  initial: { nameservers: string[]; magicDns: boolean; searchPaths: string[] };
  readOnly: boolean;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();
  const [nameservers, setNameservers] = useState<string[]>(initial.nameservers);
  const [magicDns, setMagicDns] = useState(initial.magicDns);
  const [searchPaths, setSearchPaths] = useState<string[]>(initial.searchPaths);

  const dirty =
    JSON.stringify({ nameservers, magicDns, searchPaths }) !==
    JSON.stringify({ nameservers: initial.nameservers, magicDns: initial.magicDns, searchPaths: initial.searchPaths });

  const save = useMutation({
    mutationFn: async () => {
      unwrap(
        await api.PUT("/ts/{id}/dns", {
          params: { path: { id: instanceId } },
          body: { nameservers, magicDns, searchPaths },
        }),
      );
    },
    onSuccess: () => {
      toast("success", t("ts.dns.saved"));
      void queryClient.invalidateQueries({ queryKey: ["ts", instanceId, "dns"] });
    },
    onError: (err) =>
      toast("error", t("ts.dns.saveFailed"), isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <div className="max-w-2xl space-y-4">
      <Card>
        <CardHeader title={t("ts.dns.nameservers")} description={t("ts.dns.nameserversHint")} />
        <CardBody>
          <ListEditor
            values={nameservers}
            onChange={setNameservers}
            placeholder="1.1.1.1"
            readOnly={readOnly}
            addLabel={t("ts.dns.addNameserver")}
          />
        </CardBody>
      </Card>

      <Card>
        <CardBody className="flex items-center justify-between gap-4">
          <div>
            <p className="text-fg flex items-center gap-2 text-[14px] font-medium">
              <Globe className="size-4" />
              MagicDNS
            </p>
            <p className="text-fg-muted text-[13px]">{t("ts.dns.magicDnsHint")}</p>
          </div>
          <Switch
            checked={magicDns}
            onCheckedChange={setMagicDns}
            disabled={readOnly || (nameservers.length === 0 && !magicDns)}
            aria-label="MagicDNS"
          />
        </CardBody>
      </Card>

      <Card>
        <CardHeader title={t("ts.dns.searchPaths")} description={t("ts.dns.searchPathsHint")} />
        <CardBody>
          <ListEditor
            values={searchPaths}
            onChange={setSearchPaths}
            placeholder="lan"
            readOnly={readOnly}
            addLabel={t("ts.dns.addSearchPath")}
          />
        </CardBody>
      </Card>

      {!readOnly && (
        <div className="flex justify-end">
          <Button variant="primary" disabled={!dirty} loading={save.isPending} onClick={() => save.mutate()}>
            {t("common.save")}
          </Button>
        </div>
      )}
    </div>
  );
}

function ListEditor({
  values,
  onChange,
  placeholder,
  readOnly,
  addLabel,
}: {
  values: string[];
  onChange: (next: string[]) => void;
  placeholder: string;
  readOnly: boolean;
  addLabel: string;
}) {
  const { t } = useTranslation();
  const [draft, setDraft] = useState("");

  const add = () => {
    const value = draft.trim();
    if (!value || values.includes(value)) return;
    onChange([...values, value]);
    setDraft("");
  };

  return (
    <div className="space-y-2">
      {values.length === 0 && <p className="text-fg-muted text-[13px]">{t("ts.dns.none")}</p>}
      {values.map((value) => (
        <div key={value} className="border-edge bg-surface flex items-center gap-2 rounded-lg border px-3 py-1.5">
          <code className="min-w-0 flex-1 font-mono text-[13px]">{value}</code>
          {!readOnly && (
            <button
              className="text-fg-muted hover:text-danger transition-colors"
              onClick={() => onChange(values.filter((v) => v !== value))}
              aria-label={t("common.remove")}
            >
              <X className="size-4" />
            </button>
          )}
        </div>
      ))}
      {!readOnly && (
        <div className="flex gap-2">
          <Input
            value={draft}
            placeholder={placeholder}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                add();
              }
            }}
            className="font-mono text-[13px]"
          />
          <Button size="sm" variant="secondary" onClick={add} disabled={!draft.trim()}>
            <Plus className="size-3.5" />
            {addLabel}
          </Button>
        </div>
      )}
    </div>
  );
}
