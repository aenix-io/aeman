import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { CardDetail } from "./CardDetail";
import type { Board, Card, Provider } from "../providers/types";

// The card pane is where a card is met away from any board, so what a board
// says by WHERE it puts a card has to be sayable here in words. It is a
// drawer now — the rows of its sidebar are the card's kind's — and this pins
// which rows each kind gets. Markup only, so what it holds is the shape.

const card = (over: Partial<Card> = {}): Card =>
  ({
    itemId: "c1",
    title: "A card",
    assignees: [],
    description: "",
    notes: [],
    ...over,
  }) as Card;

const board = { title: "t", url: "", cards: [], teams: [], members: [], domains: [] } as unknown as Board;

const lineage = [
  { uid: "g", type: "goal" as const, title: "Grow" },
  { uid: "e", type: "epic" as const, title: "Invoices" },
];

function draw(c: Card, onReveal = vi.fn()) {
  return renderToStaticMarkup(
    <CardDetail
      card={c}
      board={board}
      provider={{} as Provider}
      onClose={vi.fn()}
      reload={vi.fn()}
      patchCard={vi.fn()}
      onReveal={onReveal}
      onOpenCard={vi.fn()}
    />,
  );
}

const rows = (html: string) => [...html.matchAll(/class="drawer-label">([^<]+)</g)].map((m) => m[1]);

