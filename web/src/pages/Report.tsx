import { useState } from "react";
import { Link, useSearchParams } from "react-router";
import { ArrowLeft } from "lucide-react";
import { toast } from "sonner";
import { IcpNotice } from "@/components/IcpNotice";
import { buttonClass } from "@/components/ui/Button";
import { inputClass } from "@/components/ui/Field";
import { reportsAPI } from "@/lib/api";
import { useT } from "@/lib/i18n";

/** The values `POST /api/reports` accepts. They are the same strings blocked_hosts.category uses. */
const REASONS = ["porn", "gambling", "drugs", "fraud", "phishing", "malware", "other"] as const;

/**
 * The public complaint form.
 *
 * It is reachable by anyone, including a visitor who has never used the product: the person who needs it
 * was sent somewhere illegal by a short link and has no account here. The short code is prefilled from the
 * stopped page that sent them, so reporting one is two taps and a sentence.
 */
export default function ReportPage() {
  const t = useT();
  const [searchParams] = useSearchParams();

  const [code, setCode] = useState(searchParams.get("code") ?? "");
  const [reason, setReason] = useState("");
  const [detail, setDetail] = useState("");
  const [contact, setContact] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [done, setDone] = useState(false);

  const reasonLabel = (value: string) => {
    switch (value) {
      case "porn":
        return t("色情内容");
      case "gambling":
        return t("赌博");
      case "drugs":
        return t("毒品");
      case "fraud":
        return t("诈骗");
      case "phishing":
        return t("钓鱼网站");
      case "malware":
        return t("恶意软件");
      default:
        return t("其他");
    }
  };

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!code.trim()) {
      toast.error(t("请填写短链代码"));
      return;
    }
    if (!reason) {
      toast.error(t("请选择原因"));
      return;
    }

    setSubmitting(true);
    try {
      await reportsAPI.create({
        short_code: code.trim(),
        reason,
        detail: detail.trim(),
        contact: contact.trim() || undefined,
      });
      setDone(true);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("提交失败，请稍后重试"));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="min-h-screen bg-subtle px-4 py-10">
      <div className="mx-auto max-w-lg">
        <Link to="/" className="inline-flex items-center gap-1.5 text-sm text-muted transition-colors hover:text-brand-ink">
          <ArrowLeft className="size-4" />
          {t("返回首页")}
        </Link>

        <div className="mt-6 rounded-xl border border-line bg-canvas p-6 sm:p-8">
          <h1 className="text-xl font-bold text-strong">{t("举报链接")}</h1>
          <p className="mt-2 text-sm text-muted">
            {t("如果你打开的短链接指向违规内容，请告诉我们。我们会在 24 小时内核查。")}
          </p>

          {done ? (
            <div className="mt-6 rounded-lg border border-line bg-subtle p-4">
              <p className="text-sm font-medium text-strong">{t("举报已提交")}</p>
              <p className="mt-1 text-sm text-muted">{t("感谢你的反馈，我们会在 24 小时内核查。")}</p>
            </div>
          ) : (
            <form onSubmit={submit} className="mt-6 space-y-4">
              <div>
                <label htmlFor="report-code" className="mb-1 block text-xs font-medium text-muted">
                  {t("短链代码")}
                </label>
                <input
                  id="report-code"
                  required
                  maxLength={20}
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                  placeholder="abc123"
                  className={`${inputClass()} w-full font-mono`}
                />
              </div>

              <div>
                <label htmlFor="report-reason" className="mb-1 block text-xs font-medium text-muted">
                  {t("举报原因")}
                </label>
                <select
                  id="report-reason"
                  required
                  value={reason}
                  onChange={(e) => setReason(e.target.value)}
                  className={`${inputClass()} w-full`}
                >
                  <option value="">{t("请选择原因")}</option>
                  {REASONS.map((value) => (
                    <option key={value} value={value}>
                      {reasonLabel(value)}
                    </option>
                  ))}
                </select>
              </div>

              <div>
                <label htmlFor="report-detail" className="mb-1 block text-xs font-medium text-muted">
                  {t("详细说明")}
                </label>
                <textarea
                  id="report-detail"
                  rows={4}
                  maxLength={2000}
                  value={detail}
                  onChange={(e) => setDetail(e.target.value)}
                  placeholder={t("请描述你看到了什么，帮助我们核查")}
                  className={`${inputClass()} w-full resize-y`}
                />
              </div>

              <div>
                <label htmlFor="report-contact" className="mb-1 block text-xs font-medium text-muted">
                  {t("联系方式（选填）")}
                </label>
                <input
                  id="report-contact"
                  maxLength={255}
                  value={contact}
                  onChange={(e) => setContact(e.target.value)}
                  placeholder={t("邮箱或手机号，仅在需要核实时使用")}
                  className={`${inputClass()} w-full`}
                />
              </div>

              <button type="submit" disabled={submitting} className={buttonClass({ className: "w-full" })}>
                {submitting ? t("提交中...") : t("提交举报")}
              </button>
            </form>
          )}
        </div>

        <div className="mt-6 text-center">
          <IcpNotice className="text-xs text-muted hover:text-brand" />
        </div>
      </div>
    </div>
  );
}
