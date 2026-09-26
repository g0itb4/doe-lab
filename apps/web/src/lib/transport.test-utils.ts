import type { Transport } from "@connectrpc/connect";

// transport.ts builds its transport at module scope, so mocking the factory
// is the only way in. The stub is a stable transport that forwards to
// whichever router the current test installed, read through the getter on
// every call; createRouterTransport is Connect's own in-memory server, so the
// generated clients are exercised for real.
//
//   let router: Transport;
//   vi.mock("@connectrpc/connect-web", async () => {
//     const { transportStub } = await import("$lib/transport.test-utils.ts");
//     return transportStub(() => router);
//   });
export function transportStub(getRouter: () => Transport) {
  return {
    createConnectTransport: () => ({
      unary: (...args: Parameters<Transport["unary"]>) => getRouter().unary(...args),
      stream: (...args: Parameters<Transport["stream"]>) => getRouter().stream(...args),
    }),
  };
}
