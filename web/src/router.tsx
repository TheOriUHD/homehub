import {
  createRootRoute,
  createRoute,
  createRouter,
  Navigate,
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
import { SystemsPage } from "./pages/systems";
import { SystemTypeLanding } from "./pages/systems/type-landing";
import { PveModulePage } from "./pages/pve/module";
import { PveSummaryPage } from "./pages/pve/summary";
import { PveGuestsPage } from "./pages/pve/guests";
import { PveGuestDetailPage } from "./pages/pve/guest-detail";
import { PveBackupsPage, PveStoragePage, PveTasksPage } from "./pages/pve/misc";
import { DockerModulePage } from "./pages/docker/module";
import { DockerAllContainersPage, DockerHostsPage, DockerHubPage } from "./pages/docker/unified";
import { DockerContainersPage } from "./pages/docker/containers";
import { DockerContainerDetailPage } from "./pages/docker/container-detail";
import { DockerImagesPage, DockerNetworksPage, DockerStacksPage, DockerVolumesPage } from "./pages/docker/misc";
import { HaModulePage } from "./pages/ha/module";
import { HaBoardPage } from "./pages/ha/board";
import { HaEntitiesPage } from "./pages/ha/entities";
import { HaAutomationsPage } from "./pages/ha/automations";
import { McModulePage } from "./pages/mc/module";
import { McLandingPage } from "./pages/mc/landing";
import { McConsolePage } from "./pages/mc/console";
import { McPlayersPage } from "./pages/mc/players";
import { McSetupPage } from "./pages/mc/setup";
import { McPerformancePage } from "./pages/mc/performance";
import { DnsModulePage } from "./pages/dns/module";
import { DnsOverviewPage } from "./pages/dns/overview";
import { DnsQueryLogPage } from "./pages/dns/querylog";
import { DnsListsPage } from "./pages/dns/lists";
import { DnsClientsPage } from "./pages/dns/clients";
import { DnsSyncPage } from "./pages/dns/sync";
import { DbLandingPage } from "./pages/db/landing";
import { DbModulePage } from "./pages/db/module";
import { DbDatabasesPage } from "./pages/db/databases";
import { DbBrowsePage } from "./pages/db/browse";
import { DbConsolePage } from "./pages/db/console";
import { DbSessionsPage } from "./pages/db/sessions";
import { DbPerformancePage } from "./pages/db/performance";
import { AmpModulePage } from "./pages/amp/module";
import { TsModulePage } from "./pages/ts/module";
import { CaddyModulePage } from "./pages/caddy/module";
import { CaddySitesPage } from "./pages/caddy/sites";
import { CaddyServedPage } from "./pages/caddy/served";
import { TsDevicesPage } from "./pages/ts/devices";
import { TsKeysPage } from "./pages/ts/keys";
import { TsDnsPage } from "./pages/ts/dns";
import { AmpOverviewPage } from "./pages/amp/overview";
import { AmpInstancePage } from "./pages/amp/instance";
import { TruenasModulePage } from "./pages/truenas/module";
import { TruenasOverviewPage } from "./pages/truenas/overview";
import { TruenasDatasetsPage } from "./pages/truenas/datasets";
import { TruenasSharesPage } from "./pages/truenas/shares";
import { TruenasSnapshotsPage } from "./pages/truenas/snapshots";
import { TruenasPerformancePage } from "./pages/truenas/performance";

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

const systemsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/systems",
  component: SystemsPage,
});

// The old admin page moved to /systems — keep saved links working.
const integrationsRedirectRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/admin/integrations",
  component: () => <Navigate to="/systems" replace />,
});

// Types without a unified module get a minimal per-type landing.
const pveLandingRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/pve",
  component: () => <SystemTypeLanding type="proxmox" />,
});

const haLandingRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/ha",
  component: () => <SystemTypeLanding type="homeassistant" />,
});

const mcLandingRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/mc",
  component: McLandingPage,
});

const truenasLandingRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/truenas",
  component: () => <SystemTypeLanding type="truenas" />,
});

const ampLandingRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/amp",
  component: () => <SystemTypeLanding type="amp" />,
});

const tsLandingRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/ts",
  component: () => <SystemTypeLanding type="tailscale" />,
});

const caddyLandingRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/caddy",
  component: () => <SystemTypeLanding type="caddy" />,
});

const caddyModuleRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/caddy/$instanceId",
  component: CaddyModulePage,
});

const caddySitesRoute = createRoute({
  getParentRoute: () => caddyModuleRoute,
  path: "/",
  component: CaddySitesPage,
});

const caddyServedRoute = createRoute({
  getParentRoute: () => caddyModuleRoute,
  path: "/served",
  component: CaddyServedPage,
});

const tsModuleRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/ts/$instanceId",
  component: TsModulePage,
});

const tsDevicesRoute = createRoute({
  getParentRoute: () => tsModuleRoute,
  path: "/",
  component: TsDevicesPage,
});

const tsKeysRoute = createRoute({
  getParentRoute: () => tsModuleRoute,
  path: "/keys",
  component: TsKeysPage,
});

const tsDnsRoute = createRoute({
  getParentRoute: () => tsModuleRoute,
  path: "/dns",
  component: TsDnsPage,
});

const ampModuleRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/amp/$instanceId",
  component: AmpModulePage,
});

const ampOverviewRoute = createRoute({
  getParentRoute: () => ampModuleRoute,
  path: "/",
  component: AmpOverviewPage,
});

const ampInstanceRoute = createRoute({
  getParentRoute: () => ampModuleRoute,
  path: "/i/$serverId",
  component: AmpInstancePage,
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

// The unified Docker module: all hosts' containers in one place. Static
// segments outrank $instanceId, so /docker and /docker/hosts resolve here.
const dockerHubRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/docker",
  component: DockerHubPage,
});

