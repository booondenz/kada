// Resolves the API base URL.
import { getToken, type User } from "./auth";
//
// The three cases are deliberately distinct, because an empty value cannot express them:
//
//   unset        -> the local Go API. This is the development default.
//   "same-origin" -> relative requests, served by whatever hosts the page: production runs nginx in front
//                    of both the frontend and the API, so /api/ is proxied by the same origin.
//   a URL        -> that URL, used whenever the API lives somewhere else.
//
// Why a sentinel and not an empty string: Vite replaces `import.meta.env.VITE_API_URL` with the value the
// bundle was built with, so "not configured" arrives as undefined and an empty configuration would be
// indistinguishable from a deliberate one. The sentinel keeps the two apart, and it names the same-origin
// case at the build site (the CI job and web/Dockerfile both pass it).
const SAME_ORIGIN = "same-origin";

function resolveAPIURL(): string {
  const configured = import.meta.env.VITE_API_URL;
  if (!configured) return "http://localhost:8080";
  return configured === SAME_ORIGIN ? "" : configured;
}

const API_URL = resolveAPIURL();

interface FetchOptions extends RequestInit {
  token?: string;
}

/**
 * Reads a response that is expected to be JSON.
 *
 * Calling `res.json()` on an HTML body throws `Unexpected token '<', "<!DOCTYPE "... is not valid JSON`,
 * which says nothing about what actually happened: the request probably never reached the Go API and was
 * answered by a proxy or a dev server instead. Checking the content type first turns that into an error that
 * names the URL, the status and the content type, so the cause is visible without opening DevTools.
 */
async function readJSON(res: Response, url: string) {
  const contentType = res.headers.get("content-type") ?? "(none)";

  if (!contentType.includes("application/json")) {
    const body = (await res.text()).slice(0, 200).replace(/\s+/g, " ");
    throw new Error(
      `${url} returned ${res.status} ${res.statusText} with content-type ${contentType} instead of JSON. ` +
        `Check that VITE_API_URL points at the API and that it is running. Body starts with: ${body}`
    );
  }

  return res.json();
}

async function fetchAPI(path: string, options: FetchOptions = {}) {
  const { token, ...fetchOptions } = options;
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...(options.headers as Record<string, string>),
  };

  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const url = `${API_URL}${path}`;

  let res: Response;
  try {
    res = await fetch(url, { ...fetchOptions, headers });
  } catch (cause) {
    // A network-level failure (API not running, wrong host, CORS preflight rejected) never produces a
    // Response at all, so it needs its own message rather than surfacing as a bare "Failed to fetch".
    throw new Error(`cannot reach the API at ${url}: ${cause instanceof Error ? cause.message : cause}`, {
      cause,
    });
  }

  const data = await readJSON(res, url);

  if (!res.ok) {
    throw new Error(data.error || `request failed with status ${res.status}`);
  }

  return data;
}

/** Performs a request that returns CSV rather than JSON, used by the link export. */
export async function fetchCSV(path: string, token: string): Promise<string> {
  const url = `${API_URL}${path}`;

  let res: Response;
  try {
    res = await fetch(url, { headers: { Authorization: `Bearer ${token}` } });
  } catch (cause) {
    throw new Error(`cannot reach the API at ${url}: ${cause instanceof Error ? cause.message : cause}`, {
      cause,
    });
  }

  if (!res.ok) {
    throw new Error(`export failed with status ${res.status}`);
  }

  return res.text();
}

// ========== Auth API ==========
//
// There is one call here, because the app no longer has a sign-in screen: it opens in a guest session (see
// `lib/session.ts`), and the account it runs in is a real row whose phone number is still empty. The phone
// endpoints on the server - captcha, send code, register, password login - are still registered and still
// tested; they are how a guest session gets claimed, and the client will speak them again when the claim
// step and the payment flow that needs it exist. They are not exposed here until something calls them, the
// same way the old email and WeChat routes were deleted rather than kept "just in case".

export const authAPI = {
  /** Opens a session for a visitor who has not given any identity yet. */
  guest: (): Promise<{ token: string; user: User }> =>
    fetchAPI("/api/auth/guest", { method: "POST" }),
};

// ========== Links API ==========

