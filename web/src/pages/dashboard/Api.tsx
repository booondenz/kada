import { useState } from "react";
import useSWR from "swr";
import { toast } from "sonner";
import { Copy, Plus, Trash2 } from "lucide-react";
import { tokensAPI } from "@/lib/api";
import { getToken } from "@/lib/auth";
import { Button } from "@/components/ui/Button";
import { inputClass } from "@/components/ui/Field";
import { PageHeader } from "@/components/ui/PageHeader";
import { cn } from "@/lib/utils";
import { useT } from "@/lib/i18n";

const BASE = typeof window === "undefined" ? "https://kada.click" : window.location.origin;

/**
 * The API page: what the endpoints are, how to call them from the language you write in, and the tokens
 * that reach them.
 *
 * It is a documentation page before it is a token manager, because the question that brings somebody here
 * is "how do I call this" and the token is the answer to the second half of it. The examples are text, not
 * a playground: a playground would need its own backend and would teach nothing a copy-paste does not.
 *
 * One example per language, and each one is a complete program or a complete fetch - a snippet that leaves
 * out the imports is the snippet that costs the reader ten minutes with a compiler.
 */

/** Templates are indented with the file; this strips the four spaces that belong to the source. */
const code = (text: string) => text.replace(/^ {4}/gm, "").trim();

type Language = { id: string; label: string; example: (base: string) => string };

export const LANGUAGES: Language[] = [
  {
    id: "curl",
    label: "cURL",
    example: (base) =>
      code(`
    curl -X POST ${base}/api/links \\
      -H "Authorization: Bearer kada_your_token" \\
      -H "Content-Type: application/json" \\
      -d '{"original_url":"https://example.com/some/long/page","title":"示例"}'
    `),
  },
  {
    id: "go",
    label: "Go",
    example: (base) =>
      code(`
    package main

    import (
        "encoding/json"
        "fmt"
        "net/http"
        "strings"
    )

    func main() {
        body := strings.NewReader(\`{"original_url":"https://example.com/some/long/page","title":"示例"}\`)
        req, _ := http.NewRequest("POST", "${base}/api/links", body)
        req.Header.Set("Authorization", "Bearer kada_your_token")
        req.Header.Set("Content-Type", "application/json")

        res, err := http.DefaultClient.Do(req)
        if err != nil {
            panic(err)
        }
        defer res.Body.Close()

        var out struct {
            Link struct {
                ShortURL string \`json:"short_url"\`
            } \`json:"link"\`
        }
        json.NewDecoder(res.Body).Decode(&out)
        fmt.Println(out.Link.ShortURL)
    }
    `),
  },
  {
    id: "c",
    label: "C",
    example: (base) =>
      code(`
    #include <curl/curl.h>

    int main(void) {
        CURL *curl = curl_easy_init();
        struct curl_slist *headers = NULL;
        headers = curl_slist_append(headers, "Authorization: Bearer kada_your_token");
        headers = curl_slist_append(headers, "Content-Type: application/json");

        curl_easy_setopt(curl, CURLOPT_URL, "${base}/api/links");
        curl_easy_setopt(curl, CURLOPT_HTTPHEADER, headers);
        curl_easy_setopt(curl, CURLOPT_POSTFIELDS,
                         "{\\"original_url\\":\\"https://example.com/some/long/page\\",\\"title\\":\\"示例\\"}");

        CURLcode rc = curl_easy_perform(curl);
        curl_slist_free_all(headers);
        curl_easy_cleanup(curl);
        return rc == CURLE_OK ? 0 : 1;
    }
    `),
  },
  {
    id: "cpp",
    label: "C++",
    example: (base) =>
      code(`
    #include <curl/curl.h>

    int main() {
        CURL *curl = curl_easy_init();
        struct curl_slist *headers = nullptr;
        headers = curl_slist_append(headers, "Authorization: Bearer kada_your_token");
        headers = curl_slist_append(headers, "Content-Type: application/json");

        const char *payload =
            R"({"original_url":"https://example.com/some/long/page","title":"示例"})";

        curl_easy_setopt(curl, CURLOPT_URL, "${base}/api/links");
        curl_easy_setopt(curl, CURLOPT_HTTPHEADER, headers);
        curl_easy_setopt(curl, CURLOPT_POSTFIELDS, payload);

        CURLcode rc = curl_easy_perform(curl);
        curl_slist_free_all(headers);
        curl_easy_cleanup(curl);
        return rc == CURLE_OK ? 0 : 1;
    }
    `),
  },
  {
    id: "java",
    label: "Java",
    example: (base) =>
      code(`
    import java.net.URI;
    import java.net.http.HttpClient;
    import java.net.http.HttpRequest;
    import java.net.http.HttpResponse;

    var client = HttpClient.newHttpClient();
    var payload = """
        {"original_url":"https://example.com/some/long/page","title":"示例"}
        """;

    var request = HttpRequest.newBuilder(URI.create("${base}/api/links"))
        .header("Authorization", "Bearer kada_your_token")
        .header("Content-Type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(payload))
        .build();

    var response = client.send(request, HttpResponse.BodyHandlers.ofString());
    System.out.println(response.body());
    `),
  },
  {
    id: "python",
    label: "Python",
    example: (base) =>
      code(`
    import requests

    res = requests.post(
        "${base}/api/links",
        headers={"Authorization": "Bearer kada_your_token"},
        json={"original_url": "https://example.com/some/long/page", "title": "示例"},
        timeout=10,
    )
    print(res.json()["link"]["short_url"])
    `),
  },
  {
    id: "rust",
    label: "Rust",
    example: (base) =>
      code(`
    fn main() -> Result<(), Box<dyn std::error::Error>> {
        let client = reqwest::blocking::Client::new();
        let res: serde_json::Value = client
            .post("${base}/api/links")
            .bearer_auth("kada_your_token")
            .json(&serde_json::json!({
                "original_url": "https://example.com/some/long/page",
                "title": "示例"
            }))
            .send()?
            .json()?;

        println!("{}", res["link"]["short_url"]);
        Ok(())
    }
    `),
  },
  {
    id: "javascript",
    label: "JavaScript",
    example: (base) =>
      code(`
    const res = await fetch("${base}/api/links", {
      method: "POST",
      headers: {
        Authorization: "Bearer kada_your_token",
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        original_url: "https://example.com/some/long/page",
        title: "示例",
      }),
    });

    const { link } = await res.json();
    console.log(link.short_url);
    `),
  },
  {
    id: "typescript",
    label: "TypeScript",
    example: (base) =>
      code(`
    type Created = { link: { short_url: string } };

    const res = await fetch("${base}/api/links", {
      method: "POST",
      headers: {
        Authorization: "Bearer kada_your_token",
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        original_url: "https://example.com/some/long/page",
        title: "示例",
      }),
    });

    const { link }: Created = await res.json();
    console.log(link.short_url);
    `),
  },
  {
    id: "csharp",
    label: "C#",
    example: (base) =>
      code(`
    using System.Net.Http.Headers;
    using System.Text;

    using var http = new HttpClient();
    http.DefaultRequestHeaders.Authorization =
        new AuthenticationHeaderValue("Bearer", "kada_your_token");

    var payload = """
        {"original_url":"https://example.com/some/long/page","title":"示例"}
        """;

    var response = await http.PostAsync(
        "${base}/api/links",
        new StringContent(payload, Encoding.UTF8, "application/json"));

    Console.WriteLine(await response.Content.ReadAsStringAsync());
    `),
  },
];

