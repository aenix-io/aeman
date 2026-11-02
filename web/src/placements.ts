// Placements: where a card stands on the Project board, and where it may be
// attached to. The server is the authority (boardservice/refile.go, the
// scope and project actions); everything here exists so the UI offers only
// what the server would accept, and mirrors its outcomes optimistically.

import { addDays } from "./date";
import { parkable } from "./backlog";
import { inPrimary, primaryDomain, rosterDomain, type DomainInfo, type RosterDomains } from "./domains";
import { isPlanning, isSubtask } from "./subtasks";
import type { Card, CardPatch, ScopeRef } from "./providers/types";

/** ProjectTargets is one project the picker offers, with its columns. */
export interface ProjectTargets {
  name: string;
  scopes: string[];
}

/** RosterHomes is the roster's side of a domain question: which repository
 *  each project was declared in, the board's primary — the name an
 *  unstamped entry means — and whether the board draws any repository
 *  boundaries at all. Build it with rosterOf(board). */
export interface RosterHomes {
  projectDomains?: RosterDomains;
  primary?: string;
  /** A board of ONE repository (or one that names none) has nowhere else
   *  for anything to be: every domain question answers yes there, and the
   *  server would accept every one of them. */
  single?: boolean;
}

/** sameRepository is the ONE comparison the domain rules make: two stamps
 *  read in the board's single namespace (an unstamped entry is the
 *  primary), with a board that draws no boundaries answering yes to
 *  everything. Every rule here goes through it — one of them comparing raw
 *  stamps is how a board that stamps everything and names one repository
 *  came to have two names for it. */
export function sameRepository(
  a: string | undefined,
  b: string | undefined,
  roster: RosterHomes,
): boolean {
  if (roster.single) {
    return true;
  }
  const primary = roster.primary ?? "";
  return inPrimary(a, primary) === inPrimary(b, primary);
}

/** rosterOf reads the roster's domain side off a board, so a caller that
 *  has the board passes the board and nothing else has to remember which
 *  two fields the domain rules need. */
export function rosterOf(board: {
  projectDomains?: RosterDomains;
  domains?: readonly DomainInfo[];
}): RosterHomes {
  const domains = board.domains ?? [];
  return {
    projectDomains: board.projectDomains,
    primary: primaryDomain(domains),
    // A board that lists one repository — or, from a server that named
    // none, no list at all — has no boundary to police. Reading its
    // absent primary as "" while every entry carries the repository's
    // NAME is the split this flag exists to make impossible.
    single: domains.length < 2,
  };
}

/** attachTargets is what a card outside every project may be attached to,
 *  with the columns of each: those the server would accept. A project with
 *  no columns is not offered — a column is where a card lands. */
export function attachTargets(
  projects: readonly string[],
  scopes: readonly ScopeRef[],
  cardDomain: string,
  teamBound = true,
  roster: RosterHomes = {},
): ProjectTargets[] {
  const primary = roster.primary ?? "";
  const home = inPrimary(cardDomain, primary);
  const out: ProjectTargets[] = [];
  for (const p of projectsWithColumns(projects, scopes)) {
    // Where the card will BE after the attach, not where it is: the new
    // PROJECT decides (G14), and it carries the card into its own
    // repository — which is the move the no-project bucket and the
    // cross-repository attach both exist for. The BUCKET carries nothing:
    // it has no project to decide, so a card attached there stays where it
    // already was.
    const lands = p === "" ? home : inPrimary(rosterDomain(roster.projectDomains, p), primary);
    // A card its TEAM holds cannot follow: its team and its project must
    // live in one repository (G46), so a project of another one is a
    // refusal with a friendly label.
    if (teamBound && !sameRepository(lands, home, roster)) {
      continue;
    }
    // And the COLUMN answers for itself, never for its project: one
    // project NAME may be declared in two repositories with its columns
    // merged under one entry (G13), so a column of the OTHER one is not
    // where this project would put the card — the server compares exactly
    // these two and returns 422.
    const cols = scopes
      .filter((e) => e.project === p && sameRepository(e.domain, lands, roster))
      .map((e) => e.name);
    if (cols.length > 0) {
      out.push({ name: p, scopes: cols });
    }
  }
  return out;
}

/** attachSlotDates is the span a week-scheduled card takes when it is
 *  attached to a column: the slot of the week it was taken from — Monday to
 *  Friday, the whole of the week it was owed in. The same dates the server
 *  writes, so the optimistic card does not jump on the re-list. */