export const linksAPI = {
  list: (token: string, page = 1, pageSize = 20, search = "", folderId = 0, tagId = 0, workspaceId = 0, sort = "created_desc") => {
    const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) });
    if (search) params.set("search", search);
    if (folderId > 0) params.set("folder_id", String(folderId));
    if (tagId > 0) params.set("tag_id", String(tagId));
    if (workspaceId > 0) params.set("workspace_id", String(workspaceId));
    if (sort) params.set("sort", sort);
    return fetchAPI(`/api/links?${params.toString()}`, { token });
  },

  create: (token: string, data: {
    original_url: string;
    short_code?: string;
    title?: string;
    description?: string;
    domain?: string;
    password?: string;
    expires_at?: string;
    folder_id?: number;
    workspace_id?: number;
    tag_ids?: number[];
    utm_source?: string;
    utm_medium?: string;
    utm_campaign?: string;
    utm_term?: string;
    utm_content?: string;
    ios_url?: string;
    android_url?: string;
  }) =>
    fetchAPI("/api/links", {
      method: "POST",
      token,
      body: JSON.stringify(data),
    }),

  get: (token: string, id: string) =>
    fetchAPI(`/api/links/${id}`, { token }),

  update: (token: string, id: number, data: Record<string, unknown>) =>
    fetchAPI(`/api/links/${id}`, {
      method: "PATCH",
      token,
      body: JSON.stringify(data),
    }),

  delete: (token: string, id: number) =>
    fetchAPI(`/api/links/${id}`, {
      method: "DELETE",
      token,
    }),

  batchDelete: (token: string, ids: number[]) =>
    fetchAPI("/api/links/batch-delete", {
      method: "POST",
      token,
      body: JSON.stringify({ ids }),
    }),

  batchTag: (token: string, ids: number[], tagId: number) =>
    fetchAPI("/api/links/batch-tag", {
      method: "POST",
      token,
      body: JSON.stringify({ ids, tag_id: tagId }),
    }),

  preview: (token: string, url: string): Promise<{
    preview: { title: string; description: string; image_url: string; favicon_url: string };
  }> =>
    fetchAPI("/api/links/preview", {
      method: "POST",
      token,
      body: JSON.stringify({ url }),
    }),

  /** Downloads the user's links as CSV. */
  export: (token: string) => fetchCSV("/api/links/export", token),
};

// ========== Analytics API ==========

export const analyticsAPI = {
  overview: (token: string) =>
    fetchAPI("/api/analytics/overview", { token }),

  platforms: (token: string) =>
    fetchAPI("/api/analytics/platforms", { token }),

  /** Platform breakdown for a single link, used by the link detail page. */
  platformsForLink: (token: string, linkId: number): Promise<{ platforms: { platform: string; count: number }[] }> =>
    fetchAPI(`/api/analytics/platforms?link_id=${linkId}`, { token }),

  daily: (token: string) =>
    fetchAPI("/api/analytics/daily", { token }),

  /** Daily clicks for a single link, used by the link detail page. */
  dailyForLink: (token: string, linkId: number): Promise<{ daily: { date: string; count: number }[] }> =>
    fetchAPI(`/api/analytics/daily?link_id=${linkId}`, { token }),

  events: (token: string, page = 1, pageSize = 20) =>
    fetchAPI(`/api/analytics/events?page=${page}&page_size=${pageSize}`, { token }),

  customers: (token: string) =>
    fetchAPI("/api/analytics/customers", { token }),
};

// ========== Folders API ==========

export const foldersAPI = {
  list: (token: string) =>
    fetchAPI("/api/folders", { token }),

  create: (token: string, name: string) =>
    fetchAPI("/api/folders", {
      method: "POST",
      token,
      body: JSON.stringify({ name }),
    }),

  update: (token: string, id: number, name: string) =>
    fetchAPI(`/api/folders/${id}`, {
      method: "PATCH",
      token,
      body: JSON.stringify({ name }),
    }),

  delete: (token: string, id: number) =>
    fetchAPI(`/api/folders/${id}`, {
      method: "DELETE",
      token,
    }),
};

// ========== Tags API ==========

