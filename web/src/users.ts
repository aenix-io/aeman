// People on the board. The roster (GET /board metadata.members) is the only
// source of avatars: there is no per-login lookup any more, and no display
// names — a person is their login. An assignee who is not a member has no
// avatar URL, and the Avatar component draws their initials instead.

/** Member is one person of the board roster as GET /board reports it. */
export interface Member {
  login: string;
  avatarUrl?: string;
}

/** Avatars maps a login onto its avatar URL. */
export type Avatars = Record<string, string>;

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
