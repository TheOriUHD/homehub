import { Link, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Cpu, Gamepad2, MemoryStick, Server, Users } from "lucide-react";
import type { AmpServerInstance, AmpTarget } from "@/lib/api";
import { capacityBarClass } from "@/lib/capacity";
import { cn } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { ampStateBusy, ampStateTone, useAmpLive, useAmpOverview } from "./shared";

/**
 * AmpOverviewPage is the panel dashboard: one platform card per ADS target,
 * then the game-server instances as a card grid with live state, players,
 * and CPU/memory bars.
 */
export function AmpOverviewPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/amp/$instanceId/" });

  const overview = useAmpOverview(instanceId);
  useAmpLive(instanceId);

  if (overview.isLoading) return <PageLoader />;
  if (overview.isError) {
    return (
      <Card>
        <ErrorState error={overview.error} onRetry={() => void overview.refetch()} />
      </Card>
    );
  }

  const targets = overview.data!.targets;
  const allInstances = targets.flatMap((target) => target.instances);
  // The controller's own ADS "instance" is infrastructure, not a game server.
  const servers = allInstances.filter((inst) => inst.module !== "ADS");
  const running = servers.filter((inst) => inst.state === "running").length;
  const players = servers.reduce((sum, inst) => sum + (inst.players ?? 0), 0);

  return (
    <div className="space-y-5">
      <div className="grid items-stretch gap-3 md:grid-cols-2 xl:grid-cols-3">
        <Card>
          <CardBody className="flex items-center gap-4">
            <span className="bg-accent-soft text-accent-soft-fg flex size-11 shrink-0 items-center justify-center rounded-xl">
              <Gamepad2 className="size-5" />
            </span>
            <div className="min-w-0">
              <p className="text-fg text-2xl font-semibold tracking-tight">
                {running}
                <span className="text-fg-muted text-base font-normal"> / {servers.length}</span>
              </p>
              <p className="text-fg-muted text-[13px]">{t("amp.overview.serversRunning")}</p>
            </div>
          </CardBody>
        </Card>
        <Card>
          <CardBody className="flex items-center gap-4">
            <span className="bg-accent-soft text-accent-soft-fg flex size-11 shrink-0 items-center justify-center rounded-xl">
              <Users className="size-5" />
            </span>
            <div className="min-w-0">
              <p className="text-fg text-2xl font-semibold tracking-tight">{players}</p>
              <p className="text-fg-muted text-[13px]">{t("amp.overview.playersOnline")}</p>
            </div>
          </CardBody>
        </Card>
        {targets.map((target) => (
          <TargetCard key={target.instanceId} target={target} />
        ))}
      </div>

      {servers.length === 0 ? (
        <Card>
          <EmptyState icon={Gamepad2} title={t("amp.overview.emptyTitle")} body={t("amp.overview.emptyBody")} />
        </Card>
      ) : (
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {servers.map((inst) => (
            <InstanceCard key={inst.id} panelId={instanceId} inst={inst} />
          ))}
        </div>
      )}
    </div>
  );
}

function TargetCard({ target }: { target: AmpTarget }) {
  const { t } = useTranslation();
  return (
    <Card>
      <CardHeader
        title={
          <span className="flex items-center gap-2">
            <Server className="text-fg-muted size-4" />
            {target.friendlyName}
            <Badge tone={target.online ? "success" : "danger"}>
              <StatusDot tone={target.online ? "success" : "danger"} />
              {t(target.online ? "amp.overview.targetOnline" : "amp.overview.targetOffline")}
            </Badge>
          </span>
        }
      />
      <CardBody className="space-y-1 text-[13px]">
        {target.cpuModel && (
          <p className="text-fg-muted truncate">
            <span className="text-fg">{target.cpuModel}</span>
            {target.cores ? ` · ${t("amp.overview.cores", { count: target.cores })}` : null}
          </p>
        )}
        <p className="text-fg-muted">
          {target.platform && <span>{target.platform}</span>}
          {target.ramMb ? (
            <span>
              {target.platform ? " · " : ""}
              {t("amp.overview.ram", { gb: (target.ramMb / 1024).toFixed(0) })}
            </span>
          ) : null}
        </p>
      </CardBody>
    </Card>
  );
}

function InstanceCard({ panelId, inst }: { panelId: string; inst: AmpServerInstance }) {
  const { t } = useTranslation();
  const tone = ampStateTone(inst.state);
  const cpu = inst.cpuPercent ?? null;
  const memPct =
    inst.memUsedMb != null && inst.memMaxMb ? (inst.memUsedMb / inst.memMaxMb) * 100 : null;

  return (
    <Link
      to="/amp/$instanceId/i/$serverId"
      params={{ instanceId: panelId, serverId: inst.id }}
      className="group block"
    >
      <Card className="hover:border-accent/50 h-full transition-colors">
        <CardBody className="space-y-3">
          <div className="flex items-start justify-between gap-2">
            <div className="min-w-0">
              <p className="text-fg group-hover:text-accent truncate font-medium transition-colors">
                {inst.friendlyName}
              </p>
              <p className="text-fg-muted truncate text-[12px]">
                {inst.moduleName}
                {inst.description ? ` · ${inst.description}` : ""}
              </p>
            </div>
            <Badge tone={tone}>
              <StatusDot tone={tone} pulse={ampStateBusy(inst.state)} />
              {t(`amp.state.${inst.state}`, { defaultValue: inst.state })}
            </Badge>
          </div>

          <div className="grid grid-cols-3 gap-2 text-[12px]">
            <MetricCell
              icon={Users}
              label={t("amp.overview.players")}
              value={inst.players != null ? `${inst.players}/${inst.maxPlayers ?? "?"}` : "—"}
            />
            <MetricCell
              icon={Cpu}
              label="CPU"
              value={cpu != null ? `${cpu.toFixed(0)}%` : "—"}
            />
            <MetricCell
              icon={MemoryStick}
              label={t("amp.overview.memory")}
              value={
                inst.memUsedMb != null && inst.memMaxMb
                  ? `${(inst.memUsedMb / 1024).toFixed(1)}/${(inst.memMaxMb / 1024).toFixed(0)} GB`
                  : "—"
              }
            />
          </div>

          <div className="space-y-1.5">
            <UsageBar pct={cpu} />
            <UsageBar pct={memPct} />
          </div>
        </CardBody>
      </Card>
    </Link>
  );
}

function MetricCell({
  icon: Icon,
  label,
  value,
}: {
  icon: typeof Cpu;
  label: string;
  value: string;
}) {
  return (
    <div className="min-w-0">
      <p className="text-fg-muted flex items-center gap-1 truncate">
        <Icon className="size-3 shrink-0" />
        {label}
      </p>
      <p className="text-fg truncate font-medium">{value}</p>
    </div>
  );
}

function UsageBar({ pct }: { pct: number | null }) {
  return (
    <div className="bg-edge/60 h-1 overflow-hidden rounded-full">
      {pct != null && (
        <div
          className={cn("h-full rounded-full transition-all", capacityBarClass(pct))}
          style={{ width: `${Math.min(100, Math.max(2, pct))}%` }}
        />
      )}
    </div>
  );
}
