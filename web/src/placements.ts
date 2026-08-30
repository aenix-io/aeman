// Placements: where a card stands on the Project board, and where it may be
// attached or mirrored to. A mirror is the same card standing in a second
// column — one file, one log, one set of dates — so shared work is one card
// on one person, not a duplicate per project drifting apart. The server is
// the authority (boardservice/mirrors.go); everything here exists so the UI
// offers only what the server would accept, and mirrors its outcomes
// optimistically.

import { addDays } from "./date";
import { inPrimary, rosterDomain, type RosterDomains } from "./domains";
import type { Card, CardPatch, EpicRef } from "./providers/types";

/** ProjectTargets is one project the picker offers, with its columns. */
export interface ProjectTargets {
  name: string;
  epics: string[];
}

/** attachTargets is what a card outside every project may be attached to:
 *  the columns of ITS OWN repository (the server refuses a cross-repo
 *  pair), each with its epic columns; a project with no columns is not
 *  offered — a column is where a card lands. */
export function attachTargets(
  projects: readonly string[],
  epics: readonly EpicRef[],
  cardDomain: string,
  teamBound = true,
): ProjectTargets[] {
  const out: ProjectTargets[] = [];
  for (const p of projectsWithColumns(projects, epics)) {
    // The COLUMN answers, not its project: one project NAME may be
    // declared in two repositories with its columns merged under one entry
    // (G13), and the server asks the column — filtering by the project
    // offered columns it refuses, and hid columns it would have taken.
    // Where the card will BE after the attach, not where it is: for a card
    // whose PROJECT decides (no team to hold it), the new project carries
    // it along, so a column of another repository is a lawful destination
    // — the server accepts it, and refusing to offer it hid the move the
    // whole no-project bucket exists for. A card whose team holds it stays
    // in that team's repository (G46).
    const cols = epics
      .filter((e) => e.project === p && (!teamBound || (e.domain ?? "") === cardDomain))
      .map((e) => e.name);
    if (cols.length > 0) {
      out.push({ name: p, epics: cols });
    }
  }
  return out;
}

/** mirrorTargets is where a card already in a column may be mirrored to:
 *  the projects of its HOME project's repository, minus every column it
 *  already stands in. */
export function mirrorTargets(
  card: Pick<Card, "project" | "epic" | "mirrors" | "domain">,
  projects: readonly string[],
  epics: readonly EpicRef[],
): ProjectTargets[] {
  const home = card.domain ?? "";
  const standing = new Set<string>([`${card.project}\u0000${card.epic}`]);
  for (const m of card.mirrors ?? []) {
    standing.add(`${m.project}\u0000${m.epic}`);
  }
  const out: ProjectTargets[] = [];
  for (const p of projectsWithColumns(projects, epics)) {
    // The column's own repository decides, as it does for the server.
    const cols = epics
      .filter(
        (e) =>
          e.project === p &&
          (e.domain ?? "") === home &&
          !standing.has(`${p}\u0000${e.name}`),
      )
      .map((e) => e.name);
    if (cols.length > 0) {
      out.push({ name: p, epics: cols });
    }
  }
  return out;
}

/** attachSlotDates is the span a weekly-plan card takes when it is attached
 *  to a column: the slot of the week it was taken from — start on its
 *  Monday, end on its band's day. The same dates the server writes, so the
 *  optimistic card does not jump on the re-list. */
export function attachSlotDates(
  band: "wed" | "fri",
  week: string,
): { startDate: string; day: string } {
  return { startDate: week, day: addDays(week, band === "wed" ? 2 : 4) };
}

/** Outcome of the Project board's × on one placement — what the server will
 *  do, so the UI patches the same thing and asks only before a delete. */
export type PlacementRemoval = "unmirror" | "promote" | "orphan" | "delete";

/** removeFromProjectOutcome mirrors boardservice.RemoveFromProject: a
 *  mirror goes; the home with mirrors left hands over; the last column
 *  keeps only a WORKED card (someone had it and moved it) as an orphan of
 *  the working area, and deletes the rest. */