describe("the card drawer", () => {
  // From a review card one can get to the card it reviews: "Reviews: «task»"
  // at the top, with the task's icon, opening it in the drawer.
  // A review card's breadcrumb ends in the task it reviews: the last crumb
  // of the same line, styled and opened like the others — no line of its own.
  it("ends a review card's breadcrumb in the task it reviews", () => {
    const html = draw(
      card({
        itemId: "r",
        title: "review: Invoices",
        reviewOf: "t",
        lineage: [{ uid: "e", type: "epic", title: "Billing" }],
        reviewed: { uid: "t", title: "Invoices" },
      }),
    );
    const nav = html.slice(html.indexOf('aria-label="Hierarchy"'), html.indexOf("</nav>"));
    expect(nav.indexOf(">Billing<")).toBeLessThan(nav.indexOf(">Invoices<"));
    expect(nav).toMatch(/<button[^>]*class="modal-crumb-link"[^>]*title="Open «Invoices»"[^>]*>Invoices<\/button>/);
    expect(html).not.toContain("drawer-reviews");
    expect(html).not.toContain("Reviews:");
  });

  it("has no such crumb on a card that reviews nothing", () => {
    expect(draw(card({}))).not.toContain('aria-label="Hierarchy"');
  });

  it("is a drawer, not a centred box", () => {
    const html = draw(card());
    expect(html).toContain("drawer-backdrop");
    expect(html).toContain('class="drawer card-drawer"');
    expect(html).not.toContain('class="modal-backdrop"');
  });

  // #289: the drawer's bar is the large one — the full width of its row, a
  // bigger handle reachable by the keyboard, and the value beside it.
  it("draws the large progress bar, keyboard-reachable, with its value", () => {
    const html = draw(card({ progress: 60, team: "core" }));
    expect(html).toContain('class="card-bar card-bar-large"');
    expect(html).toMatch(/role="slider"[^>]*tabindex="0"|tabindex="0"[^>]*role="slider"/);
    expect(html).toContain('class="drawer-progress-pct">60%<');
  });

  it("gives a task every row: its owner, zone, size, and what it sits under", () => {
    const html = draw(card({ zone: "red", team: "core", assignees: ["ann"] }));
    expect(rows(html)).toEqual(["Status", "Progress", "Owner", "Zone", "Size", "Under", "Customers"]);
    expect(html).toContain(">urgent<");
    // The owner is the team and the person, in one menu (OwnerPicker).
    expect(html).toContain("core · ann");
    expect(html).toContain('class="owner-picker drawer-owner-picker"');
    expect(html).toContain("Mark as done");
    // A task's subtasks live in the main column, with a way to add one.
    expect(html).toContain('aria-label="Subtasks"');
    expect(html).toContain("+ Add subtask");
  });

  it("asks who reviews a card on review, from those who can read it", () => {
    const original = card({ itemId: "o", stage: "review", team: "core", assignees: ["ann"] });
    const review = card({ itemId: "r", reviewOf: "o", team: "core", assignees: ["bob"] });
    const withBoth = { ...board, cards: [original, review] } as Board;
    const html = renderToStaticMarkup(
      <CardDetail card={original} board={withBoth} provider={{} as Provider} onClose={vi.fn()} reload={vi.fn()} patchCard={vi.fn()} />,
    );
    expect(rows(html)).toContain("Reviewer");
    expect(html).toContain(">bob<");
    // And not on a card that is not on review.
    expect(rows(draw(card({ team: "core" })))).not.toContain("Reviewer");
  });

  it("reads a subtask's owner as its task's, and names the task as a way to it", () => {
    const sub = card({ parent: "t", team: "core", assignees: ["ann"], lineage: [{ uid: "t", title: "Render PDF" }] });
    const html = draw(sub);
    expect(rows(html)).toEqual(["Status", "Progress", "Owner", "Zone", "Size", "Subtask of", "Customers"]);
    expect(html).not.toContain("owner-picker");
    expect(html).toContain('title="Open «Render PDF»"');
    expect(html).not.toContain('aria-label="Subtasks"'); // one level deep: a subtask has none
  });

  it("gives a planning card its level, its standing, its owner and team, its dates, what it is under and who it is for — no band, no size, no assignee", () => {
    const html = draw(card({ type: "epic", hierarchy: { status: "in-progress", done: 2, total: 5 }, startDate: "2026-02-09" }));
    expect(rows(html)).toEqual(["Level", "Status", "Owner", "Dates", "Under", "Customers"]);
    expect(html).toContain("roadmap-status-in-progress");
    expect(html).toContain(">2/5<");
    expect(html).not.toContain("modal-size-chip");
    expect(html).not.toContain("modal-zone-btn");
    expect(html).toContain('type="date"');
    expect(html).toContain("drawer-children");
    expect(html).toContain("+ Add task");
    expect(html).not.toContain("Mark as done");
  });

  it("gives a goal no Under row, and an initiative or an epic one", () => {
    expect(rows(draw(card({ type: "goal" })))).toEqual(["Level", "Status", "Owner", "Dates", "Customers"]);
    expect(rows(draw(card({ type: "initiative" })))).toEqual(["Level", "Status", "Owner", "Dates", "Under", "Customers"]);
  });

  it("reads only on a record", () => {
    // A record is a picture of a day that has passed; a change made from
    // there would land on today's card.
    const html = draw(card({ asOf: "2026-08-31", zone: "red" }));
    expect(html).toContain("read only");
    expect(html).toContain("disabled");
    expect(html).not.toContain("drawer-actions");
  });
});

// The hierarchy in the pane: the breadcrumb above the title, what the card
// sits under, and what sits under a planning card. Mirrors what hierarchy.md
// gives the detail pane.
describe("the card drawer and the hierarchy", () => {
  it("draws the ancestors root first as a breadcrumb, every crumb opening its card, planning crumbs with the Roadmap too", () => {
    const html = draw(card({ parent: "e", lineage }));
    expect(html).toContain("modal-crumbs");
    expect(html.indexOf("Grow")).toBeLessThan(html.indexOf("Invoices"));
    expect(html.match(/modal-crumb-link/g)?.length).toBe(2);
    expect(html).toContain('title="Open «Grow»"');
    expect(html.match(/modal-crumb-reveal/g)?.length).toBe(2);
    expect(html).toContain("kind-icon-goal");
    expect(html).toContain("kind-icon-epic");
  });

  it("names a subtask's task as a crumb that opens it, and offers no parent picker on it", () => {
    const html = draw(card({ parent: "t", lineage: [...lineage, { uid: "t", title: "Render PDF" }] }));
    expect(html).toContain('title="Open «Render PDF»"');
    expect(html.match(/modal-crumb-reveal/g)?.length).toBe(2); // the goal and the epic, not the task
    expect(rows(html)).toContain("Subtask of");
    expect(rows(html)).not.toContain("Under");
  });

  it("offers a way back when the card was opened from another", () => {
    const html = renderToStaticMarkup(
      <CardDetail card={card()} board={board} provider={{} as Provider} onClose={vi.fn()} reload={vi.fn()} patchCard={vi.fn()} onBack={vi.fn()} />,
    );
    expect(html).toContain('class="drawer-back"');
    expect(draw(card())).not.toContain('class="drawer-back"');
  });

  it("shows the parent in the Under row and offers to change it", () => {
    const html = draw(card({ parent: "e", lineage }));
    expect(html).toContain('class="drawer-pick"');
    expect(html).toContain("Invoices");
    expect(html).toContain("Delete or unlink…");
  });

  it("offers delete alone on a card that is under nothing", () => {
    expect(draw(card())).toContain("Delete…");
    expect(draw(card())).not.toContain("Delete or unlink…");
  });
});

