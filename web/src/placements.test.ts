import { describe, expect, it } from "vitest";

import {
  attachSlotDates,
  attachTargets,
  countedForProgress,
  drawnAsSlot,
  drawnOnProjectBoard,
  hasOriginToShow,
  hasPlacementOffer,
  makeCardPlacements,
  mirrorTargets,
  movingSlot,
  placementTargets,
  removeFromProjectOutcome,
  settleMirrorDrop,
  slotDragPlan,
  slotDropMirrors,
  teamFollowsParent,
  type CardPlacements,
} from "./placements";
import type { Card } from "./providers/types";

// The picker offers only columns the server would accept: projects of the
// card's own repository, each with its epics — a pair the server refuses
// has no business being clickable.
describe("attach and mirror targets", () => {
  const projects = ["engineering", "freedom", "strategy"];
  // The repository rides on the COLUMN, the way the server records it: a
  // column declared in the closed repository carries it, whatever its
  // project is called.
  const epics = [
    { name: "Cozystack", project: "engineering" },
    { name: "Ingress", project: "engineering" },
    { name: "Launch", project: "freedom" },
    { name: "Fundraising", project: "strategy", domain: "founders" },
  ];

  it("offers the projects of the card's repository, with their epics", () => {
    const got = attachTargets(projects, epics, "");
    expect(got).toEqual([
      { name: "engineering", epics: ["Cozystack", "Ingress"] },
      { name: "freedom", epics: ["Launch"] },
    ]);
    // A card living in the founders repository sees only founders projects.
    expect(attachTargets(projects, epics, "founders")).toEqual([
      { name: "strategy", epics: ["Fundraising"] },
    ]);
  });

  it("offers everything on a board that names no domains", () => {
    const single = epics.map((e) => ({ name: e.name, project: e.project }));
    expect(attachTargets(projects, single, "").map((p) => p.name)).toEqual(projects);
  });

  it("mirror targets follow the HOME project's repository and skip where the card stands", () => {
    const card = {
      project: "engineering",
      epic: "Cozystack",
      mirrors: [{ project: "freedom", epic: "Launch" }],
    } as Card;
    const got = mirrorTargets(card, projects, epics);
    // Cozystack is the home, Launch is already mirrored: only Ingress is left.
    expect(got).toEqual([{ name: "engineering", epics: ["Ingress"] }]);
  });
});

// Attaching a weekly-plan card gives it the slot of the week it was taken
// from: start on its Monday, end on its band's day — the same dates the
// server writes, so the optimistic card does not jump on the re-list.
describe("attachSlotDates", () => {
  it("spans to Friday for a by-Friday card and Wednesday for a by-Wednesday one", () => {
    expect(attachSlotDates("fri", "2026-08-24")).toEqual({
      startDate: "2026-08-24",
      day: "2026-08-28",
    });
    expect(attachSlotDates("wed", "2026-08-24")).toEqual({
      startDate: "2026-08-24",
      day: "2026-08-26",
    });
  });
});

// The Project board's × means four different things, and the UI must know
// which before it asks anything: a mirror goes silently, the home hands
// over silently, an orphan survives, and only the delete is worth a
// question.
describe("removeFromProjectOutcome", () => {
  const base = { project: "engineering", epic: "Cozystack" } as Card;

  it("is unmirror on a mirror placement", () => {
    const c = { ...base, mirrors: [{ project: "freedom", epic: "Launch" }] } as Card;
    expect(removeFromProjectOutcome(c, "freedom", "Launch")).toBe("unmirror");
  });

  it("is promote on the home while mirrors remain", () => {
    const c = { ...base, mirrors: [{ project: "freedom", epic: "Launch" }] } as Card;
    expect(removeFromProjectOutcome(c, "engineering", "Cozystack")).toBe("promote");
  });

  it("is orphan on the last column of a worked card, delete otherwise", () => {
    const worked = { ...base, assignees: ["kvaps"], progress: 40 } as Card;
    expect(removeFromProjectOutcome(worked, "engineering", "Cozystack")).toBe("orphan");
    const idle = { ...base, assignees: [] } as Card;
    expect(removeFromProjectOutcome(idle, "engineering", "Cozystack")).toBe("delete");
    // Progress without a person, or a person without progress, is not
    // "worked" — the server deletes it, and the UI must ask first.
    expect(
      removeFromProjectOutcome({ ...base, assignees: [], progress: 40 } as Card, "engineering", "Cozystack"),
    ).toBe("delete");
  });
});

