import { OidcConfigurationError, startOidcLogin } from "@/lib/server/oidc";
import { hostCookie, LOGIN_COOKIE_NAME, LOGIN_MAX_AGE_SECONDS } from "@/lib/server/session";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

const noStoreHeaders = { "cache-control": "no-store, max-age=0" };

export async function GET(_request: Request) {
  try {
    const location = await startOidcLogin();
    const state = location.searchParams.get("state");
    if (!state) throw new Error("missing generated state");
    return new Response(null, {
      status: 302,
      headers: { ...noStoreHeaders, location: location.href, "set-cookie": hostCookie(LOGIN_COOKIE_NAME, state, LOGIN_MAX_AGE_SECONDS) },
    });
  } catch (error) {
    if (error instanceof OidcConfigurationError) {
      return Response.json({ error: "configuration_error" }, { status: 500, headers: noStoreHeaders });
    }
    return Response.json({ error: "login_unavailable" }, { status: 503, headers: noStoreHeaders });
  }
}
