// The board's domains — the repositories its cards live in. The first one is
// the primary; a board with a single domain shows nothing of this at all.
// Pure helpers shared by the card badge, the create flows (which repository a
// new team/project/process is declared in) and the reviewer picker.

/** DomainInfo is one repository of the board as GET /board reports it. */
export interface DomainInfo {
  name: string;
  /** Whether the visitor may write to this repository. */
  writable: boolean;
  /** Logins who can read the repository (empty when the server has no
   *  membership for it). */
  members: string[];
  /** The visitor's own personal board (`~<login>`), served to them alone. */
  personal?: boolean;
}

/** isPersonalDomain tells a personal board's domain by its name: `~<login>`
 *  (the flag on DomainInfo says the same; the name works on any server). */
export function isPersonalDomain(name: string): boolean {
  return name.startsWith("~");
}

/** isMultiDomain reports whether the board spans more than one repository —
 *  the only case where domains show up in the UI. */
export function isMultiDomain(domains: readonly DomainInfo[]): boolean {
  return domains.length > 1;
}

/** primaryDomain names the first configured repository ("" when the server
 *  names none). */
export function primaryDomain(domains: readonly DomainInfo[]): string {
  return domains[0]?.name ?? "";
}

/** cardDomainBadge is the badge a card wears for its repository: null on a
 *  single-domain board, for a card in the primary (a card without a domain —
 *  an older server — counts as primary), and for a personal card, whose own
 *  column already says where it lives. */
export function cardDomainBadge(
  domains: readonly DomainInfo[],
  cardDomain: string | undefined,
): string | null {
  if (!isMultiDomain(domains)) {
    return null;
  }
  const domain = cardDomain || primaryDomain(domains);
  if (domain === primaryDomain(domains) || isPersonalDomain(domain)) {
    return null;
  }
  return domain;
}

/** writableDomains keeps the repositories the visitor may declare things in,
 *  in board order. A personal board is not one of them: it holds no teams,
 *  projects or processes (the server refuses them there), so offering it as
 *  a place to declare one only leads to a refusal. */
export function writableDomains(domains: readonly DomainInfo[]): DomainInfo[] {
  return domains.filter((d) => d.writable && !d.personal && !isPersonalDomain(d.name));
}

/** declareDomain resolves the create flows' pick: undefined (the server's
 *  default, the primary) while the visitor can write to fewer than two
 *  domains — the selector is not shown then — else the pick, defaulting to
 *  the first writable domain, which is what the selector shows. */
export function declareDomain(
  domains: readonly DomainInfo[],
  picked: string,
): string | undefined {
  const options = writableDomains(domains);
  if (options.length < 2) {
    return undefined;
  }
  return picked || options[0].name;
}

/** reviewerCandidates narrows a people list to those who can read the card's
 *  repository — a reviewer who cannot see the card is no reviewer. Falls back
 *  to everyone when the server names no domains, the card's domain is unknown,
 *  or the domain carries no membership. */
export function reviewerCandidates(
  members: readonly string[],
  domains: readonly DomainInfo[],
  cardDomain: string | undefined,
): string[] {
  if (domains.length === 0) {
    return [...members];
  }
  const name = cardDomain || primaryDomain(domains);
  const domain = domains.find((d) => d.name === name);
  if (!domain || domain.members.length === 0) {
    return [...members];
  }
  const readers = new Set(domain.members);
  return members.filter((m) => readers.has(m));
}

/** RosterDomains names the repository a team or project was declared in, for
 *  the entries that live outside the primary. A board of one repository (and
 *  an older server) sends none. */
export type RosterDomains = Record<string, string> | undefined;

/** rosterDomain is the repository an entry was declared in; "" is the
 *  primary, which is never named. */
export function rosterDomain(domains: RosterDomains, name: string): string {
  if (!name || !domains) {
    return "";
  }
  return domains[name] ?? "";
}

/** offerableTeams keeps the teams a card filed under `project` may be handed
 *  to: those declared in the same repository. The project decides where the
 *  card lives, so a team from another repository would put the card out of
 *  reach of the very people it names — the server refuses that pair, and the
 *  menu must not offer it. A card under no project is constrained by
 *  nothing, and `current` — what the card already carries — always stays in
 *  the list, so a pair written before this rule can still be seen and
 *  fixed. */
export function offerableTeams(
  teams: readonly string[],
  teamDomains: RosterDomains,
  projectDomains: RosterDomains,
  project: string,
  current = "",
): string[] {
  if (!project) {
    return [...teams];
  }
  const home = rosterDomain(projectDomains, project);
  return teams.filter(
    (t) => t === "" || t === current || rosterDomain(teamDomains, t) === home,
  );
}

/** offerableProjects is the same rule from the other side: a card carrying a
 *  team may only be filed under a project of that team's repository. */
export function offerableProjects(
  projects: readonly string[],
  projectDomains: RosterDomains,
  teamDomains: RosterDomains,
  team: string,
  current = "",
): string[] {
  if (!team) {
    return [...projects];
  }
  const home = rosterDomain(teamDomains, team);
  return projects.filter(
    (p) => p === current || rosterDomain(projectDomains, p) === home,
  );
}
