import { useEffect, useState } from "react";
import { useLocation, useNavigate } from "react-router";
import { Link } from "react-router";
import { RefreshCw } from "lucide-react";
import { toast } from "sonner";
import useSWR from "swr";
import { authAPI } from "@/lib/api";
import { setToken, setUser } from "@/lib/auth";
import { AuthCard } from "@/components/auth/AuthCard";
import { Field, inputClass } from "@/components/ui/Field";
import { buttonClass } from "@/components/ui/Button";
import { TopBarControls } from "@/components/TopBarControls";
import { useT } from "@/lib/i18n";

/**
 * Signing in: a phone number and a password, with the SMS code kept for the two cases that still need it.
 *
 * Registration asks for a code and a password together, so the account is created once and every sign-in
 * after that costs nothing. Signing in with a code stays reachable - not as a shortcut, but because it is
 * the only way into an account that has no password: every account created before this flow existed has
 * none, and a number that has a password but has forgotten it has no other way back in.
 *
 * The graphical challenge protects the one endpoint that spends money. It is only fetched for the modes
 * that can send a message, so a password sign-in makes no request it does not need.
 */
type Mode = "login" | "register" | "sms";

const modeTitles: Record<Mode, string> = {
  login: "登录 Kada",
  register: "注册 Kada",
  sms: "用短信验证码登录",
};

