import { BffAssertionConfigurationError, generateTraceID } from "@/lib/server/bff-assertion";
import { resolveTrustedApplicationOrigin } from "@/lib/server/application-origin";
import { cookieNameCount, LOCAL_SESSION_COOKIE_NAME, readLocalCurrentSession } from "@/lib/server/local-auth-session";
import { forwardLocalRuntimeRequest, forwardRuntimeRequest } from "@/lib/server/runtime-forwarder";
import { guardSessionRequest } from "@/lib/server/session-guard";
import { secureEqual, serverSessions, SESSION_COOKIE_NAME } from "@/lib/server/session";

export const dynamic = "force-dynamic";

type RuntimeRouteContext = {
  params: Promise<{ path: string[] }>;
};

const SAFE_METHODS = new Set(["GET", "HEAD", "OPTIONS"]);

async function proxy(request: Request, context: RuntimeRouteContext): Promise<Response> {
  const traceId = generateTraceID();
  const localCookies = cookieNameCount(request, LOCAL_SESSION_COOKIE_NAME);
  const oidcCookies = cookieNameCount(request, SESSION_COOKIE_NAME);

  // Authentication source is explicit. A request presenting both browser
  // session families fails closed instead of selecting one by precedence.
  if (localCookies > 0 && oidcCookies > 0) return rejected("unauthenticated", traceId);
  if (localCookies > 0) return proxyLocal(request, context, traceId);
  if (oidcCookies > 0) return proxyOidc(request, context, traceId);
  return rejected("unauthenticated", traceId);
}

async function proxyOidc(request: Request, context: RuntimeRouteContext, traceId: string): Promise<Response> {
  let trustedOrigin: string | undefined;
  if (!SAFE_METHODS.has(request.method.toUpperCase())) {
    try {
      trustedOrigin = resolveTrustedApplicationOrigin(request, process.env);
    } catch {
      return unavailable(traceId);
    }
  }
  const guard = guardSessionRequest(request, serverSessions, trustedOrigin);
  if (!guard.ok) return rejected(guard.reason, traceId);

  const { path } = await context.params;
  try {
    return await forwardRuntimeRequest(request, ["api", ...path], process.env, guard.session.login, traceId);
  } catch (error) {
    if (error instanceof BffAssertionConfigurationError || (error instanceof Error && error.message === "missing BFF channel credential")) return unavailable(traceId);
    return errorResponse("upstream_unavailable", 502, traceId);
  }
}

async function proxyLocal(request: Request, context: RuntimeRouteContext, traceId: string): Promise<Response> {
  const current = await readLocalCurrentSession(request, process.env);
  if (!current.ok) {
    return current.reason === "unauthenticated"
      ? rejected("unauthenticated", traceId)
      : unavailable(traceId);
  }

  if (!SAFE_METHODS.has(request.method.toUpperCase())) {
    let trustedOrigin: string;
    try {
      trustedOrigin = resolveTrustedApplicationOrigin(request, process.env);
    } catch {
      return unavailable(traceId);
    }
    const origin = request.headers.get("origin");
    const csrf = request.headers.get("x-csrf-token");
    if (origin !== trustedOrigin || !csrf || csrf !== csrf.trim() || csrf.includes(",") || !secureEqual(csrf, current.session.csrfToken)) {
      return rejected("invalid_csrf", traceId);
    }
  }

  const { path } = await context.params;
  try {
    return await forwardLocalRuntimeRequest(request, ["api", ...path], current.session.accessToken, process.env);
  } catch {
    return errorResponse("upstream_unavailable", 502, traceId);
  }
}

const noStoreHeaders = { "cache-control": "no-store, max-age=0", vary: "Cookie" };

function unavailable(traceId: string): Response {
  return errorResponse("service_unavailable", 503, traceId);
}

function rejected(reason: "unauthenticated" | "invalid_origin" | "invalid_csrf", traceId: string): Response {
  const unauthenticated = reason === "unauthenticated";
  return errorResponse(unauthenticated ? "unauthenticated" : "forbidden", unauthenticated ? 401 : 403, traceId);
}

function errorResponse(error: string, status: 401 | 403 | 502 | 503, traceId: string): Response {
  return Response.json({ error, traceId }, { status, headers: { ...noStoreHeaders, "x-trace-id": traceId } });
}

export async function GET(request: Request, context: RuntimeRouteContext) {
  return proxy(request, context);
}

export async function HEAD(request: Request, context: RuntimeRouteContext) {
  return proxy(request, context);
}

export async function OPTIONS(request: Request, context: RuntimeRouteContext) {
  return proxy(request, context);
}

export async function POST(request: Request, context: RuntimeRouteContext) {
  return proxy(request, context);
}

export async function PATCH(request: Request, context: RuntimeRouteContext) {
  return proxy(request, context);
}

export async function PUT(request: Request, context: RuntimeRouteContext) {
  return proxy(request, context);
}

export async function DELETE(request: Request, context: RuntimeRouteContext) {
  return proxy(request, context);
}
