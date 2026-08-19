import { useEffect, useState } from "react";
import { Link, Navigate, Outlet, useRouterState } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import {
  Activity,
  BellRing,
  Boxes,
  ChartSpline,
  Container,
  Database,
  DatabaseBackup,
  Eye,
  FileClock,
  Gamepad2,
  HardDrive,
  Cctv,
  Network,
  Waypoints,
  Home,
  House,
  KeyRound,
  LogOut,
  Menu,
  Moon,
  Pickaxe,
  Search,
  Server,
  Settings,
  Shield,
  ShieldCheck,
  Sparkles,
  Sun,
  Users,
  X,
 Wrench, ServerCog } from "lucide-react";
import { cn } from "@/lib/utils";
import { useAuth } from "@/lib/auth";
import { RES } from "@/lib/perms";
import { AssistantWidget } from "./assistant-widget";
import { api, unwrap } from "@/lib/api";
import { SYSTEMS_RES, useIntegrations, worstStatus } from "@/lib/systems";
import { useTheme } from "@/lib/theme";
import { useBackupsVisible } from "@/pages/backups/shared";
import { LogoMark, Wordmark } from "./logo";
import { NotificationsBell } from "./notifications";
import { CommandPalette } from "./command-palette";
import { Kbd, PageLoader } from "@/ui/misc";
import { Button } from "@/ui/button";
import {
  Dropdown,
  DropdownContent,
  DropdownItem,
  DropdownSeparator,
  DropdownTrigger,
} from "@/ui/dropdown";

type NavItem = {
  to: string;
  label: string;
  icon: typeof Home;
  requires?: string;
};

