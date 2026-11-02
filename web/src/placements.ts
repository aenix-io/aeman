// Placements: where a card stands on the Project board, and where it may be
// attached to. The server is the authority (boardservice/refile.go, the
// scope and project actions); everything here exists so the UI offers only
// what the server would accept, and mirrors its outcomes optimistically.

import { addDays } from "./date";
import { parkable } from "./backlog";
import { inPrimary, primaryDomain, rosterDomain, type DomainInfo, type RosterDomains } from "./domains";
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
 *  the card out of its column keeps only a WORKED card (someone had it and
 *  moved it) as an orphan of the working area, and deletes the rest. */
export function removeFromProjectOutcome(
  card: Pick<Card, "project" | "scope" | "assignees" | "progress" | "parent">,
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
  // A subtask's home is its parent: the column goes, the card stays.
  const worked = (card.assignees?.length ?? 0) > 0 && (card.progress ?? 0) > 0;
  return worked || card.parent ? "orphan" : "delete";
}

/** countedForProgress decides whether a card counts towards one progress
 *  figure. The Project board draws a subtask that carries its own column,
 *  so it must count — unless its PARENT is counted in the SAME figure,
 *  whose progress already derives from its children: counting both would
 *  weigh that work twice.
 *
 *  `where` is the figure being computed: a project's line ({project}), or
 *  one column ({project, scope}). The parent answers for that figure when
 *  it is DRAWN in it (columnsOf), so the question is asked per figure and
 *  not once per card.
 *
 *  A parent nothing draws (a column with no dates is no slot) answers
 *  nowhere — deferring to it dropped the child's work from every bar while
 *  its slot sat in the column. */
export function countedForProgress(
  card: Pick<Card, "itemId" | "parent" | "project" | "scope">,
  byId: ReadonlyMap<
    string,
    Pick<Card, "itemId" | "project" | "scope" | "startDate" | "week">
  >,
  where: { project: string; scope?: string },
): boolean {
  if (!card.parent) {
    return true;
  }
  const parent = byId.get(card.parent);
  if (!parent || !drawnAsSlot(parent)) {
    return true;
  }
  return !columnsOf(parent).some(
    (c) => c.project === where.project && (where.scope === undefined || c.scope === where.scope),
  );
}

/** countedAmong is the de-duplication for a figure that spans several
 *  columns — a board's header total, a project's line: a subtask counts
 *  unless its PARENT is drawn in the SAME figure, whose bar already derives
 *  from it. Asked by identity, because such a figure has no single
 *  (project, scope) pair to compare against; countedForProgress asks the
 *  same question of one column or one project. */
