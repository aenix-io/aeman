// Placements: where a card stands on the Project board, and where it may be
// attached or mirrored to. A mirror is the same card standing in a second
// column — one file, one log, one set of dates — so shared work is one card
// on one person, not a duplicate per project drifting apart. The server is
// the authority (boardservice/mirrors.go); everything here exists so the UI
// offers only what the server would accept, and mirrors its outcomes
// optimistically.

import { addDays } from "./date";
import { rosterDomain, type RosterDomains } from "./domains";
import type { Card, CardPatch, EpicRef } from "./providers/types";

/** ProjectTargets is one project the picker offers, with its columns. */
export interface ProjectTargets {
  name: string;
  epics: string[];
}

/** attachTargets is what a card outside every project may be attached to:
 *  the projects of ITS OWN repository (the server refuses a cross-repo
 *  pair), each with its epic columns; a project with no columns is not
 *  offered — a column is where a card lands. */
export function attachTargets(
  projects: readonly string[],
  epics: readonly EpicRef[],
  projectDomains: RosterDomains,
  cardDomain: string,
): ProjectTargets[] {
  const out: ProjectTargets[] = [];
  for (const p of projects) {
    if (rosterDomain(projectDomains, p) !== cardDomain) {
      continue;
    }
    const cols = epics.filter((e) => e.project === p).map((e) => e.name);
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
  card: Pick<Card, "project" | "epic" | "mirrors">,
  projects: readonly string[],
  epics: readonly EpicRef[],
  projectDomains: RosterDomains,
): ProjectTargets[] {
  const home = rosterDomain(projectDomains, card.project ?? "");
  const standing = new Set<string>([`${card.project}\u0000${card.epic}`]);
  for (const m of card.mirrors ?? []) {
    standing.add(`${m.project}\u0000${m.epic}`);
  }
  const out: ProjectTargets[] = [];
  for (const p of projects) {
    if (rosterDomain(projectDomains, p) !== home) {
      continue;
    }
    const cols = epics
      .filter((e) => e.project === p && !standing.has(`${p}\u0000${e.name}`))
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
): PlacementRemoval {
  const mirrored = (card.mirrors ?? []).some(
    (m) => m.project === project && m.epic === epic,
  );
  if (mirrored) {
    return "unmirror";
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
  card: Pick<Card, "project" | "epic" | "mirrors" | "domain" | "stage" | "process" | "task" | "parent">,
  board: {
    projects: string[];
    epics: EpicRef[];
    projectDomains?: RosterDomains;
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
          attach: attachTargets(
            board.projects,
            board.epics,
            board.projectDomains,
            card.domain ?? "",
          ),
        };
  }
  if (card.epic) {
    if (!card.project) {
      // A no-project column names no repository, so the server refuses
      // every mirror target — the menu offers nothing rather than 422s.
      return {};
    }
    return {
      mirror: mirrorTargets(card, board.projects, board.epics, board.projectDomains),
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
    attach: attachTargets(
      board.projects,
      board.epics,
      board.projectDomains,
      card.domain ?? "",
    ),
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
    projectDomains?: RosterDomains;
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
 *  its column, and hiding it took whole groups off the planner (S4) — but
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

/** hasPlacementOffer reports whether a placement menu would draw anything.
 *  An empty section is still an empty section — `[]` is truthy, and a card
 *  in a repository with no columns to offer drew a stray divider. */
export function hasPlacementOffer(
  p: Pick<CardPlacements, "attach" | "processes" | "mirror"> | undefined,
): boolean {
  return (
    (p?.attach?.length ?? 0) > 0 ||
    (p?.processes?.length ?? 0) > 0 ||
    (p?.mirror?.length ?? 0) > 0
  );
}
