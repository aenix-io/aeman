import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { Card as CardView } from "./Card";
import type { Card } from "../providers/types";

// A card's left stripe says where the card came FROM, and it says it on every
// board the card is met on — the day boards draw it here, the Triage board on
// its own slot (.triage-slot-project / -process). It went missing from the
// day boards once already: the stripe used to be the weekly plan's band, and
// when the plan was taken out the mark went with it, leaving project and
// process cards indistinguishable from the day's own work.

const card = (over: Partial<Card> = {}): Card =>
  ({
    itemId: "c1",
    title: "A card",
    assignees: [],
    progress: 40,
    ...over,
  }) as Card;

function draw(c: Card) {
  return renderToStaticMarkup(
    <CardView
      card={c}
      selected={false}
      onSelect={vi.fn()}
      onProgress={vi.fn()}
      onDelete={vi.fn()}
      onStage={vi.fn()}
      onInProgress={vi.fn()}
      onOpen={vi.fn()}
    />,
  );
}

describe("what a card's left stripe says", () => {
  it("marks a PROJECT card", () => {
    expect(draw(card({ epic: "Auth", project: "core" }))).toContain("card-project");
  });

  it("marks a PROCESS TURN", () => {
    expect(draw(card({ task: "t1" }))).toContain("card-process");
  });

  it("marks a REVIEW card", () => {
    expect(draw(card({ reviewOf: "c0" }))).toContain("card-review");
  });

  // A review card of a project card is BOTH; the waiting is the innermost
  // thing about it, so that is what the stripe says.
  it("says review where a card is both", () => {
    const html = draw(card({ reviewOf: "c0", epic: "Auth" }));
    expect(html).toContain("card-review");
    expect(html).not.toContain("card-project");
  });

  it("leaves the day board's own work unmarked", () => {
    const html = draw(card({ startDate: "2026-09-03" }));
    expect(html).not.toContain("card-project");
    expect(html).not.toContain("card-process");
    expect(html).not.toContain("card-review");
  });
});

// The actions row has two lanes, and the ORDER between them is the contract:
// everything that appears on hover comes first, so it grows to the left, and
// the icons that are always there keep their place when a pointer crosses the
// card.
describe("what moves when a pointer crosses a card", () => {
  const at = (html: string, cls: string) => html.indexOf(cls);

  it("keeps the always-visible icons to the RIGHT of every hover-only control", () => {
    const html = draw(
      card({ stage: "review", linkRefs: [{ kind: "issue", url: "https://x/1" }] } as Partial<Card>),
    );
    // The hover lane: the id copier is always rendered (hidden by CSS), so
    // its position in the markup is what decides whether it can push.
    expect(at(html, "card-action-id")).toBeGreaterThan(-1);
    expect(at(html, "card-links")).toBeGreaterThan(at(html, "card-action-id"));
    expect(at(html, "card-links")).toBeLessThan(at(html, "card-status-btn"));
  });

  // The SIZE is not among them. It is read on the boards that plan a week —
  // Triage, where it is on the face of every box — and set in the card's own
  // page; on a day board it was another glyph in a row already crowded, on a
  // card whose week was decided somewhere else.
  it("draws no size on a day board's card", () => {
    expect(draw(card({ size: "L" } as Partial<Card>))).not.toContain("card-size");
  });
});
