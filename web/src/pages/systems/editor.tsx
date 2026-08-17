import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  api,
  unwrap,
  isApiError,
  type Integration,
  type IntegrationType,
  type TestConnectionResult,
} from "@/lib/api";
import { systemTypeIcon } from "@/lib/systems";
import { Button } from "@/ui/button";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Field } from "@/ui/field";
import { Input, PasswordInput, Textarea } from "@/ui/input";
import { Checkbox } from "@/ui/misc";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { SkeletonRows } from "@/ui/skeleton";
import { useToast } from "@/ui/toast";

// ---- per-type form state ---------------------------------------------------

type PveFormState = {
  name: string;
  host: string;
  tokenId: string;
  tokenSecret: string;
  insecureTls: boolean;
  fingerprint: string;
};

type DockerTransport = "socket" | "tcp" | "ssh";

type DockerFormState = {
  name: string;
  transport: DockerTransport;
  socketPath: string;
  host: string;
  port: string;
  username: string;
  authType: "password" | "key";
  tls: boolean;
  caCert: string;
  clientCert: string;
  clientKey: string;
  sshSecret: string;
};

const emptyPveForm: PveFormState = {
  name: "",
  host: "",
  tokenId: "",
  tokenSecret: "",
  insecureTls: false,
  fingerprint: "",
};

const emptyDockerForm: DockerFormState = {
  name: "",
  transport: "socket",
  socketPath: "/var/run/docker.sock",
  host: "",
  port: "22",
  username: "",
  authType: "password",
  tls: false,
  caCert: "",
  clientCert: "",
  clientKey: "",
  sshSecret: "",
};

type PiholeFormState = {
  name: string;
  host: string;
  password: string;
  insecureTls: boolean;
};

type AdguardFormState = {
  name: string;
  host: string;
  username: string;
  password: string;
  insecureTls: boolean;
};

type HaFormState = {
  name: string;
  host: string;
  token: string;
  insecureTls: boolean;
};

const emptyPiholeForm: PiholeFormState = { name: "", host: "", password: "", insecureTls: false };

const emptyAdguardForm: AdguardFormState = {
  name: "",
  host: "",
  username: "",
  password: "",
  insecureTls: false,
};

const emptyHaForm: HaFormState = { name: "", host: "", token: "", insecureTls: false };

type FormPayload = { config: Record<string, unknown>; secrets: Record<string, string> };

function pvePayload(form: PveFormState): FormPayload {
  const secrets: Record<string, string> = {};
  if (form.tokenSecret) secrets.tokenSecret = form.tokenSecret;
  return {
    config: {
      host: form.host,
      tokenId: form.tokenId,
      insecureTls: form.insecureTls,
      fingerprint: form.fingerprint,
    },
    secrets,
  };
}

// Empty secrets are omitted so a stored value survives an edit untouched.
function dockerPayload(form: DockerFormState): FormPayload {
  const secrets: Record<string, string> = {};
  if (form.transport === "tcp" && form.tls) {
    if (form.caCert) secrets.caCert = form.caCert;
    if (form.clientCert) secrets.clientCert = form.clientCert;
    if (form.clientKey) secrets.clientKey = form.clientKey;
  }
  if (form.transport === "ssh" && form.sshSecret) secrets.sshSecret = form.sshSecret;
  return {
    config: {
      transport: form.transport,
      socketPath: form.socketPath,
      host: form.host,
      port: Number(form.port) || 22,
      username: form.username,
      authType: form.authType,
      tls: form.tls,
    },
    secrets,
  };
}

function piholePayload(form: PiholeFormState): FormPayload {
  const secrets: Record<string, string> = {};
  if (form.password) secrets.password = form.password;
  return { config: { host: form.host, insecureTls: form.insecureTls }, secrets };
}

function adguardPayload(form: AdguardFormState): FormPayload {
  const secrets: Record<string, string> = {};
  if (form.username) secrets.username = form.username;
  if (form.password) secrets.password = form.password;
  return { config: { host: form.host, insecureTls: form.insecureTls }, secrets };
}

function haPayload(form: HaFormState): FormPayload {
  const secrets: Record<string, string> = {};
  if (form.token) secrets.token = form.token;
  return { config: { host: form.host, insecureTls: form.insecureTls }, secrets };
}

function pveComplete(form: PveFormState, isNew: boolean): boolean {
  return Boolean(
    form.name.trim() && form.host.trim() && form.tokenId.trim() && (form.tokenSecret.trim() || !isNew),
  );
}

function dockerComplete(form: DockerFormState, isNew: boolean): boolean {
  if (!form.name.trim()) return false;
  switch (form.transport) {
    case "socket":
      return true;
    case "tcp":
      return Boolean(form.host.trim());
    case "ssh":
      return Boolean(form.host.trim() && form.username.trim() && (form.sshSecret.trim() || !isNew));
  }
}

function piholeComplete(form: PiholeFormState, isNew: boolean): boolean {
  return Boolean(form.name.trim() && form.host.trim() && (form.password.trim() || !isNew));
}

function adguardComplete(form: AdguardFormState, isNew: boolean): boolean {
  return Boolean(
    form.name.trim() &&
      form.host.trim() &&
      ((form.username.trim() && form.password.trim()) || !isNew),
  );
}

function haComplete(form: HaFormState, isNew: boolean): boolean {
  return Boolean(form.name.trim() && form.host.trim() && (form.token.trim() || !isNew));
}

type MysqlFormState = {
  name: string;
  host: string;
  username: string;
  password: string;
  tls: boolean;
  insecureTls: boolean;
};

type MongoFormState = {
  name: string;
  host: string;
  username: string;
  password: string;
  authSource: string;
  tls: boolean;
  insecureTls: boolean;
};

type TruenasFormState = {
  name: string;
  host: string;
  apiKey: string;
  insecureTls: boolean;
};

type McFormState = { name: string };

type CaddyFormState = { name: string; baseUrl: string; code: string };

type ApertureFormState = { name: string; baseUrl: string; apiKey: string };

type TailscaleFormState = {
  name: string;
  tailnet: string;
  apiKey: string;
  autoRenew: boolean;
};

type AmpFormState = {
  name: string;
  host: string;
  username: string;
  password: string;
  insecureTls: boolean;
};

const emptyMysqlForm: MysqlFormState = {
  name: "",
  host: "",
  username: "",
  password: "",
  tls: false,
  insecureTls: false,
};

const emptyMongoForm: MongoFormState = {
  name: "",
  host: "",
  username: "",
  password: "",
  authSource: "",
  tls: false,
  insecureTls: false,
};

const emptyTruenasForm: TruenasFormState = { name: "", host: "", apiKey: "", insecureTls: false };

const emptyMcForm: McFormState = { name: "" };

const emptyCaddyForm: CaddyFormState = { name: "", baseUrl: "", code: "" };

const emptyApertureForm: ApertureFormState = { name: "", baseUrl: "", apiKey: "" };

const emptyAmpForm: AmpFormState = { name: "", host: "", username: "", password: "", insecureTls: false };

const emptyTailscaleForm: TailscaleFormState = { name: "", tailnet: "", apiKey: "", autoRenew: false };

function mysqlPayload(form: MysqlFormState): FormPayload {
  const secrets: Record<string, string> = {};
  if (form.username) secrets.username = form.username;
  if (form.password) secrets.password = form.password;
  return {
    config: { host: form.host, tls: form.tls, insecureTls: form.insecureTls },
    secrets,
  };
}

