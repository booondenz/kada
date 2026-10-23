import { useState } from "react";
import useSWR from "swr";
import { toast } from "sonner";
import { linksAPI, uploadsAPI } from "@/lib/api";
import type { LinkTarget } from "@/lib/api";
import { buttonClass } from "@/components/ui/Button";
import { inputClass } from "@/components/ui/Field";
import { useT } from "@/lib/i18n";

/**
 * A row as the form holds it: the API speaks RFC 3339 and the date input speaks days, so `expiresAt` is the
 * `YYYY-MM-DD` part a person picked.
 */
interface Draft {
  url: string;
  weight: number;
  dailyQuota: number;
  enabled: boolean;
  clicks: number;
  kind: "url" | "wechat_group";
  expiresAt: string;
}

function toDraft(target: LinkTarget): Draft {
  return {
    url: target.url,
    weight: target.weight,
    dailyQuota: target.daily_quota,
    enabled: target.enabled,
    clicks: target.clicks,
    kind: target.kind === "wechat_group" ? "wechat_group" : "url",
    expiresAt: target.expires_at ? target.expires_at.slice(0, 10) : "",
  };
}

const BLANK_TARGET: Draft = {
  url: "",
  weight: 1,
  dailyQuota: 0,
  enabled: true,
  clicks: 0,
  kind: "url",
  expiresAt: "",
};

// A group code runs out: WeChat's last seven days, and the QR itself stops admitting anyone once the group
// is full. The pool skips an expired target on its own; these two say so to the one person who can replace
// it, on the row where the replacement happens.
const expired = (date: string) => new Date(`${date}T23:59:59Z`).getTime() <= Date.now();
const expiresWithin = (date: string, days: number) =>
  !expired(date) && new Date(`${date}T23:59:59Z`).getTime() - Date.now() <= days * 86_400_000;

/**
 * The pool of destinations behind one link, and the form that replaces it.
 *
 * It owns its own read as well as its own write: a pool is a routing decision with statistics attached, and
 * the redirect carries it on every hit, so it never rides along with the ordinary link read.
 */
