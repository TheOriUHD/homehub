import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  ArrowUpCircle,
  Check,
  Globe,
  Laptop,
  MonitorSmartphone,
  MoreHorizontal,
  Network,
  Route,
  ShieldAlert,
  Smartphone,
  Server,
  Trash2,
  UserCheck,
} from "lucide-react";
import { api, isApiError, unwrap, type TsDevice } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { formatRelative } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Card, CardBody } from "@/ui/card";
import { ConfirmDialog } from "@/ui/confirm";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Dropdown, DropdownContent, DropdownItem, DropdownSeparator, DropdownTrigger } from "@/ui/dropdown";
import { Checkbox, PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { useToast } from "@/ui/toast";
import { EXIT_ROUTES, isExitRoute, tsResource, useTsDevices, useTsLive } from "./shared";

function osIcon(os: string) {
  switch (os.toLowerCase()) {
    case "ios":
    case "android":
      return Smartphone;
    case "macos":
    case "windows":
      return Laptop;
    case "linux":
      return Server;
    default:
      return MonitorSmartphone;
  }
}

export function TsDevicesPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/ts/$instanceId/" });
  const devices = useTsDevices(instanceId);
  useTsLive(instanceId);

  if (devices.isLoading) return <PageLoader />;
  if (devices.isError) {
    return (
      <Card>
        <ErrorState error={devices.error} onRetry={() => void devices.refetch()} />
      </Card>
    );
  }

  const rows = devices.data ?? [];
  const online = rows.filter((d) => d.online).length;
  const updates = rows.filter((d) => d.updateAvailable).length;
  const pending = rows.filter((d) => !d.authorized).length;

  return (
    <div className="space-y-5">
      <div className="grid gap-3 sm:grid-cols-3">
        <StatCard icon={Network} value={`${online} / ${rows.length}`} label={t("ts.devices.online")} />
        <StatCard icon={ArrowUpCircle} value={String(updates)} label={t("ts.devices.updatesAvailable")} />
        <StatCard
          icon={ShieldAlert}
          value={String(pending)}
          label={t("ts.devices.pendingApproval")}
          tone={pending > 0 ? "warning" : undefined}
        />
      </div>

      {rows.length === 0 ? (
        <Card>
          <EmptyState icon={Network} title={t("ts.devices.emptyTitle")} body={t("ts.devices.emptyBody")} />
        </Card>
      ) : (
        <Card>
          <div className="overflow-x-auto">
            <Table>
              <THead>
                <TR>
                  <TH>{t("ts.devices.device")}</TH>
                  <TH>{t("ts.devices.status")}</TH>
                  <TH>{t("ts.devices.address")}</TH>
                  <TH>{t("ts.devices.version")}</TH>
                  <TH>{t("ts.devices.owner")}</TH>
                  <TH>{t("ts.devices.routes")}</TH>
                  <TH aria-label={t("common.actions")} />
                </TR>
              </THead>
              <TBody>
                {rows.map((d) => (
                  <DeviceRow key={d.id} instanceId={instanceId} device={d} />
                ))}
              </TBody>
            </Table>
          </div>
        </Card>
      )}
    </div>
  );
}

function StatCard({
  icon: Icon,
  value,
  label,
  tone,
}: {
  icon: typeof Network;
  value: string;
  label: string;
  tone?: "warning";
}) {
  return (
    <Card>
      <CardBody className="flex items-center gap-4">
        <span
          className={
            tone === "warning"
              ? "bg-warning-soft text-warning-soft-fg flex size-11 shrink-0 items-center justify-center rounded-xl"
              : "bg-accent-soft text-accent-soft-fg flex size-11 shrink-0 items-center justify-center rounded-xl"
          }
        >
          <Icon className="size-5" />
        </span>
        <div className="min-w-0">
          <p className="text-fg text-2xl font-semibold tracking-tight">{value}</p>
          <p className="text-fg-muted text-[13px]">{label}</p>
        </div>
      </CardBody>
    </Card>
  );
}

