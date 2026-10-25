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
  Split,
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
import { FilingCredits } from "@/components/FilingCredits";

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
      // The numbers, the raw events behind them, the visitors they came from, and where the traffic is being
      // sent: four ways of looking at the same clicks, so they sit together. 分流 used to be a block on the
      // link page, which meant the one question it answers - "where does my traffic go" - was asked per link.
      { name: "统计", href: "/dashboard/analytics", icon: BarChart3 },
      { name: "事件", href: "/dashboard/events", icon: MousePointerClick },
      { name: "客户", href: "/dashboard/customers", icon: Users },
      { name: "分流", href: "/dashboard/targets", icon: Split },
    ],
  },
  {
    // The API page used to sit under AI, next to the assistant that called it. It documents this product's
    // own endpoints and holds the tokens that reach them, which makes it a resource like the three above.
    name: "资源管理",
    items: [
      { name: "文件夹", href: "/dashboard/folders", icon: Folder },
      { name: "标签", href: "/dashboard/tags", icon: Tag },
      { name: "UTM 模板", href: "/dashboard/utm", icon: ArrowRightLeft },
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

  // The operator entry has a group of its own, appended after the last one: it is not a resource the
  // product offers, and it exists only for a browser that holds ADMIN_SECRET.
  const sections = isOperator ? [...NAV_SECTIONS, { items: [MODERATION_ITEM] }] : NAV_SECTIONS;

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
          the account row that used to be here named an identity the product no longer has. They are one
          control now and the rule above them is gone with the rows they used to fill: see FilingCredits. */}
      <div className="px-4 py-3">
        <FilingCredits />
      </div>
    </aside>
  );
}
