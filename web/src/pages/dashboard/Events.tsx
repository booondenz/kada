import { useState } from "react";
import useSWR from "swr";
import { ExternalLink, MousePointerClick, ChevronLeft, ChevronRight } from "lucide-react";
import { analyticsAPI } from "@/lib/api";
import { getToken } from "@/lib/auth";
import { safeHref } from "@/lib/utils";
import { useT } from "@/lib/i18n";
import { useI18n } from "@/lib/i18n/context";

function parseReferer(referer: string): { isAction: boolean; action?: string; refererUrl?: string } {
  if (!referer) return { isAction: false };
  if (referer.startsWith("action:")) {
    const action = referer.slice(7);
    return { isAction: true, action };
  }
  return { isAction: false, refererUrl: referer };
}

export default function EventsPage() {
  const token = getToken();
  const t = useT();
  const { locale } = useI18n();
  const localeTag = locale === "en" ? "en-US" : "zh-CN";
  const [page, setPage] = useState(1);

  const platformConfig: Record<string, { label: string; color: string; icon: string }> = {
    browser: { label: t("浏览器"), color: "bg-blue-50 text-blue-700", icon: "🌐" },
    wechat: { label: t("微信"), color: "bg-green-50 text-green-700", icon: "💬" },
    qq: { label: "QQ", color: "bg-sky-50 text-sky-700", icon: "🐧" },
    weibo: { label: t("微博"), color: "bg-red-50 text-red-700", icon: "📢" },
    xiaohongshu: { label: t("小红书"), color: "bg-rose-50 text-rose-700", icon: "📕" },
    sms: { label: t("短信"), color: "bg-amber-50 text-amber-700", icon: "📩" },
    unknown: { label: t("其他"), color: "bg-gray-50 text-muted", icon: "❓" },
  };

  const actionLabels: Record<string, string> = {
    copy_link: t("📋 复制链接"),
    qr_view: t("📱 查看二维码"),
    open_browser: t("🚀 浏览器打开"),
    deeplink_try: t("📲 DeepLink 尝试"),
    open_link: t("🔗 直接打开"),
  };

  const { data, isLoading } = useSWR(
    token ? `analytics-events-${page}` : null,
    () => analyticsAPI.events(token!, page, 20)
  );

  const events = data?.events ?? [];
  const total = data?.total ?? 0;
  const totalPages = Math.ceil(total / 20);

  return (
    <div className="space-y-5">
      <div>
        <h1 className="text-2xl font-bold text-strong">{t("事件")}</h1>
        <p className="text-sm text-muted mt-1">{t("点击事件与平台行为记录")}</p>
      </div>

      <div className="bg-canvas rounded-xl border border-line shadow-sm overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-line bg-gray-50/50">
                <th className="text-left px-5 py-3 text-xs font-medium text-muted uppercase">{t("时间")}</th>
                <th className="text-left px-5 py-3 text-xs font-medium text-muted uppercase">{t("短链")}</th>
                <th className="text-left px-5 py-3 text-xs font-medium text-muted uppercase">{t("平台")}</th>
                <th className="text-left px-5 py-3 text-xs font-medium text-muted uppercase">{t("行为")}</th>
                <th className="text-left px-5 py-3 text-xs font-medium text-muted uppercase">IP</th>
              </tr>
            </thead>
            <tbody>
              {isLoading ? (
                Array.from({ length: 5 }).map((_, i) => (
                  <tr key={i} className="border-b border-gray-50 animate-pulse">
                    {Array.from({ length: 5 }).map((_, j) => (
                      <td key={j} className="px-5 py-3"><div className="h-4 bg-muted-surface rounded w-20" /></td>
                    ))}
                  </tr>
                ))
              ) : events.length === 0 ? (
                <tr>
                  <td colSpan={5} className="px-5 py-16 text-center">
                    <MousePointerClick className="size-8 text-gray-300 mx-auto mb-2" />
                    <p className="text-sm text-muted">{t("暂无点击事件")}</p>
                    <p className="text-xs text-faint mt-1">{t("当有人点击您的短链时，事件会显示在这里")}</p>
                  </td>
                </tr>
              ) : (
                events.map((e: {
                  id: number;
                  short_code: string;
                  original_url: string;
                  platform: string;
                  ip: string;
                  referer: string;
                  created_at: string;
                }) => {
                  const pConfig = platformConfig[e.platform] || platformConfig.unknown;
                  const { isAction, action, refererUrl } = parseReferer(e.referer);
                  return (
                    <tr key={e.id} className="border-b border-gray-50 hover:bg-gray-50/50 transition">
                      <td className="px-5 py-3 text-muted whitespace-nowrap font-mono text-xs">
                        {new Date(e.created_at).toLocaleString(localeTag, {
                          month: "2-digit", day: "2-digit",
                          hour: "2-digit", minute: "2-digit",
                        })}
                      </td>
                      <td className="px-5 py-3">
                        <div className="flex items-center gap-1.5">
                          <code className="text-brand-ink font-mono text-xs bg-brand-soft px-1.5 py-0.5 rounded">{e.short_code}</code>
                          <a href={safeHref(e.original_url)} target="_blank" rel="noopener noreferrer" className="text-gray-300 hover:text-muted" title={e.original_url}>
                            <ExternalLink className="size-3" />
                          </a>
                        </div>
                      </td>
                      <td className="px-5 py-3">
                        <span className={`inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full ${pConfig.color}`}>
                          <span>{pConfig.icon}</span>
                          <span>{pConfig.label}</span>
                        </span>
                      </td>
                      <td className="px-5 py-3">
                        {isAction ? (
                          <span className="text-xs px-2 py-0.5 rounded-full bg-violet-50 text-violet-700">
                            {actionLabels[action || ""] || `📌 ${action}`}
                          </span>
                        ) : refererUrl ? (
                          <span className="text-xs text-faint max-w-[160px] truncate block" title={refererUrl}>
                            {t("来源: {refererUrl}", { refererUrl })}
                          </span>
                        ) : (
                          <span className="text-xs text-faint">{t("🔗 直接访问")}</span>
                        )}
                      </td>
                      <td className="px-5 py-3 text-faint font-mono text-xs">{e.ip || "-"}</td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>

        {totalPages > 1 && (
          <div className="flex items-center justify-between px-5 py-3 border-t border-line">
            <p className="text-xs text-muted">{t("共 {total} 条记录", { total })}</p>
            <div className="flex items-center gap-2">
              <button
                onClick={() => setPage(p => Math.max(1, p - 1))}
                disabled={page <= 1}
                className="p-1.5 rounded-lg text-muted hover:bg-muted-surface disabled:opacity-30 disabled:cursor-not-allowed transition"
              >
                <ChevronLeft className="size-4" />
              </button>
              <span className="text-xs text-muted tabular-nums">{page} / {totalPages}</span>
              <button
                onClick={() => setPage(p => Math.min(totalPages, p + 1))}
                disabled={page >= totalPages}
                className="p-1.5 rounded-lg text-muted hover:bg-muted-surface disabled:opacity-30 disabled:cursor-not-allowed transition"
              >
                <ChevronRight className="size-4" />
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
