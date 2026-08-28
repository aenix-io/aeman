// People on the board. The roster (GET /board metadata.members) is the only
// source of avatars and display names: there is no per-login lookup any more.
// A person IS their login — it is the identifier in filters, assignments and
// URLs — and a display name, when the forge has one (GitLab does, GitHub does
// not), only decorates the human-facing labels. An assignee who is not a
// member has no avatar URL and no name: the Avatar component draws their
// initials, and their login is what is shown.

/** Member is one person of the board roster as GET /board reports it. */
export interface Member {
  login: string;
  avatarUrl?: string;
  /** Display name; empty or absent on a GitHub board. */
  name?: string;
}

/** Avatars maps a login onto its avatar URL. */
export type Avatars = Record<string, string>;

/** Names maps a login onto its display name — only for members that have one. */
export type Names = Record<string, string>;

/** avatarsFrom builds the login → avatar URL map from the roster, skipping
 *  entries without a URL. */
export function avatarsFrom(members: readonly Member[]): Avatars {
  const out: Avatars = {};
  for (const m of members) {
    if (m.login && m.avatarUrl) {
      out[m.login] = m.avatarUrl;
    }
  }
  return out;
}

/** avatarUrlFor is the roster's avatar URL for a login, or undefined when the
 *  person is not a member (no URL is invented for them). */
export function avatarUrlFor(login: string, avatars?: Avatars): string | undefined {
  return avatars?.[login];
}

/** namesFrom builds the login → display name map from the roster, skipping
 *  entries whose name is absent or blank. */
export function namesFrom(members: readonly Member[]): Names {
  const out: Names = {};
  for (const m of members) {
    const name = m.name?.trim();
    if (m.login && name) {
      out[m.login] = name;
    }
  }
  return out;
}

/** displayName is what a person is called on screen: their display name when
 *  the roster has one, their login otherwise. Never used as an identifier. */
export function displayName(login: string, names?: Names): string {
  return names?.[login] || login;
}
