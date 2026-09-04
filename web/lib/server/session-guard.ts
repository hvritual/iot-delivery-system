import { readExactCookie, secureEqual, SESSION_COOKIE_NAME, type ServerSession, type ServerSessionStore } from "@/lib/server/session";

const SAFE_METHODS = new Set(["GET", "HEAD", "OPTIONS"]);

export type SessionGuardResult =
  | { ok: true; session: ServerSession }
  | { ok: false; reason: "unauthenticated" | "invalid_origin" | "invalid_csrf" };

export function guardSessionRequest(request: Request, store: ServerSessionStore, trustedOrigin?: string): SessionGuardResult {
  const sessionId = readExactCookie(request, SESSION_COOKIE_NAME);
  const session = sessionId ? store.peek(sessionId) : undefined;
  if (!session) return { ok: false, reason: "unauthenticated" };
  if (SAFE_METHODS.has(request.method.toUpperCase())) return touchSession(store, session.id);
  if (!trustedOrigin || !hasExactOrigin(request.headers.get("origin"), trustedOrigin)) return { ok: false, reason: "invalid_origin" };
  const csrfToken = request.headers.get("x-csrf-token");
  if (!csrfToken || !secureEqual(csrfToken, session.csrfToken)) return { ok: false, reason: "invalid_csrf" };
  return touchSession(store, session.id);
}

function touchSession(store: ServerSessionStore, id: string): SessionGuardResult {
  const session = store.read(id);
  return session ? { ok: true, session } : { ok: false, reason: "unauthenticated" };
}

function hasExactOrigin(origin: string | null, trustedOrigin: string): boolean {
  if (!origin) return false;
  try {
    return new URL(origin).origin === origin && origin === trustedOrigin;
  } catch {
    return false;
  }
}
