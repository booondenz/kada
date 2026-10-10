import { useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router";
import {
  Copy, ExternalLink, Pencil, Save, X, Check, QrCode, Download,
  BarChart3, Globe, Clock, Shield, Smartphone, Tags, ArrowLeft, Folder,
} from "lucide-react";
import { toast } from "sonner";
import useSWR from "swr";
import { linksAPI, foldersAPI, tagsAPI, domainsAPI, workspacesAPI, analyticsAPI } from "@/lib/api";
import { getToken } from "@/lib/auth";
import { safeHref } from "@/lib/utils";
import { useT, useI18n } from "@/lib/i18n";

interface TagInfo {
  id: number;
  name: string;
  color: string;
}

interface LinkDetail {
  id: number;
  short_code: string;
  short_url: string;
  original_url: string;
  title: string;
  description: string;
  domain: string;
  click_count: number;
  is_active: boolean;
  created_at: string;
  updated_at: string;
  utm_source?: string;
  utm_medium?: string;
  utm_campaign?: string;
  utm_term?: string;
  utm_content?: string;
  ios_url?: string;
  android_url?: string;
  has_password?: boolean;
  expires_at?: string;
  folder_id?: number;
  folder_name?: string;
  workspace_id?: number;
  tags?: TagInfo[];
}

export default function LinkDetailPage() {
  const { id = "" } = useParams<{ id: string }>();
  const t = useT();
  const { locale } = useI18n();
  const dateLocale = locale === "en" ? "en-US" : "zh-CN";

  const platformLabels: Record<string, string> = {
    browser: t("浏览器"), wechat: t("微信"), qq: "QQ",
    weibo: t("微博"), xiaohongshu: t("小红书"), sms: t("短信"), unknown: t("其他"),
  };

  const [link, setLink] = useState<LinkDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [editing, setEditing] = useState(false);
  const [copied, setCopied] = useState(false);

  // Edit form
  const [editTitle, setEditTitle] = useState("");
  const [editDescription, setEditDescription] = useState("");
  const [editOriginalUrl, setEditOriginalUrl] = useState("");
  const [editShortCode, setEditShortCode] = useState("");
  const [editDomain, setEditDomain] = useState("");
  const [editPassword, setEditPassword] = useState("");
  const [editExpiresAt, setEditExpiresAt] = useState("");
  const [editFolderId, setEditFolderId] = useState<number | null>(null);
  const [editWorkspaceId, setEditWorkspaceId] = useState<number | null>(null);
  const [editTagIds, setEditTagIds] = useState<number[]>([]);
  const [editUtmSource, setEditUtmSource] = useState("");
  const [editUtmMedium, setEditUtmMedium] = useState("");
  const [editUtmCampaign, setEditUtmCampaign] = useState("");
  const [editUtmTerm, setEditUtmTerm] = useState("");
  const [editUtmContent, setEditUtmContent] = useState("");
  const [editIosUrl, setEditIosUrl] = useState("");
  const [editAndroidUrl, setEditAndroidUrl] = useState("");
  const [editShowAdvanced, setEditShowAdvanced] = useState(false);

  // QR code
  const [showQR, setShowQR] = useState(false);
  const [qrURL, setQrURL] = useState<string | null>(null);

  // Analytics
  const [showAnalytics, setShowAnalytics] = useState(false);
  const [clickData, setClickData] = useState<{ daily: { date: string; count: number }[]; platforms: { platform: string; count: number }[] } | null>(null);

  const token = getToken();
  const { data: folderData } = useSWR(token ? "folders" : null, () => foldersAPI.list(token!));
  const { data: tagData } = useSWR(token ? "tags" : null, () => tagsAPI.list(token!));
  const { data: domainData } = useSWR(token ? "domains" : null, () => domainsAPI.list(token!));
  const { data: workspaceData } = useSWR(token ? "workspaces" : null, () => workspacesAPI.list(token!));
  const folders = folderData?.folders ?? [];
  const allTags = tagData?.tags ?? [];
  const workspaces = workspaceData?.workspaces ?? [];
  const verifiedDomains = (domainData?.domains ?? []).filter((d: { verified: boolean }) => d.verified);

  // Memoised so the effect below can depend on it without refetching on every render.
  const fetchLink = useCallback(() => {
    if (!token) return;
    // `loading` starts as true (useState(true)), so no synchronous setState
    // happens here; state is only updated in the async callbacks below.
    linksAPI.get(token, id)
      .then((data) => {
        const l = data.link || data;
        setLink(l);
        setEditTitle(l.title || "");
        setEditDescription(l.description || "");
        setEditOriginalUrl(l.original_url || "");
        setEditShortCode(l.short_code || "");
        setEditDomain(l.domain || "");
        setEditExpiresAt(l.expires_at ? l.expires_at.slice(0, 16) : "");
        setEditFolderId(l.folder_id ?? null);
        setEditWorkspaceId(l.workspace_id ?? null);
        setEditTagIds((l.tags ?? []).map((tag: TagInfo) => tag.id));
        setEditUtmSource(l.utm_source || "");
        setEditUtmMedium(l.utm_medium || "");
        setEditUtmCampaign(l.utm_campaign || "");
        setEditUtmTerm(l.utm_term || "");
        setEditUtmContent(l.utm_content || "");
        setEditIosUrl(l.ios_url || "");
        setEditAndroidUrl(l.android_url || "");
      })
      .catch(() => toast.error(t("加载链接失败")))
      .finally(() => setLoading(false));
  }, [id, token, t]);

  useEffect(() => { fetchLink(); }, [fetchLink]);

  // Fetch per-link analytics
  useEffect(() => {
    if (showAnalytics && link && token) {
      Promise.all([
        analyticsAPI.dailyForLink(token, link.id),
        analyticsAPI.platformsForLink(token, link.id),
      ]).then(([dailyData, platformsData]) => {
        setClickData({
          daily: dailyData.daily ?? [],
          platforms: platformsData.platforms ?? [],
        });
      }).catch((e: unknown) => toast.error(e instanceof Error ? e.message : t("加载统计数据失败")));
    }
  }, [showAnalytics, link, token, t]);

  // QR code
  useEffect(() => {
    if (showQR && link && !qrURL) {
      import("qrcode").then((QRCode) => {
        const canvas = document.createElement("canvas");
        QRCode.toCanvas(canvas, link.short_url, {
          width: 256, margin: 2,
          color: { dark: "#000000", light: "#ffffff" },
          errorCorrectionLevel: "M",
        });
        setQrURL(canvas.toDataURL("image/png"));
      });
    }
  }, [showQR, link, qrURL]);

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
    setCopied(true);
    toast.success(t("已复制"));
    setTimeout(() => setCopied(false), 2000);
  };

  const handleSave = async () => {
    if (!token || !link) return;
    setSaving(true);
    try {
      const payload: Record<string, unknown> = {
        title: editTitle,
        description: editDescription,
        original_url: editOriginalUrl,
        folder_id: editFolderId,
        workspace_id: editWorkspaceId,
        tag_ids: editTagIds,
        short_code: editShortCode || undefined,
        domain: editDomain || undefined,
        utm_source: editUtmSource || undefined,
        utm_medium: editUtmMedium || undefined,
        utm_campaign: editUtmCampaign || undefined,
        utm_term: editUtmTerm || undefined,
        utm_content: editUtmContent || undefined,
        ios_url: editIosUrl || undefined,
        android_url: editAndroidUrl || undefined,
      };
      if (editPassword) payload.password = editPassword;
      if (editExpiresAt) payload.expires_at = new Date(editExpiresAt).toISOString();
      const updated = await linksAPI.update(token, link.id, payload);
      setLink({ ...link, ...updated });
      setEditing(false);
      toast.success(t("已更新"));
    } catch (e: unknown) {
      toast.error(e instanceof Error ? e.message : t("更新失败"));
    } finally {
      setSaving(false);
    }
  };

  const handleToggleActive = async () => {
    if (!token || !link) return;
    try {
      await linksAPI.update(token, link.id, { is_active: !link.is_active });
      setLink({ ...link, is_active: !link.is_active });
      toast.success(link.is_active ? t("已停用") : t("已启用"));
    } catch {
      toast.error(t("状态更新失败"));
    }
  };

  if (loading) {
    return (
      <div className="max-w-2xl mx-auto">
        <div className="animate-pulse space-y-4">
          <div className="h-6 w-24 bg-raised rounded" />
          <div className="h-64 bg-muted-surface rounded-xl" />
        </div>
      </div>
    );
  }

  if (!link) {
    return (
      <div className="max-w-2xl mx-auto text-center py-16">
        <div className="text-4xl mb-4">🔍</div>
        <h2 className="text-lg font-medium text-strong">{t("链接不存在")}</h2>
        <Link to="/dashboard" className="inline-block mt-4 text-brand-ink text-sm">{t("返回列表")}</Link>
      </div>
    );
  }

  return (
    <div className="max-w-2xl mx-auto space-y-6">
      <Link to="/dashboard" className="inline-flex items-center gap-1.5 text-sm text-muted hover:text-body transition">
        <ArrowLeft className="size-4" /> {t("返回链接列表")}
      </Link>

      <div className="bg-canvas rounded-xl border border-line shadow-sm overflow-hidden">
        {/* Header */}
        <div className="p-6 border-b border-line">
          <div className="flex items-center justify-between gap-4">
            {editing ? (
              <input
                type="text" value={editTitle}
                onChange={(e) => setEditTitle(e.target.value)}
                className="flex-1 text-xl font-bold text-strong border-b-2 border-indigo-300 focus:border-indigo-500 focus:outline-none px-2 py-1"
                placeholder={t("链接标题")} autoFocus
              />
            ) : (
              <div className="flex items-center gap-3 flex-1 min-w-0">
                <h1 className="text-xl font-bold text-strong truncate">{link.title || t("未命名链接")}</h1>
                <button onClick={handleToggleActive}
                  className={`shrink-0 inline-flex items-center gap-1 px-2.5 py-1 rounded-full text-xs font-medium transition ${
                    link.is_active ? "bg-emerald-50 text-emerald-700" : "bg-muted-surface text-muted"
                  }`}
                >
                  {link.is_active ? t("启用中") : t("已停用")}
                </button>
              </div>
            )}
            {editing ? (
              <div className="flex items-center gap-2 shrink-0">
                <button onClick={() => setEditing(false)} disabled={saving}
                  className="p-2 text-faint hover:text-muted transition"><X className="size-4" /></button>
                <button onClick={handleSave} disabled={saving}
                  className="inline-flex items-center gap-1 px-4 py-2 text-sm font-medium text-white bg-indigo-600 hover:bg-indigo-700 rounded-lg transition disabled:opacity-50">
                  <Save className="size-3.5" /> {saving ? t("保存中...") : t("保存")}
                </button>
              </div>
            ) : (
              <button onClick={() => setEditing(true)}
                className="p-2 text-faint hover:text-brand-ink transition shrink-0"><Pencil className="size-4" /></button>
            )}
          </div>

          {/* Folder + Tags display */}
          <div className="flex items-center gap-2 mt-2 flex-wrap">
            {link.folder_name && (
              <span className="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full bg-amber-50 text-amber-700">
                <Folder className="size-3" />
                {link.folder_name}
              </span>
            )}
            {(link.tags ?? []).map((tag) => (
              <span key={tag.id} className="text-xs px-2 py-0.5 rounded-full text-white" style={{ backgroundColor: tag.color }}>
                {tag.name}
              </span>
            ))}
          </div>
        </div>

        {/* Short URL bar */}
        <div className="px-6 py-4 bg-brand-soft/50 flex items-center gap-2">
          <code className="text-brand-ink font-mono font-medium text-sm flex-1 truncate">{link.short_url}</code>
          <button onClick={() => copyToClipboard(link.short_url)}
            className={`p-1.5 rounded-lg transition shrink-0 ${copied ? "text-green-500 bg-green-50" : "text-indigo-400 hover:text-brand-ink"}`}>
            {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
          </button>
          <button onClick={() => setShowQR(true)}
            className="p-1.5 rounded-lg text-indigo-400 hover:text-brand-ink transition shrink-0"><QrCode className="size-3.5" /></button>
          <a href={link.short_url} target="_blank" rel="noopener noreferrer"
            className="p-1.5 rounded-lg text-indigo-400 hover:text-brand-ink transition shrink-0"><ExternalLink className="size-3.5" /></a>
        </div>

        {/* Stats row */}
        <div className="grid grid-cols-3 divide-x border-b border-line">
          <div className="p-4 text-center">
            <p className="text-2xl font-bold text-strong tabular-nums">{link.click_count.toLocaleString()}</p>
            <p className="text-xs text-muted mt-0.5">{t("总点击")}</p>
          </div>
          <div className="p-4 text-center">
            <p className="text-sm font-bold text-strong">{new Date(link.created_at).toLocaleDateString(dateLocale)}</p>
            <p className="text-xs text-muted mt-0.5">{t("创建日期")}</p>
          </div>
          <div className="p-4 text-center">
            <button
              onClick={() => setShowAnalytics(!showAnalytics)}
              className={`text-sm font-bold transition ${showAnalytics ? "text-brand-ink" : "text-strong"}`}
            >
              <BarChart3 className="size-4 inline mr-1" />
              {t("统计")}
            </button>
            <p className="text-xs text-muted mt-0.5">{t("点击详情")}</p>
          </div>
        </div>

        {/* Per-link analytics */}
        {showAnalytics && (
          <div className="p-6 border-b border-line bg-gray-50/50 space-y-4">
            <h3 className="text-sm font-semibold text-strong">{t("点击统计")}</h3>
            {clickData ? (
              <>
                {/* Daily chart */}
                {clickData.daily.length > 0 && (
                  <div>
                    <p className="text-xs text-muted mb-2">{t("每日点击量（近30天）")}</p>
                    <div className="flex items-end gap-1 h-24">
                      {clickData.daily.map((d) => {
                        const max = Math.max(...clickData.daily.map(dd => dd.count), 1);
                        const h = (d.count / max) * 100;
                        return (
                          <div key={d.date} className="flex-1 flex flex-col items-center gap-1 min-w-0">
                            <div className="w-full flex flex-col justify-end" style={{ height: "80px" }}>
                              <div className="w-full rounded-t bg-indigo-500 min-h-[2px]" style={{ height: `${Math.max(h, 1)}%` }}
                                title={`${d.date}: ${d.count} ${t("点击")}`} />
                            </div>
                            <span className="text-[9px] text-faint truncate w-full text-center">{d.date.slice(5)}</span>
                          </div>
                        );
                      })}
                    </div>
                  </div>
                )}
                {/* Platforms */}
                {clickData.platforms.length > 0 && (
                  <div>
                    <p className="text-xs text-muted mb-2">{t("平台分布")}</p>
                    <div className="space-y-1.5">
                      {clickData.platforms.map((p) => {
                        const total = clickData.platforms.reduce((s, pp) => s + pp.count, 0);
                        const pct = total > 0 ? Math.round((p.count / total) * 100) : 0;
                        return (
                          <div key={p.platform} className="flex items-center gap-2 text-xs">
                            <span className="w-14 text-muted">{platformLabels[p.platform] || p.platform}</span>
                            <div className="flex-1 h-1.5 rounded-full bg-raised overflow-hidden">
                              <div className="h-full rounded-full bg-indigo-500" style={{ width: `${pct}%` }} />
                            </div>
                            <span className="w-10 text-right text-muted tabular-nums">{p.count}</span>
                          </div>
                        );
                      })}
                    </div>
                  </div>
                )}
                {clickData.daily.length === 0 && clickData.platforms.length === 0 && (
                  <p className="text-xs text-faint py-2">{t("暂无点击数据")}</p>
                )}
              </>
            ) : (
              <div className="flex items-center justify-center py-4">
                <div className="size-5 animate-spin rounded-full border-2 border-indigo-600 border-t-transparent" />
              </div>
            )}
          </div>
        )}

        {/* Edit form */}
        {editing && (
          <div className="p-6 border-b border-line bg-gray-50/50 space-y-4">
            <div>
              <label className="block text-xs font-medium text-muted mb-1">{t("目标 URL")}</label>
              <input type="url" value={editOriginalUrl} onChange={(e) => setEditOriginalUrl(e.target.value)}
                className="w-full rounded-lg border border-line px-3 py-2 text-sm focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 transition" />
            </div>
            <div>
              <label className="block text-xs font-medium text-muted mb-1">{t("描述")}</label>
              <textarea value={editDescription} onChange={(e) => setEditDescription(e.target.value)}
                className="w-full rounded-lg border border-line px-3 py-2 text-sm focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 transition" rows={2} />
            </div>

            {/* Folder + Workspace + Tags edit */}
            <div className="grid gap-4 sm:grid-cols-3">
              <div>
                <label className="block text-xs font-medium text-muted mb-1">{t("文件夹")}</label>
                <select
                  value={editFolderId ?? ""}
                  onChange={(e) => setEditFolderId(e.target.value ? Number(e.target.value) : null)}
                  className="w-full rounded-lg border border-line px-3 py-2 text-sm focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 transition bg-canvas"
                >
                  <option value="">{t("不分类")}</option>
                  {folders.map((f: { id: number; name: string }) => (
                    <option key={f.id} value={f.id}>{f.name}</option>
                  ))}
                </select>
              </div>
              {workspaces.length > 0 && (
                <div>
                  <label className="block text-xs font-medium text-muted mb-1">{t("工作区")}</label>
                  <select
                    value={editWorkspaceId ?? ""}
                    onChange={(e) => setEditWorkspaceId(e.target.value ? Number(e.target.value) : null)}
                    className="w-full rounded-lg border border-line px-3 py-2 text-sm focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 transition bg-canvas"
                  >
                    <option value="">{t("默认")}</option>
                    {workspaces.map((w: { id: number; name: string }) => (
                      <option key={w.id} value={w.id}>{w.name}</option>
                    ))}
                  </select>
                </div>
              )}
              <div>
                <label className="block text-xs font-medium text-muted mb-1">{t("标签")}</label>
                <select
                  value=""
                  onChange={(e) => {
                    const id = Number(e.target.value);
                    if (id && !editTagIds.includes(id)) {
                      setEditTagIds(prev => [...prev, id]);
                    }
                  }}
                  className="w-full rounded-lg border border-line px-3 py-2 text-sm focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 transition bg-canvas"
                >
                  <option value="">{t("添加标签...")}</option>
                  {allTags.filter((tag: { id: number }) => !editTagIds.includes(tag.id)).map((tag: { id: number; name: string }) => (
                    <option key={tag.id} value={tag.id}>{tag.name}</option>
                  ))}
                </select>
                {editTagIds.length > 0 && (
                  <div className="flex flex-wrap gap-1 mt-1.5">
                    {allTags.filter((tag: { id: number }) => editTagIds.includes(tag.id)).map((tag: { id: number; name: string; color: string }) => (
                      <span key={tag.id}
                        className="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full text-white"
                        style={{ backgroundColor: tag.color }}
                      >
                        {tag.name}
                        <button type="button" onClick={() => setEditTagIds(prev => prev.filter(id => id !== tag.id))}
                          className="hover:bg-white/20 rounded-full p-0.5 -mr-1">
                          <X className="size-2.5" />
                        </button>
                      </span>
                    ))}
                  </div>
                )}
              </div>
            </div>

            <div className="grid gap-4 sm:grid-cols-2">
              <div>
                <label className="block text-xs font-medium text-muted mb-1">{t("密码保护")}</label>
                <input type="text" value={editPassword} onChange={(e) => setEditPassword(e.target.value)}
                  className="w-full rounded-lg border border-line px-3 py-2 text-sm focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 transition"
                  placeholder={t("留空则不加密")} />
              </div>
              <div>
                <label className="block text-xs font-medium text-muted mb-1">{t("过期时间")}</label>
                <input type="datetime-local" value={editExpiresAt} onChange={(e) => setEditExpiresAt(e.target.value)}
                  className="w-full rounded-lg border border-line px-3 py-2 text-sm focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 transition" />
              </div>
            </div>

            {/* Advanced toggle */}
            <button type="button" onClick={() => setEditShowAdvanced(!editShowAdvanced)}
              className="text-xs text-brand-ink hover:text-brand font-medium">
              {editShowAdvanced ? t("隐藏高级选项 ▲") : t("展开高级选项 ▼")}
            </button>

            {editShowAdvanced && (
              <div className="space-y-4 pt-2">
                <div>
                  <label className="block text-xs font-medium text-muted mb-1">{t("短码")}</label>
                  <input type="text" value={editShortCode} onChange={(e) => setEditShortCode(e.target.value.replace(/[^a-zA-Z0-9_-]/g, ""))}
                    className="w-full rounded-lg border border-line px-3 py-2 text-sm font-mono focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 transition"
                    placeholder={t("修改短码")} />
                </div>
                <div>
                  <label className="block text-xs font-medium text-muted mb-1">{t("域名")}</label>
                  <select value={editDomain} onChange={(e) => setEditDomain(e.target.value)}
                    className="w-full rounded-lg border border-line px-3 py-2 text-sm focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 transition bg-canvas">
                    <option value="">{t("kada.click（默认）")}</option>
                    {verifiedDomains.map((d: { id: number; name: string }) => (
                      <option key={d.id} value={d.name}>{d.name}</option>
                    ))}
                  </select>
                </div>
                <div className="grid gap-4 sm:grid-cols-2">
                  {[["utm_source", t("来源"), editUtmSource, setEditUtmSource],
                    ["utm_medium", t("媒介"), editUtmMedium, setEditUtmMedium],
                    ["utm_campaign", t("活动"), editUtmCampaign, setEditUtmCampaign],
                    ["utm_term", t("关键词"), editUtmTerm, setEditUtmTerm],
                    ["utm_content", t("内容"), editUtmContent, setEditUtmContent],
                  ].map(([key, label, value, setter]) => (
                    <div key={key as string}>
                      <label className="block text-xs font-medium text-muted mb-1">UTM {label as string}</label>
                      <input type="text" value={value as string} onChange={(e) => (setter as (v: string) => void)(e.target.value)}
                        className="w-full rounded-lg border border-line px-3 py-2 text-sm font-mono focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 transition" />
                    </div>
                  ))}
                </div>
                <div className="grid gap-4 sm:grid-cols-2">
                  <div>
                    <label className="block text-xs font-medium text-muted mb-1">{t("iOS 深度链接")}</label>
                    <input type="text" value={editIosUrl} onChange={(e) => setEditIosUrl(e.target.value)}
                      className="w-full rounded-lg border border-line px-3 py-2 text-sm font-mono focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 transition" />
                  </div>
                  <div>
                    <label className="block text-xs font-medium text-muted mb-1">{t("Android 深度链接")}</label>
                    <input type="text" value={editAndroidUrl} onChange={(e) => setEditAndroidUrl(e.target.value)}
                      className="w-full rounded-lg border border-line px-3 py-2 text-sm font-mono focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 transition" />
                  </div>
                </div>
              </div>
            )}
          </div>
        )}

        {/* Details */}
        <div className="p-6 space-y-2">
          {[
            { label: t("短码"), value: link.short_code, mono: true },
            { label: t("域名"), value: link.domain, icon: Globe },
            { label: t("描述"), value: link.description },
            { label: t("创建时间"), value: new Date(link.created_at).toLocaleString(dateLocale) },
            { label: t("更新时间"), value: new Date(link.updated_at).toLocaleString(dateLocale) },
            { label: t("过期时间"), value: link.expires_at ? new Date(link.expires_at).toLocaleString(dateLocale) : t("永不过期"), icon: Clock },
            { label: t("密码保护"), value: link.has_password ? t("已设置") : t("未设置"), icon: Shield },
          ].filter(({ value }) => value).map(({ label, value, icon: Icon, mono }) => (
            <div key={label} className="flex items-center justify-between py-1.5 text-sm">
              <span className="text-muted flex items-center gap-1.5">
                {Icon && <Icon className="size-3.5" />}{label}
              </span>
              <span className={`text-strong ${mono ? "font-mono" : ""}`}>{value}</span>
            </div>
          ))}

          {/* UTM */}
          {[link.utm_source, link.utm_medium, link.utm_campaign, link.utm_term, link.utm_content].some(Boolean) && (
            <div className="mt-3 pt-3 border-t">
              <p className="text-xs font-medium text-muted mb-2 flex items-center gap-1.5">
                <Tags className="size-3.5" />{t("UTM 参数")}
              </p>
              {[
                [t("来源"), link.utm_source], [t("媒介"), link.utm_medium], [t("活动"), link.utm_campaign],
                [t("关键词"), link.utm_term], [t("内容"), link.utm_content],
              ].filter(([, v]) => v).map(([k, v]) => (
                <div key={k} className="flex items-center justify-between py-1 text-sm">
                  <span className="text-faint text-xs">{k}</span>
                  <code className="text-xs text-body font-mono">{v}</code>
                </div>
              ))}
            </div>
          )}

          {/* Deep links */}
          {(link.ios_url || link.android_url) && (
            <div className="mt-3 pt-3 border-t">
              <p className="text-xs font-medium text-muted mb-2 flex items-center gap-1.5">
                <Smartphone className="size-3.5" />{t("深度链接")}
              </p>
              {link.ios_url && <div className="flex justify-between py-1 text-sm"><span className="text-faint text-xs">iOS</span><code className="text-xs text-body">{link.ios_url}</code></div>}
              {link.android_url && <div className="flex justify-between py-1 text-sm"><span className="text-faint text-xs">Android</span><code className="text-xs text-body">{link.android_url}</code></div>}
            </div>
          )}

          {/* Target URL */}
          <div className="mt-3 pt-3 border-t">
            <p className="text-xs font-medium text-muted mb-1">{t("目标 URL")}</p>
            <a href={safeHref(link.original_url)} target="_blank" rel="noopener noreferrer" className="text-brand-ink text-sm break-all hover:underline">
              {link.original_url}
            </a>
          </div>
        </div>
      </div>

      {/* QR Code Modal */}
      {showQR && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm"
          onClick={() => { setShowQR(false); setQrURL(null); }}>
          <div className="bg-canvas rounded-2xl shadow-xl p-6 max-w-sm w-full mx-4" onClick={(e) => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-4">
              <h3 className="font-semibold text-strong">{t("二维码")}</h3>
              <button onClick={() => { setShowQR(false); setQrURL(null); }} className="p-1.5 rounded-lg text-faint hover:bg-muted-surface transition"><X className="size-4" /></button>
            </div>
            <div className="flex flex-col items-center gap-4">
              <div className="bg-canvas border border-line rounded-xl p-3">
                {/* A data: URL built locally by qrcode's canvas: no remote host to allowlist and nothing
                    for a server to fetch: the image is inlined, so nothing is requested. */}
                {qrURL ? <img src={qrURL} alt="QR" width={224} height={224} />
                  : <div className="size-56 flex items-center justify-center">
                    <div className="size-8 animate-spin rounded-full border-2 border-indigo-600 border-t-transparent" /></div>}
              </div>
              <p className="text-sm font-mono text-muted break-all text-center">{link.short_url}</p>
              {qrURL && (
                <a href={qrURL} download={`kada-${link.short_code}.png`}
                  className="inline-flex items-center gap-1.5 rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 transition">
                  <Download className="size-3.5" />{t("下载 PNG")}
                </a>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