function mongoPayload(form: MongoFormState): FormPayload {
  const secrets: Record<string, string> = {};
  if (form.username) secrets.username = form.username;
  if (form.password) secrets.password = form.password;
  const config: Record<string, unknown> = {
    host: form.host,
    tls: form.tls,
    insecureTls: form.insecureTls,
  };
  if (form.authSource.trim()) config.authSource = form.authSource.trim();
  return { config, secrets };
}

function truenasPayload(form: TruenasFormState): FormPayload {
  const secrets: Record<string, string> = {};
  if (form.apiKey) secrets.apiKey = form.apiKey;
  return { config: { host: form.host, insecureTls: form.insecureTls }, secrets };
}

function mcPayload(): FormPayload {
  return { config: {}, secrets: {} };
}

function ampPayload(form: AmpFormState): FormPayload {
  const secrets: Record<string, string> = {};
  if (form.password) secrets.password = form.password;
  return {
    config: { host: form.host, username: form.username, insecureTls: form.insecureTls },
    secrets,
  };
}

function aperturePayload(form: ApertureFormState): FormPayload {
  const secrets: Record<string, string> = {};
  if (form.apiKey) secrets.apiKey = form.apiKey;
  return { config: { baseUrl: form.baseUrl }, secrets };
}

function apertureComplete(form: ApertureFormState, isNew: boolean): boolean {
  return Boolean(form.name.trim() && form.baseUrl.trim() && (form.apiKey.trim() || !isNew));
}

function caddyPayload(form: CaddyFormState): FormPayload {
  return { config: { baseUrl: form.baseUrl }, secrets: {} };
}

function caddyComplete(form: CaddyFormState, isNew: boolean): boolean {
  if (!isNew) return Boolean(form.name.trim() && form.baseUrl.trim());
  return Boolean(form.name.trim() && form.baseUrl.trim() && form.code.replace(/\s/g, "").length === 6);
}

function ampComplete(form: AmpFormState, isNew: boolean): boolean {
  return Boolean(form.name.trim() && form.host.trim() && form.username.trim() && (form.password.trim() || !isNew));
}

function tailscalePayload(form: TailscaleFormState): FormPayload {
  const secrets: Record<string, string> = {};
  if (form.apiKey) secrets.apiKey = form.apiKey;
  return { config: { tailnet: form.tailnet.trim(), autoRenewKey: form.autoRenew }, secrets };
}

function tailscaleComplete(form: TailscaleFormState, isNew: boolean): boolean {
  return Boolean(form.name.trim() && (form.apiKey.trim() || !isNew));
}

function mysqlComplete(form: MysqlFormState, isNew: boolean): boolean {
  return Boolean(form.name.trim() && form.host.trim() && (form.username.trim() || !isNew));
}

function mongoComplete(form: MongoFormState, isNew: boolean): boolean {
  return Boolean(form.name.trim() && form.host.trim() && (form.username.trim() || !isNew));
}

function truenasComplete(form: TruenasFormState, isNew: boolean): boolean {
  return Boolean(form.name.trim() && form.host.trim() && (form.apiKey.trim() || !isNew));
}

function mcComplete(form: McFormState): boolean {
  return Boolean(form.name.trim());
}

// ---- add wizard ------------------------------------------------------------

/**
 * AddSystemWizard: step 1 picks a type from the server catalog; step 2 shows
 * the type's connection form with a test-before-save gate, exactly like the
 * backups target editor. On create it navigates to the new system's module.
 */
