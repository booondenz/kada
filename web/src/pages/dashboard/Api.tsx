import { useState } from "react";
import useSWR from "swr";
import { toast } from "sonner";
import { Copy, Plus, Trash2 } from "lucide-react";
import { tokensAPI } from "@/lib/api";
import { getToken } from "@/lib/auth";
import { buttonClass } from "@/components/ui/Button";
import { inputClass } from "@/components/ui/Field";
import { useT } from "@/lib/i18n";

const BASE = typeof window === "undefined" ? "https://kada.click" : window.location.origin;

/**
 * The API page: what the endpoints are, and the tokens that reach them.
 *
 * It is a documentation page before it is a token manager, because the question that brings somebody here is
 * "how do I call this" and the token is the answer to the second half of it. The examples are text, not a
 * playground: a playground would need its own backend and would teach nothing a copy-paste does not.
 */
export default function ApiPage() {
  const t = useT();
  const token = getToken();

  const { data: tokenData, mutate: mutateTokens } = useSWR(token ? "api-tokens" : null, () =>
    tokensAPI.list(token!),
  );
  const tokens = tokenData?.tokens ?? [];
  const [newTokenName, setNewTokenName] = useState("");
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<string | null>(null);

  const createToken = async () => {
    if (!token || !newTokenName.trim()) return;
    setCreating(true);
    try {
      const data = await tokensAPI.create(token, newTokenName.trim());
      setCreated(data.token);
      setNewTokenName("");
      void mutateTokens();
      toast.success(t("Token 创建成功"));
    } catch {
      toast.error(t("创建失败"));
    } finally {
      setCreating(false);
    }
  };

  const deleteToken = async (id: number) => {
    if (!token) return;
    try {
      await tokensAPI.delete(token, id);
      void mutateTokens();
      toast.success(t("Token 已删除"));
    } catch {
      toast.error(t("删除失败"));
    }
  };

  const exampleCreate = [
    `curl -X POST ${BASE}/api/links \\`,
    `  -H "Authorization: Bearer kada_your_token" \\`,
    `  -H "Content-Type: application/json" \\`,
    `  -d '{"original_url":"https://example.com/some/long/page","title":"示例"}'`,
  ].join("\n");

  const exampleList = `curl "${BASE}/api/links?page=1&page_size=20" \\
  -H "Authorization: Bearer kada_your_token"`;

  const exampleJS = [
    `const res = await fetch("${BASE}/api/links", {`,
    `  method: "POST",`,
    `  headers: {`,
    `    Authorization: "Bearer kada_your_token",`,
    `    "Content-Type": "application/json",`,
    `  },`,
    `  body: JSON.stringify({ original_url: "https://example.com/some/long/page" }),`,
    `});`,
    `const { link } = await res.json();`,
    `console.log(link.short_url);`,
  ].join("\n");

  const copy = (text: string) => {
    void navigator.clipboard.writeText(text);
    toast.success(t("已复制"));
  };

  return (
    <div className="max-w-3xl space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-strong">{t("API")}</h1>
        <p className="mt-1 text-sm text-muted">
          {t("用令牌调用接口，在你自己的程序里创建短链、读取统计。")}
        </p>
      </div>

      <section className="space-y-3 rounded-xl border border-line bg-canvas p-5">
        <h2 className="text-sm font-semibold text-strong">{t("接口地址")}</h2>
        <p className="font-mono text-sm text-body">{BASE}/api</p>

        <h2 className="pt-1 text-sm font-semibold text-strong">{t("认证方式")}</h2>
        <p className="text-sm text-body">
          {t("每个请求带上 Authorization: Bearer <令牌>。令牌在下面创建，只显示一次。")}
        </p>
      </section>

      <section className="space-y-4 rounded-xl border border-line bg-canvas p-5">
        <h2 className="text-sm font-semibold text-strong">{t("示例代码")}</h2>

        {[
          { title: t("创建短链（curl）"), code: exampleCreate },
          { title: t("列出链接（curl）"), code: exampleList },
          { title: t("创建短链（JavaScript）"), code: exampleJS },
        ].map((block) => (
          <div key={block.title} className="space-y-1.5">
            <div className="flex items-center justify-between">
              <p className="text-xs font-medium text-muted">{block.title}</p>
              <button
                onClick={() => copy(block.code)}
                className="inline-flex items-center gap-1 text-xs text-muted transition-colors hover:text-strong"
              >
                <Copy className="size-3" />
                {t("复制")}
              </button>
            </div>
            <pre className="overflow-x-auto rounded-lg border border-line bg-subtle p-3 text-xs leading-relaxed text-body">
              <code>{block.code}</code>
            </pre>
          </div>
        ))}

        <p className="text-xs text-muted">{t("把 kada_your_token 换成你自己的令牌。")}</p>
      </section>

      <section className="space-y-3 rounded-xl border border-line bg-canvas p-5">
        <div>
          <h2 className="text-sm font-semibold text-strong">{t("API 令牌")}</h2>
          <p className="mt-0.5 text-xs text-muted">{t("令牌只显示一次，创建后请立刻复制。")}</p>
        </div>

        <div className="flex flex-wrap gap-2">
          <input
            value={newTokenName}
            onChange={(e) => setNewTokenName(e.target.value)}
            placeholder={t("令牌名称")}
            className={`${inputClass()} min-w-0 flex-1`}
          />
          <button onClick={createToken} disabled={creating || !newTokenName.trim()} className={buttonClass()}>
            <Plus className="mr-1 size-3.5" />
            {creating ? t("创建中...") : t("新建令牌")}
          </button>
        </div>

        {created && (
          <div className="space-y-1 rounded-lg border border-line bg-subtle p-3">
            <p className="text-xs text-muted">{t("新令牌（只显示这一次）")}</p>
            <div className="flex items-center gap-2">
              <code className="min-w-0 flex-1 truncate font-mono text-xs text-strong">{created}</code>
              <button onClick={() => copy(created)} className="text-xs text-brand-ink hover:underline">
                {t("复制")}
              </button>
            </div>
          </div>
        )}

        {tokens.length === 0 ? (
          <p className="text-sm text-muted">{t("还没有令牌。")}</p>
        ) : (
          <ul className="divide-y divide-line">
            {tokens.map((tok: { id: number; name: string; last_used: string | null; created_at: string }) => (
              <li key={tok.id} className="flex items-center gap-3 py-2 text-sm">
                <span className="min-w-0 flex-1 truncate text-body">{tok.name}</span>
                <span className="shrink-0 text-xs text-faint">
                  {tok.last_used
                    ? t(" · 最近使用 ${date}").replace("${date}", tok.last_used.slice(0, 10))
                    : t(" · 从未使用")}
                </span>
                <button
                  onClick={() => deleteToken(tok.id)}
                  className="shrink-0 text-faint transition-colors hover:text-danger"
                  title={t("删除")}
                >
                  <Trash2 className="size-3.5" />
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
