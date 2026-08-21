import { useMemo, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useRouterState } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ChevronRight, Database, Network, Server } from "lucide-react";
import { api, unwrap, type PveGuest } from "@/lib/api";
import { cn } from "@/lib/utils";
import { Input } from "@/ui/input";
import { Skeleton } from "@/ui/skeleton";
import { nodeAnchor, storageAnchor } from "./util";

/**
 * PveResourceTree is the Proxmox-style resource sidebar: datacenter → nodes →
 * their guests and storage pools, so any guest is one click away from anywhere
 * in the module. Every query here shares a cache key with the tab that owns
 * that data, so mounting the tree costs no extra requests.
 */
export function PveResourceTree({
  instanceId,
  className,
  onNavigate,
}: {
  instanceId: string;
  className?: string;
  /** Called after any row is followed — lets the mobile drawer close itself. */
  onNavigate?: () => void;
}) {
  const { t } = useTranslation();
  const location = useRouterState({ select: (s) => s.location });
  const base = `/pve/${instanceId}`;

  const summary = useQuery({
    queryKey: ["pve", instanceId, "summary"],
    queryFn: async () =>
      unwrap(await api.GET("/proxmox/{id}/summary", { params: { path: { id: instanceId } } })),
    refetchInterval: 30_000,
  });
  const guests = useQuery({
    queryKey: ["pve", instanceId, "guests"],
    queryFn: async () =>
      unwrap(await api.GET("/proxmox/{id}/guests", { params: { path: { id: instanceId } } })),
    refetchInterval: 30_000,
  });
  const storage = useQuery({
    queryKey: ["pve", instanceId, "storage"],
    queryFn: async () =>
      unwrap(await api.GET("/proxmox/{id}/storage", { params: { path: { id: instanceId } } })),
    refetchInterval: 60_000,
  });

  const [filter, setFilter] = useState("");
  // Collapsed rather than expanded state: a new node shows its guests by
  // default, which is what you want on a cluster you have never opened.
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());

  const needle = filter.trim().toLowerCase();
  const nodes = useMemo(() => {
    const match = (text: string) => !needle || text.toLowerCase().includes(needle);
    return (summary.data?.nodes ?? []).map((node) => {
      // A node matching the filter keeps all its children, so filtering by a
      // node name shows what runs on it rather than an empty branch.
      const all = needle.length > 0 && match(node.name);
      const guestRows = (guests.data ?? [])
        .filter((g) => g.node === node.name && (all || match(`${g.vmid} ${g.name} ${g.kind}`)))
        // Proxmox's Server View lists containers before VMs, each by vmid.
        .sort((a, b) => (a.kind === b.kind ? a.vmid - b.vmid : a.kind === "lxc" ? -1 : 1));
      const storageRows = (storage.data ?? [])
        .filter((s) => s.node === node.name && (all || match(s.storage)))
        .sort((a, b) => a.storage.localeCompare(b.storage));
      return { node, guests: guestRows, storage: storageRows };
    });
  }, [summary.data, guests.data, storage.data, needle]);

  const visible = needle
    ? nodes.filter((n) => n.guests.length > 0 || n.storage.length > 0 || n.node.name.toLowerCase().includes(needle))
    : nodes;

  const loading = summary.isLoading || guests.isLoading;
  const title = summary.data?.clusterName || t("pve.tree.datacenter");

  return (
    <div className={cn("bg-surface border-edge shadow-1 flex flex-col rounded-xl border", className)}>
      <div className="border-edge border-b p-2">
        <Input
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder={t("pve.tree.filter")}
          className="h-8 text-[13px]"
          aria-label={t("pve.tree.filter")}
        />
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto p-1.5">
        <Row
          to={base}
          active={location.pathname === base && !location.hash}
          depth={0}
          icon={<Network className="size-3.5 shrink-0" />}
          label={title}
          strong
          onNavigate={onNavigate}
        />

        {loading && (
          <div className="space-y-1.5 px-2 py-2">
            {Array.from({ length: 6 }, (_, i) => (
              <Skeleton key={i} className="h-4 w-full" />
            ))}
          </div>
        )}

        {!loading && visible.length === 0 && (
          <p className="text-fg-subtle px-2.5 py-3 text-[12.5px]">{t("pve.tree.noMatch")}</p>
        )}

        {visible.map(({ node, guests: guestRows, storage: storageRows }) => {
          // While filtering, every branch stays open — a hidden match is a
          // match you cannot act on.
          const open = needle.length > 0 || !collapsed.has(node.name);
          const running = guestRows.filter((g) => g.status === "running").length;
          return (
            <div key={node.name}>
              <Row
                to={base}
                hash={nodeAnchor(node.name)}
                active={location.pathname === base && location.hash === nodeAnchor(node.name)}
                depth={1}
                icon={<Server className="size-3.5 shrink-0" />}
                label={node.name}
                strong
                dot={node.status === "online" ? "success" : "danger"}
                meta={guestRows.length > 0 ? `${running}/${guestRows.length}` : undefined}
                onNavigate={onNavigate}
                caret={
                  <button
                    type="button"
                    aria-label={open ? t("pve.tree.collapse") : t("pve.tree.expand")}
                    aria-expanded={open}
                    onClick={(e) => {
                      e.preventDefault();
                      e.stopPropagation();
                      setCollapsed((prev) => {
                        const next = new Set(prev);
                        if (next.has(node.name)) next.delete(node.name);
                        else next.add(node.name);
                        return next;
                      });
                    }}
                    className="text-fg-subtle hover:text-fg -ml-1 cursor-pointer rounded p-0.5 transition-colors"
                  >
                    <ChevronRight className={cn("size-3 transition-transform", open && "rotate-90")} />
                  </button>
                }
              />

              {open && (
                <>
                  {guestRows.map((guest) => (
                    <GuestRow
                      key={guest.vmid}
                      base={base}
                      guest={guest}
                      active={location.pathname === `${base}/guests/${guest.vmid}`}
                      onNavigate={onNavigate}
                    />
                  ))}
                  {storageRows.map((pool) => (
                    <Row
                      key={`${pool.node}/${pool.storage}`}
                      to={`${base}/storage`}
                      hash={storageAnchor(pool)}
                      active={
                        location.pathname === `${base}/storage` && location.hash === storageAnchor(pool)
                      }
                      depth={2}
                      icon={<Database className="size-3.5 shrink-0" />}
                      label={pool.storage}
                      dim={!pool.active}
                      onNavigate={onNavigate}
                    />
                  ))}
                </>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

/** GuestRow carries the CT/VM chip and live status colour Proxmox shows. */
function GuestRow({
  base,
  guest,
  active,
  onNavigate,
}: {
  base: string;
  guest: PveGuest;
  active: boolean;
  onNavigate?: () => void;
}) {
  const running = guest.status === "running";
  return (
    <Row
      to={`${base}/guests/${guest.vmid}`}
      active={active}
      depth={2}
      dim={!running}
      onNavigate={onNavigate}
      icon={
        <span
          className={cn(
            "flex h-3.5 w-5 shrink-0 items-center justify-center rounded-[3px] text-[9px] font-bold",
            guest.kind === "qemu" ? "bg-accent-soft text-accent-soft-fg" : "bg-info-soft text-info-soft-fg",
            !running && "opacity-60",
          )}
        >
          {guest.kind === "qemu" ? "VM" : "CT"}
        </span>
      }
      label={
        <>
          <span className="text-fg-subtle tabular-nums">{guest.vmid}</span>{" "}
          {guest.name || `#${guest.vmid}`}
        </>
      }
      dot={running ? "success" : "neutral"}
    />
  );
}

const dotColor = {
  success: "bg-success",
  danger: "bg-danger",
  neutral: "bg-fg-subtle/50",
} as const;

function Row({
  to,
  hash,
  active,
  depth,
  icon,
  label,
  meta,
  dot,
  caret,
  strong,
  dim,
  onNavigate,
}: {
  to: string;
  hash?: string;
  active: boolean;
  depth: number;
  icon: ReactNode;
  label: ReactNode;
  meta?: string;
  dot?: keyof typeof dotColor;
  caret?: ReactNode;
  strong?: boolean;
  dim?: boolean;
  onNavigate?: () => void;
}) {
  return (
    <div className="flex items-center" style={{ paddingLeft: depth * 12 }}>
      {caret ?? (depth > 0 ? <span className="w-4 shrink-0" /> : null)}
      <Link
        to={to}
        hash={hash}
        onClick={onNavigate}
        title={typeof label === "string" ? label : undefined}
        className={cn(
          "flex min-w-0 flex-1 items-center gap-1.5 rounded-md px-1.5 py-1 text-[12.5px] transition-colors duration-100",
          active ? "bg-accent-soft text-accent-soft-fg font-medium" : "hover:bg-surface-hover",
          !active && (strong ? "text-fg font-medium" : dim ? "text-fg-subtle" : "text-fg-muted"),
        )}
      >
        {icon}
        <span className="min-w-0 flex-1 truncate">{label}</span>
        {meta && <span className="text-fg-subtle shrink-0 text-[11px] tabular-nums">{meta}</span>}
        {dot && <span className={cn("size-1.5 shrink-0 rounded-full", dotColor[dot])} />}
      </Link>
    </div>
  );
}