export function AddSystemWizard({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const toast = useToast();
  const navigate = useNavigate();

  const types = useQuery({
    queryKey: ["integration-types"],
    queryFn: async () => unwrap(await api.GET("/integration-types")),
  });

  const [type, setType] = useState<IntegrationType | null>(null);
  const [pveForm, setPveForm] = useState<PveFormState>(emptyPveForm);
  const [dockerForm, setDockerForm] = useState<DockerFormState>(emptyDockerForm);
  const [piholeForm, setPiholeForm] = useState<PiholeFormState>(emptyPiholeForm);
  const [adguardForm, setAdguardForm] = useState<AdguardFormState>(emptyAdguardForm);
  const [haForm, setHaForm] = useState<HaFormState>(emptyHaForm);
  const [mysqlForm, setMysqlForm] = useState<MysqlFormState>(emptyMysqlForm);
  const [mongoForm, setMongoForm] = useState<MongoFormState>(emptyMongoForm);
  const [truenasForm, setTruenasForm] = useState<TruenasFormState>(emptyTruenasForm);
  const [mcForm, setMcForm] = useState<McFormState>(emptyMcForm);
  const [ampForm, setAmpForm] = useState<AmpFormState>(emptyAmpForm);
  const [tailscaleForm, setTailscaleForm] = useState<TailscaleFormState>(emptyTailscaleForm);
  const [caddyForm, setCaddyForm] = useState<CaddyFormState>(emptyCaddyForm);
  const [apertureForm, setApertureForm] = useState<ApertureFormState>(emptyApertureForm);
  const [testResult, setTestResult] = useState<TestConnectionResult | null>(null);

  const kind = type?.id ?? "";
  // Exhaustive by design: a type without its own form must never inherit
  // another type's fields. Unknown types render an honest notice instead.
  const payload = (): FormPayload | null => {
    switch (kind) {
      case "proxmox":
        return pvePayload(pveForm);
      case "docker":
        return dockerPayload(dockerForm);
      case "pihole":
        return piholePayload(piholeForm);
      case "adguard":
        return adguardPayload(adguardForm);
      case "homeassistant":
        return haPayload(haForm);
      case "mysql":
        return mysqlPayload(mysqlForm);
      case "mongodb":
        return mongoPayload(mongoForm);
      case "truenas":
        return truenasPayload(truenasForm);
      case "amp":
        return ampPayload(ampForm);
      case "tailscale":
        return tailscalePayload(tailscaleForm);
      case "caddy":
        return caddyPayload(caddyForm);
      case "aperture":
        return aperturePayload(apertureForm);
      case "minecraft":
        return mcPayload();
      default:
        return null;
    }
  };
  const formName = () => {
    switch (kind) {
      case "proxmox":
        return pveForm.name;
      case "docker":
        return dockerForm.name;
      case "pihole":
        return piholeForm.name;
      case "adguard":
        return adguardForm.name;
      case "homeassistant":
        return haForm.name;
      case "mysql":
        return mysqlForm.name;
      case "mongodb":
        return mongoForm.name;
      case "truenas":
        return truenasForm.name;
      case "amp":
        return ampForm.name;
      case "tailscale":
        return tailscaleForm.name;
      case "caddy":
        return caddyForm.name;
      case "aperture":
        return apertureForm.name;
      case "minecraft":
        return mcForm.name;
      default:
        return "";
    }
  };
  // The Minecraft companion pairs after creation, and Caddy's pairing IS
  // the connection test — neither has anything to pre-test.
  const needsTest = kind !== "minecraft" && kind !== "caddy";

  const test = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/integrations/test", { body: { type: type!.id, ...payload()! } })),
    onSuccess: setTestResult,
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const create = useMutation({
    mutationFn: async () => {
      if (kind === "caddy") {
        return unwrap(
          await api.POST("/caddy/pair", {
            body: {
              name: caddyForm.name.trim(),
              baseUrl: caddyForm.baseUrl.trim(),
              code: caddyForm.code.replace(/\s/g, ""),
            },
          }),
        );
      }
      return unwrap(
        await api.POST("/integrations", {
          body: { type: type!.id, name: formName(), ...payload()! },
        }),
      );
    },
    onSuccess: (created: Integration) => {
      void queryClient.invalidateQueries({ queryKey: ["integrations"] });
      toast("success", t("systems.created", { name: created.name }));
      onClose();
      // Land the user in the new system's module.
      switch (created.type) {
        case "proxmox":
          void navigate({ to: "/pve/$instanceId", params: { instanceId: created.id } });
          break;
        case "docker":
          void navigate({ to: "/docker/$instanceId", params: { instanceId: created.id } });
          break;
        case "homeassistant":
          void navigate({ to: "/ha/$instanceId", params: { instanceId: created.id } });
          break;
        case "pihole":
        case "adguard":
          void navigate({ to: "/dns" });
          break;
        case "mysql":
        case "mongodb":
          void navigate({ to: "/db/$instanceId", params: { instanceId: created.id } });
          break;
        case "truenas":
          void navigate({ to: "/truenas/$instanceId", params: { instanceId: created.id } });
          break;
        case "amp":
          void navigate({ to: "/amp/$instanceId", params: { instanceId: created.id } });
          break;
        case "tailscale":
          void navigate({ to: "/ts/$instanceId", params: { instanceId: created.id } });
          break;
        case "caddy":
          void navigate({ to: "/caddy/$instanceId", params: { instanceId: created.id } });
          break;
        case "aperture":
          void navigate({ to: "/nvr/$instanceId", params: { instanceId: created.id } });
          break;
        case "minecraft":
          // Straight to the pairing instructions and downloads.
          void navigate({ to: "/mc/$instanceId/setup", params: { instanceId: created.id } });
          break;
      }
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  if (!type) {
    return (
      <Dialog open onOpenChange={(open) => !open && onClose()}>
        <DialogContent wide title={t("systems.wizard.title")} description={t("systems.wizard.intro")}>
          {types.isLoading ? (
            <SkeletonRows rows={4} />
          ) : (
            <TypePicker types={types.data ?? []} onPick={setType} />
          )}
        </DialogContent>
      </Dialog>
    );
  }

  const complete = (() => {
    switch (kind) {
      case "proxmox":
        return pveComplete(pveForm, true);
      case "docker":
        return dockerComplete(dockerForm, true);
      case "pihole":
        return piholeComplete(piholeForm, true);
      case "adguard":
        return adguardComplete(adguardForm, true);
      case "homeassistant":
        return haComplete(haForm, true);
      case "mysql":
        return mysqlComplete(mysqlForm, true);
      case "mongodb":
        return mongoComplete(mongoForm, true);
      case "truenas":
        return truenasComplete(truenasForm, true);
      case "amp":
        return ampComplete(ampForm, true);
      case "tailscale":
        return tailscaleComplete(tailscaleForm, true);
      case "caddy":
        return caddyComplete(caddyForm, true);
      case "aperture":
        return apertureComplete(apertureForm, true);
      case "minecraft":
        return mcComplete(mcForm);
      default:
        return false;
    }
  })();

  const clearTest = () => setTestResult(null);

  const formBody = (() => {
    switch (kind) {
      case "proxmox":
        return <PveForm form={pveForm} setForm={setPveForm} isNew testResult={testResult} onFormChange={clearTest} />;
      case "docker":
        return <DockerForm form={dockerForm} setForm={setDockerForm} isNew testResult={testResult} onFormChange={clearTest} />;
      case "pihole":
        return <PiholeForm form={piholeForm} setForm={setPiholeForm} isNew testResult={testResult} onFormChange={clearTest} />;
      case "adguard":
        return <AdguardForm form={adguardForm} setForm={setAdguardForm} isNew testResult={testResult} onFormChange={clearTest} />;
      case "homeassistant":
        return <HaForm form={haForm} setForm={setHaForm} isNew testResult={testResult} onFormChange={clearTest} />;
      case "mysql":
        return <MysqlForm form={mysqlForm} setForm={setMysqlForm} isNew testResult={testResult} onFormChange={clearTest} />;
      case "mongodb":
        return <MongoForm form={mongoForm} setForm={setMongoForm} isNew testResult={testResult} onFormChange={clearTest} />;
      case "truenas":
        return <TruenasForm form={truenasForm} setForm={setTruenasForm} isNew testResult={testResult} onFormChange={clearTest} />;
      case "amp":
        return <AmpForm form={ampForm} setForm={setAmpForm} isNew testResult={testResult} onFormChange={clearTest} />;
      case "tailscale":
        return <TailscaleForm form={tailscaleForm} setForm={setTailscaleForm} isNew testResult={testResult} onFormChange={clearTest} />;
      case "caddy":
        return <CaddyForm form={caddyForm} setForm={setCaddyForm} isNew onFormChange={clearTest} />;
      case "aperture":
        return <ApertureForm form={apertureForm} setForm={setApertureForm} isNew testResult={testResult} onFormChange={clearTest} />;
      case "minecraft":
        return <McForm form={mcForm} setForm={setMcForm} />;
      default:
        return <UnknownTypeNotice />;
    }
  })();

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        wide
        title={type.name}
        footer={
          <>
            <Button
              variant="ghost"
              className="mr-auto"
              onClick={() => {
                setType(null);
                setTestResult(null);
              }}
            >
              {t("common.back")}
            </Button>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            {needsTest && (
              <Button variant="secondary" disabled={!complete} loading={test.isPending} onClick={() => test.mutate()}>
                {test.isPending ? t("systems.testing") : t("systems.testConnection")}
              </Button>
            )}
            <Button
              variant="primary"
              disabled={!complete || (needsTest && !testResult?.ok)}
              loading={create.isPending}
              onClick={() => create.mutate()}
            >
              {t("common.create")}
            </Button>
          </>
        }
      >
        {formBody}
        {needsTest && !testResult && (
          <p className="text-fg-subtle mt-3 text-[13px]">{t("systems.testFirst")}</p>
        )}
      </DialogContent>
    </Dialog>
  );
}

// ---- edit ------------------------------------------------------------------

export function EditSystemDialog({ row, onClose }: { row: Integration; onClose: () => void }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const toast = useToast();

  const kind = row.type;
  const cfg = row.config as Record<string, unknown>;
  const [pveForm, setPveForm] = useState<PveFormState>({
    name: row.name,
    host: String(cfg.host ?? ""),
    tokenId: String(cfg.tokenId ?? ""),
    tokenSecret: "",
    insecureTls: Boolean(cfg.insecureTls),
    fingerprint: String(cfg.fingerprint ?? ""),
  });
  const [dockerForm, setDockerForm] = useState<DockerFormState>({
    ...emptyDockerForm,
    name: row.name,
    transport: cfg.transport === "tcp" || cfg.transport === "ssh" ? cfg.transport : "socket",
    socketPath: String(cfg.socketPath ?? "") || "/var/run/docker.sock",
    host: String(cfg.host ?? ""),
    port: String(cfg.port ?? 22),
    username: String(cfg.username ?? ""),
    authType: cfg.authType === "key" ? "key" : "password",
    tls: Boolean(cfg.tls),
  });
  const [piholeForm, setPiholeForm] = useState<PiholeFormState>({
    ...emptyPiholeForm,
    name: row.name,
    host: String(cfg.host ?? ""),
    insecureTls: Boolean(cfg.insecureTls),
  });
  const [adguardForm, setAdguardForm] = useState<AdguardFormState>({
    ...emptyAdguardForm,
    name: row.name,
    host: String(cfg.host ?? ""),
    insecureTls: Boolean(cfg.insecureTls),
  });
  const [haForm, setHaForm] = useState<HaFormState>({
    ...emptyHaForm,
    name: row.name,
    host: String(cfg.host ?? ""),
    insecureTls: Boolean(cfg.insecureTls),
  });
  const [mysqlForm, setMysqlForm] = useState<MysqlFormState>({
    ...emptyMysqlForm,
    name: row.name,
    host: String(cfg.host ?? ""),
    tls: Boolean(cfg.tls),
    insecureTls: Boolean(cfg.insecureTls),
  });
  const [mongoForm, setMongoForm] = useState<MongoFormState>({
    ...emptyMongoForm,
    name: row.name,
    host: String(cfg.host ?? ""),
    authSource: String(cfg.authSource ?? ""),
    tls: Boolean(cfg.tls),
    insecureTls: Boolean(cfg.insecureTls),
  });
  const [truenasForm, setTruenasForm] = useState<TruenasFormState>({
    ...emptyTruenasForm,
    name: row.name,
    host: String(cfg.host ?? ""),
    insecureTls: Boolean(cfg.insecureTls),
  });
  const [mcForm, setMcForm] = useState<McFormState>({ name: row.name });
  const [ampForm, setAmpForm] = useState<AmpFormState>({
    ...emptyAmpForm,
    name: row.name,
    host: String(cfg.host ?? ""),
    username: String(cfg.username ?? ""),
    insecureTls: Boolean(cfg.insecureTls),
  });
  const [tailscaleForm, setTailscaleForm] = useState<TailscaleFormState>({
    ...emptyTailscaleForm,
    name: row.name,
    tailnet: String(cfg.tailnet ?? ""),
    autoRenew: Boolean(cfg.autoRenewKey),
  });
  const [caddyForm, setCaddyForm] = useState<CaddyFormState>({
    ...emptyCaddyForm,
    name: row.name,
    baseUrl: String(cfg.baseUrl ?? ""),
  });
  const [apertureForm, setApertureForm] = useState<ApertureFormState>({
    ...emptyApertureForm,
    name: row.name,
    baseUrl: String(cfg.baseUrl ?? ""),
  });
  const [testResult, setTestResult] = useState<TestConnectionResult | null>(null);

  const editPayload = (): FormPayload => {
    switch (kind) {
      case "docker":
        return dockerPayload(dockerForm);
      case "pihole":
        return piholePayload(piholeForm);
      case "adguard":
        return adguardPayload(adguardForm);
      case "homeassistant":
        return haPayload(haForm);
      case "mysql":
        return mysqlPayload(mysqlForm);
      case "mongodb":
        return mongoPayload(mongoForm);
      case "truenas":
        return truenasPayload(truenasForm);
      case "amp":
        return ampPayload(ampForm);
      case "tailscale":
        return tailscalePayload(tailscaleForm);
      case "caddy":
        return caddyPayload(caddyForm);
      case "aperture":
        return aperturePayload(apertureForm);
      case "minecraft":
        return mcPayload();
      default:
        return pvePayload(pveForm);
    }
  };
  const editName = (): string => {
    switch (kind) {
      case "docker":
        return dockerForm.name;
      case "pihole":
        return piholeForm.name;
      case "adguard":
        return adguardForm.name;
      case "homeassistant":
        return haForm.name;
      case "mysql":
        return mysqlForm.name;
      case "mongodb":
        return mongoForm.name;
      case "truenas":
        return truenasForm.name;
      case "amp":
        return ampForm.name;
      case "tailscale":
        return tailscaleForm.name;
      case "caddy":
        return caddyForm.name;
      case "aperture":
        return apertureForm.name;
      case "minecraft":
        return mcForm.name;
      default:
        return pveForm.name;
    }
  };

  const save = useMutation({
    mutationFn: async () => {
      const { config, secrets } = editPayload();
      const name = editName();
      return unwrap(
        await api.PATCH("/integrations/{id}", {
          params: { path: { id: row.id } },
          body: { name, config, secrets },
        }),
      );
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["integrations"] });
      onClose();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const complete = (() => {
    switch (kind) {
      case "docker":
        return dockerComplete(dockerForm, false);
      case "pihole":
        return piholeComplete(piholeForm, false);
      case "adguard":
        return adguardComplete(adguardForm, false);
      case "homeassistant":
        return haComplete(haForm, false);
      case "mysql":
        return mysqlComplete(mysqlForm, false);
      case "mongodb":
        return mongoComplete(mongoForm, false);
      case "truenas":
        return truenasComplete(truenasForm, false);
      case "amp":
        return ampComplete(ampForm, false);
      case "tailscale":
        return tailscaleComplete(tailscaleForm, false);
      case "caddy":
        return caddyComplete(caddyForm, false);
      case "aperture":
        return apertureComplete(apertureForm, false);
      case "minecraft":
        return mcComplete(mcForm);
      default:
        return pveComplete(pveForm, false);
    }
  })();

  const clearTest = () => setTestResult(null);

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        wide
        title={t("systems.editTitle", { name: row.name })}
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
        {(() => {
          switch (kind) {
            case "docker":
              return <DockerForm form={dockerForm} setForm={setDockerForm} isNew={false} testResult={testResult} onFormChange={clearTest} />;
            case "pihole":
              return <PiholeForm form={piholeForm} setForm={setPiholeForm} isNew={false} testResult={testResult} onFormChange={clearTest} />;
            case "adguard":
              return <AdguardForm form={adguardForm} setForm={setAdguardForm} isNew={false} testResult={testResult} onFormChange={clearTest} />;
            case "homeassistant":
              return <HaForm form={haForm} setForm={setHaForm} isNew={false} testResult={testResult} onFormChange={clearTest} />;
            case "mysql":
              return <MysqlForm form={mysqlForm} setForm={setMysqlForm} isNew={false} testResult={testResult} onFormChange={clearTest} />;
            case "mongodb":
              return <MongoForm form={mongoForm} setForm={setMongoForm} isNew={false} testResult={testResult} onFormChange={clearTest} />;
            case "truenas":
              return <TruenasForm form={truenasForm} setForm={setTruenasForm} isNew={false} testResult={testResult} onFormChange={clearTest} />;
            case "amp":
              return <AmpForm form={ampForm} setForm={setAmpForm} isNew={false} testResult={testResult} onFormChange={clearTest} />;
            case "tailscale":
              return <TailscaleForm form={tailscaleForm} setForm={setTailscaleForm} isNew={false} testResult={testResult} onFormChange={clearTest} />;
            case "caddy":
              return <CaddyForm form={caddyForm} setForm={setCaddyForm} isNew={false} onFormChange={clearTest} />;
            case "aperture":
              return <ApertureForm form={apertureForm} setForm={setApertureForm} isNew={false} testResult={testResult} onFormChange={clearTest} />;
            case "minecraft":
              return <McForm form={mcForm} setForm={setMcForm} />;
            case "proxmox":
              return <PveForm form={pveForm} setForm={setPveForm} isNew={false} testResult={testResult} onFormChange={clearTest} />;
            default:
              return <UnknownTypeNotice />;
          }
        })()}
      </DialogContent>
    </Dialog>
  );
}

// ---- per-type forms --------------------------------------------------------

function PveForm({
  form,
  setForm,
  isNew,
  testResult,
  onFormChange,
}: {
  form: PveFormState;
  setForm: (fn: (prev: PveFormState) => PveFormState) => void;
  isNew: boolean;
  testResult: TestConnectionResult | null;
  onFormChange: () => void;
}) {
  const { t } = useTranslation();
  const update = (patch: Partial<PveFormState>) => {
    onFormChange();
    setForm((prev) => ({ ...prev, ...patch }));
  };

  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t("integrations.form.name")} hint={t("integrations.form.nameHintPve")}>
          {(p) => <Input {...p} autoFocus value={form.name} onChange={(e) => update({ name: e.target.value })} />}
        </Field>
        <Field label={t("integrations.pve.host")} hint={t("integrations.pve.hostHint")}>
          {(p) => <Input {...p} placeholder="pve.lan" value={form.host} onChange={(e) => update({ host: e.target.value })} />}
        </Field>
      </div>
      <Field label={t("integrations.pve.tokenId")} hint={t("integrations.pve.tokenIdHint")}>
        {(p) => (
          <Input {...p} placeholder="root@pam!homehub" className="font-mono text-[13px]" value={form.tokenId} onChange={(e) => update({ tokenId: e.target.value })} />
        )}
      </Field>
      <Field
        label={t("integrations.pve.tokenSecret")}
        hint={isNew ? t("integrations.pve.tokenSecretHint") : t("integrations.pve.tokenSecretKeep")}
      >
        {(p) => (
          <PasswordInput {...p} className="font-mono text-[13px]" value={form.tokenSecret} onChange={(e) => update({ tokenSecret: e.target.value })} />
        )}
      </Field>
      <Field label={t("integrations.pve.fingerprint")} hint={t("integrations.pve.fingerprintHint")} optional={t("common.optional")}>
        {(p) => (
          <Input {...p} placeholder="AA:BB:CC:…" className="font-mono text-[13px]" value={form.fingerprint} onChange={(e) => update({ fingerprint: e.target.value })} />
        )}
      </Field>
      <Checkbox
        checked={form.insecureTls}
        onCheckedChange={(checked) => update({ insecureTls: checked })}
        label={t("integrations.pve.insecureTls")}
        hint={t("integrations.pve.insecureTlsHint")}
      />

      <TestResultNote result={testResult} />
    </div>
  );
}

function DockerForm({
  form,
  setForm,
  isNew,
  testResult,
  onFormChange,
}: {
  form: DockerFormState;
  setForm: (fn: (prev: DockerFormState) => DockerFormState) => void;
  isNew: boolean;
  testResult: TestConnectionResult | null;
  onFormChange: () => void;
}) {
  const { t } = useTranslation();
  const update = (patch: Partial<DockerFormState>) => {
    onFormChange();
    setForm((prev) => ({ ...prev, ...patch }));
  };

  const secretHint = isNew ? undefined : t("integrations.docker.secretKeep");
  const pemClass = "font-mono text-xs";

  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t("integrations.form.name")} hint={t("integrations.form.nameHintDocker")}>
          {(p) => <Input {...p} autoFocus value={form.name} onChange={(e) => update({ name: e.target.value })} />}
        </Field>
        <Field label={t("integrations.docker.transport")}>
          {() => (
            <Select value={form.transport} onValueChange={(v) => update({ transport: v as DockerTransport })}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="socket">{t("integrations.docker.transportSocket")}</SelectItem>
                <SelectItem value="tcp">{t("integrations.docker.transportTcp")}</SelectItem>
                <SelectItem value="ssh">{t("integrations.docker.transportSsh")}</SelectItem>
              </SelectContent>
            </Select>
          )}
        </Field>
      </div>

      {form.transport === "socket" && (
        <Field label={t("integrations.docker.socketPath")} hint={t("integrations.docker.socketPathHint")}>
          {(p) => (
            <Input {...p} className="font-mono text-[13px]" placeholder="/var/run/docker.sock" value={form.socketPath} onChange={(e) => update({ socketPath: e.target.value })} />
          )}
        </Field>
      )}

      {form.transport === "tcp" && (
        <>
          <Field label={t("integrations.docker.host")} hint={t("integrations.docker.hostHintTcp")}>
            {(p) => <Input {...p} placeholder="192.168.1.20" value={form.host} onChange={(e) => update({ host: e.target.value })} />}
          </Field>
          <Checkbox
            checked={form.tls}
            onCheckedChange={(checked) => update({ tls: checked })}
            label={t("integrations.docker.tls")}
            hint={t("integrations.docker.tlsHint")}
          />
          {form.tls && (
            <>
              <Field label={t("integrations.docker.caCert")} hint={secretHint} optional={t("common.optional")}>
                {(p) => (
                  <Textarea {...p} className={pemClass} rows={4} placeholder="-----BEGIN CERTIFICATE-----" value={form.caCert} onChange={(e) => update({ caCert: e.target.value })} />
                )}
              </Field>
              <Field label={t("integrations.docker.clientCert")} hint={secretHint} optional={t("common.optional")}>
                {(p) => (
                  <Textarea {...p} className={pemClass} rows={4} placeholder="-----BEGIN CERTIFICATE-----" value={form.clientCert} onChange={(e) => update({ clientCert: e.target.value })} />
                )}
              </Field>
              <Field label={t("integrations.docker.clientKey")} hint={secretHint} optional={t("common.optional")}>
                {(p) => (
                  <Textarea {...p} className={pemClass} rows={4} placeholder="-----BEGIN PRIVATE KEY-----" value={form.clientKey} onChange={(e) => update({ clientKey: e.target.value })} />
                )}
              </Field>
            </>
          )}
        </>
      )}

      {form.transport === "ssh" && (
        <>
          <div className="grid grid-cols-3 gap-3">
            <Field label={t("integrations.docker.host")} hint={t("integrations.docker.hostHintSsh")} className="col-span-2">
              {(p) => <Input {...p} placeholder="192.168.1.20" value={form.host} onChange={(e) => update({ host: e.target.value })} />}
            </Field>
            <Field label={t("integrations.docker.port")}>
              {(p) => <Input {...p} type="number" value={form.port} onChange={(e) => update({ port: e.target.value })} />}
            </Field>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t("integrations.docker.username")}>
              {(p) => <Input {...p} placeholder="root" value={form.username} onChange={(e) => update({ username: e.target.value })} />}
            </Field>
            <Field label={t("integrations.docker.authType")}>
              {() => (
                <Select value={form.authType} onValueChange={(v) => update({ authType: v as "password" | "key" })}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="password">{t("integrations.docker.authPassword")}</SelectItem>
                    <SelectItem value="key">{t("integrations.docker.authKey")}</SelectItem>
                  </SelectContent>
                </Select>
              )}
            </Field>
          </div>
          <Field
            label={form.authType === "password" ? t("integrations.docker.sshPassword") : t("integrations.docker.sshKey")}
            hint={secretHint}
          >
            {(p) =>
              form.authType === "password" ? (
                <PasswordInput {...p} value={form.sshSecret} onChange={(e) => update({ sshSecret: e.target.value })} />
              ) : (
                <Textarea {...p} className={pemClass} rows={5} placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" value={form.sshSecret} onChange={(e) => update({ sshSecret: e.target.value })} />
              )
            }
          </Field>
        </>
      )}

      <TestResultNote result={testResult} />
    </div>
  );
}

