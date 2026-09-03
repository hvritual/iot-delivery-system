import { forwardRuntimeRequest } from "@/lib/server/runtime-forwarder";

export const dynamic = "force-dynamic";

export async function GET(request: Request) {
  return forwardRuntimeRequest(request, ["health"]);
}
