import { cookieNameCount, LOCAL_CSRF_COOKIE_NAME, LOCAL_SESSION_COOKIE_NAME, readLocalCurrentSession } from "@/lib/server/local-auth-session";
import { guardSessionRequest } from "@/lib/server/session-guard";
import { clearHostCookie, serverSessions, SESSION_COOKIE_NAME } from "@/lib/server/session";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

const noStoreHeaders = { "cache-control": "no-store, max-age=0", vary: "Cookie" };

export async function GET(request: Request) {
  const localCookies = cookieNameCount(request, LOCAL_SESSION_COOKIE_NAME);
  const oidcCookies = cookieNameCount(request, SESSION_COOKIE_NAME);

  if (localCookies > 0 && oidcCookies > 0) return unauthenticated(true);

  if (localCookies > 0) {
    const current = await readLocalCurrentSession(request, process.env);
    if (!current.ok) {
      if (current.reason === "unavailable") {
        return Response.json({ error: "session_unavailable" }, { status: 503, headers: noStoreHeaders });
      }
      return unauthenticated(true);
    }
    return Response.json({ authenticated: true, csrfToken: current.session.csrfToken }, { headers: noStoreHeaders });
  }

  const result = guardSessionRequest(request, serverSessions);
  if (!result.ok) return unauthenticated(false);
  return Response.json({ authenticated: true, csrfToken: result.session.csrfToken }, { headers: noStoreHeaders });
}

function unauthenticated(clearLocal: boolean): Response {
  const headers = new Headers(noStoreHeaders);
  if (clearLocal) {
    headers.append("set-cookie", clearLocalCookie(LOCAL_SESSION_COOKIE_NAME, true));
    headers.append("set-cookie", clearLocalCookie(LOCAL_CSRF_COOKIE_NAME, false));
  } else {
    headers.append("set-cookie", clearHostCookie(SESSION_COOKIE_NAME));
  }
  return Response.json({ error: "unauthenticated" }, { status: 401, headers });
}

function clearLocalCookie(name: string, httpOnly: boolean): string {
  return `${name}=; Max-Age=0; Path=/; ${httpOnly ? "HttpOnly; " : ""}Secure; SameSite=Strict`;
}
