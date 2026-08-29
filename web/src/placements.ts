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
  card: Pick<Card, "project" | "epic" | "mirrors" | "assignees" | "progress">,
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
  const worked = (card.assignees?.length ?? 0) > 0 && (card.progress ?? 0) > 0;
  return worked ? "orphan" : "delete";
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
  card: Pick<Card, "project" | "epic" | "mirrors" | "domain" | "stage" | "process">,
  board: {
    projects: string[];
    epics: EpicRef[];
    projectDomains?: RosterDomains;
    processes: { name: string }[];
  },
): Pick<CardPlacements, "attach" | "processes" | "mirror"> {
  if (card.epic) {
    return {
      mirror: mirrorTargets(card, board.projects, board.epics, board.projectDomains),
    };
  }
  if (card.stage === "recurrent") {
    // The process it is already tied to is where it stands, not a target.
    return {
      processes: board.processes
        .map((p) => p.name)
        .filter((name) => name !== card.process),
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
