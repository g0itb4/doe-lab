import { browser } from "$app/environment";

// A page's address with some query parameters changed: a string sets one,
// null removes it. The state of a view (range, zoom, filter, tab) lives in
// the address, so it can be shared and the back button works.
//
// While a page is prerendered there is no query to read (SvelteKit refuses
// the question, rightly: the file on disk is the same for every query), so
// the helpers answer as for an address with none.
export function withQuery(url: URL, changes: Record<string, string | null>): string {
  const params = new URLSearchParams(browser ? url.search : "");
  for (const [key, value] of Object.entries(changes)) {
    if (value === null) params.delete(key);
    else params.set(key, value);
  }
  const query = params.toString();
  return url.pathname + (query ? `?${query}` : "");
}

// One query parameter of an address; null when it has none.
export function queryParam(url: URL, name: string): string | null {
  return browser ? url.searchParams.get(name) : null;
}
