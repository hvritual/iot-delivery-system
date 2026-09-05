import { forwardLocalAuthBrowserRequest } from "@/lib/server/local-auth-forwarder";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

type RouteContext = {
  params: Promise<{ path: string[] }>;
};

async function handle(request: Request, context: RouteContext): Promise<Response> {
  const { path } = await context.params;
  return forwardLocalAuthBrowserRequest(request, path, process.env);
}

export async function GET(request: Request, context: RouteContext) {
  return handle(request, context);
}

export async function POST(request: Request, context: RouteContext) {
  return handle(request, context);
}
