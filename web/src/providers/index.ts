import { githubProvider } from "./github/githubProvider";
import type { Provider, ProviderId } from "./types";

export const providers: Record<ProviderId, Provider> = {
  github: githubProvider,
};

/** getProvider returns the registered provider for the given id. */
export function getProvider(id: ProviderId = "github"): Provider {
  return providers[id];
}
