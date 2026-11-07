import { createBrowserRouter, Outlet, type RouteObject } from "react-router";
import { RequireAdmin, RequireAuth } from "../auth/RequireAuth";
import { AppLayout } from "../layout/AppLayout";
import { migrationsNav, navGroups, type NavItem } from "../layout/nav";
import ComingSoon from "../features/placeholder/ComingSoon";
import { NotFoundPage, RouteError } from "./errors";

const placeholder = (item: NavItem): RouteObject => {
  const element = <ComingSoon item={item} />;
  return {
    path: item.to.replace(/^\//, ""),
    element: item.adminOnly ? <RequireAdmin>{element}</RequireAdmin> : element,
  };
};

const placeholders = [...navGroups.flatMap((g) => g.items), migrationsNav].filter((i) => !i.ready).map(placeholder);

export const routes: RouteObject[] = [
  {
    path: "/login",
    errorElement: <RouteError />,
    lazy: { Component: async () => (await import("../auth/LoginPage")).default },
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
      {
        element: (
          <RequireAdmin>
            <Outlet />
          </RequireAdmin>
        ),
        children: [
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