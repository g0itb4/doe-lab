import { fileURLToPath } from "node:url";
import { sveltekit } from "@sveltejs/kit/vite";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";

// The defaults are the ports of `just dev`. Both can be moved, to run a second
// copy of the app beside the first: WEB_PORT for this server, API_URL for the
// API it proxies to.
const port = Number(process.env.WEB_PORT ?? 5273);
const apiURL = process.env.API_URL ?? "http://localhost:3100";

export default defineConfig({
  plugins: [tailwindcss(), sveltekit()],
  server: {
    port,
    strictPort: true,
    fs: {
      // The monorepo root, named explicitly: @doelab/gen resolves through a
      // workspace symlink into packages/gen, and Vite serves a linked package
      // from its real path, which must be inside the allow list.
      allow: [fileURLToPath(new URL("../..", import.meta.url))],
    },
    // Only /rpc. `ws` is off: the API streams over HTTP, not WebSocket.
    proxy: { "/rpc": { target: apiURL, changeOrigin: true } },
  },
  // `just preview` serves the production build. preview.proxy defaults to
  // server.proxy, so /rpc is already routed.
  preview: { port: 4273, strictPort: true },
});
