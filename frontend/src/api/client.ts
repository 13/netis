const STORAGE_KEY = "netis_token";

export class ApiError extends Error {
  constructor(public status: number, public detail: unknown, message: string) {
    super(message);
  }
}

function getToken(): string | null {
  return localStorage.getItem(STORAGE_KEY);
}

export function setToken(token: string | null) {
  if (token === null) localStorage.removeItem(STORAGE_KEY);
  else localStorage.setItem(STORAGE_KEY, token);
}

async function request<T>(
  method: string,
  path: string,
  opts: { json?: unknown; form?: FormData; query?: Record<string, string | number | boolean | undefined> } = {},
): Promise<T> {
  const headers: Record<string, string> = {};
  const token = getToken();
  if (token) headers.Authorization = `Bearer ${token}`;

  let body: BodyInit | undefined;
  if (opts.json !== undefined) {
    headers["Content-Type"] = "application/json";
    body = JSON.stringify(opts.json);
  } else if (opts.form) {
    body = opts.form;
  }

  let url = path;
  if (opts.query) {
    const qs = new URLSearchParams();
    for (const [k, v] of Object.entries(opts.query)) {
      if (v !== undefined) qs.set(k, String(v));
    }
    const s = qs.toString();
    if (s) url += `?${s}`;
  }

  const res = await fetch(url, { method, headers, body });
  if (res.status === 204) return undefined as T;
  const ct = res.headers.get("content-type") || "";
  const payload = ct.includes("application/json") ? await res.json() : await res.text();

  if (!res.ok) {
    const message =
      typeof payload === "object" && payload && "detail" in payload
        ? typeof payload.detail === "string"
          ? payload.detail
          : JSON.stringify(payload.detail)
        : `HTTP ${res.status}`;
    throw new ApiError(res.status, payload, message);
  }
  return payload as T;
}

export const api = {
  get: <T>(path: string, query?: Record<string, string | number | boolean | undefined>) =>
    request<T>("GET", path, { query }),
  post: <T>(path: string, json?: unknown) => request<T>("POST", path, { json }),
  postForm: <T>(path: string, form: FormData) => request<T>("POST", path, { form }),
  patch: <T>(path: string, json?: unknown) => request<T>("PATCH", path, { json }),
  delete: <T>(path: string) => request<T>("DELETE", path),
};
