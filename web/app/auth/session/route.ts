import { guardSessionRequest } from "@/lib/server/session-guard";
import { clearHostCookie, serverSessions, SESSION_COOKIE_NAME } from "@/lib/server/session";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

const noStoreHeaders = { "cache-control": "no-store, max-age=0", vary: "Cookie" };

export async function GET(request: Request) {
  const result = guardSessionRequest(request, serverSessions);
  if (!result.ok) {
    const headers = new Headers(noStoreHeaders);
    headers.append("set-cookie", clearHostCookie(SESSION_COOKIE_NAME));
    return Response.json({ error: "unauthenticated" }, { status: 401, headers });
  }
  return Response.json({ authenticated: true, csrfToken: result.session.csrfToken }, { headers: noStoreHeaders });
}
