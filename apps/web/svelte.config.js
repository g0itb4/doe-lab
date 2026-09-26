import adapter from "@sveltejs/adapter-static";
import { vitePreprocess } from "@sveltejs/vite-plugin-svelte";

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
        "script-src": ["self"],
        // 'unsafe-inline' is for style attributes: uPlot positions its
        // cursor, legend and selection with them.
        "style-src": ["self", "unsafe-inline"],
        "img-src": ["self", "data:"],
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
