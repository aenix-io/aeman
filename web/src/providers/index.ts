import { apiProvider } from "./api/apiProvider";
import type { Provider, ProviderId } from "./types";

// The app talks to its own REST + WebSocket API (which fronts GitHub) rather than
// GitHub's GraphQL directly, so every read, write and live update flows through
// one backend. githubProvider remains in the tree as the reference backend.
export const providers: Record<ProviderId, Provider> = {
  github: apiProvider,
};

/** getProvider returns the registered provider for the given id. */
export function getProvider(id: ProviderId = "github"): Provider {
  return providers[id];
}
