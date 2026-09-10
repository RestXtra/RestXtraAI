import { MOCK } from "@/lib/mock/enabled";
import { mockHandle } from "@/lib/mock/handler";

export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  if (MOCK) return mockHandle<T>(init?.method ?? "GET", path, init?.body ?? null);
  const response = await fetch(`/api${path}`, {
    ...init,
    credentials: "include",
    headers: {
      ...(init?.body ? { "Content-Type": "application/json" } : {}),
      ...(init?.headers as Record<string, string> | undefined),
    },
  });
  if (response.status === 401) {
    // 登录/初始化页上的 401 是预期（探测登录态/初始化态），不应整页跳回 /login，
    // 否则会形成「platformMy→401→跳 /login→reload→又 401」的刷新死循环。
    const path = typeof window !== "undefined" ? window.location.pathname : "";
    if (path !== "/login" && path !== "/setup") {
      window.location.href = "/login";
    }
    throw new Error("未授权");
  }
  if (!response.ok) {
    let detail = "";
    try {
      const body = (await response.json()) as { error?: string } | null;
      if (body?.error) detail = `: ${body.error}`;
    } catch {
      // Some integrations return plain-text error bodies.
    }
    throw new Error(`${init?.method ?? "GET"} ${path}: ${response.status}${detail}`);
  }
  if (response.status === 204) return undefined as T;
  return response.json();
}

export const get = <T>(path: string) => request<T>(path);
export const post = <T>(path: string, body?: unknown) =>
  request<T>(path, { method: "POST", body: body ? JSON.stringify(body) : undefined });
export const put = <T>(path: string, body?: unknown) =>
  request<T>(path, { method: "PUT", body: body ? JSON.stringify(body) : undefined });
export const patch = <T>(path: string, body?: unknown) =>
  request<T>(path, { method: "PATCH", body: body ? JSON.stringify(body) : undefined });
export const del = <T>(path: string) => request<T>(path, { method: "DELETE" });
