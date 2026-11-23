import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

import { adjacentRuns, relatedRuns, blockAnchor, blockAnchors, reassignable, takeable, takeQuestion, arrangeByParent, expandRows, groupedRows, isPlanRow, parentGroupOf, planDrop, planRowId, planRowUid } from "./grouping";
import type { Card, LineageRef } from "./providers/types";

const goal: LineageRef = { uid: "g", type: "goal", title: "Cozystack" };
const initiative: LineageRef = { uid: "i", type: "initiative", title: "Фичи" };
const epic: LineageRef = { uid: "e", type: "epic", title: "Версионирование" };
const other: LineageRef = { uid: "o", type: "epic", title: "Бэкапы" };

const card = (itemId: string, over: Partial<Card> = {}): Card =>
  ({ itemId, title: itemId, assignees: [], progress: 0, ...over }) as Card;
const under = (itemId: string, lineage: LineageRef[], over: Partial<Card> = {}) =>
  card(itemId, { parent: lineage[lineage.length - 1]?.uid, lineage, ...over });

// "Group by plan" on the Team and Me boards: a task is drawn under the plan
// item it stands in — its DIRECT parent, whichever level that is — and the
// levels above are only named, never drawn. An epic inside an initiative and
// the initiative's own tasks are two groups side by side.
describe("parentGroupOf", () => {
  it("is the epic a task stands in, naming the plan item above it", () => {
    expect(parentGroupOf(under("t", [goal, initiative, epic]))).toEqual({
      uid: "e",
      type: "epic",
      title: "Версионирование",
      above: "Фичи",
    });
  });

  it("is the initiative for a task standing straight in it, beside its epics", () => {
    expect(parentGroupOf(under("t", [goal, initiative]))).toEqual({
      uid: "i",
      type: "initiative",
      title: "Фичи",
      above: "Cozystack",
    });
  });

  it("is a goal for a task standing straight in a goal, with nothing above", () => {
    expect(parentGroupOf(under("t", [goal]))).toEqual({ uid: "g", type: "goal", title: "Cozystack" });
  });

  it("says whose the plan item is: its team and its owner", () => {
    const owned: LineageRef = { ...epic, team: "cozystack", owner: "lexfrei" };
    expect(parentGroupOf(under("t", [owned]))).toMatchObject({ team: "cozystack", owner: "lexfrei" });
  });

  it("is nothing for a task in no plan: it stays a card of its own", () => {
    expect(parentGroupOf(card("t"))).toBeNull();
  });

  it("puts a review card with the task it reviews: in that task's plan item", () => {
    // A review sits under nothing of its own (H2); its lineage is the
    // reviewed task's, which may end at the task itself when that is a
    // subtask — the plan item is the nearest one with a level.
    const subtaskLine: LineageRef[] = [goal, epic, { uid: "task", title: "Parent task" }];
    expect(parentGroupOf(card("r", { reviewOf: "x", lineage: subtaskLine }))?.uid).toBe("e");
    expect(parentGroupOf(card("r", { reviewOf: "x", lineage: [goal, epic] }))?.uid).toBe("e");
  });
});

