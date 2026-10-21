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
import Login from "@/pages/Login";
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
  // There is no landing page. It was one heading, one sentence and one button, and the only thing it said
  // was the product's name, which the tab already says; a visitor who matters is on their way to sign in.
  // `/` therefore means "wherever you belong": the dashboard guard sends anyone without a session to
  // /login, and a visitor who has one lands on their links instead of being asked to sign in again.
  { path: "/", element: <Navigate to="/dashboard" replace /> },
  { path: "/login", element: <Login /> },
  // Sign-up is a mode of the sign-in page rather than a page of its own: the two forms share the phone
  // field, the graphical challenge and the code, and only the password differs. The URL exists because it
  // was shared while a separate page was here, so it opens that mode instead of dead-ending.
  { path: "/register", element: <Login /> },
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
