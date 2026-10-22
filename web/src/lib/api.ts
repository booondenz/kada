// Resolves the API base URL.
import { getToken, type User } from "./auth";
//
// The three cases:
//
//   unset         -> same origin, which is what both environments are: the Vite dev server proxies /api to
//                    the Go API (see vite.config.ts) and nginx proxies it in production, so a relative URL
//                    reaches the API in both.
//   "same-origin" -> the same thing, named. CI and web/Dockerfile pass it so the intent is explicit in the
//                    build that ships.
//   a URL         -> that URL, for an API that lives somewhere else.
//
// Unset used to mean http://localhost:8080, and that made every development request cross-origin. A JSON
// body or a bearer token makes a request non-simple, so the browser sent a preflight; the API has no CORS
// middleware (it does not need one when everything shares an origin), so the preflight was answered 404 and
// the request behind it never left the page. The comment in vite.config.ts already said the frontend never
// deals with a second origin - the fallback here was the thing that disagreed with it.
const SAME_ORIGIN = "same-origin";

function resolveAPIURL(): string {
  const configured = import.meta.env.VITE_API_URL;
  if (!configured || configured === SAME_ORIGIN) return "";
  return configured;
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

/**
 * Performs a multipart upload.
 *
 * It is separate from `fetchAPI` because that one sets a JSON content type and serialises the body; the
 * browser has to set the multipart boundary itself, so the body is a FormData and the header is left alone.
 */
export async function uploadFile(
  path: string,
  token: string,
  file: File,
): Promise<{ upload: { id: number; url: string; filename: string; content_type: string; size: number } }> {
  const url = `${API_URL}${path}`;
  const body = new FormData();
  body.append("file", file);

  let res: Response;
  try {
    res = await fetch(url, { method: "POST", headers: { Authorization: `Bearer ${token}` }, body });
  } catch (cause) {
    throw new Error(`cannot reach the API at ${url}: ${cause instanceof Error ? cause.message : cause}`, {
      cause,
    });
  }

  const data = await readJSON(res, url);
  if (!res.ok) {
    throw new Error(data.error || `upload failed with status ${res.status}`);
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
// The app opens in a guest session (see `lib/session.ts`): a real account row whose phone number is still
// empty. Three calls turn that into a named account - the challenge the server demands before it spends an
// SMS, the code itself, and the claim - and they are the only auth endpoints the client speaks. Registration
// (phone + code + password) and password sign-in stay on the server, tested, for clients that are not this
// one; nothing here can reach them.
//
// `claim` deliberately has no password: the account exists already, the code proves the number, and asking
// somebody who has been using the product for a week to invent a password at that moment is the step they
// would abandon.

export const authAPI = {
  /** Opens a session for a visitor who has not given any identity yet. */
  guest: (): Promise<{ token: string; user: User }> =>
    fetchAPI("/api/auth/guest", { method: "POST" }),

  /** Issues the graphical challenge that sending an SMS code requires. */
  captcha: (): Promise<{ captcha_id: string; image: string }> =>
    fetchAPI("/api/auth/captcha"),

  /** Sends the verification code, once the challenge above has been answered. */
  sendCode: (phone: string, captchaId: string, captchaCode: string): Promise<unknown> =>
    fetchAPI("/api/auth/send-sms-code", {
      method: "POST",
      body: JSON.stringify({ phone, captcha_id: captchaId, captcha_code: captchaCode }),
    }),

  /**
   * Names the account this browser is already using: the phone number is written onto the guest row, or -
   * when the number already has an account - the session becomes that account's. Both outcomes answer a
   * token, because both are a sign-in.
   */
  claim: (token: string, phone: string, code: string): Promise<{ token: string; user: User }> =>
    fetchAPI("/api/auth/claim", {
      method: "POST",
      token,
      body: JSON.stringify({ phone, code }),
    }),
};

// ========== Links API ==========

/** One destination of a link's pool, as the owner submits it. */
export interface LinkTargetInput {
  url: string;
  label?: string;
  weight: number;
  daily_quota: number;
  enabled: boolean;
}

/** One destination as the API returns it, with the clicks that reached it. */
export interface LinkTarget {
  id: number;
  url: string;
  label?: string;
  weight: number;
  daily_quota: number;
  enabled: boolean;
  clicks: number;
}

export const linksAPI = {
  list: (token: string, page = 1, pageSize = 20, search = "", folderId = 0, tagId = 0, sort = "created_desc") => {
    const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) });
    if (search) params.set("search", search);
    if (folderId > 0) params.set("folder_id", String(folderId));
    if (tagId > 0) params.set("tag_id", String(tagId));
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
    tag_ids?: number[];
    utm_source?: string;
    utm_medium?: string;
    utm_campaign?: string;
    utm_term?: string;
    utm_content?: string;
    ios_url?: string;
    android_url?: string;
    /** Makes this a file link: the target is derived from the upload, not from original_url. */
    upload_id?: number;
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

  /** The link's pool of destinations, with the clicks each one took. */
  targets: (token: string, id: number) =>
    fetchAPI(`/api/links/${id}/targets`, { token }),

  /**
   * Replaces the pool whole. A target edited row by row would have no moment at which the change could be
   * recorded as a decision, which is what the audit keeps.
   */
  replaceTargets: (token: string, id: number, targets: LinkTargetInput[]) =>
    fetchAPI(`/api/links/${id}/targets`, {
      method: "PUT",
      token,
      body: JSON.stringify({ targets }),
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

// ========== Uploads API ==========

/**
 * Stores a file for a file link.
 *
 * The type is checked on the server, by content and not only by extension, so the `accept` list here is
 * convenience rather than the rule.
 */
export const uploadsAPI = {
  create: (token: string, file: File) => uploadFile("/api/uploads", token, file),
};

// ========== Reports API ==========

/**
 * Files a complaint about a short link.
 *
 * Unauthenticated on purpose: the caller is a visitor who was sent somewhere illegal, not an account here.
 */
export const reportsAPI = {
  create: (body: { short_code: string; reason: string; detail?: string; contact?: string }) =>
    fetchAPI("/api/reports", { method: "POST", body: JSON.stringify(body) }),
};

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
