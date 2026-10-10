import { useEffect, useState } from "react";
import { useLocation, useNavigate } from "react-router";
import { Menu } from "lucide-react";
import { Sidebar } from "./Sidebar";
import { TopBarControls } from "./TopBarControls";
import { TOP_BAR_GUTTER, TOP_BAR_HEIGHT } from "./topBar";
import { getToken } from "@/lib/auth";
import { useT } from "@/lib/i18n";
import { useHydrated } from "@/lib/useHydrated";

export function AppLayout({ children }: { children: React.ReactNode }) {
  const navigate = useNavigate();
  const pathname = useLocation().pathname;
  const t = useT();
  // The token lives in localStorage, which does not exist while the server renders. Reading it directly
  // would make the server's output (the spinner) disagree with the client's first render (the shell), and
  // React would throw the tree away as a hydration mismatch. Gating on `hydrated` keeps the two in step.
  const hydrated = useHydrated();
  const [sidebarOpen, setSidebarOpen] = useState(false);

  // The stored profile is not read here any more: the avatar menu that printed the name and the email is
  // gone, and the settings page reads its own copy. A valid token is the whole of what "signed in" means.
  const ready = hydrated && !!getToken();

  useEffect(() => {
    if (hydrated && !ready) {
      navigate("/login");
    }
  }, [hydrated, ready, navigate]);

  // Close the mobile sidebar when the route changes (React's documented
  // "adjust state when a prop changes" pattern).
  const [prevPath, setPrevPath] = useState(pathname);
  if (prevPath !== pathname) {
    setPrevPath(pathname);
    setSidebarOpen(false);
  }

  if (!ready) {
    return (
      <div className="flex h-screen items-center justify-center bg-subtle">
        <div className="flex flex-col items-center gap-3">
          <div className="size-8 animate-spin rounded-full border-2 border-indigo-600 border-t-transparent" />
          <p className="text-sm text-faint">{t("加载中...")}</p>
        </div>
      </div>
    );
  }

  const pageTitle = (() => {
    if (pathname === "/dashboard") return t("链接");
    if (pathname.startsWith("/dashboard/links/new")) return t("创建链接");
    if (pathname.match(/^\/dashboard\/links\/\d+$/)) return t("链接详情");
    if (pathname === "/dashboard/analytics") return t("分析");
    if (pathname === "/dashboard/domains") return t("域名");
    if (pathname === "/dashboard/ai") return t("AI 助手");
    if (pathname === "/dashboard/events") return t("事件");
    if (pathname === "/dashboard/customers") return t("客户");
    if (pathname === "/dashboard/folders") return t("文件夹");
    if (pathname === "/dashboard/tags") return t("标签");
    if (pathname === "/dashboard/utm") return t("UTM 模板");
    if (pathname === "/dashboard/settings") return t("设置");
    return "";
  })();

  return (
    <div className="flex h-screen overflow-hidden bg-subtle">
      {/* Desktop Sidebar - always visible on lg+ */}
      <div className="hidden lg:block">
        <Sidebar />
      </div>

      {/* Mobile Sidebar Overlay */}
      {sidebarOpen && (
        <div className="fixed inset-0 z-40 lg:hidden">
          {/* Backdrop */}
          <div
            className="absolute inset-0 bg-black/40 backdrop-blur-sm"
            onClick={() => setSidebarOpen(false)}
          />
          {/* Sidebar */}
          <div className="absolute left-0 top-0 h-full w-[240px] z-50 shadow-xl">
            <Sidebar onCloseMobile={() => setSidebarOpen(false)} />
          </div>
        </div>
      )}

      {/* Right: Top bar + Content */}
      <div className="flex flex-1 flex-col min-w-0">
        {/* Top Bar */}
        <header
          className={`flex ${TOP_BAR_HEIGHT} items-center justify-between border-b border-line bg-canvas ${TOP_BAR_GUTTER} shrink-0`}
        >
          <div className="flex items-center gap-3">
            {/* Mobile menu button */}
            <button
              onClick={() => setSidebarOpen(true)}
              className="lg:hidden p-1.5 -ml-1 rounded-lg text-muted hover:bg-muted-surface transition"
            >
              <Menu className="size-5" />
            </button>
            <h2 className="text-sm font-medium text-body">{pageTitle}</h2>
          </div>

          {/* Right: theme + GitHub + language, in the order and at the position every other shell uses.
              The avatar menu that used to sit here went with the profile fields it displayed, and signing
              out now lives on the settings page - the one page that is about the account. */}
          <TopBarControls />
        </header>

        {/* Content */}
        <main className="flex-1 overflow-y-auto">
          {/* 所有页面统一布局：居中 + 边距（AI 页也遵循，不再贴满） */}
          <div className="mx-auto max-w-5xl px-4 sm:px-6 py-4 sm:py-6">
            {children}
          </div>
        </main>
      </div>
    </div>
  );
}