function PiholeForm({
  form,
  setForm,
  isNew,
  testResult,
  onFormChange,
}: {
  form: PiholeFormState;
  setForm: (fn: (prev: PiholeFormState) => PiholeFormState) => void;
  isNew: boolean;
  testResult: TestConnectionResult | null;
  onFormChange: () => void;
}) {
  const { t } = useTranslation();
  const update = (patch: Partial<PiholeFormState>) => {
    onFormChange();
    setForm((prev) => ({ ...prev, ...patch }));
  };

  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t("integrations.form.name")} hint={t("integrations.form.nameHintPihole")}>
          {(p) => <Input {...p} autoFocus value={form.name} onChange={(e) => update({ name: e.target.value })} />}
        </Field>
        <Field label={t("integrations.pihole.host")} hint={t("integrations.pihole.hostHint")}>
          {(p) => <Input {...p} placeholder="pihole.lan" value={form.host} onChange={(e) => update({ host: e.target.value })} />}
        </Field>
      </div>
      <Field
        label={t("integrations.pihole.password")}
        hint={isNew ? t("integrations.pihole.passwordHint") : t("integrations.pihole.passwordKeep")}
      >
        {(p) => (
          <PasswordInput {...p} className="font-mono text-[13px]" value={form.password} onChange={(e) => update({ password: e.target.value })} />
        )}
      </Field>
      <Checkbox
        checked={form.insecureTls}
        onCheckedChange={(checked) => update({ insecureTls: checked })}
        label={t("integrations.dns.insecureTls")}
        hint={t("integrations.dns.insecureTlsHint")}
      />

      <TestResultNote result={testResult} />
    </div>
  );
}

