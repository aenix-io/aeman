import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { COLUMN_DEFAULTS, COLUMN_MAX, COLUMN_MINS } from "../strategycolumns";
import { StrategyBoard, UnassignDialog } from "./StrategyBoard";
import type { StrategyFilters } from "../ownership";
import { HIDE_DONE } from "../workstatus";
import type { Board, Card, Provider } from "../providers/types";
import { addDays, todayIso } from "../date";

// The Strategy board draws its own two panes — a table of rows and a timeline of
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
    members: [],
    domains: [],
    sprintStates: {},
  }) as unknown as Board;

const provider = {} as Provider;

function draw(
  b: Board,
  focusId: string | null = null,
  filters: StrategyFilters = { teams: [], customers: [], owners: [], statuses: [] },
) {
  return renderToStaticMarkup(
    <StrategyBoard
      board={b}
      filters={filters}
      onFilters={vi.fn()}
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

describe("the Strategy board's two panes", () => {
  it("draws the header, and a row per card from the initiative down to the tasks", () => {
    const html = draw(board([goal, ini, epic, done, open]));
    expect(html).toContain("strategy-head");
    // Initiative to task by default: the goal is above the range and its
    // initiative stands in its place; the two tasks are the epic's rows.
    expect(html.match(/class="strategy-row"/g)?.length).toBe(4);
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
    expect(html).toContain("strategy-status strategy-status-in-progress");
    expect(html).toContain("strategy-status strategy-status-done");
    // "open" is placed nowhere — not in work: it waits.
    expect(html).toContain("strategy-status strategy-status-backlog");
    expect(html).toContain(">1/2<");
  });

  // The Status column is the table's last, and the timeline's divider is the
  // table's own right border: the widest pill, IN PROGRESS, fits inside the
  // column with room to spare, and the cell — header and rows alike — keeps
  // real space before the divider. A margin on the pill inside a fixed,
  // clipping cell does not: it only cut the pill off at the line.
  it("keeps the Status column clear of the timeline, header and rows alike", () => {
    const html = draw(board([goal, ini, epic, done, open]));
    expect(html).toMatch(/<span class="strategy-cell strategy-cell-status strategy-head-cell">Status</);
    expect(html.match(/class="strategy-cell strategy-cell-status/g)?.length).toBeGreaterThan(1);
    const css = readFileSync(fileURLToPath(new URL("../styles.css", import.meta.url)), "utf8");
    const grid = css.match(/\.strategy-row \{\s*display: grid;\s*grid-template-columns:([^;]+);/)?.[1] ?? "";
    const status = Number(/var\(--rm-status, (\d+)px\)/.exec(grid)?.[1] ?? 0);
    // The CSS fallback is the default: the two-part badge fits (K15).
    expect(status).toBe(COLUMN_DEFAULTS.status);
    // The column is the reader's to size, and never narrower than the badge.
    expect(COLUMN_DEFAULTS.status).toBeGreaterThanOrEqual(COLUMN_MINS.status);
    // The drawer's tree draws the same badge in a fixed column: as wide as
    // the Strategy board's floor.
    const drawer = css.match(/\.plan-tree \.strategy-row\.plan-tree-row \{\s*grid-template-columns:([^;]+);/)?.[1] ?? "";
    expect(Number(/(\d+)px\s*$/.exec(drawer.trim())?.[1] ?? 0)).toBeGreaterThanOrEqual(COLUMN_MINS.status);
    // One line, never wrapped; dragged narrower than it, the status
    // ellipsizes.
    const badge = css.match(/\.strategy-badge \{[^}]*\}/)?.[0] ?? "";
    expect(badge).toMatch(/white-space: nowrap/);
    const statusPart = css.match(/\.strategy-badge \.strategy-status \{[^}]*\}/)?.[0] ?? "";
    expect(statusPart).toMatch(/text-overflow: ellipsis/);
    expect(statusPart).toMatch(/min-width: 0/);
    const cell = css.match(/\.strategy-cell-status \{[^}]*\}/)?.[0] ?? "";
    const right = Number(/padding-right: (\d+)px/.exec(cell)?.[1] ?? 0);
    expect(right).toBeGreaterThanOrEqual(10);
    const pill = css.match(/\.strategy-status \{[^}]*\}/)?.[0] ?? "";
    expect(pill).not.toMatch(/margin/);
  });

  it("draws a bar for every dated card, a done one distinct, and the today line", () => {
    const html = draw(board([goal, ini, epic, done, open]));
    expect(html.match(/class="strategy-bar /g)?.length).toBe(3); // initiative, epic, the done task
    expect(html).toContain("strategy-bar-done");
    expect(html).toContain("strategy-bar-initiative");
    expect(html).toContain("strategy-today");
    expect(html.match(/class="strategy-lane"/g)?.length).toBe(4);
  });

  it("says so when there is nothing to draw", () => {
    const html = draw(board([]));
    expect(html).toContain("strategy-empty");
    expect(html).not.toContain("strategy-body");
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
    expect(html).toContain('class="strategy-toggle strategy-toggle-open"'); // the chevron, open, on a row with rows under it
    expect(html).toContain('class="strategy-toggle-spacer"'); // and a spacer on a leaf
    expect(html.match(/strategy-guide/g)?.length).toBe(1 + 2 + 2); // the epic one deep, its two tasks two deep
  });

  it("gives a leaf no button in the chevron's place: a spacer, not something that lights up", () => {
    const html = draw(board([goal, ini, epic, done, open]));
    // Two leaves (the tasks), two spacers — and the only toggle buttons are
    // on the rows that fold something (the initiative and the epic).
    expect(html.match(/class="strategy-toggle-spacer"/g)?.length).toBe(2);
    expect(html.match(/<button[^>]*class="strategy-toggle /g)?.length).toBe(2);
    expect(html).not.toContain("strategy-toggle-none");
  });

  // Every row offers the picker now: a task's whose-is-this, and a plan
  // item's team and owner (ownership.md, O9) — the initiative and the epic
  // here, owned by nobody yet.
  it("shows whose a task is in an Owner column, and a plan item's owner too", () => {
    const html = draw(board([goal, ini, epic, done, open]));
    expect(html).toContain(">Owner<");
    // The done task: core · ann; the open one: the team alone.
    expect(html).toContain('title="core · ann"');
    expect(html).toContain('title="core"');
    expect(html.match(/class="owner-picker"/g)?.length).toBe(4);
    expect(html).not.toContain('class="strategy-cell strategy-cell-owner"></span>');
  });

  it("draws no landing mark while nothing is dragged", () => {
    const html = draw(board([goal, ini, epic, done, open]));
    expect(html).not.toContain("strategy-row-drop-");
  });

  it("lists the open work that is not in the tree below it: in work, then each team's backlog (K1)", () => {
    const inWork = card({ itemId: "w", title: "Fix the build", team: "core", startDate: today, day: today });
    const shelved = card({ itemId: "b", title: "Someday", team: "core", parked: true });
    const html = draw(board([goal, ini, epic, done, open, inWork, shelved]));
    const at = (s: string) => html.indexOf(s);
    expect(at("Invoices")).toBeLessThan(at(">In work<"));
    expect(at(">In work<")).toBeLessThan(at("Fix the build"));
    expect(at("Fix the build")).toBeLessThan(at(">Backlog · core<"));
    expect(at(">Backlog · core<")).toBeLessThan(at("Someday"));
    // No third section: the strip is gone.
    expect(html).not.toContain(">Unsorted<");
    // A header is a line on both panes, so the lanes stay in step.
    expect(html.match(/strategy-section-label/g)?.length).toBe(2);
    expect(html.match(/class="strategy-lane strategy-lane-section"/g)?.length).toBe(2);
  });

  it("adds a plan item from a row under the tree, before In work, not from the toolbar", () => {
    const inWork = card({ itemId: "w", title: "Fix the build", team: "core", startDate: today, day: today });
    const html = draw(board([goal, ini, epic, done, open, inWork]));
    const toolbar = html.slice(html.indexOf("strategy-toolbar"), html.indexOf("strategy-head"));
    expect(toolbar).not.toContain("+ add");
    const at = (s: string) => html.indexOf(s);
    expect(at("strategy-row-addroot")).toBeGreaterThan(at("Send by mail"));
    expect(at("strategy-row-addroot")).toBeLessThan(at(">In work<"));
    // A line on both panes: the timeline keeps a lane for it.
    expect(html.match(/strategy-lane-addroot/g)?.length).toBe(1);
  });

  it("offers the add row on an empty strategy too", () => {
    const html = draw(board([]));
    expect(html).toContain("strategy-row-addroot");
    expect(html).toContain("+ add");
  });

  it("asks whose once: the Owner filter is the one person filter (O8)", () => {
    const html = draw(board([goal, ini, epic, done, open]));
    expect(html).not.toContain("team-filter-chip");
    expect(html).not.toContain('aria-label="Assignee filter"');
    expect(html).not.toContain("strategy-who");
    expect(html.match(/aria-label="Owner filter"/g)?.length).toBe(1);
  });

  it("keeps the + button's hover-only class off every row: a form that shared it was invisible", () => {
    const html = draw(board([goal, ini, epic, done, open]));
    // The class is the BUTTON's (opacity 0 until the row is hovered); a row
    // container wearing it opened as an empty line — "the + makes an empty
    // space below". The inline form has a class of its own.
    expect(html).not.toMatch(/<div[^>]*class="[^"]*\bstrategy-row-add\b/);
    expect(html).toMatch(/<button[^>]*class="strategy-row-add"/);
  });
});

// The Project cards toggle went with the Project board (H15): the toolbar
// offers no such button.
describe("the toolbar", () => {
  it("has no Project cards toggle", () => {
    const ep = card({ itemId: "pe", title: "Billing v2", type: "epic", parent: undefined, lineage: [] });
    const html = draw(board([ep]));
    expect(html).not.toContain("Show project cards");
    expect(html).not.toContain("kind-icon-project");
  });
});

// docs/design/ownership.md on the Strategy board: the Owner column names a plan
// item's owner (and its team), and the toolbar's Team, Customer and Owner
// filters — the server's selectors — say what is chosen; the customer
// roster is managed from the toolbar too.
describe("ownership on the Strategy board", () => {
  const owned = card({ itemId: "oe", title: "Billing", type: "epic", team: "core", owner: "ann", lineage: [] });

  it("names a plan item's owner and team in the Owner column", () => {
    const html = draw(board([owned]));
    const row = html.slice(html.indexOf(">Billing<"));
    expect(row).toMatch(/strategy-cell-owner[^>]*>.*owner-badge/s);
    expect(row).toContain('title="Team core"');
  });

  // O3 — a card with no team of its own shows the team it takes from
  // above, greyed, saying where from; one with its own team does not.
  it("shows an inherited team greyed, with where it comes from", () => {
    const task = card({ itemId: "ot", title: "Receipts", parent: "oe", lineage: [{ uid: "oe", type: "epic", title: "Billing" }] });
    const html = draw(board([owned, task]));
    const row = html.slice(html.indexOf(">Receipts<"));
    expect(row).toMatch(/class="strategy-owner strategy-owner-inherited" title="core — from «Billing»"/);
    expect(row).toContain('title="Team core"');
    const own = html.slice(html.indexOf(">Billing<"), html.indexOf(">Receipts<"));
    expect(own).not.toContain("strategy-owner-inherited");
  });

  it("offers the three filters and says what is chosen", () => {
    const b = { ...board([owned]), customers: [{ id: "cu1", name: "Hidora" }] } as unknown as Board;
    const html = draw(b, null, { teams: ["core"], customers: ["cu1"], owners: [], statuses: [] });
    expect(html).toMatch(/<button[^>]*aria-label="Team filter"[^>]*>[^<]*Team: core/);
    expect(html).toMatch(/<button[^>]*aria-label="Customer filter"[^>]*>[^<]*Customer: Hidora/);
    expect(html).toMatch(/<button[^>]*aria-label="Owner filter"[^>]*>[^<]*Owner: anyone/);
  });

  // A filter that matches nothing is not an empty strategy: say so, and offer
  // the way out.
  // The Status filter (In progress, Backlog, Done) stands with the other
  // three, and a status chosen is a filter like any: the empty state says
  // nothing matches and offers to clear it.
  it("has a Status filter, and counts it as a filter", () => {
    const html = draw(board([]), null, { teams: [], customers: [], owners: [], statuses: ["in-progress", "backlog"] });
    expect(html).toMatch(/aria-label="Status filter"[^>]*>Status: In progress, Backlog</);
    expect(html).toContain("Nothing matches the filters");
    expect(html).toContain("Clear the filters");
    expect(draw(board([]))).toMatch(/aria-label="Status filter"[^>]*>Status: all</);
  });

  // O14 — Locked and No items are values of the Status filter, and Hide done
  // names them: everything but Done. An empty epic the server keeps under
  // Hide done is drawn like any row, its No items badge and all.
  it("offers Locked and No items, and Hide done keeps them", () => {
    const html = draw(board([]), null, { teams: [], customers: [], owners: [], statuses: [...HIDE_DONE] });
    expect(html).toMatch(/aria-label="Status filter"[^>]*>Status: Hide done</);
    const locked = draw(board([]), null, { teams: [], customers: [], owners: [], statuses: ["locked"] });
    expect(locked).toMatch(/aria-label="Status filter"[^>]*>Status: Locked</);
    const empty = card({ itemId: "client", title: "Acme rollout", type: "epic", hierarchy: { status: "no-items", done: 0, total: 0 } });
    const drawn = draw(board([empty]), null, { teams: [], customers: [], owners: [], statuses: [...HIDE_DONE] });
    expect(drawn).toContain(">Acme rollout<");
    expect(drawn).toContain("strategy-status strategy-status-no-items");
  });

  it("says nothing matches the filters, and offers to clear them", () => {
    const html = draw(board([]), null, { teams: [], customers: [], owners: ["nobody"], statuses: [] });
    expect(html).toContain("Nothing matches the filters");
    expect(html).not.toContain("Nothing on the Strategy board yet");
    expect(html).toMatch(/<button[^>]*>Clear the filters<\/button>/);
    expect(draw(board([]))).toContain("Nothing on the Strategy board yet");
  });

  // The rosters are managed from the bottom of their filters (FilterSelect's
  // Manage entry), not from buttons of their own; and No team is a team.
  it("keeps no roster button of its own, and names No team as a team", () => {
    const html = draw(board([owned]), null, { teams: [""], customers: [], owners: [], statuses: [] });
    expect(html).not.toContain("Manage the customers");
    expect(html).not.toContain("strategy-customers-btn");
    expect(html).toMatch(/aria-label="Team filter"[^>]*>Team: No team</);
  });
});

// The table's columns are the reader's to size (strategycolumns.ts): a handle
// on every boundary of the header — the last one being the split with the
// timeline — reachable by the keyboard, and the widths reach the rows as
// CSS variables on the board.
describe("the Strategy board's column handles", () => {
  it("stand on every boundary of the header, as keyboard separators", () => {
    const html = draw(board([goal, ini, epic, done, open]));
    const head = html.slice(html.indexOf("strategy-head"), html.indexOf("strategy-rowgroup"));
    const handles = head.match(/<span[^>]*role="separator"[^>]*>/g) ?? [];
    expect(handles).toHaveLength(5);
    for (const h of handles) {
      expect(h).toContain('tabindex="0"');
      expect(h).toContain('aria-orientation="vertical"');
      expect(h).toMatch(/aria-valuenow="\d+"/);
      expect(h).toMatch(/aria-valuemin="\d+"/);
      expect(h).toContain(`aria-valuemax="${COLUMN_MAX}"`);
    }
    expect(head).toContain('aria-label="Resize the Card column"');
    expect(head).toContain('aria-label="Resize the table against the timeline"');
  });

  it("set the widths as CSS variables on the board, the defaults first", () => {
    const html = draw(board([goal, ini, epic, done, open]));
    expect(html).toMatch(/class="strategy"[^>]*style="[^"]*--rm-title:290px[^"]*--rm-table:712px/);
  });
});

// The badge is the row's work status and its menu (K16): pressed, it opens
// UNASSIGNED alone, which asks first, by count. Priorities were removed.
describe("the badge", () => {
  it("is a button opening the menu on every row of the tree", () => {
    const html = draw(board([goal, ini, epic, done, open]));
    expect(html).toMatch(/class="strategy-badge strategy-status-button"[^>]*aria-haspopup="menu"[^>]*aria-expanded="false"/);
    // Finished work too: the menu stays, with its one item.
    expect(html).toMatch(/strategy-status-button"[^>]*><span class="strategy-status strategy-status-done">/);
  });

  it("draws the work status alone, with no priority part", () => {
    const html = draw(board([goal, ini, epic, open, done]));
    expect(html).toMatch(/strategy-status-button"[^>]*><span class="strategy-status strategy-status-backlog">Backlog<\/span><\/button>/);
    expect(html).toMatch(/strategy-status-button"[^>]*><span class="strategy-status strategy-status-done">Done<\/span><\/button>/);
    expect(html).not.toMatch(/strategy-prio/);
  });

  it("asks how many tasks leave Team and Me before taking them out of work", () => {
    const html = renderToStaticMarkup(<UnassignDialog count={3} onClose={() => {}} onConfirm={() => {}} />);
    expect(html).toContain("Take 3 tasks out of work? They leave Team and Me and go back to their team&#x27;s backlog.");
    expect(html).toContain("Take out of work");
  });
});

// The toolbar folds every row or none (the order does not change), and an
// epic whose parked tasks are scattered in a team's backlog says Split.
describe("folding and Split on the Strategy board", () => {
  it("offers Collapse all and Expand all", () => {
    const html = draw(board([goal, ini, epic, done, open]));
    expect(html).toContain("Collapse all");
    expect(html).toContain("Expand all");
  });

  it("marks an epic whose parked tasks another task stands between", () => {
    const chain = [{ uid: "i", type: "initiative" as const, title: "Billing v2" }, { uid: "e", type: "epic" as const, title: "Invoices" }];
    const e2 = card({ itemId: "e2", title: "Other", type: "epic", parent: "i", lineage: [chain[0]] });
    const p1 = card({ itemId: "p1", title: "first", parent: "e", team: "core", parked: true, lineage: chain });
    const x1 = card({ itemId: "x1", title: "between", parent: "e2", team: "core", parked: true, lineage: [chain[0], { uid: "e2", type: "epic", title: "Other" }] });
    const p2 = card({ itemId: "p2", title: "second", parent: "e", team: "core", parked: true, lineage: chain });
    const html = draw(board([goal, ini, epic, e2, p1, x1, p2]));
    expect(html.match(/class="split-badge"/g)?.length).toBe(1);
    expect(draw(board([goal, ini, epic, e2, p1, p2, x1]))).not.toContain('class="split-badge"');
  });
});

// The lower toolbar keeps the filters at the left and the folding at the
// right; the timeline's window is the top bar's now.
describe("the Strategy board's own toolbar", () => {
  it("draws the filters first, the folding after them, and no window", () => {
    const html = draw(board([goal, ini, epic, done, open]));
    const bar = html.slice(html.indexOf("strategy-toolbar"), html.indexOf("strategy-head"));
    expect(bar.indexOf("Team")).toBeLessThan(bar.indexOf("Collapse all"));
    expect(bar).not.toContain("strategy-range");
  });
});
