import { createBrowserRouter, Outlet, type RouteObject } from "react-router";
import { RequireAdmin, RequireAuth } from "../features/auth/RequireAuth";
import { AppLayout } from "../layout/AppLayout";
import { navGroups, type NavItem } from "../layout/nav";
import ComingSoon from "../features/placeholder/ComingSoon";
import { NotFoundPage, RouteError } from "./errors";

const placeholder = (item: NavItem): RouteObject => {
  const element = <ComingSoon item={item} />;
  return {
    path: item.to.replace(/^\//, ""),
    element: item.adminOnly ? <RequireAdmin>{element}</RequireAdmin> : element,
  };
};

const placeholders = navGroups.flatMap((g) => g.items).filter((i) => !i.ready).map(placeholder);

export const routes: RouteObject[] = [
  {
    path: "/login",
    errorElement: <RouteError />,
    lazy: { Component: async () => (await import("../features/auth/LoginPage")).default },
  },
  {
    path: "/",
    errorElement: <RouteError />,
    element: (
      <RequireAuth>
        <AppLayout />
      </RequireAuth>
    ),
    children: [
      { index: true, lazy: { Component: async () => (await import("../features/home/HomePage")).default } },
      { path: "approvals", lazy: { Component: async () => (await import("../features/approvals/ApprovalsPage")).default } },
      { path: "databases", lazy: { Component: async () => (await import("../features/databases/DatabasesPage")).default } },
      { path: "databases/:dbId", lazy: { Component: async () => (await import("../features/databases/DatabaseDetailPage")).default } },
      { path: "databases/:dbId/tables/:schema/:table", lazy: { Component: async () => (await import("../features/databases/TablePage")).default } },
      { path: "databases/:dbId/migrations", lazy: { Component: async () => (await import("../features/migrations/MigrationsPage")).default } },
      { path: "databases/:dbId/migrations/new", lazy: { Component: async () => (await import("../features/migrations/NewMigrationPage")).default } },
      { path: "migrations/:id", lazy: { Component: async () => (await import("../features/migrations/MigrationDetailPage")).default } },
      { path: "jobs", lazy: { Component: async () => (await import("../features/jobs/JobsPage")).default } },
      { path: "jobs/:jobId", lazy: { Component: async () => (await import("../features/jobs/JobDetailPage")).default } },
      {
        element: (
          <RequireAdmin>
            <Outlet />
          </RequireAdmin>
        ),
        children: [
          { path: "admin/databases", lazy: { Component: async () => (await import("../features/connections/ConnectionsPage")).default } },
          { path: "admin/users", lazy: { Component: async () => (await import("../features/admin/UsersPage")).default } },
          { path: "admin/groups", lazy: { Component: async () => (await import("../features/admin/GroupsPage")).default } },
          { path: "admin/groups/:groupId", lazy: { Component: async () => (await import("../features/admin/GroupDetailPage")).default } },
        ],
      },
      ...placeholders,
      { path: "*", element: <NotFoundPage /> },
    ],
  },
];

export const router = createBrowserRouter(routes);