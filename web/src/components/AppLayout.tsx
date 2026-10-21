import { useEffect, useState } from "react";
import { useLocation } from "react-router";
import { Menu } from "lucide-react";
import { Sidebar } from "./Sidebar";
import { TopBarControls } from "./TopBarControls";
import { TOP_BAR_GUTTER, TOP_BAR_HEIGHT } from "./topBar";
import { buttonClass } from "./ui/Button";
import { useT } from "@/lib/i18n";
import { ensureSession } from "@/lib/session";
import { useHydrated } from "@/lib/useHydrated";

/**
 * The shell: sidebar, top bar, and the session every page inside it runs on.
 *
 * This used to guard the door - no token, no dashboard, off to /login you go. There is no door any more:
 * the shell opens a guest session on the first load (see `lib/session.ts`) and renders the app once it has
 * one, so a visitor is inside the product before they have told it anything about themselves. A shell that
 * cannot open a session says so and offers a retry, because a failed first request is a blank page
 * otherwise, and neither cause - the API being down, or the limiter refusing a new session - is the
 * visitor's mistake.
 */
type Boot = "opening" | "ready" | "failed";

export function AppLayout({ children }: { children: React.ReactNode }) {
  const pathname = useLocation().pathname;
  const t = useT();
  // localStorage does not exist while the server renders, and reading the stored token directly would make
  // the server's output (the spinner) disagree with the client's first render, which React throws the tree
  // away for. Gating on `hydrated` keeps the two in step - and keeps the session request on the client.
  const hydrated = useHydrated();
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [boot, setBoot] = useState<Boot>("opening");
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (!hydrated) return;

    let cancelled = false;
    setBoot("opening");
    ensureSession()
      .then(() => {
        if (!cancelled) setBoot("ready");
      })
      .catch((err: unknown) => {
        // The reason belongs in the console: the screen can only say "try again" without turning into a
        // debug console, and the last investigation looked exactly here and found nothing.
        console.error("kada: could not open a session", err);
        if (!cancelled) setBoot("failed");
      });

    return () => {
      cancelled = true;
    };
  }, [hydrated, attempt]);

  // Close the mobile sidebar when the route changes (React's documented
  // "adjust state when a prop changes" pattern).
  const [prevPath, setPrevPath] = useState(pathname);
  if (prevPath !== pathname) {
    setPrevPath(pathname);
    setSidebarOpen(false);
  }

  if (boot !== "ready") {
    return (
      <div className="flex h-screen items-center justify-center bg-subtle px-6">
        {boot === "failed" ? (
          <div className="max-w-sm text-center">
            <p className="text-sm text-body">{t("暂时无法打开会话，请稍后重试。")}</p>
            <button
              type="button"
              onClick={() => setAttempt((n) => n + 1)}
              className={buttonClass({ size: "sm", className: "mt-4" })}
            >
              {t("重试")}
            </button>
          </div>
        ) : (
          <div className="flex flex-col items-center gap-3">
            <div className="size-8 animate-spin rounded-full border-2 border-brand border-t-transparent" />
            <p className="text-sm text-faint">{t("加载中...")}</p>
          </div>
        )}
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
    if (pathname === "/dashboard/settings") return t("用户");
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

          {/* Right: theme + GitHub + language, in the order and at the position every shell uses. Signing
              out used to live on the settings page; there is nothing to sign out of while the account is a
              guest one, and the claim step that will need it does not exist yet. */}
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
