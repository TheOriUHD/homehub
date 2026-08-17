import { useMemo, useState } from "react";
import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { House } from "lucide-react";
import type { HaEntity } from "@/lib/api";
import { cn } from "@/lib/utils";
import { Card } from "@/ui/card";
import { Input } from "@/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { EmptyState, ErrorState } from "@/ui/states";
import { SkeletonRows } from "@/ui/skeleton";
import { EntityCard } from "./entity-card";
import { EntityDetailDialog } from "./entity-detail";
import { useHaEntities } from "./shared";

/** Domains that live on the Automations tab, never on the dashboard. */
const AUTOMATION_DOMAINS = ["automation", "scene", "script"];

export function HaDashboardPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/ha/$instanceId/" });
  const entities = useHaEntities(instanceId);

  const [search, setSearch] = useState("");
  const [areaFilter, setAreaFilter] = useState("all");
  const [domainFilter, setDomainFilter] = useState<Set<string>>(new Set());
  const [detailId, setDetailId] = useState<string | null>(null);

  const eligible = useMemo(
    () => (entities.data?.entities ?? []).filter((e) => !AUTOMATION_DOMAINS.includes(e.domain)),
    [entities.data],
  );

  // Chips come from the domains actually present for this user.
  const domains = useMemo(
    () => [...new Set(eligible.map((e) => e.domain))].sort(),
    [eligible],
  );

  const filtered = useMemo(() => {
    const needle = search.trim().toLowerCase();
    return eligible.filter((e) => {
      if (needle && !e.name.toLowerCase().includes(needle) && !e.entityId.toLowerCase().includes(needle)) {
        return false;
      }
      if (areaFilter === "none" && e.areaId) return false;
      if (areaFilter !== "all" && areaFilter !== "none" && e.areaId !== areaFilter) return false;
      if (domainFilter.size > 0 && !domainFilter.has(e.domain)) return false;
      return true;
    });
  }, [eligible, search, areaFilter, domainFilter]);

  // Group by area — named areas first (server order), "No area" last.
  const groups = useMemo(() => {
    const areas = entities.data?.areas ?? [];
    const byArea = new Map<string, HaEntity[]>();
    for (const e of filtered) {
      const key = e.areaId && areas.some((a) => a.id === e.areaId) ? e.areaId : "";
      const list = byArea.get(key);
      if (list) list.push(e);
      else byArea.set(key, [e]);
    }
    const sortByName = (list: HaEntity[]) => list.sort((a, b) => a.name.localeCompare(b.name));
    const result = areas
      .filter((a) => byArea.has(a.id))
      .map((a) => ({ id: a.id, name: a.name, entities: sortByName(byArea.get(a.id)!) }));
    if (byArea.has("")) {
      result.push({ id: "", name: t("ha.noArea"), entities: sortByName(byArea.get("")!) });
    }
    return result;
  }, [entities.data, filtered, t]);

  if (entities.isLoading) return <SkeletonRows rows={6} />;
  if (entities.isError) {
    return (
      <Card>
        <ErrorState error={entities.error} onRetry={() => void entities.refetch()} />
      </Card>
    );
  }

  const areas = entities.data?.areas ?? [];
  const detail = detailId ? eligible.find((e) => e.entityId === detailId) : undefined;

  const toggleDomain = (domain: string) =>
    setDomainFilter((prev) => {
      const next = new Set(prev);
      if (next.has(domain)) next.delete(domain);
      else next.add(domain);
      return next;
    });

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          placeholder={t("ha.searchPlaceholder")}
          className="max-w-72"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        {areas.length > 0 && (
          <Select value={areaFilter} onValueChange={setAreaFilter}>
            <SelectTrigger className="w-44" aria-label={t("ha.areaFilter")}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{t("ha.allAreas")}</SelectItem>
              {areas.map((area) => (
                <SelectItem key={area.id} value={area.id}>
                  {area.name}
                </SelectItem>
              ))}
              <SelectItem value="none">{t("ha.noArea")}</SelectItem>
            </SelectContent>
          </Select>
        )}
      </div>

      {domains.length > 1 && (
        <div className="flex flex-wrap gap-1.5">
          {domains.map((domain) => {
            const active = domainFilter.has(domain);
            return (
              <button
                key={domain}
                type="button"
                onClick={() => toggleDomain(domain)}
                aria-pressed={active}
                className={cn(
                  "cursor-pointer rounded-full border px-2.5 py-1 text-xs font-medium transition-colors",
                  active
                    ? "bg-accent-soft text-accent-soft-fg border-transparent"
                    : "border-edge text-fg-muted hover:bg-surface-hover hover:text-fg bg-surface",
                )}
              >
                {domain.replace(/_/g, " ")}
              </button>
            );
          })}
        </div>
      )}

      {eligible.length === 0 ? (
        <Card>
          <EmptyState icon={House} title={t("ha.empty.title")} body={t("ha.empty.body")} />
        </Card>
      ) : filtered.length === 0 ? (
        <Card>
          <EmptyState icon={House} title={t("ha.noMatches.title")} body={t("ha.noMatches.body")} />
        </Card>
      ) : (
        groups.map((group) => (
          <section key={group.id || "no-area"}>
            <h2 className="text-fg-subtle mb-2 text-xs font-semibold tracking-wider uppercase">
              {group.name}
              <span className="text-fg-subtle ml-1.5 font-normal normal-case">
                {t("ha.entityCount", { count: group.entities.length })}
              </span>
            </h2>
            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
              {group.entities.map((entity) => (
                <EntityCard
                  key={entity.entityId}
                  instanceId={instanceId}
                  entity={entity}
                  onOpenDetail={(e) => setDetailId(e.entityId)}
                />
              ))}
            </div>
          </section>
        ))
      )}

      {detail && (
        <EntityDetailDialog instanceId={instanceId} entity={detail} onClose={() => setDetailId(null)} />
      )}
    </div>
  );
}