export function attachSlotDates(week: string): { startDate: string; day: string } {
  return { startDate: week, day: addDays(week, 4) };
}

/** Outcome of the Project board's × on a card — what the server will do, so
 *  the UI patches the same thing and asks only before a delete. */
export type PlacementRemoval = "orphan" | "delete";

/** removeFromProjectOutcome mirrors boardservice.RemoveFromProject: taking
 *  the card out of its column keeps only a card somebody MOVED — its
 *  progress is the one trace of work a project card can carry, since it has
 *  no owner (H4) — as an orphan a lead can hand on, and deletes the rest. */
export function removeFromProjectOutcome(
  card: Pick<Card, "project" | "scope" | "progress">,
  project: string,
  scope: string,
): PlacementRemoval | "refused" {
  // The server's two entry guards, mirrored: a column is named by its
  // SCOPE (an empty one is no column), and a card is only removed from a
  // column it stands in. Without them an empty pair "matched" a card
  // standing nowhere and fell through to the deletion branch.
  if (scope === "") {
    return "refused";
  }
  if ((card.project ?? "") !== (project ?? "") || (card.scope ?? "") !== scope) {
    return "refused";
  }
  return (card.progress ?? 0) > 0 ? "orphan" : "delete";
}

/** canBeFiledIntoAColumn reports whether the picker may offer a card a
 *  column at all. A card in a column is a PROJECT CARD and stands there alone
 *  (H4): one that carries a team, an owner, a parent or a review link is
 *  somebody's work in hand, and the server refuses to file it
 *  (ErrProjectCard) — so the picker offers it nothing, rather than a column
 *  that comes back as 422. Mirrors boardservice.projectCardCannotCarry. */
export function canBeFiledIntoAColumn(
  card: Pick<Card, "team" | "assignees" | "parent" | "lineage" | "type" | "reviewOf">,
): boolean {
  // A TASK parent makes it a subtask and bars the column; a planning parent
  // is the hierarchy's link and is fine. A planning card itself is never
  // filed into a column (hierarchy.md).
  return (
    !card.team &&
    (card.assignees?.length ?? 0) === 0 &&
    !isSubtask(card) &&
    !isPlanning(card) &&
    !card.reviewOf
  );
}

/** CardPlacements is everything the assign menu needs to attach one card —
 *  targets precomputed by the board, callbacks landing on the provider.
 *  Absent means the board offers no placement editing. */
export interface CardPlacements {
  /** For a card outside every project: where it may be attached. */
  attach?: ProjectTargets[];

  onAttachProject: (project: string, scope: string) => void;
  /** Put the card on its own team's backlog. Absent on work another board
   *  owns — a Project-board slot, whose week is not this board's to take
   *  away. */
  onPark?: () => void;
}

/** placementTargets computes the attach targets for a card from the board —
 *  the same filters the server applies, so the picker offers only what
 *  would be accepted. A card already in a column is offered nothing: it is
 *  re-filed on the Project board, not from this menu. Nor is a card that
 *  carries a team, an owner, a parent or a review link: a card in a column
 *  has none of those (H4), and the server refuses the filing. What is left
 *  — a card nothing holds — lives in the primary, and may be attached to a
 *  column of the primary. */
export function placementTargets(
  card: Pick<
    Card,
    | "project"
    | "scope"
    | "domain"
    | "stage"
    | "parent"
    | "team"
    | "assignees"
    | "reviewOf"
  >,
  board: {
    projects: string[];
    scopes: ScopeRef[];
    projectDomains?: RosterDomains;
    domains?: readonly DomainInfo[];
  },
): Pick<CardPlacements, "attach"> {
  if (card.scope || !canBeFiledIntoAColumn(card)) {
    return {};
  }
  return {
    attach: attachTargets(board.projects, board.scopes, card.domain ?? "", false, rosterOf(board)),
  };
}

/** PlacementDeps is what makeCardPlacements needs from a board component:
 *  its provider slice, its optimistic cache and its error/refresh plumbing. */
export interface PlacementDeps {
  provider: {
    patchCard(uid: string, patch: CardPatch): Promise<unknown>;
  };
  patchCard(itemId: string, patch: Partial<Card>): void;
  reload(): void;
  onError(message: string): void;
  errMessage(err: unknown): string;
}

/** makeCardPlacements builds the assign menu's placement section for one
 *  card: targets the server would accept, callbacks that mirror its
 *  outcomes optimistically and converge on the re-list. One factory for
 *  every board, so the Me and Team boards cannot drift apart — the ×'s
 *  history shows where per-board copies of one rule end up. */
