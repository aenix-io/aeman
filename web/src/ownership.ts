// Owner, team and customers on the plan (docs/design/ownership.md). The
// server computes a card's effective team and customers (status.effectiveTeam
// / effectiveCustomers, board.Index); the board works them out the same way
// from the cards it holds, so an edit to an ancestor shows below at once,
// and falls back to the server's where the ancestors are not on the board.
import type { Card } from "./providers/types";

type Held = readonly Pick<Card, "itemId" | "title" | "parent" | "team" | "customers" | "reviewOf">[];

function ancestorsOf(c: Pick<Card, "itemId" | "parent">, cards: Held): Held[number][] | null {
  const byId = new Map(cards.map((x) => [x.itemId, x]));
  const up: Held[number][] = [];
  const seen = new Set([c.itemId]);
  for (let id = c.parent; id; ) {
    const p = byId.get(id);
    if (!p) {
      return null; // an ancestor the board does not hold: ask the server's
    }
    if (seen.has(p.itemId) || up.length > 6) {
      break;
    }
    seen.add(p.itemId);
    up.unshift(p);
    id = p.parent;
  }
  return up;
}

/** effectiveTeamOf is the team a card counts under (O3): its own, else the
 *  nearest ancestor's that has one. A project card has none (H4). */
export function effectiveTeamOf(c: Card, cards: Held): string {
  if (c.scope) {
    return "";
  }
  if (c.team) {
    return c.team;
  }
  const up = ancestorsOf(c, cards);
  if (!up) {
    return c.effectiveTeam ?? "";
  }
  for (let i = up.length - 1; i >= 0; i--) {
    if (up[i].team) {
      return up[i].team ?? "";
    }
  }
  return "";
}

/** inheritedTeamOf is the team a card takes from an ancestor, and which one
 *  — null when the card has a team of its own or inherits none. */
export function inheritedTeamOf(c: Card, cards: Held): { team: string; from: string } | null {
  if (c.team || c.scope) {
    return null;
  }
  const up = ancestorsOf(c, cards);
  if (!up) {
    return c.effectiveTeam ? { team: c.effectiveTeam, from: "" } : null;
  }
  for (let i = up.length - 1; i >= 0; i--) {
    if (up[i].team) {
      return { team: up[i].team ?? "", from: up[i].title };
    }
  }
  return null;
}

/** effectiveCustomersOf is who a card is for (O4): every ancestor's customers
 *  and its own, the ancestors' first, each once; a review card's are its
 *  task's (O6), a project card's none. */
export function effectiveCustomersOf(c: Card, cards: Held): string[] {
  if (c.scope) {
    return [];
  }
  const up = ancestorsOf(c, cards);
  if (!up) {
    return c.effectiveCustomers ?? c.customers ?? [];
  }
  const out: string[] = [];
  const add = (ids?: string[]) => {
    for (const id of ids ?? []) {
      if (id && !out.includes(id)) {
        out.push(id);
      }
    }
  };
  if (c.reviewOf) {
    const task = cards.find((x) => x.itemId === c.reviewOf);
    if (!task) {
      // A task the board does not hold (another team's): the server's word.
      return c.effectiveCustomers ?? [];
    }
    if (!task.reviewOf) {
      add(effectiveCustomersOf(task as Card, cards));
    }
  }
  for (const a of up) {
    add(a.customers);
  }
  add(c.customers);
  return out;
}

/** roadmapOwnerCell is what the Roadmap's Owner column shows for a card:
 *  its own team and person (a plan item's owner, a task's first assignee),
 *  and — when it has no team of its own — the team it takes from above, with
 *  the title it comes from (`from`, "" when only the server knows), to be
 *  drawn greyed (O3). A task somebody holds shows them as it is; a plan
 *  item's owner is its own only, never inherited (O2). */
