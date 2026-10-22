import { Link } from "react-router";
import { useLocation } from "react-router";
import {
  Link2,
  Globe,
  BarChart3,
  MousePointerClick,
  Users,
  Folder,
  Tag,
  ArrowRightLeft,
  Sparkles,
  ShieldCheck,
  Braces,
  X,
  type LucideIcon,
} from "lucide-react";
import useSWR from "swr";
import { adminAPI } from "@/lib/api";
import { getToken } from "@/lib/auth";
import { getAdminSecret } from "@/lib/adminSecret";
import { cn } from "@/lib/utils";
import { useT } from "@/lib/i18n";
import { IcpNotice } from "@/components/IcpNotice";
import { PublicSecurityNotice } from "@/components/PublicSecurityNotice";

interface NavItem {
  name: string;
  href: string;
  icon: LucideIcon;
  isActive?: (pathname: string, href: string) => boolean;
}

interface NavSection {
  name?: string;
  items: NavItem[];
}

// Labels live here as Chinese source strings; they are resolved through t() at
// render time because this table is evaluated before React context is available.
const NAV_SECTIONS: NavSection[] = [
  {
    items: [
      { name: "链接", href: "/dashboard", icon: Link2,
        isActive: (pathname: string) =>
          pathname === "/dashboard" || pathname.startsWith("/dashboard/links"), },
      { name: "域名", href: "/dashboard/domains", icon: Globe },
    ],
  },
  {
    name: "洞察",
    items: [
      { name: "分析", href: "/dashboard/analytics", icon: BarChart3 },
      { name: "事件", href: "/dashboard/events", icon: MousePointerClick },
      { name: "客户", href: "/dashboard/customers", icon: Users },
    ],
  },
  {
    name: "资源管理",
    items: [
      { name: "文件夹", href: "/dashboard/folders", icon: Folder },
      { name: "标签", href: "/dashboard/tags", icon: Tag },
      { name: "UTM 模板", href: "/dashboard/utm", icon: ArrowRightLeft },
    ],
  },
  {
    name: "AI",
    items: [
      { name: "AI 助手", href: "/dashboard/ai", icon: Sparkles },
      { name: "API", href: "/dashboard/api", icon: Braces },
    ],
  },
];

// The operator entry. It is not in the table above because whether it belongs there is a question only the
// server can answer: the operator list is a deployment setting, not a client one.
const MODERATION_ITEM: NavItem = { name: "运营", href: "/dashboard/moderation", icon: ShieldCheck };

export function Sidebar({ onCloseMobile }: { onCloseMobile?: () => void }) {
  const pathname = useLocation().pathname;
  const t = useT();
  const token = getToken();

  // Whether this browser holds the operator secret. Without one the routes answer 403, so there is nothing
  // to ask; with one, the cheapest operator route is the check, and its answer is cached for the session.
  const secret = getAdminSecret();
  const { data: isOperator } = useSWR(
    token && secret ? "admin-probe" : null,
    async () => {
      try {
        await adminAPI.reports(token!, 1);
        return true;
      } catch {
        return false;
      }
    },
    { revalidateOnFocus: false, shouldRetryOnError: false },
  );

  // The new entries sit in the existing AI section rather than in a group of their own, which is where the
  // operator asked for them while there is only one of them.
  const sections = NAV_SECTIONS.map((section) =>
    isOperator && section.name === "AI" ? { ...section, items: [...section.items, MODERATION_ITEM] } : section,
  );

  const handleClick = () => {
    // Close mobile sidebar after navigation
    setTimeout(() => onCloseMobile?.(), 100);
  };

  return (
    <aside className="flex h-full w-[240px] shrink-0 flex-col border-r border-line bg-canvas">
      {/* The wordmark alone. It used to sit next to a filled indigo tile holding the link glyph, which spent
          the one accent on a logo - the one place in the shell where the accent cannot mean anything. */}
      <div className="flex h-14 items-center justify-between px-4 border-b border-line">
        <span className="font-bold text-lg text-strong">Kada</span>
        {onCloseMobile && (
          <button onClick={onCloseMobile} className="lg:hidden p-1.5 rounded-lg text-faint hover:bg-muted-surface transition">
            <X className="size-4" />
          </button>
        )}
      </div>

      {/* Nav */}
      <nav className="flex-1 overflow-y-auto px-3 py-4">
        {sections.map((section, secIdx) => (
          <div key={secIdx} className={cn(secIdx > 0 && "mt-6")}>
            {section.name && (
              <p className="mb-1.5 px-3 text-xs font-medium text-faint uppercase tracking-wider">
                {t(section.name)}
              </p>
            )}
            <div className="space-y-0.5">
              {section.items.map((item) => {
                const isActive = item.isActive
                  ? item.isActive(pathname, item.href)
                  : pathname.startsWith(item.href);
                const Icon = item.icon;

                return (
                  <Link
                    key={item.href}
                    to={item.href}
                    onClick={handleClick}
                    className={cn(
                      "flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors",
                      isActive
                        ? "bg-brand-soft text-brand-ink"
                        : "text-muted hover:bg-muted-surface hover:text-strong"
                    )}
                  >
                    <Icon className={cn("size-4 shrink-0", isActive && "text-brand-ink")} />
                    <span>{t(item.name)}</span>
                  </Link>
                );
              })}
            </div>
          </div>
        ))}
      </nav>

      {/* The bottom of the sidebar is the one place every page reaches, so it is where the filings sit -
          the account row that used to be here named an identity the product no longer has. The links are
          above the numbers because a filing is not navigation: the two things a visitor might act on come
          first, and the two things a regulator checks come after. */}
      <div className="space-y-1.5 border-t border-line px-4 py-3 text-xs">
        <div className="flex flex-wrap items-center gap-x-3">
          <Link to="/privacy" className="text-muted transition-colors hover:text-strong">
            {t("隐私政策")}
          </Link>
          <Link to="/report" className="text-muted transition-colors hover:text-strong">
            {t("举报")}
          </Link>
        </div>
        <IcpNotice className="block text-faint transition-colors hover:text-muted" />
        <PublicSecurityNotice className="block text-faint transition-colors hover:text-muted" />
      </div>
    </aside>
  );
}
