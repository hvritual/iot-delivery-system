export const DEFAULT_DELIVERY_API_TARGET = "http://127.0.0.1:8281";

export type RuntimeEnvironment = Partial<NodeJS.ProcessEnv>;

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

export async function createRuntimeRequest(
  request: Request,
  pathSegments: readonly string[],
  environment: RuntimeEnvironment = process.env,
): Promise<Request> {
  const incoming = new URL(request.url);
  const headers = new Headers(request.headers);
  headers.delete("host");
  headers.delete("connection");
  headers.delete("content-length");

  const apiKey = environment.IOT_DELIVERY_LOCAL_API_KEY?.trim();
  if (apiKey) headers.set("X-API-Key", apiKey);

  const method = request.method.toUpperCase();
  const body = method === "GET" || method === "HEAD" ? undefined : await request.arrayBuffer();

  return new Request(buildRuntimeUrl(requestPath(pathSegments), incoming.searchParams, environment), {
    method,
    headers,
    body,
  });
}