describe("arrangeByParent", () => {
  const ids = (cs: Card[]) => cs.map((c) => c.itemId);

  it("keeps the cell's order, gathering each group where its first card stood", () => {
    const cs = [
      under("a1", [goal, initiative, epic]),
      card("loose"),
      under("b1", [other]),
      under("a2", [goal, initiative, epic]),
      under("i1", [goal, initiative]),
    ];
    const { cards, heads, member } = arrangeByParent(cs);
    expect(ids(cards)).toEqual(["a1", "a2", "loose", "b1", "i1"]);
    expect([...heads.keys()]).toEqual(["a1", "loose", "b1", "i1"]);
    expect(heads.get("a1")).toMatchObject({ uid: "e", count: 2, done: 0, reviewOnly: false });
    expect(member.has("a2")).toBe(true);
  });

  it("counts the finished work of a group", () => {
    const { heads } = arrangeByParent([
      under("a", [epic], { progress: 100 }),
      under("b", [epic], { stage: "done", progress: 100 }),
      under("c", [epic]),
    ]);
    expect(heads.get("a")).toMatchObject({ count: 3, done: 2 });
  });

  it("marks a group of review cards only as a review — one task among them is not", () => {
    const reviews = arrangeByParent([
      card("r1", { reviewOf: "x", lineage: [epic] }),
      card("r2", { reviewOf: "y", lineage: [epic] }),
    ]);
    expect(reviews.heads.get("r1")?.reviewOnly).toBe(true);
    const mixed = arrangeByParent([card("r1", { reviewOf: "x", lineage: [epic] }), under("t", [epic])]);
    expect(mixed.heads.get("r1")?.reviewOnly).toBe(false);
  });

  it("draws a group of one as a group: the view does not flip between shapes", () => {
    const { heads } = arrangeByParent([under("a", [epic])]);
    expect(heads.get("a")).toMatchObject({ count: 1 });
  });

  it("gathers the cards in no plan as one more group, Ungrouped", () => {
    const cs = [card("x"), under("a", [epic]), card("y")];
    const { cards, heads, member } = arrangeByParent(cs);
    expect(ids(cards)).toEqual(["x", "y", "a"]);
    expect(heads.get("x")).toMatchObject({ uid: "", type: "none", title: "Ungrouped", count: 2 });
    expect(member.has("y")).toBe(true);
  });
});

// The cell as the board draws it grouped: each group's head is a row the
// cards nest under — folded by default, so a cell reads as its plan items
// first — and a drop between groups changes the task's plan item.
describe("groupedRows", () => {
  const ids = (cs: Card[]) => cs.map((c) => c.itemId);
  const cs = [under("a1", [goal, epic]), card("loose"), under("a2", [goal, epic]), under("b1", [other])];
  const headA = planRowId("cell", "e");
  const headB = planRowId("cell", "o");

  const loose = planRowId("cell", "");

  it("folds every group by default: the heads alone, the cards in no plan under Ungrouped", () => {
    const { rows, parentOf } = groupedRows("cell", cs, () => false);
    expect(ids(rows)).toEqual([headA, loose, headB]);
    expect(rows.filter((r) => isPlanRow(r.itemId)).map((r) => r.title)).toEqual(["Версионирование", "Ungrouped", "Бэкапы"]);
    expect(parentOf.size).toBe(0);
  });

  it("unfolds a group under its head, its cards nested under the head row", () => {
    const { rows, parentOf } = groupedRows("cell", cs, (id) => id === headA);
    expect(ids(rows)).toEqual([headA, "a1", "a2", loose, headB]);
    expect(parentOf.get("a1")).toBe(headA);
    expect(parentOf.has("loose")).toBe(false);
    expect(parentOf.has("b1")).toBe(false);
  });

  it("gives one plan item a head of its own in every cell, and reads the plan item back", () => {
    expect(planRowId("ivan::gray", "e")).not.toBe(planRowId("dan::gray", "e"));
    expect(planRowUid(planRowId("ivan::gray", "01M6KKQPS6S3GP6C8GDN0ZHWEZ"))).toBe("01M6KKQPS6S3GP6C8GDN0ZHWEZ");
  });

  it("keeps the lineage down to the plan item: what a card dropped into the group takes", () => {
    const { heads } = groupedRows("cell", [under("t", [goal, initiative, epic])], () => true);
    expect(heads.get(headA)?.lineage).toEqual([goal, initiative, epic]);
  });
});

describe("planDrop", () => {
  const { heads } = groupedRows("cell", [under("a", [goal, epic]), under("b", [other])], () => true);
  const headA = planRowId("cell", "e");
  const headB = planRowId("cell", "o");

  it("moves a task into the group it was dropped in", () => {
    expect(planDrop(under("b", [other]), headA, heads)).toEqual({ parent: "e", lineage: [goal, epic] });
  });

  it("changes nothing for a drop inside its own group", () => {
    expect(planDrop(under("a", [goal, epic]), headA, heads)).toBeNull();
  });

  it("takes a task out of its plan item when dropped into Ungrouped", () => {
    const withLoose = groupedRows("cell", [under("b", [other]), card("x")], () => true).heads;
    expect(planDrop(under("b", [other]), planRowId("cell", ""), withLoose)).toEqual({ parent: "", lineage: [] });
    expect(planDrop(card("x"), planRowId("cell", ""), withLoose)).toBeNull();
  });

  it("takes a task out of its plan item when dropped out of every group", () => {
    expect(planDrop(under("b", [other]), null, heads)).toEqual({ parent: "", lineage: [] });
  });

  it("puts a loose task into a plan item, and leaves a loose one loose", () => {
    expect(planDrop(card("x"), headB, heads)?.parent).toBe("o");
    expect(planDrop(card("x"), null, heads)).toBeNull();
  });

  it("never moves a review card: its place is its task's", () => {
    expect(planDrop(card("r", { reviewOf: "x", lineage: [epic] }), headB, heads)).toBeNull();
  });
});

