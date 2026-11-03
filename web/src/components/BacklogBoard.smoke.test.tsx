import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { isValidElement, type ReactElement, type ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { BacklogBoard, ParentChain } from "./BacklogBoard";
import type { Board, Card, Provider } from "../providers/types";

// The Backlog board draws its one list and its wide cards, and nothing else in
// the suite renders them. Markup only: no effect runs and no pointer moves,
// so what this pins is the shape — ONE list of the selected teams' K2 cards
// mixed in the board's order, each with its team chip, its parent chain on
// the right, its due date, size and owner, and one add box — and that
// nothing of the day boards or the Triage board comes out. The rules
// themselves are backlogboard.test.ts's.

const card = (over: Partial<Card>): Card =>
  ({ itemId: "c", title: "a task", assignees: [], progress: 0, parked: true, ...over }) as Card;

const GOAL = { uid: "g", type: "goal" as const, title: "Grow revenue across every market we serve" };
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
  const first = card({ itemId: "p1", title: "Rotate the certificates", team: "portal", size: "L", parent: "e", lineage: [GOAL, EPIC] });
  const second = card({ itemId: "p2", title: "Drop the old API", team: "portal" });
  const cozy = card({ itemId: "k1", title: "Bump the chart", team: "cozystack", assignees: ["kvaps"] });

  it("draws ONE list, the selected teams' cards mixed in the board's order (K2)", () => {
    const html = draw(board([first, cozy, second]));
    expect(html.match(/data-backlog-list=""/g)?.length).toBe(1);
    expect(html).not.toContain("backlog-column");
    expect(html.indexOf("Rotate the certificates")).toBeLessThan(html.indexOf("Bump the chart"));
    expect(html.indexOf("Bump the chart")).toBeLessThan(html.indexOf("Drop the old API"));
  });

  it("puts a chip of its team on every card", () => {
    const html = draw(board([first, cozy]));
    expect(html).toContain('<span class="backlog-row-team-name">portal</span>');
    expect(html).toContain('<span class="backlog-row-team-name">cozystack</span>');
  });

  it("draws the parent chain on the right of the title, root first, each with its kind icon and a tooltip", () => {
    const html = draw(board([first]));
    const title = html.indexOf('<span class="backlog-row-title">Rotate the certificates</span>');
    const chain = html.indexOf('class="backlog-row-chain"');
    expect(title).toBeGreaterThan(-1);
    expect(chain).toBeGreaterThan(title);
    expect(html.indexOf("Grow revenue across every market we serve")).toBeLessThan(html.indexOf("Billing v2"));
    expect(html).toContain("kind-icon-goal");
    expect(html).toContain("kind-icon-epic");
    expect(html).toContain('title="The goal «Grow revenue across every market we serve»"');
  });

  it("keeps the due date: the nearest planning card's end, and the size", () => {
    const html = draw(board([first]));
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

  it("has ONE add box at the foot of the list", () => {
    const html = draw(board([first, cozy]));
    expect(html.match(/class="add-card[ "]/g)?.length).toBe(1);
  });

  // K9: nothing of the Triage board — no weeks, no strip, no load.
  it("draws no week, no strip and no load", () => {
    const html = draw(board([first, second, cozy]));
    expect(html).not.toMatch(/triage/i);
    expect(html).not.toMatch(/\bweek\b/i);
  });

  // A plain card: no coloured accent down the left edge — the card is in no
  // band yet, and its team is its chip.
  it("draws a plain card, with no accent down its left edge", () => {
    const css = readFileSync(fileURLToPath(new URL("../styles.css", import.meta.url)), "utf8");
    for (const rule of css.match(/\.backlog-row(:hover)?\s*\{[^}]*\}/g) ?? []) {
      expect(rule).not.toMatch(/border-left/);
    }
    expect(draw(board([first]))).not.toMatch(/border-left/);
  });
});

// Clicking a name in the chain opens that planning card's drawer: the link
// hands its uid to the board, which fetches the card and opens the drawer
// the Roadmap opens (backlogboard.openPlanning).
describe("the parent chain", () => {
  const buttons = (node: ReactNode): ReactElement<{ onClick: () => void; title: string }>[] => {
    if (Array.isArray(node)) {
      return node.flatMap(buttons);
    }
    if (!isValidElement(node)) {
      return [];
    }
    const el = node as ReactElement<{ children?: ReactNode; onClick: () => void; title: string }>;
    return [...(el.type === "button" ? [el] : []), ...buttons(el.props.children)];
  };

  it("opens the planning card a name stands for", () => {
    const onOpen = vi.fn();
    const links = buttons(ParentChain({ chain: [GOAL, EPIC], onOpen }));
    expect(links.map((b) => b.props.title)).toEqual([
      "The goal «Grow revenue across every market we serve»",
      "The epic «Billing v2»",
    ]);
    links[1].props.onClick();
    expect(onOpen).toHaveBeenCalledWith("e");
  });

  it("draws nothing for a task under no planning card", () => {
    expect(ParentChain({ chain: [], onOpen: vi.fn() })).toBeNull();
  });
});
