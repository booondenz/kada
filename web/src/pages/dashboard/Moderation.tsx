import { useState } from "react";
import useSWR from "swr";
import { toast } from "sonner";
import { adminAPI, type AbuseReport } from "@/lib/api";
import type { LinkBlockRecord } from "@/lib/api";
import { getToken } from "@/lib/auth";
import { buttonClass } from "@/components/ui/Button";
import { inputClass } from "@/components/ui/Field";
import { useT } from "@/lib/i18n";

/**
 * The operator console: what visitors reported, and the two decisions an operator makes.
 *
 * It is one page because the job is one job - read a complaint, decide, write down why. The routes behind it
 * refuse anybody whose number is not in `ADMIN_PHONES`; this page shows the refusal rather than hiding the
 * possibility, so an operator who has just been added (or removed) can see which side they are on.
 */
export default function ModerationPage() {
  const t = useT();
  const token = getToken();

  const [code, setCode] = useState("");
  const [reason, setReason] = useState("");
  const [evidence, setEvidence] = useState("");
  const [busy, setBusy] = useState(false);
  const [historyCode, setHistoryCode] = useState<string | null>(null);

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

  const { data, error, mutate, isLoading } = useSWR(token ? "admin-reports" : null, () =>
    adminAPI.reports(token!),
  );
  const reports: AbuseReport[] = data?.reports ?? [];
  const notOperator = error instanceof Error && /not an operator/.test(error.message);

  const { data: history } = useSWR(
    token && historyCode ? ["admin-blocks", historyCode] : null,
    () => adminAPI.blocks(token!, historyCode!),
  );
  const blockHistory: LinkBlockRecord[] = history?.blocks ?? [];

  const block = async () => {
    if (!token || !code.trim()) return;
    setBusy(true);
    try {
      await adminAPI.block(token, code.trim(), reason.trim() || t("未填写"), evidence.trim());
      toast.success(t("已停止该链接"));
      setReason("");
      setEvidence("");
      await mutate();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("操作失败"));
    } finally {
      setBusy(false);
    }
  };

  const unblock = async (shortCode: string) => {
    if (!token) return;
    setBusy(true);
    try {
      await adminAPI.unblock(token, shortCode, "");
      toast.success(t("已恢复该链接"));
      await mutate();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("操作失败"));
    } finally {
      setBusy(false);
    }
  };

  if (notOperator) {
    return (
      <div className="max-w-2xl">
        <h1 className="text-2xl font-bold text-strong">{t("运营")}</h1>
        <p className="mt-3 text-sm text-muted">
          {t("这个账号不是运营。把手机号加进 ADMIN_PHONES 之后才会看到这里的举报。")}
        </p>
      </div>
    );
  }

  return (
    <div className="max-w-3xl space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-strong">{t("运营")}</h1>
        <p className="mt-1 text-sm text-muted">
          {t("访客举报的链接，以及停用和恢复。每一次操作都会记录是谁、为什么。")}
        </p>
      </div>

      {/* The decision itself, by short code: a report names one, and the code is the handle an operator has. */}
      <section className="rounded-xl border border-line bg-canvas p-5">
        <h2 className="text-sm font-semibold text-strong">{t("按短码操作")}</h2>
        <div className="mt-3 space-y-2">
          <input
            value={code}
            onChange={(e) => setCode(e.target.value)}
            placeholder={t("短链代码")}
            className={`${inputClass()} w-full font-mono text-sm`}
          />
          <div className="flex flex-wrap gap-2">
            <input
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder={t("停用原因")}
              className={`${inputClass()} min-w-0 flex-1`}
            />
            <input
              value={evidence}
              onChange={(e) => setEvidence(e.target.value)}
              placeholder={t("证据（举报编号或链接）")}
              className={`${inputClass()} min-w-0 flex-1`}
            />
          </div>
          <div className="flex flex-wrap gap-2">
            <button onClick={block} disabled={busy || !code.trim()} className={buttonClass({ size: "sm" })}>
              {t("停止访问")}
            </button>
            <button
              onClick={() => code.trim() && unblock(code.trim())}
              disabled={busy || !code.trim()}
              className={buttonClass({ variant: "secondary", size: "sm" })}
            >
              {t("恢复访问")}
            </button>
            <button
              onClick={() => setHistoryCode(code.trim() || null)}
              disabled={!code.trim()}
              className={buttonClass({ variant: "ghost", size: "sm" })}
            >
              {t("查看处理记录")}
            </button>
          </div>

          {historyCode && blockHistory.length > 0 && (
            <ul className="mt-2 space-y-1 border-t border-line pt-2 text-xs text-muted">
              {blockHistory.map((row) => (
                <li key={row.id} className="flex flex-wrap gap-x-2">
                  <span className="font-mono">{row.created_at.slice(0, 16).replace("T", " ")}</span>
                  <span>{row.reason}</span>
                  {row.evidence && <span>· {row.evidence}</span>}
                  <span>· {row.operator}</span>
                  {row.lifted_at && <span className="text-emerald-600">· {t("已恢复")}</span>}
                </li>
              ))}
            </ul>
          )}
        </div>
      </section>

      {/* What was reported. Newest first, and whether the link it names is stopped now - which is the
          "has this been dealt with" question without a status column. */}
      <section className="rounded-xl border border-line bg-canvas">
        <div className="flex items-center justify-between border-b border-line px-5 py-3">
          <h2 className="text-sm font-semibold text-strong">{t("举报")}</h2>
          <button onClick={() => void mutate()} className="text-xs text-muted hover:text-strong">
            {t("重新加载")}
          </button>
        </div>

        {isLoading ? (
          <p className="px-5 py-6 text-sm text-muted">{t("加载中...")}</p>
        ) : reports.length === 0 ? (
          <p className="px-5 py-6 text-sm text-muted">{t("还没有人举报。")}</p>
        ) : (
          <ul className="divide-y divide-line">
            {reports.map((report) => (
              <li key={report.id} className="px-5 py-4">
                <div className="flex flex-wrap items-center gap-2 text-sm">
                  <button
                    onClick={() => {
                      setCode(report.short_code);
                      setHistoryCode(report.short_code);
                    }}
                    className="font-mono text-brand-ink hover:underline"
                  >
                    {report.short_code}
                  </button>
                  <span className="rounded-full bg-muted-surface px-1.5 py-0.5 text-[10px] text-muted">
                    {reasonLabel(report.reason)}
                  </span>
                  <span className="text-xs text-faint">
                    {report.created_at.slice(0, 16).replace("T", " ")}
                  </span>
                  {report.link_blocked ? (
                    <span className="rounded-full bg-red-50 px-1.5 py-0.5 text-[10px] font-medium text-red-700">
                      {t("该链接已被停止访问")}
                    </span>
                  ) : (
                    <button
                      onClick={() => {
                        setCode(report.short_code);
                        setReason(t("访客举报：") + reasonLabel(report.reason));
                        setEvidence(`${t("举报")} #${report.id}`);
                      }}
                      className="text-xs text-brand-ink hover:underline"
                    >
                      {t("据此停用")}
                    </button>
                  )}
                </div>
                {report.detail && <p className="mt-1 text-sm text-body">{report.detail}</p>}
                {report.contact && <p className="mt-1 text-xs text-faint">{report.contact}</p>}
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
