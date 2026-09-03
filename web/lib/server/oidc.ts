import {
  ClientSecretBasic,
  allowInsecureRequests,
  authorizationCodeGrant,
  buildAuthorizationUrl,
  calculatePKCECodeChallenge,
  discovery,
  randomNonce,
  randomPKCECodeVerifier,
  randomState,
} from "openid-client";
import { createRemoteJWKSet, jwtVerify } from "jose";

const DEFAULT_TRANSACTION_TTL_MS = 10 * 60 * 1000;
const DEFAULT_TRANSACTION_CAPACITY = 1_000;

export type VerifiedLogin = Readonly<{
  issuer: string;
  subject: string;
  email?: string;
  displayName?: string;
}>;

export type LoginTransaction = Readonly<{
  state: string;
  nonce: string;
  codeVerifier: string;
  issuer: string;
  clientId: string;
  redirectUri: string;
  expiresAt: number;
}>;

export interface LoginTransactionStore {
  put(transaction: LoginTransaction): void;
  consume(state: string): LoginTransaction | undefined;
}

export class LoginTransactionCapacityError extends Error {}

export class InMemoryLoginTransactionStore implements LoginTransactionStore {
  private readonly transactions = new Map<string, LoginTransaction>();

  constructor(
    private readonly options: {
      clock?: () => number;
      ttlMs?: number;
      capacity?: number;
    } = {},
  ) {}

  create(metadata: Pick<LoginTransaction, "issuer" | "clientId" | "redirectUri">): LoginTransaction {
    this.evictExpired();
    const capacity = this.options.capacity ?? DEFAULT_TRANSACTION_CAPACITY;
    if (this.transactions.size >= capacity) throw new LoginTransactionCapacityError();

    const transaction: LoginTransaction = {
      state: randomState(),
      nonce: randomNonce(),
      codeVerifier: randomPKCECodeVerifier(),
      ...metadata,
      expiresAt: this.now() + (this.options.ttlMs ?? DEFAULT_TRANSACTION_TTL_MS),
    };
    this.put(transaction);
    return transaction;
  }

  put(transaction: LoginTransaction): void {
    this.evictExpired();
    const capacity = this.options.capacity ?? DEFAULT_TRANSACTION_CAPACITY;
    if (!this.transactions.has(transaction.state) && this.transactions.size >= capacity) {
      throw new LoginTransactionCapacityError();
    }
    this.transactions.set(transaction.state, transaction);
  }

  consume(state: string): LoginTransaction | undefined {
    this.evictExpired();
    const transaction = this.transactions.get(state);
    if (!transaction) return undefined;
    this.transactions.delete(state);
    return transaction;
  }

  private evictExpired(): void {
    const now = this.now();
    for (const [state, transaction] of this.transactions) {
      if (transaction.expiresAt <= now) this.transactions.delete(state);
    }
  }

  private now(): number {
    return (this.options.clock ?? Date.now)();
  }
}

export class OidcConfigurationError extends Error {}

export type OidcConfiguration = Readonly<{
  issuer: URL;
  issuerIdentifier: string;
  clientId: string;
  clientSecret: string;
  redirectUri: URL;
  permitInsecureLocalTestHttp: boolean;
}>;

export function readOidcConfiguration(environment = process.env): OidcConfiguration {
  const issuer = parseRequiredUrl(environment.OIDC_ISSUER, "OIDC_ISSUER");
  const redirectUri = parseRequiredUrl(environment.OIDC_REDIRECT_URI, "OIDC_REDIRECT_URI");
  const clientId = requireEnvironmentValue(environment.OIDC_CLIENT_ID, "OIDC_CLIENT_ID");
  const clientSecret = requireEnvironmentValue(environment.OIDC_CLIENT_SECRET, "OIDC_CLIENT_SECRET");
  const permitInsecureLocalTestHttp = environment.OIDC_ALLOW_INSECURE_TEST_HTTP === "1";

  if (!isSafeOidcUrl(issuer, permitInsecureLocalTestHttp) || !isSafeOidcUrl(redirectUri, permitInsecureLocalTestHttp)) {
    throw new OidcConfigurationError();
  }
  if (issuer.search || issuer.hash) throw new OidcConfigurationError();
  if (redirectUri.pathname !== "/auth/callback" || redirectUri.search || redirectUri.hash) {
    throw new OidcConfigurationError();
  }

  return {
    issuer,
    issuerIdentifier: issuer.pathname === "/" ? issuer.origin : issuer.href,
    clientId,
    clientSecret,
    redirectUri,
    permitInsecureLocalTestHttp,
  };
}

const transactionStoreKey = "__iotDeliveryOidcLoginTransactions";
const globalTransactionStore = globalThis as typeof globalThis & {
  [transactionStoreKey]?: InMemoryLoginTransactionStore;
};

// Next loads route modules independently in development and production bundles.
// The store must therefore be process-global, while still remaining server-only.
export const loginTransactions = globalTransactionStore[transactionStoreKey]
  ?? (globalTransactionStore[transactionStoreKey] = new InMemoryLoginTransactionStore());

