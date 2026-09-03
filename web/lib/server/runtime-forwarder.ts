import { createRuntimeRequest, type RuntimeEnvironment } from "./runtime-proxy";
import type { VerifiedLogin } from "./oidc";

export async function forwardRuntimeRequest(
  request: Request,
  pathSegments: readonly string[],
  environment: RuntimeEnvironment = process.env,
  login?: VerifiedLogin,
  traceId?: string,
): Promise<Response> {
  if (login && !environment.IOT_DELIVERY_LOCAL_API_KEY?.trim()) throw new Error("missing BFF channel credential");
  const upstreamRequest = await createRuntimeRequest(request, pathSegments, environment, login, traceId);
  return fetch(upstreamRequest, { cache: "no-store" });
}
