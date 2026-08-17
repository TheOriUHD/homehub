import { Command } from "cmdk";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  Activity,
  BellRing,
  ChartSpline,
  DatabaseBackup,
  FileClock,
  Home,
  KeyRound,
  LogOut,
  Moon,
  Settings,
  Shield,
  Sun,
  UserPlus,
  Users,
} from "lucide-react";
import { useAuth } from "@/lib/auth";
import { RES } from "@/lib/perms";
import { useTheme } from "@/lib/theme";
import { useBackupsVisible } from "@/pages/backups/shared";
import { Dialog } from "@/ui/dialog";
import * as DialogPrimitive from "@radix-ui/react-dialog";

/**
 * Global command palette (⌘K). Entries are filtered by the same grant
 * evaluation as navigation — a denied page never appears here.
 */
export function CommandPalette({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { can, signOut } = useAuth();
  const [theme, setTheme] = useTheme();

  const go = (to: string) => {
    onOpenChange(false);
    void navigate({ to });
  };

  // Same probe-driven visibility as the sidebar entry.
  const backupsVisible = useBackupsVisible();

  const pages = [
    { label: t("nav.overview"), icon: Home, to: "/" },
    { label: t("nav.health"), icon: Activity, to: "/health", requires: RES.health },
    { label: t("nav.history"), icon: ChartSpline, to: "/history" },
    { label: t("nav.alerts"), icon: BellRing, to: "/alerts" },
    ...(backupsVisible ? [{ label: t("nav.backups"), icon: DatabaseBackup, to: "/backups" }] : []),
    { label: t("nav.users"), icon: Users, to: "/admin/users", requires: RES.users },
    { label: t("nav.roles"), icon: Shield, to: "/admin/roles", requires: RES.roles },
    { label: t("nav.audit"), icon: FileClock, to: "/admin/audit", requires: RES.audit },
    { label: t("nav.settings"), icon: Settings, to: "/admin/settings", requires: RES.settings },
    { label: t("nav.account"), icon: KeyRound, to: "/account" },
  ].filter((p) => !p.requires || can(p.requires));

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-40 bg-black/45 backdrop-blur-[2px] animate-[hh-fade-in_150ms_var(--ease-out-quick)]" />
        <DialogPrimitive.Content className="fixed top-[18vh] left-1/2 z-50 w-[calc(100vw-2rem)] max-w-lg -translate-x-1/2 focus:outline-none">
          <DialogPrimitive.Title className="sr-only">{t("palette.hint")}</DialogPrimitive.Title>
          <Command
            className="bg-overlay border-edge overflow-hidden rounded-xl border shadow-3 animate-[hh-pop-in_160ms_var(--ease-out-quick)]"
            loop
          >
            <Command.Input
              autoFocus
              placeholder={t("palette.placeholder")}
              className="text-fg placeholder:text-fg-subtle border-edge h-12 w-full border-b bg-transparent px-4 text-sm focus:outline-none"
            />
            <Command.List className="max-h-80 overflow-y-auto p-2">
              <Command.Empty className="text-fg-subtle px-3 py-8 text-center text-sm">
                {t("palette.empty", { query: "" })}
              </Command.Empty>

              <Command.Group
                heading={t("palette.sectionPages")}
                className="text-fg-subtle [&_[cmdk-group-heading]]:px-2.5 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:font-medium"
              >
                {pages.map((p) => (
                  <PaletteItem key={p.to} icon={<p.icon className="size-4" />} onSelect={() => go(p.to)}>
                    {p.label}
                  </PaletteItem>
                ))}
              </Command.Group>

              <Command.Group
                heading={t("palette.sectionActions")}
                className="text-fg-subtle [&_[cmdk-group-heading]]:px-2.5 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:font-medium"
              >
                {can(RES.users, "configure") && (
                  <PaletteItem icon={<UserPlus className="size-4" />} onSelect={() => go("/admin/users")}>
                    {t("users.add")}
                  </PaletteItem>
                )}
                <PaletteItem
                  icon={theme === "dark" ? <Sun className="size-4" /> : <Moon className="size-4" />}
                  onSelect={() => {
                    setTheme(theme === "dark" ? "light" : "dark");
                    onOpenChange(false);
                  }}
                >
                  {theme === "dark" ? t("theme.light") : t("theme.dark")}
                </PaletteItem>
                <PaletteItem
                  icon={<LogOut className="size-4" />}
                  onSelect={() => {
                    onOpenChange(false);
                    void signOut();
                  }}
                >
                  {t("nav.signOut")}
                </PaletteItem>
              </Command.Group>
            </Command.List>
          </Command>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </Dialog>
  );
}

function PaletteItem({
  icon,
  children,
  onSelect,
}: {
  icon: React.ReactNode;
  children: React.ReactNode;
  onSelect: () => void;
}) {
  return (
    <Command.Item
      onSelect={onSelect}
      className="text-fg data-[selected=true]:bg-surface-hover flex cursor-pointer items-center gap-2.5 rounded-md px-2.5 py-2 text-sm select-none"
    >
      <span className="text-fg-subtle">{icon}</span>
      {children}
    </Command.Item>
  );
}
