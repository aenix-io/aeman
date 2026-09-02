import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { TriageBoard } from "./TriageBoard";
import type { Board, Card, Provider } from "../providers/types";

// Triage reads down a person's column: every card is a plain box of one week,
// they stand one under the next at the full column width, and the week grows
// to hold them. The first row is NOW, and it carries everything the team has
// — this week's work and everything nobody has given a week. This is the one
// test that proves the tree comes out that way: markup only, so what it pins
// is the shape, not the gestures.

// 2026-08-31 is a Monday, and the grid opens on the week holding today.
const store = new Map<string, string>();
globalThis.localStorage = {
  getItem: (k: string) => store.get(k) ?? null,
  setItem: (k: string, v: string) => void store.set(k, v),
  removeItem: (k: string) => void store.delete(k),
  clear: () => store.clear(),
  key: () => null,
  get length() {
    return store.size;
  },
} as Storage;

const week = "2026-08-31";

const card = (over: Partial<Card> = {}): Card =>
  ({
    itemId: "c1",
    title: "A card of one week",
    assignees: ["lexfrei"],
    stage: "",
    team: "core",
    week,
    ...over,
  }) as Card;

const board = (cards: Card[]): Board =>
  ({
    title: "test",
    url: "",
    cards,
    teams: ["core"],
    projects: [],
    epics: [],
    deadlines: [],
    processes: [],
    members: [],
    domains: [],
    sprintStates: {},
  }) as unknown as Board;

function draw(cards: Card[]) {
  return renderToStaticMarkup(
    <TriageBoard
      board={board(cards)}
      provider={{} as Provider}
      roster={["core"]}
      teamFilter={["core"]}
      onSetFilter={vi.fn()}
      avatars={{}}
      names={{}}
      patchCard={vi.fn()}
      addCard={vi.fn()}
      reorderCards={vi.fn()}
      onOpen={vi.fn()}
      onError={vi.fn()}
    />,
  );
}

