import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { BacklogBoard } from "./BacklogBoard";
import type { Board, Card, Provider } from "../providers/types";

// The Backlog board draws its columns and its wide cards, and nothing else in
// the suite renders them. Markup only: no effect runs and no pointer moves,
// so what this pins is the shape — a column per selected team, the K2 cards
// in the board's order, each with its tag, due date, size and owner — and
// that nothing of the day boards or the Triage board comes out. The rules
// themselves are backlogboard.test.ts's.

const card = (over: Partial<Card>): Card =>
  ({ itemId: "c", title: "a task", assignees: [], progress: 0, parked: true, ...over }) as Card;

const EPIC = { uid: "e", type: "epic" as const, title: "Billing v2", end: "2026-12-18" };

const board = (cards: Card[]): Board =>
  ({
    title: "test",
    url: "",
    cards,
    teams: ["portal", "cozystack"],
    projects: [],
    scopes: [],
    deadlines: [],
    members: [{ login: "kvaps" }],
    domains: [],
    sprintStates: {},
  }) as unknown as Board;

function draw(b: Board, teamFilter: string[] | null = ["portal", "cozystack"]) {
  return renderToStaticMarkup(
    <BacklogBoard
      board={b}
      provider={{} as Provider}
      roster={["portal", "cozystack"]}
      teamFilter={teamFilter}
      onSetFilter={vi.fn()}
      avatars={{}}
      names={{}}
      patchCard={vi.fn()}
      addCard={vi.fn()}
      removeCard={vi.fn()}
      reorderCards={vi.fn()}
      reload={vi.fn()}
      onOpen={vi.fn()}
      onError={vi.fn()}
    />,
  );
}

describe("the Backlog board", () => {
  const first = card({ itemId: "p1", title: "Rotate the certificates", team: "portal", size: "L", parent: "e", lineage: [EPIC] });
  const second = card({ itemId: "p2", title: "Drop the old API", team: "portal" });
  const cozy = card({ itemId: "k1", title: "Bump the chart", team: "cozystack", assignees: ["kvaps"] });

  it("draws a column per selected team, in the chips' order, with its count", () => {
    const html = draw(board([first, second, cozy]));
    expect(html.match(/class="backlog-column"/g)?.length).toBe(2);
    expect(html.indexOf('data-backlog-team="portal"')).toBeLessThan(html.indexOf('data-backlog-team="cozystack"'));
    expect(html).toContain('<span class="backlog-column-count">2</span>');
  });

  it("gives the no-team group a column of its own when nothing is selected", () => {
    const html = draw(board([first]), null);
    expect(html).toContain('data-backlog-team=""');
    expect(html).toContain(">No team<");
  });

  it("draws the cards in the board's order: the listing's, which is BacklogOrder (K2)", () => {
    const html = draw(board([second, first]));
    expect(html.indexOf("Drop the old API")).toBeLessThan(html.indexOf("Rotate the certificates"));
  });

  it("draws one wide card per task: the title, the nearest planning card, its due date, the size", () => {
    const html = draw(board([first]));
    expect(html).toContain('<span class="backlog-row-title">Rotate the certificates</span>');
    expect(html).toContain("kind-icon-epic");
    expect(html).toContain('<span class="backlog-row-tag-text">Billing v2</span>');
    expect(html).toContain("due 18 Dec");
    expect(html).toContain(">L</button>");
  });

  it("offers the owner picker on every card: Take when nobody has it, the person when somebody does", () => {
    const html = draw(board([first, cozy]));
    expect(html.match(/class="owner-picker"/g)?.length).toBe(2);
    expect(html).toContain(">Take<");
    expect(html).toContain("backlog-row-person");
  });

  it("draws nothing that is not a parked open task of a shown team (K1, K2)", () => {
    const html = draw(
      board([
        first,
        card({ itemId: "w", title: "In hand today", team: "portal", parked: false, startDate: "2026-11-03" }),
        card({ itemId: "d", title: "Already done", team: "portal", progress: 100 }),
        card({ itemId: "s", title: "A step", team: "portal", parent: "p1" }),
        card({ itemId: "r", title: "Review: x", team: "portal", reviewOf: "p1" }),
        card({ itemId: "g", title: "Some epic", team: "portal", type: "epic" }),
        card({ itemId: "o", title: "Other team's", team: "freedom" }),
      ]),
    );
    for (const gone of ["In hand today", "Already done", "A step", "Review: x", "Some epic", "Other team's"]) {
      expect(html).not.toContain(gone);
    }
  });

  it("has an add box at the foot of every column", () => {
    const html = draw(board([first]));
    expect(html.match(/class="add-card[^"]*"/g)?.length).toBeGreaterThanOrEqual(2);
  });

  // K9: nothing of the Triage board — no weeks, no strip, no load.
  it("draws no week, no strip and no load", () => {
    const html = draw(board([first, second, cozy]));
    expect(html).not.toMatch(/triage/i);
    expect(html).not.toMatch(/\bweek\b/i);
  });
});