function AdguardForm({
  form,
  setForm,
  isNew,
  testResult,
  onFormChange,
}: {
  form: AdguardFormState;
  setForm: (fn: (prev: AdguardFormState) => AdguardFormState) => void;
  isNew: boolean;
  testResult: TestConnectionResult | null;
  onFormChange: () => void;
}) {
  const { t } = useTranslation();
  const update = (patch: Partial<AdguardFormState>) => {
    onFormChange();
    setForm((prev) => ({ ...prev, ...patch }));
  };

  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t("integrations.form.name")} hint={t("integrations.form.nameHintAdguard")}>
          {(p) => <Input {...p} autoFocus value={form.name} onChange={(e) => update({ name: e.target.value })} />}
        </Field>
        <Field label={t("integrations.adguard.host")} hint={t("integrations.adguard.hostHint")}>
          {(p) => <Input {...p} placeholder="192.168.1.5:3000" value={form.host} onChange={(e) => update({ host: e.target.value })} />}
        </Field>
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field
          label={t("integrations.adguard.username")}
          hint={isNew ? undefined : t("integrations.adguard.usernameKeep")}
        >
          {(p) => <Input {...p} value={form.username} onChange={(e) => update({ username: e.target.value })} />}
        </Field>
        <Field
          label={t("integrations.adguard.password")}
          hint={isNew ? undefined : t("integrations.adguard.passwordKeep")}
        >
          {(p) => <PasswordInput {...p} value={form.password} onChange={(e) => update({ password: e.target.value })} />}
        </Field>
      </div>
      <Checkbox
        checked={form.insecureTls}
        onCheckedChange={(checked) => update({ insecureTls: checked })}
        label={t("integrations.dns.insecureTls")}
        hint={t("integrations.dns.insecureTlsHint")}
      />

      <TestResultNote result={testResult} />
    </div>
  );
}

