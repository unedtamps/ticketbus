import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

const protectedRoutes = ["/dashboard", "/events", "/admin", "/checkout"];
const authRoutes = ["/login", "/register"];
const publicEventPattern = /^\/events\/.+$/;

export function proxy(request: NextRequest) {
  const { pathname } = request.nextUrl;
  const token = request.cookies.get("access_token")?.value;

  // Public event detail pages — allow through
  if (publicEventPattern.test(pathname)) {
    return NextResponse.next();
  }

  // Auth pages — redirect to dashboard if already logged in
  if (authRoutes.includes(pathname)) {
    if (token) {
      return NextResponse.redirect(new URL("/dashboard", request.url));
    }
    return NextResponse.next();
  }

  // Protected routes — redirect to login if no token
  if (protectedRoutes.some(r => pathname === r || pathname.startsWith(r + "/"))) {
    if (!token) {
      const loginUrl = new URL("/login", request.url);
      loginUrl.searchParams.set("from", pathname);
      return NextResponse.redirect(loginUrl);
    }
  }

  return NextResponse.next();
}

export const config = {
  matcher: [
    "/((?!_next/static|_next/image|favicon.ico|.*\\.(?:svg|png|jpg|jpeg|gif|webp)$).*)",
  ],
};