export default function LoginPage() {
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const t = useT();
  // The URL decides which form opens. /register is a shared link, so it has to land on sign-up rather than
  // on a sign-in form the visitor has to notice is the wrong one.
  const [mode, setMode] = useState<Mode>(pathname === "/register" ? "register" : "login");
  const [loading, setLoading] = useState(false);

  const [phone, setPhone] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [codeSent, setCodeSent] = useState(false);
  const [countdown, setCountdown] = useState(0);
  const [sending, setSending] = useState(false);

  const sendsMessages = mode !== "login";

  // The challenge is fetched with SWR rather than from an effect: an effect that calls setState on mount
  // is precisely the cascading render the react-hooks rule warns about, and SWR already expresses "fetch
  // this once, and let me ask for it again". A null key skips the fetch entirely, which is what keeps the
  // password form from asking for a puzzle it has nowhere to show.
  //
  // Revalidation is off on purpose. The challenge is one-time and tied to its id, so the background
  // refresh a window focus would trigger would replace an image the user is halfway through reading with
  // a different one, whose answer they cannot know.
  const {
    data: challenge,
    error: challengeError,
    mutate: refreshCaptcha,
  } = useSWR(sendsMessages ? "auth-captcha" : null, () => authAPI.captcha(), {
    revalidateOnFocus: false,
    revalidateOnReconnect: false,
    // One blip must not take the whole form down with it. Deploying restarts kada-api, and nginx can
    // answer the first request after that with a 502 from a pooled connection the old process owned.
    // With retries switched off, that single failure left the field in its error state until the user
    // clicked the image - which is indistinguishable from a login page that simply does not work. SWR
    // only calls onErrorRetry at all when shouldRetryOnError is left at its default, so that line is
    // gone rather than unused.
    //
    // Bounded on purpose: this is an unauthenticated endpoint, so three attempts spaced over ~3s is
    // enough to ride out a restart and not enough to hammer a service that is genuinely down.
    onErrorRetry: (_err, _key, _config, revalidate, { retryCount }) => {
      if (retryCount > 3) return;
      setTimeout(() => revalidate({ retryCount }), 500 * retryCount);
    },
    // No dedupe window: the refresh after a send attempt MUST reach the server. The server consumes the
    // challenge on every attempt, so a suppressed refresh would leave the user looking at an image that
    // can no longer be answered - and it would only happen when they clicked quickly, which is the worst
    // way to find that out.
    dedupingInterval: 0,
  });

  // The field can only say "could not load"; it cannot print a 502 or an unreachable API without turning
  // the login form into a debug console. The reason still has to exist somewhere, so it goes to the
  // browser console - which is exactly where the last investigation looked and found nothing at all.
  useEffect(() => {
    if (challengeError) {
      console.error("kada: captcha challenge failed", challengeError);
    }
  }, [challengeError]);

  const captchaId = challenge?.captcha_id ?? "";
  const captchaImage = challenge?.image ?? "";
  const [captcha, setCaptcha] = useState("");

  const [agreed, setAgreed] = useState(false);

  const [phoneError, setPhoneError] = useState("");
  const [captchaError, setCaptchaError] = useState("");
  const [codeError, setCodeError] = useState("");
  const [passwordError, setPasswordError] = useState("");
  const [agreeError, setAgreeError] = useState("");

  const phoneValid = phone.length === 11;
  // The same floor the API enforces. Checking it here as well is not duplication for its own sake: it is
  // the difference between "that failed" and "that is too short", and the second one is answerable.
  const passwordValid = password.length >= 8;

  // The resend countdown. The timer is cleared on unmount so a page that is gone does not keep one.
  useEffect(() => {
    if (countdown <= 0) return;
    const timer = setTimeout(() => setCountdown((c) => c - 1), 1000);
    return () => clearTimeout(timer);
  }, [countdown]);

  // Switching modes clears what the other mode was complaining about. A stale "the password is too short"
  // left over the password form, or a captcha error on a form that has no captcha, reads as a bug in the
  // page rather than as a leftover.
  const switchTo = (next: Mode) => {
    setMode(next);
    setPhoneError("");
    setCaptchaError("");
    setCodeError("");
    setPasswordError("");
    setAgreeError("");
  };

  const validatePhone = () => {
    if (!phoneValid) {
      setPhoneError(t("请输入 11 位手机号"));
      return false;
    }
    setPhoneError("");
    return true;
  };

  // Registering and signing in both write something the user agreed to; the agree box only exists on the
  // forms that do.
  const validateCodeForm = () => {
    let ok = validatePhone();
    if (!agreed) {
      setAgreeError(t("请先阅读并同意隐私政策"));
      ok = false;
    } else {
      setAgreeError("");
    }
    return ok;
  };

  const validatePassword = () => {
    if (!passwordValid) {
      setPasswordError(t("密码至少 8 位"));
      return false;
    }
    setPasswordError("");
    return true;
  };

  const handleSendCode = async () => {
    let ok = validateCodeForm();
    if (!captcha.trim()) {
      setCaptchaError(t("请输入图形验证码"));
      ok = false;
    } else {
      setCaptchaError("");
    }
    if (!ok) return;

    setSending(true);
    try {
      await authAPI.sendSMSCode(phone, captchaId, captcha.trim());
      setCodeSent(true);
      toast.success(t("验证码已发送"));
      setCountdown(60);
    } catch (e: unknown) {
      toast.error(e instanceof Error ? e.message : t("发送失败"));
    } finally {
      setSending(false);
      // The server burns the challenge whether the send succeeded or was refused, so a fresh one is the
      // only state worth leaving behind.
      refreshCaptcha();
    }
  };

  // One form, three ways out. The two code paths differ only in whether a password is being set.
  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();

    if (mode === "login") {
      if (!validatePhone() || !validatePassword()) return;

      setLoading(true);
      try {
        const data = await authAPI.login(phone, password);
        setToken(data.token);
        setUser(data.user);
        toast.success(t("登录成功"));
        navigate("/dashboard");
      } catch (e: unknown) {
        toast.error(e instanceof Error ? e.message : t("登录失败"));
      } finally {
        setLoading(false);
      }
      return;
    }

    let ok = validateCodeForm();
    if (code.length !== 6) {
      setCodeError(t("请输入 6 位验证码"));
      ok = false;
    } else {
      setCodeError("");
    }
    if (mode === "register" && !validatePassword()) ok = false;
    if (!ok) return;

    setLoading(true);
    try {
      const data =
        mode === "register"
          ? await authAPI.register(phone, code, password)
          : await authAPI.loginByPhone(phone, code);
      setToken(data.token);
      setUser(data.user);
      toast.success(mode === "register" ? t("注册成功") : t("登录成功"));
      navigate("/dashboard");
    } catch (e: unknown) {
      toast.error(e instanceof Error ? e.message : mode === "register" ? t("注册失败") : t("登录失败"));
    } finally {
      setLoading(false);
    }
  };

  return (
    <>
      {/* The corner the app shell's top bar puts these controls in, so nothing moves as you sign in: the
          cluster and the corner it is pinned to both come from TopBarControls. */}
      <TopBarControls pinned />
      <AuthCard
        title={t(modeTitles[mode])}
        subtitle={t("智能短链接管理平台")}
        footer={
          mode === "login" ? (
            <>
              {t("还没有账号？")}
              <button
                type="button"
                onClick={() => switchTo("register")}
                className="text-link underline underline-offset-2"
              >
                {t("注册")}
              </button>
            </>
          ) : (
            <>
              {t("已有账号？")}
              <button
                type="button"
                onClick={() => switchTo("login")}
                className="text-link underline underline-offset-2"
              >
                {t("登录")}
              </button>
            </>
          )
        }
      >
        <form onSubmit={handleSubmit} className="space-y-4" noValidate>
          <Field id="phone" label={t("手机号")} error={phoneError}>
            <input
              id="phone"
              type="tel"
              value={phone}
              onChange={(e) => setPhone(e.target.value.replace(/\D/g, "").slice(0, 11))}
              className={inputClass(!!phoneError)}
              placeholder={t("请输入 11 位手机号")}
              autoComplete="tel"
              required
            />
          </Field>

          {mode === "login" && (
            <Field id="password" label={t("密码")} error={passwordError}>
              <input
                id="password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                className={inputClass(!!passwordError)}
                placeholder={t("请输入密码")}
                autoComplete="current-password"
                required
              />
            </Field>
          )}

          {sendsMessages && (
            <>
              <Field
                id="captcha"
                label={t("图形验证码")}
                error={captchaError || (challengeError ? t("图形验证码加载失败，请点击刷新") : "")}
              >
                <div className="flex gap-3">
                  <input
                    id="captcha"
                    type="text"
                    value={captcha}
                    onChange={(e) => setCaptcha(e.target.value.slice(0, 6))}
                    className={inputClass(!!captchaError || !!challengeError)}
                    placeholder={t("输入图中字符")}
                    autoComplete="off"
                    required
                  />
                  {/* The image is the refresh control: clicking the picture to get another one is what everyone
                      tries first, and it keeps the affordance the same size as the challenge itself. */}
                  <button
                    type="button"
                    onClick={() => refreshCaptcha()}
                    title={t("看不清？换一张")}
                    aria-label={t("看不清？换一张")}
                    className="relative h-9 w-[120px] shrink-0 overflow-hidden rounded-md border border-line-strong bg-subtle transition-colors hover:bg-muted-surface"
                  >
                    {captchaImage ? (
                      // Painted as a background rather than with <img>: the challenge is an inline SVG data URI
                      // generated per request, and an <img> is fetched once, and painting it as a background keeps it out of the page resources. A background image needs no lint suppression and no loader.
                      <span
                        className="absolute inset-0 bg-contain bg-center bg-no-repeat"
                        style={{ backgroundImage: `url("${captchaImage}")` }}
                      />
                    ) : (
                      <RefreshCw className="size-4 animate-spin text-muted" />
                    )}
                  </button>
                </div>
              </Field>

              <Field id="code" label={t("验证码")} error={codeError}>
                <div className="flex gap-3">
                  <input
                    id="code"
                    type="text"
                    inputMode="numeric"
                    value={code}
                    onChange={(e) => setCode(e.target.value.replace(/\D/g, "").slice(0, 6))}
                    className={inputClass(!!codeError)}
                    placeholder={t("6 位验证码")}
                    autoComplete="one-time-code"
                    required
                  />
                  <button
                    type="button"
                    onClick={handleSendCode}
                    // No challenge, no send: without an id to send back the server would refuse the request
                    // anyway, and a button that visibly does nothing is worse than a disabled one.
                    disabled={countdown > 0 || !phoneValid || !captchaId || sending}
                    className={buttonClass({ variant: "secondary" })}
                  >
                    {countdown > 0 ? `${countdown}s` : codeSent ? t("重新发送") : t("获取验证码")}
                  </button>
                </div>
              </Field>
            </>
          )}

          {mode === "register" && (
            <Field
              id="new-password"
              label={t("密码")}
              hint={t("至少 8 位字符")}
              error={passwordError}
            >
              <input
                id="new-password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                className={inputClass(!!passwordError)}
                placeholder={t("请输入密码")}
                autoComplete="new-password"
                required
              />
            </Field>
          )}

          {sendsMessages && (
            <div>
              <label className="flex items-start gap-2 text-xs text-muted">
                <input
                  type="checkbox"
                  checked={agreed}
                  onChange={(e) => {
                    setAgreed(e.target.checked);
                    if (e.target.checked) setAgreeError("");
                  }}
                  className="mt-0.5 size-3.5 shrink-0 cursor-pointer rounded border-line-strong bg-canvas accent-brand"
                />
                <span>
                  {t("我已阅读并同意")}
                  <Link
                    to="/privacy"
                    target="_blank"
                    className="text-link underline underline-offset-2"
                  >
                    {t("《隐私政策》")}
                  </Link>
                </span>
              </label>
              {agreeError && <p className="mt-1 text-xs text-danger">{agreeError}</p>}
            </div>
          )}

          <button
            type="submit"
            disabled={loading}
            className={buttonClass({ className: "w-full" })}
          >
            {loading
              ? mode === "register"
                ? t("注册中...")
                : t("登录中...")
              : mode === "register"
                ? t("注册")
                : t("登录")}
          </button>

          {/* The way into an account that has no password yet, and out of the code flow once it has one.
              Small and unemphasised: it is not how anyone should sign in, it is how they get unstuck. */}
          <button
            type="button"
            onClick={() => switchTo(mode === "sms" ? "login" : "sms")}
            className="w-full text-center text-xs text-muted transition hover:text-strong"
          >
            {mode === "sms" ? t("用密码登录") : t("用短信验证码登录")}
          </button>
        </form>
      </AuthCard>
    </>
  );
}
