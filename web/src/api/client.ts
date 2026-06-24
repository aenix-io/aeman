// Thin client over the aeman Go server: /api/config for bootstrap data and
// /api/github/graphql which the server proxies to the GitHub API with a token
// from the local gh CLI.

export interface AppConfig {
  mode: string;
  version: string;
  login?: string;
  tokenAvailable: boolean;
  defaultOwner?: string;
  defaultProject?: number;
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
