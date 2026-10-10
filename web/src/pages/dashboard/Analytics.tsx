import { BarChart3, Link2, MousePointerClick, TrendingUp } from "lucide-react";
import useSWR from "swr";
import { analyticsAPI } from "@/lib/api";
import { getToken } from "@/lib/auth";
import { useT } from "@/lib/i18n";

export default function AnalyticsPage() {
  const t = useT();
  const token = getToken();

  const { data: overview } = useSWR(
    token ? "analytics-overview" : null,
    () => analyticsAPI.overview(token!)
  );

  const { data: platformsData } = useSWR(
    token ? "analytics-platforms" : null,
    () => analyticsAPI.platforms(token!)
  );

  const { data: dailyData } = useSWR(
    token ? "analytics-daily" : null,
    () => analyticsAPI.daily(token!)
  );

  const totalLinks = overview?.total_links ?? 0;
  const totalClicks = overview?.total_clicks ?? 0;
  const platforms: Array<{ platform: string; count: number }> = platformsData?.platforms ?? [];
  const daily: Array<{ date: string; count: number }> = dailyData?.daily ?? [];

  const statCards = [
    {
      label: t("总链接数"),
      value: totalLinks.toLocaleString(),
      icon: Link2,
      color: "text-brand-ink bg-brand-soft",
    },
    {
      label: t("总点击数"),
      value: totalClicks.toLocaleString(),
      icon: MousePointerClick,
      color: "text-emerald-600 bg-emerald-50",
    },
    {
      label: t("平均点击"),
      value: totalLinks > 0 ? Math.round(totalClicks / totalLinks).toLocaleString() : "0",
      icon: BarChart3,
      color: "text-amber-600 bg-amber-50",
    },
    {
      label: t("平台来源"),
      value: t("${platforms.length} 种").replace("${platforms.length}", String(platforms.length)),
      icon: TrendingUp,
      color: "text-rose-600 bg-rose-50",
    },
  ];

  const maxDailyCount = Math.max(...daily.map((d) => d.count), 1);

  const platformLabels: Record<string, string> = {
    browser: t("浏览器"),
    wechat: t("微信"),
    qq: "QQ",
    weibo: t("微博"),
    xiaohongshu: t("小红书"),
    sms: t("短信"),
    unknown: t("其他"),
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-strong">{t("数据统计")}</h1>
        <p className="text-sm text-muted mt-1">{t("链接点击数据和概览")}</p>
      </div>

      {/* Stat Cards */}
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        {statCards.map(({ label, value, icon: Icon, color }) => (
          <div key={label} className="rounded-xl border border-line bg-canvas p-5 shadow-sm">
            <div className={`inline-flex size-10 items-center justify-center rounded-lg ${color}`}>
              <Icon className="size-5" />
            </div>
            <p className="mt-3 text-2xl font-bold text-strong tabular-nums">{value}</p>
            <p className="mt-0.5 text-sm text-muted">{label}</p>
          </div>
        ))}
      </div>

      <div className="grid gap-6 lg:grid-cols-5">
        {/* Platform Distribution */}
        <div className="lg:col-span-2 rounded-xl border border-line bg-canvas shadow-sm">
          <div className="border-b border-line px-5 py-4">
            <h2 className="font-semibold text-strong">{t("平台来源分布")}</h2>
          </div>
          <div className="p-5">
            {platforms.length === 0 ? (
              <div className="flex flex-col items-center py-8 text-center">
                <BarChart3 className="size-8 text-gray-300 mb-2" />
                <p className="text-sm text-muted">{t("暂无点击数据")}</p>
              </div>
            ) : (
              <div className="space-y-3">
                {platforms.map((p) => {
                  const pct = totalClicks > 0 ? Math.round((p.count / totalClicks) * 100) : 0;
                  return (
                    <div key={p.platform}>
                      <div className="flex justify-between text-sm mb-1">
                        <span className="text-muted">{platformLabels[p.platform] || p.platform}</span>
                        <span className="text-strong font-medium">{p.count.toLocaleString()} <span className="text-xs text-faint">({pct}%)</span></span>
                      </div>
                      <div className="h-2 rounded-full bg-muted-surface overflow-hidden">
                        <div
                          className="h-full rounded-full bg-indigo-500 transition-all duration-500"
                          style={{ width: `${pct}%` }}
                        />
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        </div>

        {/* Daily Clicks Chart */}
        <div className="lg:col-span-3 rounded-xl border border-line bg-canvas shadow-sm">
          <div className="border-b border-line px-5 py-4">
            <h2 className="font-semibold text-strong">{t("每日点击量（近30天）")}</h2>
          </div>
          <div className="p-5">
            {daily.length === 0 ? (
              <div className="flex flex-col items-center py-12 text-center">
                <BarChart3 className="size-8 text-gray-300 mb-2" />
                <p className="text-sm text-muted">{t("暂无点击数据")}</p>
              </div>
            ) : (
              <div className="flex items-end gap-1 h-40">
                {daily.map((d) => {
                  const height = maxDailyCount > 0 ? (d.count / maxDailyCount) * 100 : 0;
                  return (
                    <div key={d.date} className="flex-1 flex flex-col items-center gap-1 min-w-0">
                      <div className="w-full flex flex-col justify-end" style={{ height: "140px" }}>
                        <div
                          className="w-full rounded-t bg-indigo-500 hover:bg-indigo-600 transition-colors min-h-[2px]"
                          style={{ height: `${Math.max(height, 1)}%` }}
                          title={t("${d.date}: ${d.count} 点击")
                            .replace("${d.date}", d.date)
                            .replace("${d.count}", String(d.count))}
                        />
                      </div>
                      <span className="text-[10px] text-faint truncate w-full text-center">
                        {d.date.slice(5)}
                      </span>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
