import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { ProjectBoard } from "./ProjectBoard";
import type { Board, Card, Provider } from "../providers/types";
import { addDays, mondayOf, todayIso } from "../date";

// The Project board draws its own grid — a table of weeks against scopes — and
// nothing else in the suite renders it. This is the one test that proves the
// tree still comes out: the header row, the week labels, the cells and a
// card's slot, in the grid the reader sees. It is markup only, not a browser:
// no effect runs, so what it pins is the shape, not the gestures.
// THIS week's Monday, counted rather than named: the window opens two weeks
// back from today, so a calendar date drifts out of the row it was written
// for. This fixture named 2026-08-31 and the assertion below broke on the
// Monday after, when that week slid from row 4 to row 3.
const week = mondayOf(todayIso());
// Friday of the NEXT week: the card reaches into a second row.
const nextFriday = addDays(week, 11);

// The board reads the reader's zoom and column widths as it mounts. There is
// no browser here, so it gets an empty one and falls back to its defaults.
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

const card = (over: Partial<Card> = {}): Card =>
  ({
    itemId: "c1",
    title: "A card in a column",
    assignees: [],
    stage: "",
    project: "cozy",
    scope: "Storage",
    // A slot's row is its start's week (a task has no week, K7).
    startDate: week,
    ...over,
  }) as Card;

const board = (over: Partial<Board> = {}): Board =>
  ({
    title: "test",
    url: "",
    cards: [card()],
    teams: ["core"],
    projects: ["cozy"],
    scopes: [{ project: "cozy", name: "Storage" }],
    deadlines: [],
    members: [],
    domains: [],
    ...over,
  }) as Board;

const provider = {} as Provider;

function draw(b: Board, filter: string[] | null = ["cozy"]) {
  return renderToStaticMarkup(
    <ProjectBoard
      board={b}
      provider={provider}
      filter={filter}
      onSetFilter={vi.fn()}
      onManageProjects={vi.fn()}
      patchCard={vi.fn()}
      addCard={vi.fn()}
      replaceCard={vi.fn()}
      removeCard={vi.fn()}
      reload={vi.fn()}
      onError={vi.fn()}
      onOpen={vi.fn()}
    />,
  );
}

describe("the Project board's grid", () => {
  it("draws a header for every column and a label for every week", () => {
    const html = draw(board());
    expect(html).toContain("project-scope-head");
    expect(html).toContain(">Storage<");
    // The window opens two weeks back and eight ahead: eleven labels, plus
    // the gutter that offers a new column.
    expect(html.match(/project-week[ "]/g)?.length).toBe(11);
    expect(html).toContain("project-scope-add");
  });

  it("gives the surface one cell per column and week", () => {
    const html = draw(board());
    expect(html.match(/class="project-cell/g)?.length).toBe(11);
  });

  // A project card has no menu: what it did lives where the card's other
  // gestures live — the drawer's Under, Done and Delete, the Roadmap's drag.
  // And no team badge: a project card has no team.
  it("gives a card no … menu and no team badge", () => {
    const html = draw(board());
    expect(html).not.toContain("project-slot-menu");
    expect(html).not.toContain("Actions on");
    expect(html).not.toContain('aria-haspopup="menu"');
    expect(html).not.toContain("project-slot-team");
    expect(html).not.toContain("border-left-color");
  });

  // A card attached under a goal, an initiative or an epic shows that card's
  // KIND ICON alone, where the menu button was — its name is the tooltip,
  // never text on the card — and the icon opens it.
  it("shows the kind icon of the planning card it is attached to, and no name", () => {
    const html = draw(
      board({
        cards: [
          card({
            lineage: [
              { uid: "g", type: "goal", title: "Grow" },
              { uid: "e", type: "epic", title: "Billing v2" },
            ],
          }),
        ],
      }),
    );
    expect(html).toMatch(/<button[^>]*class="project-slot-plan"[^>]*title="The epic «Billing v2»"[^>]*><svg[^>]*kind-icon-epic[^>]*>.*?<\/svg><\/button>/s);
    expect(html).not.toContain(">Billing v2<");
    expect(html).not.toContain("project-slot-plan-text");
    expect(html).not.toContain(">Grow<");
    // In the title's row, after the title — where the menu button stood.
    expect(html.indexOf("project-slot-plan")).toBeGreaterThan(html.indexOf('class="project-slot-title"'));
  });

  it("shows nothing there on a card attached to nothing", () => {
    expect(draw(board())).not.toContain("project-slot-plan");
  });

  it("puts no × on a card: deleting is the pane's, never a hover's", () => {
    const html = draw(board());
    expect(html).not.toContain("project-slot-actions");
    expect(html).not.toContain("card-action-delete");
    expect(html).not.toContain('title="Delete"');
    expect(html).not.toContain("modal-backdrop");
    expect(html).not.toContain("Remove from project");
  });

  it("stands a card in its own week, spanning the weeks it reaches", () => {
    const html = draw(board({ cards: [card({ day: nextFriday })] }));
    // The header is row 1 and the window opens two weeks back, so THIS week
    // is row 4; the card ends in the week after, hence two rows.
    expect(html).toContain('style="grid-column:2;grid-row:4 / span 2"');
    expect(html).toContain("A card in a column");
  });

  it("puts today's week and today's cells where today is", () => {
    const html = draw(board());
    expect(html).toContain('class="project-week project-week-today" style="grid-row:4;grid-column:1"');
    expect(html).toContain('class="project-cell project-cell-today" style="grid-row:4;grid-column:2"');
  });

  it("says so when there is no column to draw", () => {
    const html = draw(board({ scopes: [], cards: [] }));
    expect(html).toContain("project-empty");
    expect(html).not.toContain("project-grid");
  });

  it("slices the column between two cards of the same week", () => {
    const html = draw(board({ cards: [card(), card({ itemId: "c2", title: "Beside it" })] }));
    // Every row stays 28px tall; the two cards take half the column each.
    expect(html).toContain("grid-template-rows:26px repeat(11, 28px)");
    expect(html).toContain("width:calc(50% - 2px);margin-left:0%");
    expect(html).toContain("width:calc(50% - 2px);margin-left:50%");
  });

  it("draws a deadline as a line on its week", () => {
    const html = draw(board({ deadlines: [{ project: "cozy", week }] }));
    expect(html).toContain("project-deadline-head");
    expect(html).toContain("project-deadline-body");
  });
});