// A subtask's home is its parent, so the Project board's × may take it out
// of a column but never delete it — the server refuses to as well.
describe("removeFromProjectOutcome for a subtask", () => {
  it("orphans instead of deleting, however untouched", () => {
    const card = {
      project: "engineering",
      epic: "Cozystack",
      parent: "p1",
      assignees: [],
      progress: 0,
    } as unknown as Card;
    expect(removeFromProjectOutcome(card, "engineering", "Cozystack")).toBe("orphan");
  });
});

// The Project board draws a subtask that carries its own column, so the
// column's progress must count it — but never twice: a parent standing in
// the same project already answers for its children.
describe("countedForProgress", () => {
  const child = { itemId: "c1", parent: "p1", project: "engineering", epic: "Cozystack" } as Card;
  const index = (cards: Card[]) => new Map(cards.map((c) => [c.itemId, c]));

  it("counts a subtask whose parent is on no board of this project", () => {
    // The parent lives in the weekly plan with no column of its own.
    const parent = { itemId: "p1" } as Card;
    expect(countedForProgress(child, index([child, parent]), "project")).toBe(true);
  });

  it("skips a subtask whose parent stands in the same project", () => {
    const parent = {
      itemId: "p1",
      project: "engineering",
      epic: "Roadmap",
      startDate: "2026-08-24",
    } as Card;
    expect(countedForProgress(child, index([child, parent]), "project")).toBe(false);
  });

  it("counts a subtask whose parent is drawn nowhere", () => {
    // A parent attached to a column but carrying no dates is no slot, so
    // nothing counts it — deferring to it dropped the child's work from
    // every bar while its slot was drawn in the column.
    const parent = { itemId: "p1", project: "engineering", epic: "Roadmap" } as Card;
    expect(countedForProgress(child, index([child, parent]), "project")).toBe(true);
  });

  it("keeps a child in ITS column's bar when the parent stands in another", () => {
    // Column bars are per column: a parent in X answers for the work in X,
    // and the child drawn in Y is Y's only work. Deduplicating by PROJECT
    // there subtracted a column's own slot from its own percentage.
    const parent = {
      itemId: "p1",
      project: "engineering",
      epic: "Roadmap",
      startDate: "2026-08-24",
    } as Card;
    expect(countedForProgress(child, index([child, parent]), "column")).toBe(true);
    // The project bar still counts it once: the parent's own progress
    // already derives from this child.
    expect(countedForProgress(child, index([child, parent]), "project")).toBe(false);
  });

  it("counts an ordinary card always", () => {
    const plain = { itemId: "x", project: "engineering", epic: "Cozystack" } as Card;
    expect(countedForProgress(plain, index([plain]), "project")).toBe(true);
  });
});