function DeviceRow({ instanceId, device }: { instanceId: string; device: TsDevice }) {
  const { t } = useTranslation();
  const { can } = useAuth();
  const toast = useToast();
  const queryClient = useQueryClient();
  const [confirm, setConfirm] = useState<"delete" | "expire" | null>(null);
  const [routesOpen, setRoutesOpen] = useState(false);

  const controllable = can(`${tsResource(instanceId)}/device/${device.id}`, "control");
  const OsIcon = osIcon(device.os);
  const ipv4 = device.addresses.find((a) => !a.includes(":")) ?? device.addresses[0] ?? "—";

  const refresh = () => void queryClient.invalidateQueries({ queryKey: ["ts", instanceId, "devices"] });
  const onError = (err: unknown) =>
    toast("error", t("ts.devices.actionFailed"), isApiError(err) ? err.message : t("common.unknownError"));

  const authorize = useMutation({
    mutationFn: async (authorized: boolean) => {
      unwrap(
        await api.POST("/ts/{id}/devices/{deviceId}/authorized", {
          params: { path: { id: instanceId, deviceId: device.id } },
          body: { authorized },
        }),
      );
    },
    onSuccess: refresh,
    onError,
  });
  const keyExpiry = useMutation({
    mutationFn: async (keyExpiryDisabled: boolean) => {
      unwrap(
        await api.POST("/ts/{id}/devices/{deviceId}/key", {
          params: { path: { id: instanceId, deviceId: device.id } },
          body: { keyExpiryDisabled },
        }),
      );
    },
    onSuccess: refresh,
    onError,
  });
  const expire = useMutation({
    mutationFn: async () => {
      unwrap(
        await api.POST("/ts/{id}/devices/{deviceId}/expire", {
          params: { path: { id: instanceId, deviceId: device.id } },
        }),
      );
    },
    onSuccess: () => {
      setConfirm(null);
      refresh();
    },
    onError,
  });
  const remove = useMutation({
    mutationFn: async () => {
      unwrap(
        await api.DELETE("/ts/{id}/devices/{deviceId}", {
          params: { path: { id: instanceId, deviceId: device.id } },
        }),
      );
    },
    onSuccess: () => {
      setConfirm(null);
      refresh();
    },
    onError,
  });

  const enabledSubnets = device.enabledRoutes.filter((r) => !isExitRoute(r));
  const advertisedSubnets = device.advertisedRoutes.filter((r) => !isExitRoute(r));
  const advertisesExit = device.advertisedRoutes.some(isExitRoute);
  const exitEnabled = EXIT_ROUTES.every((r) => device.enabledRoutes.includes(r));

  return (
    <>
      <TR>
        <TD>
          <div className="flex items-center gap-2.5">
            <OsIcon className="text-fg-muted size-4 shrink-0" />
            <div className="min-w-0">
              <p className="text-fg truncate font-medium">{device.name}</p>
              <p className="text-fg-muted truncate text-[12px]">{device.hostname}</p>
            </div>
            {!device.authorized && <Badge tone="warning">{t("ts.devices.unauthorized")}</Badge>}
          </div>
        </TD>
        <TD>
          <span className="flex items-center gap-1.5 text-[13px]">
            <StatusDot tone={device.online ? "success" : "neutral"} />
            {device.online
              ? t("ts.devices.onlineNow")
              : device.lastSeen
                ? formatRelative(device.lastSeen)
                : "—"}
          </span>
        </TD>
        <TD>
          <span className="font-mono text-[12.5px]">{ipv4}</span>
        </TD>
        <TD>
          <span className="flex items-center gap-1.5 text-[13px]">
            {device.os} · {device.clientVersion.split("-")[0]}
            {device.updateAvailable && (
              <Badge tone="info">
                <ArrowUpCircle className="size-3" />
                {t("ts.devices.update")}
              </Badge>
            )}
          </span>
        </TD>
        <TD>
          {device.tags.length > 0 ? (
            <span className="flex flex-wrap gap-1">
              {device.tags.map((tag) => (
                <Badge key={tag} tone="neutral">
                  {tag.replace(/^tag:/, "")}
                </Badge>
              ))}
            </span>
          ) : (
            <span className="text-fg-muted text-[13px]">{device.user}</span>
          )}
        </TD>
        <TD>
          {advertisesExit || advertisedSubnets.length > 0 ? (
            <span className="flex flex-wrap items-center gap-1 text-[12.5px]">
              {advertisesExit && (
                <Badge tone={exitEnabled ? "success" : "neutral"}>
                  <Globe className="size-3" />
                  {t("ts.devices.exitNode")}
                </Badge>
              )}
              {advertisedSubnets.length > 0 && (
                <Badge tone={enabledSubnets.length > 0 ? "success" : "neutral"}>
                  <Route className="size-3" />
                  {t("ts.devices.subnets", { enabled: enabledSubnets.length, total: advertisedSubnets.length })}
                </Badge>
              )}
            </span>
          ) : (
            <span className="text-fg-muted">—</span>
          )}
        </TD>
        <TD>
          {controllable && (
            <Dropdown>
              <DropdownTrigger asChild>
                <Button size="sm" variant="ghost" aria-label={t("common.actions")}>
                  <MoreHorizontal className="size-4" />
                </Button>
              </DropdownTrigger>
              <DropdownContent align="end">
                {!device.authorized && (
                  <DropdownItem onSelect={() => authorize.mutate(true)}>
                    <UserCheck className="size-3.5" />
                    {t("ts.devices.authorize")}
                  </DropdownItem>
                )}
                {(advertisesExit || advertisedSubnets.length > 0) && (
                  <DropdownItem onSelect={() => setRoutesOpen(true)}>
                    <Route className="size-3.5" />
                    {t("ts.devices.editRoutes")}
                  </DropdownItem>
                )}
                <DropdownItem onSelect={() => keyExpiry.mutate(!device.keyExpiryDisabled)}>
                  <Check className="size-3.5" />
                  {device.keyExpiryDisabled ? t("ts.devices.enableKeyExpiry") : t("ts.devices.disableKeyExpiry")}
                </DropdownItem>
                <DropdownItem onSelect={() => setConfirm("expire")}>
                  <ShieldAlert className="size-3.5" />
                  {t("ts.devices.expireKey")}
                </DropdownItem>
                <DropdownSeparator />
                <DropdownItem destructive onSelect={() => setConfirm("delete")}>
                  <Trash2 className="size-3.5" />
                  {t("ts.devices.remove")}
                </DropdownItem>
              </DropdownContent>
            </Dropdown>
          )}
        </TD>
      </TR>

      <ConfirmDialog
        open={confirm === "delete"}
        onOpenChange={(open) => !open && setConfirm(null)}
        title={t("ts.devices.removeTitle")}
        body={t("ts.devices.removeBody", { name: device.name })}
        confirmLabel={t("ts.devices.remove")}
        loading={remove.isPending}
        onConfirm={() => remove.mutate()}
      />
      <ConfirmDialog
        open={confirm === "expire"}
        onOpenChange={(open) => !open && setConfirm(null)}
        title={t("ts.devices.expireTitle")}
        body={t("ts.devices.expireBody", { name: device.name })}
        confirmLabel={t("ts.devices.expireKey")}
        loading={expire.isPending}
        onConfirm={() => expire.mutate()}
      />
      {routesOpen && (
        <RoutesDialog
          instanceId={instanceId}
          device={device}
          onClose={() => setRoutesOpen(false)}
          onSaved={() => {
            setRoutesOpen(false);
            refresh();
          }}
        />
      )}
    </>
  );
}

