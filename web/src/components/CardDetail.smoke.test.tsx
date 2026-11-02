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
  it("is a drawer, not a centred box", () => {
    const html = draw(card());
    expect(html).toContain("drawer-backdrop");
    expect(html).toContain('class="drawer card-drawer"');
    expect(html).not.toContain('class="modal-backdrop"');
  });

  it("gives a task every row: people, team, zone, size, and what it sits under", () => {
    const html = draw(card({ zone: "red", team: "core", assignees: ["ann"], project: "cozy" }));
    expect(rows(html)).toEqual(["Status", "Assignees", "Team", "Zone", "Size", "Under", "Project"]);
    expect(html).toContain(">urgent<");
    expect(html).toContain(">ann<");
    expect(html).toContain("Mark as done");
  });

  it("gives a planning card its level, its standing, its dates and what it is under — no band, no size, no people", () => {
    const html = draw(card({ type: "epic", hierarchy: { status: "in-progress", done: 2, total: 5 }, startDate: "2026-02-09" }));
    expect(rows(html)).toEqual(["Level", "Status", "Dates", "Under"]);
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
    expect(rows(draw(card({ type: "goal" })))).toEqual(["Level", "Status", "Dates"]);
    expect(rows(draw(card({ type: "initiative" })))).toEqual(["Level", "Status", "Dates", "Under"]);
  });

  it("gives a project card its column and its dates, and a way under a planning card — no band, no size, no people", () => {
    const html = draw(card({ project: "cozy", scope: "Storage", startDate: "2026-03-02", day: "2026-03-13" }));
    expect(rows(html)).toEqual(["Status", "Dates", "Under", "Project", "Scope"]);
    expect(html).not.toContain("modal-size-chip");
    expect(html).not.toContain("modal-zone-btn");
    expect(html).toContain("Remove from project");
    expect(html).toContain('type="date"');
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
  it("draws the ancestors root first as a breadcrumb, planning crumbs as links to the Roadmap", () => {
    const html = draw(card({ parent: "e", lineage }));
    expect(html).toContain("modal-crumbs");
    expect(html.indexOf("Grow")).toBeLessThan(html.indexOf("Invoices"));
    expect(html.match(/modal-crumb-link/g)?.length).toBe(2);
    expect(html).toContain("kind-icon-goal");
    expect(html).toContain("kind-icon-epic");
  });

  it("names a subtask's task as a crumb that is not a link, and offers no parent picker on it", () => {
    const html = draw(card({ parent: "t", lineage: [...lineage, { uid: "t", title: "Render PDF" }] }));
    expect(html).toContain('class="modal-crumb-name">Render PDF<');
    expect(rows(html)).toContain("Subtask of");
    expect(rows(html)).not.toContain("Under");
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