export function makeCardPlacements(
  card: Card,
  board: {
    projects: string[];
    scopes: ScopeRef[];
    projectDomains?: RosterDomains;
    domains?: readonly DomainInfo[];
    teams?: string[];
    teamDomains?: RosterDomains;
  },
  deps: PlacementDeps,
): CardPlacements {
  const targets = placementTargets(card, board);
  const call = (p: Promise<unknown>) => {
    void p.then(() => deps.reload()).catch((err: unknown) => {
      deps.onError(deps.errMessage(err));
      deps.reload();
    });
  };
  return {
    ...targets,
    // Parking is not a reassignment: the shelf is the card's own team's, and
    // every team has one — so there is nothing to pick and nothing to check.
    onPark: parkable(card)
      ? () => {
          deps.patchCard(card.itemId, { parked: true, week: undefined, assignees: [] });
          call(deps.provider.patchCard(card.itemId, { parked: true }));
        }
      : undefined,
    onAttachProject: (project, scope) => {
      const patch: Partial<Card> = { project, scope };
      if (!card.startDate && card.week) {
        // The card takes the slot of the week it was taken from — the same
        // dates the server writes (attachSlotDates).
        Object.assign(patch, attachSlotDates(card.week));
      }
      deps.patchCard(card.itemId, patch);
      call(deps.provider.patchCard(card.itemId, { scope, project }));
    },
  };
}

/** SlotDrag is what dropping a dragged slot means: a drop in the column it
 *  was grabbed in is a date change, a drop on another column re-files the
 *  card there — and the date change itself is shared by both, since dates
 *  are the card's. */
export type SlotDrag = { kind: "dates" } | { kind: "refileHome" };

/** slotDragPlan: what the drop does, given the column the slot was grabbed
 *  in and the column it was dropped on. */
export function slotDragPlan(
  grabbed: { project: string; scope: string },
  target: { project: string; scope: string },
): SlotDrag {
  const same = grabbed.project === target.project && grabbed.scope === target.scope;
  return same ? { kind: "dates" } : { kind: "refileHome" };
}

/** movingSlot: is THIS rendered slot the one being dragged? Asked of the
 *  slot's column as well as its card, so a stale render of the card in a
 *  column it just left never dims. */
export function movingSlot(
  move: { card: { itemId: string }; grabbed: { project: string; scope: string } } | null,
  itemId: string,
  col: { project: string; scope: string },
): boolean {
  return (
    !!move &&
    move.card.itemId === itemId &&
    move.grabbed.project === col.project &&
    move.grabbed.scope === col.scope
  );
}

/** drawnOnProjectBoard reports whether the column grid draws this card:
 *  it must stand in one of the shown columns by its (project, scope) pair.
 *  A SUBTASK counts like any other card — it carries its column, and
 *  hiding it took whole groups off the planner (G57) — but one with no
 *  column of its own is on no Project board at all. `shown` holds the
 *  column keys, project and scope joined by a NUL. */
export function drawnOnProjectBoard(
  card: Pick<Card, "project" | "scope">,
  shown: ReadonlySet<string>,
): boolean {
  if (!card.scope) {
    return false;
  }
  return shown.has(`${card.project ?? ""}\u0000${card.scope}`);
}

/** teamFollowsParent reports whether a card's team is decided elsewhere: a
 *  subtask always carries its parent's team (S9), and the server rewrites
 *  any other choice — so the badge shows it and offers no list. It is NOT
 *  a reason to close the menu the badge opens: the same menu carries "Mark
 *  as done". */
/** drawnAsSlot reports whether the card HAS a slot on the Project grid: a
 *  column alone is not enough — a slot spans weeks, so it needs a start
 *  date, which is its row. The progress bars count what has a slot, whether
 *  or not the visible week window happens to be scrolled to it; what they
 *  must not count is a card with no slot at all, which nobody can see
 *  anywhere. (A stored week used to stand in for a missing start; nothing
 *  writes a week on a project card any more, H4.) */
export function drawnAsSlot(card: Pick<Card, "scope" | "startDate">): boolean {
  return !!card.scope && !!card.startDate;
}

/** hasOriginToShow reports whether the assign menu's origin block has any
 *  content — where the card came from. A COLUMN counts, project or not —
 *  it is where the card stands. */
export function hasOriginToShow(card: Pick<Card, "project" | "scope">): boolean {
  return !!card.project || !!card.scope;
}

