export async function loginThroughUI(context, webBase, organizationId, userId, password, expectedDisplayName) {
  await context.navigate(webBase);
  await context.waitFor(`Boolean(document.querySelector('form[aria-label="本地成员登录"]'))`);
  await context.evaluate(`(() => {
    const values = ${JSON.stringify({ organizationId, userId, password })};
    const form = document.querySelector('form[aria-label="本地成员登录"]');
    if (!form) throw new Error('local login form missing');
    for (const [name, value] of Object.entries(values)) {
      const input = form.querySelector('[name="' + name + '"]');
      if (!(input instanceof HTMLInputElement)) throw new Error('login field missing: ' + name);
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;
      setter.call(input, value);
      input.dispatchEvent(new Event('input', { bubbles: true }));
      input.dispatchEvent(new Event('change', { bubbles: true }));
    }
    form.requestSubmit();
    return true;
  })()`);
  await context.waitFor(`document.body.innerText.includes(${JSON.stringify(expectedDisplayName)}) && !document.querySelector('form[aria-label="本地成员登录"]')`);
}

export async function browserRequest(context, requestPath, options = {}) {
  const method = String(options.method ?? "GET").toUpperCase();
  const csrf = options.csrf ?? true;
  const explicitCSRF = options.csrfToken ?? null;
  const serializedBody = options.body === undefined ? null : JSON.stringify(options.body);
  return context.evaluate(`(async () => {
    const method = ${JSON.stringify(method)};
    const headers = new Headers({ Accept: 'application/json' });
    let csrfToken = ${JSON.stringify(explicitCSRF)};
    if (${JSON.stringify(csrf)} && !['GET', 'HEAD', 'OPTIONS'].includes(method) && !csrfToken && ${JSON.stringify(requestPath)} !== '/auth/local/login') {
      const sessionResponse = await fetch('/auth/session', { method: 'GET', credentials: 'same-origin', cache: 'no-store', headers: { Accept: 'application/json' } });
      const sessionText = await sessionResponse.text();
      let sessionBody = null;
      try { sessionBody = sessionText ? JSON.parse(sessionText) : null; } catch {}
      if (!sessionResponse.ok) return { status: sessionResponse.status, body: sessionBody, phase: 'csrf-session' };
      csrfToken = sessionBody?.csrfToken;
    }
    if (csrfToken) headers.set('X-CSRF-Token', csrfToken);
    const body = ${JSON.stringify(serializedBody)};
    if (body !== null) headers.set('Content-Type', 'application/json');
    const response = await fetch(${JSON.stringify(requestPath)}, {
      method,
      headers,
      credentials: 'same-origin',
      cache: 'no-store',
      ...(body !== null ? { body } : {}),
    });
    const text = await response.text();
    let parsed = null;
    try { parsed = text ? JSON.parse(text) : null; } catch { parsed = text; }
    return { status: response.status, body: parsed, phase: 'request' };
  })()`);
}

export async function localCookies(context) {
  const cookies = await context.cookies();
  return {
    session: cookies.find((cookie) => cookie.name === "__Host-iotd_local_session")?.value,
    csrf: cookies.find((cookie) => cookie.name === "__Host-iotd_local_csrf")?.value,
  };
}

export function expectStatus(result, status, label) {
  if (result?.status !== status) {
    throw new Error(`${label}: expected HTTP ${status}, got ${result?.status}; body=${JSON.stringify(result?.body)}`);
  }
  return result;
}

export function assert(condition, message) {
  if (!condition) throw new Error(`YU-29 assertion failed: ${message}`);
}