describe("expandRows", () => {
  const cs = [under("a1", [goal, epic]), card("loose"), under("a2", [goal, epic]), under("b1", [other])];
  const headA = planRowId("cell", "e");
  const headB = planRowId("cell", "o");

  it("puts a folded group's cards where its head stands", () => {
    const { rows, heads } = groupedRows("cell", cs, () => false);
    expect(expandRows(rows.map((r) => r.itemId), heads)).toEqual(["a1", "a2", "loose", "b1"]);
  });

  it("leaves an unfolded group's cards where they are drawn", () => {
    const { rows, heads } = groupedRows("cell", cs, (id) => id === headA);
    expect(expandRows(rows.map((r) => r.itemId), heads)).toEqual(["a1", "a2", "loose", "b1"]);
  });

  it("carries a group moved by its head: its cards follow the head to its new place", () => {
    const { heads } = groupedRows("cell", cs, () => false);
    expect(expandRows([planRowId("cell", ""), headB, headA], heads)).toEqual(["loose", "b1", "a1", "a2"]);
  });
});

// The Backlog is one queue: a group there is the cards of one plan item
// STANDING TOGETHER. A card of another plan item between them splits the plan
// item into two groups; brought together again they are one.
describe("adjacentRuns", () => {
  const ids = (runs: ReturnType<typeof adjacentRuns>) => runs.map((r) => [r.head.uid, r.cards.map((c) => c.itemId)]);

  it("groups neighbours of one plan item, in the list's order", () => {
    const cs = [under("a1", [epic]), under("a2", [epic]), under("b1", [other]), card("x")];
    expect(ids(adjacentRuns(cs))).toEqual([
      ["e", ["a1", "a2"]],
      ["o", ["b1"]],
      ["", ["x"]],
    ]);
  });

  it("splits a plan item into two groups when another plan item stands between", () => {
    const cs = [under("a1", [epic]), under("b1", [other]), under("a2", [epic])];
    const runs = adjacentRuns(cs);
    expect(ids(runs)).toEqual([
      ["e", ["a1"]],
      ["o", ["b1"]],
      ["e", ["a2"]],
    ]);
    // Two heads of one plan item are two rows of their own.
    expect(runs[0].id).not.toBe(runs[2].id);
  });

  it("joins them again once they stand together", () => {
    const cs = [under("b1", [other]), under("a1", [epic]), under("a2", [epic])];
    expect(ids(adjacentRuns(cs))).toEqual([
      ["o", ["b1"]],
      ["e", ["a1", "a2"]],
    ]);
  });

  it("runs the loose cards under Ungrouped, and a review with its task's plan item", () => {
    const cs = [card("r", { reviewOf: "x", lineage: [epic] }), under("a1", [epic]), card("x1"), card("x2")];
    const runs = adjacentRuns(cs);
    expect(ids(runs)).toEqual([
      ["e", ["r", "a1"]],
      ["", ["x1", "x2"]],
    ]);
    expect(runs[1].head).toMatchObject({ type: "none", title: "Ungrouped", count: 2 });
  });

  it("marks a run of reviews only as a review", () => {
    const runs = adjacentRuns([card("r1", { reviewOf: "x", lineage: [epic] }), card("r2", { reviewOf: "y", lineage: [epic] })]);
    expect(runs[0].head.reviewOnly).toBe(true);
  });

  it("is nothing for an empty list", () => {
    expect(adjacentRuns([])).toEqual([]);
  });
});

