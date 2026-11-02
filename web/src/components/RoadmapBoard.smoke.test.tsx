import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { RoadmapBoard } from "./RoadmapBoard";
import type { Board, Card, Provider } from "../providers/types";
import { addDays, todayIso } from "../date";

// The Roadmap draws its own two panes — a table of rows and a timeline of
// bars — and nothing else in the suite renders them. This is the one test
// that proves the tree comes out: the header, a row per card at its depth,
// the status pills, a bar per dated card and the today line. Markup only:
// no effect runs, so what it pins is the shape, not the gestures.

// The board reads the reader's depth, zoom and folds as it mounts. There is
// no browser here, so it gets an empty store and falls back to its defaults
// — initiative to task, three months, everything unfolded.
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

const today = todayIso();

const card = (over: Partial<Card>): Card =>
  ({ itemId: "c", title: "a card", assignees: [], progress: 0, ...over }) as Card;

const goal = card({ itemId: "g", title: "Grow revenue", type: "goal", startDate: addDays(today, -30), day: addDays(today, 120) });
const ini = card({
  itemId: "i", title: "Billing v2", type: "initiative", parent: "g",
  startDate: addDays(today, -10), day: addDays(today, 40),
  hierarchy: { status: "in-progress", done: 1, total: 2 },
  lineage: [{ uid: "g", type: "goal", title: "Grow revenue" }],
});
const epic = card({
  itemId: "e", title: "Invoices", type: "epic", parent: "i",
  startDate: addDays(today, -5), day: addDays(today, 20),
  hierarchy: { status: "in-progress", done: 1, total: 2 },
  lineage: [{ uid: "g", type: "goal", title: "Grow revenue" }, { uid: "i", type: "initiative", title: "Billing v2" }],
});
const done = card({
  itemId: "t1", title: "Render PDF", parent: "e", progress: 100, team: "core", assignees: ["ann"],
  startDate: addDays(today, -5), day: addDays(today, -1),
  lineage: [
    { uid: "g", type: "goal", title: "Grow revenue" },
    { uid: "i", type: "initiative", title: "Billing v2" },
    { uid: "e", type: "epic", title: "Invoices" },
  ],
});
const open = card({
  itemId: "t2", title: "Send by mail", parent: "e", progress: 0, team: "core",
  lineage: [
    { uid: "g", type: "goal", title: "Grow revenue" },
    { uid: "i", type: "initiative", title: "Billing v2" },
    { uid: "e", type: "epic", title: "Invoices" },
  ],
});

const board = (cards: Card[]): Board =>
  ({
    title: "test",
    url: "",
    cards,
    teams: ["core"],
    projects: [],
    scopes: [],
    deadlines: [],
    members: [],
    domains: [],
    sprintStates: {},
  }) as unknown as Board;

const provider = {} as Provider;

function draw(b: Board, focusId: string | null = null) {
  return renderToStaticMarkup(
    <RoadmapBoard
      board={b}
      provider={provider}
      roster={["core"]}
      patchCard={vi.fn()}
      addCard={vi.fn()}
      replaceCard={vi.fn()}
      removeCard={vi.fn()}
      reload={vi.fn()}
      onError={vi.fn()}
      onOpen={vi.fn()}
      focusId={focusId}
    />,
  );
}