export function removeFromProjectOutcome(
  card: Pick<Card, "project" | "epic" | "mirrors" | "assignees" | "progress" | "parent">,
  project: string,
  epic: string,
): PlacementRemoval | "refused" {
  // The server's two entry guards, mirrored: a column is named by its
  // EPIC (an empty one is no column), and a card is only removed from a
  // column it stands in. Without them an empty pair "matched" a card
  // standing nowhere and fell through to the last-column branch, which
  // deletes.
  if (epic === "") {
    return "refused";
  }
  const mirrored = (card.mirrors ?? []).some(
    (m) => m.project === project && m.epic === epic,
  );
  if (mirrored) {
    return "unmirror";
  }
  if ((card.project ?? "") !== (project ?? "") || (card.epic ?? "") !== epic) {
    return "refused";
  }
  if ((card.mirrors ?? []).length > 0) {
    return "promote";
  }
  // A subtask's home is its parent: the column goes, the card stays.
  const worked = (card.assignees?.length ?? 0) > 0 && (card.progress ?? 0) > 0;
  return worked || card.parent ? "orphan" : "delete";
}

/** countedForProgress decides whether a card counts towards a progress
 *  figure. The Project board draws a subtask that carries its own column,
 *  so it must count — unless its PARENT is counted in the SAME figure,
 *  whose progress already derives from its children: counting both would
 *  weigh that work twice.
 *
 *  Which figure matters. A column's bar is per column, so a parent in
 *  another column answers for nothing here and the child is that column's
 *  own work; a project's line spans them, so there the parent does answer.
 *  And a parent nothing draws (a column with no dates is no slot) answers
 *  nowhere — deferring to it dropped the child's work from every bar while
 *  its slot sat in the column. */
export function countedForProgress(
  card: Pick<Card, "itemId" | "parent" | "project" | "epic">,
  byId: ReadonlyMap<string, Pick<Card, "itemId" | "project" | "epic" | "startDate" | "week">>,
  scope: "project" | "column",
): boolean {
  if (!card.parent) {
    return true;
  }
  const parent = byId.get(card.parent);
  if (!parent || !drawnAsSlot(parent)) {
    return true;
  }
  if ((parent.project ?? "") !== (card.project ?? "")) {
    return true;
  }
  return scope === "column" && parent.epic !== card.epic;
}

/** CardPlacements is everything the assign menu needs to attach or mirror
 *  one card — targets precomputed by the board, callbacks landing on the
 *  provider. Absent means the board offers no placement editing. */
export interface CardPlacements {
  /** For a card outside every project: where it may be attached. */
  attach?: ProjectTargets[];
  /** For a recurrent card: the processes it may be tied to. */
  processes?: string[];
  /** For a card in a column: where it may be mirrored to. */
  mirror?: ProjectTargets[];
  onAttachProject: (project: string, epic: string) => void;
  onAttachProcess: (process: string) => void;
  onMirror: (project: string, epic: string) => void;
  onUnmirror: (project: string, epic: string) => void;
}

/** placementTargets computes the attach/mirror/process targets for a card
 *  from the board — the same filters the server applies, so the picker
 *  offers only what would be accepted. */
export function placementTargets(
  card: Pick<
    Card,
    "project" | "epic" | "mirrors" | "domain" | "stage" | "process" | "task" | "parent" | "team"
  >,
  board: {
    projects: string[];
    epics: EpicRef[];
    processes: { name: string }[];
    processDomains?: RosterDomains;
  },
): Pick<CardPlacements, "attach" | "processes" | "mirror"> {
  // A subtask carries at most ONE column of its own (G14): the server
  // refuses it a second placement (ErrSubtaskMirror) and a process tie
  // (ErrSubtaskTie), because its file rides its parent and both would be
  // stranded the moment the parent changes repository. Attaching is its
  // right, though — and the Project board's × takes that column away, so
  // without the offer the × would be a one-way door. The rule lives here
  // rather than in each board, or the next board to render cards has to
  // remember it a third time.
  if (card.parent) {
    return card.epic
      ? {}
      : {
          attach: attachTargets(board.projects, board.epics, card.domain ?? "", !!card.team),
        };
  }
  if (card.epic) {
    return {
      mirror: mirrorTargets(card, board.projects, board.epics),
    };
  }
  if (card.stage === "recurrent") {
    if (card.task) {
      // A process TURN belongs to its task, and the task names the
      // process — the server refuses a re-tie, so the menu offers none.
      return {};
    }
    // The process it is already tied to is where it stands, not a target —
    // and only processes of the card's own repository: the server refuses
    // a cross-repository tie, so the menu must not offer one.
    return {
      processes: board.processes
        .map((p) => p.name)
        .filter(
          (name) =>
            name !== card.process &&
            rosterDomain(board.processDomains, name) === (card.domain ?? ""),
        ),
    };
  }
  return {
    attach: attachTargets(board.projects, board.epics, card.domain ?? "", !!card.team),
  };
}

