import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import { api, ApiClientError } from "./api";
import { server } from "@/test/msw-server";

const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

describe("api client", () => {
  it("returns the authenticated user", async () => {
    const me = await api.me();
    expect(me.user.email).toBe("owner@example.com");
    expect(me.membership.role).toBe("owner");
  });

  it("getImage returns the record + a presigned download URL", async () => {
    server.use(
      http.get(`${API}/v1/images/img-1`, () =>
        HttpResponse.json({
          image: {
            id: "img-1",
            org_id: "org-1",
            status: "ready",
            storage_key: "orgs/org-1/images/img-1.png",
            content_type: "image/png",
            byte_size: 1024,
            width: 800,
            height: 600,
          },
          download: {
            url: "https://r2.example/orgs/org-1/images/img-1.png?sig=abc",
            method: "GET",
            headers: {},
            expires_at: "2026-05-10T12:00:00Z",
          },
        }),
      ),
    );

    const out = await api.getImage("img-1");
    expect(out.image.id).toBe("img-1");
    expect(out.download.method).toBe("GET");
    expect(out.download.url).toContain("?sig=abc");
  });

  it("translates server errors into ApiClientError", async () => {
    server.use(
      http.get(`${API}/v1/me`, () =>
        HttpResponse.json(
          { error: { code: "unauthorized", message: "missing session", request_id: "req-1" } },
          { status: 401 },
        ),
      ),
    );

    await expect(api.me()).rejects.toMatchObject({
      status: 401,
      code: "unauthorized",
      requestId: "req-1",
    });

    server.use(
      http.get(`${API}/v1/me`, () => new HttpResponse(null, { status: 503 })),
    );
    await expect(api.me()).rejects.toBeInstanceOf(ApiClientError);
  });
});

describe("annotation client (Phase 4 spec §10)", () => {
  const imageId = "00000000-0000-0000-0000-000000000aaa";
  const annId = "00000000-0000-0000-0000-000000000bbb";

  it("listLabels returns the org's label catalog", async () => {
    server.use(
      http.get(`${API}/v1/labels`, () =>
        HttpResponse.json({
          items: [
            { id: "lbl-1", name: "car", color: "#4a8ff5", created_at: "2026-05-01T00:00:00Z" },
            { id: "lbl-2", name: "person", color: "#3ecf8e", created_at: "2026-05-01T00:00:00Z" },
          ],
        }),
      ),
    );

    const out = await api.listLabels();
    expect(out.items).toHaveLength(2);
    expect(out.items[0].name).toBe("car");
    expect(out.items[1].color).toBe("#3ecf8e");
  });

  it("getAnnotationSet returns the set with version", async () => {
    server.use(
      http.get(`${API}/v1/annotation-sets/${imageId}`, () =>
        HttpResponse.json({
          id: "set-1",
          image_id: imageId,
          version: 7,
          annotations: [
            {
              id: annId,
              annotation_set_id: "set-1",
              label_id: null,
              kind: "bbox",
              geometry: { x: 10, y: 20, w: 100, h: 50 },
              human_accepted: null,
            },
          ],
        }),
      ),
    );

    const set = await api.getAnnotationSet(imageId);
    expect(set.version).toBe(7);
    expect(set.annotations).toHaveLength(1);
    expect(set.annotations[0].kind).toBe("bbox");
  });

  it("patchAnnotation sends If-Match header and returns new_version", async () => {
    let capturedIfMatch: string | null = null;
    server.use(
      http.patch(`${API}/v1/annotations/${annId}`, ({ request }) => {
        capturedIfMatch = request.headers.get("If-Match");
        return HttpResponse.json({
          annotation: {
            id: annId,
            annotation_set_id: "set-1",
            label_id: null,
            kind: "bbox",
            geometry: { x: 11, y: 21, w: 100, h: 50 },
            human_accepted: true,
          },
          new_version: 8,
        });
      }),
    );

    const out = await api.patchAnnotation(annId, 7, { human_accepted: true });
    expect(capturedIfMatch).toBe("7");
    expect(out.new_version).toBe(8);
    expect(out.annotation.human_accepted).toBe(true);
  });

  it("createAnnotation POSTs with If-Match and returns new_version", async () => {
    let capturedIfMatch: string | null = null;
    let capturedBody: unknown = null;
    server.use(
      http.post(`${API}/v1/annotations`, async ({ request }) => {
        capturedIfMatch = request.headers.get("If-Match");
        capturedBody = await request.json();
        return HttpResponse.json(
          {
            annotation: {
              id: "00000000-0000-0000-0000-000000000ccc",
              annotation_set_id: "set-1",
              label_id: null,
              kind: "bbox",
              geometry: { x: 1, y: 2, w: 3, h: 4 },
              human_accepted: null,
            },
            new_version: 8,
          },
          { status: 201 },
        );
      }),
    );

    const out = await api.createAnnotation(7, {
      annotation_set_id: "set-1",
      kind: "bbox",
      geometry: { x: 1, y: 2, w: 3, h: 4 },
    });
    expect(capturedIfMatch).toBe("7");
    expect(capturedBody).toMatchObject({ annotation_set_id: "set-1", kind: "bbox" });
    expect(out.new_version).toBe(8);
    expect(out.annotation.id).toBe("00000000-0000-0000-0000-000000000ccc");
  });

  it("deleteAnnotation sends DELETE with If-Match and returns new_version", async () => {
    let capturedIfMatch: string | null = null;
    server.use(
      http.delete(`${API}/v1/annotations/${annId}`, ({ request }) => {
        capturedIfMatch = request.headers.get("If-Match");
        return HttpResponse.json({ new_version: 9 });
      }),
    );

    const out = await api.deleteAnnotation(annId, 8);
    expect(capturedIfMatch).toBe("8");
    expect(out.new_version).toBe(9);
  });

  it("patchAnnotation 409 conflict surfaces current_version on the error", async () => {
    server.use(
      http.patch(`${API}/v1/annotations/${annId}`, () =>
        HttpResponse.json(
          {
            error: {
              code: "conflict",
              message: "If-Match version is stale; reload and retry",
              current_version: 9,
              request_id: "req-2",
            },
          },
          { status: 409 },
        ),
      ),
    );

    try {
      await api.patchAnnotation(annId, 7, { human_accepted: true });
      throw new Error("expected ApiClientError");
    } catch (err) {
      expect(err).toBeInstanceOf(ApiClientError);
      const e = err as ApiClientError;
      expect(e.status).toBe(409);
      expect(e.code).toBe("conflict");
      expect(e.currentVersion).toBe(9);
      expect(e.requestId).toBe("req-2");
    }
  });
});