// THE PLANS READING IS ONE RULE ON BOTH SIDES (ADR 0002): what the boards
// draw here, a listing asked with group=plans carries as its groups
// (apiserver.PlanGroups, TestPlanGroupsMatchTheSharedFixture), on the same
// fixture.
describe("the Plans reading, shared with the server", () => {
  type Fx = {
    cards: { uid: string; zone: string; assignees: string[]; reviewOf?: string; subtask?: boolean; lineage: LineageRef[] }[];
    me: { zone: string; plan: string; cards: string[] }[];
    team: { assignee: string; zone: string; plan: string; cards: string[] }[];
    backlog: { plan: string; cards: string[] }[];
  };
  const fx = JSON.parse(
    readFileSync(fileURLToPath(new URL("../../pkg/board/testdata/plan_groups.json", import.meta.url)), "utf8"),
  ) as Fx;
  const zones = ["urgent", "unplanned", "planned", "niceToHave"];
  const cards = fx.cards
    .filter((c) => !c.subtask)
    .map((c) => ({ ...card(c.uid, { assignees: c.assignees, reviewOf: c.reviewOf, lineage: c.lineage }), zoneName: c.zone }));
  // A cell's groups: each head, with the cards drawn under it.
  const groupsOf = (cs: typeof cards, zone: string) => {
    const { cards: drawn, heads } = arrangeByParent(cs.filter((c) => c.zoneName === zone));
    return [...heads.values()].map((h) => ({
      plan: h.uid,
      cards: drawn.filter((c) => (parentGroupOf(c)?.uid ?? "") === h.uid).map((c) => c.itemId),
    }));
  };

  it("groups the Me board per band", () => {
    const got = zones.flatMap((z) => groupsOf(cards, z).map((g) => ({ zone: z, ...g })));
    expect(got).toEqual(fx.me);
  });

  it("groups the Team board per person and band", () => {
    const people: string[] = [];
    for (const c of cards) {
      for (const p of c.assignees.length ? c.assignees : [""]) {
        if (!people.includes(p)) {
          people.push(p);
        }
      }
    }
    const got = people.flatMap((p) => {
      const mine = cards.filter((c) => (c.assignees.length ? c.assignees : [""]).includes(p));
      return zones.flatMap((z) => groupsOf(mine, z).map((g) => ({ assignee: p, zone: z, ...g })));
    });
    expect(got).toEqual(fx.team);
  });

  it("cuts the Backlog into runs of neighbours", () => {
    expect(adjacentRuns(cards).map((r) => ({ plan: r.head.uid, cards: r.cards.map((c) => c.itemId) }))).toEqual(fx.backlog);
  });
});

// The head's avatar hands the group's WORK over — a team or a person for
// its tasks in that cell — and leaves the plan item's own owner alone. What
// it moves: the open tasks; a finished one keeps who did it, and a review
// card is the reviewer's, which the review's own menu changes.
describe("reassignable", () => {
  it("takes the open tasks and leaves the done ones and the reviews", () => {
    const open = card("o", {});
    const done = card("d", { progress: 100 });
    const review = card("r", { reviewOf: "o" });
    expect(reassignable([open, done, review]).map((c) => c.itemId)).toEqual(["o"]);
  });
  it("is empty for a group of reviews only", () => {
    expect(reassignable([card("r", { reviewOf: "x" })])).toEqual([]);
  });
});

// Where a moved group stands, as ONE anchor for the whole block (S21): its
// cell's card in front of it, else the cell's card behind it, else — alone in
// its cell — the board's order around it.
describe("blockAnchor", () => {
  const order = ["x", "a", "b", "y"];
  it("anchors behind the cell's card in front of the block", () => {
    expect(blockAnchor(["p", "a", "b", "q"], ["a", "b"], order)).toEqual({ after: "p" });
  });
  it("anchors before the cell's card behind the block when it leads the cell", () => {
    expect(blockAnchor(["a", "b", "q"], ["a", "b"], order)).toEqual({ before: "q" });
  });
  it("falls back to the board's order when the block is the whole cell", () => {
    expect(blockAnchor(["a", "b"], ["a", "b"], order)).toEqual({ after: "x" });
    expect(blockAnchor(["x", "y"], ["x", "y"], ["x", "y"])).toEqual({ after: null });
  });
});

