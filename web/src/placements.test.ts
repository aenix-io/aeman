import { describe, expect, it } from "vitest";

import {
  attachSlotDates,
  attachTargets,
  canCreateInColumn,
  columnFollows,
  columnsOf,
  markOf,
  countedAmong,
  countedForProgress,
  drawnAsSlot,
  drawnOnProjectBoard,
  hasOriginToShow,
  makeCardPlacements,
  mirrorTargets,
  movingSlot,
  placementTargets,
  projectsAColumnCanJoin,
  projectsWithColumns,
  removeFromProjectOutcome,
  rosterOf,
  settleMirrorDrop,
  slotDragPlan,
  slotDropMirrors,
  teamFollowsParent,
  teamlessIsLawful,
  teamsACardCanTake,
  type CardPlacements,
} from "./placements";
import type { Card } from "./providers/types";

// The picker offers only columns the server would accept: projects of the
// card's own repository, each with its epics — a pair the server refuses
// has no business being clickable.
/** card builds the slice of a card these rules read, TYPED. A cast through
 *  `unknown` asserts nothing about the shape it hides, and these cases are
 *  the rules' documentation. */
const mk = (c: Partial<Card>): Card => c as Card;

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

  // The PROJECT carries the card into its repository and the COLUMN has to
  // be in that same one, so both stamps are part of the question — the
  // roster's for the project, the column's own for the column.
  const roster = {
    projectDomains: { strategy: "founders" },
    primary: "shared",
    single: false,
  };

  it("offers the projects of the card's repository, with their epics", () => {
    const got = attachTargets(projects, epics, "", true, roster);
    expect(got).toEqual([
      { name: "engineering", epics: ["Cozystack", "Ingress"] },
      { name: "freedom", epics: ["Launch"] },
    ]);
    // A card living in the founders repository sees only founders projects.
    expect(attachTargets(projects, epics, "founders", true, roster)).toEqual([
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

// Attaching a card scheduled for a WEEK gives it the slot of that week —
// Monday to Friday, the same dates the server writes, so the optimistic card
// does not jump on the re-list.
describe("attachSlotDates", () => {
  it("spans the week it was taken from, Monday to Friday", () => {
    expect(attachSlotDates("2026-08-24")).toEqual({
      startDate: "2026-08-24",
      day: "2026-08-28",
    });
  });
});

// The Project board's × means four different things, and the UI must know
// which before it asks anything: a mirror goes silently, the home hands
// over silently, an orphan survives, and only the delete is worth a
// question.
describe("removeFromProjectOutcome", () => {
  const base = mk({ project: "engineering", epic: "Cozystack" });

  it("is unmirror on a mirror placement", () => {
    const c = { ...base, mirrors: [{ project: "freedom", epic: "Launch" }] } as Card;
    expect(removeFromProjectOutcome(c, "freedom", "Launch")).toBe("unmirror");
  });

  it("is promote on the home while mirrors remain", () => {
    const c = { ...base, mirrors: [{ project: "freedom", epic: "Launch" }] } as Card;
    expect(removeFromProjectOutcome(c, "engineering", "Cozystack")).toBe("promote");
  });

  it("is orphan on the last column of a worked card, delete otherwise", () => {
    const worked = mk({ ...base, assignees: ["kvaps"], progress: 40 });
    expect(removeFromProjectOutcome(worked, "engineering", "Cozystack")).toBe("orphan");
    const idle = mk({ ...base, assignees: [] });
    expect(removeFromProjectOutcome(idle, "engineering", "Cozystack")).toBe("delete");
    // Progress without a person, or a person without progress, is not
    // "worked" — the server deletes it, and the UI must ask first.
    expect(
      removeFromProjectOutcome(mk({ ...base, assignees: [], progress: 40 }), "engineering", "Cozystack"),
    ).toBe("delete");
  });
});

// A subtask's home is its parent, so the Project board's × may take it out
// of a column but never delete it — the server refuses to as well.
describe("removeFromProjectOutcome for a subtask", () => {
  it("orphans instead of deleting, however untouched", () => {
    const card = mk({
      project: "engineering",
      epic: "Cozystack",
      parent: "p1",
      assignees: [],
      progress: 0,
    });
    expect(removeFromProjectOutcome(card, "engineering", "Cozystack")).toBe("orphan");
  });
});

// The Project board draws a subtask that carries its own column, so the
// column's progress must count it — but never twice: a parent standing in
// the same project already answers for its children.
describe("countedForProgress", () => {
  const child = mk({ itemId: "c1", parent: "p1", project: "engineering", epic: "Cozystack" });
  const index = (cards: Card[]) => new Map(cards.map((c) => [c.itemId, c]));

  it("counts a subtask whose parent is on no board of this project", () => {
    // The parent is on no board of this project — no column of its own.
    const parent = mk({ itemId: "p1" });
    expect(countedForProgress(child, index([child, parent]), { project: "engineering" })).toBe(true);
  });

  it("skips a subtask whose parent stands in the same project", () => {
    const parent = mk({
      itemId: "p1",
      project: "engineering",
      epic: "Roadmap",
      startDate: "2026-08-24",
    });
    expect(countedForProgress(child, index([child, parent]), { project: "engineering" })).toBe(false);
  });

  it("counts a subtask whose parent is drawn nowhere", () => {
    // A parent attached to a column but carrying no dates is no slot, so
    // nothing counts it — deferring to it dropped the child's work from
    // every bar while its slot was drawn in the column.
    const parent = mk({ itemId: "p1", project: "engineering", epic: "Roadmap" });
    expect(countedForProgress(child, index([child, parent]), { project: "engineering" })).toBe(true);
  });

  it("keeps a child in ITS column's bar when the parent stands in another", () => {
    // Column bars are per column: a parent in X answers for the work in X,
    // and the child drawn in Y is Y's only work. Deduplicating by PROJECT
    // there subtracted a column's own slot from its own percentage.
    const parent = mk({
      itemId: "p1",
      project: "engineering",
      epic: "Roadmap",
      startDate: "2026-08-24",
    });
    expect(countedForProgress(child, index([child, parent]), { project: "engineering", epic: "Cozystack" })).toBe(true);
    // The project bar still counts it once: the parent's own progress
    // already derives from this child.
    expect(countedForProgress(child, index([child, parent]), { project: "engineering" })).toBe(false);
  });

  // A parent MIRRORED into the column its child stands in answers for that
  // child there: the column's bar counted both, since the parent's home
  // epic differed while the column it was drawn in did not.
  it("skips a child whose parent MIRRORS into the same column", () => {
    const parent = {
      itemId: "p1",
      project: "engineering",
      epic: "Roadmap",
      startDate: "2026-08-24",
      mirrors: [{ project: "engineering", epic: "Cozystack" }],
    } as Card;
    expect(
      countedForProgress(child, index([child, parent]), {
        project: "engineering",
        epic: "Cozystack",
      }),
    ).toBe(false);
    // …and still counts in the column the parent is NOT drawn in.
    expect(
      countedForProgress(child, index([child, parent]), {
        project: "engineering",
        epic: "Ingress",
      }),
    ).toBe(true);
  });

  it("counts an ordinary card always", () => {
    const plain = mk({ itemId: "x", project: "engineering", epic: "Cozystack" });
    expect(countedForProgress(plain, index([plain]), { project: "engineering" })).toBe(true);
  });
});

// placementTargets is the dispatcher the boards call: which section a card
// gets is decided by what it is, not where it is rendered.
describe("placementTargets", () => {
  const board = {
    domains: [
      { name: "shared", writable: true, members: [] },
      { name: "closed", writable: true, members: [] },
    ],
    projects: ["engineering"],
    epics: [{ name: "Cozystack", project: "engineering" }],
    projectDomains: undefined,
    processes: [{ name: "Invoicing" }, { name: "Reporting" }],
  };

  it("offers mirrors to a card already in a column", () => {
    const got = placementTargets(mk({ project: "engineering", epic: "Cozystack" }), board);
    expect(got.mirror).toEqual([]);
    expect(got.attach).toBeUndefined();
  });

  it("offers a SUBTASK a column to attach to, but never a second one", () => {
    // A subtask may carry ONE column of its own (G14, G57) — so the × that
    // takes the column away must have a way back, or the Project board
    // hands out a one-way door. What it may not have is a SECOND placement
    // or a process tie: its file rides its parent, and both would be
    // stranded the moment the parent changes repository.
    const inColumn = placementTargets(
      mk({ project: "engineering", epic: "Cozystack", parent: "p1" }),
      board,
    );
    expect(inColumn.mirror).toBeUndefined();
    expect(inColumn.processes).toBeUndefined();

    const loose = placementTargets(mk({ parent: "p1", stage: "recurrent" }), board);
    expect(loose.attach).toEqual([{ name: "engineering", epics: ["Cozystack"] }]);
    expect(loose.processes).toBeUndefined();
  });

  it("offers a subtask already in a column nothing more", () => {
    // It has its column; a second one is refused by the server
    // (ErrSubtaskMirror), so no board offers it. The Me and Team boards
    // each remembered this separately — the rule belongs here, where the
    // module promises "only what the server would accept".
    const got = placementTargets(
      mk({ project: "engineering", epic: "Cozystack", parent: "p1" }),
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
    const got = placementTargets(mk({ epic: "Inbox" }), board);
    expect(got.mirror).toEqual([{ name: "engineering", epics: ["Cozystack"] }]);
    expect(got.attach).toBeUndefined();
  });

  it("offers processes to a recurrent card", () => {
    const got = placementTargets(mk({ stage: "recurrent" }), board);
    expect(got.processes).toEqual(["Invoicing", "Reporting"]);
    expect(got.attach).toBeUndefined();
  });

  it("drops the process the card is already tied to", () => {
    // Only works because spec.process round-trips: the server serves the
    // stored tie back, so after a re-list the card carries it here.
    const got = placementTargets(mk({ stage: "recurrent", process: "Invoicing" }), board);
    expect(got.processes).toEqual(["Reporting"]);
  });

  it("offers a process TURN nothing — its process is its task's", () => {
    const got = placementTargets(mk({ stage: "recurrent", task: "t1" }), board);
    expect(got.processes).toBeUndefined();
    expect(got.attach).toBeUndefined();
    expect(got.mirror).toBeUndefined();
  });

  it("offers only processes of the card's own repository", () => {
    // The server refuses a cross-repository tie (ErrCrossDomain), so the
    // menu must not offer one: dead items ending in a 422 are not targets.
    const multi = { ...board, processDomains: { Reporting: "founders" } };
    expect(placementTargets(mk({ stage: "recurrent" }), multi).processes).toEqual([
      "Invoicing",
    ]);
    expect(
      placementTargets(mk({ stage: "recurrent", domain: "founders" }), multi).processes,
    ).toEqual(["Reporting"]);
  });

  it("offers projects to everything else", () => {
    const got = placementTargets(mk({}), board);
    expect(got.attach).toEqual([{ name: "engineering", epics: ["Cozystack"] }]);
  });
});

// Dragging a slot acts on the placement it was grabbed in. The everyday
// gesture — nudging a mirror copy a week down without leaving its column —
// used to re-file the card's home into the mirror column, collapsing two
// placements into one with no question asked.
describe("slotDragPlan", () => {
  const card = mk({ project: "engineering", epic: "Cozystack" });
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
    const card = mk({ mirrors: [grabbed] });
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
    const card = mk({ mirrors: [grabbed] });
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
    domains: [
      { name: "shared", writable: true, members: [] },
      { name: "closed", writable: true, members: [] },
    ],
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

  it("gives a dateless card scheduled for a week that week's slot", () => {
    const patch = run(mk({ itemId: "c1", week: "2026-08-24" }));
    expect(patch.startDate).toBe("2026-08-24");
    expect(patch.day).toBe("2026-08-28");
  });

  it("adds the placement optimistically when mirroring", () => {
    // The Project board draws one slot per placement, so the patch is what
    // puts the card in the second column before the round trip returns.
    const patch = run(
      mk({ itemId: "c1", project: "engineering", epic: "Cozystack" }),
      (p) => p.onMirror("freedom", "Launch"),
    );
    expect(patch.mirrors).toEqual([{ project: "freedom", epic: "Launch" }]);
  });

  it("keeps a dated card's chosen schedule", () => {
    const patch = run(mk({
      itemId: "c1",
      week: "2026-08-24",
      startDate: "2026-09-07",
      day: "2026-09-09",
    }));
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
      drawnOnProjectBoard(mk({ project: "engineering", epic: "Cozystack" }), shown),
    ).toBe(true);
  });

  it("draws a SUBTASK that carries its own column", () => {
    expect(
      drawnOnProjectBoard(
        mk({ project: "engineering", epic: "Cozystack", parent: "p1" }),
        shown,
      ),
    ).toBe(true);
  });

  it("draws nothing for a card with no column of its own", () => {
    expect(drawnOnProjectBoard(mk({ parent: "p1" }), shown)).toBe(false);
    expect(drawnOnProjectBoard(mk({ project: "engineering" }), shown)).toBe(false);
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

// the slot menu must stay reachable: a subtask's team follows its
// parent, but that is a reason to show the team read-only, not to take the
// menu's handle away — the same menu carries "Mark as done" and "Mirror
// to…".
describe("teamFollowsParent", () => {
  it("is true for a subtask and false for a standalone card", () => {
    expect(teamFollowsParent(mk({ parent: "p1" }))).toBe(true);
    expect(teamFollowsParent(mk({}))).toBe(false);
  });
});

// the progress bars count what the grid DRAWS. A card in a column with
// no dates has no slot — attaching a column to a dateless subtask (group,
// then attach) put occupancy into the bar that nothing rendered.
describe("drawnAsSlot", () => {
  it("needs dates to be a slot", () => {
    expect(drawnAsSlot(mk({ epic: "Cozystack", startDate: "2026-08-24" }))).toBe(true);
    expect(drawnAsSlot(mk({ epic: "Cozystack", week: "2026-08-24" }))).toBe(true);
    expect(drawnAsSlot(mk({ epic: "Cozystack" }))).toBe(false);
  });
});


// A project NAME can be declared in two repositories, its columns merged
// under one entry (G13) while each column keeps its own. The picker must
// ask the COLUMN — filtering by the project offered a column the server
// refuses, which is exactly what these targets promise never to do.
describe("targets on an alias project", () => {
  const board = {
    domains: [
      { name: "shared", writable: true, members: [] },
      { name: "closed", writable: true, members: [] },
    ],
    projects: ["portal"],
    epics: [
      { name: "Bugs", project: "portal" },
      { name: "Docs", project: "portal", domain: "closed" },
    ],
    projectDomains: undefined,
    processes: [],
  };

  it("attaches a primary card only to the primary column", () => {
    // A TEAM holds this card in its repository (G46), so the columns of
    // another one are not destinations; a card nothing holds is carried
    // by whatever project takes it, and is offered both.
    const got = placementTargets(mk({ team: "platform" }), board);
    expect(got.attach).toEqual([{ name: "portal", epics: ["Bugs"] }]);
  });

  it("offers a closed card nothing under the alias project", () => {
    // The merged entry resolves to ONE repository — the primary's, here —
    // so the project would carry the card there, and its team holds it in
    // the closed one (G46). The closed COLUMN under that name is not a
    // destination either: the server compares the column's repository
    // against the project's and returns 422. Offering it was the friendly
    // label on a refusal.
    const got = placementTargets(mk({ domain: "closed", team: "board" }), board);
    expect(got.attach).toEqual([]);
  });

  it("offers a card nothing holds only the columns its new project can hold", () => {
    // No team to keep it anywhere, so the project carries it — but only to
    // the repository the project itself is in, which the OTHER half of the
    // alias is not.
    const got = placementTargets(mk({}), board);
    expect(got.attach).toEqual([{ name: "portal", epics: ["Bugs"] }]);
  });

  it("mirrors within the card's own repository", () => {
    const got = placementTargets(
      mk({ project: "portal", epic: "Bugs", domain: "" }),
      board,
    );
    expect(got.mirror).toEqual([]);
  });
});

// The origin block draws what the card IS, not what the menu can offer.
// Asking the offer instead left a card in a no-project column — newly
// mirrorable — with an empty block: a stray divider line, the artefact
// the origin rule exists to prevent.
describe("hasOriginToShow", () => {
  it("is what the block asks — the menus are its siblings, not its children", () => {
    // A fresh unattached card has attach targets and no origin: asking the
    // OFFER drew the block empty, which is a bare divider bar with nothing
    // above it (the placement menus render outside this block and hide
    // themselves when they have nothing to list).
    expect(hasOriginToShow(mk({}))).toBe(false);
  });

  it("counts a column of no project", () => {
    expect(hasOriginToShow(mk({ epic: "Inbox" }))).toBe(true);
  });

  it("counts a project, a process and a mirror", () => {
    expect(hasOriginToShow(mk({ project: "engineering" }))).toBe(true);
    expect(hasOriginToShow(mk({ process: "Invoicing" }))).toBe(true);
    expect(
      hasOriginToShow({ mirrors: [{ project: "freedom", epic: "Launch" }] } as Card),
    ).toBe(true);
  });

  it("is false for a card that came from nowhere", () => {
    expect(hasOriginToShow(mk({}))).toBe(false);
  });
});

// The pickers narrow themselves to what the server accepts — the promise
// this module opens with. Two were left offering refusals after the column
// rules changed: a column cannot change repository at all, and a card in a
// no-project column takes its repository from its TEAM, so only that
// repository's teams can hold it.
describe("pickers that must not offer a refusal", () => {
  // With the roster, as the boards pass it: the stamps are read in the
  // board's one namespace, so an unstamped entry is the primary rather
  // than a repository of its own.
  const roster = { primary: "aeman-db", single: false };

  it("offers a column only the projects of its own repository", () => {
    const projects = ["engineering", "strategy"];
    const domains = { strategy: "founders" };
    expect(projectsAColumnCanJoin("aeman-db", projects, domains, "", roster)).toEqual([
      "engineering",
    ]);
    expect(projectsAColumnCanJoin("founders", projects, domains, "", roster)).toEqual([
      "strategy",
    ]);
    // And without a roster to read them in — a board that draws no
    // boundaries — nothing is refused.
    expect(projectsAColumnCanJoin("founders", projects, domains, "", { single: true })).toEqual(
      projects,
    );
  });

  it("offers a card in a closed column only that repository's teams", () => {
    const teams = ["platform", "board"];
    const domains = { board: "founders" };
    expect(teamsACardCanTake("founders", teams, domains, "", roster)).toEqual(["board"]);
    // What the card already carries stays on the list, so a pair written
    // before the rule can be seen and changed.
    expect(teamsACardCanTake("founders", teams, domains, "platform", roster)).toEqual([
      "platform",
      "board",
    ]);
  });
});

// The exported mirrors of the server's rules carry its refusals too — an
// outcome that says "delete" where the server says "no such column" is a
// deletion nobody sanctioned, which is the shape the server's own guard
// was written against.
describe("removeFromProjectOutcome refuses what the server refuses", () => {
  it("refuses the empty pair — a column is named by its epic", () => {
    expect(removeFromProjectOutcome(mk({}), "", "")).toBe("refused");
  });

  it("refuses a column the card does not stand in", () => {
    const card = mk({ project: "engineering", epic: "Cozystack" });
    expect(removeFromProjectOutcome(card, "freedom", "Launch")).toBe("refused");
  });

  it("still answers for the column the card is in", () => {
    const card = mk({
      project: "engineering",
      epic: "Cozystack",
      assignees: [],
      progress: 0,
    });
    expect(removeFromProjectOutcome(card, "engineering", "Cozystack")).toBe("delete");
  });
});

// Attaching asks where the card will BE. A card whose project decides is
// carried along by the new one, so a column of another repository is a
// lawful destination — the server accepts it, and hiding it kept the
// no-project bucket unreachable. A card its TEAM holds stays put (G46).
describe("attachTargets and the card's binding", () => {
  const projects = ["engineering", "strategy"];
  const epics = [
    { name: "Cozystack", project: "engineering" },
    { name: "Fundraising", project: "strategy", domain: "founders" },
  ];
  // As the server sends it: the project of the closed column is stamped
  // too, and a column agrees with the project that owns it.
  const projectDomains = { strategy: "founders" };
  // Two repositories, the primary named: the shape a board with a boundary
  // to police is served in. Without a second one there is no boundary at
  // all and every domain rule answers yes (rosterOf().single).
  const domains = [
    { name: "shared", writable: true, members: [] },
    { name: "closed", writable: true, members: [] },
  ];


  it("offers every column to a card nothing holds", () => {
    const got = placementTargets(mk({}), {
      projects,
      epics,
      processes: [],
      projectDomains,
      domains,
    });
    expect(got.attach?.map((p) => p.name)).toEqual(["engineering", "strategy"]);
  });

  it("offers a team's card only its own repository", () => {
    const got = placementTargets(mk({ team: "platform" }), {
      projects,
      epics,
      processes: [],
      projectDomains,
      domains,
    });
    expect(got.attach?.map((p) => p.name)).toEqual(["engineering"]);
  });

  // The relaxation is the PROJECT's doing, and the no-project bucket is not
  // a project: a card attached there is placed by nothing, so it stays in
  // the repository it was in. A foreign bucket column is a refusal with a
  // friendly label.
  it("never offers a foreign no-project bucket", () => {
    const withBucket = [...epics, { name: "Inbox", project: "", domain: "founders" }];
    const got = placementTargets(mk({}), {
      projects,
      epics: withBucket,
      processes: [],
      projectDomains,
      domains,
    });
    expect(got.attach?.find((p) => p.name === "")).toBeUndefined();
  });

  it("offers the bucket of the card's own repository", () => {
    const withBucket = [...epics, { name: "Inbox", project: "" }];
    const got = placementTargets(mk({}), {
      projects,
      epics: withBucket,
      processes: [],
      projectDomains,
      domains,
    });
    expect(got.attach?.find((p) => p.name === "")?.epics).toEqual(["Inbox"]);
  });

  // A column of an ALIAS project (G13) is declared in the other half of the
  // merged entry: the project would carry the card to ITS repository, and
  // the column is not in it. The server compares exactly those two.
  it("never offers a column its own project could not hold the card in", () => {
    expect(
      attachTargets(
        ["freedom"],
        [{ name: "Launch", project: "freedom", domain: "closed" }],
        "shared",
        false,
      ),
    ).toEqual([]);
  });
});

// A column's bar counts what the column DRAWS, mirrors included: keying by
// the home pair left a mirror column reporting nothing while showing
// slots, so the header and the columns disagreed on one screen.
describe("columnsOf", () => {
  it("lists the home and every mirror", () => {
    expect(
      columnsOf({
        project: "engineering",
        epic: "Cozystack",
        mirrors: [{ project: "freedom", epic: "Launch" }],
      } as Card),
    ).toEqual([
      { project: "engineering", epic: "Cozystack" },
      { project: "freedom", epic: "Launch" },
    ]);
  });

  it("lists nothing for a card in no column", () => {
    expect(columnsOf(mk({}))).toEqual([]);
  });
});

// The controls that create and un-team a card must not offer a refusal
// either — a card with no team is held by the PRIMARY repository, so a
// column of another one cannot show it.
describe("controls that must match the server", () => {
  // A board of TWO repositories: the primary is named, and so is every
  // entry — the shape the server serves whenever there is a boundary to
  // police.
  const db = { primary: "aeman-db", single: false };

  it("offers No team only where a teamless card can stand", () => {
    expect(teamlessIsLawful("aeman-db", db)).toBe(true);
    expect(teamlessIsLawful("", db)).toBe(true);
    expect(teamlessIsLawful("founders", db)).toBe(false);
    // ...unless a PROJECT holds the card: it stays in that project's
    // repository with no team at all, so the entry is lawful there.
    expect(teamlessIsLawful("founders", db, "strategy")).toBe(true);
  });

  it("opens a new card only where one can be created", () => {
    // The card carries no team, so its PROJECT puts it in its own
    // repository — and the column has to be there too. A project of the
    // closed repository with its own column: lawful.
    const closed = { ...db, projectDomains: { engineering: "founders" } };
    expect(
      canCreateInColumn({ project: "engineering", domain: "founders" }, closed),
    ).toBe(true);
    // The same column under a project of the PRIMARY — the alias case
    // (G13), where one project name holds columns of two repositories:
    // the project would put the card in the primary, and the column is
    // not there. The server returns 422; the "+" does not open.
    expect(canCreateInColumn({ project: "engineering", domain: "founders" }, db)).toBe(
      false,
    );
    // A bucket column cannot hold a teamless card unless it is the
    // primary's own: nothing else places such a card.
    expect(canCreateInColumn({ project: "", domain: "aeman-db" }, db)).toBe(true);
    expect(canCreateInColumn({ project: "", domain: "founders" }, db)).toBe(false);
  });
});

// The no-project bucket is a home the server accepts; a picker that walks
// only the roster's projects could never name it, so the gesture the
// codec, the store and the service were all changed for had no way in.
describe("projectsWithColumns", () => {
  it("adds the bucket when it holds columns", () => {
    expect(
      projectsWithColumns(["engineering"], [
        { name: "Cozystack", project: "engineering" },
        { name: "Inbox", project: "" },
      ]),
    ).toEqual(["engineering", ""]);
  });

  it("leaves it out when nothing is filed there", () => {
    expect(
      projectsWithColumns(["engineering"], [{ name: "Cozystack", project: "engineering" }]),
    ).toEqual(["engineering"]);
  });
});

// The optimistic side of unmirroring, which had no test while its two
// siblings did: the placement must leave the card at once, or the slot it
// draws stays on the grid until the re-list.
describe("makeCardPlacements onUnmirror", () => {
  it("drops the placement from the card at once", () => {
    const patches: Partial<Card>[] = [];
    const card = {
      itemId: "c1",
      project: "engineering",
      epic: "Cozystack",
      mirrors: [
        { project: "freedom", epic: "Launch" },
        { project: "freedom", epic: "Ship" },
      ],
    } as Card;
    makeCardPlacements(
      card,
      { projects: ["engineering", "freedom"], epics: [], processes: [] },
      {
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
      },
    ).onUnmirror("freedom", "Launch");
    expect(patches[0].mirrors).toEqual([{ project: "freedom", epic: "Ship" }]);
  });
});

// A subtask carries at most ONE column (G57), so no drag of its slot can
// ever ask for a second: the grid draws a subtask once, and the plans that
// move or fold a MIRROR cannot arise from it.
describe("dragging a subtask's slot", () => {
  it("never asks for a mirror move", () => {
    const kid = mk({
      parent: "p",
      project: "engineering",
      epic: "Cozystack",
      mirrors: [],
    });
    const grabbed = { project: "engineering", epic: "Cozystack" };
    expect(slotDragPlan(kid, grabbed, { project: "freedom", epic: "Launch" }).kind).toBe(
      "refileHome",
    );
    expect(slotDragPlan(kid, grabbed, grabbed).kind).toBe("dates");
  });
});

// The client's copy of boardservice.columnFollows. A subtask's file rides
// its parent, so a column of the parent's repository is one the card cannot
// keep once it is pulled out — and the × means something else entirely then
// (removal.test.ts). A board that could not ask this question let a
// destructive × through as an ungroup.
describe("columnFollows", () => {
  const epics = [
    { name: "Closed", project: "", domain: "founders" },
    { name: "Cozystack", project: "engineering" },
  ];
  const roster = {
    epics,
    primary: "aeman-db",
    teamDomains: { founders: "founders" },
    projectDomains: { strategy: "founders" },
  };

  it("keeps a column of the repository the card lands in", () => {
    expect(columnFollows({ epic: "Cozystack", project: "engineering" }, roster)).toBe(true);
    expect(columnFollows({ epic: "Closed", project: "", team: "founders" }, roster)).toBe(true);
  });

  it("leaves behind a column of the repository the card is leaving", () => {
    // Team "platform" is unstamped, so the card lands in the primary; the
    // column was declared in founders.
    expect(columnFollows({ epic: "Closed", project: "", team: "platform" }, roster)).toBe(false);
  });

  it("says nothing about a column no roster declares", () => {
    expect(columnFollows({ epic: "Ghost", project: "", team: "platform" }, roster)).toBe(true);
  });

  it("has no column to lose when it stands in none", () => {
    expect(columnFollows({ team: "platform" }, roster)).toBe(true);
  });

  it("follows the LINK when the card still has one", () => {
    // A review card takes its original's repository whatever its own team
    // says (G14), so the column of that repository comes along.
    expect(
      columnFollows({ epic: "Closed", project: "", team: "platform" }, roster, "founders"),
    ).toBe(true);
  });
});

// The shape a SINGLE-REPOSITORY board is served in — the default
// deployment, and the one no test covered: the board names its one
// repository, and every entry in the payload carries that name, because
// the store stamps them all (G59). Read with an empty primary, those
// stamps make one repository look like two, and every rule here then
// refuses what the server accepts: the "+" in the no-project bucket, "No
// team", and the grid's × on a subtask, which the client would send as a
// hard delete while the server ungroups the card and keeps it.
describe("a board of one repository", () => {
  const roster = rosterOf({
    domains: [{ name: "aeman", writable: true, members: [] }],
    projectDomains: { engineering: "aeman" },
  });
  const epics = [
    { name: "Inbox", project: "", domain: "aeman" },
    { name: "Cozystack", project: "engineering", domain: "aeman" },
  ];

  it("has no boundary to police", () => {
    expect(roster.primary).toBe("aeman");
    expect(roster.single).toBe(true);
  });

  it("opens the + in the no-project bucket", () => {
    expect(canCreateInColumn({ project: "", domain: "aeman" }, roster)).toBe(true);
    expect(canCreateInColumn({ project: "engineering", domain: "aeman" }, roster)).toBe(true);
  });

  it("offers No team in the no-project bucket", () => {
    expect(teamlessIsLawful("aeman", roster)).toBe(true);
  });

  it("keeps a subtask's bucket column when it is pulled out of the group", () => {
    // The server's columnFollows answers true here — nothing places the
    // card, so it belongs to the primary, which is where the column is.
    expect(
      columnFollows({ epic: "Inbox", project: "", team: "" }, { ...roster, epics }),
    ).toBe(true);
  });

  // The two rules that reached the payload's stamps without the one
  // comparison: a card the store stamped and a picker reading the roster
  // raw agreed only while both happened to carry the same string.
  it("offers the mirrors and the processes of its own repository", () => {
    const card = mk({ project: "engineering", epic: "Cozystack", mirrors: [] });
    const board = {
      projects: ["engineering"],
      epics,
      processes: [{ name: "Invoicing" }],
      processDomains: { Invoicing: "aeman" },
      projectDomains: { engineering: "aeman" },
      domains: [{ name: "aeman", writable: true, members: [] }],
    };
    // A card with NO stamp of its own — the shape a hand-built board and an
    // older payload both produce — belongs to the primary, which is where
    // both the columns and the process are.
    expect(placementTargets(card, board).mirror).toEqual([
      { name: "", epics: ["Inbox"] },
    ]);
    const chore = mk({ stage: "recurrent" });
    expect(placementTargets(chore, board).processes).toEqual(["Invoicing"]);
  });

  it("offers every column to attach to", () => {
    expect(
      attachTargets(["engineering"], epics, "aeman", true, roster).map((p) => p.name),
    ).toEqual(["engineering", ""]);
  });

  // And a board that names NO repository at all — nothing to compare
  // against, while the entries still carry a name — is the same case: no
  // boundary, so nothing is refused. Read literally, that payload puts
  // "aeman" beside an empty primary and calls one repository two.
  it("has no boundary when the board names no repository at all", () => {
    const unnamed = rosterOf({ projectDomains: { engineering: "aeman" } });
    expect(unnamed.single).toBe(true);
    expect(canCreateInColumn({ project: "", domain: "aeman" }, unnamed)).toBe(true);
    expect(teamlessIsLawful("aeman", unnamed)).toBe(true);
    expect(
      columnFollows({ epic: "Inbox", project: "", team: "" }, { ...unnamed, epics }),
    ).toBe(true);
    expect(
      attachTargets(["engineering"], epics, "aeman", true, unnamed).map((p) => p.name),
    ).toEqual(["engineering", ""]);
  });
});

// A figure that spans columns — the board header's total, a project's line
// — de-duplicates by IDENTITY: the parent is either drawn in it or not.
// Asking about the child's HOME project instead named a project nobody was
// looking at, and a mirrored child was counted beside the parent whose bar
// already derives from it.
describe("countedAmong", () => {
  it("skips a subtask whose parent is drawn in the same figure", () => {
    expect(countedAmong(mk({ parent: "p1" }), new Set(["p1", "c1"]))).toBe(false);
  });

  it("counts one whose parent is not", () => {
    expect(countedAmong(mk({ parent: "p1" }), new Set(["c1"]))).toBe(true);
  });

  it("counts an ordinary card always", () => {
    expect(countedAmong(mk({}), new Set(["c1"]))).toBe(true);
  });
});

// A LINK decides where a card lives ahead of its project (G14), so a
// subtask — or a review card, or an iteration — cannot be carried into
// another repository by a new project. Offering those columns was a
// refusal with a friendly label, exactly as it was for a teamed card.
describe("attach targets for a card a LINK holds", () => {
  const board = {
    projects: ["engineering", "strategy"],
    epics: [
      { name: "Cozystack", project: "engineering", domain: "aeman-db" },
      { name: "Fundraising", project: "strategy", domain: "founders" },
    ],
    processes: [],
    projectDomains: { engineering: "aeman-db", strategy: "founders" },
    domains: [
      { name: "aeman-db", writable: true, members: [] },
      { name: "founders", writable: true, members: [] },
    ],
  };

  it("offers a subtask only its own repository's columns", () => {
    const kid = mk({ parent: "p", domain: "aeman-db" });
    expect(placementTargets(kid, board).attach?.map((p) => p.name)).toEqual(["engineering"]);
  });

  it("offers a review card the same", () => {
    const rev = mk({ reviewOf: "orig", domain: "aeman-db" });
    expect(placementTargets(rev, board).attach?.map((p) => p.name)).toEqual(["engineering"]);
  });

  it("still carries a card nothing holds", () => {
    const loose = mk({ domain: "aeman-db" });
    expect(placementTargets(loose, board).attach?.map((p) => p.name)).toEqual([
      "engineering",
      "strategy",
    ]);
  });
});

// The left stripe says where a card came FROM, and every board that draws one
// asks this — the day boards down the card's edge, Triage on its slot. Two
// boards deciding it apart is how one of them came to draw nothing at all
// after the weekly plan, whose band the stripe used to be, was taken out.
describe("markOf", () => {
  it("marks a project's work, a process's turn, and neither", () => {
    expect(markOf({ epic: "Auth" })).toBe("project");
    expect(markOf({ task: "t1" })).toBe("process");
    expect(markOf({})).toBe("");
  });

  // A review card of a project card is BOTH; the waiting is the innermost
  // thing about it, so that is what the stripe says.
  it("says review where a card is both", () => {
    expect(markOf({ reviewOf: "c1", epic: "Auth" })).toBe("review");
  });
});