describe("the Roadmap's two panes", () => {
  it("draws the header, and a row per card from the initiative down to the tasks", () => {
    const html = draw(board([goal, ini, epic, done, open]));
    expect(html).toContain("roadmap-head");
    // Initiative to task by default: the goal is above the range and its
    // initiative stands in its place; the two tasks are the epic's rows.
    expect(html.match(/class="roadmap-row"/g)?.length).toBe(4);
    expect(html).toContain("Billing v2");
    expect(html).toContain("Invoices");
    expect(html).toContain("Render PDF");
    expect(html).toContain("Send by mail");
    expect(html).not.toContain("kind-icon-goal"); // the goal is above the range
    // Depth is the indent: the epic one step in, its tasks two.
    expect(html).toContain('style="padding-left:24px"');
    expect(html).toContain('style="padding-left:42px"');
  });

  it("wears the server's standing on a planning card and a task's own on a task", () => {
    const html = draw(board([goal, ini, epic, done, open]));
    expect(html).toContain("roadmap-status roadmap-status-in-progress");
    expect(html).toContain("roadmap-status roadmap-status-done");
    expect(html).toContain("roadmap-status roadmap-status-todo");
    expect(html).toContain(">1/2<");
  });

  it("draws a bar for every dated card, a done one distinct, and the today line", () => {
    const html = draw(board([goal, ini, epic, done, open]));
    expect(html.match(/class="roadmap-bar /g)?.length).toBe(3); // initiative, epic, the done task
    expect(html).toContain("roadmap-bar-done");
    expect(html).toContain("roadmap-bar-initiative");
    expect(html).toContain("roadmap-today");
    expect(html.match(/class="roadmap-lane"/g)?.length).toBe(4);
  });

  it("says so when there is nothing to draw", () => {
    const html = draw(board([]));
    expect(html).toContain("roadmap-empty");
    expect(html).not.toContain("roadmap-body");
  });

  it("offers the + and the x on a planning row, and neither on a task", () => {
    const html = draw(board([goal, ini, epic, done, open]));
    expect(html).toContain('aria-label="Add under Billing v2"');
    expect(html).toContain('aria-label="Add under Invoices"'); // an epic adds a task
    expect(html).toContain('aria-label="Delete Invoices"');
    expect(html).not.toContain('aria-label="Delete Render PDF"');
  });

  it("draws the tree as a tree: a pictogram per kind, a chevron on a row with rows under it, guides by depth", () => {
    const html = draw(board([goal, ini, epic, done, open]));
    expect(html).toContain("kind-icon-initiative");
    expect(html).toContain("kind-icon-epic");
    expect(html).toContain("kind-icon-task");
    expect(html).toContain('class="roadmap-toggle roadmap-toggle-open"'); // the chevron, open, on a row with rows under it
    expect(html).toContain('class="roadmap-toggle-spacer"'); // and a spacer on a leaf
    expect(html.match(/roadmap-guide/g)?.length).toBe(1 + 2 + 2); // the epic one deep, its two tasks two deep
  });

  it("gives a leaf no button in the chevron's place: a spacer, not something that lights up", () => {
    const html = draw(board([goal, ini, epic, done, open]));
    // Two leaves (the tasks), two spacers — and the only toggle buttons are
    // on the rows that fold something (the initiative and the epic).
    expect(html.match(/class="roadmap-toggle-spacer"/g)?.length).toBe(2);
    expect(html.match(/<button[^>]*class="roadmap-toggle /g)?.length).toBe(2);
    expect(html).not.toContain("roadmap-toggle-none");
  });

  it("shows whose a task is in an Owner column, and offers the menu on a task alone", () => {
    const html = draw(board([goal, ini, epic, done, open]));
    expect(html).toContain(">Owner<");
    // The done task: core · ann; the open one: the team alone.
    expect(html).toContain('title="core · ann"');
    expect(html).toContain('title="core"');
    // Two tasks, two pickers; a planning row has an empty owner cell.
    expect(html.match(/class="owner-picker"/g)?.length).toBe(2);
    expect(html).toContain('class="roadmap-cell roadmap-cell-owner"></span>');
  });

  it("draws a drag's landing place as the moved rows alone: no outline, no line", () => {
    const html = draw(board([goal, ini, epic, done, open]));
    expect(html).not.toContain("roadmap-row-drop-");
  });

  it("lists the open work that is not in the tree below it: in work, the shelves by team, unsorted", () => {
    const inWork = card({ itemId: "w", title: "Fix the build", team: "core", startDate: today, day: today });
    const shelved = card({ itemId: "b", title: "Someday", team: "core", parked: true });
    const strip = card({ itemId: "u", title: "Just typed" });
    const html = draw(board([goal, ini, epic, done, open, inWork, shelved, strip]));
    const at = (s: string) => html.indexOf(s);
    expect(at("Invoices")).toBeLessThan(at(">In work<"));
    expect(at(">In work<")).toBeLessThan(at("Fix the build"));
    expect(at("Fix the build")).toBeLessThan(at(">Backlog · core<"));
    expect(at(">Backlog · core<")).toBeLessThan(at("Someday"));
    expect(at("Someday")).toBeLessThan(at(">Unsorted<"));
    expect(at(">Unsorted<")).toBeLessThan(at("Just typed"));
    // A header is a line on both panes, so the lanes stay in step.
    expect(html.match(/roadmap-section-label/g)?.length).toBe(3);
    expect(html.match(/class="roadmap-lane roadmap-lane-section"/g)?.length).toBe(3);
  });

  it("filters by nobody's team: a planning card has none", () => {
    const html = draw(board([goal, ini, epic, done, open]));
    expect(html).not.toContain("team-filter-chip");
    expect(html).toContain('aria-label="Assignee filter"');
  });

  it("keeps the + button's hover-only class off every row: a form that shared it was invisible", () => {
    const html = draw(board([goal, ini, epic, done, open]));
    // The class is the BUTTON's (opacity 0 until the row is hovered); a row
    // container wearing it opened as an empty line — "the + makes an empty
    // space below". The inline form has a class of its own.
    expect(html).not.toMatch(/<div[^>]*class="[^"]*\broadmap-row-add\b/);
    expect(html).toMatch(/<button[^>]*class="roadmap-row-add"/);
  });
});
