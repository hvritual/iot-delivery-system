// R2 capability detection is intentionally narrow: a missing endpoint in the
// old comparison backend can degrade to the dashboard-only experience, while
// authentication, network, and server errors must remain visible to operators.
export async function loadR2Workspace(loaders) {
  const entries = Object.entries(loaders ?? {});
  const resolved = await Promise.all(entries.map(async ([key, descriptor]) => {
    try {
      return [key, await descriptor.load(), false];
    } catch (cause) {
      if (!isUnsupportedR2Endpoint(cause)) throw cause;
      return [key, descriptor.fallback, true];
    }
  }));
  const values = {};
  let unavailable = false;
  resolved.forEach(([key, value, isUnavailable]) => {
    values[key] = value;
    unavailable ||= isUnavailable;
  });
  return { values, available: !unavailable };
}

export function isUnsupportedR2Endpoint(cause) {
  return cause?.status === 404 || cause?.status === 501;
}