export const tagsAPI = {
  list: (token: string) =>
    fetchAPI("/api/tags", { token }),

  create: (token: string, name: string, color?: string) =>
    fetchAPI("/api/tags", {
      method: "POST",
      token,
      body: JSON.stringify({ name, color }),
    }),

  delete: (token: string, id: number) =>
    fetchAPI(`/api/tags/${id}`, {
      method: "DELETE",
      token,
    }),

  addToLink: (token: string, linkId: number, tagId: number) =>
    fetchAPI(`/api/links/${linkId}/tags`, {
      method: "POST",
      token,
      body: JSON.stringify({ tag_id: tagId }),
    }),

  removeFromLink: (token: string, linkId: number, tagId: number) =>
    fetchAPI(`/api/links/${linkId}/tags/${tagId}`, {
      method: "DELETE",
      token,
    }),
};

// ========== Domains API ==========

export const domainsAPI = {
  list: (token: string) =>
    fetchAPI("/api/domains", { token }),

  create: (token: string, name: string) =>
    fetchAPI("/api/domains", {
      method: "POST",
      token,
      body: JSON.stringify({ name }),
    }),

  verify: (token: string, id: number) =>
    fetchAPI(`/api/domains/${id}/verify`, {
      method: "POST",
      token,
    }),

  delete: (token: string, id: number) =>
    fetchAPI(`/api/domains/${id}`, {
      method: "DELETE",
      token,
    }),
};

// ========== API Tokens ==========

export const tokensAPI = {
  list: (token: string) =>
    fetchAPI("/api/api-tokens", { token }),

  create: (token: string, name: string) =>
    fetchAPI("/api/api-tokens", {
      method: "POST",
      token,
      body: JSON.stringify({ name }),
    }),

  delete: (token: string, id: number) =>
    fetchAPI(`/api/api-tokens/${id}`, {
      method: "DELETE",
      token,
    }),
};

// ========== Workspaces API ==========

export const workspacesAPI = {
  list: (token: string) =>
    fetchAPI("/api/workspaces", { token }),

  create: (token: string, name: string, slug: string) =>
    fetchAPI("/api/workspaces", {
      method: "POST",
      token,
      body: JSON.stringify({ name, slug }),
    }),

  update: (token: string, id: number, data: { name?: string; slug?: string }) =>
    fetchAPI(`/api/workspaces/${id}`, {
      method: "PATCH",
      token,
      body: JSON.stringify(data),
    }),

  delete: (token: string, id: number) =>
    fetchAPI(`/api/workspaces/${id}`, {
      method: "DELETE",
      token,
    }),
};

// ========== UTM Templates API ==========

export const utmAPI = {
  list: (token: string) =>
    fetchAPI("/api/utm-templates", { token }),

  create: (token: string, data: {
    name: string;
    utm_source?: string;
    utm_medium?: string;
    utm_campaign?: string;
    utm_term?: string;
    utm_content?: string;
  }) =>
    fetchAPI("/api/utm-templates", {
      method: "POST",
      token,
      body: JSON.stringify(data),
    }),

  delete: (token: string, id: number) =>
    fetchAPI(`/api/utm-templates/${id}`, {
      method: "DELETE",
      token,
    }),
};

// ========== AI Chat API ==========
// 通过 Go 网关（同源 /api/ai/*）访问 Python AI 服务：Go 用 JWT 认证后会把用户 id
// 注入 X-Kada-User-ID，前端不再直接接触 Python，也不传用户 id（防止伪造）。
// 注意：streamChat 不能用 fetchAPI —— 它是 JSON 专用的，SSE 流会被它当 JSON 解析抛错。
export const aiAPI = {
  // 对话：SSE 流式，返回 Response 让页面自己读流（不能用 readJSON）
  streamChat: (body: { conversation_id?: string; message: string }) =>
    fetch(`${API_URL}/api/ai/chat`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${getToken() ?? ""}`,
      },
      body: JSON.stringify(body),
    }),

  // 获取当前会话（最新一个）及其消息：进 AI 页时调用，自动续上上次聊天。
  // 路径按数据模型叫 conversations：表是 ai_conversations，返回体是 conversation_id。
  getCurrentConversation: () =>
    fetchAPI("/api/ai/conversations/current", { token: getToken() ?? undefined }),

  // 重新开始：后端删掉该用户的旧会话，新建一个空会话并返回其 id
  restartConversation: () =>
    fetchAPI("/api/ai/conversations/restart", {
      method: "POST",
      token: getToken() ?? undefined,
    }),
};
