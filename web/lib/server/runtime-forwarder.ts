import { createRuntimeRequest, type RuntimeEnvironment } from "./runtime-proxy";

export async function forwardRuntimeRequest(
  request: Request,
  pathSegments: readonly string[],
  environment: RuntimeEnvironment = process.env,
): Promise<Response> {
  const upstreamRequest = await createRuntimeRequest(request, pathSegments, environment);
  return fetch(upstreamRequest, { cache: "no-store" });
}
