import { useEffect, useState } from "react";
import { Link } from "react-router";
import { User, Key, Copy, Trash2, Plus, Monitor, Moon, Sun } from "lucide-react";
import { toast } from "sonner";
import useSWR from "swr";
import { IcpNotice } from "@/components/IcpNotice";
import { PublicSecurityNotice } from "@/components/PublicSecurityNotice";
import { buttonClass } from "@/components/ui/Button";
import { inputClass } from "@/components/ui/Field";
import { authAPI, tokensAPI } from "@/lib/api";
import { getToken, getUser, setToken, setUser } from "@/lib/auth";
import { useT, useI18n } from "@/lib/i18n";
import { useTheme } from "@/lib/theme";

export default function SettingsPage() {
  const token = getToken();
  const savedUser = getUser();
  const t = useT();
  const { locale } = useI18n();
  const localeTag = locale === "en" ? "en-US" : "zh-CN";
  const { theme, followsSystem, setTheme, useSystemTheme } = useTheme();

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

  // ---- Claiming the account ----
  //
  // The challenge is fetched only while the account has no phone number: it is a puzzle that exists to
  // guard the one endpoint that spends money, and a page that already has an identity has nothing to ask.
  // Revalidation on focus is off for the same reason the old sign-in page turned it off - the challenge is
  // one-time, so replacing the image under somebody who is halfway through reading it is worse than stale.
  const phoneEmpty = !savedUser?.phone;
  const { data: challenge, mutate: refreshCaptcha } = useSWR(
    token && phoneEmpty ? "auth-captcha" : null,
    () => authAPI.captcha(),
    { revalidateOnFocus: false, dedupingInterval: 0 }
  );
  const [phone, setPhone] = useState("");
  const [captchaAnswer, setCaptchaAnswer] = useState("");
  const [code, setCode] = useState("");
  const [sending, setSending] = useState(false);
  const [signingIn, setSigningIn] = useState(false);
  const [signInError, setSignInError] = useState("");
  const [countdown, setCountdown] = useState(0);

  useEffect(() => {
    if (countdown <= 0) return;
    const timer = setTimeout(() => setCountdown((c) => c - 1), 1000);
    return () => clearTimeout(timer);
  }, [countdown]);

  const sendCode = async () => {
    if (!token || !challenge) return;
    if (phone.length !== 11) {
      setSignInError(t("请输入 11 位手机号"));
      return;
    }
    if (!captchaAnswer.trim()) {
      setSignInError(t("请先输入图形验证码"));
      return;
    }

    setSending(true);
    setSignInError("");
    try {
      await authAPI.sendCode(phone, challenge.captcha_id, captchaAnswer.trim());
      setCountdown(60);
      setCaptchaAnswer("");
      toast.success(t("验证码已发送"));
    } catch (err) {
      setSignInError(err instanceof Error ? err.message : t("发送失败"));
    } finally {
      // The server consumes the challenge whether the send worked or not, so the image on screen is spent
      // either way and asking for another one is not an optimisation.
      setSending(false);
      setCaptchaAnswer("");
      refreshCaptcha();
    }
  };

  const submitClaim = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!token) return;
    if (phone.length !== 11) {
      setSignInError(t("请输入 11 位手机号"));
      return;
    }
    if (!code.trim()) {
      setSignInError(t("验证码"));
      return;
    }

    setSigningIn(true);
    setSignInError("");
    try {
      const res = await authAPI.claim(token, phone, code.trim());
      setToken(res.token);
      setUser(res.user);
      // A reload rather than a re-render: the account is read once at boot (`ensureSession`), and every SWR
      // key on every page is scoped to the account that was in the token. Reloading is the honest way to say
      // "the account changed", and it costs one request.
      window.location.reload();
    } catch (err) {
      setSignInError(err instanceof Error ? err.message : t("登录失败"));
      setSigningIn(false);
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-strong">{t("用户")}</h1>
        <p className="text-sm text-muted mt-1">{t("管理你的个人信息")}</p>
      </div>

      {/* Profile Card. The identity and nothing else: a guest account has no phone number, so 游客 is the
          normal state here rather than an error - the row is where the claim step will write one, and where
          the payment flow will need an answer. The name, the email and the avatar that stood here were
          editable fields nothing reads any more (PATCH /me went with them), and a field that can be edited
          but changes nothing is worse than no field at all. */}
      <div className="rounded-xl border border-line bg-canvas shadow-sm overflow-hidden">
        <div className="border-b border-line px-6 py-4">
          <div className="flex items-center gap-2">
            <User className="size-4 text-muted" />
            <h2 className="font-semibold text-strong">{t("个人信息")}</h2>
          </div>
        </div>

        <div className="px-6 py-5 space-y-4">
          <div>
            <p className="text-xs text-muted">{t("身份")}</p>
            <p className="text-sm text-strong">
              {savedUser?.phone || <span className="text-muted">{t("游客")}</span>}
            </p>
          </div>

          {/* The way out of the guest account. It is on this page because this is where the identity is
              shown; a separate sign-in page would be a page whose only content is a form nobody could reach
              from the product. Both outcomes are the same form: a number nobody has used yet is written
              onto this account, a number that already has one signs the browser into that account. */}
          {!savedUser?.phone && (
            <form onSubmit={submitClaim} className="space-y-3 border-t border-line pt-4">
              <p className="text-xs text-muted">
                {t("用手机号登录，本机创建的链接会归到这个账号。")}
              </p>

              <div className="grid gap-3 sm:grid-cols-2">
                <input
                  type="tel"
                  inputMode="numeric"
                  autoComplete="tel"
                  maxLength={11}
                  value={phone}
                  onChange={(e) => setPhone(e.target.value.replace(/\D/g, ""))}
                  placeholder={t("手机号")}
                  aria-label={t("手机号")}
                  className={`${inputClass(Boolean(signInError) && phone.length !== 11)} sm:col-span-2`}
                />

                <div className="flex items-center gap-2">
                  {challenge?.image ? (
                    <img
                      src={challenge.image}
                      alt={t("图形验证码")}
                      title={t("图形验证码")}
                      onClick={() => refreshCaptcha()}
                      className="h-9 w-24 shrink-0 cursor-pointer rounded-md border border-line bg-canvas object-contain"
                    />
                  ) : (
                    <div className="h-9 w-24 shrink-0 animate-pulse rounded-md border border-line bg-muted-surface" />
                  )}
                  <input
                    type="text"
                    value={captchaAnswer}
                    onChange={(e) => setCaptchaAnswer(e.target.value)}
                    placeholder={t("图形验证码")}
                    aria-label={t("图形验证码")}
                    maxLength={8}
                    className={`${inputClass()} flex-1`}
                  />
                </div>

                <div className="flex items-center gap-2">
                  <input
                    type="text"
                    inputMode="numeric"
                    autoComplete="one-time-code"
                    value={code}
                    onChange={(e) => setCode(e.target.value.replace(/\D/g, ""))}
                    placeholder={t("验证码")}
                    aria-label={t("验证码")}
                    maxLength={6}
                    className={`${inputClass()} min-w-0 flex-1`}
                  />
                  <button
                    type="button"
                    onClick={sendCode}
                    disabled={sending || countdown > 0 || phone.length !== 11}
                    className={buttonClass({ variant: "secondary", className: "shrink-0" })}
                  >
                    {countdown > 0 ? `${countdown}s` : t("获取验证码")}
                  </button>
                  {/* The button ends the row rather than starting a new one: it submits the three fields to
                      its left, and a form of four rows for three inputs reads as a longer form than it is. */}
                  <button type="submit" disabled={signingIn} className={buttonClass({ className: "shrink-0" })}>
                    {t("登录")}
                  </button>
                </div>
              </div>

              {signInError && <p className="text-xs text-danger">{signInError}</p>}
            </form>
          )}
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

      {/* There is no workspaces card. It offered a second way to group links, next to folders, and the
          grouping it produced was a dropdown that behaved exactly like the folder one - the slug it made
          you invent was never used by a URL, and it granted access to nobody. The feature is gone from the
          API and the table is dropped by the migration. */}

      {/* There is no sign-out card any more. The account this page is about is a guest one - it exists
          because the browser opened the app, not because somebody signed in - so "sign out" would mean
          "throw this browser's links away", which is not an action to put one click away from the theme
          switch. It comes back, with the claim step that gives the account a phone number, and with the
          payment flow that needs one. */}

      {/* The version string and the filing number, at the bottom of the page. They used to sit in the
          sidebar beside the links, where nothing about them is a link: they are statements about the site,
          and this is the page that answers "what am I using". `/privacy` sits with them because the policy
          and the number are the same kind of statement - the filing office's link should not be the only
          way to find out what happens to your data. */}
      <footer className="border-t border-line pt-4">
        <p className="text-xs text-faint">Kada v0.2</p>
        <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-faint">
          <IcpNotice className="transition-colors hover:text-muted" />
          <PublicSecurityNotice className="transition-colors hover:text-muted" />
          <Link to="/privacy" className="transition-colors hover:text-muted">
            {t("隐私政策")}
          </Link>
          <Link to="/report" className="transition-colors hover:text-muted">
            {t("举报")}
          </Link>
        </div>
      </footer>
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
