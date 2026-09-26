// A page's address with some query parameters changed: a string sets one,
// null removes it. The state of a view (range, zoom, filter, tab) lives in
// the address, so it can be shared and the back button works.
export function withQuery(url: URL, changes: Record<string, string | null>): string {
  const params = new URLSearchParams(url.search);
  for (const [key, value] of Object.entries(changes)) {
    if (value === null) params.delete(key);
    else params.set(key, value);
  }
  const query = params.toString();
  return url.pathname + (query ? `?${query}` : "");
}