function HaForm({
  form,
  setForm,
  isNew,
  testResult,
  onFormChange,
}: {
  form: HaFormState;
  setForm: (fn: (prev: HaFormState) => HaFormState) => void;
  isNew: boolean;
  testResult: TestConnectionResult | null;
  onFormChange: () => void;
}) {
  const { t } = useTranslation();
  const update = (patch: Partial<HaFormState>) => {
    onFormChange();
    setForm((prev) => ({ ...prev, ...patch }));
  };

  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t("integrations.form.name")} hint={t("integrations.form.nameHintHa")}>
          {(p) => <Input {...p} autoFocus value={form.name} onChange={(e) => update({ name: e.target.value })} />}
        </Field>
        <Field label={t("integrations.homeassistant.host")} hint={t("integrations.homeassistant.hostHint")}>
          {(p) => (
            <Input {...p} placeholder="homeassistant.lan:8123" value={form.host} onChange={(e) => update({ host: e.target.value })} />
          )}
        </Field>
      </div>
      <Field
        label={t("integrations.homeassistant.token")}
        hint={isNew ? t("integrations.homeassistant.tokenHint") : t("integrations.homeassistant.tokenKeep")}
      >
        {(p) => (
          <PasswordInput {...p} className="font-mono text-[13px]" value={form.token} onChange={(e) => update({ token: e.target.value })} />
        )}
      </Field>
      <Checkbox
        checked={form.insecureTls}
        onCheckedChange={(checked) => update({ insecureTls: checked })}
        label={t("integrations.dns.insecureTls")}
        hint={t("integrations.dns.insecureTlsHint")}
      />

      <TestResultNote result={testResult} />
    </div>
  );
}

function TestResultNote({ result }: { result: TestConnectionResult | null }) {
  if (!result) return null;
  return (
    <div
      role="status"
      className={
        "rounded-md px-3 py-2.5 text-[13px] " +
        (result.ok ? "bg-success-soft text-success-soft-fg" : "bg-danger-soft text-danger-soft-fg")
      }
    >
      {result.message}
    </div>
  );
}

function MysqlForm({
  form,
  setForm,
  isNew,
  testResult,
  onFormChange,
}: {
  form: MysqlFormState;
  setForm: (fn: (prev: MysqlFormState) => MysqlFormState) => void;
  isNew: boolean;
  testResult: TestConnectionResult | null;
  onFormChange: () => void;
}) {
  const { t } = useTranslation();
  const update = (patch: Partial<MysqlFormState>) => {
    onFormChange();
    setForm((prev) => ({ ...prev, ...patch }));
  };
  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t("integrations.form.name")} hint={t("integrations.form.nameHintMysql")}>
          {(p) => <Input {...p} autoFocus value={form.name} onChange={(e) => update({ name: e.target.value })} />}
        </Field>
        <Field label={t("integrations.mysql.host")} hint={t("integrations.mysql.hostHint")}>
          {(p) => <Input {...p} placeholder="db.lan:3306" value={form.host} onChange={(e) => update({ host: e.target.value })} />}
        </Field>
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t("integrations.mysql.username")} hint={isNew ? t("integrations.mysql.usernameHint") : t("integrations.form.secretKeep")}>
          {(p) => <Input {...p} value={form.username} onChange={(e) => update({ username: e.target.value })} />}
        </Field>
        <Field label={t("integrations.mysql.password")} hint={isNew ? undefined : t("integrations.form.secretKeep")}>
          {(p) => (
            <PasswordInput {...p} className="font-mono text-[13px]" value={form.password} onChange={(e) => update({ password: e.target.value })} />
          )}
        </Field>
      </div>
      <Checkbox
        checked={form.tls}
        onCheckedChange={(checked) => update({ tls: checked, insecureTls: checked ? form.insecureTls : false })}
        label={t("integrations.mysql.tls")}
        hint={t("integrations.mysql.tlsHint")}
      />
      {form.tls && (
        <Checkbox
          checked={form.insecureTls}
          onCheckedChange={(checked) => update({ insecureTls: checked })}
          label={t("integrations.dns.insecureTls")}
          hint={t("integrations.dns.insecureTlsHint")}
        />
      )}
      <TestResultNote result={testResult} />
    </div>
  );
}

