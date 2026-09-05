import { cookieNameCount, LOCAL_CSRF_COOKIE_NAME, LOCAL_SESSION_COOKIE_NAME, readLocalCurrentSession } from "@/lib/server/local-auth-session";
import { guardSessionRequest } from "@/lib/server/session-guard";
import { clearHostCookie, serverSessions, SESSION_COOKIE_NAME } from "@/lib/server/session";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

const noStoreHeaders = { "cache-control": "no-store, max-age=0", vary: "Cookie" };

type SessionFamily = "local" | "oidc" | "both";

export async function GET(request: Request) {
  const localCookies = cookieNameCount(request, LOCAL_SESSION_COOKIE_NAME);
  const oidcCookies = cookieNameCount(request, SESSION_COOKIE_NAME);

  if (localCookies > 0 && oidcCookies > 0) return unauthenticated("both");

  if (localCookies > 0) {
    const current = await readLocalCurrentSession(request, process.env);
    if (!current.ok) {
      if (current.reason === "unavailable") {
        return Response.json({ error: "session_unavailable" }, { status: 503, headers: noStoreHeaders });
      }
      return unauthenticated("local");
    }
    const headers = new Headers(noStoreHeaders);
    if (current.setCookie) headers.append("set-cookie", current.setCookie);
    return Response.json({ authenticated: true, csrfToken: current.session.csrfToken }, { headers });
  }

  const result = guardSessionRequest(request, serverSessions);
  if (!result.ok) return unauthenticated("oidc");
  return Response.json({ authenticated: true, csrfToken: result.session.csrfToken }, { headers: noStoreHeaders });
}

function unauthenticated(family: SessionFamily): Response {
  const headers = new Headers(noStoreHeaders);
  if (family === "local" || family === "both") {
    headers.append("set-cookie", clearLocalCookie(LOCAL_SESSION_COOKIE_NAME, true));
    headers.append("set-cookie", clearLocalCookie(LOCAL_CSRF_COOKIE_NAME, false));
  }
  if (family === "oidc" || family === "both") {
    headers.append("set-cookie", clearHostCookie(SESSION_COOKIE_NAME));
  }
  return Response.json({ error: "unauthenticated" }, { status: 401, headers });
}

function clearLocalCookie(name: string, httpOnly: boolean): string {
  return `${name}=; Max-Age=0; Path=/; ${httpOnly ? "HttpOnly; " : ""}Secure; SameSite=Strict`;
}
