import { forwardRuntimeRequest } from "@/lib/server/runtime-forwarder";

export const dynamic = "force-dynamic";

type RuntimeRouteContext = {
  params: Promise<{ path: string[] }>;
};

async function proxy(request: Request, context: RuntimeRouteContext): Promise<Response> {
  const { path } = await context.params;
  return forwardRuntimeRequest(request, ["api", ...path]);
}

export async function GET(request: Request, context: RuntimeRouteContext) {
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