/** projectsAColumnCanJoin narrows the picker that moves a column between
 *  projects. A column cannot change REPOSITORY — its stub is written back
 *  to the backend that holds it — so only the projects of its own
 *  repository can take it, and the server refuses the rest (G57). The
 *  no-project bucket is always reachable: unbinding moves nothing. */
export function projectsAColumnCanJoin(
  columnDomain: string,
  projects: readonly string[],
  projectDomains: RosterDomains,
  current = "",
  roster: RosterHomes = {},
): string[] {
  // What the column already carries stays on the list, so a pair written
  // before the rule can be seen and changed — every other picker here
  // keeps its current value for the same reason.
  return projects.filter(
    (p) =>
      p === current ||
      sameRepository(rosterDomain(projectDomains, p), columnDomain, roster),
  );
}

/** teamsACardCanTake narrows the team picker for a card standing in a
 *  COLUMN. A team decides where a card lives when its project does not, so
 *  a card in a no-project column may only take teams of that column's
 *  repository — every other entry is a refusal (G57). A card outside every
 *  column is constrained by its project instead (offerableTeams). */
export function teamsACardCanTake(
  columnDomain: string,
  teams: readonly string[],
  teamDomains: RosterDomains,
  current: string,
  roster: RosterHomes = {},
): string[] {
  return teams.filter(
    (t) =>
      t === current ||
      sameRepository(rosterDomain(teamDomains, t), columnDomain, roster),
  );
}

/** columnsOf lists the columns a card is drawn in: its (project, scope) pair,
 *  or none for a card outside every column. A column's own bar has to
 *  count exactly what that column shows. */
export function columnsOf(
  card: Pick<Card, "project" | "scope">,
): { project: string; scope: string }[] {
  if (!card.scope) {
    return [];
  }
  return [{ project: card.project ?? "", scope: card.scope }];
}

/** teamlessIsLawful reports whether a card standing in this column may be
 *  left with NO team. A card with neither team nor project is held by the
 *  primary repository, so a column of another one could not show it — and
 *  "No team" offered there is a refusal with a friendly label. A card with
 *  a PROJECT is placed by that project whatever its team says (G14), so
 *  dropping the team moves it nowhere and the entry is lawful: hiding it
 *  there took away a gesture the server accepts. */
export function teamlessIsLawful(
  columnDomain: string,
  roster: RosterHomes,
  cardProject = "",
): boolean {
  if (cardProject !== "") {
    return true;
  }
  // Nothing else places the card, so it belongs to the PRIMARY: the
  // column has to be there.
  return sameRepository(columnDomain, "", roster);
}

/** canCreateInColumn reports whether the board's "+" may open a card in
 *  this column. A card created there carries no team, so its repository is
 *  its PROJECT's — or the primary, when the column has no project. Either
 *  way the column has to be in that same repository: a project-less column
 *  of another one can hold no such card, and neither can a column of an
 *  ALIAS project (G13) that was declared in the other half of the merged
 *  entry. Offering the gesture there only produces a 422. */
export function canCreateInColumn(
  col: { project: string; domain?: string },
  roster: RosterHomes,
): boolean {
  const lands = col.project ? rosterDomain(roster.projectDomains, col.project) : "";
  return sameRepository(col.domain, lands, roster);
}

/** projectsWithColumns lists the projects a picker should walk: the
 *  roster's, plus the NO-PROJECT bucket when it actually holds columns.
 *  Walking board.projects alone never reached the bucket — so the column
 *  kind this line made a first-class home stayed unreachable from the one
 *  place a person could have used it. */
export function projectsWithColumns(
  projects: readonly string[],
  scopes: readonly ScopeRef[],
): string[] {
  const out = projects.slice();
  if (scopes.some((e) => !e.project) && !out.includes("")) {
    out.push("");
  }
  return out;
}

/** CardMark is what a card's left stripe says about where the work came FROM:
 *  a REVIEW waiting on this person — or nothing, for the day board's own
 *  work. The stripe is worth a glance precisely because it marks what
 *  arrived from elsewhere. (A PROJECT board's commitment used to be the
 *  other kind; the day boards and Triage draw no project card any more,
 *  H4.) */
export type CardMark = "review" | "";

/** markOf answers it once, for every board that draws the mark: the day
 *  boards down a card's left edge (Card.tsx: .card-review) and the Triage
 *  board on its own slot. Two boards deciding this separately is how one of
 *  them came to draw no mark at all after the weekly plan — whose band the
 *  stripe used to be — was taken out. */
export function markOf(c: Pick<Card, "reviewOf">): CardMark {
  return c.reviewOf ? "review" : "";
}
