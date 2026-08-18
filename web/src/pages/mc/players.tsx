import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Heart, MapPin, Network, Sparkles, Users, X } from "lucide-react";
import { api, isApiError, unwrap, type McPlayer } from "@/lib/api";
import { cn } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Card } from "@/ui/card";
import { CopyButton, PageLoader } from "@/ui/misc";
import { EmptyState } from "@/ui/states";
import { ItemGrid, MinecraftInventory, type Inventory } from "./inventory";
import { useMcServer } from "./shared";

/**
 * Online players, live via "players-changed" pushes patching the shared
 * overview cache. Each card shows the cheap live fields (position, health,
 * gamemode, ping); clicking one opens the full on-demand detail — inventory,
 * armor, ender chest, effects, and statistics fetched from the companion.
 * Behind a Velocity proxy the downstream backends get their own section.
 */
export function McPlayersPage() {
  const { t } = useTranslation();
  const { instanceId, serverId } = useParams({ from: "/shell/mc/$instanceId/s/$serverId/players" });
  const server = useMcServer(instanceId, serverId);
  const [selected, setSelected] = useState<McPlayer | null>(null);

  if (!server) return <PageLoader />;

  const players = server.players ?? [];
  const downstream = server.status?.downstream ?? [];

  return (
    <div className="space-y-6">
      <section className="space-y-2.5">
        <h2 className="text-fg flex items-center gap-2 text-[15px] font-semibold">
          <Users className="text-fg-subtle size-4" />
          {t("mc.players.onlineTitle")}
          {server.connected && server.status && (
            <span className="text-fg-subtle text-[13px] font-normal">
              ({server.status.playersOnline}/{server.status.playersMax})
            </span>
          )}
        </h2>

        {players.length === 0 ? (
          <Card>
            <EmptyState
              icon={Users}
              title={server.connected ? t("mc.players.empty.title") : t("mc.players.offline.title")}
              body={server.connected ? t("mc.players.empty.body") : t("mc.players.offline.body")}
            />
          </Card>
        ) : (
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {players.map((player) => (
              <PlayerCard
                key={player.uuid ?? player.name}
                player={player}
                onOpen={() => player.uuid && setSelected(player)}
              />
            ))}
          </div>
        )}
      </section>

      {downstream.length > 0 && (
        <section className="space-y-2.5">
          <h2 className="text-fg flex items-center gap-2 text-[15px] font-semibold">
            <Network className="text-fg-subtle size-4" />
            {t("mc.players.downstreamTitle")}
          </h2>
          <p className="text-fg-muted -mt-1 text-[13px]">{t("mc.players.downstreamBody")}</p>
          <Card className="divide-edge divide-y">
            {downstream.map((backend) => (
              <div key={backend.name} className="flex items-center gap-3 px-4 py-3">
                <span className="text-fg min-w-0 flex-1 truncate text-sm font-medium">{backend.name}</span>
                <Badge tone={backend.players > 0 ? "accent" : "neutral"}>
                  {t("mc.players.playerCount", { count: backend.players })}
                </Badge>
              </div>
            ))}
          </Card>
        </section>
      )}

      {selected?.uuid && (
        <PlayerDetailDialog
          instanceId={instanceId}
          serverId={serverId}
          player={selected}
          onClose={() => setSelected(null)}
        />
      )}
    </div>
  );
}

function PlayerCard({ player, onOpen }: { player: McPlayer; onOpen: () => void }) {
  const { t } = useTranslation();
  const health = typeof player.health === "number" ? player.health : null;
  const maxHealth = typeof player.maxHealth === "number" ? player.maxHealth : 20;
  return (
    <button type="button" onClick={onOpen} disabled={!player.uuid} className="group text-left disabled:cursor-default">
      <Card className={cn("h-full p-3.5 transition-colors", player.uuid && "group-hover:border-accent/50")}>
        <div className="flex items-center gap-3">
          <span className="bg-accent-soft text-accent-soft-fg flex size-9 shrink-0 items-center justify-center rounded-lg text-sm font-semibold">
            {player.name.charAt(0).toUpperCase()}
          </span>
          <div className="min-w-0 flex-1">
            <p className="text-fg truncate text-sm font-semibold">{player.name}</p>
            <p className="text-fg-subtle flex flex-wrap items-center gap-x-2 text-[11.5px]">
              {player.gameMode && <span className="capitalize">{player.gameMode}</span>}
              {typeof player.pingMs === "number" && <span>{player.pingMs} ms</span>}
              {typeof player.level === "number" && <span>lvl {player.level}</span>}
            </p>
          </div>
        </div>
        {(health !== null || player.world) && (
          <div className="text-fg-muted mt-2.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px]">
            {health !== null && (
              <span className="flex items-center gap-1">
                <Heart className="size-3 text-red-400" />
                {health.toFixed(0)}/{maxHealth.toFixed(0)}
              </span>
            )}
            {player.world && (
              <span className="flex min-w-0 items-center gap-1">
                <MapPin className="size-3 shrink-0" />
                <span className="truncate">
                  {player.world}
                  {typeof player.x === "number" &&
                    ` ${Math.round(player.x)}, ${Math.round(player.y ?? 0)}, ${Math.round(player.z ?? 0)}`}
                </span>
              </span>
            )}
          </div>
        )}
        {player.uuid && (
          <p className="text-accent mt-2 text-[11px] opacity-0 transition-opacity group-hover:opacity-100">
            {t("mc.players.viewDetail")}
          </p>
        )}
      </Card>
    </button>
  );
}

