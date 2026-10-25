import { createBrowserRouter, Navigate, Outlet } from "react-router";

import { AppLayout } from "@/components/AppLayout";
import Analytics from "@/pages/dashboard/Analytics";
import Api from "@/pages/dashboard/Api";
import Customers from "@/pages/dashboard/Customers";
import Domains from "@/pages/dashboard/Domains";
import Events from "@/pages/dashboard/Events";
import Folders from "@/pages/dashboard/Folders";
import LinkDetail from "@/pages/dashboard/LinkDetail";
import LinkNew from "@/pages/dashboard/LinkNew";
import Links from "@/pages/dashboard/Links";
import Moderation from "@/pages/dashboard/Moderation";
import Tags from "@/pages/dashboard/Tags";
import Targets from "@/pages/dashboard/Targets";
import Utm from "@/pages/dashboard/Utm";
import Privacy from "@/pages/Privacy";
import Redirect from "@/pages/Redirect";
import Report from "@/pages/Report";

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
  // The complaint form a stopped link offers. Public, because the visitor who needs it has no account.
  { path: "/report", element: <Report /> },
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
      // The AI assistant is gone and its URL sat in the sidebar of every deployment, so a bookmark to it
      // lands on the dashboard the way the other retired URLs do.
      { path: "ai", element: <Navigate to="/dashboard" replace /> },
      // The API page carries the documentation and the tokens that reach it, which is where the token
      // management used to live: the user page it was on no longer exists, because there are no users.
      { path: "api", element: <Api /> },
      { path: "customers", element: <Customers /> },
      { path: "domains", element: <Domains /> },
      { path: "events", element: <Events /> },
      { path: "folders", element: <Folders /> },
      // The user page was deleted with the sign-in it belonged to. The URL was shared and may be bookmarked,
      // and the two things on it - tokens and the theme - live on the API page and in the top bar now.
      { path: "settings", element: <Navigate to="/dashboard/api" replace /> },
      // The operator console. It lives behind the same shell as everything else and the API refuses it to
      // anybody who cannot present ADMIN_SECRET; the sidebar only shows the entry when that check passes.
      { path: "moderation", element: <Moderation /> },
      { path: "tags", element: <Tags /> },
      // 分流: the pool of destinations behind each link, which used to be edited on the link page itself.
      { path: "targets", element: <Targets /> },
      { path: "utm", element: <Utm /> },
    ],
  },
  // There is no 404 screen either, and a stale bookmark should not be a dead end: an unknown path is
  // treated exactly like `/`, so it lands wherever the visitor belongs.
  { path: "*", element: <Navigate to="/dashboard" replace /> },
]);