export function roadmapOwnerCell(
  c: Card,
  cards: Held,
  planning: boolean,
): { owner: { team: string; person: string }; from?: string } {
  const own = { team: c.team ?? "", person: planning ? (c.owner ?? "") : (c.assignees?.[0] ?? "") };
  if (own.team || (!planning && own.person)) {
    return { owner: own };
  }
  const up = inheritedTeamOf(c, cards);
  return up ? { owner: { ...own, team: up.team }, from: up.from } : { owner: own };
}

/** customerPasses is the day boards' Customer filter on one card (O11): it
 *  passes when nothing is chosen, or when one of its effective customers is
 *  — the server's rule for the listing and the watch, mirrored for the cards
 *  a board holds. */
export function customerPasses(c: Card, cards: Held, chosen: readonly string[] | null): boolean {
  if (!chosen || chosen.length === 0) {
    return true;
  }
  return effectiveCustomersOf(c, cards).some((id) => chosen.includes(id));
}

/** CustomerChip is one customer in the drawer's Customers field: the card's
 *  own (removable there), or inherited from an ancestor (`from`, grey, not
 *  removable there). */
export interface CustomerChip {
  id: string;
  from?: string;
}

/** customerChips is the drawer's Customers field: the inherited ones first,
 *  each with the nearest ancestor that names it, then the card's own. */
export function customerChips(c: Card, cards: Held): CustomerChip[] {
  const own = c.customers ?? [];
  const up = ancestorsOf(c, cards) ?? [];
  const out: CustomerChip[] = [];
  for (const id of effectiveCustomersOf(c, cards)) {
    if (own.includes(id)) {
      continue;
    }
    const from = [...up].reverse().find((a) => (a.customers ?? []).includes(id));
    out.push({ id, from: from?.title ?? "" });
  }
  for (const id of own) {
    out.push({ id });
  }
  return out;
}

/** ownerFilterOptions is whom the Roadmap's Owner filter offers — its one
 *  person filter (O8): the board's members, every plan item's owner, every
 *  task's assignee, whoever is already chosen and whoever the board has
 *  shown before (`seen`: a filtered listing holds only the filter's answer,
 *  and the options must not shrink with it), once each, sorted. */
export function ownerFilterOptions(
  members: readonly string[],
  cards: readonly Pick<Card, "owner" | "assignees">[],
  selected: readonly string[],
  seen: Iterable<string> = [],
): string[] {
  const out = new Set<string>(members);
  for (const p of seen) {
    out.add(p);
  }
  for (const c of cards) {
    if (c.owner) {
      out.add(c.owner);
    }
    for (const a of c.assignees ?? []) {
      out.add(a);
    }
  }
  for (const s of selected) {
    out.add(s);
  }
  return [...out].sort();
}

/** peopleOf is every plan item's owner and every task's assignee. */
export function peopleOf(cards: readonly Pick<Card, "owner" | "assignees">[]): string[] {
  const out = new Set<string>();
  for (const c of cards) {
    if (c.owner) {
      out.add(c.owner);
    }
    for (const a of c.assignees ?? []) {
      out.add(a);
    }
  }
  return [...out];
}

/** RoadmapFilters are the Roadmap's team, customer and owner filters
 *  (O7, O8), passed to the server's listing and watch. */
export interface RoadmapFilters {
  teams: string[];
  customers: string[];
  owners: string[];
}

export const NO_ROADMAP_FILTERS: RoadmapFilters = { teams: [], customers: [], owners: [] };

/** ROADMAP_FILTERS_KEY is where the viewer's choice is remembered. */
export const ROADMAP_FILTERS_KEY = "aeman.roadmap.filters";

/** storedRoadmapFilters reads the remembered choice; anything else is no
 *  filter. */
export function storedRoadmapFilters(raw: string | null): RoadmapFilters {
  if (!raw) {
    return { ...NO_ROADMAP_FILTERS };
  }
  try {
    const v = JSON.parse(raw) as Partial<RoadmapFilters>;
    const list = (x: unknown) => (Array.isArray(x) ? x.filter((s): s is string => typeof s === "string") : []);
    return { teams: list(v.teams), customers: list(v.customers), owners: list(v.owners) };
  } catch {
    return { ...NO_ROADMAP_FILTERS };
  }
}
