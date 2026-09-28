// A static app: no server renders it at run time. Every route that can be is
// prerendered at build time to a page with its headings, its text and the
// skeletons of its data, so the first paint does not wait for JavaScript; the
// data arrives in the browser. /sites/[nmi] cannot be prerendered and is
// served from the fallback shell.
export const prerender = true;
