import { createBrowserRouter, Navigate, Outlet } from "react-router";

import { AppLayout } from "@/components/AppLayout";
import Ai from "@/pages/dashboard/Ai";
import Analytics from "@/pages/dashboard/Analytics";
import Customers from "@/pages/dashboard/Customers";
import Domains from "@/pages/dashboard/Domains";
import Events from "@/pages/dashboard/Events";
import Folders from "@/pages/dashboard/Folders";
import LinkDetail from "@/pages/dashboard/LinkDetail";
import LinkNew from "@/pages/dashboard/LinkNew";
import Links from "@/pages/dashboard/Links";
import Settings from "@/pages/dashboard/Settings";
import Tags from "@/pages/dashboard/Tags";
import Utm from "@/pages/dashboard/Utm";
import Privacy from "@/pages/Privacy";
import Redirect from "@/pages/Redirect";

/**
 * The route table.
 *
 * It says in one place what a file system used to say with file names: a single-page app has no directory
 * to derive routes from at runtime, so the mapping is written out - which also makes it the only file to
 * read to answer "what URLs does this site serve".
 */
export const router = createBrowserRouter([
  // There is no landing page and no sign-in page: `/` goes to the dashboard, whose shell opens a guest
  // session and renders the app. The two URLs below are not routes any more, they are redirects - they were
  // shared, bookmarked and linked (the privacy policy's own "back" pointed at one of them), and a link that
  // dead-ends is a worse answer than one that lands where the visitor was going.
  { path: "/", element: <Navigate to="/dashboard" replace /> },
  { path: "/login", element: <Navigate to="/dashboard" replace /> },
  { path: "/register", element: <Navigate to="/dashboard" replace /> },
  { path: "/privacy", element: <Privacy /> },
  { path: "/privacy", element: <Privacy /> },
  // The interstitial the backend redirects to when a short link needs a password or a deep-link hop.
  { path: "/r/:code", element: <Redirect /> },
  {
    path: "/dashboard",
    // The shell owns the sidebar, the top bar and the sign-in guard; every page below renders inside it.
    element: (
      <AppLayout>
        <Outlet />
      </AppLayout>
    ),
    children: [
      { index: true, element: <Links /> },
      { path: "links/new", element: <LinkNew /> },
      { path: "links/:id", element: <LinkDetail /> },
      { path: "analytics", element: <Analytics /> },
      { path: "ai", element: <Ai /> },
      { path: "customers", element: <Customers /> },
      { path: "domains", element: <Domains /> },
      { path: "events", element: <Events /> },
      { path: "folders", element: <Folders /> },
      { path: "settings", element: <Settings /> },
      { path: "tags", element: <Tags /> },
      { path: "utm", element: <Utm /> },
    ],
  },
  // There is no 404 screen either, and a stale bookmark should not be a dead end: an unknown path is
  // treated exactly like `/`, so it lands wherever the visitor belongs.
  { path: "*", element: <Navigate to="/dashboard" replace /> },
]);