// placementTargets is the dispatcher the boards call: which section a card
// gets is decided by what it is, not where it is rendered.
describe("placementTargets", () => {
  const board = {
    projects: ["engineering"],
    epics: [{ name: "Cozystack", project: "engineering" }],
    projectDomains: undefined,
    processes: [{ name: "Invoicing" }, { name: "Reporting" }],
  };

  it("offers mirrors to a card already in a column", () => {
    const got = placementTargets({ project: "engineering", epic: "Cozystack" } as Card, board);
    expect(got.mirror).toEqual([]);
    expect(got.attach).toBeUndefined();
  });

  it("offers a SUBTASK a column to attach to, but never a second one", () => {
    // A subtask may carry ONE column of its own (G14, S4) — so the × that
    // takes the column away must have a way back, or the Project board
    // hands out a one-way door. What it may not have is a SECOND placement
    // or a process tie: its file rides its parent, and both would be
    // stranded the moment the parent changes repository.
    const inColumn = placementTargets(
      { project: "engineering", epic: "Cozystack", parent: "p1" } as Card,
      board,
    );
    expect(inColumn.mirror).toBeUndefined();
    expect(inColumn.processes).toBeUndefined();

    const loose = placementTargets({ parent: "p1", stage: "recurrent" } as Card, board);
    expect(loose.attach).toEqual([{ name: "engineering", epics: ["Cozystack"] }]);
    expect(loose.processes).toBeUndefined();
  });

  it("offers a subtask already in a column nothing more", () => {
    // It has its column; a second one is refused by the server
    // (ErrSubtaskMirror), so no board offers it. The Me and Team boards
    // each remembered this separately — the rule belongs here, where the
    // module promises "only what the server would accept".
    const got = placementTargets(
      { project: "engineering", epic: "Cozystack", parent: "p1" } as Card,
      board,
    );
    expect(got.mirror).toBeUndefined();
    expect(got.attach).toBeUndefined();
    expect(got.processes).toBeUndefined();
  });

  it("offers a card in a no-project column the mirrors of its repository", () => {
    // A no-project column is a home like any other: it names a repository
    // (its own), so the card mirrors inside it. The refusal this case used
    // to pin was the PROJECT's answer to the column's question.
    const got = placementTargets({ epic: "Inbox" } as Card, board);
    expect(got.mirror).toEqual([{ name: "engineering", epics: ["Cozystack"] }]);
    expect(got.attach).toBeUndefined();
  });

  it("offers processes to a recurrent card", () => {
    const got = placementTargets({ stage: "recurrent" } as Card, board);
    expect(got.processes).toEqual(["Invoicing", "Reporting"]);
    expect(got.attach).toBeUndefined();
  });

  it("drops the process the card is already tied to", () => {
    // Only works because spec.process round-trips: the server serves the
    // stored tie back, so after a re-list the card carries it here.
    const got = placementTargets({ stage: "recurrent", process: "Invoicing" } as Card, board);
    expect(got.processes).toEqual(["Reporting"]);
  });

  it("offers a process TURN nothing — its process is its task's", () => {
    const got = placementTargets({ stage: "recurrent", task: "t1" } as Card, board);
    expect(got.processes).toBeUndefined();
    expect(got.attach).toBeUndefined();
    expect(got.mirror).toBeUndefined();
  });

  it("offers only processes of the card's own repository", () => {
    // The server refuses a cross-repository tie (ErrCrossDomain), so the
    // menu must not offer one: dead items ending in a 422 are not targets.
    const multi = { ...board, processDomains: { Reporting: "founders" } };
    expect(placementTargets({ stage: "recurrent" } as Card, multi).processes).toEqual([
      "Invoicing",
    ]);
    expect(
      placementTargets({ stage: "recurrent", domain: "founders" } as Card, multi).processes,
    ).toEqual(["Reporting"]);
  });

  it("offers projects to everything else", () => {
    const got = placementTargets({} as Card, board);
    expect(got.attach).toEqual([{ name: "engineering", epics: ["Cozystack"] }]);
  });
});

// Dragging a slot acts on the placement it was grabbed in. The everyday
// gesture — nudging a mirror copy a week down without leaving its column —
// used to re-file the card's home into the mirror column, collapsing two
// placements into one with no question asked.
describe("slotDragPlan", () => {
  const card = { project: "engineering", epic: "Cozystack" } as Card;
  const home = { project: "engineering", epic: "Cozystack" };
  const mirror = { project: "freedom", epic: "Launch" };
  const third = { project: "freedom", epic: "Ship" };

  it("a vertical move — in any column — is a date change only", () => {
    expect(slotDragPlan(card, home, home)).toEqual({ kind: "dates" });
    expect(slotDragPlan(card, mirror, mirror)).toEqual({ kind: "dates" });
  });

  it("dragging the home into another column re-files the home", () => {
    expect(slotDragPlan(card, home, mirror)).toEqual({ kind: "refileHome" });
  });

  it("dragging a mirror copy moves the mirror, not the home", () => {
    expect(slotDragPlan(card, mirror, third)).toEqual({ kind: "moveMirror" });
  });

  it("dropping a mirror onto the home collapses it into the home", () => {
    expect(slotDragPlan(card, mirror, home)).toEqual({ kind: "collapseMirror" });
  });
});

