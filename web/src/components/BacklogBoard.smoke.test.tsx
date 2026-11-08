import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { isValidElement, type ReactElement, type ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { BacklogBoard, Landing, ParentChain, TakeDialog } from "./BacklogBoard";
import type { Board, Card, Provider } from "../providers/types";
import type { PresenceMap } from "../presence";

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
    members: [{ login: "kvaps" }],
    domains: [],
    sprintStates: {},
  }) as unknown as Board;

function draw(b: Board, teamFilter: string[] | null = ["portal", "cozystack"], presence?: PresenceMap) {
  return renderToStaticMarkup(
    <BacklogBoard
      presence={presence}
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
      me="kvaps"
    />,
  );
}

describe("the Backlog board", () => {
  const first = card({ itemId: "p1", title: "Rotate the certificates", team: "portal", size: "L", parent: "e", lineage: [GOAL, EPIC], dueDate: "2026-12-18" });
  const second = card({ itemId: "p2", title: "Drop the old API", team: "portal" });
  const cozy = card({ itemId: "k1", title: "Bump the chart", team: "cozystack", assignees: ["kvaps"] });

  it("draws ONE list, the selected teams' cards mixed in the board's order (K2)", () => {
    const html = draw(board([first, cozy, second]));
    expect(html.match(/data-backlog-list=""/g)?.length).toBe(1);
    expect(html).not.toContain("backlog-column");
    expect(html.indexOf("Rotate the certificates")).toBeLessThan(html.indexOf("Bump the chart"));
    expect(html.indexOf("Bump the chart")).toBeLessThan(html.indexOf("Drop the old API"));
  });

  // A backlog card IS a day board's card: the same `card` look, padding and
  // title, only as wide as the list.
  it("draws each card as the Me and Team boards do", () => {
    const html = draw(board([first, cozy]));
    expect(html.match(/class="card backlog-card"/g)?.length).toBe(2);
    expect(html).toContain('<span class="card-title backlog-card-title">Rotate the certificates</span>');
  });

  // Whose a card is: the team's AVATAR only — its name is the tooltip, no
  // label — and the person's over it when somebody has the card.
  it("draws the team's avatar without a label, and the person's over it when somebody has it", () => {
    const html = draw(board([first, cozy]));
    expect(html.match(/class="team-avatar owner-badge-team"/g)?.length).toBe(2);
    expect(html).toContain('title="Team portal"');
    expect(html).toContain("owner-badge-overlap");
    expect(html).not.toContain("owner-badge-label");
  });

  it("draws the parent chain on the right of the title, root first, each with its kind icon and a tooltip", () => {
    const html = draw(board([first]));
    const title = html.indexOf('<span class="card-title backlog-card-title">Rotate the certificates</span>');
    const chain = html.indexOf('class="backlog-row-chain"');
    expect(title).toBeGreaterThan(-1);
    expect(chain).toBeGreaterThan(title);
    expect(html.indexOf("Grow revenue across every market we serve")).toBeLessThan(html.indexOf("Billing v2"));
    expect(html).toContain("kind-icon-goal");
    expect(html).toContain("kind-icon-epic");
    expect(html).toContain('title="The goal «Grow revenue across every market we serve»"');
  });

  it("keeps the due date the server sends (its nearest planning card's end), and the size", () => {
    const html = draw(board([first]));
    expect(html).toContain("due 18 Dec");
    expect(html).toContain(">L</button>");
  });

  // TAKE is a button of its own, the RIGHT-MOST of a card nobody has, after
  // the avatar (which is the owner picker): it asks in a dialog, it does not
  // open the picker.
  it("draws Take right-most, after the avatar, as a button that opens a dialog", () => {
    const html = draw(board([first, cozy]));
    expect(html.match(/class="owner-picker backlog-row-badge"/g)?.length).toBe(2);
    const row = html.slice(html.indexOf('data-card="p1"'), html.indexOf('data-card="k1"'));
    const take = row.indexOf('class="backlog-take-btn"');
    expect(take).toBeGreaterThan(row.indexOf("owner-badge"));
    // Right-most ON THE CARD: what follows it is the gap below, not the card.
    const card = row.slice(take, row.indexOf("data-insert-gap", take));
    expect(card).not.toMatch(/<button[^>]*class="(?!backlog-take-btn)/);
    expect(row).toMatch(/<button[^>]*class="backlog-take-btn"[^>]*aria-haspopup="dialog"/);
    expect(card).not.toContain("owner-picker");
    const taken = html.slice(html.indexOf('data-card="k1"'));
    expect(taken).not.toContain("backlog-take-btn");
  });

  // The × takes NO room while it is hidden: at rest the right side reads
  // chain, size, avatar, Take with no gap. It stands LEFT of that whole
  // cluster — before the chain — anchored in a zero-width slot and laid over
  // the title's end, shown on hover.
  it("draws the × left of the chain, in a slot that takes no room at rest", () => {
    const html = draw(board([first, second]));
    const row = html.slice(html.indexOf('data-card="p1"'), html.indexOf('data-card="p2"'));
    const remove = row.indexOf('class="backlog-row-remove"');
    expect(remove).toBeGreaterThan(row.indexOf('class="backlog-row-remove-slot"'));
    expect(remove).toBeGreaterThan(row.indexOf("Rotate the certificates"));
    expect(remove).toBeLessThan(row.indexOf('class="backlog-row-chain"'));
    expect(row.indexOf('class="backlog-row-chain"')).toBeLessThan(row.indexOf(">L</button>"));
    expect(row.indexOf(">L</button>")).toBeLessThan(row.indexOf('class="backlog-row-owner"'));
    expect(row.indexOf('class="backlog-row-owner"')).toBeLessThan(row.indexOf('class="backlog-take-btn"'));
    const css = readFileSync(fileURLToPath(new URL("../styles.css", import.meta.url)), "utf8");
    const slot = css.match(/\.backlog-row-remove-slot \{[^}]*\}/)?.[0] ?? "";
    expect(slot).toMatch(/width: 0;/);
    expect(slot).toMatch(/position: relative;/);
    const hidden = css.match(/\.backlog-row-remove \{[^}]*\}/)?.[0] ?? "";
    expect(hidden).toMatch(/position: absolute;/);
    expect(hidden).toMatch(/right: 0;/);
    expect(hidden).toMatch(/opacity: 0;/);
    expect(hidden).toMatch(/pointer-events: none;/);
  });

  // K10: a review nobody has taken is NOT a section of its own and wears no
  // badge: it is in the same list as the tasks, in the board's order, drawn
  // as a review card is on the Me and Team boards, and dragged like any.
  it("draws a review nobody has taken as a review card, in the same list", () => {
    const review = card({
      itemId: "r1",
      title: "review: Invoices",
      team: "portal",
      reviewOf: "t9",
      lineage: [EPIC],
    });
    const html = draw(board([review, first]));
    expect(html).not.toContain("backlog-pinned");
    expect(html).not.toContain("needs a reviewer");
    expect(html).toContain('class="card backlog-card card-review"');
    expect(html).toContain('data-card="r1"');
    expect(html.indexOf("data-backlog-list")).toBeLessThan(html.indexOf('data-card="r1"'));
    expect(html.indexOf("review: Invoices")).toBeLessThan(html.indexOf("Rotate the certificates"));
  });

  // A review card shows whose the REVIEWED task is as the Team and Me boards
  // show it on a review card: the same small round counterpart avatar, on
  // the card's avatar. The people come from the server (status.reviewed).
  it("draws the reviewed task's owner as a review card's small counterpart avatar", () => {
    const review = card({
      itemId: "r1",
      title: "review: Invoices",
      team: "portal",
      reviewOf: "t9",
      reviewed: { uid: "t9", title: "Invoices", assignees: ["ann"] },
    });
    const html = draw(board([review, first]));
    const row = html.slice(html.indexOf('data-card="r1"'), html.indexOf('data-card="p1"'));
    // The Team card's own structure: the main avatar and the counterpart in
    // one .card-assign, the counterpart overlapping the main one's corner.
    expect(row).toMatch(/<div class="card-assign">.*class="team-avatar[^"]*".*<button[^>]*class="card-counterpart-avatar-btn"[^>]*title="In implementation: ann"/s);
    expect(row).not.toContain("Invoices</span></button></span></nav>");
    expect(row).toContain("card-counterpart-avatar");
    const task = html.slice(html.indexOf('data-card="p1"'));
    expect(task).not.toContain("card-counterpart-avatar");
  });

  // #290: the Backlog draws who else has a card selected, as the Team board
  // does — the same marks, on the card they selected.
  it("draws the others' selections on the cards they selected", () => {
    const html = draw(board([first, second]), ["portal", "cozystack"], { t1: { login: "ann", card: "p2" } });
    const row = html.slice(html.lastIndexOf("<div", html.indexOf('data-card="p2"')));
    expect(row).toMatch(/^<div class="card backlog-card card-peer-selected"/);
    expect(row).toContain('class="card-presence"');
    expect(row).toContain('title="Selected by ann"');
    const other = html.slice(html.indexOf('data-card="p1"'), html.indexOf('data-card="p2"'));
    expect(other).not.toContain("card-presence");
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

  // No add box at the foot of the list: every gap — above the first card,
  // between two, below the last — has an insert "+" that types a card in
  // right there (backlogboard.insertSpot), and an empty backlog has one.
  it("has no add box at the foot, and an insert + in every gap", () => {
    const html = draw(board([first, cozy]));
    expect(html).not.toMatch(/class="add-card[ "]/);
    expect(html.match(/data-insert-gap="/g)?.length).toBe(3);
    expect(html.match(/<button[^>]*class="insert-gap-btn"[^>]*aria-label="Add a card here"/g)?.length).toBe(3);
    // The gaps stand between the rows: one before the first card, one after
    // the last.
    expect(html.indexOf('data-insert-gap="0"')).toBeLessThan(html.indexOf('data-card="p1"'));
    expect(html.indexOf('data-insert-gap="2"')).toBeGreaterThan(html.indexOf('data-card="k1"'));
  });

  it("offers an empty backlog one +, always shown, to add the first card", () => {
    const html = draw(board([]));
    expect(html.match(/data-insert-gap="/g)?.length).toBe(1);
    expect(html).toContain("insert-gap insert-gap-always");
    expect(html).not.toMatch(/class="add-card[ "]/);
  });

  // K9: nothing of the Triage board — no weeks, no strip, no load.
  // There are no priorities (removed): a backlog card carries no priority
  // part and no menu to set one.
  it("draws no priority on a backlog card", () => {
    const html = draw(board([first, second, cozy]));
    expect(html).not.toMatch(/prio/);
    expect(html).not.toMatch(/Priority/);
  });

  it("draws no week, no strip and no load", () => {
    const html = draw(board([first, second, cozy]));
    expect(html).not.toMatch(/triage/i);
    expect(html).not.toMatch(/\bweek\b/i);
  });

  // The list spans the full width of the screen, with the Team board's
  // gutters; the avatar trigger sits square in the row, and Take is drawn
  // like the app's small buttons — a plain border and no shadow.
  it("spans the full width, and draws Take like the app's small buttons", () => {
    const css = readFileSync(fileURLToPath(new URL("../styles.css", import.meta.url)), "utf8");
    const whole = css.match(/\.backlog-list \{[^}]*\}/)?.[0] ?? "";
    expect(whole).not.toMatch(/max-width/);
    expect(whole).toMatch(/padding: 8px/);
    expect(css).toMatch(/\.card\.backlog-card \{\s*width: 100%;/);
    expect(css).toMatch(/\.backlog-row-owner \.owner-picker-btn \{\s*margin-left: 0;/);
    const take = css.match(/\.backlog-take-btn \{[^}]*\}/)?.[0] ?? "";
    expect(take).toMatch(/border: 1px solid var\(--line\)/);
    expect(take).toMatch(/box-shadow: none/);
  });
});

// Clicking a name in the chain opens that planning card's drawer: the link
// hands its uid to the board, which fetches the card and opens the drawer
// the Strategy board opens (backlogboard.openPlanning).
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

// The card in hand is drawn where it would land exactly as the card being
// dragged: a review keeps its review stripe there, a task has none.
describe("the card in hand", () => {
  it("wears the review stripe while a review card is dragged", () => {
    const html = renderToStaticMarkup(<Landing card={card({ itemId: "r1", title: "review: Invoices", reviewOf: "t9" })} />);
    expect(html).toContain('class="card backlog-card card-review backlog-card-landing"');
  });

  it("wears no stripe while a task is dragged", () => {
    const html = renderToStaticMarkup(<Landing card={card({ itemId: "p1", title: "Drop the old API" })} />);
    expect(html).toContain('class="card backlog-card backlog-card-landing"');
  });
});

// TAKE asks before it acts: "Take this card into work?" with Take and Cancel.
// Take answers and closes; Cancel only closes.
describe("the take dialog", () => {
  it("asks, and Take takes while Cancel does not", () => {
    const html = renderToStaticMarkup(<TakeDialog title="Rotate the certificates" onClose={vi.fn()} onTake={vi.fn()} />);
    expect(html).toContain("Take this card into work?");
    expect(html).toContain("Rotate the certificates");
    expect(html).toMatch(/>Cancel</);
    expect(html).toMatch(/>Take</);
    const onTake = vi.fn();
    const onClose = vi.fn();
    const el = TakeDialog({ title: "x", onClose, onTake });
    const find = (node: ReactNode, text: string): ReactElement<{ onClick: () => void }> | null => {
      if (Array.isArray(node)) {
        for (const n of node) {
          const hit = find(n, text);
          if (hit) {
            return hit;
          }
        }
        return null;
      }
      if (!isValidElement(node)) {
        return null;
      }
      const e = node as ReactElement<{ children?: ReactNode; onClick: () => void }>;
      if (e.type === "button" && e.props.children === text) {
        return e;
      }
      return find(e.props.children, text);
    };
    find(el, "Cancel")?.props.onClick();
    expect(onTake).not.toHaveBeenCalled();
    find(el, "Take")?.props.onClick();
    expect(onTake).toHaveBeenCalledOnce();
    expect(onClose).toHaveBeenCalledTimes(2);
  });
});

// O11 — the Backlog wears the boards' one filter dropdown: Team (No team
// among the teams, Manage teams at its foot) and Customer, in place of the
// chips; the customer choice narrows the list by effective customers.
describe("the Backlog's filters", () => {
  const drawWith = (cards: Card[], customerFilter: string[]) =>
    renderToStaticMarkup(
      <BacklogBoard
        board={{ ...board(cards), customers: [{ id: "cu1", name: "Hidora" }] } as Board}
        provider={{} as Provider}
        roster={["portal", "cozystack"]}
        teamFilter={["portal", ""]}
        onSetFilter={vi.fn()}
        customerFilter={customerFilter}
        onSetCustomerFilter={vi.fn()}
        onManageTeams={vi.fn()}
        avatars={{}}
        names={{}}
        patchCard={vi.fn()}
        addCard={vi.fn()}
        removeCard={vi.fn()}
        reorderCards={vi.fn()}
        reload={vi.fn()}
        onOpen={vi.fn()}
        onError={vi.fn()}
        me="kvaps"
      />,
    );
  const cards = [
    card({ itemId: "a", title: "For Hidora", team: "portal", effectiveCustomers: ["cu1"], customers: ["cu1"] }),
    card({ itemId: "b", title: "For nobody", team: "portal" }),
  ];

  it("are the Team and Customer dropdowns, not chips", () => {
    const html = drawWith(cards, []);
    expect(html).not.toContain("team-filter-chip");
    expect(html).toMatch(/aria-label="Team filter"[^>]*>Team: portal, No team</);
    expect(html).toMatch(/aria-label="Customer filter"[^>]*>Customer: anyone</);
  });

  it("narrow the list by effective customers", () => {
    const html = drawWith(cards, ["cu1"]);
    expect(html).toContain("For Hidora");
    expect(html).not.toContain("For nobody");
  });
});
