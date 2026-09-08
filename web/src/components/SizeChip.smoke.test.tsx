import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { SizeChip } from "./SizeChip";
import type { Card } from "../providers/types";

// The chip is the one place a size is SET on a board — the Triage grid and
// the backlog beside it both draw this component, so what it says about an
// unsized card is said in both.
const card = (over: Partial<Card> = {}): Card =>
  ({ itemId: "c1", title: "A card", assignees: [], progress: 0, ...over }) as Card;

const draw = (c: Card) => renderToStaticMarkup(<SizeChip card={c} onPick={vi.fn()} />);

describe("the size chip", () => {
  it("wears the size somebody gave it, solid", () => {
    const html = draw(card({ size: "XL" }));
    expect(html).toContain('class="size-chip"');
    expect(html).toContain(">XL</button>");
    expect(html).toContain("Click to change");
  });

  it("wears the size it is COUNTED as, dashed, while nobody has said", () => {
    // Not a blank: that letter is what the numbers on the board are made of,
    // and hiding it hid the board's own assumption while still charging for
    // it. Dashed, so it never reads as anybody's word.
    const html = draw(card());
    expect(html).toContain("size-chip size-chip-unset");
    expect(html).toContain(">M</button>");
    expect(html).toContain("Nobody has sized this: counted as M");
  });

  it("counts an unsized REVIEW as S", () => {
    const html = draw(card({ reviewOf: "orig" }));
    expect(html).toContain("size-chip-unset");
    expect(html).toContain(">S</button>");
  });
});