export function AppShell() {
  const { t } = useTranslation();
  const { me, loading, can, signOut } = useAuth();
  const [theme, setTheme] = useTheme();
  const [mobileNav, setMobileNav] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const pathname = useRouterState({ select: (s) => s.location.pathname });

  // Close the mobile drawer on navigation.
  useEffect(() => setMobileNav(false), [pathname]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setPaletteOpen((v) => !v);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // Every hook must run on every render — these sit above the loading/auth
  // returns (Rules of Hooks) and gate their fetches on a signed-in session
  // instead of being skipped.
  // Backups has no client-side grant shortcut — the entry hides when the
  // server answers the targets probe with a 403.
  const backupsVisible = useBackupsVisible(!!me);
  const assistantState = useQuery({
    queryKey: ["assistant"],
    queryFn: async () => unwrap(await api.GET("/assistant")),
    enabled: !!me,
  });
  const assistantVisible = Boolean(
    assistantState.data && (assistantState.data.enabled || assistantState.data.canConfigure),
  );
  // Connected systems the user may see — the server filters by permission.
  const integrations = useIntegrations(!!me);

  if (loading) {
    return (
      <div className="bg-bg flex min-h-full items-center justify-center">
        <PageLoader />
      </div>
    );
  }
  if (!me) return <Navigate to="/login" />;
  if (me.user.mustChangePassword) return <Navigate to="/change-password" />;

  const mainNav: NavItem[] = [
    { to: "/", label: t("nav.overview"), icon: Home },
    // The assistant entry appears when it's enabled, or for admins who
    // could enable it.
    ...(assistantVisible ? [{ to: "/assistant", label: t("nav.assistant"), icon: Sparkles }] : []),
    { to: "/health", label: t("nav.health"), icon: Activity, requires: RES.health },
    { to: "/tools", label: t("nav.tools"), icon: Wrench },
    // History and Alerts are visible to everyone — their contents are already
    // permission-filtered server-side.
    { to: "/history", label: t("nav.history"), icon: ChartSpline },
    { to: "/alerts", label: t("nav.alerts"), icon: BellRing },
    ...(backupsVisible
      ? [{ to: "/backups", label: t("nav.backups"), icon: DatabaseBackup }]
      : []),
  ];
  const adminNav: NavItem[] = [
    { to: "/admin/users", label: t("nav.users"), icon: Users, requires: RES.users },
    { to: "/admin/roles", label: t("nav.roles"), icon: Shield, requires: RES.roles },
    { to: "/admin/audit", label: t("nav.audit"), icon: FileClock, requires: RES.audit },
    { to: "/admin/settings", label: t("nav.settings"), icon: Settings, requires: RES.settings },
  ];

  const visible = (items: NavItem[]) => items.filter((i) => !i.requires || can(i.requires));
  const adminVisible = visible(adminNav);

  // One SYSTEMS entry per type present — the dot aggregates the worst
  // instance status. Types with no unified module link straight to the
  // instance when there is exactly one, else to the per-type landing.
  const rows = (integrations.data ?? []).filter((row) => row.enabled);
  const pveRows = rows.filter((row) => row.type === "proxmox");
  const dockerRows = rows.filter((row) => row.type === "docker");
  const dnsRows = rows.filter((row) => row.type === "pihole" || row.type === "adguard");
  const dbRows = rows.filter((row) => row.type === "mysql" || row.type === "mongodb");
  const haRows = rows.filter((row) => row.type === "homeassistant");
  const mcRows = rows.filter((row) => row.type === "minecraft");
  const ampRows = rows.filter((row) => row.type === "amp");
  const tsRows = rows.filter((row) => row.type === "tailscale");
  const caddyRows = rows.filter((row) => row.type === "caddy");
  const nvrRows = rows.filter((row) => row.type === "aperture");
  const truenasRows = rows.filter((row) => row.type === "truenas");
  const ppRows = rows.filter((row) => row.type === "patchpilot");
  const systemsVisible = (integrations.data ?? []).length > 0 || can(SYSTEMS_RES, "configure");

  type TypeEntry = NavItem & { status: string; prefix: string };
  const typeEntries: TypeEntry[] = [
    ...(pveRows.length > 0
      ? [{
          to: pveRows.length === 1 ? `/pve/${pveRows[0].id}` : "/pve",
          label: t("systems.types.proxmox"),
          icon: Server,
          status: worstStatus(pveRows),
          prefix: "/pve",
        }]
      : []),
    ...(dockerRows.length > 0
      ? [{
          to: "/docker",
          label: t("systems.types.docker"),
          icon: Container,
          status: worstStatus(dockerRows),
          prefix: "/docker",
        }]
      : []),
    ...(dnsRows.length > 0
      ? [{
          to: "/dns",
          label: t("dns.title"),
          icon: ShieldCheck,
          status: worstStatus(dnsRows),
          prefix: "/dns",
        }]
      : []),
    ...(dbRows.length > 0
      ? [{
          to: "/db",
          label: t("db.title"),
          icon: Database,
          status: worstStatus(dbRows),
          prefix: "/db",
        }]
      : []),
    ...(haRows.length > 0
      ? [{
          to: haRows.length === 1 ? `/ha/${haRows[0].id}` : "/ha",
          label: t("systems.types.homeassistant"),
          icon: House,
          status: worstStatus(haRows),
          prefix: "/ha",
        }]
      : []),
    ...(mcRows.length > 0
      ? [{
          to: mcRows.length === 1 ? `/mc/${mcRows[0].id}` : "/mc",
          label: t("systems.types.minecraft"),
          icon: Pickaxe,
          status: worstStatus(mcRows),
          prefix: "/mc",
        }]
      : []),
    ...(ampRows.length > 0
      ? [{
          to: ampRows.length === 1 ? `/amp/${ampRows[0].id}` : "/amp",
          label: t("systems.types.amp"),
          icon: Gamepad2,
          status: worstStatus(ampRows),
          prefix: "/amp",
        }]
      : []),
    ...(tsRows.length > 0
      ? [{
          to: tsRows.length === 1 ? `/ts/${tsRows[0].id}` : "/ts",
          label: t("systems.types.tailscale"),
          icon: Network,
          status: worstStatus(tsRows),
          prefix: "/ts",
        }]
      : []),
    ...(caddyRows.length > 0
      ? [{
          to: caddyRows.length === 1 ? `/caddy/${caddyRows[0].id}` : "/caddy",
          label: t("systems.types.caddy"),
          icon: Waypoints,
          status: worstStatus(caddyRows),
          prefix: "/caddy",
        }]
      : []),
    ...(nvrRows.length > 0
      ? [{
          to: nvrRows.length === 1 ? `/nvr/${nvrRows[0].id}` : "/nvr",
          label: t("systems.types.aperture"),
          icon: Cctv,
          status: worstStatus(nvrRows),
          prefix: "/nvr",
        }]
      : []),
    ...(truenasRows.length > 0
      ? [{
          to: truenasRows.length === 1 ? `/truenas/${truenasRows[0].id}` : "/truenas",
          label: t("systems.types.truenas"),
          icon: HardDrive,
          status: worstStatus(truenasRows),
          prefix: "/truenas",
        }]
      : []),
    ...(ppRows.length > 0
      ? [{
          to: ppRows.length === 1 ? `/patchpilot/${ppRows[0].id}` : "/patchpilot",
          label: t("systems.types.patchpilot"),
          icon: ServerCog,
          status: worstStatus(ppRows),
          prefix: "/patchpilot",
        }]
      : []),
  ];

  const initials = me.user.displayName
    .split(/\s+/)
    .map((w) => w[0])
    .slice(0, 2)
    .join("")
    .toUpperCase();

  const sidebar = (
    <div className="flex h-full flex-col">
      <div className="flex h-14 items-center gap-2.5 px-4">
        <LogoMark />
        <Wordmark />
      </div>

      <nav className="flex-1 space-y-6 overflow-y-auto px-3 py-3">
        <div className="space-y-0.5">
          {visible(mainNav).map((item) => (
            <SidebarLink key={item.to} item={item} active={pathname === item.to} />
          ))}
        </div>
        {typeEntries.length > 0 && (
          <div>
            <p className="text-fg-subtle mb-1.5 px-2.5 text-[11px] font-semibold tracking-wider uppercase">
              {t("nav.systems")}
            </p>
            <div className="space-y-0.5">
              {typeEntries.map((entry) => (
                <SidebarLink
                  key={entry.prefix}
                  item={entry}
                  active={pathname.startsWith(entry.prefix)}
                  status={entry.status}
                />
              ))}
            </div>
          </div>
        )}
        {(adminVisible.length > 0 || systemsVisible) && (
          <div>
            <p className="text-fg-subtle mb-1.5 px-2.5 text-[11px] font-semibold tracking-wider uppercase">
              {t("nav.administration")}
            </p>
            <div className="space-y-0.5">
              {systemsVisible && (
                <SidebarLink
                  item={{ to: "/systems", label: t("nav.systems"), icon: Boxes }}
                  active={pathname.startsWith("/systems")}
                />
              )}
              {adminVisible.map((item) => (
                <SidebarLink key={item.to} item={item} active={pathname.startsWith(item.to)} />
              ))}
            </div>
          </div>
        )}
      </nav>

      <div className="border-edge border-t p-3">
        <Dropdown>
          <DropdownTrigger asChild>
            <button className="hover:bg-surface-hover flex w-full cursor-pointer items-center gap-2.5 rounded-lg p-2 text-left transition-colors">
              <span className="bg-accent-soft text-accent-soft-fg flex size-8 shrink-0 items-center justify-center rounded-full text-xs font-semibold">
                {initials || "?"}
              </span>
              <span className="min-w-0 flex-1">
                <span className="text-fg block truncate text-[13px] font-medium">{me.user.displayName}</span>
                <span className="text-fg-subtle block truncate text-xs">{me.user.username}</span>
              </span>
            </button>
          </DropdownTrigger>
          <DropdownContent side="top" align="start" className="w-56">
            <DropdownItem asChild>
              <Link to="/account">
                <KeyRound className="size-4" />
                {t("nav.account")}
              </Link>
            </DropdownItem>
            <DropdownItem onSelect={() => setTheme(theme === "dark" ? "light" : "dark")}>
              {theme === "dark" ? <Sun className="size-4" /> : <Moon className="size-4" />}
              {theme === "dark" ? t("theme.light") : t("theme.dark")}
            </DropdownItem>
            <DropdownSeparator />
            <DropdownItem destructive onSelect={() => void signOut()}>
              <LogOut className="size-4" />
              {t("nav.signOut")}
            </DropdownItem>
          </DropdownContent>
        </Dropdown>
      </div>
    </div>
  );

  return (
    <div className="bg-bg flex min-h-full">
      {/* Desktop sidebar */}
      <aside className="border-edge bg-surface fixed inset-y-0 left-0 z-30 hidden w-60 border-r lg:block">
        {sidebar}
      </aside>

      {/* Mobile drawer */}
      {mobileNav && (
        <div className="fixed inset-0 z-40 lg:hidden">
          <div
            className="absolute inset-0 bg-black/45 animate-[hh-fade-in_150ms_var(--ease-out-quick)]"
            onClick={() => setMobileNav(false)}
          />
          <aside className="bg-surface absolute inset-y-0 left-0 w-64 shadow-3 animate-[hh-slide-up_180ms_var(--ease-out-quick)]">
            <button
              className="interactive-muted absolute top-4 right-3 flex size-7 cursor-pointer items-center justify-center"
              onClick={() => setMobileNav(false)}
              aria-label={t("common.close")}
            >
              <X className="size-4" />
            </button>
            {sidebar}
          </aside>
        </div>
      )}

      <div className="flex min-h-full w-full flex-col lg:pl-60">
        {me.viewAsRoleId && <ViewAsBanner roleId={me.viewAsRoleId} />}

        <header className="border-edge bg-bg/85 sticky top-0 z-20 flex h-14 items-center gap-2 border-b px-4 backdrop-blur-sm">
          <button
            className="interactive-muted flex size-8 cursor-pointer items-center justify-center lg:hidden"
            onClick={() => setMobileNav(true)}
            aria-label={t("nav.menu")}
          >
            <Menu className="size-4.5" />
          </button>

          <button
            onClick={() => setPaletteOpen(true)}
            className="border-edge bg-surface text-fg-subtle hover:border-edge-strong hover:text-fg-muted ml-auto flex h-8.5 w-full max-w-64 cursor-pointer items-center gap-2 rounded-md border px-3 text-[13px] shadow-1 transition-colors"
          >
            <Search className="size-3.5" />
            <span className="flex-1 text-left">{t("common.search")}</span>
            <Kbd>⌘K</Kbd>
          </button>

          <NotificationsBell />
        </header>

        <main
          className={cn(
            "mx-auto w-full flex-1 px-4 py-6 sm:px-6",
            // The workhorse apps get the whole viewport; everything else
            // keeps the comfortable reading column.
            /^\/tools\/(editor|files|convert)(\/|$)/.test(pathname) ? "max-w-none" : "max-w-6xl",
          )}
        >
          <Outlet />
          <AssistantWidget />
        </main>
      </div>

      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />
    </div>
  );
}

function SidebarLink({
  item,
  active,
  status,
}: {
  item: NavItem;
  active: boolean;
  status?: string;
}) {
  const Icon = item.icon;
  return (
    <Link
      to={item.to}
      className={cn(
        "flex items-center gap-2.5 rounded-md px-2.5 py-1.5 text-[13px] font-medium transition-colors duration-100",
        active
          ? "bg-accent-soft text-accent-soft-fg"
          : "text-fg-muted hover:bg-surface-hover hover:text-fg",
      )}
    >
      <Icon className="size-4 shrink-0" />
      <span className="min-w-0 flex-1 truncate">{item.label}</span>
      {status && status !== "connected" && (
        <span
          className={cn(
            "size-1.5 shrink-0 rounded-full",
            status === "degraded" || status === "connecting" ? "bg-warning" : "bg-danger",
          )}
        />
      )}
    </Link>
  );
}

/** Banner shown while an admin previews the app as a role. */
function ViewAsBanner({ roleId }: { roleId: string }) {
  const { t } = useTranslation();
  const { refresh } = useAuth();
  const queryClient = useQueryClient();

  const exit = useMutation({
    mutationFn: async () => unwrap(await api.PUT("/auth/view-as", { body: { roleId: null } })),
    onSuccess: async () => {
      queryClient.clear();
      await refresh();
    },
  });

  // The role name isn't in Me; show the persisted label if the roles query is
  // cached, else a generic label. Admins arrive here from the roles page.
  const cachedRoles = queryClient.getQueryData<{ id: string; name: string }[]>(["roles"]);
  const roleName = cachedRoles?.find((r) => r.id === roleId)?.name ?? roleId.slice(0, 8);

  return (
    <div className="bg-warning-soft text-warning-soft-fg flex items-center justify-center gap-3 px-4 py-1.5 text-[13px] font-medium">
      <Eye className="size-3.5" />
      {t("viewAs.banner", { role: roleName })}
      <Button size="sm" variant="secondary" loading={exit.isPending} onClick={() => exit.mutate()}>
        {t("viewAs.exit")}
      </Button>
    </div>
  );
}