/** PlacementDeps is what makeCardPlacements needs from a board component:
 *  its provider slice, its optimistic cache and its error/refresh plumbing. */
export interface PlacementDeps {
  provider: {
    patchCard(uid: string, patch: CardPatch): Promise<unknown>;
    mirrorCard(uid: string, project: string, epic: string): Promise<void>;
    unmirrorCard(uid: string, project: string, epic: string): Promise<void>;
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
    epics: EpicRef[];
    processes: { name: string }[];
    processDomains?: RosterDomains;
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
    onAttachProject: (project, epic) => {
      const patch: Partial<Card> = { project, epic };
      if (!card.startDate && card.plan && card.week) {
        // The card takes the slot of the week it was taken from — the same
        // dates the server writes (attachSlotDates).
        Object.assign(patch, attachSlotDates(card.plan, card.week));
      }
      deps.patchCard(card.itemId, patch);
      call(deps.provider.patchCard(card.itemId, { epic, project }));
    },
    onAttachProcess: (process) => {
      deps.patchCard(card.itemId, { process });
      call(deps.provider.patchCard(card.itemId, { process }));
    },
    onMirror: (project, epic) => {
      deps.patchCard(card.itemId, {
        mirrors: [...(card.mirrors ?? []), { project, epic }],
      });
      call(deps.provider.mirrorCard(card.itemId, project, epic));
    },
    onUnmirror: (project, epic) => {
      deps.patchCard(card.itemId, {
        mirrors: (card.mirrors ?? []).filter(
          (m) => !(m.project === project && m.epic === epic),
        ),
      });
      call(deps.provider.unmirrorCard(card.itemId, project, epic));
    },
  };
}

/** SlotDrag is what dropping a dragged slot means. The grid renders a
 *  mirrored card once per column, and the drag must act on the PLACEMENT it
 *  grabbed: nudging a mirror copy down a week is a date change, not an
 *  order to re-file the card's home into the mirror column — which is what
 *  a home-blind drop did, silently collapsing two placements into one. */
export type SlotDrag =
  | { kind: "dates" }
  | { kind: "refileHome" }
  | { kind: "moveMirror" }
  | { kind: "collapseMirror" };

/** slotDragPlan: what the drop does, given the column the slot was grabbed
 *  in and the column it was dropped on. The date change itself is shared by
 *  every kind — dates are the card's, not a placement's. */
export function slotDragPlan(
  card: Pick<Card, "project" | "epic">,
  grabbed: { project: string; epic: string },
  target: { project: string; epic: string },
): SlotDrag {
  const same = grabbed.project === target.project && grabbed.epic === target.epic;
  if (same) {
    return { kind: "dates" };
  }
  const grabbedHome =
    grabbed.project === (card.project ?? "") && grabbed.epic === (card.epic ?? "");
  if (grabbedHome) {
    return { kind: "refileHome" };
  }
  const targetHome =
    target.project === (card.project ?? "") && target.epic === (card.epic ?? "");
  return targetHome ? { kind: "collapseMirror" } : { kind: "moveMirror" };
}

/** slotDropMirrors is the card's mirror list after a drag lands — the same
 *  list the server converges on, so the grid never draws one slot twice in
 *  a column while the round trip runs: moving a mirror onto a column the
 *  card already mirrors folds them together (the server no-ops the add and
 *  removes the grabbed one); re-filing the home onto a mirror drops the
 *  now-duplicate mirror (the server's invariant does the same). */
