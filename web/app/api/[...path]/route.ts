import { BffAssertionConfigurationError, generateTraceID } from "@/lib/server/bff-assertion";
import { readOidcConfiguration } from "@/lib/server/oidc";
import { forwardRuntimeRequest } from "@/lib/server/runtime-forwarder";
import { guardSessionRequest } from "@/lib/server/session-guard";
import { serverSessions } from "@/lib/server/session";

export const dynamic = "force-dynamic";

type RuntimeRouteContext = {
  params: Promise<{ path: string[] }>;
};

async function proxy(request: Request, context: RuntimeRouteContext): Promise<Response> {
  const traceId = generateTraceID();
  const preliminary = guardSessionRequest(request, serverSessions);
  if (!preliminary.ok && preliminary.reason === "unauthenticated") return rejected(preliminary.reason, traceId);
  let trustedOrigin: string | undefined;
  if (!new Set(["GET", "HEAD", "OPTIONS"]).has(request.method.toUpperCase())) {
    try {
      trustedOrigin = readOidcConfiguration().redirectUri.origin;
    } catch {
      return unavailable(traceId);
    }
  }
  const guard = preliminary.ok ? preliminary : guardSessionRequest(request, serverSessions, trustedOrigin);
  if (!guard.ok) {
    return rejected(guard.reason, traceId);
  }
  const { path } = await context.params;
  try {
    return await forwardRuntimeRequest(request, ["api", ...path], process.env, guard.session.login, traceId);
  } catch (error) {
    if (error instanceof BffAssertionConfigurationError || (error instanceof Error && error.message === "missing BFF channel credential")) return unavailable(traceId);
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
