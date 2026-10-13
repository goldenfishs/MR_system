export class ApiError extends Error {
  constructor(
    message: string,
    public status: number,
    public retryAfterSeconds?: number,
  ) {
    super(message);
    this.name = "ApiError";
  }
}
let sessionGeneration = 0;
let sessionActive = false;
export function setApiSession(active: boolean) {
  sessionGeneration++;
  sessionActive = active;
}
function retryAfterSeconds(response: Response, data?: any): number | undefined {
  const header = response.headers.get("Retry-After");
  if (header) {
    const seconds = /^\d+$/.test(header)
      ? Number(header)
      : (Date.parse(header) - Date.now()) / 1000;
    if (Number.isFinite(seconds)) return Math.max(0, Math.ceil(seconds));
  }
  const seconds = Number(data?.retryAfterSeconds ?? data?.cooldownSeconds);
  return Number.isFinite(seconds) && seconds >= 0
    ? Math.ceil(seconds)
    : undefined;
}
async function request<T>(
  path: string,
  method = "GET",
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  const requestSession = sessionGeneration;
  let response: Response;
  try {
    response = await fetch(`/api${path}`, {
      method,
      credentials: "same-origin",
      cache: "no-store",
      headers:
        body === undefined || body instanceof FormData
          ? {}
          : { "Content-Type": "application/json" },
      body:
        body === undefined
          ? undefined
          : body instanceof FormData
            ? body
            : JSON.stringify(body),
      signal: signal
        ? AbortSignal.any([
            signal,
            AbortSignal.timeout(body instanceof FormData ? 120000 : 20000),
          ])
        : AbortSignal.timeout(body instanceof FormData ? 120000 : 20000),
    });
  } catch {
    throw new ApiError(
      "连接失败，请检查网络后重试。你的输入仍保留在页面中。",
      0,
    );
  }
  let data: any;
  try {
    data = await response.json();
  } catch {
    throw new ApiError(
      "服务返回了无效响应，请稍后重试。",
      response.status,
      retryAfterSeconds(response),
    );
  }
  if (!response.ok) {
    if (response.status === 401 && !path.startsWith("/auth/") && sessionActive && requestSession === sessionGeneration)
      window.dispatchEvent(new Event("session-expired"));
    throw new ApiError(
      data.error || "操作失败，请稍后重试。",
      response.status,
      retryAfterSeconds(response, data),
    );
  }
  return data as T;
}
export const api = {
  upload: <T>(path: string, body: FormData, signal?: AbortSignal) =>
    request<T>(path, "POST", body, signal),
  get: <T>(path: string) => request<T>(path),
  post: <T>(path: string, body?: unknown) => request<T>(path, "POST", body),
  patch: <T>(path: string, body: unknown) => request<T>(path, "PATCH", body),
  delete: <T>(path: string) => request<T>(path, "DELETE"),
};
