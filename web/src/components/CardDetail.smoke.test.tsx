import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { CardDetail } from "./CardDetail";
import type { Board, Card, Provider } from "../providers/types";

// The card pane is where a card is met away from any board, so what a board
// says by WHERE it puts a card has to be sayable here in words. This pins the
// row that does it — markup only, so what it holds is the shape.

const card = (over: Partial<Card> = {}): Card =>
  ({
    itemId: "c1",
    title: "A card",
    assignees: [],
    description: "",
    notes: [],
    ...over,
  }) as Card;

const board = { title: "t", url: "", cards: [], teams: [], members: [] } as unknown as Board;

function draw(c: Card) {
  return renderToStaticMarkup(
    <CardDetail
      card={c}
      board={board}
      provider={{} as Provider}
      onClose={vi.fn()}
      reload={vi.fn()}
      patchCard={vi.fn()}
    />,
  );
}

describe("the card pane", () => {
  it("offers the zone, whatever else the card is part of", () => {
    // A card met here may have come from a board with no zone areas at all
    // (Triage columns are people), so this is where one is given.
    const html = draw(card());
    expect(html).toContain("modal-zone-btn");
    expect(html).toContain(">none<");
  });

  it("says which zone the card is in", () => {
    expect(draw(card({ zone: "red" }))).toContain(">urgent<");
    expect(draw(card({ zone: "green" }))).toContain(">nice to have<");
  });

  it("does not offer to change one on a record", () => {
    // A record is a picture of a day that has passed; a change made from
    // there would land on today's card.
    expect(draw(card({ asOf: "2026-08-31" }))).toContain("disabled");
  });

  it("still says where the card comes from", () => {
    const html = draw(card({ project: "cozy", scope: "Storage" }));
    expect(html).toContain(">project<");
    expect(html).toContain(">scope<");
  });
});

// The hierarchy in the pane: the breadcrumb above the title, what the card
// sits under, and — on a planning card — its level and standing in place of
// a band. Mirrors what hierarchy.md gives the detail pane.
describe("the card pane and the hierarchy", () => {
  const lineage = [
    { uid: "g", type: "goal" as const, title: "Grow" },
    { uid: "e", type: "epic" as const, title: "Invoices" },
  ];

  function drawWith(c: Card, onReveal = vi.fn()) {
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

  it("draws the ancestors root first as a breadcrumb, planning crumbs as links to the Roadmap", () => {
    const html = drawWith(card({ parent: "e", lineage }));
    expect(html).toContain("modal-crumbs");
    expect(html.indexOf("Grow")).toBeLessThan(html.indexOf("Invoices"));
    expect(html.match(/modal-crumb-link/g)?.length).toBe(2);
  });

  it("names a subtask's task as a crumb that is not a link, and offers no parent picker on it", () => {
    const html = drawWith(card({ parent: "t", lineage: [...lineage, { uid: "t", title: "Render PDF" }] }));
    expect(html).toContain('class="modal-crumb-name">Render PDF<');
    expect(html).toContain(">subtask of<");
    expect(html).not.toContain(">under<");
  });

  it("offers a task a parent to sit under, and a goal none", () => {
    expect(drawWith(card())).toContain(">under<");
    expect(drawWith(card({ type: "goal" }))).not.toContain(">under<");
  });

  it("gives a planning card its level and standing instead of a band, and its dates", () => {
    const html = drawWith(card({ type: "epic", hierarchy: { status: "in-progress", done: 2, total: 5 }, startDate: "2026-02-09" }));
    expect(html).toContain(">epic<");
    expect(html).toContain("roadmap-status-in-progress");
    expect(html).toContain(">2/5<");
    expect(html).not.toContain(">zone<");
    expect(html).toContain('type="date"');
    expect(html).toContain("modal-children");
  });
});
