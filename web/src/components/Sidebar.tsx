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
  User,
  X,
  type LucideIcon,
} from "lucide-react";
import { getUser } from "@/lib/auth";
import { cn } from "@/lib/utils";
import { useT } from "@/lib/i18n";

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
    ],
  },
];

export function Sidebar({ onCloseMobile }: { onCloseMobile?: () => void }) {
  const pathname = useLocation().pathname;
  const t = useT();
  // The account the app is running as. A guest row has no phone number yet, and "游客" is what that
  // state is called here rather than an absence: the account exists from the first page load, so the row
  // names who it belongs to instead of reporting a missing field.
  const identity = getUser()?.phone || t("游客");

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
        {NAV_SECTIONS.map((section, secIdx) => (
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

      {/* The account, where "设置" used to be. The row is the only place in the shell that can say which
          account the links below belong to, and it says the same thing the user page does: 游客 until the
          guest row is claimed, the phone number after. The version string and the filing number are not
          navigation, so they moved to the bottom of that page. */}
      <div className="border-t border-line px-3 py-3">
        <Link
          to="/dashboard/settings"
          onClick={handleClick}
          className={cn(
            "flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors",
            pathname === "/dashboard/settings"
              ? "bg-brand-soft text-brand-ink"
              : "text-muted hover:bg-muted-surface hover:text-strong"
          )}
        >
          <User className={cn("size-4 shrink-0", pathname === "/dashboard/settings" && "text-brand-ink")} />
          <span className="truncate">{identity}</span>
        </Link>
      </div>
    </aside>
  );
}