type PlayerDetail = {
  gameMode?: string;
  position?: { world?: string; x: number; y: number; z: number };
  vitals?: Record<string, number>;
  effects?: Array<{ type: string; amplifier: number; durationTicks: number }>;
  inventory?: Inventory;
  enderChest?: Array<{ slot?: number; type: string; count: number; name?: string; enchants?: Record<string, number> }>;
  statistics?: Record<string, number>;
  op?: boolean;
};

function PlayerDetailDialog({
  instanceId,
  serverId,
  player,
  onClose,
}: {
  instanceId: string;
  serverId: string;
  player: McPlayer;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const detail = useQuery({
    queryKey: ["mc", instanceId, "player", serverId, player.uuid],
    queryFn: async () =>
      unwrap(
        await api.GET("/mc/{id}/servers/{serverId}/players/{playerUuid}", {
          params: { path: { id: instanceId, serverId, playerUuid: player.uuid! } },
        }),
      ),
    refetchInterval: 5_000,
  });
  const d = detail.data as PlayerDetail | undefined;

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/45 p-4 backdrop-blur-[2px]"
      onClick={onClose}
    >
      <div
        className="border-edge bg-surface animate-[hh-pop-in_140ms_var(--ease-out-quick)] my-8 w-full max-w-2xl rounded-2xl border shadow-2"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="border-edge flex items-center gap-3 border-b px-5 py-3.5">
          <span className="bg-accent-soft text-accent-soft-fg flex size-9 items-center justify-center rounded-lg text-sm font-semibold">
            {player.name.charAt(0).toUpperCase()}
          </span>
          <div className="min-w-0 flex-1">
            <p className="text-fg flex items-center gap-2 text-[15px] font-semibold">
              {player.name}
              {d?.op && <Badge tone="warning">OP</Badge>}
              {d?.gameMode && <Badge tone="neutral">{d.gameMode}</Badge>}
            </p>
            {player.uuid && <p className="text-fg-subtle truncate font-mono text-[11px]">{player.uuid}</p>}
          </div>
          {player.uuid && <CopyButton value={player.uuid} />}
          <button type="button" onClick={onClose} aria-label={t("common.close")} className="text-fg-subtle hover:text-fg rounded-md p-1.5">
            <X className="size-4" />
          </button>
        </div>

        <div className="max-h-[70vh] space-y-5 overflow-y-auto px-5 py-4">
          {detail.isLoading && <PageLoader className="py-8" />}
          {detail.isError && (
            <p className="text-danger text-[13px]">
              {isApiError(detail.error) ? detail.error.message : t("common.unknownError")}
            </p>
          )}
          {d && (
            <>
              {d.position && (
                <Section title={t("mc.detail.position")} icon={MapPin}>
                  <p className="text-fg text-[13px]">
                    <span className="font-medium">{d.position.world}</span>{" "}
                    <span className="font-mono">
                      {d.position.x.toFixed(1)}, {d.position.y.toFixed(1)}, {d.position.z.toFixed(1)}
                    </span>
                  </p>
                </Section>
              )}
              {d.vitals && (
                <Section title={t("mc.detail.vitals")} icon={Heart}>
                  <StatGrid entries={Object.entries(d.vitals)} />
                </Section>
              )}
              {(d.effects?.length ?? 0) > 0 && (
                <Section title={t("mc.detail.effects")} icon={Sparkles}>
                  <div className="flex flex-wrap gap-1.5">
                    {d.effects!.map((e, i) => (
                      <Badge key={i} tone="info">
                        {e.type.replace(/_/g, " ")} {e.amplifier + 1}
                      </Badge>
                    ))}
                  </div>
                </Section>
              )}
              {d.inventory && (
                <Section title={t("mc.detail.inventory")}>
                  <MinecraftInventory inventory={d.inventory} uuid={player.uuid ?? undefined} />
                </Section>
              )}
              {(d.enderChest?.length ?? 0) > 0 && (
                <Section title={t("mc.detail.enderChest")}>
                  <ItemGrid items={d.enderChest!} />
                </Section>
              )}
              {d.statistics && (
                <Section title={t("mc.detail.statistics")}>
                  <StatGrid entries={Object.entries(d.statistics)} />
                </Section>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  );
}

function StatGrid({ entries }: { entries: Array<[string, number]> }) {
  return (
    <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-[13px] sm:grid-cols-3">
      {entries.map(([k, v]) => (
        <div key={k} className="flex justify-between gap-2">
          <dt className="text-fg-muted capitalize">{k.replace(/([A-Z])/g, " $1")}</dt>
          <dd className="text-fg font-medium tabular-nums">
            {Number.isInteger(v) ? v.toLocaleString() : v.toFixed(1)}
          </dd>
        </div>
      ))}
    </dl>
  );
}

function Section({ title, icon: Icon, children }: { title: string; icon?: typeof Heart; children: React.ReactNode }) {
  return (
    <div>
      <p className="text-fg mb-2 flex items-center gap-1.5 text-[13px] font-semibold">
        {Icon && <Icon className="text-fg-subtle size-3.5" />}
        {title}
      </p>
      {children}
    </div>
  );
}
