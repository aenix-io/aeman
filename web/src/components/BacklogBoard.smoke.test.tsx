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

  // Whose a card is, as the day boards draw it: the team's avatar (the
  // shared OwnerBadge), the person's over it when somebody has the card, and
  // one label — no dot and no team chip of the Backlog's own.
  it("draws the team's avatar on every card, and the person's over it when somebody has it", () => {
    const html = draw(board([first, cozy]));
    expect(html.match(/class="team-avatar owner-badge-team"/g)?.length).toBe(2);
    expect(html).toContain('title="Team portal"');
    expect(html).toContain("owner-badge-overlap");
    expect(html).toContain('<span class="owner-badge-label">portal</span>');
    expect(html).toContain('<span class="owner-badge-label">kvaps</span>');
    expect(html).not.toContain("backlog-row-team");
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

  // Take is a button of its own, LEFT of the avatar, on a card nobody has;
  // the avatar is the owner picker on every card.
  it("draws Take as a separate button left of the avatar, only on a card nobody has", () => {
    const html = draw(board([first, cozy]));
    expect(html.match(/class="owner-picker backlog-row-badge"/g)?.length).toBe(2);
    expect(html.match(/class="owner-picker backlog-take"/g)?.length).toBe(1);
    const row = html.slice(html.indexOf('data-card="p1"'), html.indexOf('data-card="k1"'));
    expect(row.indexOf("backlog-take")).toBeGreaterThan(-1);
    expect(row.indexOf("backlog-take")).toBeLessThan(row.indexOf("owner-badge"));
    const taken = html.slice(html.indexOf('data-card="k1"'));
    expect(taken).not.toContain("backlog-take");
  });

  // K10: the reviews nobody has taken are pinned on top, out of the ranked
  // list, marked, with the reviewed task's chain.
  it("pins the reviews nobody has taken above the ranked tasks, marked", () => {
    const review = card({
      itemId: "r1",
      title: "review: Invoices",
      team: "portal",
      reviewOf: "t9",
      lineage: [EPIC],
      createdAt: "2026-11-03T10:00:00Z",
    });
    const html = draw(board([first, review]));
    const pinned = html.indexOf('class="backlog-pinned"');
    expect(pinned).toBeGreaterThan(-1);
    expect(pinned).toBeLessThan(html.indexOf("data-backlog-list"));
    expect(html).toContain("needs a reviewer");
    expect(html.indexOf("review: Invoices")).toBeLessThan(html.indexOf("Rotate the certificates"));
    expect(html).not.toContain('data-card="r1"');
    expect(html.slice(pinned, html.indexOf("data-backlog-list"))).toContain("Billing v2");
  });

  it("draws nothing that is not a parked open task of a shown team (K1, K2)", () => {
    const html = draw(
      board([
        first,
        card({ itemId: "w", title: "In hand today", team: "portal", parked: false, startDate: "2026-11-03" }),
        card({ itemId: "d", title: "Already done", team: "portal", progress: 100 }),
        card({ itemId: "s", title: "A step", team: "portal", parent: "p1" }),
        card({ itemId: "r", title: "Review: x", team: "portal", reviewOf: "p1", assignees: ["kvaps"] }),
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

  // A backlog card is BIG: about twice a day board's row height, the title
  // one step above the day boards' size, room between the rows, and the
  // chain and the avatars sized to match.
  it("draws big cards: tall rows, a 14px title, room between them", () => {
    const css = readFileSync(fileURLToPath(new URL("../styles.css", import.meta.url)), "utf8");
    const row = css.match(/\.backlog-row \{[^}]*\}/)?.[0] ?? "";
    expect(row).toMatch(/min-height: 56px/);
    const title = css.match(/\.backlog-row-title \{[^}]*\}/)?.[0] ?? "";
    expect(title).toMatch(/font-size: 14px/);
    const list = css.match(/\.backlog-list-body \{[^}]*\}/)?.[0] ?? "";
    expect(list).toMatch(/gap: 8px/);
    // The full width of the screen, with the Team board's gutters.
    const whole = css.match(/\.backlog-list \{[^}]*\}/)?.[0] ?? "";
    expect(whole).not.toMatch(/max-width/);
    expect(whole).toMatch(/padding: 8px/);
    expect(css).toMatch(/\.backlog-row-owner \.owner-badge-team,\s*\.backlog-row-owner \.owner-badge-person \{\s*width: 20px;/);
  });

  // Take's box sits square on its button: the owner picker's -6px pull is
  // undone on the backlog row, and Take is drawn like the app's small
  // buttons — a plain border and no shadow.
  it("draws Take square, like the app's small buttons", () => {
    const css = readFileSync(fileURLToPath(new URL("../styles.css", import.meta.url)), "utf8");
    expect(css).toMatch(/\.backlog-row-owner \.owner-picker-btn \{\s*margin-left: 0;/);
    const take = css.match(/\.backlog-take \.owner-picker-btn \{[^}]*\}/)?.[0] ?? "";
    expect(take).toMatch(/border: 1px solid var\(--line\)/);
    expect(take).toMatch(/box-shadow: none/);
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
