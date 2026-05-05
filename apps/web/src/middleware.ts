// Edge middleware redirects unauthenticated users away from /dashboard,
// /images, /studio, etc. Authentication state lives in the Better Auth
// session cookie; the actual session validity is checked on the API
// gateway side.

import { NextResponse, type NextRequest } from "next/server";

const PROTECTED = ["/dashboard", "/images", "/studio", "/queue", "/jobs", "/dataset", "/models", "/analytics", "/settings"];

export function middleware(req: NextRequest) {
  const { pathname } = req.nextUrl;
  const isProtected = PROTECTED.some((p) => pathname === p || pathname.startsWith(`${p}/`));
  if (!isProtected) return NextResponse.next();

  const cookie = req.cookies.get("better-auth.session_token");
  if (!cookie) {
    const login = new URL("/login", req.url);
    login.searchParams.set("next", pathname);
    return NextResponse.redirect(login);
  }
  return NextResponse.next();
}

export const config = {
  matcher: ["/dashboard/:path*", "/images/:path*", "/studio/:path*", "/queue/:path*", "/jobs/:path*", "/dataset/:path*", "/models/:path*", "/analytics/:path*", "/settings/:path*"],
};
