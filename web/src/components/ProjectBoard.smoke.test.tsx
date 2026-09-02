import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { ProjectBoard } from "./ProjectBoard";
import type { Board, Card, Provider } from "../providers/types";

// The Project board draws its own grid — a table of weeks against epics — and
// nothing else in the suite renders it. This is the one test that proves the
// tree still comes out: the header row, the week labels, the cells and a
// card's slot, in the grid the reader sees. It is markup only, not a browser:
// no effect runs, so what it pins is the shape, not the gestures.
const week = "2026-08-31";

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
    epic: "Storage",
    week,
    ...over,
  }) as Card;

const board = (over: Partial<Board> = {}): Board =>
  ({
    title: "test",
    url: "",
    cards: [card()],
    teams: ["core"],
    projects: ["cozy"],
    epics: [{ project: "cozy", name: "Storage" }],
    deadlines: [],
    processes: [],
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
    expect(html).toContain("project-epic-head");
    expect(html).toContain(">Storage<");
    // The window opens two weeks back and eight ahead: eleven labels, plus
    // the gutter that offers a new column.
    expect(html.match(/project-week[ "]/g)?.length).toBe(11);
    expect(html).toContain("project-epic-add");
  });

  it("gives the surface one cell per column and week", () => {
    const html = draw(board());
    expect(html.match(/class="project-cell/g)?.length).toBe(11);
  });

  it("stands a card in its own week, spanning the weeks it reaches", () => {
    const html = draw(board({ cards: [card({ day: "2026-09-11" })] }));
    // The header is row 1 and the window opens two weeks back, so the week of
    // 2026-08-31 is row 4; the card ends in the week after, hence two rows.
    expect(html).toContain('style="grid-column:2;grid-row:4 / span 2"');
    expect(html).toContain("A card in a column");
  });

  it("puts today's week and today's cells where today is", () => {
    const html = draw(board());
    expect(html).toContain('class="project-week project-week-today" style="grid-row:4;grid-column:1"');
    expect(html).toContain('class="project-cell project-cell-today" style="grid-row:4;grid-column:2"');
  });

  it("says so when there is no column to draw", () => {
    const html = draw(board({ epics: [], cards: [] }));
    expect(html).toContain("project-empty");
    expect(html).not.toContain("project-grid");
  });

  it("draws a deadline as a line on its week", () => {
    const html = draw(board({ deadlines: [{ project: "cozy", week }] }));
    expect(html).toContain("project-deadline-head");
    expect(html).toContain("project-deadline-body");
  });
});