export function slotDropMirrors(
  card: Pick<Card, "mirrors">,
  grabbed: { project: string; epic: string },
  target: { project: string; epic: string },
  kind: SlotDrag["kind"],
): { project: string; epic: string }[] {
  const mirrors = card.mirrors ?? [];
  const without = (list: typeof mirrors, p: { project: string; epic: string }) =>
    list.filter((m) => !(m.project === p.project && m.epic === p.epic));
  switch (kind) {
    case "dates":
      return mirrors;
    case "refileHome":
      // The home lands where a mirror may already stand: that mirror is a
      // duplicate now, and the server drops it.
      return without(mirrors, target);
    case "collapseMirror":
      return without(mirrors, grabbed);
    case "moveMirror": {
      const kept = without(mirrors, grabbed);
      // A drop onto a column the card already mirrors FOLDS the grabbed
      // copy away — the standing entry keeps its position, because the
      // server keeps it too, and the first mirror is the promotion heir.
      if (kept.some((m) => m.project === target.project && m.epic === target.epic)) {
        return kept;
      }
      return kept.concat([target]);
    }
  }
}

/** MirrorDropProvider is the provider slice a mirror drop drives. */
export interface MirrorDropProvider {
  mirrorCard(uid: string, project: string, epic: string): Promise<void>;
  unmirrorCard(uid: string, project: string, epic: string): Promise<void>;
  patchCard(uid: string, patch: CardPatch): Promise<unknown>;
}

/** settleMirrorDrop runs the multi-request drop of a dragged mirror copy —
 *  add the new placement first (a failure half-way must never leave the
 *  card short one), then remove the grabbed one, then the shared dates —
 *  and settles the UI honestly either way. A failure in the MIDDLE leaves
 *  the server in a state the pre-drag snapshot does not describe, so the
 *  error path must re-list rather than restore-and-believe: a rollback
 *  without a reload showed a board the server did not hold, and overwrote
 *  the watch frame the successful first step had already delivered. */
export async function settleMirrorDrop(
  provider: MirrorDropProvider,
  itemId: string,
  grabbed: { project: string; epic: string },
  target: { project: string; epic: string },
  kind: "moveMirror" | "collapseMirror",
  dates: { start: string; end: string } | null,
  ui: {
    restore(): void;
    reload(): void;
    onError(message: string): void;
    errMessage(err: unknown): string;
  },
): Promise<void> {
  try {
    if (kind === "moveMirror") {
      await provider.mirrorCard(itemId, target.project, target.epic);
    }
    await provider.unmirrorCard(itemId, grabbed.project, grabbed.epic);
    if (dates) {
      await provider.patchCard(itemId, { dates });
    }
    ui.reload();
  } catch (err: unknown) {
    ui.restore();
    ui.onError(ui.errMessage(err));
    ui.reload();
  }
}

/** movingSlot: is THIS rendered placement the one being dragged? A mirrored
 *  card renders once per column, so dimming by card id alone dimmed the
 *  home copy while a mirror copy moved — only the grabbed placement dims. */
export function movingSlot(
  move: { card: { itemId: string }; grabbed: { project: string; epic: string } } | null,
  itemId: string,
  col: { project: string; epic: string },
): boolean {
  return (
    !!move &&
    move.card.itemId === itemId &&
    move.grabbed.project === col.project &&
    move.grabbed.epic === col.epic
  );
}

/** drawnOnProjectBoard reports whether the column grid draws this card:
 *  it must stand in one of the shown columns, by its own (project, epic)
 *  pair or by a mirror. A SUBTASK counts like any other card — it carries
 *  its column, and hiding it took whole groups off the planner (G57) — but
 *  one with no column of its own is on no Project board at all. `shown`
 *  holds the column keys, project and epic joined by a NUL. */
export function drawnOnProjectBoard(
  card: Pick<Card, "project" | "epic" | "mirrors">,
  shown: ReadonlySet<string>,
): boolean {
  if (!card.epic) {
    return false;
  }
  const key = (project: string, epic: string) => `${project}\u0000${epic}`;
  return (
    shown.has(key(card.project ?? "", card.epic)) ||
    (card.mirrors ?? []).some((m) => shown.has(key(m.project, m.epic)))
  );
}

