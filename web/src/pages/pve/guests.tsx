import { useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Boxes, Play, Power, RotateCcw, Square } from "lucide-react";
import { api, unwrap, isApiError, type PveGuest, type PveGuestAction } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useLiveInvalidate } from "@/lib/live";
import { cn, formatBytes, formatDuration } from "@/lib/utils";
import { Card } from "@/ui/card";
import { Button } from "@/ui/button";
import { Badge } from "@/ui/badge";
import { Input } from "@/ui/input";
import { Checkbox } from "@/ui/misc";
import { ConfirmDialog } from "@/ui/confirm";
import { EmptyState, ErrorState } from "@/ui/states";
import { SkeletonRows } from "@/ui/skeleton";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { useToast } from "@/ui/toast";
import { primaryIpv4, useGuestAgent } from "./agent";

export const guestStatusTone = {
  running: "success",
  stopped: "neutral",
  paused: "warning",
  suspended: "warning",
} as const;

export function guestStatusLabel(t: (k: string) => string, status: string): string {
  switch (status) {
    case "running":
    case "stopped":
    case "paused":
    case "suspended":
      return t(`pve.guests.status.${status}`);
    default:
      return status;
  }
}

export function PveGuestsPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/pve/$instanceId/guests" });
  const navigate = useNavigate();
  const { can } = useAuth();
  const toast = useToast();
  const queryClient = useQueryClient();

  const guests = useQuery({
    queryKey: ["pve", instanceId, "guests"],
    queryFn: async () =>
      unwrap(await api.GET("/proxmox/{id}/guests", { params: { path: { id: instanceId } } })),
  });
  useLiveInvalidate(`integration:proxmox/${instanceId}`, [["pve", instanceId, "guests"]], ["guests-changed"]);

  const [filter, setFilter] = useState("");
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [confirmBulk, setConfirmBulk] = useState<PveGuestAction | null>(null);

  const filtered = useMemo(() => {
    const list = guests.data ?? [];
    const needle = filter.trim().toLowerCase();
    if (!needle) return list;
    return list.filter(
      (g) =>
        g.name.toLowerCase().includes(needle) ||
        String(g.vmid).includes(needle) ||
        g.node.toLowerCase().includes(needle) ||
        g.kind.includes(needle),
    );
  }, [guests.data, filter]);

  const controllable = (g: PveGuest) => can(`proxmox/${instanceId}/guest/${g.vmid}`, "control");

  const bulk = useMutation({
    mutationFn: async (action: PveGuestAction) =>
      unwrap(
        await api.POST("/proxmox/{id}/bulk", {
          params: { path: { id: instanceId } },
          body: { vmids: [...selected], action },
        }),
      ),
    onSuccess: (result, action) => {
      const ok = result.results.filter((r) => r.upid).length;
      const failed = result.results.filter((r) => r.error);
      if (failed.length === 0) {
        toast("success", t("pve.actions.started"), t("pve.actions.bulkTitle", { action: t(`pve.actions.${action}`), count: ok }));
      } else {
        toast("warning", t("pve.actions.bulkPartial", { ok, failed: failed.length }),
          failed.map((f) => `${f.vmid}: ${f.error}`).join("\n"));
      }
      setSelected(new Set());
      setConfirmBulk(null);
      void queryClient.invalidateQueries({ queryKey: ["pve", instanceId, "guests"] });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  if (guests.isLoading) return <SkeletonRows rows={6} />;
  if (guests.isError) {
    return (
      <Card>
        <ErrorState error={guests.error} onRetry={() => void guests.refetch()} />
      </Card>
    );
  }

  const selectable = filtered.filter((g) => !g.template && controllable(g));
  const allSelected = selectable.length > 0 && selectable.every((g) => selected.has(g.vmid));

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          placeholder={t("pve.guests.searchPlaceholder")}
          className="max-w-72"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
        {selected.size > 0 && (
          <div className="bg-accent-soft text-accent-soft-fg flex items-center gap-2 rounded-lg px-3 py-1.5 text-[13px] font-medium">
            {t("pve.guests.selected", { count: selected.size })}
            <span className="flex gap-1">
              <Button size="sm" variant="secondary" onClick={() => bulk.mutate("start")} loading={bulk.isPending}>
                <Play className="size-3.5" /> {t("pve.actions.start")}
              </Button>
              <Button size="sm" variant="secondary" onClick={() => bulk.mutate("shutdown")} loading={bulk.isPending}>
                <Power className="size-3.5" /> {t("pve.actions.shutdown")}
              </Button>
              <Button size="sm" variant="secondary" onClick={() => bulk.mutate("reboot")} loading={bulk.isPending}>
                <RotateCcw className="size-3.5" /> {t("pve.actions.reboot")}
              </Button>
              <Button size="sm" variant="danger-ghost" onClick={() => setConfirmBulk("stop")} loading={bulk.isPending}>
                <Square className="size-3.5" /> {t("pve.actions.stop")}
              </Button>
            </span>
          </div>
        )}
      </div>

      <Card>
        {filtered.length === 0 ? (
          <EmptyState icon={Boxes} title={t("pve.guests.empty.title")} body={t("pve.guests.empty.body")} />
        ) : (
          <Table>
            <THead>
              <tr>
                <TH className="w-10">
                  <Checkbox
                    checked={allSelected}
                    onCheckedChange={(checked) =>
                      setSelected(checked ? new Set(selectable.map((g) => g.vmid)) : new Set())
                    }
                    label=""
                  />
                </TH>
                <TH>{t("pve.guests.columns.guest")}</TH>
                <TH className="hidden sm:table-cell">{t("pve.guests.columns.node")}</TH>
                <TH>{t("pve.guests.columns.status")}</TH>
                <TH className="hidden lg:table-cell">{t("pve.guests.columns.ip")}</TH>
                <TH className="hidden md:table-cell">{t("pve.guests.columns.cpu")}</TH>
                <TH className="hidden md:table-cell">{t("pve.guests.columns.memory")}</TH>
                <TH className="hidden lg:table-cell">{t("pve.guests.columns.uptime")}</TH>
              </tr>
            </THead>
            <TBody>
              {filtered.map((guest) => (
                <TR
                  key={guest.vmid}
                  interactive
                  onClick={() =>
                    void navigate({
                      to: "/pve/$instanceId/guests/$vmid",
                      params: { instanceId, vmid: String(guest.vmid) },
                    })
                  }
                >
                  <TD onClick={(e) => e.stopPropagation()}>
                    {!guest.template && controllable(guest) && (
                      <Checkbox
                        checked={selected.has(guest.vmid)}
                        onCheckedChange={(checked) =>
                          setSelected((prev) => {
                            const next = new Set(prev);
                            if (checked) next.add(guest.vmid);
                            else next.delete(guest.vmid);
                            return next;
                          })
                        }
                        label=""
                      />
                    )}
                  </TD>
                  <TD>
                    <div className="flex items-center gap-2.5">
                      <span
                        className={cn(
                          "flex size-7 shrink-0 items-center justify-center rounded-md text-[10px] font-bold",
                          guest.kind === "qemu" ? "bg-accent-soft text-accent-soft-fg" : "bg-info-soft text-info-soft-fg",
                        )}
                      >
                        {guest.kind === "qemu" ? "VM" : "CT"}
                      </span>
                      <div className="min-w-0">
                        <p className="text-fg flex items-center gap-1.5 truncate font-medium">
                          {guest.name || `#${guest.vmid}`}
                          {guest.template && <Badge tone="neutral">{t("pve.guests.template")}</Badge>}
                        </p>
                        <p className="text-fg-subtle text-xs">{guest.vmid}</p>
                      </div>
                    </div>
                  </TD>
                  <TD className="text-fg-muted hidden sm:table-cell">{guest.node}</TD>
                  <TD>
                    {!guest.template && (
                      <Badge tone={guestStatusTone[guest.status as keyof typeof guestStatusTone] ?? "neutral"}>
                        {guestStatusLabel(t, guest.status)}
                      </Badge>
                    )}
                  </TD>
                  <TD className="hidden lg:table-cell">
                    <AgentIpCell instanceId={instanceId} guest={guest} />
                  </TD>
                  <TD className="text-fg-muted hidden md:table-cell">
                    {guest.status === "running" ? `${Math.round(guest.cpu * 100)}%` : "—"}
                  </TD>
                  <TD className="text-fg-muted hidden md:table-cell">
                    {guest.status === "running"
                      ? `${formatBytes(guest.memBytes)} / ${formatBytes(guest.maxMemBytes)}`
                      : formatBytes(guest.maxMemBytes)}
                  </TD>
                  <TD className="text-fg-muted hidden lg:table-cell">
                    {guest.status === "running" ? formatDuration(guest.uptimeSeconds) : "—"}
                  </TD>
                </TR>
              ))}
            </TBody>
          </Table>
        )}
      </Card>

      <ConfirmDialog
        open={confirmBulk !== null}
        onOpenChange={(open) => !open && setConfirmBulk(null)}
        title={t("pve.actions.bulkTitle", { action: t("pve.actions.stop"), count: selected.size })}
        body={t("pve.actions.stopConfirmBody")}
        confirmLabel={t("pve.actions.stop")}
        loading={bulk.isPending}
        onConfirm={() => confirmBulk && bulk.mutate(confirmBulk)}
      />
    </div>
  );
}

/**
 * AgentIpCell lazily shows the agent-reported primary IPv4. The fetch fires
 * only once the cell scrolls into view, and only for running QEMU guests —
 * a long list never fans out into hundreds of eager requests.
 */
function AgentIpCell({ instanceId, guest }: { instanceId: string; guest: PveGuest }) {
  const ref = useRef<HTMLSpanElement | null>(null);
  const [seen, setSeen] = useState(false);
  const eligible = guest.kind === "qemu" && guest.status === "running" && !guest.template;

  useEffect(() => {
    if (!eligible || seen) return;
    const el = ref.current;
    if (!el) return;
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) setSeen(true);
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [eligible, seen]);

  const agent = useGuestAgent(instanceId, guest.vmid, eligible && seen);
  const ip = agent.data?.available ? primaryIpv4(agent.data) : null;

  return (
    <span ref={ref} className={cn("font-mono text-xs", ip ? "text-fg" : "text-fg-subtle")}>
      {ip ?? "—"}
    </span>
  );
}