// docs/design/ownership.md in the drawer: a plan item's owner and team in one
// picker (a person needs no team there), and every card's customers — its
// own as chips it can remove, the inherited ones grey, saying where they
// come from, not removable here.
describe("ownership in the drawer", () => {
  const withRoster = (cards: Card[]) =>
    ({
      ...board,
      cards,
      customers: [
        { id: "hidora", name: "Hidora" },
        { id: "acme", name: "Acme" },
      ],
    }) as unknown as Board;
  const drawOn = (c: Card, b: Board) =>
    renderToStaticMarkup(
      <CardDetail card={c} board={b} provider={{} as Provider} onClose={vi.fn()} reload={vi.fn()} patchCard={vi.fn()} onOpenCard={vi.fn()} />,
    );

  it("names a plan item's team and owner in the owner picker", () => {
    const html = drawOn(card({ type: "epic", team: "portal", owner: "ann" }), withRoster([]));
    expect(html).toContain("portal · ann");
    expect(html).toContain('class="owner-picker drawer-owner-picker"');
  });

  it("shows inherited customers grey with where they come from, and the card's own removable", () => {
    const goal = card({ itemId: "g", title: "Grow", type: "goal", customers: ["hidora"] });
    const task = card({ itemId: "t", title: "Receipts", parent: "g", team: "core", customers: ["acme"], lineage: [{ uid: "g", type: "goal", title: "Grow" }] });
    const html = drawOn(task, withRoster([goal, task]));
    expect(html).toMatch(/<span class="customer-chip customer-chip-inherited" title="From «Grow»">Hidora<\/span>/);
    expect(html).toMatch(/<span class="customer-chip">Acme<button[^>]*aria-label="Remove Acme"/);
    expect(html).not.toContain('aria-label="Remove Hidora"');
    expect(html).toContain('aria-label="Add a customer"');
  });

  it("shows a task's inherited team inside its Owner, greyed, with where it comes from", () => {
    const goal = card({ itemId: "g", title: "Grow", type: "goal", team: "portal" });
    const task = card({ itemId: "t", title: "Receipts", parent: "g", lineage: [{ uid: "g", type: "goal", title: "Grow" }] });
    const html = drawOn(task, withRoster([goal, task]));
    expect(html).toMatch(/class="drawer-owner drawer-owner-inherited"[^>]*title="portal — from «Grow»"/);
    expect(html).toMatch(/drawer-owner-inherited[\s\S]*?<span class="drawer-owner-text">portal<\/span>/);
    // Said once, in the Owner: no second Team row beside it.
    expect(html).not.toContain("drawer-row-quiet");
  });

  it("shows a plan item's inherited team inside its Owner too, beside its own owner", () => {
    const goal = card({ itemId: "g", title: "Grow", type: "goal", team: "portal" });
    const epic = card({ itemId: "e", title: "Ship", type: "epic", parent: "g", owner: "ann", lineage: [{ uid: "g", type: "goal", title: "Grow" }] });
    const html = drawOn(epic, withRoster([goal, epic]));
    expect(html).toMatch(/class="drawer-owner drawer-owner-inherited"[^>]*title="portal · ann — from «Grow»"/);
  });

  it("does not grey a team the card has of its own", () => {
    const goal = card({ itemId: "g", title: "Grow", type: "goal", team: "portal" });
    const task = card({ itemId: "t", title: "Receipts", parent: "g", team: "core", lineage: [{ uid: "g", type: "goal", title: "Grow" }] });
    expect(drawOn(task, withRoster([goal, task]))).not.toContain("drawer-owner-inherited");
  });

  it("offers a × to unset an Owner the card has of its own", () => {
    const epic = card({ itemId: "e", title: "Ship", type: "epic", team: "portal", owner: "ann" });
    expect(drawOn(epic, withRoster([epic]))).toMatch(/<button[^>]*class="drawer-owner-clear"[^>]*aria-label="Unset the owner"/);
    const task = card({ itemId: "t", title: "Receipts", team: "portal", assignees: ["ann"] });
    expect(drawOn(task, withRoster([task]))).toContain('aria-label="Unset the owner"');
  });

  it("offers no × where there is nothing of the card's own to unset", () => {
    const goal = card({ itemId: "g", title: "Grow", type: "goal", team: "portal" });
    const inherited = card({ itemId: "e", title: "Ship", type: "epic", parent: "g", lineage: [{ uid: "g", type: "goal", title: "Grow" }] });
    expect(drawOn(inherited, withRoster([goal, inherited]))).not.toContain("Unset the owner");
    const bare = card({ itemId: "b", title: "Bare", type: "epic" });
    expect(drawOn(bare, withRoster([bare]))).not.toContain("Unset the owner");
  });

  it("offers no × on a review card, whose owner follows the card it reviews", () => {
    const orig = card({ itemId: "t", title: "Receipts", team: "portal", stage: "review" });
    const review = card({ itemId: "r", title: "review: Receipts", team: "portal", assignees: ["ann"], reviewOf: "t" });
    expect(drawOn(review, withRoster([orig, review]))).not.toContain("Unset the owner");
  });
});

