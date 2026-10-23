import useSWR from "swr";
import { Link, useSearchParams } from "react-router";
import { ArrowLeft } from "lucide-react";
import { analyticsAPI, linksAPI } from "@/lib/api";
import { getToken } from "@/lib/auth";
import { useT } from "@/lib/i18n";

/**
 * 统计: the numbers behind the clicks.
 *
 * It answers two questions from one page. Without a link it is the account's whole picture; with
 * `?link=<id>` it is one link's, which is what a link card's click total asks for - the per-link numbers
 * used to be folded away inside the link's own page, so the same chart existed twice and the page under
 * 洞察 knew nothing about any single link.
 */
export default function AnalyticsPage() {
  const t = useT();
  const token = getToken();
  const [searchParams] = useSearchParams();
  // A link id from the query string is untrusted input, and both per-link endpoints take it as a number:
  // anything that is not digits falls back to the account-wide view rather than asking for link "NaN".
  const requested = searchParams.get("link");
  const linkId = requested && /^\d+$/.test(requested) ? requested : null;

  // The link a scoped view is about: its title and short URL are the page's heading, and its total is the
  // one number the per-link endpoints do not return.
  const { data: linkData, error: linkError } = useSWR(
    token && linkId ? ["analytics-link", linkId] : null,
    () => linksAPI.get(token!, linkId!),
  );
  const link = linkId ? linkData?.link ?? linkData : null;

  const { data: overview } = useSWR(
    token && !linkId ? "analytics-overview" : null,
    () => analyticsAPI.overview(token!),
  );

  // The same two charts serve both views: the API takes an optional link_id, and the client picks the call.
  const { data: platformsData } = useSWR(
    token ? ["analytics-platforms", linkId ?? "all"] : null,
    () => (linkId ? analyticsAPI.platformsForLink(token!, Number(linkId)) : analyticsAPI.platforms(token!)),
  );

  const { data: dailyData } = useSWR(
    token ? ["analytics-daily", linkId ?? "all"] : null,
    () => (linkId ? analyticsAPI.dailyForLink(token!, Number(linkId)) : analyticsAPI.daily(token!)),
  );

  const platforms: Array<{ platform: string; count: number }> = platformsData?.platforms ?? [];
  const daily: Array<{ date: string; count: number }> = dailyData?.daily ?? [];
  const recentClicks = daily.reduce((sum, day) => sum + day.count, 0);

  // Four numbers, so four tiles - and nothing else: the tiles used to carry a tinted icon chip each, in
  // brand, emerald, amber and rose, which reported no status and made the eye read the colour before the
  // number. Size and weight say which number is which.
  const statCards = linkId
    ? [
        { label: t("总点击"), value: (link?.click_count ?? 0).toLocaleString() },
        { label: t("近 30 天点击"), value: recentClicks.toLocaleString() },
        { label: t("平均每日"), value: Math.round(recentClicks / 30).toLocaleString() },
        {
          label: t("平台来源"),
          value: t("${platforms.length} 种").replace("${platforms.length}", String(platforms.length)),
        },
      ]
    : [
        {
          label: t("总链接数"),
          value: (overview?.total_links ?? 0).toLocaleString(),
        },
        {
          label: t("总点击数"),
          value: (overview?.total_clicks ?? 0).toLocaleString(),
        },
        {
          label: t("平均点击"),
          value:
            (overview?.total_links ?? 0) > 0
              ? Math.round((overview?.total_clicks ?? 0) / overview.total_links).toLocaleString()
              : "0",
        },
        {
          label: t("平台来源"),
          value: t("${platforms.length} 种").replace("${platforms.length}", String(platforms.length)),
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

  const header = linkId ? (
    <div>
      <Link
        to="/dashboard/analytics"
        className="inline-flex items-center gap-1.5 text-sm text-muted transition hover:text-body"
      >
        <ArrowLeft className="size-4" /> {t("全部统计")}
      </Link>
      <h1 className="mt-2 text-2xl font-bold text-strong">
        {link ? link.title || link.short_code : linkError ? t("链接不存在") : ""}
      </h1>
      <p className="mt-1 flex flex-wrap items-center gap-x-3 text-sm text-muted">
        {link && <code className="font-mono text-xs text-brand-ink">{link.short_url}</code>}
        {linkError && <span>{t("这条链接的统计读不出来。")}</span>}
      </p>
    </div>
  ) : (
    <div>
      <h1 className="text-2xl font-bold text-strong">{t("数据统计")}</h1>
      <p className="mt-1 text-sm text-muted">{t("链接点击数据和概览")}</p>
    </div>
  );

  return (
    <div className="space-y-6">
      {header}

      {/* Stat Cards */}
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        {statCards.map(({ label, value }) => (
          <div key={label} className="rounded-xl border border-line bg-canvas p-5 shadow-sm">
            <p className="text-2xl font-bold text-strong tabular-nums">{value}</p>
            <p className="mt-0.5 text-sm text-muted">{label}</p>
          </div>
        ))}
      </div>

      <div className="grid gap-6 lg:grid-cols-5">
        {/* Platform Distribution */}
        <div className="rounded-xl border border-line bg-canvas shadow-sm lg:col-span-2">
          <div className="border-b border-line px-5 py-4">
            <h2 className="font-semibold text-strong">{t("平台来源分布")}</h2>
          </div>
          <div className="p-5">
            {platforms.length === 0 ? (
              <p className="py-8 text-center text-sm text-muted">{t("暂无点击数据")}</p>
            ) : (
              <div className="space-y-3">
                {platforms.map((p) => {
                  const total = platforms.reduce((sum, platform) => sum + platform.count, 0);
                  const pct = total > 0 ? Math.round((p.count / total) * 100) : 0;
                  return (
                    <div key={p.platform}>
                      <div className="mb-1 flex justify-between text-sm">
                        <span className="text-muted">{platformLabels[p.platform] || p.platform}</span>
                        <span className="font-medium text-strong">
                          {p.count.toLocaleString()}{" "}
                          <span className="text-xs text-faint">({pct}%)</span>
                        </span>
                      </div>
                      <div className="h-2 overflow-hidden rounded-full bg-muted-surface">
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
        <div className="rounded-xl border border-line bg-canvas shadow-sm lg:col-span-3">
          <div className="border-b border-line px-5 py-4">
            <h2 className="font-semibold text-strong">{t("每日点击量（近30天）")}</h2>
          </div>
          <div className="p-5">
            {daily.length === 0 ? (
              <p className="py-12 text-center text-sm text-muted">{t("暂无点击数据")}</p>
            ) : (
              <div className="flex h-40 items-end gap-1">
                {daily.map((d) => {
                  const height = maxDailyCount > 0 ? (d.count / maxDailyCount) * 100 : 0;
                  return (
                    <div key={d.date} className="flex min-w-0 flex-1 flex-col items-center gap-1">
                      <div className="flex w-full flex-col justify-end" style={{ height: "140px" }}>
                        <div
                          className="w-full min-h-[2px] rounded-t bg-indigo-500 transition-colors hover:bg-indigo-600"
                          style={{ height: `${Math.max(height, 1)}%` }}
                          title={t("${d.date}: ${d.count} 点击")
                            .replace("${d.date}", d.date)
                            .replace("${d.count}", String(d.count))}
                        />
                      </div>
                      <span className="w-full truncate text-center text-[10px] text-faint">
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
