// Studio route group — full-bleed (no app sidebar) but still gated by auth
// + organization provisioning, exactly like the (app) layout. Phase 4 §10
// requires ≥70% of the viewport for the canvas, so the 240px global rail
// is dropped here.
import { redirect } from "next/navigation";
import { headers } from "next/headers";

import { auth } from "@/lib/auth";
import { ensureOrganization } from "@/lib/onboarding";

export default async function StudioLayout({ children }: { children: React.ReactNode }) {
  const session = await auth.api.getSession({ headers: await headers() });
  if (!session?.user) {
    redirect("/login");
  }
  await ensureOrganization({
    userId: session.user.id,
    email: session.user.email,
    preferredName: session.user.name ?? undefined,
  });

  return <div className="h-[100dvh] w-full overflow-hidden">{children}</div>;
}