export default function ApiPage() {
  const t = useT();
  const token = getToken();
  const [language, setLanguage] = useState(LANGUAGES[0].id);
  const active = LANGUAGES.find((item) => item.id === language) ?? LANGUAGES[0];
  const snippet = active.example(BASE);

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

  const copy = (text: string) => {
    void navigator.clipboard.writeText(text);
    toast.success(t("已复制"));
  };

  return (
    <div className="max-w-3xl space-y-6">
      <PageHeader
        title={t("API")}
        description={t("用令牌调用接口，在你自己的程序里创建短链、读取统计。")}
      />

      <section className="rounded-xl border border-line bg-canvas p-5">
        <div className="grid gap-5 sm:grid-cols-2">
          <div>
            <h2 className="text-sm font-semibold text-strong">{t("接口地址")}</h2>
            <p className="mt-1 font-mono text-sm text-body">{BASE}/api</p>
          </div>
          <div>
            <h2 className="text-sm font-semibold text-strong">{t("认证方式")}</h2>
            <p className="mt-1 text-sm text-body">
              {t("每个请求带上 Authorization: Bearer <令牌>。令牌在下面创建，只显示一次。")}
            </p>
          </div>
        </div>
      </section>

      <section className="space-y-3 rounded-xl border border-line bg-canvas p-5">
        <div className="flex items-center justify-between gap-3">
          <h2 className="text-sm font-semibold text-strong">{t("示例代码")}</h2>
          <button
            onClick={() => copy(snippet)}
            className="inline-flex items-center gap-1 text-xs text-muted transition-colors hover:text-strong"
          >
            <Copy className="size-3" />
            {t("复制")}
          </button>
        </div>

        {/* One row of languages: it is a choice between equals, and a select would hide how many there
            are behind a click. */}
        <div className="flex flex-wrap gap-1" role="group" aria-label={t("示例代码")}>
          {LANGUAGES.map((item) => (
            <button
              key={item.id}
              type="button"
              aria-pressed={item.id === active.id}
              onClick={() => setLanguage(item.id)}
              className={cn(
                "h-7 rounded-md px-2.5 text-xs font-medium transition-colors",
                item.id === active.id
                  ? "bg-brand-soft text-brand-ink"
                  : "text-muted hover:bg-muted-surface hover:text-strong",
              )}
            >
              {item.label}
            </button>
          ))}
        </div>

        <pre className="overflow-x-auto rounded-lg border border-line bg-subtle p-3 text-xs leading-relaxed text-body">
          <code>{snippet}</code>
        </pre>

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
          <Button onClick={createToken} disabled={creating || !newTokenName.trim()}>
            <Plus className="size-3.5" />
            {creating ? t("创建中...") : t("新建令牌")}
          </Button>
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
