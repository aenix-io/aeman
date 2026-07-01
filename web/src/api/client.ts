// Thin client over the aeman Go server: /api/config for bootstrap data and
// /api/github/graphql which the server proxies to the GitHub API with a token
// from the local gh CLI.

/** clientId identifies this browser tab to the server: REST mutations carry it
 * as X-Aeman-Client and the watch subscription passes it as ?client=, so the
 * server does not echo a tab's own changes back over its watch stream. */
export const clientId: string =
  typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `c-${Math.random().toString(36).slice(2)}`;

export interface AppConfig {
  mode: string;
  version: string;
  login?: string;
  tokenAvailable: boolean;
  /** Whether the visitor is signed in (always true in local gh-token mode). */
  authenticated: boolean;
  /** OAuth mode: URL to start the GitHub sign-in flow. */
  authUrl?: string;
  /** OAuth mode: URL to sign out. */
  logoutUrl?: string;
  defaultOwner?: string;
  defaultProject?: number;
  /** Pin the UI to defaultOwner/defaultProject and hide the board picker. */
  lockBoard?: boolean;
}

export async function fetchConfig(): Promise<AppConfig> {
  const res = await fetch("/api/config");
  if (!res.ok) {
    throw new Error(`config request failed: HTTP ${res.status}`);
  }
  return (await res.json()) as AppConfig;
}

interface GraphQLError {
  message: string;
}

interface GraphQLResponse<T> {
  data?: T;
  errors?: GraphQLError[];
}

export async function graphql<T>(
  query: string,
  variables: Record<string, unknown>,
): Promise<T> {
  const res = await fetch("/api/github/graphql", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ query, variables }),
  });
  let body: GraphQLResponse<T>;
  try {
    body = (await res.json()) as GraphQLResponse<T>;
  } catch {
    throw new Error(`GitHub API returned HTTP ${res.status} with no JSON body`);
  }
  if (body.errors && body.errors.length > 0) {
    throw new Error(body.errors.map((e) => e.message).join("; "));
  }
  if (!res.ok || !body.data) {
    throw new Error(`GitHub API request failed: HTTP ${res.status}`);
  }
  return body.data;
}