const dockerAllContainersRoute = createRoute({
  getParentRoute: () => dockerHubRoute,
  path: "/",
  component: DockerAllContainersPage,
});

const dockerHostsRoute = createRoute({
  getParentRoute: () => dockerHubRoute,
  path: "/hosts",
  component: DockerHostsPage,
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
  // ?tab= lets the unified containers table deep-link logs/shell.
  validateSearch: (search: Record<string, unknown>): { tab?: "inspect" | "logs" | "shell" } => {
    const tab = search.tab;
    return tab === "inspect" || tab === "logs" || tab === "shell" ? { tab } : {};
  },
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

const haModuleRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/ha/$instanceId",
  component: HaModulePage,
});

// The index is the caller's personal board; the previous browse-everything
// view lives at /entities. The automations deep link is unchanged.
const haBoardRoute = createRoute({
  getParentRoute: () => haModuleRoute,
  path: "/",
  component: HaBoardPage,
});

const haEntitiesRoute = createRoute({
  getParentRoute: () => haModuleRoute,
  path: "/entities",
  component: HaEntitiesPage,
});

const haAutomationsRoute = createRoute({
  getParentRoute: () => haModuleRoute,
  path: "/automations",
  component: HaAutomationsPage,
});

const mcModuleRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/mc/$instanceId",
  component: McModulePage,
});

const mcConsoleRoute = createRoute({
  getParentRoute: () => mcModuleRoute,
  path: "/",
  component: McConsolePage,
});

const mcPlayersRoute = createRoute({
  getParentRoute: () => mcModuleRoute,
  path: "/players",
  component: McPlayersPage,
});

const mcSetupRoute = createRoute({
  getParentRoute: () => mcModuleRoute,
  path: "/setup",
  component: McSetupPage,
});

const mcPerformanceRoute = createRoute({
  getParentRoute: () => mcModuleRoute,
  path: "/performance",
  component: McPerformancePage,
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

// The unified Databases module: MySQL/MariaDB and MongoDB instances behind
// one entry. The static /db landing outranks $instanceId.
const dbLandingRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/db",
  component: DbLandingPage,
});

const dbModuleRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/db/$instanceId",
  component: DbModulePage,
});

const dbDatabasesRoute = createRoute({
  getParentRoute: () => dbModuleRoute,
  path: "/",
  component: DbDatabasesPage,
});

// The Adminer-style data browser/editor for one table or collection.
const dbBrowseRoute = createRoute({
  getParentRoute: () => dbModuleRoute,
  path: "/browse/$database/$object",
  component: DbBrowsePage,
});

const dbConsoleRoute = createRoute({
  getParentRoute: () => dbModuleRoute,
  path: "/console",
  component: DbConsolePage,
});

const dbSessionsRoute = createRoute({
  getParentRoute: () => dbModuleRoute,
  path: "/sessions",
  component: DbSessionsPage,
});

const dbPerformanceRoute = createRoute({
  getParentRoute: () => dbModuleRoute,
  path: "/performance",
  component: DbPerformancePage,
});

// The unified TrueNAS module: one storage instance with tabbed views. The
// static /truenas landing outranks $instanceId.
const truenasModuleRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/truenas/$instanceId",
  component: TruenasModulePage,
});

const truenasOverviewRoute = createRoute({
  getParentRoute: () => truenasModuleRoute,
  path: "/",
  component: TruenasOverviewPage,
});

const truenasDatasetsRoute = createRoute({
  getParentRoute: () => truenasModuleRoute,
  path: "/datasets",
  component: TruenasDatasetsPage,
});

const truenasSharesRoute = createRoute({
  getParentRoute: () => truenasModuleRoute,
  path: "/shares",
  component: TruenasSharesPage,
});

const truenasSnapshotsRoute = createRoute({
  getParentRoute: () => truenasModuleRoute,
  path: "/snapshots",
  component: TruenasSnapshotsPage,
});

const truenasPerformanceRoute = createRoute({
  getParentRoute: () => truenasModuleRoute,
  path: "/performance",
  component: TruenasPerformancePage,
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
    systemsRoute,
    integrationsRedirectRoute,
    pveLandingRoute,
    haLandingRoute,
    mcLandingRoute,
    ampLandingRoute,
    tsLandingRoute,
    caddyLandingRoute,
    truenasLandingRoute,
    dockerHubRoute.addChildren([dockerAllContainersRoute, dockerHostsRoute]),
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
    haModuleRoute.addChildren([haBoardRoute, haEntitiesRoute, haAutomationsRoute]),
    mcModuleRoute.addChildren([
      mcConsoleRoute,
      mcPlayersRoute,
      mcSetupRoute,
      mcPerformanceRoute,
    ]),
    dnsModuleRoute.addChildren([
      dnsOverviewRoute,
      dnsQueriesRoute,
      dnsListsRoute,
      dnsClientsRoute,
      dnsSyncRoute,
    ]),
    dbLandingRoute,
    dbModuleRoute.addChildren([
      dbDatabasesRoute,
      dbBrowseRoute,
      dbConsoleRoute,
      dbSessionsRoute,
      dbPerformanceRoute,
    ]),
    truenasModuleRoute.addChildren([
      truenasOverviewRoute,
      truenasDatasetsRoute,
      truenasSharesRoute,
      truenasSnapshotsRoute,
      truenasPerformanceRoute,
    ]),
    ampModuleRoute.addChildren([ampOverviewRoute, ampInstanceRoute]),
    tsModuleRoute.addChildren([tsDevicesRoute, tsKeysRoute, tsDnsRoute]),
    caddyModuleRoute.addChildren([caddySitesRoute, caddyServedRoute]),
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
