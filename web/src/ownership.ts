// Owner, team and customers on the plan (docs/design/ownership.md). The
// server computes a card's effective team and customers (status.effectiveTeam
// / effectiveCustomers, board.Index); the board works them out the same way
// from the cards it holds, so an edit to an ancestor shows below at once,
// and falls back to the server's where the ancestors are not on the board.
import { HIDE_DONE, isStatusFilter } from "./workstatus";
import type { Card } from "./providers/types";

type Held = readonly Pick<Card, "itemId" | "title" | "parent" | "team" | "customers" | "reviewOf" | "effectiveTeam" | "type">[];

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

type HeldCard = Held[number];

/** effectiveIn is effectiveTeamOf over a map of the held cards by id. */
function effectiveIn(byId: ReadonlyMap<string, HeldCard>, c: HeldCard): string {
  if (c.team) {
    return c.team;
  }
  // A review card counts under its task's (O13).
  if (c.reviewOf) {
    const task = byId.get(c.reviewOf);
    return task && !task.reviewOf ? effectiveIn(byId, task) : (c.effectiveTeam ?? "");
  }
  const seen = new Set([c.itemId]);
  for (let id = c.parent, hops = 0; id && hops < 7; hops++) {
    const p = byId.get(id);
    if (!p) {
      return c.effectiveTeam ?? ""; // an ancestor the board does not hold: ask the server's
    }
    if (seen.has(p.itemId)) {
      break;
    }
    seen.add(p.itemId);
    if (p.team) {
      return p.team;
    }
    id = p.parent;
  }
  return "";
}

const byIdOf = (cards: Held) => new Map(cards.map((x) => [x.itemId, x]));

/** effectiveTeamOf is the team a card counts under (O3): its own, else the
 *  nearest ancestor's that has one; a review card's, its task's. */
export function effectiveTeamOf(c: HeldCard, cards: Held): string {
  return effectiveIn(byIdOf(cards), c);
}

/** placerOf reads the held cards once and answers, card by card, the team
 *  each STANDS UNDER on the day boards and backlogs (O13): its effective
 *  team, a plan item keeping its own — the server's board.PlacedTeam. A
 *  board builds one per render rather than a map per card. */
export function placerOf(cards: Held): (c: HeldCard) => string {
  const byId = byIdOf(cards);
  return (c) => (c.type ? (c.team ?? "") : effectiveIn(byId, c));
}

/** placedTeamOf is one card's placed team (placerOf, for a single read). */
export function placedTeamOf(c: HeldCard, cards: Held): string {
  return placerOf(cards)(c);
}

/** sprintTeamOf is the team a card's sprint is resolved by (O13) — the
 *  team a gesture names when it names one; "" names none, so the card's
 *  own team is off and it stands under what it inherits; left out, the
 *  team it stands under now — as currentSprint takes it: null for the
 *  no-team group. */
export function sprintTeamOf(c: HeldCard, cards: Held, team?: string): string | null {
  if (team) {
    return team;
  }
  const placed = placedTeamOf(team === "" ? { ...c, team: "" } : c, cards);
  return placed || null;
}

/** inheritedTeamOf is the team a card takes from an ancestor — or, a
 *  review card, from its task — and which one: null when the card has a
 *  team of its own or inherits none. */
