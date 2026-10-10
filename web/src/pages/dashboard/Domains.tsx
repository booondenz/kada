import { useState } from "react";
import { Globe, Plus, Trash2, Check, X, ShieldCheck, ShieldAlert, Info } from "lucide-react";
import useSWR from "swr";
import { toast } from "sonner";
import { domainsAPI } from "@/lib/api";
import { getToken } from "@/lib/auth";
import { useT } from "@/lib/i18n";
import { useI18n } from "@/lib/i18n/context";

interface DomainItem {
  id: number;
  name: string;
  verified: boolean;
  verified_at: string | null;
  created_at: string;
}

export default function DomainsPage() {
  const token = getToken();
  const t = useT();
  const { locale } = useI18n();
  const localeTag = locale === "en" ? "en-US" : "zh-CN";

  const { data, error, isLoading, mutate } = useSWR(
    token ? "domains" : null,
    () => domainsAPI.list(token!)
  );

  const domains: DomainItem[] = data?.domains ?? [];

  const [newDomain, setNewDomain] = useState("");
  const [creating, setCreating] = useState(false);
  const [verifyingId, setVerifyingId] = useState<number | null>(null);
  const [deletingId, setDeletingId] = useState<number | null>(null);
  const [showGuide, setShowGuide] = useState(false);

  const handleCreate = async () => {
    if (!token || !newDomain.trim()) return;
    setCreating(true);
    try {
      await domainsAPI.create(token, newDomain.trim().toLowerCase());
      setNewDomain("");
      mutate();
      toast.success(t("域名已添加"));
    } catch {
      toast.error(t("添加失败，域名可能已存在"));
    } finally {
      setCreating(false);
    }
  };

  const handleVerify = async (id: number) => {
    if (!token) return;
    setVerifyingId(id);
    try {
      await domainsAPI.verify(token, id);
      mutate();
      toast.success(t("域名已验证"));
    } catch {
      toast.error(t("验证失败"));
    } finally {
      setVerifyingId(null);
    }
  };

  const handleDelete = async (id: number) => {
    if (!token) return;
    try {
      await domainsAPI.delete(token, id);
      setDeletingId(null);
      mutate();
      toast.success(t("已删除"));
    } catch {
      toast.error(t("删除失败"));
    }
  };

  return (
    <div className="space-y-5">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-strong">{t("域名")}</h1>
          <p className="text-sm text-muted mt-1">{t("管理你的自定义域名")}</p>
        </div>
        <button
          onClick={() => setShowGuide(!showGuide)}
          className="inline-flex items-center gap-1.5 text-sm text-brand-ink hover:text-brand transition"
        >
          <Info className="size-4" />
          {t("如何配置？")}
        </button>
      </div>

      {/* DNS Setup Guide */}
      {showGuide && (
        <div className="rounded-xl border border-line bg-brand-soft/50 p-5">
          <h3 className="font-semibold text-indigo-900 text-sm mb-3">{t("配置自定义域名")}</h3>
          <ol className="space-y-2 text-sm text-indigo-800">
            <li className="flex gap-2">
              <span className="font-medium shrink-0">1.</span>
              <span>{t("在域名 DNS 管理中添加一条 ")}<code className="bg-brand-soft px-1 rounded text-xs font-mono">CNAME</code>{t(" 记录，指向 ")}<code className="bg-brand-soft px-1 rounded text-xs font-mono">47.122.124.48</code></span>
            </li>
            <li className="flex gap-2">
              <span className="font-medium shrink-0">2.</span>
              <span>{t("在下方添加你的域名（例如 ")}<code className="bg-brand-soft px-1 rounded text-xs font-mono">s.example.com</code>{t("）")}</span>
            </li>
            <li className="flex gap-2">
              <span className="font-medium shrink-0">3.</span>
              <span>{t("DNS 生效后点击「验证」确认域名所有权")}</span>
            </li>
            <li className="flex gap-2">
              <span className="font-medium shrink-0">4.</span>
              <span>{t("创建链接时即可选择已验证的域名")}</span>
            </li>
          </ol>
        </div>
      )}

      {/* Add domain */}
      <div className="rounded-xl border border-line bg-canvas shadow-sm p-4">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            handleCreate();
          }}
          className="flex items-center gap-3"
        >
          <div className="flex size-9 items-center justify-center rounded-lg bg-sky-50 shrink-0">
            <Globe className="size-4 text-sky-600" />
          </div>
          <input
            type="text"
            value={newDomain}
            onChange={(e) => setNewDomain(e.target.value)}
            placeholder="s.example.com"
            className="flex-1 border-none bg-transparent text-sm text-strong placeholder:text-faint focus:outline-none font-mono"
          />
          <button
            type="submit"
            disabled={!newDomain.trim() || creating}
            className="inline-flex items-center gap-1.5 rounded-lg bg-indigo-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-indigo-700 disabled:opacity-40 disabled:cursor-not-allowed transition shrink-0"
          >
            <Plus className="size-3.5" />
            {t("添加")}
          </button>
        </form>
      </div>

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
        <div className="space-y-1.5">
          {Array.from({ length: 3 }).map((_, i) => (
            <div
              key={i}
              className="animate-pulse rounded-xl border border-line bg-canvas p-4 flex items-center gap-4"
            >
              <div className="size-9 rounded-lg bg-muted-surface" />
              <div className="flex-1 space-y-2">
                <div className="h-4 w-40 rounded bg-muted-surface" />
                <div className="h-3 w-24 rounded bg-gray-50" />
              </div>
            </div>
          ))}
        </div>
      ) : domains.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-16 text-center">
          <div className="flex size-14 items-center justify-center rounded-2xl bg-muted-surface mb-4">
            <Globe className="size-7 text-faint" />
          </div>
          <h3 className="text-lg font-semibold text-strong">{t("还没有自定义域名")}</h3>
          <p className="mt-1 text-sm text-muted max-w-sm">
            {t("绑定你自己的域名来创建品牌短链接，在上方输入框添加")}
          </p>
        </div>
      ) : (
        <div className="space-y-1.5">
          {domains.map((d) => (
            <div
              key={d.id}
              className="group rounded-xl border border-line bg-canvas p-4 flex items-center gap-4 hover:border-line hover:shadow-sm transition"
            >
              <div className="flex size-9 items-center justify-center rounded-lg bg-sky-50 shrink-0">
                <Globe className="size-4 text-sky-600" />
              </div>

              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2">
                  <p className="text-sm font-mono font-medium text-strong truncate">
                    {d.name}
                  </p>
                  {d.verified ? (
                    <span className="inline-flex items-center gap-1 rounded-full bg-emerald-50 px-2 py-0.5 text-xs font-medium text-emerald-700">
                      <ShieldCheck className="size-3" />
                      {t("已验证")}
                    </span>
                  ) : (
                    <span className="inline-flex items-center gap-1 rounded-full bg-amber-50 px-2 py-0.5 text-xs font-medium text-amber-700">
                      <ShieldAlert className="size-3" />
                      {t("待验证")}
                    </span>
                  )}
                </div>
                <p className="text-xs text-faint mt-0.5">
                  {d.verified
                    ? `${t("验证于 ")}${new Date(d.verified_at!).toLocaleDateString(localeTag)}`
                    : t("请将域名 CNAME 解析到服务器并点击验证")}
                </p>
              </div>

              <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition">
                {!d.verified && (
                  <button
                    onClick={() => handleVerify(d.id)}
                    disabled={verifyingId === d.id}
                    className="inline-flex items-center gap-1 rounded-lg px-2.5 py-1.5 text-xs font-medium text-amber-700 bg-amber-50 hover:bg-amber-100 disabled:opacity-50 transition"
                  >
                    {verifyingId === d.id ? t("验证中...") : t("验证")}
                  </button>
                )}

                {deletingId === d.id ? (
                  <div className="flex items-center gap-1.5 bg-red-50 px-2 py-1 rounded-lg">
                    <span className="text-xs text-red-600">{t("删除？")}</span>
                    <button
                      onClick={() => handleDelete(d.id)}
                      className="p-1 rounded text-red-600 hover:bg-red-100 transition"
                    >
                      <Check className="size-3" />
                    </button>
                    <button
                      onClick={() => setDeletingId(null)}
                      className="p-1 rounded text-faint hover:bg-raised transition"
                    >
                      <X className="size-3" />
                    </button>
                  </div>
                ) : (
                  <button
                    onClick={() => setDeletingId(d.id)}
                    className="p-1.5 rounded-lg text-faint hover:text-red-500 hover:bg-red-50 transition"
                  >
                    <Trash2 className="size-3.5" />
                  </button>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