export interface VerifiedLoginCompleter {
  complete(login: VerifiedLogin): Promise<void> | void;
}

const noSessionLoginCompleter: VerifiedLoginCompleter = {
  complete: () => undefined,
};

type CallbackErrorCode = "invalid_state" | "missing_code" | "provider_access_denied" | "provider_temporarily_unavailable" | "provider_server_error" | "provider_error" | "authentication_failed" | "configuration_error";

export class OidcCallbackError extends Error {
  constructor(
    readonly code: CallbackErrorCode,
    readonly status: 400 | 401 | 500,
  ) {
    super(code);
  }
}

export async function startOidcLogin(store: InMemoryLoginTransactionStore = loginTransactions): Promise<URL> {
  const settings = readOidcConfiguration();
  const client = await oidcClient(settings);
  const transaction = store.create({
    issuer: settings.issuer.href,
    clientId: settings.clientId,
    redirectUri: settings.redirectUri.href,
  });
  const codeChallenge = await calculatePKCECodeChallenge(transaction.codeVerifier);

  return buildAuthorizationUrl(client, {
    response_type: "code",
    redirect_uri: settings.redirectUri.href,
    scope: "openid email",
    state: transaction.state,
    nonce: transaction.nonce,
    code_challenge: codeChallenge,
    code_challenge_method: "S256",
  });
}

export async function finishOidcLogin(
  request: Request,
  store: LoginTransactionStore = loginTransactions,
  completer: VerifiedLoginCompleter = noSessionLoginCompleter,
): Promise<void> {
  const callback = new URL(request.url);
  const state = callback.searchParams.get("state");
  if (!state) throw new OidcCallbackError("invalid_state", 400);

  const transaction = store.consume(state);
  if (!transaction) throw new OidcCallbackError("invalid_state", 400);

  const providerError = callback.searchParams.get("error");
  if (providerError) throw providerErrorResponse(providerError);

  if (!callback.searchParams.get("code")) throw new OidcCallbackError("missing_code", 400);

  let settings: OidcConfiguration;
  try {
    settings = readOidcConfiguration();
  } catch {
    throw new OidcCallbackError("configuration_error", 500);
  }
  if (
    settings.issuer.href !== transaction.issuer
    || settings.clientId !== transaction.clientId
    || settings.redirectUri.href !== transaction.redirectUri
  ) {
    throw new OidcCallbackError("configuration_error", 500);
  }

  try {
    const client = await oidcClient(settings);
    const tokens = await authorizationCodeGrant(client, request, {
      expectedState: transaction.state,
      expectedNonce: transaction.nonce,
      pkceCodeVerifier: transaction.codeVerifier,
      idTokenExpected: true,
    });
    if (typeof tokens.id_token !== "string") throw new Error("missing ID token");
    const jwksUri = client.serverMetadata().jwks_uri;
    if (!jwksUri) throw new Error("missing JWKS URI");
    const verified = await jwtVerify(tokens.id_token, createRemoteJWKSet(new URL(jwksUri)), {
      algorithms: ["RS256"],
      issuer: settings.issuerIdentifier,
      audience: settings.clientId,
    });
    if (typeof verified.payload.sub !== "string" || verified.payload.sub.length === 0 || verified.payload.nonce !== transaction.nonce) {
      throw new Error("missing subject");
    }
    await completer.complete({
      issuer: settings.issuerIdentifier,
      subject: verified.payload.sub,
      ...(typeof verified.payload.email === "string" ? { email: verified.payload.email } : {}),
      ...(typeof verified.payload.name === "string" ? { displayName: verified.payload.name } : {}),
    });
  } catch {
    throw new OidcCallbackError("authentication_failed", 401);
  }
}

async function oidcClient(settings: OidcConfiguration) {
  const client = await discovery(
    settings.issuer,
    settings.clientId,
    { redirect_uris: [settings.redirectUri.href], response_types: ["code"] },
    ClientSecretBasic(settings.clientSecret),
    settings.permitInsecureLocalTestHttp ? { execute: [allowInsecureRequests] } : undefined,
  );
  return client;
}

function requireEnvironmentValue(value: string | undefined, name: string): string {
  if (!value || value.trim() === "") throw new OidcConfigurationError(name);
  return value;
}

function parseRequiredUrl(value: string | undefined, name: string): URL {
  try {
    return new URL(requireEnvironmentValue(value, name));
  } catch {
    throw new OidcConfigurationError(name);
  }
}

function isSafeOidcUrl(url: URL, permitInsecureLocalTestHttp: boolean): boolean {
  if (url.protocol === "https:") return true;
  return permitInsecureLocalTestHttp
    && url.protocol === "http:"
    && (url.hostname === "127.0.0.1" || url.hostname === "localhost");
}

function providerErrorResponse(providerError: string): OidcCallbackError {
  const providerErrorMap: Record<string, CallbackErrorCode> = {
    access_denied: "provider_access_denied",
    temporarily_unavailable: "provider_temporarily_unavailable",
    server_error: "provider_server_error",
  };
  const code = providerErrorMap[providerError] ?? "provider_error";
  return new OidcCallbackError(code, 400);
}
