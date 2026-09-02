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
    const html = draw(card({ project: "cozy", epic: "Storage" }));
    expect(html).toContain(">project<");
    expect(html).toContain(">epic<");
  });
});