// The optimistic mirror list is the one the server converges on — a drag
// must never draw one slot twice in a column while the round trip runs.
describe("slotDropMirrors", () => {
  const grabbed = { project: "freedom", epic: "Launch" };

  it("folds a mirror dragged onto a column the card already mirrors", () => {
    const card = {
      mirrors: [grabbed, { project: "freedom", epic: "Ship" }],
    } as Card;
    expect(
      slotDropMirrors(card, grabbed, { project: "freedom", epic: "Ship" }, "moveMirror"),
    ).toEqual([{ project: "freedom", epic: "Ship" }]);
  });

  it("drops the mirror the re-filed home lands on", () => {
    const card = { mirrors: [grabbed] } as Card;
    expect(
      slotDropMirrors(card, { project: "engineering", epic: "Cozystack" }, grabbed, "refileHome"),
    ).toEqual([]);
  });

  it("keeps the standing order when a drop folds into an existing mirror", () => {
    // The server keeps the standing entry where it is, and the FIRST
    // mirror is the promotion heir — reordering optimistically would show
    // the wrong heir until the re-list.
    const card = {
      mirrors: [
        { project: "freedom", epic: "A" },
        { project: "freedom", epic: "B" },
        { project: "freedom", epic: "C" },
      ],
    } as Card;
    expect(
      slotDropMirrors(card, { project: "freedom", epic: "C" }, { project: "freedom", epic: "A" }, "moveMirror"),
    ).toEqual([
      { project: "freedom", epic: "A" },
      { project: "freedom", epic: "B" },
    ]);
  });

  it("moves and collapses as the plain cases say", () => {
    const card = { mirrors: [grabbed] } as Card;
    expect(
      slotDropMirrors(card, grabbed, { project: "freedom", epic: "Ship" }, "moveMirror"),
    ).toEqual([{ project: "freedom", epic: "Ship" }]);
    expect(
      slotDropMirrors(card, grabbed, { project: "engineering", epic: "Cozystack" }, "collapseMirror"),
    ).toEqual([]);
  });
});

// A mirror drop is up to three requests. A failure in the middle leaves the
// server holding a state the pre-drag snapshot does not describe, so the
// error path must re-list — a silent local restore showed a board the
// server did not hold until something else touched the card.
describe("settleMirrorDrop", () => {
  const grabbed = { project: "freedom", epic: "Launch" };
  const target = { project: "freedom", epic: "Ship" };
  const ui = () => {
    const calls: string[] = [];
    return {
      calls,
      restore: () => calls.push("restore"),
      reload: () => calls.push("reload"),
      onError: (m: string) => calls.push(`error:${m}`),
      errMessage: (e: unknown) => String(e),
    };
  };

  it("re-lists after a failure half-way through the chain", async () => {
    const u = ui();
    await settleMirrorDrop(
      {
        mirrorCard: () => Promise.resolve(),
        unmirrorCard: () => Promise.reject(new Error("boom")),
        patchCard: () => Promise.resolve(),
      },
      "c1",
      grabbed,
      target,
      "moveMirror",
      { start: "2026-08-24", end: "2026-08-28" },
      u,
    );
    expect(u.calls).toEqual(["restore", "error:Error: boom", "reload"]);
  });

  it("re-lists once on success and never restores", async () => {
    const u = ui();
    const seen: string[] = [];
    await settleMirrorDrop(
      {
        mirrorCard: (_, p, e) => (seen.push(`mirror:${p}/${e}`), Promise.resolve()),
        unmirrorCard: (_, p, e) => (seen.push(`unmirror:${p}/${e}`), Promise.resolve()),
        patchCard: () => (seen.push("dates"), Promise.resolve()),
      },
      "c1",
      grabbed,
      target,
      "moveMirror",
      { start: "2026-08-24", end: "2026-08-28" },
      u,
    );
    // The new placement lands before the old one goes.
    expect(seen).toEqual(["mirror:freedom/Ship", "unmirror:freedom/Launch", "dates"]);
    expect(u.calls).toEqual(["reload"]);
  });
});

