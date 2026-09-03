import { OidcConfigurationError, startOidcLogin } from "@/lib/server/oidc";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

const noStoreHeaders = { "cache-control": "no-store, max-age=0" };

export async function GET(_request: Request) {
  try {
    return new Response(null, {
      status: 302,
      headers: { ...noStoreHeaders, location: (await startOidcLogin()).href },
    });
  } catch (error) {
    if (error instanceof OidcConfigurationError) {
      return Response.json({ error: "configuration_error" }, { status: 500, headers: noStoreHeaders });
    }
    return Response.json({ error: "login_unavailable" }, { status: 503, headers: noStoreHeaders });
  }
}
