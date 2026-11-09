import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { Card as CardView } from "./Card";
import type { Card } from "../providers/types";

// A card's left stripe says where the card came FROM, and it says it on every
// board the card is met on — the day boards draw it here. It went missing from the day boards
// once already: the stripe used to be the weekly plan's band, and when the
// plan was taken out the mark went with it, leaving a review waiting on the
// person indistinguishable from the day's own work.

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
  it("marks a REVIEW card", () => {
    expect(draw(card({ reviewOf: "c0" }))).toContain("card-review");
  });

  it("leaves the day board's own work unmarked", () => {
    const html = draw(card({ startDate: "2026-09-03" }));
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

  // The SIZE is not among them. It is read on the board where work is
  // weighed up — the Backlog, where it is on the face of every card — and set
  // in the card's own page; on a day board it was another glyph in a row
  // already crowded, on a card taken into work somewhere else.
  it("draws no size on a day board's card", () => {
    // The chip's own class, not a name nothing renders: asserting the
    // absence of a class that never existed passes whatever the card draws.
    expect(draw(card({ size: "L" } as Partial<Card>))).not.toContain("size-chip");
    expect(draw(card({ size: "L" } as Partial<Card>))).not.toContain(">L<");
  });
});

// A card waiting for a dependency not done yet wears the locked look on the
// day boards, its tooltip naming what it waits for (dependencies.md); the
// card stays fully usable — only the mark is drawn.
describe("a card locked by what it waits for", () => {
  it("shows the locked mark with what it waits for", () => {
    const html = draw(card({ locked: true, blockedBy: [{ uid: "a", title: "Auth API" }], hiddenBlockers: 1 }));
    expect(html).toContain("Locked: waiting for «Auth API» and 1 hidden");
    expect(html).toContain(`class="card-stage"`);
  });

  it("is unmarked when nothing it waits for is open", () => {
    expect(draw(card({}))).not.toContain("Locked:");
  });
});

// The status flag shows only while the pointer is on the card, but its menu
// is drawn in a portal outside the card: moving onto the menu took the flag
// out of the layout, and the menu, placed by it, flew to the screen's corner.
// While the menu is open the flag stays where it is.
describe("the status flag while its menu is open", () => {
  it("stays laid out once the pointer leaves the card for the menu", () => {
    const css = readFileSync(fileURLToPath(new URL("../styles.css", import.meta.url)), "utf8");
    expect(css).toMatch(/\.card-stage\.card-hoveronly\.card-stage-open \{\s*display: inline-flex;/);
  });

  it("is drawn hover-only, without the open mark, while its menu is closed", () => {
    const html = draw(card({}));
    expect(html).toContain('class="card-stage card-hoveronly"');
    expect(html).not.toContain("card-stage-open");
  });
});
