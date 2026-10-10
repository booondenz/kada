import { useState } from "react";
import { useNavigate } from "react-router";
import { User, Phone, LogOut, Key, Copy, Trash2, Plus, Building2, Globe, Monitor, Moon, Sun } from "lucide-react";
import { toast } from "sonner";
import useSWR from "swr";
import { tokensAPI, workspacesAPI } from "@/lib/api";
import { getToken, getUser, removeToken } from "@/lib/auth";
import { useT, useI18n } from "@/lib/i18n";
import { useTheme } from "@/lib/theme";

export default function SettingsPage() {
  const navigate = useNavigate();
  const token = getToken();
  const savedUser = getUser();
  const t = useT();
  const { locale } = useI18n();
  const localeTag = locale === "en" ? "en-US" : "zh-CN";
  const { theme, followsSystem, setTheme, useSystemTheme } = useTheme();

  // Signing out is here rather than in the top bar: with phone-only sign-in there is no profile to hang a
  // menu on, and the menu that used to hold this button had no other entry to justify itself.
  const handleLogout = () => {
    removeToken();
    toast.success(t("已退出"));
    navigate("/login");
  };

  // API Tokens
  const { data: tokenData, mutate: mutateTokens } = useSWR(
    token ? "api-tokens" : null,
    () => tokensAPI.list(token!)
  );
  const tokens = tokenData?.tokens ?? [];
  const [newTokenName, setNewTokenName] = useState("");
  const [creating, setCreating] = useState(false);
  const [newToken, setNewToken] = useState<string | null>(null);

  const handleCreateToken = async () => {
    if (!token || !newTokenName.trim()) return;
    setCreating(true);
    try {
      const data = await tokensAPI.create(token, newTokenName.trim());
      setNewToken(data.token);
      setNewTokenName("");
      mutateTokens();
      toast.success(t("Token 创建成功"));
    } catch {
      toast.error(t("创建失败"));
    } finally {
      setCreating(false);
    }
  };

  const handleDeleteToken = async (id: number) => {
    if (!token) return;
    try {
      await tokensAPI.delete(token, id);
      mutateTokens();
      toast.success(t("Token 已删除"));
    } catch {
      toast.error(t("删除失败"));
    }
  };

  // Workspaces
  const { data: workspaceData, mutate: mutateWorkspaces } = useSWR(
    token ? "workspaces" : null,
    () => workspacesAPI.list(token!)
  );
  const workspaces = workspaceData?.workspaces ?? [];
  const [newWorkspaceName, setNewWorkspaceName] = useState("");
  const [newWorkspaceSlug, setNewWorkspaceSlug] = useState("");
  const [creatingWs, setCreatingWs] = useState(false);

  const handleCreateWorkspace = async () => {
    if (!token || !newWorkspaceName.trim() || !newWorkspaceSlug.trim()) return;
    setCreatingWs(true);
    try {
      await workspacesAPI.create(token, newWorkspaceName.trim(), newWorkspaceSlug.trim().toLowerCase());
      setNewWorkspaceName("");
      setNewWorkspaceSlug("");
      mutateWorkspaces();
      toast.success(t("工作区已创建"));
    } catch {
      toast.error(t("创建失败"));
    } finally {
      setCreatingWs(false);
    }
  };

  const handleDeleteWorkspace = async (id: number) => {
    if (!token) return;
    try {
      await workspacesAPI.delete(token, id);
      mutateWorkspaces();
      toast.success(t("工作区已删除"));
    } catch {
      toast.error(t("删除失败"));
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-strong">{t("设置")}</h1>
        <p className="text-sm text-muted mt-1">{t("管理你的个人信息")}</p>
      </div>

      {/* Profile Card. The phone number and nothing else: it is the account - the sign-in method, the
          unique key, and the only field the API still publishes. The name, the email and the avatar that
          stood here were editable fields nothing reads any more (PATCH /me went with them), and a field
          that can be edited but changes nothing is worse than no field at all. */}
      <div className="rounded-xl border border-line bg-canvas shadow-sm overflow-hidden">
        <div className="border-b border-line px-6 py-4">
          <div className="flex items-center gap-2">
            <User className="size-4 text-muted" />
            <h2 className="font-semibold text-strong">{t("个人信息")}</h2>
          </div>
        </div>

        <div className="px-6 py-5">
          <div className="flex items-center gap-3 py-2">
            <Phone className="size-4 text-faint shrink-0" />
            <div>
              <p className="text-xs text-muted">{t("手机号")}</p>
              <p className="text-sm text-strong">
                {savedUser?.phone || <span className="text-faint italic">{t("未绑定")}</span>}
              </p>
            </div>
          </div>
        </div>
      </div>

      {/* Appearance Card. The default follows the browser's preference; choosing a theme pins it and
          "Follow system" hands control back. */}
      <div className="rounded-xl border border-line bg-canvas shadow-sm overflow-hidden">
        <div className="border-b border-line px-6 py-4">
          <div className="flex items-center gap-2">
            <Sun className="size-4 text-muted" />
            <h2 className="font-semibold text-strong">{t("外观")}</h2>
          </div>
        </div>

        <div className="px-6 py-5 space-y-4">
          <p className="text-sm text-muted">
            {followsSystem
              ? t("当前跟随浏览器的深浅色设置。")
              : t("已手动选择，不再跟随浏览器设置。")}
          </p>

          <div className="flex flex-wrap gap-2">
            <ThemeOption
              active={followsSystem}
              icon={<Monitor className="size-4" />}
              label={t("跟随系统")}
              onClick={useSystemTheme}
            />
            <ThemeOption
              active={!followsSystem && theme === "light"}
              icon={<Sun className="size-4" />}
              label={t("浅色")}
              onClick={() => setTheme("light")}
            />
            <ThemeOption
              active={!followsSystem && theme === "dark"}
              icon={<Moon className="size-4" />}
              label={t("深色")}
              onClick={() => setTheme("dark")}
            />
          </div>
        </div>
      </div>

      {/* API Tokens Card */}
      <div className="rounded-xl border border-line bg-canvas shadow-sm overflow-hidden">
        <div className="border-b border-line px-6 py-4">
          <div className="flex items-center gap-2">
            <Key className="size-4 text-muted" />
            <h2 className="font-semibold text-strong">API Tokens</h2>
          </div>
        </div>

        <div className="px-6 py-5 space-y-4">
          {/* New token shown once */}
          {newToken && (
            <div className="rounded-xl bg-amber-50 border border-amber-200 p-4">
              <p className="text-sm font-medium text-amber-800 mb-2">{t("新 Token 已创建，仅显示一次，请立即复制保存：")}</p>
              <div className="flex items-center gap-2">
                <code className="flex-1 bg-canvas rounded-lg px-3 py-2 text-sm font-mono text-strong border border-amber-200 break-all">
                  {newToken}
                </code>
                <button
                  onClick={() => { navigator.clipboard.writeText(newToken); toast.success(t("已复制")); }}
                  className="p-2 rounded-lg text-amber-600 hover:bg-amber-100 transition shrink-0"
                >
                  <Copy className="size-4" />
                </button>
              </div>
              <button onClick={() => setNewToken(null)}
                className="mt-2 text-xs text-amber-600 hover:text-amber-700">
                {t("我已保存，关闭提示")}
              </button>
            </div>
          )}

          {/* Create new */}
          <div className="flex items-center gap-2">
            <input
              type="text" value={newTokenName}
              onChange={(e) => setNewTokenName(e.target.value)}
              className="flex-1 rounded-lg border border-line px-3 py-2 text-sm focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 transition"
              placeholder={t("Token 名称，如：生产环境、本地开发")}
              onKeyDown={(e) => { if (e.key === "Enter") handleCreateToken(); }}
            />
            <button
              onClick={handleCreateToken}
              disabled={creating || !newTokenName.trim()}
              className="inline-flex items-center gap-1.5 rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-40 disabled:cursor-not-allowed transition shrink-0"
            >
              <Plus className="size-3.5" /> {t("创建")}
            </button>
          </div>

          {/* Token list */}
          {tokens.length > 0 ? (
            <div className="space-y-1">
              {tokens.map((tok: { id: number; name: string; last_used?: string; created_at: string }) => (
                <div key={tok.id} className="flex items-center justify-between py-2 px-3 rounded-lg hover:bg-gray-50 transition">
                  <div>
                    <p className="text-sm font-medium text-strong">{tok.name}</p>
                    <p className="text-xs text-faint">
                      {t("创建于 {date}", { date: new Date(tok.created_at).toLocaleDateString(localeTag) })}
                      {tok.last_used ? t(" · 最近使用 {date}", { date: new Date(tok.last_used).toLocaleDateString(localeTag) }) : t(" · 从未使用")}
                    </p>
                  </div>
                  <button
                    onClick={() => handleDeleteToken(tok.id)}
                    className="p-1.5 rounded-lg text-faint hover:text-red-500 hover:bg-red-50 transition"
                  >
                    <Trash2 className="size-3.5" />
                  </button>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-sm text-faint py-2">{t("暂无 API Token，创建一个用于外部程序调用 API")}</p>
          )}
        </div>
      </div>

      {/* Workspaces Card */}
      <div className="rounded-xl border border-line bg-canvas shadow-sm overflow-hidden">
        <div className="border-b border-line px-6 py-4">
          <div className="flex items-center gap-2">
            <Building2 className="size-4 text-muted" />
            <h2 className="font-semibold text-strong">{t("工作区")}</h2>
          </div>
        </div>

        <div className="px-6 py-5 space-y-4">
          {/* Create workspace */}
          <div className="flex items-center gap-2">
            <input
              type="text" value={newWorkspaceName}
              onChange={(e) => setNewWorkspaceName(e.target.value)}
              className="flex-1 rounded-lg border border-line px-3 py-2 text-sm focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 transition"
              placeholder={t("工作区名称，如：个人项目")}
              onKeyDown={(e) => { if (e.key === "Enter") handleCreateWorkspace(); }}
            />
            <input
              type="text" value={newWorkspaceSlug}
              onChange={(e) => setNewWorkspaceSlug(e.target.value.replace(/[^a-z0-9-]/g, ""))}
              className="w-36 rounded-lg border border-line px-3 py-2 text-sm focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 transition font-mono"
              placeholder="slug"
              onKeyDown={(e) => { if (e.key === "Enter") handleCreateWorkspace(); }}
            />
            <button
              onClick={handleCreateWorkspace}
              disabled={creatingWs || !newWorkspaceName.trim() || !newWorkspaceSlug.trim()}
              className="inline-flex items-center gap-1.5 rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-40 disabled:cursor-not-allowed transition shrink-0"
            >
              <Plus className="size-3.5" /> {t("创建")}
            </button>
          </div>

          {/* Workspace list */}
          {workspaces.length > 0 ? (
            <div className="space-y-1">
              {workspaces.map((w: { id: number; name: string; slug: string; link_count: number; created_at: string }) => (
                <div key={w.id} className="flex items-center justify-between py-2 px-3 rounded-lg hover:bg-gray-50 transition">
                  <div className="flex items-center gap-2">
                    <Globe className="size-4 text-faint" />
                    <div>
                      <p className="text-sm font-medium text-strong">{w.name}</p>
                      <p className="text-xs text-faint">
                        {t("{slug} · {count} 条链接 · {date}", { slug: w.slug, count: w.link_count ?? 0, date: new Date(w.created_at).toLocaleDateString(localeTag) })}
                      </p>
                    </div>
                  </div>
                  <button
                    onClick={() => handleDeleteWorkspace(w.id)}
                    className="p-1.5 rounded-lg text-faint hover:text-red-500 hover:bg-red-50 transition"
                  >
                    <Trash2 className="size-3.5" />
                  </button>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-sm text-faint py-2">{t("暂无工作区，创建一个来组织你的链接")}</p>
          )}
        </div>
      </div>

      {/* Sign-out Card. It replaced the account-information card, whose two rows said nothing worth a
          card: "registered at" was never sent by the API (the field is not in the payload, so it always
          rendered a dash) and "sign-in method" is the same sentence for every account on this page. */}
      <div className="rounded-xl border border-line bg-canvas shadow-sm overflow-hidden">
        <div className="border-b border-line px-6 py-4">
          <div className="flex items-center gap-2">
            <LogOut className="size-4 text-muted" />
            <h2 className="font-semibold text-strong">{t("退出登录")}</h2>
          </div>
        </div>
        <div className="px-6 py-5 space-y-3">
          <p className="text-sm text-muted">{t("退出后需要重新用手机号验证码登录。")}</p>
          <button
            onClick={handleLogout}
            className="inline-flex items-center gap-1.5 rounded-lg border border-red-200 bg-red-50 px-4 py-2 text-sm font-medium text-red-600 transition hover:bg-red-100"
          >
            <LogOut className="size-3.5" />
            {t("退出登录")}
          </button>
        </div>
      </div>
    </div>
  );
}

/** One of the three appearance choices. */
function ThemeOption({
  active,
  icon,
  label,
  onClick,
}: {
  active: boolean;
  icon: React.ReactNode;
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={`inline-flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-sm font-medium transition ${
        active
          ? "border-brand bg-brand-soft text-brand-ink"
          : "border-line text-muted hover:bg-muted-surface hover:text-strong"
      }`}
    >
      {icon}
      {label}
    </button>
  );
}