// The drawer gives its card away from its title row, left of the ✕: the id
// to copy and the link to copy — the board's address with the view and this
// card (deeplink.ts).
describe("the drawer's card identity line", () => {
  it("shows the id and the link of the card it shows", () => {
    const html = renderToStaticMarkup(
      <CardDetail
        card={card()}
        board={board}
        provider={{} as Provider}
        onClose={vi.fn()}
        reload={vi.fn()}
        patchCard={vi.fn()}
        cardLinkFor={(uid) => `https://b.example/?view=me&card=${uid}`}
      />,
    );
    // The chips sit in the title row, just left of the close ✕.
    const titleRow = html.slice(html.indexOf('class="drawer-title-row"'), html.indexOf("</header>"));
    expect(titleRow.indexOf('class="drawer-identity"')).toBeGreaterThan(-1);
    expect(titleRow.indexOf('class="drawer-identity"')).toBeLessThan(titleRow.indexOf('class="modal-close"'));
    expect(titleRow).toMatch(/aria-label="Copy the link to this card" title="https:\/\/b.example\/\?view=me&amp;card=/);
    expect(html.slice(html.indexOf('class="drawer-side"'))).not.toContain("drawer-identity");
  });
});

// A plan item's "Under it" is the Roadmap's tree rooted one level below it,
// with the counts header kept.
describe("a plan item's drawer", () => {
  it("draws what is under it as the Roadmap's tree", () => {
    const epic = card({ itemId: "e", title: "Invoices", type: "epic", hierarchy: { status: "backlog", done: 1, total: 3 } } as Partial<Card>);
    const t = card({ itemId: "t", title: "Render PDF", parent: "e", lineage: [{ uid: "e", type: "epic", title: "Invoices" }] });
    const b = { ...board, cards: [epic, t] } as Board;
    const html = renderToStaticMarkup(
      <CardDetail card={epic} board={b} provider={{} as Provider} onClose={vi.fn()} reload={vi.fn()} patchCard={vi.fn()} />,
    );
    expect(html).toContain("1/3 tasks done");
    expect(html).toMatch(/class="plan-tree"[^>]*>.*Render PDF/s);
    expect(html).toMatch(/>\+ Add task<\/button>/);
  });
});