// A mirrored card renders once per column; while one copy is dragged, only
// THAT placement dims — dimming by card id alone dimmed the home copy
// whenever a mirror copy moved.
describe("movingSlot", () => {
  const move = {
    card: { itemId: "c1" },
    grabbed: { project: "freedom", epic: "Launch" },
  };
  it("dims exactly the grabbed placement", () => {
    expect(movingSlot(move, "c1", { project: "freedom", epic: "Launch" })).toBe(true);
    expect(movingSlot(move, "c1", { project: "engineering", epic: "Cozystack" })).toBe(false);
    expect(movingSlot(move, "c2", { project: "freedom", epic: "Launch" })).toBe(false);
    expect(movingSlot(null, "c1", { project: "freedom", epic: "Launch" })).toBe(false);
  });
});

// The attach patch mirrors the server's G55 clause: only a card with NO
// dates of its own takes its week's slot — a chosen schedule survives.
describe("makeCardPlacements onAttachProject", () => {
  const board = {
    projects: ["engineering", "freedom"],
    epics: [
      { name: "Cozystack", project: "engineering" },
      { name: "Launch", project: "freedom" },
    ],
    projectDomains: undefined,
    processes: [],
  };
  const run = (card: Card, act?: (p: CardPlacements) => void) => {
    const patches: Partial<Card>[] = [];
    const deps = {
      provider: {
        patchCard: () => Promise.resolve(undefined),
        mirrorCard: () => Promise.resolve(),
        unmirrorCard: () => Promise.resolve(),
      },
      patchCard: (_: string, patch: Partial<Card>) => {
        patches.push(patch);
      },
      reload: () => {},
      onError: () => {},
      errMessage: () => "",
    };
    const placements = makeCardPlacements(card, board, deps);
    if (act) {
      act(placements);
    } else {
      placements.onAttachProject("engineering", "Cozystack");
    }
    return patches[0];
  };

  it("gives a dateless plan card its week's slot", () => {
    const patch = run({ itemId: "c1", plan: "fri", week: "2026-08-24" } as Card);
    expect(patch.startDate).toBe("2026-08-24");
    expect(patch.day).toBe("2026-08-28");
  });

  it("adds the placement optimistically when mirroring", () => {
    // The Project board draws one slot per placement, so the patch is what
    // puts the card in the second column before the round trip returns.
    const patch = run(
      { itemId: "c1", project: "engineering", epic: "Cozystack" } as Card,
      (p) => p.onMirror("freedom", "Launch"),
    );
    expect(patch.mirrors).toEqual([{ project: "freedom", epic: "Launch" }]);
  });

  it("keeps a dated card's chosen schedule", () => {
    const patch = run({
      itemId: "c1",
      plan: "fri",
      week: "2026-08-24",
      startDate: "2026-09-07",
      day: "2026-09-09",
    } as Card);
    expect(patch.startDate).toBeUndefined();
    expect(patch.day).toBeUndefined();
  });
});

// The Project board's own filter, as a rule rather than a line inside a
// component: which cards a column grid draws.
describe("drawnOnProjectBoard", () => {
  const shown = new Set(["engineering\u0000Cozystack", "freedom\u0000Launch"]);

  it("draws a card filed under a shown column", () => {
    expect(
      drawnOnProjectBoard({ project: "engineering", epic: "Cozystack" } as Card, shown),
    ).toBe(true);
  });

  it("draws a SUBTASK that carries its own column", () => {
    expect(
      drawnOnProjectBoard(
        { project: "engineering", epic: "Cozystack", parent: "p1" } as Card,
        shown,
      ),
    ).toBe(true);
  });

  it("draws nothing for a card with no column of its own", () => {
    expect(drawnOnProjectBoard({ parent: "p1" } as Card, shown)).toBe(false);
    expect(drawnOnProjectBoard({ project: "engineering" } as Card, shown)).toBe(false);
  });

  it("draws a card whose MIRROR names a shown column", () => {
    expect(
      drawnOnProjectBoard(
        {
          project: "strategy",
          epic: "Fundraising",
          mirrors: [{ project: "freedom", epic: "Launch" }],
        } as Card,
        shown,
      ),
    ).toBe(true);
  });
});