function RoutesDialog({
  instanceId,
  device,
  onClose,
  onSaved,
}: {
  instanceId: string;
  device: TsDevice;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const advertisedSubnets = device.advertisedRoutes.filter((r) => !isExitRoute(r));
  const advertisesExit = device.advertisedRoutes.some(isExitRoute);
  const [enabled, setEnabled] = useState<Set<string>>(
    () => new Set(device.enabledRoutes.filter((r) => !isExitRoute(r))),
  );
  const [exitOn, setExitOn] = useState(() => EXIT_ROUTES.every((r) => device.enabledRoutes.includes(r)));

  const save = useMutation({
    mutationFn: async () => {
      const routes = [...enabled];
      if (exitOn) routes.push(...EXIT_ROUTES);
      unwrap(
        await api.POST("/ts/{id}/devices/{deviceId}/routes", {
          params: { path: { id: instanceId, deviceId: device.id } },
          body: { routes },
        }),
      );
    },
    onSuccess: onSaved,
    onError: (err) =>
      toast("error", t("ts.devices.actionFailed"), isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={t("ts.devices.routesTitle", { name: device.name })}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>
              {t("common.save")}
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          {advertisesExit && (
            <Checkbox
              checked={exitOn}
              onCheckedChange={setExitOn}
              label={t("ts.devices.exitNodeLabel")}
              hint={t("ts.devices.exitNodeHint")}
            />
          )}
          {advertisedSubnets.length > 0 && (
            <div className="space-y-2">
              <p className="text-fg text-[13px] font-medium">{t("ts.devices.subnetRoutes")}</p>
              {advertisedSubnets.map((route) => (
                <Checkbox
                  key={route}
                  checked={enabled.has(route)}
                  onCheckedChange={(checked) =>
                    setEnabled((prev) => {
                      const next = new Set(prev);
                      if (checked) next.add(route);
                      else next.delete(route);
                      return next;
                    })
                  }
                  label={route}
                />
              ))}
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
