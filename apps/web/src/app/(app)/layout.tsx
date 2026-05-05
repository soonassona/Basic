import { redirect } from "next/navigation";
import { auth } from "@/lib/auth";
import { headers } from "next/headers";
import { ensureOrganization } from "@/lib/onboarding";
import { Sidebar } from "@/components/sidebar";

export default async function AppLayout({ children }: { children: React.ReactNode }) {
  const session = await auth.api.getSession({ headers: await headers() });
  if (!session?.user) {
    redirect("/login");
  }

  // Idempotent provisioning: if signup's post-hook didn't land (cold start
  // or race), provision now so /me on the API never sees a user without a
  // membership (ADR-0003).
  await ensureOrganization({
    userId: session.user.id,
    email: session.user.email,
    preferredName: session.user.name ?? undefined,
  });

  return (
    <div className="grid min-h-[100dvh] grid-cols-[240px_1fr]">
      <Sidebar />
      <main className="px-8 py-8">{children}</main>
    </div>
  );
}