function MongoForm({
  form,
  setForm,
  isNew,
  testResult,
  onFormChange,
}: {
  form: MongoFormState;
  setForm: (fn: (prev: MongoFormState) => MongoFormState) => void;
  isNew: boolean;
  testResult: TestConnectionResult | null;
  onFormChange: () => void;
}) {
  const { t } = useTranslation();
  const update = (patch: Partial<MongoFormState>) => {
    onFormChange();
    setForm((prev) => ({ ...prev, ...patch }));
  };
  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t("integrations.form.name")} hint={t("integrations.form.nameHintMongo")}>
          {(p) => <Input {...p} autoFocus value={form.name} onChange={(e) => update({ name: e.target.value })} />}
        </Field>
        <Field label={t("integrations.mongodb.host")} hint={t("integrations.mongodb.hostHint")}>
          {(p) => <Input {...p} placeholder="mongo.lan:27017" value={form.host} onChange={(e) => update({ host: e.target.value })} />}
        </Field>
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t("integrations.mongodb.username")} hint={isNew ? t("integrations.mongodb.usernameHint") : t("integrations.form.secretKeep")}>
          {(p) => <Input {...p} value={form.username} onChange={(e) => update({ username: e.target.value })} />}
        </Field>
        <Field label={t("integrations.mongodb.password")} hint={isNew ? undefined : t("integrations.form.secretKeep")}>
          {(p) => (
            <PasswordInput {...p} className="font-mono text-[13px]" value={form.password} onChange={(e) => update({ password: e.target.value })} />
          )}
        </Field>
      </div>
      <Field label={t("integrations.mongodb.authSource")} hint={t("integrations.mongodb.authSourceHint")}>
        {(p) => <Input {...p} placeholder="admin" value={form.authSource} onChange={(e) => update({ authSource: e.target.value })} />}
      </Field>
      <Checkbox
        checked={form.tls}
        onCheckedChange={(checked) => update({ tls: checked, insecureTls: checked ? form.insecureTls : false })}
        label={t("integrations.mongodb.tls")}
        hint={t("integrations.mongodb.tlsHint")}
      />
      {form.tls && (
        <Checkbox
          checked={form.insecureTls}
          onCheckedChange={(checked) => update({ insecureTls: checked })}
          label={t("integrations.dns.insecureTls")}
          hint={t("integrations.dns.insecureTlsHint")}
        />
      )}
      <TestResultNote result={testResult} />
    </div>
  );
}

function ApertureForm({
  form,
  setForm,
  isNew,
  testResult,
  onFormChange,
}: {
  form: ApertureFormState;
  setForm: (fn: (prev: ApertureFormState) => ApertureFormState) => void;
  isNew: boolean;
  testResult: TestConnectionResult | null;
  onFormChange: () => void;
}) {
  const { t } = useTranslation();
  const update = (patch: Partial<ApertureFormState>) => {
    onFormChange();
    setForm((prev) => ({ ...prev, ...patch }));
  };
  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t("integrations.form.name")} hint={t("integrations.form.nameHintAperture")}>
          {(p) => <Input {...p} autoFocus value={form.name} onChange={(e) => update({ name: e.target.value })} />}
        </Field>
        <Field label={t("integrations.aperture.baseUrl")} hint={t("integrations.aperture.baseUrlHint")}>
          {(p) => <Input {...p} placeholder="https://aperture.example.com" value={form.baseUrl} onChange={(e) => update({ baseUrl: e.target.value })} />}
        </Field>
      </div>
      <Field
        label={t("integrations.aperture.apiKey")}
        hint={isNew ? t("integrations.aperture.apiKeyHint") : t("integrations.form.secretKeep")}
      >
        {(p) => (
          <PasswordInput {...p} className="font-mono text-[13px]" value={form.apiKey} onChange={(e) => update({ apiKey: e.target.value })} />
        )}
      </Field>
      <TestResultNote result={testResult} />
    </div>
  );
}

function CaddyForm({
  form,
  setForm,
  isNew,
  onFormChange,
}: {
  form: CaddyFormState;
  setForm: (fn: (prev: CaddyFormState) => CaddyFormState) => void;
  isNew: boolean;
  onFormChange: () => void;
}) {
  const { t } = useTranslation();
  const update = (patch: Partial<CaddyFormState>) => {
    onFormChange();
    setForm((prev) => ({ ...prev, ...patch }));
  };
  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t("integrations.form.name")} hint={t("integrations.form.nameHintCaddy")}>
          {(p) => <Input {...p} autoFocus value={form.name} onChange={(e) => update({ name: e.target.value })} />}
        </Field>
        <Field label={t("integrations.caddy.baseUrl")} hint={t("integrations.caddy.baseUrlHint")}>
          {(p) => <Input {...p} placeholder="10.0.0.2:9776" value={form.baseUrl} onChange={(e) => update({ baseUrl: e.target.value })} />}
        </Field>
      </div>
      {isNew && (
        <Field label={t("integrations.caddy.code")} hint={t("integrations.caddy.codeHint")}>
          {(p) => (
            <Input {...p} className="font-mono text-[15px] tracking-widest" placeholder="481 502" value={form.code} onChange={(e) => update({ code: e.target.value })} />
          )}
        </Field>
      )}
      {!isNew && <p className="text-fg-muted text-[13px]">{t("integrations.caddy.repairNote")}</p>}
      <div className="bg-warning-soft text-warning-soft-fg rounded-lg px-3 py-2 text-[13px]">
        {t("integrations.caddy.adminWarning")}
      </div>
    </div>
  );
}

function TailscaleForm({
  form,
  setForm,
  isNew,
  testResult,
  onFormChange,
}: {
  form: TailscaleFormState;
  setForm: (fn: (prev: TailscaleFormState) => TailscaleFormState) => void;
  isNew: boolean;
  testResult: TestConnectionResult | null;
  onFormChange: () => void;
}) {
  const { t } = useTranslation();
  const update = (patch: Partial<TailscaleFormState>) => {
    onFormChange();
    setForm((prev) => ({ ...prev, ...patch }));
  };
  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t("integrations.form.name")} hint={t("integrations.form.nameHintTailscale")}>
          {(p) => <Input {...p} autoFocus value={form.name} onChange={(e) => update({ name: e.target.value })} />}
        </Field>
        <Field label={t("integrations.tailscale.tailnet")} hint={t("integrations.tailscale.tailnetHint")}>
          {(p) => <Input {...p} placeholder="example.com" value={form.tailnet} onChange={(e) => update({ tailnet: e.target.value })} />}
        </Field>
      </div>
      <Field
        label={t("integrations.tailscale.apiKey")}
        hint={isNew ? t("integrations.tailscale.apiKeyHint") : t("integrations.form.secretKeep")}
      >
        {(p) => (
          <PasswordInput {...p} className="font-mono text-[13px]" value={form.apiKey} onChange={(e) => update({ apiKey: e.target.value })} />
        )}
      </Field>
      <Checkbox
        checked={form.autoRenew}
        onCheckedChange={(checked) => update({ autoRenew: checked })}
        label={t("integrations.tailscale.autoRenew")}
        hint={t("integrations.tailscale.autoRenewHint")}
      />
      <TestResultNote result={testResult} />
    </div>
  );
}

