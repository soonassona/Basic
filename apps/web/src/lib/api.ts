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
  /** Server-provided extras (e.g. current_version on 409 conflict). */
  details?: Record<string, unknown>;

  constructor(err: ApiError & { details?: Record<string, unknown> }) {
    super(err.message);
    this.status = err.status;
    this.code = err.code;
    this.requestId = err.requestId;
    this.details = err.details;
  }

  /** Returns the server's current_version when this is a 409 conflict from a
   * PATCH /annotations endpoint. Lets callers reload + retry without parsing. */
  get currentVersion(): number | undefined {
    const v = this.details?.current_version;
    return typeof v === "number" ? v : undefined;
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
    let body: {
      error?: { code?: string; message?: string; request_id?: string } & Record<string, unknown>;
    } = {};
    try {
      body = (await res.json()) as typeof body;
    } catch {
      // empty body or non-JSON; fall through to defaults
    }
    const { code, message, request_id, ...rest } = body.error ?? {};
    throw new ApiClientError({
      status: res.status,
      code: code ?? "unknown",
      message: message ?? res.statusText,
      requestId: request_id,
      details: Object.keys(rest).length > 0 ? rest : undefined,
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

// ── Annotations (Phase 4 spec §10) ───────────────────────────────────────────

export type AnnotationKind = "bbox" | "polygon" | "point" | "mask";

export type Annotation = {
  id: string;
  annotation_set_id: string;
  label_id: string | null;
  kind: AnnotationKind;
  /** Geometry shape depends on kind: {x,y,w,h} for bbox, {points: [[x,y],...]}
   * for polygon, {x,y} for point, {storage_key} for mask. */
  geometry: unknown;
  human_accepted: boolean | null;
};

export type AnnotationSet = {
  id: string;
  image_id: string;
  version: number;
  notes?: string;
  annotations: Annotation[];
};

export type AnnotationPatch = {
  geometry?: unknown;
  label_id?: string | null;
  human_accepted?: boolean | null;
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

  /** GET /v1/annotation-sets/:image_id — version doubles as the If-Match
   * value for subsequent annotation PATCHes. */
  getAnnotationSet: (imageId: string) =>
    request<AnnotationSet>(`/v1/annotation-sets/${imageId}`),

  /** PATCH /v1/annotations/:id — sends If-Match per spec §10. On 409 the
   * thrown ApiClientError exposes `.currentVersion` for the conflict UI. */
  patchAnnotation: (id: string, ifMatch: number, patch: AnnotationPatch) =>
    request<{ annotation: Annotation; new_version: number }>(
      `/v1/annotations/${id}`,
      {
        method: "PATCH",
        headers: { "If-Match": String(ifMatch) },
        body: JSON.stringify(patch),
      },
    ),
};