export function inheritedTeamOf(c: Card, cards: Held): { team: string; from: string } | null {
  if (c.team) {
    return null;
  }
  // A review card counts under its task's (O13), and says so.
  if (c.reviewOf) {
    const task = cards.find((x) => x.itemId === c.reviewOf);
    if (!task || task.reviewOf) {
      return c.effectiveTeam ? { team: c.effectiveTeam, from: "" } : null;
    }
    const team = placedTeamOf(task, cards);
    return team ? { team, from: task.title } : null;
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
 *  task's (O6). */
export function effectiveCustomersOf(c: Card, cards: Held): string[] {
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
  // And those the work waiting for it — or for an ancestor — brings
  // (status.dependentCustomers, dependencies.md).
  for (const a of up) {
    add((a as Card).dependentCustomers?.map((d) => d.customer));
  }
  add(c.dependentCustomers?.map((d) => d.customer));
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

/** bornCustomers is what a card typed into a day board is born for while
 *  its Customer filter stands: the one customer chosen, so it stays on the
 *  board it was typed into; with none or several chosen, nobody. */
export function bornCustomers(chosen: readonly string[]): string[] | undefined {
  return chosen.length === 1 ? [chosen[0]] : undefined;
}

/** customerPasses is the day boards' Customer filter on one card (O11): it
 *  passes when nothing is chosen, or when one of its effective customers is
 *  — the server's rule for the listing and the watch, mirrored for the cards
 *  a board holds. `kept` are the cards the board itself made under the
 *  filter with several customers chosen (bornCustomers): shown until the
 *  filter changes, rather than gone the moment they are typed in. */
export function customerPasses(
  c: Card,
  cards: Held,
  chosen: readonly string[] | null,
  kept?: ReadonlySet<string>,
): boolean {
  if (!chosen || chosen.length === 0 || kept?.has(c.itemId)) {
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
 *  each with the nearest ancestor that names it, then those that arrive
 *  through the work that waits for the card (status.dependentCustomers),
 *  then the card's own. */
export function customerChips(c: Card, cards: Held): CustomerChip[] {
  const own = c.customers ?? [];
  const up = ancestorsOf(c, cards) ?? [];
  const out: CustomerChip[] = [];
  for (const id of effectiveCustomersOf(c, cards)) {
    if (own.includes(id)) {
      continue;
    }
    const from = [...up].reverse().find((a) => (a.customers ?? []).includes(id));
    // Else it arrives through the work that waits for this card or for an
    // ancestor (dependencies.md): from the nearest card that waits.
    const waiting = [c, ...[...up].reverse()]
      .flatMap((a) => (a as Card).dependentCustomers ?? [])
      .find((d) => d.customer === id);
    out.push({ id, from: from?.title ?? waiting?.from.title ?? "" });
  }
  // The server's own word for one it brings that the held cards cannot
  // place (a card off the board): once, when not already named.
  for (const d of c.dependentCustomers ?? []) {
    if (!own.includes(d.customer) && !out.some((x) => x.id === d.customer)) {
      out.push({ id: d.customer, from: d.from.title });
    }
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

/** RoadmapFilters are the Roadmap's team, customer, owner and status
 *  filters (O7, O8), passed to the server's listing and watch. Statuses are
 *  the Status filter's values (workstatus.ts): in-progress, backlog,
 *  locked, no-items, done. */
export interface RoadmapFilters {
  teams: string[];
  customers: string[];
  owners: string[];
  statuses: string[];
}

export const NO_ROADMAP_FILTERS: RoadmapFilters = { teams: [], customers: [], owners: [], statuses: [] };

/** ROADMAP_FILTERS_KEY is where the viewer's choice is remembered. */
export const ROADMAP_FILTERS_KEY = "aeman.roadmap.filters";

/** ROADMAP_FILTERS_VERSION marks a choice stored since the Status filter
 *  took Locked and No items (O14). */
const ROADMAP_FILTERS_VERSION = 2;

/** storeRoadmapFilters writes the choice to remember, versioned. */
export function storeRoadmapFilters(f: RoadmapFilters): string {
  return JSON.stringify({ ...f, v: ROADMAP_FILTERS_VERSION });
}

/** storedRoadmapFilters reads the remembered choice; anything else is no
 *  filter. A Hide done stored before Locked and No items existed — In
 *  progress + Backlog, then the only way to say "everything but done" —
 *  reads as today's Hide done. */
export function storedRoadmapFilters(raw: string | null): RoadmapFilters {
  if (!raw) {
    return { ...NO_ROADMAP_FILTERS };
  }
  try {
    const v = JSON.parse(raw) as Partial<RoadmapFilters> & { v?: unknown };
    const list = (x: unknown) => (Array.isArray(x) ? x.filter((s): s is string => typeof s === "string") : []);
    let statuses: string[] = list(v.statuses).filter(isStatusFilter);
    const oldHideDone = statuses.length === 2 && statuses.includes("backlog") && statuses.includes("in-progress");
    if (v.v !== ROADMAP_FILTERS_VERSION && oldHideDone) {
      statuses = [...HIDE_DONE];
    }
    return { teams: list(v.teams), customers: list(v.customers), owners: list(v.owners), statuses };
  } catch {
    return { ...NO_ROADMAP_FILTERS };
  }
}
