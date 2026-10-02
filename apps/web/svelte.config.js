import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import adapter from "@sveltejs/adapter-static";
import { vitePreprocess } from "@sveltejs/vite-plugin-svelte";

// The one inline script of app.html, which SvelteKit does not know about: its
// hash goes into the CSP, computed from the file so the two cannot drift.
const shell = readFileSync(new URL("./src/app.html", import.meta.url), "utf8");
const inline = /<script>([\s\S]*?)<\/script>/.exec(shell);
if (!inline) throw new Error("src/app.html has no inline script; remove its hash from the CSP");
const inlineHash = /** @type {`sha256-${string}`} */ (
  `sha256-${createHash("sha256").update(inline[1]).digest("base64")}`
);

/** @type {import('@sveltejs/kit').Config} */
export default {
  preprocess: vitePreprocess(),
  kit: {
    adapter: adapter({
      pages: "dist",
      assets: "dist",
      // 200.html, not index.html: `/` is itself prerendered and the two would
      // collide. /sites/[nmi] falls through to it, because it cannot be
      // prerendered without knowing every NMI.
      fallback: "200.html",
      // .br and .gz beside every file, built once at the highest quality.
      // Caddy's file_server serves them; it cannot brotli on the fly.
      precompress: true,
      strict: true,
    }),
    // Hash mode: the shells are files on disk, so a nonce cannot differ per
    // response. SvelteKit hashes the bootstrap script it inlines.
    csp: {
      mode: "hash",
      directives: {
        "default-src": ["none"],
        "script-src": ["self", inlineHash],
        // 'unsafe-inline' is for style attributes: uPlot positions its
        // cursor, legend and selection with them.
        "style-src": ["self", "unsafe-inline"],
        // The street tiles of the fleet map are the one thing the browser
        // fetches from another host (FleetMap.svelte).
        "img-src": ["self", "data:", "https://tile.openstreetmap.org"],
        "font-src": ["self"],
        // /rpc is same-origin. The browser calls nothing else.
        "connect-src": ["self"],
        "base-uri": ["none"],
        "form-action": ["none"],
        "object-src": ["none"],
        "manifest-src": ["self"],
      },
    },
  },
};
