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
