import { useEffect, useState } from "react";
import { Link, useSearchParams } from "react-router";
import useSWR from "swr";
import { linksAPI } from "@/lib/api";
import { TargetPool } from "@/components/TargetPool";
import { inputClass } from "@/components/ui/Field";
import { buttonClass } from "@/components/ui/Button";
import { getToken } from "@/lib/auth";
import { useT } from "@/lib/i18n";
import type { LinkItem } from "@/components/LinkCard";

/**
 * 分流: which links send their visitors to more than one place.
 *
 * The pool itself belongs to a link, so the page is a choice of link followed by an editor - one request
 * per pool, for the link that is on screen. It used to live on the link page, where the question "where does
 * my traffic go" could only be asked one link at a time, from a page about that link's title and tags.
 */
export default function TargetsPage() {
  const t = useT();
  const token = getToken();
  const [searchParams] = useSearchParams();
  // Only digits reach the API: a hand-edited ?link= nonsense falls back to no selection.
  const requested = searchParams.get("link");
  const wanted = requested && /^\d+$/.test(requested) ? requested : "";
  const [selected, setSelected] = useState(wanted);

  const { data } = useSWR(token ? "routing-links" : null, () => linksAPI.list(token!, 1, 100));

  const links: LinkItem[] = data?.links ?? [];
  const total: number = data?.total_count ?? 0;

  // The link page sends ?link=<id>: arriving from a link means that link's pool is the one to edit.
  useEffect(() => {
    if (wanted) setSelected(wanted);
  }, [wanted]);

  // A link named in the URL is not necessarily in the hundred above, and the select must not claim
  // otherwise: it gets a row of its own rather than silently showing nothing.
  const current = links.find((link) => String(link.id) === selected);

  return (
    <div className="space-y-5">
      <div>
        <h1 className="text-2xl font-bold text-strong">{t("分流")}</h1>
        <p className="mt-1 text-sm text-muted">
          {t("把一条短链的访客按权重分给多个目标；没有目标时，链接直接跳到原地址。")}
        </p>
      </div>

      {data && links.length === 0 ? (
        <div className="rounded-xl border border-line bg-canvas p-10 text-center shadow-sm">
          <p className="text-sm text-muted">{t("还没有链接")}</p>
          <Link to="/dashboard/links/new" className={buttonClass({ size: "sm", className: "mt-4" })}>
            {t("创建短链接")}
          </Link>
        </div>
      ) : (
        <div className="overflow-hidden rounded-xl border border-line bg-canvas shadow-sm">
          <div className="flex flex-wrap items-center gap-3 border-b border-line px-5 py-4">
            <label htmlFor="routing-link" className="text-sm font-medium text-strong">
              {t("链接")}
            </label>
            <select
              id="routing-link"
              value={selected}
              onChange={(e) => setSelected(e.target.value)}
              className={`${inputClass()} max-w-md`}
            >
              <option value="">{t("选择链接")}</option>
              {links.map((link) => (
                <option key={link.id} value={link.id}>
                  {link.title || t("未命名链接")} · {link.short_code}
                </option>
              ))}
              {selected && !current && (
                <option value={selected}>
                  {t("链接")} #{selected}
                </option>
              )}
            </select>
            {current && <code className="truncate text-xs text-faint">{current.short_url}</code>}
          </div>

          <div className="p-5">
            {token && selected ? (
              <TargetPool key={selected} token={token} linkId={Number(selected)} />
            ) : (
              <p className="text-sm text-muted">{t("选择一条链接，编辑它的分流目标。")}</p>
            )}
          </div>

          {total > links.length && (
            <p className="border-t border-line px-5 py-3 text-xs text-faint">
              {t("只列出最近的 {n} 条链接", { n: links.length })}
            </p>
          )}
        </div>
      )}
    </div>
  );
}