// What a Backlog run's Take takes (K11): every open card of the run nobody
// has — a review nobody has taken included, which Take makes the viewer's
// review as on its own row; a finished card and one somebody has stay.
describe("takeable", () => {
  it("takes the open cards nobody has, reviews included", () => {
    const open = card("o", {});
    const review = card("r", { reviewOf: "x" });
    const done = card("d", { progress: 100 });
    const held = card("h", { assignees: ["ann"] });
    expect(takeable([open, review, done, held]).map((c) => c.itemId)).toEqual(["o", "r"]);
  });
  it("offers Take on a run of reviews only", () => {
    expect(takeable([card("r", { reviewOf: "x" })]).length).toBe(1);
  });
});

// What a run's Take asks before it takes (K11): on every head with something
// to take — Ungrouped included, whose cards stand in no plan item and say so
// rather than naming "Ungrouped" as if it were one — and on none without.
describe("takeQuestion", () => {
  const run = (uid: string, title: string, cards: ReturnType<typeof card>[], reviewOnly = false) =>
    ({ id: `plan:backlog:${uid}:x`, head: { uid, type: uid ? "epic" : "none", title, count: cards.length, done: 0, reviewOnly }, cards }) as never;
  it("asks on a plan item's run", () => {
    expect(takeQuestion(run("e", "Billing", [card("a", {})]))?.text).toBe(
      "The open cards of «Billing» here that nobody has become yours, today, and leave the backlog.",
    );
  });
  it("asks on Ungrouped too, naming no plan item", () => {
    expect(takeQuestion(run("", "Ungrouped", [card("a", {})]))?.text).toBe(
      "The open cards here in no plan item that nobody has become yours, today, and leave the backlog.",
    );
    expect(takeQuestion(run("", "Ungrouped", [card("r", { reviewOf: "x" })], true))).toEqual({
      heading: "Review these yourself?",
      text: "The reviews here of tasks in no plan item that nobody has taken become yours, today.",
    });
  });
  it("asks nothing where nothing is to take", () => {
    expect(takeQuestion(run("", "Ungrouped", [card("d", { progress: 100 })]))).toBeNull();
  });
});

// A plan item cut into several runs on the Backlog: selecting one of them
// lights the others up, so the reader sees where the rest of that plan item
// waits. Ungrouped is no plan item, and nothing selected lights nothing.
describe("relatedRuns", () => {
  const e = { uid: "e", type: "epic", title: "E" } as LineageRef;
  const i = { uid: "i", type: "initiative", title: "I" } as LineageRef;
  const cards = [
    card("a1", { lineage: [e] }),
    card("x1", {}),
    card("b1", { lineage: [i] }),
    card("a2", { lineage: [e] }),
    card("x2", {}),
  ];
  const runs = adjacentRuns(cards);
  const id = (first: string) => runs.find((r) => r.cards[0].itemId === first)!.id;
  it("lights the other runs of the selected run's plan item", () => {
    expect([...relatedRuns(runs, id("a1"))]).toEqual([id("a2")]);
    expect([...relatedRuns(runs, id("b1"))]).toEqual([]);
  });
  it("lights nothing for Ungrouped, or with nothing selected", () => {
    expect([...relatedRuns(runs, id("x1"))]).toEqual([]);
    expect([...relatedRuns(runs, null)]).toEqual([]);
  });
});

// Where each card of a moved run stands, for the board's own redraw: the
// block's one anchor outside it (blockAnchor) for the first, each next right
// behind the one before — the same order the server writes (S21). A run
// dropped at the very top must not be anchored before its own second card,
// which redrew it where it was while the server had moved it.
describe("blockAnchors", () => {
  it("anchors a run dropped at the top before the first card outside it", () => {
    expect(blockAnchors(["a", "b", "x", "y"], ["a", "b"])).toEqual([
      { id: "a", anchor: { before: "x" } },
      { id: "b", anchor: { after: "a" } },
    ]);
  });
  it("anchors a run dropped lower behind the card in front of it", () => {
    expect(blockAnchors(["x", "a", "b", "y"], ["a", "b"])).toEqual([
      { id: "a", anchor: { after: "x" } },
      { id: "b", anchor: { after: "a" } },
    ]);
  });
  it("leaves a run that is the whole list where it is", () => {
    expect(blockAnchors(["a", "b"], ["a", "b"])).toEqual([
      { id: "a", anchor: null },
      { id: "b", anchor: { after: "a" } },
    ]);
  });
});
