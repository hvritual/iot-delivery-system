import { randomBytes, timingSafeEqual } from "node:crypto";

import type { VerifiedLogin } from "@/lib/server/oidc";

export const SESSION_COOKIE_NAME = "__Host-iotd_session";
export const LOGIN_COOKIE_NAME = "__Host-iotd_login";
export const SESSION_MAX_AGE_SECONDS = 8 * 60 * 60;
export const LOGIN_MAX_AGE_SECONDS = 10 * 60;

const DEFAULT_SESSION_CAPACITY = 10_000;
const DEFAULT_IDLE_TTL_MS = 30 * 60 * 1_000;
const DEFAULT_ABSOLUTE_TTL_MS = SESSION_MAX_AGE_SECONDS * 1_000;
const OPAQUE_VALUE_PATTERN = /^[A-Za-z0-9_-]{32,}$/;

export type ServerSession = Readonly<{
  id: string;
  csrfToken: string;
  login: VerifiedLogin;
  createdAt: number;
  lastAccessedAt: number;
  expiresAt: number;
}>;

export interface ServerSessionStore {
  create(login: VerifiedLogin): ServerSession;
  read(id: string): ServerSession | undefined;
  peek(id: string): ServerSession | undefined;
  revoke(id: string): boolean;
}

export class SessionCapacityError extends Error {}

export class InMemoryServerSessionStore implements ServerSessionStore {
  private readonly sessions = new Map<string, ServerSession>();

  constructor(private readonly options: {
    clock?: () => number;
    capacity?: number;
    idleTtlMs?: number;
    absoluteTtlMs?: number;
  } = {}) {}

  create(login: VerifiedLogin): ServerSession {
    this.evictExpired();
    if (this.sessions.size >= (this.options.capacity ?? DEFAULT_SESSION_CAPACITY)) throw new SessionCapacityError();

    const now = this.now();
    const id = this.newOpaqueValue();
    let csrfToken = this.newOpaqueValue();
    while (csrfToken === id) csrfToken = this.newOpaqueValue();
    const session: ServerSession = {
      id,
      csrfToken,
      login,
      createdAt: now,
      lastAccessedAt: now,
      expiresAt: now + (this.options.absoluteTtlMs ?? DEFAULT_ABSOLUTE_TTL_MS),
    };
    this.sessions.set(id, session);
    return session;
  }

  read(id: string): ServerSession | undefined {
    return this.find(id, true);
  }

  peek(id: string): ServerSession | undefined {
    return this.find(id, false);
  }

  private find(id: string, renew: boolean): ServerSession | undefined {
    this.evictExpired();
    const session = this.sessions.get(id);
    if (!session) return undefined;
    const now = this.now();
    if (now >= Math.min(session.expiresAt, session.lastAccessedAt + (this.options.idleTtlMs ?? DEFAULT_IDLE_TTL_MS))) {
      this.sessions.delete(id);
      return undefined;
    }
    if (!renew) return session;
    const renewed = { ...session, lastAccessedAt: Math.min(now, session.expiresAt) };
    this.sessions.set(id, renewed);
    return renewed;
  }

  revoke(id: string): boolean {
    this.evictExpired();
    return this.sessions.delete(id);
  }

  private evictExpired(): void {
    const now = this.now();
    const idleTtlMs = this.options.idleTtlMs ?? DEFAULT_IDLE_TTL_MS;
    for (const [id, session] of this.sessions) {
      if (now >= session.expiresAt || now >= session.lastAccessedAt + idleTtlMs) this.sessions.delete(id);
    }
  }

  private newOpaqueValue(): string {
    let value = randomBytes(32).toString("base64url");
    while (this.sessions.has(value)) value = randomBytes(32).toString("base64url");
    return value;
  }

  private now(): number {
    return (this.options.clock ?? Date.now)();
  }
}

const sessionStoreKey = "__iotDeliveryServerSessions";
const globalSessionStore = globalThis as typeof globalThis & { [sessionStoreKey]?: InMemoryServerSessionStore };

export const serverSessions = globalSessionStore[sessionStoreKey]
  ?? (globalSessionStore[sessionStoreKey] = new InMemoryServerSessionStore());

export function readExactCookie(request: Request, name: string): string | undefined {
  const header = request.headers.get("cookie");
  if (!header) return undefined;
  let value: string | undefined;
  for (const segment of header.split(";")) {
    const pair = segment.trim();
    const separator = pair.indexOf("=");
    if (separator <= 0) {
      if (pair === name) return undefined;
      continue;
    }
    const cookieName = pair.slice(0, separator);
    const cookieValue = pair.slice(separator + 1);
    if (cookieName !== name) continue;
    if (value !== undefined || !cookieValue || !OPAQUE_VALUE_PATTERN.test(cookieValue)) return undefined;
    value = cookieValue;
  }
  return value;
}

export function secureEqual(left: string, right: string): boolean {
  const leftBytes = Buffer.from(left);
  const rightBytes = Buffer.from(right);
  return leftBytes.length === rightBytes.length && timingSafeEqual(leftBytes, rightBytes);
}

export function hostCookie(name: typeof SESSION_COOKIE_NAME | typeof LOGIN_COOKIE_NAME, value: string, maxAge: number): string {
  return `${name}=${value}; Max-Age=${maxAge}; Path=/; HttpOnly; Secure; SameSite=Lax`;
}

export function clearHostCookie(name: typeof SESSION_COOKIE_NAME | typeof LOGIN_COOKIE_NAME): string {
  return hostCookie(name, "", 0);
}
