// Thin client over the Go API. Forwards Better Auth's session cookie via
// `credentials: "include"` so the API can authenticate against the same
// session table.
import { env } from "./env";

export type ApiError = {
  status: number;
  code: string;
  message: string;
  requestId?: string;
};

export class ApiClientError extends Error implements ApiError {
  status: number;
  code: string;
  requestId?: string;
  constructor(err: ApiError) {
    super(err.message);
    this.status = err.status;
    this.code = err.code;
    this.requestId = err.requestId;
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const res = await fetch(`${env.API_URL}${path}`, {
    ...init,
    credentials: "include",
    headers: {
      Accept: "application/json",
      ...(init.body ? { "Content-Type": "application/json" } : {}),
      ...init.headers,
    },
  });
  if (!res.ok) {
    let body: { error?: { code?: string; message?: string; request_id?: string } } = {};
    try {
      body = (await res.json()) as typeof body;
    } catch {
      // empty body or non-JSON; fall through to defaults
    }
    throw new ApiClientError({
      status: res.status,
      code: body.error?.code ?? "unknown",
      message: body.error?.message ?? res.statusText,
      requestId: body.error?.request_id,
    });
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export type Me = {
  user: { id: string; email: string; display_name: string; locale: string };
  membership: { org_id: string; role: "owner" | "admin" | "annotator" | "viewer" };
};

export type ImageRecord = {
  id: string;
  org_id: string;
  status: "uploading" | "ready" | "errored" | "archived";
  storage_key: string;
  content_type: string;
  byte_size: number;
  width?: number;
  height?: number;
};

export type PresignResponse = {
  image: ImageRecord;
  upload: {
    url: string;
    method: string;
    headers: Record<string, string>;
    expires_at: string;
  };
};

export const api = {
  me: () => request<Me>("/v1/me"),
  listImages: (limit = 50) =>
    request<{ items: ImageRecord[]; total: number }>(`/v1/images?limit=${limit}`),
  presignUpload: (input: { content_type: string; byte_size: number }) =>
    request<PresignResponse>("/v1/images:presign", {
      method: "POST",
      body: JSON.stringify(input),
    }),
  confirmUpload: (id: string, input: { width: number; height: number; sha256?: string }) =>
    request<{ image: ImageRecord }>(`/v1/images/${id}/confirm`, {
      method: "POST",
      body: JSON.stringify(input),
    }),
};