export function countedAmong(
  card: Pick<Card, "parent">,
  drawn: ReadonlySet<string>,
): boolean {
  return !card.parent || !drawn.has(card.parent);
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
 *  re-filed on the Project board, not from this menu. */
export function placementTargets(
  card: Pick<
    Card,
    | "project"
    | "scope"
    | "domain"
    | "stage"
    | "parent"
    | "team"
    | "reviewOf"
  >,
  board: {
    projects: string[];
    scopes: ScopeRef[];
    projectDomains?: RosterDomains;
    domains?: readonly DomainInfo[];
  },
): Pick<CardPlacements, "attach"> {
  if (card.scope) {
    return {};
  }
  // A subtask may carry a column of its own (G14), and the Project board's
  // × takes that column away, so without the offer the × would be a
  // one-way door. The rule lives here rather than in each board, or the
  // next board to render cards has to remember it a third time.
  if (card.parent) {
    return {
      // A LINK holds the card where the linked card is, ahead of its
      // project (G14) — so a subtask cannot follow a new project into
      // another repository any more than a teamed card can, and the
      // columns there are refusals with a friendly label.
      attach: attachTargets(board.projects, board.scopes, card.domain ?? "", true,
        rosterOf(board)),
    };
  }
  return {
    attach: attachTargets(board.projects, board.scopes, card.domain ?? "",
      !!card.team || !!card.reviewOf, rosterOf(board)),
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
export function teamFollowsParent(card: Pick<Card, "parent">): boolean {
  return !!card.parent;
}

/** drawnAsSlot reports whether the card HAS a slot on the Project grid: a
 *  column alone is not enough — a slot spans weeks, so it needs dates. The
 *  progress bars count what has a slot, whether or not the visible week
 *  window happens to be scrolled to it; what they must not count is a card
 *  with no slot at all, which nobody can see anywhere. */
export function drawnAsSlot(card: Pick<Card, "scope" | "startDate" | "week">): boolean {
  return !!card.scope && (!!card.startDate || !!card.week);
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

/** columnFollows mirrors boardservice.columnFollows: whether the column a
 *  SUBTASK carries can come with it out of the group. Once the parent link
 *  is gone the card's file goes where its own project — or, failing that,
 *  its team — names, and a column of any OTHER repository has to be left
 *  behind; the card is then an ordinary columnless one, which is what the ×
 *  answers for. An UNDECLARED column is left alone, exactly as the server
 *  leaves it.
 *
 *  `linked` is the repository of the card this one still points at when the
 *  parent is gone — the original it reviews — since a link outranks both
 *  project and team (G14). Omitted for the ordinary subtask, which points
 *  at nothing else. */
export function columnFollows(
  card: Pick<Card, "project" | "scope" | "team">,
  roster: RosterHomes & {
    scopes: readonly ScopeRef[];
    teamDomains?: RosterDomains;
  },
  linked?: string,
): boolean {
  if (!card.scope) {
    return true;
  }
  const col = roster.scopes.find(
    (e) => e.name === card.scope && (e.project ?? "") === (card.project ?? ""),
  );
  if (!col) {
    return true;
  }
  const home =
    linked !== undefined
      ? linked
      : card.project
        ? rosterDomain(roster.projectDomains, card.project)
        : rosterDomain(roster.teamDomains, card.team ?? "");
  return sameRepository(col.domain, home, roster);
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
 *  a REVIEW waiting on this person, a PROJECT board's commitment — or
 *  nothing, for the day board's own work. The stripe is worth a glance
 *  precisely because it marks what arrived from elsewhere. */
export type CardMark = "review" | "project" | "";

/** markOf answers it once, for every board that draws the mark: the day
 *  boards down a card's left edge (Card.tsx: .card-review / -project) and
 *  the Triage board on its own slot (.triage-slot-project). Two boards
 *  deciding this separately is how one of them came to draw no mark at all
 *  after the weekly plan — whose band the stripe used to be — was taken out.
 *
 *  A review card of a project card is BOTH; the waiting is the innermost
 *  thing about it, so that is what the stripe says. Triage never meets a
 *  review card (a review follows the card it reviews and is not scheduled),
 *  so the two boards agree wherever they both draw. */
/** datesAreAnotherBoards reports that a card's dates are not this board's to
 *  change.
 *
 *  A Project-board SLOT's dates ARE its row there: the week it stands in is
 *  the Monday of its start, and its span is how many weeks the work takes.
 *  Editing them from a day board re-dates another board's commitment in
 *  passing — the same thing the Triage board's catch holds still (triage.gripOf
 *  answers "pinned"), said about the dates instead of about the drag.
 *
 *  It is a rule of the DAY BOARDS, not of the server. The Project board sets
 *  those dates itself and must go on doing so, and the server cannot tell
 *  which board is asking: a narrowing meant for one board, put in the service,
 *  refused every board that sends the same call. */
export function datesAreAnotherBoards(c: Pick<Card, "scope">): boolean {
  return !!c.scope;
}

export function markOf(c: Pick<Card, "reviewOf" | "scope">): CardMark {
  if (c.reviewOf) {
    return "review";
  }
  return c.scope ? "project" : "";
}
