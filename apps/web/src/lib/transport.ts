import { createConnectTransport } from "@connectrpc/connect-web";

// The one transport every client shares. /rpc is same-origin: the dev server
// and Caddy both proxy it to the API. Reads go out as GET, so the edge can
// cache what is cacheable and a reader of the network tab sees the request.
export const transport = createConnectTransport({ baseUrl: "/rpc", useHttpGet: true });