// #2 the slot menu must stay reachable: a subtask's team follows its
// parent, but that is a reason to show the team read-only, not to take the
// menu's handle away — the same menu carries "Mark as done" and "Mirror
// to…".
describe("teamFollowsParent", () => {
  it("is true for a subtask and false for a standalone card", () => {
    expect(teamFollowsParent({ parent: "p1" } as Card)).toBe(true);
    expect(teamFollowsParent({} as Card)).toBe(false);
  });
});

// #8 the progress bars count what the grid DRAWS. A card in a column with
// no dates has no slot — attaching a column to a dateless subtask (group,
// then attach) put occupancy into the bar that nothing rendered.
describe("drawnAsSlot", () => {
  it("needs dates to be a slot", () => {
    expect(drawnAsSlot({ epic: "Cozystack", startDate: "2026-08-24" } as Card)).toBe(true);
    expect(drawnAsSlot({ epic: "Cozystack", week: "2026-08-24" } as Card)).toBe(true);
    expect(drawnAsSlot({ epic: "Cozystack" } as Card)).toBe(false);
  });
});

// #6 an empty target list is still an empty section: [] is truthy, so the
// origin block drew a stray divider for a card whose repository offers no
// column at all.
describe("hasPlacementOffer", () => {
  it("is false when every section is empty", () => {
    expect(hasPlacementOffer({ attach: [] })).toBe(false);
    expect(hasPlacementOffer({})).toBe(false);
    expect(hasPlacementOffer(undefined)).toBe(false);
  });

  it("is true as soon as one section has an entry", () => {
    expect(hasPlacementOffer({ attach: [{ name: "engineering", epics: ["Cozystack"] }] })).toBe(
      true,
    );
    expect(hasPlacementOffer({ processes: ["Invoicing"] })).toBe(true);
    expect(hasPlacementOffer({ mirror: [{ name: "freedom", epics: ["Launch"] }] })).toBe(true);
  });
});

// A project NAME can be declared in two repositories, its columns merged
// under one entry (G13) while each column keeps its own. The picker must
// ask the COLUMN — filtering by the project offered a column the server
// refuses, which is exactly what these targets promise never to do.
describe("targets on an alias project", () => {
  const board = {
    projects: ["portal"],
    epics: [
      { name: "Bugs", project: "portal" },
      { name: "Docs", project: "portal", domain: "closed" },
    ],
    projectDomains: undefined,
    processes: [],
  };

  it("attaches a primary card only to the primary column", () => {
    const got = placementTargets({} as Card, board);
    expect(got.attach).toEqual([{ name: "portal", epics: ["Bugs"] }]);
  });

  it("attaches a closed card only to the closed column", () => {
    const got = placementTargets({ domain: "closed" } as Card, board);
    expect(got.attach).toEqual([{ name: "portal", epics: ["Docs"] }]);
  });

  it("mirrors within the card's own repository", () => {
    const got = placementTargets(
      { project: "portal", epic: "Bugs", domain: "" } as Card,
      board,
    );
    expect(got.mirror).toEqual([]);
  });
});

// The origin block draws what the card IS, not what the menu can offer.
// Asking the offer instead left a card in a no-project column — newly
// mirrorable — with an empty block: a stray divider line, the artefact
// hasPlacementOffer was written to remove.
describe("hasOriginToShow", () => {
  it("is what the block asks — the menus are its siblings, not its children", () => {
    // A fresh unattached card has attach targets and no origin: asking the
    // OFFER drew the block empty, which is a bare divider bar with nothing
    // above it (the placement menus render outside this block and hide
    // themselves when they have nothing to list).
    expect(hasOriginToShow({} as Card)).toBe(false);
  });

  it("counts a column of no project", () => {
    expect(hasOriginToShow({ epic: "Inbox" } as Card)).toBe(true);
  });

  it("counts a project, a process and a mirror", () => {
    expect(hasOriginToShow({ project: "engineering" } as Card)).toBe(true);
    expect(hasOriginToShow({ process: "Invoicing" } as Card)).toBe(true);
    expect(
      hasOriginToShow({ mirrors: [{ project: "freedom", epic: "Launch" }] } as Card),
    ).toBe(true);
  });

  it("is false for a card that came from nowhere", () => {
    expect(hasOriginToShow({} as Card)).toBe(false);
  });
});
