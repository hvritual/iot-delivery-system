import { generateTraceID } from "./bff-assertion";
import { resolveTrustedApplicationOrigin } from "./application-origin";
import { cookieNameCount, LOCAL_CSRF_COOKIE_NAME, LOCAL_SESSION_COOKIE_NAME } from "./local-auth-session";
import { buildRuntimeUrl, type RuntimeEnvironment } from "./runtime-proxy";
import { readExactCookie } from "./session";

const noStoreHeaders = { "cache-control": "no-store, max-age=0", vary: "Cookie, Origin" };
const responseHeaderAllowlist = new Set(["cache-control", "content-type", "pragma", "vary", "x-trace-id", "retry-after"]);

type RouteSpec = Readonly<{
  method: "GET" | "POST";
  pathname: string;
  sanitizeBrowserPayload?: boolean;
}>;

export async function forwardLocalAuthBrowserRequest(
  request: Request,
  path: readonly string[],
  environment: RuntimeEnvironment = process.env,
  fetcher: typeof fetch = fetch,
): Promise<Response> {
  const traceId = generateTraceID();
  const spec = resolveRoute(path);
  if (!spec) return errorResponse("not_found", 404, traceId);
  if (request.method.toUpperCase() !== spec.method) return errorResponse("method_not_allowed", 405, traceId);

  if (spec.method === "POST") {
    let trustedOrigin: string;
    try {
      trustedOrigin = resolveTrustedApplicationOrigin(request, environment);
    } catch {
      return errorResponse("service_unavailable", 503, traceId);
    }
    if (request.headers.get("origin") !== trustedOrigin) return errorResponse("forbidden", 403, traceId);
  }

  let upstreamURL: string;
  try {
    upstreamURL = buildRuntimeUrl(spec.pathname, undefined, environment);
  } catch {
    return errorResponse("service_unavailable", 503, traceId);
  }

  const upstream = new URL(upstreamURL);
  const headers = new Headers({ Accept: "application/json" });
  const contentType = request.headers.get("content-type");
  if (contentType) headers.set("content-type", contentType);

  const cookie = localCookieHeader(request);
  if (cookie) headers.set("cookie", cookie);

  const csrf = request.headers.get("x-csrf-token");
  if (csrf) headers.set("x-csrf-token", csrf);

  if (spec.method === "POST") {
    // The Next BFF validates the browser's application Origin above. The
    // server-to-server hop then uses the runtime's own canonical origin so the
    // YU-26 host-bound Origin check remains meaningful instead of trusting a
    // browser-controlled Host/Forwarded header.
    headers.set("origin", upstream.origin);
  }

  let body: Uint8Array<ArrayBuffer> | undefined;
  if (spec.method !== "GET") body = new Uint8Array(await request.arrayBuffer());

  let response: Response;
  try {
    response = await fetcher(new Request(upstreamURL, { method: spec.method, headers, body }), { cache: "no-store" });
  } catch {
    return errorResponse("service_unavailable", 503, traceId);
  }

  return rebuildBrowserResponse(response, Boolean(spec.sanitizeBrowserPayload));
}

function resolveRoute(path: readonly string[]): RouteSpec | undefined {
  if (path.length === 1 && path[0] === "login") return { method: "POST", pathname: "/auth/local/login", sanitizeBrowserPayload: true };
  if (path.length === 1 && path[0] === "current") return { method: "GET", pathname: "/auth/local/current", sanitizeBrowserPayload: true };
  if (path.length === 1 && path[0] === "logout") return { method: "POST", pathname: "/auth/local/logout" };
  if (path.length === 1 && path[0] === "change-password") return { method: "POST", pathname: "/auth/local/change-password" };
  if (path.length === 2 && path[0] === "admin" && path[1] === "members") {
    return { method: "POST", pathname: "/auth/local/admin/members" };
  }
  if (path.length === 4 && path[0] === "admin" && path[1] === "members" && canonicalIdentifier(path[2])) {
    if (path[3] === "disable") return { method: "POST", pathname: `/auth/local/admin/members/${encodeURIComponent(path[2])}/disable` };
    if (path[3] === "reset-credential") return { method: "POST", pathname: `/auth/local/admin/members/${encodeURIComponent(path[2])}/reset-credential` };
  }
  if (path.length === 2 && path[0] === "admin" && path[1] === "project-role-bindings") {
    return { method: "POST", pathname: "/auth/local/admin/project-role-bindings" };
  }
  if (path.length === 4 && path[0] === "admin" && path[1] === "project-role-bindings" && canonicalIdentifier(path[2]) && path[3] === "revoke") {
    return { method: "POST", pathname: `/auth/local/admin/project-role-bindings/${encodeURIComponent(path[2])}/revoke` };
  }
  return undefined;
}

function canonicalIdentifier(value: string | undefined): value is string {
  return typeof value === "string" && value.length > 0 && value.length <= 255 && value === value.trim() && !value.includes("/") && value !== "." && value !== "..";
}

function localCookieHeader(request: Request): string | undefined {
  const values: string[] = [];
  for (const name of [LOCAL_SESSION_COOKIE_NAME, LOCAL_CSRF_COOKIE_NAME]) {
    if (cookieNameCount(request, name) !== 1) continue;
    const value = readExactCookie(request, name);
    if (value) values.push(`${name}=${value}`);
  }
  return values.length > 0 ? values.join("; ") : undefined;
}

async function rebuildBrowserResponse(upstream: Response, sanitize: boolean): Promise<Response> {
  const headers = new Headers();
  for (const [name, value] of upstream.headers) {
    if (responseHeaderAllowlist.has(name.toLowerCase())) headers.set(name, value);
  }
  headers.set("cache-control", upstream.headers.get("cache-control") ?? noStoreHeaders["cache-control"]);
  headers.set("vary", upstream.headers.get("vary") ?? noStoreHeaders.vary);
  for (const cookie of setCookieValues(upstream.headers)) headers.append("set-cookie", cookie);

  if (upstream.status === 204 || upstream.status === 205) return new Response(null, { status: upstream.status, headers });

  const contentType = upstream.headers.get("content-type") ?? "";
  if (!sanitize || !contentType.includes("application/json")) {
    return new Response(await upstream.arrayBuffer(), { status: upstream.status, headers });
  }

  let payload: unknown;
  try {
    payload = await upstream.json();
  } catch {
    return errorResponse("service_unavailable", 503, upstream.headers.get("x-trace-id") ?? generateTraceID());
  }
  if (payload && typeof payload === "object" && !Array.isArray(payload)) {
    const browserPayload = { ...(payload as Record<string, unknown>) };
    delete browserPayload.accessToken;
    delete browserPayload.accessExpiresAt;
    delete browserPayload.csrfToken;
    headers.set("content-type", "application/json; charset=utf-8");
    return new Response(JSON.stringify(browserPayload), { status: upstream.status, headers });
  }
  return new Response(JSON.stringify(payload), { status: upstream.status, headers });
}

function setCookieValues(headers: Headers): string[] {
  const extended = headers as Headers & { getSetCookie?: () => string[] };
  if (typeof extended.getSetCookie === "function") return extended.getSetCookie();
  const value = headers.get("set-cookie");
  return value ? [value] : [];
}

function errorResponse(error: string, status: 403 | 404 | 405 | 503, traceId: string): Response {
  return Response.json({ error, traceId }, {
    status,
    headers: { ...noStoreHeaders, "x-trace-id": traceId },
  });
}