function AmpForm({
  form,
  setForm,
  isNew,
  testResult,
  onFormChange,
}: {
  form: AmpFormState;
  setForm: (fn: (prev: AmpFormState) => AmpFormState) => void;
  isNew: boolean;
  testResult: TestConnectionResult | null;
  onFormChange: () => void;
}) {
  const { t } = useTranslation();
  const update = (patch: Partial<AmpFormState>) => {
    onFormChange();
    setForm((prev) => ({ ...prev, ...patch }));
  };
  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t("integrations.form.name")} hint={t("integrations.form.nameHintAmp")}>
          {(p) => <Input {...p} autoFocus value={form.name} onChange={(e) => update({ name: e.target.value })} />}
        </Field>
        <Field label={t("integrations.amp.host")} hint={t("integrations.amp.hostHint")}>
          {(p) => <Input {...p} placeholder="amp.lan:8080" value={form.host} onChange={(e) => update({ host: e.target.value })} />}
        </Field>
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t("integrations.amp.username")} hint={t("integrations.amp.usernameHint")}>
          {(p) => <Input {...p} value={form.username} onChange={(e) => update({ username: e.target.value })} />}
        </Field>
        <Field
          label={t("integrations.amp.password")}
          hint={isNew ? undefined : t("integrations.form.secretKeep")}
        >
          {(p) => (
            <PasswordInput {...p} value={form.password} onChange={(e) => update({ password: e.target.value })} />
          )}
        </Field>
      </div>
      <Checkbox
        checked={form.insecureTls}
        onCheckedChange={(checked) => update({ insecureTls: checked })}
        label={t("integrations.amp.insecureTls")}
        hint={t("integrations.amp.insecureTlsHint")}
      />
      <TestResultNote result={testResult} />
    </div>
  );
}

function TruenasForm({
  form,
  setForm,
  isNew,
  testResult,
  onFormChange,
}: {
  form: TruenasFormState;
  setForm: (fn: (prev: TruenasFormState) => TruenasFormState) => void;
  isNew: boolean;
  testResult: TestConnectionResult | null;
  onFormChange: () => void;
}) {
  const { t } = useTranslation();
  const update = (patch: Partial<TruenasFormState>) => {
    onFormChange();
    setForm((prev) => ({ ...prev, ...patch }));
  };
  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t("integrations.form.name")} hint={t("integrations.form.nameHintTruenas")}>
          {(p) => <Input {...p} autoFocus value={form.name} onChange={(e) => update({ name: e.target.value })} />}
        </Field>
        <Field label={t("integrations.truenas.host")} hint={t("integrations.truenas.hostHint")}>
          {(p) => <Input {...p} placeholder="truenas.lan" value={form.host} onChange={(e) => update({ host: e.target.value })} />}
        </Field>
      </div>
      <Field
        label={t("integrations.truenas.apiKey")}
        hint={isNew ? t("integrations.truenas.apiKeyHint") : t("integrations.form.secretKeep")}
      >
        {(p) => (
          <PasswordInput {...p} className="font-mono text-[13px]" value={form.apiKey} onChange={(e) => update({ apiKey: e.target.value })} />
        )}
      </Field>
      <Checkbox
        checked={form.insecureTls}
        onCheckedChange={(checked) => update({ insecureTls: checked })}
        label={t("integrations.truenas.insecureTls")}
        hint={t("integrations.truenas.insecureTlsHint")}
      />
      <TestResultNote result={testResult} />
    </div>
  );
}

/**
 * McForm: a Minecraft instance needs only a name — the companion plugin
 * connects outward and pairs with a code after creation, so there is no
 * connection to configure or test here.
 */
function McForm({
  form,
  setForm,
}: {
  form: McFormState;
  setForm: (fn: (prev: McFormState) => McFormState) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="space-y-4">
      <Field label={t("integrations.form.name")} hint={t("integrations.form.nameHintMc")}>
        {(p) => (
          <Input
            {...p}
            autoFocus
            value={form.name}
            onChange={(e) => setForm((prev) => ({ ...prev, name: e.target.value }))}
          />
        )}
      </Field>
      <div className="bg-accent-soft/40 border-accent/30 text-fg rounded-lg border p-4 text-[13px] leading-relaxed">
        <p className="font-semibold">{t("integrations.minecraft.howTitle")}</p>
        <ol className="text-fg-muted mt-2 list-decimal space-y-1 pl-4">
          <li>{t("integrations.minecraft.step1")}</li>
          <li>{t("integrations.minecraft.step2")}</li>
          <li>{t("integrations.minecraft.step3")}</li>
        </ol>
      </div>
    </div>
  );
}

/** UnknownTypeNotice renders when a type has no form yet — never another type's fields. */

/** The type groups shown by the wizard, in display order. Types the server
 * knows but this list doesn't fall into "other" so nothing is hidden. */
const TYPE_CATEGORIES: { key: string; ids: string[] }[] = [
  { key: "infrastructure", ids: ["proxmox", "docker", "truenas"] },
  { key: "network", ids: ["caddy", "tailscale", "pihole", "adguard"] },
  { key: "game", ids: ["minecraft", "amp"] },
  { key: "data", ids: ["mysql", "mongodb"] },
  { key: "home", ids: ["homeassistant", "aperture"] },
];

function TypePicker({ types, onPick }: { types: IntegrationType[]; onPick: (t: IntegrationType) => void }) {
  const { t } = useTranslation();
  const byId = new Map(types.map((typeInfo) => [typeInfo.id, typeInfo]));
  const seen = new Set<string>();
  const groups = TYPE_CATEGORIES.map((cat) => ({
    key: cat.key,
    entries: cat.ids
      .map((id) => {
        seen.add(id);
        return byId.get(id);
      })
      .filter((x): x is IntegrationType => Boolean(x)),
  })).filter((g) => g.entries.length > 0);
  const other = types.filter((typeInfo) => !seen.has(typeInfo.id));
  if (other.length > 0) groups.push({ key: "other", entries: other });

  return (
    <div className="space-y-4">
      {groups.map((group) => (
        <div key={group.key}>
          <p className="text-fg-subtle mb-1.5 text-[11px] font-semibold tracking-wide uppercase">
            {t(`systems.wizard.categories.${group.key}`)}
          </p>
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
            {group.entries.map((typeInfo) => {
              const Icon = systemTypeIcon(typeInfo.id);
              return (
                <button
                  key={typeInfo.id}
                  onClick={() => onPick(typeInfo)}
                  title={typeInfo.description}
                  className="border-edge hover:border-accent hover:bg-accent-soft/30 flex cursor-pointer items-center gap-2.5 rounded-lg border px-3 py-2.5 text-left transition-colors"
                >
                  <Icon className="text-fg-subtle size-4 shrink-0" />
                  <span className="text-fg truncate text-[13.5px] font-medium">{typeInfo.name}</span>
                </button>
              );
            })}
          </div>
        </div>
      ))}
    </div>
  );
}

function UnknownTypeNotice() {
  const { t } = useTranslation();
  return <p className="text-fg-muted text-sm">{t("systems.wizard.noForm")}</p>;
}
