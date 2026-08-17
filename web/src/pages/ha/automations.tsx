import { useMemo, useState } from "react";
import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Play, Workflow, Zap } from "lucide-react";
import type { HaEntity } from "@/lib/api";
import { formatRelative } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Card, CardHeader } from "@/ui/card";
import { Switch } from "@/ui/switch";
import { EmptyState, ErrorState } from "@/ui/states";
import { SkeletonRows } from "@/ui/skeleton";
import { EntityDetailDialog } from "./entity-detail";
import { isUnknownState, useHaEntities, useHaService } from "./shared";

export function HaAutomationsPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/ha/$instanceId/automations" });
  const entities = useHaEntities(instanceId);
  const [detailId, setDetailId] = useState<string | null>(null);

  const byDomain = useMemo(() => {
    const list = entities.data?.entities ?? [];
    const pick = (domain: string) =>
      list.filter((e) => e.domain === domain).sort((a, b) => a.name.localeCompare(b.name));
    return { automations: pick("automation"), scenes: pick("scene"), scripts: pick("script") };
  }, [entities.data]);

  if (entities.isLoading) return <SkeletonRows rows={6} />;
  if (entities.isError) {
    return (
      <Card>
        <ErrorState error={entities.error} onRetry={() => void entities.refetch()} />
      </Card>
    );
  }

  const { automations, scenes, scripts } = byDomain;
  const detail = detailId
    ? (entities.data?.entities ?? []).find((e) => e.entityId === detailId)
    : undefined;

  if (automations.length === 0 && scenes.length === 0 && scripts.length === 0) {
    return (
      <Card>
        <EmptyState
          icon={Workflow}
          title={t("ha.automations.empty.title")}
          body={t("ha.automations.empty.body")}
        />
      </Card>
    );
  }

  return (
    <div className="space-y-4">
      {automations.length > 0 && (
        <Card>
          <CardHeader title={t("ha.automations.title")} />
          <ul>
            {automations.map((entity) => (
              <AutomationRow
                key={entity.entityId}
                instanceId={instanceId}
                entity={entity}
                onOpenDetail={setDetailId}
              />
            ))}
          </ul>
        </Card>
      )}

      {scenes.length > 0 && (
        <Card>
          <CardHeader title={t("ha.automations.scenes")} />
          <ul>
            {scenes.map((entity) => (
              <SimpleActionRow
                key={entity.entityId}
                instanceId={instanceId}
                entity={entity}
                actionLabel={t("ha.scene.activate")}
                onOpenDetail={setDetailId}
              />
            ))}
          </ul>
        </Card>
      )}

      {scripts.length > 0 && (
        <Card>
          <CardHeader title={t("ha.automations.scripts")} />
          <ul>
            {scripts.map((entity) => (
              <SimpleActionRow
                key={entity.entityId}
                instanceId={instanceId}
                entity={entity}
                actionLabel={t("ha.script.run")}
                showRunning
                onOpenDetail={setDetailId}
              />
            ))}
          </ul>
        </Card>
      )}

      {detail && (
        <EntityDetailDialog instanceId={instanceId} entity={detail} onClose={() => setDetailId(null)} />
      )}
    </div>
  );
}

function lastTriggered(entity: HaEntity): string | null {
  const v = entity.attributes.last_triggered;
  return typeof v === "string" && v ? v : null;
}

function AutomationRow({
  instanceId,
  entity,
  onOpenDetail,
}: {
  instanceId: string;
  entity: HaEntity;
  onOpenDetail: (entityId: string) => void;
}) {
  const { t } = useTranslation();
  const svc = useHaService(instanceId);
  const triggered = lastTriggered(entity);

  return (
    <li className="border-edge flex flex-wrap items-center gap-x-4 gap-y-2 border-b px-5 py-3 last:border-b-0">
      <Switch
        checked={entity.state === "on"}
        disabled={svc.isPending || isUnknownState(entity.state)}
        aria-label={entity.name}
        onCheckedChange={(on) =>
          svc.mutate({ entityId: entity.entityId, service: on ? "turn_on" : "turn_off" })
        }
      />
      <button
        type="button"
        onClick={() => onOpenDetail(entity.entityId)}
        className="group min-w-0 flex-1 cursor-pointer text-left"
      >
        <p className="text-fg group-hover:text-accent truncate text-sm font-medium transition-colors">
          {entity.name}
        </p>
        <p className="text-fg-subtle truncate text-xs">
          {triggered
            ? t("ha.automations.lastTriggered", { time: formatRelative(triggered) })
            : t("ha.automations.neverTriggered")}
        </p>
      </button>
      <Button
        size="sm"
        disabled={svc.isPending}
        loading={svc.isPending && svc.variables?.service === "trigger"}
        onClick={() => svc.mutate({ entityId: entity.entityId, service: "trigger" })}
      >
        <Zap className="size-3.5" /> {t("ha.automations.triggerNow")}
      </Button>
    </li>
  );
}

/** Scene/script row: name plus one action button. */
function SimpleActionRow({
  instanceId,
  entity,
  actionLabel,
  showRunning,
  onOpenDetail,
}: {
  instanceId: string;
  entity: HaEntity;
  actionLabel: string;
  showRunning?: boolean;
  onOpenDetail: (entityId: string) => void;
}) {
  const { t } = useTranslation();
  const svc = useHaService(instanceId);
  const triggered = lastTriggered(entity);
  // A scene's state is the timestamp it last activated.
  const activated =
    entity.domain === "scene" && !Number.isNaN(Date.parse(entity.state)) ? entity.state : null;
  const subtitle = triggered
    ? t("ha.automations.lastTriggered", { time: formatRelative(triggered) })
    : activated
      ? t("ha.automations.lastActivated", { time: formatRelative(activated) })
      : entity.entityId;

  return (
    <li className="border-edge flex flex-wrap items-center gap-x-4 gap-y-2 border-b px-5 py-3 last:border-b-0">
      <button
        type="button"
        onClick={() => onOpenDetail(entity.entityId)}
        className="group min-w-0 flex-1 cursor-pointer text-left"
      >
        <p className="text-fg group-hover:text-accent flex items-center gap-2 truncate text-sm font-medium transition-colors">
          {entity.name}
          {showRunning && entity.state === "on" && (
            <Badge tone="info">{t("ha.automations.running")}</Badge>
          )}
        </p>
        <p className="text-fg-subtle truncate text-xs">{subtitle}</p>
      </button>
      <Button
        size="sm"
        disabled={svc.isPending}
        loading={svc.isPending}
        onClick={() => svc.mutate({ entityId: entity.entityId, service: "turn_on" })}
      >
        <Play className="size-3.5" /> {actionLabel}
      </Button>
    </li>
  );
}
