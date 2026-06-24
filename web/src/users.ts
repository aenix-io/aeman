import { graphql } from "./api/client";

export interface GhUser {
  login: string;
  name?: string;
  avatarUrl: string;
}

/** fetchUsers loads name + avatar for the given GitHub logins in one query. */
export async function fetchUsers(
  logins: string[],
): Promise<Record<string, GhUser>> {
  const uniq = [...new Set(logins.filter(Boolean))];
  if (uniq.length === 0) {
    return {};
  }
  const decls = uniq.map((_, i) => `$l${i}: String!`).join(", ");
  const body = uniq
    .map((_, i) => `u${i}: user(login: $l${i}) { login name avatarUrl }`)
    .join("\n");
  const query = `query(${decls}) {\n${body}\n}`;
  const vars: Record<string, string> = {};
  uniq.forEach((l, i) => {
    vars[`l${i}`] = l;
  });
  const data = await graphql<Record<string, GhUser | null>>(query, vars);
  const out: Record<string, GhUser> = {};
  for (const value of Object.values(data)) {
    if (value) {
      out[value.login] = value;
    }
  }
  return out;
}

/** avatarUrlFor returns a usable avatar URL, falling back to GitHub's by login. */
export function avatarUrlFor(login: string, user?: GhUser): string {
  return user?.avatarUrl ?? `https://github.com/${login}.png?size=48`;
}

/** displayName formats a person like GitHub: "Full Name (login)" or "login". */
export function displayName(login: string, user?: GhUser): string {
  return user?.name ? `${user.name} (${login})` : login;
}
