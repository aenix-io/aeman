import { beforeEach, describe, expect, it } from "vitest";

import { forgetMade, justMade, noteMade } from "./justmade";
import { asksFirst } from "./removal";

beforeEach(forgetMade);

// A card the reader made a moment ago, in the view they are still looking at,
// is one they have the whole of in their head: they typed it, they can see it,
// and the × they reach for means "no, not that". Anything else is a card they
// are meeting again, and the × is a decision.

describe("cards made in the view being looked at", () => {
  it("remembers what was made here", () => {
    noteMade("c1");
    expect(justMade("c1")).toBe(true);
    expect(justMade("c2")).toBe(false);
  });

  // A create is drawn under a tmp id and swapped for the real one when the
  // server answers. Both are the same card to the reader, and the × may fall
  // on either — so both are remembered.
  it("remembers a card under its optimistic id and its real one", () => {
    noteMade("tmp-7");
    noteMade("01ABC");
    expect(justMade("tmp-7")).toBe(true);
    expect(justMade("01ABC")).toBe(true);
  });

  it("forgets everything when the reader moves to another view", () => {
    noteMade("c1");
    forgetMade();
    expect(justMade("c1")).toBe(false);
  });
});

// The × acts alone only where there is nothing to decide.
describe("when the × asks first", () => {
  it("does not ask about a card just made here and untouched", () => {
    expect(asksFirst({ progress: 0 }, true)).toBe(false);
  });

  it("asks about a card the reader is meeting again", () => {
    // Made in a view they have since left, or on another day: they no longer
    // have the whole of it in their head.
    expect(asksFirst({ progress: 0 }, false)).toBe(true);
  });

  it("asks about work somebody has done, however new the card is", () => {
    expect(asksFirst({ progress: 40 }, true)).toBe(true);
  });

  it("asks when it is told nothing", () => {
    // The safe reading for a caller that has not looked: a gesture that acts
    // in silence is how an × comes to be feared.
    expect(asksFirst({ progress: 0 })).toBe(true);
  });
});
