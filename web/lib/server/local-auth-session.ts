import { buildRuntimeUrl, type RuntimeEnvironment } from "./runtime-proxy";
import { readExactCookie } from "./session";

export const LOCAL_SESSION_COOKIE_NAME = "__Host-iotd_local_session";
export const LOCAL_CSRF_COOKIE_NAME = "__Host-iotd_local_csrf";

export type LocalCurrentSession = Readonly<{
  accessToken: string;
  csrfToken: string;
  organizationId: string;
  userId: string;
}>;

export type LocalCurrentResult =
  | Readonly<{ ok: true; session: LocalCurrentSession; setCookie?: string }>
  | Readonly<{ ok: false; reason: "unauthenticated" | "unavailable" }>;

export function cookieNameCount(request: Request, name: string): number {
  const header = request.headers.get("cookie");
  if (!header) return 0;
  let count = 0;
  for (const segment of header.split(";")) {
    const pair = segment.trim();
    const separator = pair.indexOf("=");
    const cookieName = separator < 0 ? pair : pair.slice(0, separator);
    if (cookieName === name) count += 1;
  }
  return count;
}

export function localSessionCookie(request: Request): string | undefined {
  return readExactCookie(request, LOCAL_SESSION_COOKIE_NAME);
}

// readLocalCurrentSession performs a server-to-server read of YU-26 current.
// The browser never supplies an access JWT to the Next proxy. The identity
// credential forwarded here is only the exact local opaque-session cookie. The
// existing local CSRF cookie is also forwarded, when canonical, so YU-26 does
// not rotate its double-submit value on every server-side current check.
export async function readLocalCurrentSession(
  request: Request,
  environment: RuntimeEnvironment = process.env,
  fetcher: typeof fetch = fetch,
): Promise<LocalCurrentResult> {
  const sessionToken = localSessionCookie(request);
  if (!sessionToken || cookieNameCount(request, LOCAL_SESSION_COOKIE_NAME) !== 1) {
    return { ok: false, reason: "unauthenticated" };
  }
  const csrfCookie = cookieNameCount(request, LOCAL_CSRF_COOKIE_NAME) === 1
    ? readExactCookie(request, LOCAL_CSRF_COOKIE_NAME)
    : undefined;
  const cookies = [`${LOCAL_SESSION_COOKIE_NAME}=${sessionToken}`];
  if (csrfCookie) cookies.push(`${LOCAL_CSRF_COOKIE_NAME}=${csrfCookie}`);

  let response: Response;
  try {
    const current = new Request(buildRuntimeUrl("/auth/local/current", undefined, environment), {
      method: "GET",
      headers: {
        accept: "application/json",
        cookie: cookies.join("; "),
      },
    });
    response = await fetcher(current, { cache: "no-store" });
  } catch {
    return { ok: false, reason: "unavailable" };
  }

  if (response.status === 401) return { ok: false, reason: "unauthenticated" };
  if (!response.ok) return { ok: false, reason: "unavailable" };

  let payload: unknown;
  try {
    payload = await response.json();
  } catch {
    return { ok: false, reason: "unavailable" };
  }
  if (!validCurrentPayload(payload)) return { ok: false, reason: "unavailable" };
  const upstreamSetCookie = response.headers.get("set-cookie")?.trim();
  const setCookie = upstreamSetCookie?.startsWith(`${LOCAL_CSRF_COOKIE_NAME}=`) ? upstreamSetCookie : undefined;
  return {
    ok: true,
    session: {
      accessToken: payload.accessToken,
      csrfToken: payload.csrfToken,
      organizationId: payload.organizationId,
      userId: payload.userId,
    },
    ...(setCookie ? { setCookie } : {}),
  };
}

function validCurrentPayload(value: unknown): value is {
  authenticated: true;
  accessToken: string;
  csrfToken: string;
  organizationId: string;
  userId: string;
} {
  if (!value || typeof value !== "object") return false;
  const payload = value as Record<string, unknown>;
  return payload.authenticated === true
    && canonicalNonSecretIdentifier(payload.organizationId)
    && canonicalNonSecretIdentifier(payload.userId)
    && canonicalAccessToken(payload.accessToken)
    && canonicalOpaque(payload.csrfToken);
}

function canonicalNonSecretIdentifier(value: unknown): value is string {
  return typeof value === "string" && value.length > 0 && value.length <= 255 && value === value.trim();
}

function canonicalAccessToken(value: unknown): value is string {
  return typeof value === "string"
    && value.length >= 32
    && value.length <= 8192
    && value === value.trim()
    && !/\s/.test(value);
}

function canonicalOpaque(value: unknown): value is string {
  return typeof value === "string" && /^[A-Za-z0-9_-]{43}$/.test(value);
}