export function TargetPool({ token, linkId }: { token: string; linkId: number }) {
  const t = useT();
  const { data, isLoading, mutate } = useSWR<{ targets?: LinkTarget[] }>(
    ["link-targets", linkId],
    () => linksAPI.targets(token, linkId),
  );

  // null means "not editing": the rows below the button are what the link currently does.
  const [draft, setDraft] = useState<Draft[] | null>(null);
  const [saving, setSaving] = useState(false);

  const targets = (data?.targets ?? []).map(toDraft);

  const update = (index: number, patch: Partial<Draft>) => {
    setDraft((prev) => prev && prev.map((target, i) => (i === index ? { ...target, ...patch } : target)));
  };

  // A group code is an image, and the image has to be hosted somewhere the visitor can see it - which is
  // what the upload route is for. The target keeps the file's URL, so replacing the code later is editing
  // the row rather than reprinting anything.
  const uploadImage = async (index: number, file: File) => {
    try {
      const uploaded = await uploadsAPI.create(token, file);
      update(index, { url: uploaded.upload.url, kind: "wechat_group" });
      toast.success(t("文件已上传"));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("上传失败"));
    }
  };

  const save = async () => {
    if (!draft) return;
    const filled = draft.filter((target) => target.url.trim() !== "");
    setSaving(true);
    try {
      await linksAPI.replaceTargets(
        token,
        linkId,
        filled.map((target) => ({
          url: target.url.trim(),
          weight: target.weight,
          daily_quota: target.dailyQuota,
          enabled: target.enabled,
          kind: target.kind,
          // The end of the chosen day, in UTC: the owner picks a date rather than a time, and the target
          // stays live through it.
          expires_at: target.expiresAt ? `${target.expiresAt}T23:59:59Z` : undefined,
        })),
      );
      toast.success(t("分流目标已保存"));
      setDraft(null);
      await mutate();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("保存分流失败"));
    } finally {
      setSaving(false);
    }
  };

  if (isLoading) {
    return <p className="py-6 text-sm text-muted">{t("加载中...")}</p>;
  }

  if (draft) {
    return (
      <div className="space-y-2">
        {draft.map((target, index) => (
          <div key={index} className="space-y-2 rounded-lg border border-line p-2">
            <div className="flex gap-2">
              <input
                value={target.url}
                onChange={(e) => update(index, { url: e.target.value })}
                placeholder="https://"
                className={`${inputClass()} min-w-0 flex-1 font-mono text-xs`}
              />
              <label
                className={buttonClass({
                  variant: "secondary",
                  size: "sm",
                  className: "shrink-0 cursor-pointer",
                })}
              >
                {t("上传二维码")}
                <input
                  type="file"
                  accept=".png,.jpg,.jpeg"
                  className="hidden"
                  onChange={(e) => {
                    const file = e.target.files?.[0];
                    if (file) void uploadImage(index, file);
                    e.target.value = "";
                  }}
                />
              </label>
            </div>

            <div className="flex flex-wrap items-center gap-2 text-xs text-muted">
              <select
                value={target.kind}
                onChange={(e) => update(index, { kind: e.target.value as "url" | "wechat_group" })}
                className={inputClass()}
                title={t("目标类型")}
              >
                <option value="url">{t("普通链接")}</option>
                <option value="wechat_group">{t("微信群二维码")}</option>
              </select>
              <input
                type="number"
                min={0}
                value={target.weight}
                onChange={(e) => update(index, { weight: Number(e.target.value) })}
                title={t("权重")}
                className={`${inputClass()} w-20`}
              />
              <input
                type="number"
                min={0}
                value={target.dailyQuota}
                onChange={(e) => update(index, { dailyQuota: Number(e.target.value) })}
                title={t("每日上限")}
                className={`${inputClass()} w-24`}
              />
              <input
                type="date"
                value={target.expiresAt}
                onChange={(e) => update(index, { expiresAt: e.target.value })}
                title={t("有效期")}
                className={inputClass()}
              />
              <label className="flex items-center gap-1">
                <input
                  type="checkbox"
                  checked={target.enabled}
                  onChange={(e) => update(index, { enabled: e.target.checked })}
                />
                {t("启用")}
              </label>
              <button
                onClick={() =>
                  setDraft((prev) => prev && prev.filter((_, i) => i !== index))
                }
                className="ml-auto text-danger"
              >
                {t("删除")}
              </button>
            </div>
          </div>
        ))}

        <div className="flex flex-wrap items-center gap-2">
          <button
            onClick={() => setDraft((prev) => [...(prev ?? []), BLANK_TARGET])}
            className={buttonClass({ variant: "secondary", size: "sm" })}
          >
            {t("添加目标")}
          </button>
          <button onClick={save} disabled={saving} className={buttonClass({ size: "sm" })}>
            {saving ? t("保存中...") : t("保存分流")}
          </button>
          <button
            onClick={() => setDraft(null)}
            className={buttonClass({ variant: "ghost", size: "sm" })}
          >
            {t("取消")}
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <div className="flex items-start justify-between gap-3">
        <p className="text-xs text-muted">
          {t("设置多个目标后，访客会按权重被分流；全部停用或满额时链接不再跳转")}
        </p>
        <button
          onClick={() => setDraft(targets.length > 0 ? targets : [BLANK_TARGET])}
          className={buttonClass({ variant: "secondary", size: "sm", className: "shrink-0" })}
        >
          {targets.length > 0 ? t("编辑") : t("添加目标")}
        </button>
      </div>

      {targets.length === 0 ? (
        <p className="text-sm text-muted">{t("没有分流目标，访客会直接跳到原目标地址")}</p>
      ) : (
        <ul className="divide-y divide-line overflow-hidden rounded-lg border border-line">
          {targets.map((target, index) => (
            <li key={index} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-sm">
              <span
                className={`size-1.5 shrink-0 rounded-full ${target.enabled ? "bg-emerald-500" : "bg-muted-surface"}`}
              />
              <span className="min-w-0 flex-1 truncate font-mono text-xs text-body">{target.url}</span>
              {target.kind === "wechat_group" && (
                <span className="shrink-0 rounded-full bg-sky-50 px-1.5 py-0.5 text-[10px] font-medium text-sky-700">
                  {t("微信群二维码")}
                </span>
              )}
              {target.expiresAt && (
                <span
                  className={`shrink-0 text-xs tabular-nums ${
                    expiresWithin(target.expiresAt, 3) ? "font-medium text-amber-600" : "text-muted"
                  }`}
                >
                  {expired(target.expiresAt) ? t("已过期") : `${t("有效期至")} ${target.expiresAt}`}
                </span>
              )}
              <span className="shrink-0 text-xs text-muted tabular-nums">
                {t("权重")} {target.weight}
              </span>
              {target.dailyQuota > 0 && (
                <span className="shrink-0 text-xs text-muted tabular-nums">
                  {t("每日上限")} {target.dailyQuota}
                </span>
              )}
              <span className="shrink-0 text-xs text-muted tabular-nums">
                {target.clicks.toLocaleString()} {t("点击")}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
