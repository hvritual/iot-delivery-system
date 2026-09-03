import { OidcCallbackError, finishOidcLogin } from "@/lib/server/oidc";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

const noStoreHeaders = { "cache-control": "no-store, max-age=0" };

export async function GET(request: Request) {
  try {
    await finishOidcLogin(request);
    return Response.json({ status: "verified" }, { headers: noStoreHeaders });
  } catch (error) {
    if (error instanceof OidcCallbackError) {
      return Response.json({ error: error.code }, { status: error.status, headers: noStoreHeaders });
    }
    return Response.json({ error: "authentication_failed" }, { status: 401, headers: noStoreHeaders });
  }
}
