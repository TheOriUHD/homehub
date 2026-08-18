import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Network, Users } from "lucide-react";
import type { McPlayer } from "@/lib/api";
import { Badge } from "@/ui/badge";
import { Card } from "@/ui/card";
import { CopyButton, PageLoader } from "@/ui/misc";
import { EmptyState } from "@/ui/states";
import { useMcServer } from "./shared";

/**
 * Online players, live via "players-changed" pushes patching the shared
 * overview cache. Behind a Velocity proxy the downstream backends get their
 * own section with per-server player counts.
 */
export function McPlayersPage() {
  const { t } = useTranslation();
  const { instanceId, serverId } = useParams({ from: "/shell/mc/$instanceId/s/$serverId/players" });
  const server = useMcServer(instanceId, serverId);

  if (!server) return <PageLoader />;

  const data = server;
  const players = data.players ?? [];
  const downstream = data.status?.downstream ?? [];

  return (
    <div className="space-y-6">
      <section className="space-y-2.5">
        <h2 className="text-fg flex items-center gap-2 text-[15px] font-semibold">
          <Users className="text-fg-subtle size-4" />
          {t("mc.players.onlineTitle")}
          {data.connected && data.status && (
            <span className="text-fg-subtle text-[13px] font-normal">
              ({data.status.playersOnline}/{data.status.playersMax})
            </span>
          )}
        </h2>

        {players.length === 0 ? (
          <Card>
            <EmptyState
              icon={Users}
              title={data.connected ? t("mc.players.empty.title") : t("mc.players.offline.title")}
              body={data.connected ? t("mc.players.empty.body") : t("mc.players.offline.body")}
            />
          </Card>
        ) : (
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {players.map((player) => (
              <PlayerCard key={player.uuid ?? player.name} player={player} />
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
                <span className="text-fg min-w-0 flex-1 truncate text-sm font-medium">
                  {backend.name}
                </span>
                <Badge tone={backend.players > 0 ? "accent" : "neutral"}>
                  {t("mc.players.playerCount", { count: backend.players })}
                </Badge>
              </div>
            ))}
          </Card>
        </section>
      )}
    </div>
  );
}

function PlayerCard({ player }: { player: McPlayer }) {
  return (
    <Card className="flex items-center gap-3 p-3.5">
      <span className="bg-accent-soft text-accent-soft-fg flex size-9 shrink-0 items-center justify-center rounded-lg text-sm font-semibold">
        {player.name.charAt(0).toUpperCase()}
      </span>
      <div className="min-w-0 flex-1">
        <p className="text-fg truncate text-sm font-semibold">{player.name}</p>
        {player.uuid && (
          <p className="text-fg-subtle truncate font-mono text-[11px]">{player.uuid}</p>
        )}
      </div>
      {player.uuid && <CopyButton value={player.uuid} />}
    </Card>
  );
}
