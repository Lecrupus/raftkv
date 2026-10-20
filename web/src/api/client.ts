import type { BenchParams, BenchResult, ClusterView, KVOp, KVResponse, NodeAction } from "./types";

/** A response the UI can show verbatim: HTTP status, parsed body, round-trip time. */
export interface ApiResult<T> {
  status: number;
  ok: boolean;
  body: T;
  ms: number;
}

/** Thrown for non-2xx control-API calls so callers can surface the server's message inline. */
export class ApiError extends Error {
  readonly status: number;
  constructor(message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<ApiResult<T>> {
  const t0 = performance.now();
  const res = await fetch(path, { cache: "no-store", ...init });
  const text = await res.text();
  let body: unknown = text;
  try {
    body = JSON.parse(text);
  } catch {
    // plain-text body (e.g. a proxy error page): keep it as a string
  }
  return { status: res.status, ok: res.ok, body: body as T, ms: performance.now() - t0 };
}

function errorText(body: unknown, status: number): string {
  if (body && typeof body === "object" && "error" in body && typeof body.error === "string") return body.error;
  return typeof body === "string" && body ? body : `HTTP ${status}`;
}

export async function getCluster(signal?: AbortSignal): Promise<ClusterView> {
  const r = await request<ClusterView>("/api/cluster", signal ? { signal } : {});
  if (!r.ok) throw new ApiError(errorText(r.body, r.status), r.status);
  return r.body;
}

export async function nodeAction(id: string, action: NodeAction): Promise<void> {
  const r = await request<unknown>(`/api/nodes/${encodeURIComponent(id)}/${action}`, { method: "POST" });
  if (!r.ok) throw new ApiError(errorText(r.body, r.status), r.status);
}

/**
 * Key-value call through the cluster gateway. Never throws on HTTP errors:
 * a 503 while a majority is down is correct (CP) behaviour the UI must show.
 */
export function kv(op: KVOp, key: string, value = ""): Promise<ApiResult<KVResponse | string>> {
  let url = `/api/kv/${encodeURIComponent(key)}`;
  const init: RequestInit = { method: op === "GET_STALE" ? "GET" : op };
  if (op === "GET_STALE") url += "?consistency=stale";
  if (op === "PUT") init.body = value;
  return request<KVResponse | string>(url, init);
}

export async function runBench(p: BenchParams): Promise<BenchResult> {
  const q = new URLSearchParams({ ops: String(p.ops), clients: String(p.clients), reads: String(p.reads) });
  const r = await request<BenchResult>(`/api/bench?${q}`, { method: "POST" });
  if (!r.ok) throw new ApiError(errorText(r.body, r.status), r.status);
  return r.body;
}
