// Catch-all handler that mounts Better Auth at /api/auth/*. Better Auth
// owns the wire format here; we plug in the post-signup org provisioning
// via a wrapper so the membership row lives in the same Postgres pool.

import { auth } from "@/lib/auth";
import { ensureOrganization } from "@/lib/onboarding";
import { toNextJsHandler } from "better-auth/next-js";

const handlers = toNextJsHandler(auth);

async function withOnboarding(req: Request): Promise<Response> {
  const url = new URL(req.url);
  const isSignup =
    url.pathname.endsWith("/sign-up/email") || url.pathname.endsWith("/sign-up");

  // Pass-through to Better Auth.
  const res = await handlers.POST(req);

  if (isSignup && res.status >= 200 && res.status < 300) {
    try {
      const cloned = res.clone();
      const body = (await cloned.json()) as { user?: { id?: string; email?: string; name?: string } };
      if (body.user?.id && body.user.email) {
        await ensureOrganization({
          userId: body.user.id,
          email: body.user.email,
          preferredName: body.user.name,
        });
      }
    } catch (err) {
      // Onboarding failed after the user row landed. We log for ops to
      // backfill, but do not 500 the signup — Better Auth has already
      // committed the user; the next /me call will reattempt provisioning
      // (see middleware).
      console.error("[onboarding] provision org failed", err);
    }
  }
  return res;
}

export const POST = withOnboarding;
export const GET = handlers.GET;