describe("the Triage board", () => {
  it("opens on this week — what was owed before is owed now", () => {
    const html = draw([card()]);
    // No past rows at all, and no button offering any.
    expect(html).toContain("grid-row:2;grid-column:1");
    expect(html).toContain(">now<");
    expect(html).not.toContain("earlier weeks");
  });

  it("lets a week's rows grow, so the cards in it need not be sliced", () => {
    const html = draw([card()]);
    expect(html).toContain("minmax(28px, auto)");
  });

  it("stands a week's cards one under the next, each the same height", () => {
    const html = draw([
      card(),
      card({ itemId: "c2", title: "Under it" }),
      card({ itemId: "c3", title: "And under that" }),
    ]);
    expect(html).toContain("grid-row:2;height:24px;margin-top:0px");
    expect(html).toContain("grid-row:2;height:24px;margin-top:28px");
    expect(html).toContain("grid-row:2;height:24px;margin-top:56px");
    // The column is never sliced: there is nothing to stand beside.
    expect(html).not.toContain("margin-left");
  });

  it("draws a card of two weeks as two cards, each saying which it is", () => {
    const html = draw([card({ title: "Two weeks of work", day: "2026-09-11" })]);
    expect(html).toContain("(1/2)");
    expect(html).toContain("(2/2)");
    // One in this week's row and one in the next, each a box of one row: no
    // card on this board is ever a rectangle across several of them.
    expect(html).toContain("grid-row:2;height:24px;margin-top:0px");
    expect(html).toContain("grid-row:3;height:24px;margin-top:0px");
    expect(html).not.toMatch(/class="project-slot[^"]*" style="[^"]*span/);
  });

  it("stripes a card down its left in the colour of the zone it stands in", () => {
    expect(draw([card({ zone: "red" })])).toContain("triage-slot-zone-red");
    expect(draw([card({ zone: "green" })])).toContain("triage-slot-zone-green");
  });

  it("says what a card belongs to — the column here is a person, not an epic", () => {
    const html = draw([card({ epic: "Storage", project: "cozy" })]);
    expect(html).toContain("triage-slot-chip");
    expect(html).toContain(">Storage<");
    expect(html).toContain('title="cozy · Storage"');
  });

  it("names the project when a card is on no column of it", () => {
    expect(draw([card({ project: "cozy" })])).toContain(">cozy<");
  });

  it("says nothing of belonging for work the board holds alone", () => {
    // The zone stripe and the week are its whole address.
    expect(draw([card({ zone: "gray" })])).not.toContain("triage-slot-chip");
  });

  it("stacks a week in the order somebody triaging wants to meet it", () => {
    // Debts first, then the project's own work, then the zones.
    const html = draw([
      card({ itemId: "a", title: "If time left", zone: "green" }),
      card({ itemId: "b", title: "Planned", zone: "gray" }),
      card({ itemId: "c", title: "A slot", epic: "Storage" }),
      card({ itemId: "d", title: "Urgent", zone: "red" }),
      card({ itemId: "e", title: "A debt", zone: "green", overdue: true }),
    ]);
    const order = ["A debt", "A slot", "Urgent", "Planned", "If time left"];
    expect(order.map((t) => html.indexOf(t))).toEqual(
      [...order.map((t) => html.indexOf(t))].sort((x, y) => x - y),
    );
    // …and they stand under one another, in that order.
    expect(html).toContain("margin-top:0px");
    expect(html).toContain("margin-top:112px");
  });

  it("colours the progress bar by stage, as Team and Me do", () => {
    // A card locked or in review says so by its bar on the other boards; it
    // must not say something else here.
    expect(draw([card({ stage: "locked", progress: 30 })])).toContain(
      "background:var(--stage-locked)",
    );
    expect(draw([card({ stage: "review", progress: 85 })])).toContain(
      "background:var(--stage-review)",
    );
    expect(draw([card({ progress: 40 })])).toContain("background:var(--bar-default)");
  });

  it("leaves a card of no zone the plain edge", () => {
    expect(draw([card()])).not.toContain("triage-slot-zone");
  });

  it("keeps the debt's own red over the zone's", () => {
    // Urgent wears the lighter red and a debt the strong one; a card that is
    // both must read as the debt, so the late class comes last in the sheet.
    const html = draw([card({ zone: "red", overdue: true })]);
    expect(html).toContain("project-slot-late");
    expect(html).toContain("triage-slot-zone-red");
  });

  it("fills in nothing while nothing is being carried", () => {
    // The gap between the boxes of one card is filled in only while the card
    // is in the reader's hand; a board at rest has nothing to say about it.
    // And the cell under a carried card is never tinted at all: the card is
    // already drawn where it would land, which says it once.
    const html = draw([card({ title: "Two weeks of work", day: "2026-09-11" })]);
    expect(html).not.toContain("triage-span");
    expect(html).not.toContain("project-cell-drag");
  });

  it("counts a card of two weeks against both of them", () => {
    const html = draw([card({ day: "2026-09-11" })]);
    // The week's own count, beside its date, in the first two rows.
    expect(html.match(/class="triage-count">1</g)?.length).toBe(2);
  });

  it("says nothing of parts for a card that takes a single week", () => {
    expect(draw([card()])).not.toContain("triage-slot-part");
  });

  it("draws a card just placed, whatever start date it carried in", () => {
    // A card waiting in the strip usually carries dates from the day board.
    // The row here is the week TRIAGE gave it and nothing else: letting an
    // old start date win drew the card nowhere at all, which is what a card
    // dragged out of the strip looked like.
    const html = draw([card({ startDate: "2026-06-01", day: "2026-06-05" })]);
    expect(html).toContain("A card of one week");
    expect(html).toContain("grid-row:2;height:24px;margin-top:0px");
  });

  it("does not drag the window back to a start date nobody is looking at", () => {
    const html = draw([card({ startDate: "2026-06-01" })]);
    // Still this week first, and the nine rows this board always opens with.
    expect(html).toContain(">now<");
    expect(html.match(/project-week[ "]/g)?.length).toBe(9);
  });

  it("stands what nobody has given a week in the first row, with this week's work", () => {
    const html = draw([
      card({ title: "Planned for this week" }),
      card({ itemId: "c2", week: undefined, title: "Nobody has dated it" }),
    ]);
    // Both in row 2 — the grid's first row, which is now — one under the
    // other, and drawn exactly alike: the row is what the team is carrying.
    expect(html).toContain("grid-row:2;height:24px;margin-top:0px");
    expect(html).toContain("grid-row:2;height:24px;margin-top:28px");
    expect(html).toContain("Nobody has dated it");
    // No pile beside the grid any more.
    expect(html).not.toContain("triage-strip");
  });

  it("counts what is waiting, so the size of the pile is not a surprise", () => {
    const html = draw([card({ week: undefined }), card({ itemId: "c2", week: undefined })]);
    expect(html).toContain("2 untriaged");
  });

  it("stands a card of no week in its owner's column", () => {
    const html = draw([card({ week: undefined, assignees: ["lexfrei"] })]);
    // Column 3: the week labels, then Unassigned, then lexfrei.
    expect(html).toContain("grid-column:3");
  });

  it("draws no review of its own — it follows the card it reviews", () => {
    const html = draw([card({ week: undefined, title: "Review of it", reviewOf: "c9" })]);
    expect(html).not.toContain("Review of it");
    expect(html).toContain("0 untriaged");
  });

  it("keeps a review with a week on the board, where the work is", () => {
    const html = draw([card({ title: "Review of it", reviewOf: "c9" })]);
    expect(html).toContain("Review of it");
  });

  it("draws no card sent to review — it waits on a reviewer, not on a week", () => {
    const html = draw([
      card({ week: undefined, title: "Sent to review", stage: "review", progress: 85 }),
    ]);
    expect(html).not.toContain("Sent to review");
    expect(html).toContain("0 untriaged");
  });

  it("keeps a card sent to review on the board once it has a week", () => {
    const html = draw([card({ title: "Sent to review", stage: "review", progress: 85 })]);
    expect(html).toContain("Sent to review");
  });

  it("gives a column to every person, with the unassigned first", () => {
    const html = draw([card(), card({ itemId: "c2", assignees: [] })]);
    expect(html).toContain("Unassigned");
    expect(html.match(/project-epic-head triage-person/g)?.length).toBe(2);
    // Unassigned stands in the first column.
    expect(html.indexOf("Unassigned")).toBeLessThan(html.indexOf("lexfrei"));
  });
});
