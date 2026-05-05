// Consumer-driven contract for the web → api boundary.
//
// What's covered: the four request shapes the web's `api.ts` actually
// sends today (`me`, `listImages`, `presignUpload`, `confirmUpload`).
// We deliberately do not invent interactions for endpoints the web does
// not yet call — that would defeat the purpose of consumer-driven
// contracts. New consumer methods grow the pact organically.
//
// Output: writes a pact JSON to `apps/web/pacts/visionloop-web-visionloop-api.json`.
// The Go provider verifies that file under `make test-pact-provider`.
//
// Run: `bun run test:pact:consumer` (vitest config at vitest.pact.config.ts).

import path from "node:path";
import { describe, it, expect, beforeEach, vi } from "vitest";
import { PactV4, MatchersV3, SpecificationVersion } from "@pact-foundation/pact";

const { like, regex, string, integer, boolean, eachLike } = MatchersV3;

const PACT_DIR = path.resolve(__dirname, "..", "..", "pacts");

const provider = new PactV4({
  consumer: "visionloop-web",
  provider: "visionloop-api",
  dir: PACT_DIR,
  logLevel: "warn",
  spec: SpecificationVersion.SPECIFICATION_VERSION_V4,
});

// Each test re-imports the api module after stubbing NEXT_PUBLIC_API_URL,
// because env.ts captures process.env at module load — see lib/env.ts.
async function loadApiAt(baseUrl: string): Promise<typeof import("@/lib/api")> {
  vi.resetModules();
  vi.stubEnv("NEXT_PUBLIC_API_URL", baseUrl);
  return await import("@/lib/api");
}

beforeEach(() => {
  vi.unstubAllEnvs();
});

const uuidRegex = "[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}";

describe("Pact consumer: visionloop-web → visionloop-api", () => {
  it("GET /v1/me — owner with a primary membership", async () => {
    await provider
      .addInteraction()
      .given("a session for an owner of an organisation")
      .uponReceiving("a request for the current caller")
      .withRequest("GET", "/v1/me", (b) => {
        b.headers({ Accept: "application/json" });
      })
      .willRespondWith(200, (b) => {
        b.headers({ "Content-Type": regex("application/json.*", "application/json") });
        b.jsonBody({
          user: {
            id: regex(uuidRegex, "00000000-0000-0000-0000-000000000010"),
            email: string("owner@example.com"),
            display_name: string("Owner"),
            email_verified: boolean(true),
            locale: string("en"),
          },
          membership: {
            org_id: regex(uuidRegex, "00000000-0000-0000-0000-000000000020"),
            role: regex("owner|admin|annotator|viewer", "owner"),
          },
        });
      })
      .executeTest(async (mockServer) => {
        const { api } = await loadApiAt(mockServer.url);
        const me = await api.me();
        expect(me.membership.role).toBe("owner");
        expect(me.user.email).toBe("owner@example.com");
      });
  });

  it("GET /v1/images — first page for an empty org", async () => {
    await provider
      .addInteraction()
      .given("an org with no images")
      .uponReceiving("a request for the first page of images")
      .withRequest("GET", "/v1/images", (b) => {
        b.headers({ Accept: "application/json" });
        b.query({ limit: "50" });
      })
      .willRespondWith(200, (b) => {
        b.headers({ "Content-Type": regex("application/json.*", "application/json") });
        b.jsonBody({
          items: [],
          total: integer(0),
          limit: integer(50),
          offset: integer(0),
        });
      })
      .executeTest(async (mockServer) => {
        const { api } = await loadApiAt(mockServer.url);
        const page = await api.listImages(50);
        expect(page.total).toBe(0);
        expect(page.items).toEqual([]);
      });
  });

  it("POST /v1/images:presign — annotator reserves an upload slot", async () => {
    await provider
      .addInteraction()
      .given("a session for an annotator with quota for one image")
      .uponReceiving("a presign request for a small JPEG")
      .withRequest("POST", "/v1/images:presign", (b) => {
        b.headers({ "Content-Type": "application/json", Accept: "application/json" });
        b.jsonBody({
          content_type: like("image/jpeg"),
          byte_size: integer(1024),
        });
      })
      .willRespondWith(201, (b) => {
        b.headers({ "Content-Type": regex("application/json.*", "application/json") });
        b.jsonBody({
          image: {
            id: regex(uuidRegex, "00000000-0000-0000-0000-0000000000a1"),
            org_id: regex(uuidRegex, "00000000-0000-0000-0000-000000000020"),
            status: regex("uploading|ready|errored|archived", "uploading"),
            storage_key: string("orgs/00000000-0000-0000-0000-000000000020/images/00000000-0000-0000-0000-0000000000a1.jpg"),
            content_type: string("image/jpeg"),
            byte_size: integer(1024),
          },
          upload: {
            url: regex("https?://.+", "http://localhost:9000/visionloop/orgs/abc.jpg?X-Amz-Signature=…"),
            method: regex("PUT", "PUT"),
            headers: like({ "Content-Type": "image/jpeg" }),
            expires_at: regex("\\d{4}-\\d{2}-\\d{2}T\\d{2}:\\d{2}:\\d{2}Z", "2026-05-05T20:30:00Z"),
          },
        });
      })
      .executeTest(async (mockServer) => {
        const { api } = await loadApiAt(mockServer.url);
        const res = await api.presignUpload({ content_type: "image/jpeg", byte_size: 1024 });
        expect(res.image.status).toBe("uploading");
        expect(res.upload.method).toBe("PUT");
      });
  });

  it("POST /v1/images/:id/confirm — finalize an uploaded image", async () => {
    await provider
      .addInteraction()
      .given("an image in uploading state owned by the caller")
      .uponReceiving("a confirm request with width and height")
      .withRequest("POST", "/v1/images/00000000-0000-0000-0000-0000000000a1/confirm", (b) => {
        b.headers({ "Content-Type": "application/json", Accept: "application/json" });
        b.jsonBody({
          width: integer(1920),
          height: integer(1080),
        });
      })
      .willRespondWith(200, (b) => {
        b.headers({ "Content-Type": regex("application/json.*", "application/json") });
        b.jsonBody({
          image: {
            id: regex(uuidRegex, "00000000-0000-0000-0000-0000000000a1"),
            org_id: regex(uuidRegex, "00000000-0000-0000-0000-000000000020"),
            status: regex("ready", "ready"),
            storage_key: string("orgs/00000000-0000-0000-0000-000000000020/images/00000000-0000-0000-0000-0000000000a1.jpg"),
            content_type: string("image/jpeg"),
            byte_size: integer(1024),
            width: integer(1920),
            height: integer(1080),
          },
        });
      })
      .executeTest(async (mockServer) => {
        const { api } = await loadApiAt(mockServer.url);
        const res = await api.confirmUpload("00000000-0000-0000-0000-0000000000a1", { width: 1920, height: 1080 });
        expect(res.image.status).toBe("ready");
        expect(res.image.width).toBe(1920);
      });
  });
});
