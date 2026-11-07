// The board's domains — the repositories its cards live in. The first one is
// the primary; a board with a single domain shows nothing of this at all.
// Pure helpers shared by the card badge, the create flows (which repository a
// new team is declared in) and the reviewer picker.

/** DomainInfo is one repository of the board as GET /board reports it. The
 *  board lists them whatever it spans — one entry for a single-repository
 *  board — so these names are what every stamp in the payload is compared
 *  against (G59). */
export interface DomainInfo {
  name: string;
  /** Whether the visitor may write to this repository. */
  writable: boolean;
  /** Logins who can read the repository (empty when the server has no
   *  membership for it). */
  members: string[];
}

/** isMultiDomain reports whether the board spans more than one repository —
 *  the only case where domains show up in the UI. */
export function isMultiDomain(domains: readonly DomainInfo[]): boolean {
  return domains.length > 1;
}

/** primaryDomain names the first configured repository ("" when the server
 *  names none) — the repository a board's own entries belong to. The
 *  server stamps every roster entry with its domain's NAME, the primary
 *  included, while a card that nothing places carries none, so a client
 *  comparing the two reads them through this name (inPrimary), exactly as
 *  board.inPrimary does on the server. */
export function primaryDomain(domains: readonly DomainInfo[]): string {
  return domains[0]?.name ?? "";
}

/** cardDomainBadge is the badge a card wears for its repository: null on a
 *  single-domain board, and for a card in the primary (a card without a
 *  domain — an older server — counts as primary). */
export function cardDomainBadge(
  domains: readonly DomainInfo[],
  cardDomain: string | undefined,
): string | null {
  if (!isMultiDomain(domains)) {
    return null;
  }
  const domain = cardDomain || primaryDomain(domains);
  if (domain === primaryDomain(domains)) {
    return null;
  }
  return domain;
}

/** writableDomains keeps the repositories the visitor may declare things in,
 *  in board order. */
export function writableDomains(domains: readonly DomainInfo[]): DomainInfo[] {
  return domains.filter((d) => d.writable);
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

/** inPrimary reads an entry's stamp in that one namespace: no stamp means
 *  the primary. */
export function inPrimary(domain: string | undefined, primary: string): string {
  return domain ? domain : primary;
}
