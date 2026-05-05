import { http, HttpResponse } from "msw";

const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export const handlers = [
  http.get(`${API}/v1/me`, () =>
    HttpResponse.json({
      user: {
        id: "00000000-0000-0000-0000-000000000010",
        email: "owner@example.com",
        display_name: "Owner",
        locale: "en",
      },
      membership: {
        org_id: "00000000-0000-0000-0000-000000000020",
        role: "owner",
      },
    }),
  ),
  http.get(`${API}/v1/images`, () =>
    HttpResponse.json({ items: [], total: 0, limit: 50, offset: 0 }),
  ),
];
