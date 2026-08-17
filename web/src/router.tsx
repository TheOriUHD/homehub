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
import { AccountPage } from "./pages/account";
import { UsersPage } from "./pages/users";
import { UserDetailPage } from "./pages/user-detail";
import { RolesPage } from "./pages/roles";
import { AuditPage } from "./pages/audit";
import { AppSettingsPage } from "./pages/app-settings";
import { NotFoundPage } from "./pages/not-found";

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

const routeTree = rootRoute.addChildren([
  setupRoute,
  loginRoute,
  inviteRoute,
  changePasswordRoute,
  shellRoute.addChildren([
    overviewRoute,
    healthRoute,
    accountRoute,
    usersRoute,
    userDetailRoute,
    rolesRoute,
    auditRoute,
    appSettingsRoute,
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
