import { resolveTrustedApplicationOrigin } from "@/lib/server/application-origin";
import { guardSessionRequest } from "@/lib/server/session-guard";
import { clearHostCookie, serverSessions, SESSION_COOKIE_NAME } from "@/lib/server/session";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

const noStoreHeaders = { "cache-control": "no-store, max-age=0", vary: "Cookie" };

export async function POST(request: Request) {
  let trustedOrigin: string;
  try {
    trustedOrigin = resolveTrustedApplicationOrigin(request, process.env);
  } catch {
    return Response.json({ error: "configuration_error" }, { status: 500, headers: noStoreHeaders });
  }
  const result = guardSessionRequest(request, serverSessions, trustedOrigin);
  if (!result.ok) {
    const status = result.reason === "unauthenticated" ? 401 : 403;
    return Response.json({ error: status === 401 ? "unauthenticated" : "forbidden" }, { status, headers: noStoreHeaders });
  }
  serverSessions.revoke(result.session.id);
  const headers = new Headers(noStoreHeaders);
  headers.append("set-cookie", clearHostCookie(SESSION_COOKIE_NAME));
  return new Response(null, { status: 204, headers });
}
