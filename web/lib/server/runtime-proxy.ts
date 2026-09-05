import { BFF_ASSERTION_HEADER, BFF_ASSERTION_SIGNATURE_HEADER, BFF_TRACE_HEADER, createBffAssertionHeaders } from "./bff-assertion";
import type { VerifiedLogin } from "./oidc";

export const DEFAULT_DELIVERY_API_TARGET = "http://127.0.0.1:8281";

export type RuntimeEnvironment = Partial<NodeJS.ProcessEnv>;

const FORWARD_REQUEST_HEADERS = new Set(["accept", "content-type", "if-match", "idempotency-key"]);

function resolveRuntimeTarget(environment: RuntimeEnvironment): URL {
  const configured = environment.IOT_DELIVERY_API_TARGET?.trim() || DEFAULT_DELIVERY_API_TARGET;
  const target = new URL(configured);
  if (target.protocol !== "http:" && target.protocol !== "https:") {
    throw new Error("IOT_DELIVERY_API_TARGET 必须是 HTTP(S) 地址。");
  }
  return target;
}

function normalisePath(pathname: string): string {
  if (!pathname.startsWith("/") || pathname.startsWith("//")) {
    throw new Error("运行时代理路径必须是本站绝对路径。");
  }
  return pathname;
}

function searchString(search: URLSearchParams | Record<string, string> | undefined): string {
  if (search instanceof URLSearchParams) return search.toString();
  return new URLSearchParams(search ?? {}).toString();
}

export function buildRuntimeUrl(
  pathname: string,
  search: URLSearchParams | Record<string, string> | undefined = undefined,
  environment: RuntimeEnvironment = process.env,
): string {
  const target = resolveRuntimeTarget(environment);
  const upstreamPath = normalisePath(pathname);
  const basePath = target.pathname.replace(/\/$/, "");
  target.pathname = `${basePath}${upstreamPath}`;
  target.search = searchString(search);
  return target.toString();
}

function requestPath(pathSegments: readonly string[]): string {
  return `/${pathSegments.map((segment) => encodeURIComponent(segment)).join("/")}`;
}

function forwardedHeaders(request: Request): Headers {
  const headers = new Headers();
  for (const [name, value] of request.headers) {
    if (FORWARD_REQUEST_HEADERS.has(name.toLowerCase())) headers.append(name, value);
  }
  return headers;
}

async function requestBody(request: Request): Promise<Uint8Array<ArrayBuffer> | undefined> {
  const method = request.method.toUpperCase();
  return method === "GET" || method === "HEAD" ? undefined : new Uint8Array(await request.arrayBuffer());
}

export async function createRuntimeRequest(
  request: Request,
  pathSegments: readonly string[],
  environment: RuntimeEnvironment = process.env,
  login?: VerifiedLogin,
  traceId?: string,
): Promise<Request> {
  const incoming = new URL(request.url);
  const headers = forwardedHeaders(request);

  const apiKey = environment.IOT_DELIVERY_LOCAL_API_KEY?.trim();
  if (apiKey) headers.set("X-API-Key", apiKey);

  const method = request.method.toUpperCase();
  const body = await requestBody(request);
  const url = buildRuntimeUrl(requestPath(pathSegments), incoming.searchParams, environment);
  if (login) {
    const upstream = new URL(url);
    const assertionHeaders = createBffAssertionHeaders(login, method, `${upstream.pathname}${upstream.search}`, body, environment, undefined, traceId);
    assertionHeaders.forEach((value, name) => headers.set(name, value));
  }

  return new Request(url, { method, headers, body });
}

// createLocalRuntimeRequest is an explicit local-member forwarding mode. The
// access token is obtained server-side from YU-26 current and is never accepted
// from browser headers. Development API-key and BFF assertion credentials are
// deliberately omitted so credential families cannot be mixed by precedence.
export async function createLocalRuntimeRequest(
  request: Request,
  pathSegments: readonly string[],
  accessToken: string,
  environment: RuntimeEnvironment = process.env,
): Promise<Request> {
  if (!accessToken || accessToken !== accessToken.trim() || accessToken.length > 8192 || /\s/.test(accessToken)) {
    throw new Error("local runtime access token is invalid");
  }
  const incoming = new URL(request.url);
  const headers = forwardedHeaders(request);
  headers.set("Authorization", `Bearer ${accessToken}`);

  const method = request.method.toUpperCase();
  const body = await requestBody(request);
  const url = buildRuntimeUrl(requestPath(pathSegments), incoming.searchParams, environment);
  return new Request(url, { method, headers, body });
}

export { BFF_ASSERTION_HEADER, BFF_ASSERTION_SIGNATURE_HEADER, BFF_TRACE_HEADER };
