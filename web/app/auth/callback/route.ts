import { OidcCallbackError, finishOidcLogin } from "@/lib/server/oidc";
import {
  clearHostCookie,
  hostCookie,
  LOGIN_COOKIE_NAME,
  readExactCookie,
  secureEqual,
  serverSessions,
  SessionCapacityError,
  SESSION_COOKIE_NAME,
  SESSION_MAX_AGE_SECONDS,
} from "@/lib/server/session";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

const noStoreHeaders = { "cache-control": "no-store, max-age=0" };

export async function GET(request: Request) {
  const callback = new URL(request.url);
  const states = callback.searchParams.getAll("state");
  const state = states.length === 1 ? states[0] : undefined;
  const loginBinding = readExactCookie(request, LOGIN_COOKIE_NAME);
  if (!state || !loginBinding || !secureEqual(state, loginBinding)) {
    return Response.json({ error: "invalid_state" }, { status: 400, headers: noStoreHeaders });
  }
  try {
    const login = await finishOidcLogin(request);
    const currentSessionId = readExactCookie(request, SESSION_COOKIE_NAME);
    const currentSession = currentSessionId ? serverSessions.peek(currentSessionId) : undefined;
    const session = serverSessions.create(login);
    if (currentSession) serverSessions.revoke(currentSession.id);
    const response = Response.json({ authenticated: true }, { headers: noStoreHeaders });
    response.headers.append("set-cookie", hostCookie(SESSION_COOKIE_NAME, session.id, SESSION_MAX_AGE_SECONDS));
    response.headers.append("set-cookie", clearHostCookie(LOGIN_COOKIE_NAME));
    return response;
  } catch (error) {
    const headers = new Headers(noStoreHeaders);
    headers.append("set-cookie", clearHostCookie(LOGIN_COOKIE_NAME));
    if (error instanceof SessionCapacityError) {
      return Response.json({ error: "session_unavailable" }, { status: 503, headers });
    }
    if (error instanceof OidcCallbackError) {
      return Response.json({ error: error.code }, { status: error.status, headers });
    }
    return Response.json({ error: "authentication_failed" }, { status: 401, headers });
  }
}
