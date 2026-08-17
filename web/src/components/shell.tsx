import { useEffect, useState } from "react";
import { Link, Navigate, Outlet, useRouterState } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import {
  Activity,
  Container,
  Eye,
  FileClock,
  Home,
  KeyRound,
  LogOut,
  Menu,
  Moon,
  Plug,
  Search,
  Server,
  Settings,
  Shield,
  ShieldCheck,
  Sun,
  Users,
  X,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { cn } from "@/lib/utils";
import { useAuth } from "@/lib/auth";
import { RES } from "@/lib/perms";
import { api, unwrap } from "@/lib/api";
import { useTheme } from "@/lib/theme";
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
    { to: "/health", label: t("nav.health"), icon: Activity, requires: RES.health },
  ];
  const adminNav: NavItem[] = [
    { to: "/admin/users", label: t("nav.users"), icon: Users, requires: RES.users },
    { to: "/admin/roles", label: t("nav.roles"), icon: Shield, requires: RES.roles },
    { to: "/admin/integrations", label: t("integrations.title"), icon: Plug, requires: "system/integrations" },
    { to: "/admin/audit", label: t("nav.audit"), icon: FileClock, requires: RES.audit },
    { to: "/admin/settings", label: t("nav.settings"), icon: Settings, requires: RES.settings },
  ];

  const visible = (items: NavItem[]) => items.filter((i) => !i.requires || can(i.requires));
  const adminVisible = visible(adminNav);

  // Connected systems the user may see — the server filters by permission.
  const integrations = useQuery({
    queryKey: ["integrations"],
    queryFn: async () => unwrap(await api.GET("/integrations")),
    staleTime: 30_000,
  });
  const systems = (integrations.data ?? []).filter(
    (row) => row.enabled && (row.type === "proxmox" || row.type === "docker"),
  );
  // DNS is one module spanning all Pi-hole/AdGuard instances — a single nav
  // entry whose dot aggregates the worst instance status.
  const dnsInstances = (integrations.data ?? []).filter(
    (row) => row.enabled && (row.type === "pihole" || row.type === "adguard"),
  );
  const dnsStatus = dnsInstances.some((row) => row.status === "unreachable")
    ? "unreachable"
    : dnsInstances.some((row) => row.status === "degraded" || row.status === "connecting")
      ? "degraded"
      : "connected";

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
        {(systems.length > 0 || dnsInstances.length > 0) && (
          <div>
            <p className="text-fg-subtle mb-1.5 px-2.5 text-[11px] font-semibold tracking-wider uppercase">
              {t("nav.systems")}
            </p>
            <div className="space-y-0.5">
              {systems.map((system) => {
                const base = system.type === "docker" ? `/docker/${system.id}` : `/pve/${system.id}`;
                return (
                  <SidebarLink
                    key={system.id}
                    item={{ to: base, label: system.name, icon: system.type === "docker" ? Container : Server }}
                    active={pathname.startsWith(base)}
                    status={system.status}
                  />
                );
              })}
              {dnsInstances.length > 0 && (
                <SidebarLink
                  item={{ to: "/dns", label: t("dns.title"), icon: ShieldCheck }}
                  active={pathname.startsWith("/dns")}
                  status={dnsStatus}
                />
              )}
            </div>
          </div>
        )}
        {adminVisible.length > 0 && (
          <div>
            <p className="text-fg-subtle mb-1.5 px-2.5 text-[11px] font-semibold tracking-wider uppercase">
              {t("nav.administration")}
            </p>
            <div className="space-y-0.5">
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

        <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-6 sm:px-6">
          <Outlet />
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
