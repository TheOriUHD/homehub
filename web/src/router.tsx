import {
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
} from "@tanstack/react-router";
import { SetupPage } from "./pages/setup";
import { LoginPage } from "./pages/login";
import { InvitePage } from "./pages/invite";
import { ChangePasswordPage } from "./pages/change-password";
import { AppShell } from "./components/shell";
import { OverviewPage } from "./pages/overview";
import { HealthPage } from "./pages/health";
import { HistoryPage } from "./pages/history";
import { AlertsPage } from "./pages/alerts";
import { BackupsPage } from "./pages/backups";
import { AccountPage } from "./pages/account";
import { UsersPage } from "./pages/users";
import { UserDetailPage } from "./pages/user-detail";
import { RolesPage } from "./pages/roles";
import { AuditPage } from "./pages/audit";
import { AppSettingsPage } from "./pages/app-settings";
import { NotFoundPage } from "./pages/not-found";
import { IntegrationsPage } from "./pages/integrations";
import { PveModulePage } from "./pages/pve/module";
import { PveSummaryPage } from "./pages/pve/summary";
import { PveGuestsPage } from "./pages/pve/guests";
import { PveGuestDetailPage } from "./pages/pve/guest-detail";
import { PveBackupsPage, PveStoragePage, PveTasksPage } from "./pages/pve/misc";
import { DockerModulePage } from "./pages/docker/module";
import { DockerContainersPage } from "./pages/docker/containers";
import { DockerContainerDetailPage } from "./pages/docker/container-detail";
import { DockerImagesPage, DockerNetworksPage, DockerStacksPage, DockerVolumesPage } from "./pages/docker/misc";
import { DnsModulePage } from "./pages/dns/module";
import { DnsOverviewPage } from "./pages/dns/overview";
import { DnsQueryLogPage } from "./pages/dns/querylog";
import { DnsListsPage } from "./pages/dns/lists";
import { DnsClientsPage } from "./pages/dns/clients";
import { DnsSyncPage } from "./pages/dns/sync";

const rootRoute = createRootRoute({
  component: Outlet,
  notFoundComponent: NotFoundPage,
});

const setupRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/setup",
  component: SetupPage,
});

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  component: LoginPage,
});

const inviteRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/invite/$token",
  component: InvitePage,
});

const changePasswordRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/change-password",
  component: ChangePasswordPage,
});

// Everything below /app-shell requires an authenticated session; the shell
// enforces that and renders only permitted navigation.
const shellRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "shell",
  component: AppShell,
});

const overviewRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/",
  component: OverviewPage,
});

const healthRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/health",
  component: HealthPage,
});

const historyRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/history",
  component: HistoryPage,
});

const alertsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/alerts",
  component: AlertsPage,
});

const backupsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/backups",
  component: BackupsPage,
});

const accountRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/account",
  component: AccountPage,
});

const usersRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/admin/users",
  component: UsersPage,
});

const userDetailRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/admin/users/$userId",
  component: UserDetailPage,
});

const rolesRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/admin/roles",
  component: RolesPage,
});

const auditRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/admin/audit",
  component: AuditPage,
});

const appSettingsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/admin/settings",
  component: AppSettingsPage,
});

const integrationsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/admin/integrations",
  component: IntegrationsPage,
});

const pveModuleRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/pve/$instanceId",
  component: PveModulePage,
});

const pveSummaryRoute = createRoute({
  getParentRoute: () => pveModuleRoute,
  path: "/",
  component: PveSummaryPage,
});

const pveGuestsRoute = createRoute({
  getParentRoute: () => pveModuleRoute,
  path: "/guests",
  component: PveGuestsPage,
});

const pveGuestDetailRoute = createRoute({
  getParentRoute: () => pveModuleRoute,
  path: "/guests/$vmid",
  component: PveGuestDetailPage,
});

const pveStorageRoute = createRoute({
  getParentRoute: () => pveModuleRoute,
  path: "/storage",
  component: PveStoragePage,
});

const pveTasksRoute = createRoute({
  getParentRoute: () => pveModuleRoute,
  path: "/tasks",
  component: PveTasksPage,
});

const pveBackupsRoute = createRoute({
  getParentRoute: () => pveModuleRoute,
  path: "/backups",
  component: PveBackupsPage,
});

const dockerModuleRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/docker/$instanceId",
  component: DockerModulePage,
});

const dockerContainersRoute = createRoute({
  getParentRoute: () => dockerModuleRoute,
  path: "/",
  component: DockerContainersPage,
});

const dockerContainerDetailRoute = createRoute({
  getParentRoute: () => dockerModuleRoute,
  path: "/containers/$name",
  component: DockerContainerDetailPage,
});

const dockerStacksRoute = createRoute({
  getParentRoute: () => dockerModuleRoute,
  path: "/stacks",
  component: DockerStacksPage,
});

const dockerImagesRoute = createRoute({
  getParentRoute: () => dockerModuleRoute,
  path: "/images",
  component: DockerImagesPage,
});

const dockerVolumesRoute = createRoute({
  getParentRoute: () => dockerModuleRoute,
  path: "/volumes",
  component: DockerVolumesPage,
});

const dockerNetworksRoute = createRoute({
  getParentRoute: () => dockerModuleRoute,
  path: "/networks",
  component: DockerNetworksPage,
});

const dnsModuleRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/dns",
  component: DnsModulePage,
});

const dnsOverviewRoute = createRoute({
  getParentRoute: () => dnsModuleRoute,
  path: "/",
  component: DnsOverviewPage,
});

const dnsQueriesRoute = createRoute({
  getParentRoute: () => dnsModuleRoute,
  path: "/queries",
  component: DnsQueryLogPage,
});

const dnsListsRoute = createRoute({
  getParentRoute: () => dnsModuleRoute,
  path: "/lists",
  component: DnsListsPage,
});

const dnsClientsRoute = createRoute({
  getParentRoute: () => dnsModuleRoute,
  path: "/clients",
  component: DnsClientsPage,
});

const dnsSyncRoute = createRoute({
  getParentRoute: () => dnsModuleRoute,
  path: "/sync",
  component: DnsSyncPage,
});

const routeTree = rootRoute.addChildren([
  setupRoute,
  loginRoute,
  inviteRoute,
  changePasswordRoute,
  shellRoute.addChildren([
    overviewRoute,
    healthRoute,
    historyRoute,
    alertsRoute,
    backupsRoute,
    accountRoute,
    usersRoute,
    userDetailRoute,
    rolesRoute,
    auditRoute,
    appSettingsRoute,
    integrationsRoute,
    pveModuleRoute.addChildren([
      pveSummaryRoute,
      pveGuestsRoute,
      pveGuestDetailRoute,
      pveStorageRoute,
      pveTasksRoute,
      pveBackupsRoute,
    ]),
    dockerModuleRoute.addChildren([
      dockerContainersRoute,
      dockerContainerDetailRoute,
      dockerStacksRoute,
      dockerImagesRoute,
      dockerVolumesRoute,
      dockerNetworksRoute,
    ]),
    dnsModuleRoute.addChildren([
      dnsOverviewRoute,
      dnsQueriesRoute,
      dnsListsRoute,
      dnsClientsRoute,
      dnsSyncRoute,
    ]),
  ]),
]);

export const router = createRouter({
  routeTree,
  defaultPreload: "intent",
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
