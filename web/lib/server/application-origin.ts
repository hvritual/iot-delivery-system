export type ApplicationOriginEnvironment = Partial<NodeJS.ProcessEnv>;

export class ApplicationOriginConfigurationError extends Error {}

// resolveTrustedApplicationOrigin defines the browser application's own origin
// without consulting OIDC configuration. An explicit origin is useful behind a
// reverse proxy; otherwise the canonical request origin is the authority.
// Plain HTTP is accepted only for loopback development origins.
export function resolveTrustedApplicationOrigin(
  request: Request,
  environment: ApplicationOriginEnvironment = process.env,
): string {
  let requestOrigin: string;
  try {
    requestOrigin = canonicalApplicationOrigin(new URL(request.url).origin);
  } catch {
    throw new ApplicationOriginConfigurationError();
  }

  const configured = environment.IOT_DELIVERY_WEB_ORIGIN?.trim();
  if (!configured) return requestOrigin;

  let trusted: string;
  try {
    trusted = canonicalApplicationOrigin(configured);
  } catch {
    throw new ApplicationOriginConfigurationError();
  }
  if (trusted !== requestOrigin) throw new ApplicationOriginConfigurationError();
  return trusted;
}

export function canonicalApplicationOrigin(value: string): string {
  if (!value || value !== value.trim()) throw new ApplicationOriginConfigurationError();
  const parsed = new URL(value);
  if (parsed.origin !== value || parsed.username || parsed.password || parsed.pathname !== "/" || parsed.search || parsed.hash) {
    throw new ApplicationOriginConfigurationError();
  }
  if (parsed.protocol === "https:") return parsed.origin;
  if (parsed.protocol !== "http:" || !isLoopbackHostname(parsed.hostname)) {
    throw new ApplicationOriginConfigurationError();
  }
  return parsed.origin;
}

function isLoopbackHostname(hostname: string): boolean {
  const normalized = hostname.startsWith("[") && hostname.endsWith("]")
    ? hostname.slice(1, -1)
    : hostname;
  if (normalized.toLowerCase() === "localhost") return true;
  if (normalized === "127.0.0.1" || normalized === "::1") return true;
  if (/^127(?:\.\d{1,3}){3}$/.test(normalized)) {
    return normalized.split(".").every((part) => Number(part) >= 0 && Number(part) <= 255);
  }
  return false;
}
