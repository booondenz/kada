import { useState } from "react";
import { ArrowRightLeft, Plus, Trash2, Check, X, Copy } from "lucide-react";
import useSWR from "swr";
import { toast } from "sonner";
import { utmAPI } from "@/lib/api";
import { getToken } from "@/lib/auth";
import { useT } from "@/lib/i18n";

interface UTMTemplate {
  id: number;
  name: string;
  utm_source: string | null;
  utm_medium: string | null;
  utm_campaign: string | null;
  utm_term: string | null;
  utm_content: string | null;
}

export default function UTMPage() {
  const token = getToken();
  const t = useT();

  const UTM_FIELDS = [
    { key: "utm_source", label: t("来源 (source)"), placeholder: "wechat, qq, weibo..." },
    { key: "utm_medium", label: t("媒介 (medium)"), placeholder: "social, cpc, email..." },
    { key: "utm_campaign", label: t("活动 (campaign)"), placeholder: "spring-sale, launch..." },
    { key: "utm_term", label: t("关键词 (term)"), placeholder: "running+shoes..." },
    { key: "utm_content", label: t("内容 (content)"), placeholder: "banner-a, button-1..." },
  ];

  const { data, error, isLoading, mutate } = useSWR(
    token ? "utm-templates" : null,
    () => utmAPI.list(token!)
  );

  const templates: UTMTemplate[] = data?.templates ?? [];

  const [showForm, setShowForm] = useState(false);
  const [formName, setFormName] = useState("");
  const [formValues, setFormValues] = useState<Record<string, string>>({});
  const [creating, setCreating] = useState(false);
  const [deletingId, setDeletingId] = useState<number | null>(null);
  const [expandedId, setExpandedId] = useState<number | null>(null);

  const resetForm = () => {
    setFormName("");
    setFormValues({});
    setShowForm(false);
  };

  const handleCreate = async () => {
    if (!token || !formName.trim()) return;
    setCreating(true);
    try {
      await utmAPI.create(token, {
        name: formName.trim(),
        ...(formValues.utm_source && { utm_source: formValues.utm_source }),
        ...(formValues.utm_medium && { utm_medium: formValues.utm_medium }),
        ...(formValues.utm_campaign && { utm_campaign: formValues.utm_campaign }),
        ...(formValues.utm_term && { utm_term: formValues.utm_term }),
        ...(formValues.utm_content && { utm_content: formValues.utm_content }),
      });
      resetForm();
      mutate();
      toast.success(t("模板已创建"));
    } catch {
      toast.error(t("创建失败"));
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (id: number) => {
    if (!token) return;
    try {
      await utmAPI.delete(token, id);
      setDeletingId(null);
      mutate();
      toast.success(t("已删除"));
    } catch {
      toast.error(t("删除失败"));
    }
  };

  const buildPreviewURL = (t: UTMTemplate) => {
    const params = new URLSearchParams();
    if (t.utm_source) params.set("utm_source", t.utm_source);
    if (t.utm_medium) params.set("utm_medium", t.utm_medium);
    if (t.utm_campaign) params.set("utm_campaign", t.utm_campaign);
    if (t.utm_term) params.set("utm_term", t.utm_term);
    if (t.utm_content) params.set("utm_content", t.utm_content);
    return `?${params.toString()}`;
  };

  return (
    <div className="space-y-5">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-strong">{t("UTM 模板")}</h1>
          <p className="text-sm text-muted mt-1">{t("管理 UTM 参数模板，创建链接时快速复用")}</p>
        </div>
        <button
          onClick={() => setShowForm(true)}
          className="inline-flex items-center gap-1.5 rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 transition shadow-sm"
        >
          <Plus className="size-4" />
          {t("新建模板")}
        </button>
      </div>

      {/* Create form modal-ish */}
      {showForm && (
        <div className="rounded-xl border border-line bg-canvas shadow-sm p-6">
          <div className="flex items-center justify-between mb-4">
            <h2 className="font-semibold text-strong">{t("新建 UTM 模板")}</h2>
            <button
              onClick={resetForm}
              className="p-1 rounded text-faint hover:text-muted transition"
            >
              <X className="size-5" />
            </button>
          </div>

          <div className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-body mb-1">
                {t("模板名称 *")}
              </label>
              <input
                type="text"
                value={formName}
                onChange={(e) => setFormName(e.target.value)}
                placeholder={t("春季推广活动")}
                className="w-full rounded-lg border border-line px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
              />
            </div>

            <div className="grid gap-4 sm:grid-cols-2">
              {UTM_FIELDS.map(({ key, label, placeholder }) => (
                <div key={key}>
                  <label className="block text-sm font-medium text-body mb-1">
                    {label}
                  </label>
                  <input
                    type="text"
                    value={formValues[key] || ""}
                    onChange={(e) =>
                      setFormValues((prev) => ({ ...prev, [key]: e.target.value }))
                    }
                    placeholder={placeholder}
                    className="w-full rounded-lg border border-line px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent font-mono"
                  />
                </div>
              ))}
            </div>

            <div className="flex items-center gap-3 pt-2">
              <button
                onClick={handleCreate}
                disabled={!formName.trim() || creating}
                className="inline-flex items-center gap-1.5 rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-40 disabled:cursor-not-allowed transition"
              >
                {creating ? t("创建中...") : t("创建模板")}
              </button>
              <button
                onClick={resetForm}
                className="text-sm text-muted hover:text-body transition"
              >
                {t("取消")}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* List */}
      {error ? (
        <div className="flex flex-col items-center justify-center py-16 text-center">
          <div className="text-3xl mb-3">😞</div>
          <h3 className="text-lg font-semibold text-strong">{t("加载失败")}</h3>
          <p className="mt-1 text-sm text-muted">{t("请检查网络后重试")}</p>
          <button
            onClick={() => mutate()}
            className="mt-3 text-sm font-medium text-brand-ink hover:text-brand"
          >
            {t("重新加载")}
          </button>
        </div>
      ) : isLoading ? (
        <div className="space-y-2">
          {Array.from({ length: 3 }).map((_, i) => (
            <div
              key={i}
              className="animate-pulse rounded-xl border border-line bg-canvas p-5 space-y-3"
            >
              <div className="h-5 w-32 rounded bg-muted-surface" />
              <div className="h-4 w-64 rounded bg-gray-50" />
            </div>
          ))}
        </div>
      ) : templates.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-16 text-center">
          <div className="flex size-14 items-center justify-center rounded-2xl bg-muted-surface mb-4">
            <ArrowRightLeft className="size-7 text-faint" />
          </div>
          <h3 className="text-lg font-semibold text-strong">{t("还没有 UTM 模板")}</h3>
          <p className="mt-1 text-sm text-muted max-w-sm">
            {t("保存常用的 UTM 参数组合，创建链接时一键应用，轻松追踪营销效果")}
          </p>
          <button
            onClick={() => setShowForm(true)}
            className="mt-5 inline-flex items-center gap-2 rounded-lg bg-indigo-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-indigo-700 transition shadow-sm"
          >
            <Plus className="size-4" />
            {t("创建第一个模板")}
          </button>
        </div>
      ) : (
        <div className="space-y-2">
          {templates.map((tpl) => {
            const fieldsFilled = [
              tpl.utm_source,
              tpl.utm_medium,
              tpl.utm_campaign,
              tpl.utm_term,
              tpl.utm_content,
            ].filter(Boolean).length;

            return (
              <div
                key={tpl.id}
                className="rounded-xl border border-line bg-canvas hover:border-line hover:shadow-sm transition"
              >
                <div
                  className="p-4 flex items-center gap-4 cursor-pointer"
                  onClick={() => setExpandedId(expandedId === tpl.id ? null : tpl.id)}
                >
                  <div className="flex size-9 items-center justify-center rounded-lg bg-violet-50 shrink-0">
                    <ArrowRightLeft className="size-4 text-violet-600" />
                  </div>

                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <p className="text-sm font-medium text-strong">{tpl.name}</p>
                      <span className="text-xs text-faint">
                        {t("{fieldsFilled} 个参数", { fieldsFilled })}
                      </span>
                    </div>
                    <p className="text-xs text-faint font-mono truncate mt-0.5">
                      {buildPreviewURL(tpl)}
                    </p>
                  </div>

                  <div className="flex items-center gap-1">
                    {/* Copy URL */}
                    <button
                      onClick={(e) => {
                        e.stopPropagation();
                        navigator.clipboard.writeText(buildPreviewURL(tpl));
                        toast.success(t("已复制参数到剪贴板"));
                      }}
                      className="p-1.5 rounded-lg text-faint hover:text-muted hover:bg-muted-surface transition"
                      title={t("复制参数")}
                    >
                      <Copy className="size-3.5" />
                    </button>

                    {/* Delete */}
                    {deletingId === tpl.id ? (
                      <div className="flex items-center gap-1.5 bg-red-50 px-2 py-1 rounded-lg">
                        <span className="text-xs text-red-600">{t("删除？")}</span>
                        <button
                          onClick={(e) => {
                            e.stopPropagation();
                            handleDelete(tpl.id);
                          }}
                          className="p-1 rounded text-red-600 hover:bg-red-100 transition"
                        >
                          <Check className="size-3" />
                        </button>
                        <button
                          onClick={(e) => {
                            e.stopPropagation();
                            setDeletingId(null);
                          }}
                          className="p-1 rounded text-faint hover:bg-raised transition"
                        >
                          <X className="size-3" />
                        </button>
                      </div>
                    ) : (
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          setDeletingId(tpl.id);
                        }}
                        className="p-1.5 rounded-lg text-faint hover:text-red-500 hover:bg-red-50 transition"
                        title={t("删除")}
                      >
                        <Trash2 className="size-3.5" />
                      </button>
                    )}
                  </div>
                </div>

                {/* Expanded detail */}
                {expandedId === tpl.id && (
                  <div className="border-t border-line px-4 py-3">
                    <div className="grid gap-2 sm:grid-cols-2">
                      {[
                        { label: t("来源"), value: tpl.utm_source },
                        { label: t("媒介"), value: tpl.utm_medium },
                        { label: t("活动"), value: tpl.utm_campaign },
                        { label: t("关键词"), value: tpl.utm_term },
                        { label: t("内容"), value: tpl.utm_content },
                      ]
                        .filter((f) => f.value)
                        .map(({ label, value }) => (
                          <div
                            key={label}
                            className="flex items-center gap-2 text-sm"
                          >
                            <span className="text-faint shrink-0">{label}</span>
                            <code className="text-xs bg-gray-50 px-1.5 py-0.5 rounded text-body font-mono">
                              {value}
                            </code>
                          </div>
                        ))}
                    </div>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
