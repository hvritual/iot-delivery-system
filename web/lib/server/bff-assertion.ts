import { createHash, createHmac, randomBytes, timingSafeEqual } from "node:crypto";

import type { VerifiedLogin } from "./oidc";

export const BFF_ASSERTION_HEADER = "X-IoT-Delivery-Assertion";
export const BFF_ASSERTION_SIGNATURE_HEADER = "X-IoT-Delivery-Assertion-Signature";
export const BFF_TRACE_HEADER = "X-Trace-ID";
export const BFF_ASSERTION_VERSION = 1;

const TRACE_ID_PATTERN = /^[0-9a-f]{32}$/;
const ASSERTION_TTL_SECONDS = 60;
export const MAX_BFF_ASSERTION_BYTES = 8_192;

export type BffAssertion = Readonly<{
  v: 1;
  issuer: string;
  subject: string;
  email?: string;
  displayName?: string;
  nonce: string;
  traceId: string;
  method: string;
  path: string;
  bodySha256: string;
  iat: number;
  exp: number;
}>;

export class BffAssertionConfigurationError extends Error {}

export function createBffAssertionHeaders(
  login: VerifiedLogin,
  method: string,
  path: string,
  body: Uint8Array | undefined,
  environment: Partial<NodeJS.ProcessEnv> = process.env,
  now = Math.floor(Date.now() / 1_000),
  traceId = generateTraceID(),
  nonce = randomBytes(16).toString("base64url"),
): Headers {
  const key = assertionKey(environment.IOT_DELIVERY_BFF_ASSERTION_KEY);
  if (!isSignedTraceId(traceId)) throw new BffAssertionConfigurationError("invalid trace ID");
  const assertion: BffAssertion = {
    v: BFF_ASSERTION_VERSION,
    issuer: requireIdentityValue(login.issuer),
    subject: requireIdentityValue(login.subject),
    ...(optionalIdentityValue(login.email, "email")),
    ...(optionalIdentityValue(login.displayName, "displayName")),
    nonce,
    traceId,
    method: method.toUpperCase(),
    path: exactPath(path),
    bodySha256: createHash("sha256").update(body ?? new Uint8Array()).digest("hex"),
    iat: now,
    exp: now + ASSERTION_TTL_SECONDS,
  };
  const payload = Buffer.from(JSON.stringify(assertion)).toString("base64url");
  if (Buffer.byteLength(payload, "utf8") > MAX_BFF_ASSERTION_BYTES) {
    throw new BffAssertionConfigurationError("assertion exceeds header limit");
  }
  const signature = createHmac("sha256", key).update(payload).digest("base64url");
  const headers = new Headers();
  headers.set(BFF_ASSERTION_HEADER, payload);
  headers.set(BFF_ASSERTION_SIGNATURE_HEADER, signature);
  headers.set(BFF_TRACE_HEADER, traceId);
  return headers;
}

export function isSignedTraceId(value: string | null): boolean {
  return value !== null && TRACE_ID_PATTERN.test(value);
}

export function generateTraceID(): string {
  return randomBytes(16).toString("hex");
}

export function secureHeaderEqual(left: string, right: string): boolean {
  const leftBytes = Buffer.from(left);
  const rightBytes = Buffer.from(right);
  return leftBytes.length === rightBytes.length && timingSafeEqual(leftBytes, rightBytes);
}

function assertionKey(value: string | undefined): Buffer {
  if (!value || value.trim() === "") throw new BffAssertionConfigurationError("missing assertion key");
  try {
    const key = Buffer.from(value.trim(), "base64url");
    if (key.length < 32 || key.toString("base64url") !== value.trim()) throw new Error("invalid key");
    return key;
  } catch {
    throw new BffAssertionConfigurationError("invalid assertion key");
  }
}

function requireIdentityValue(value: string): string {
  if (value !== value.trim() || !isSafeIdentityValue(value)) throw new BffAssertionConfigurationError("invalid session identity");
  return value;
}

function optionalIdentityValue(value: string | undefined, key: "email" | "displayName"): Partial<Pick<BffAssertion, "email" | "displayName">> {
  if (value === undefined) return {};
  const normalized = value.trim();
  if (!isSafeIdentityValue(normalized)) throw new BffAssertionConfigurationError("invalid session identity");
  return { [key]: normalized };
}

function isSafeIdentityValue(value: string): boolean {
  return value.length > 0 && value.length <= 4_096 && !/\p{Cc}/u.test(value);
}

function exactPath(value: string): string {
  if (!value.startsWith("/") || value.startsWith("//") || value.length > 8_192) throw new BffAssertionConfigurationError("invalid upstream path");
  return value;
}