/** teamFollowsParent reports whether a card's team is decided elsewhere: a
 *  subtask always carries its parent's team (S3), and the server rewrites
 *  any other choice — so the badge shows it and offers no list. It is NOT
 *  a reason to close the menu the badge opens: the same menu carries "Mark
 *  as done" and the mirror section. */
export function teamFollowsParent(card: Pick<Card, "parent">): boolean {
  return !!card.parent;
}

/** drawnAsSlot reports whether the card HAS a slot on the Project grid: a
 *  column alone is not enough — a slot spans weeks, so it needs dates. The
 *  progress bars count what has a slot, whether or not the visible week
 *  window happens to be scrolled to it; what they must not count is a card
 *  with no slot at all, which nobody can see anywhere. */
export function drawnAsSlot(card: Pick<Card, "epic" | "startDate" | "week">): boolean {
  return !!card.epic && (!!card.startDate || !!card.week);
}

/** hasOriginToShow reports whether the assign menu's origin block has any
 *  content — where the card came from. A different question from
 *  what the menu can OFFER ("is there anywhere to send it"): a card in a
 *  no-project column can be mirrored, so the menu offers something, while
 *  the block itself would draw nothing but its own divider. A COLUMN
 *  counts, project or not — it is where the card stands. */
export function hasOriginToShow(
  card: Pick<Card, "process" | "project" | "epic" | "mirrors">,
): boolean {
  return (
    !!card.process || !!card.project || !!card.epic || (card.mirrors?.length ?? 0) > 0
  );
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
): string[] {
  // What the column already carries stays on the list, so a pair written
  // before the rule can be seen and changed — every other picker here
  // keeps its current value for the same reason.
  return projects.filter(
    (p) => p === current || rosterDomain(projectDomains, p) === columnDomain,
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
): string[] {
  return teams.filter(
    (t) => t === current || rosterDomain(teamDomains, t) === columnDomain,
  );
}

/** columnsOf lists the columns a card is drawn in — its home pair and
 *  every mirror. A column's own bar has to count what that column shows,
 *  and keying the count by the home pair alone left a mirror column
 *  reporting a total of zero while drawing slots, so the header
 *  percentage and the sum of the columns' contradicted each other. */
export function columnsOf(
  card: Pick<Card, "project" | "epic" | "mirrors">,
): { project: string; epic: string }[] {
  if (!card.epic) {
    return [];
  }
  return [{ project: card.project ?? "", epic: card.epic }].concat(
    (card.mirrors ?? []).map((m) => ({ project: m.project, epic: m.epic })),
  );
}

/** teamlessIsLawful reports whether a card standing in this column may be
 *  left with NO team. A card with neither team nor project is held by the
 *  primary repository, so a column of another one could not show it — and
 *  "No team" offered there is a refusal with a friendly label. */
export function teamlessIsLawful(columnDomain: string, primary: string): boolean {
  return inPrimary(columnDomain, primary) === inPrimary("", primary);
}

/** canCreateInColumn reports whether the board's "+" may open a card in
 *  this column. A card created there carries no team, so its repository is
 *  its PROJECT's — or the primary, when the column has no project. A
 *  project-less column of another repository can hold no such card, and
 *  offering the gesture there only produces a 422. */
export function canCreateInColumn(
  col: { project: string; domain?: string },
  primary: string,
): boolean {
  if (col.project) {
    return true; // the project decides, and it decides for itself
  }
  return inPrimary(col.domain, primary) === inPrimary("", primary);
}

/** projectsWithColumns lists the projects a picker should walk: the
 *  roster's, plus the NO-PROJECT bucket when it actually holds columns.
 *  Walking board.projects alone never reached the bucket — so the column
 *  kind this line made a first-class home stayed unreachable from the one
 *  place a person could have used it. */
export function projectsWithColumns(
  projects: readonly string[],
  epics: readonly EpicRef[],
): string[] {
  const out = projects.slice();
  if (epics.some((e) => !e.project) && !out.includes("")) {
    out.push("");
  }
  return out;
}
