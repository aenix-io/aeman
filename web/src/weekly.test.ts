import { describe, expect, it } from "vitest";
import { effectiveBand, owedIn, planRemoveOffered, slotBand } from "./weekly";

// Mirrors pkg/board WeeklyPlanAt's derived band: a Project-board slot needs no
// stored band — its span is its plan, and the band derives from the end date.
describe("slotBand", () => {
  it("ends by Wednesday of the viewed week -> wed", () => {
    expect(slotBand("2026-08-24", "2026-08-26")).toBe("wed");
  });
  it("ends exactly Wednesday -> wed (the boundary belongs to wed)", () => {
    expect(slotBand("2026-08-24", "2026-08-26")).toBe("wed");
  });
  it("ends Thursday -> fri", () => {
    expect(slotBand("2026-08-24", "2026-08-27")).toBe("fri");
  });
  it("ends Friday -> fri", () => {
    expect(slotBand("2026-08-24", "2026-08-28")).toBe("fri");
  });
  it("a middle week of a long span -> fri (open through that Friday)", () => {
    expect(slotBand("2026-08-24", "2026-09-04")).toBe("fri");
  });
  it("ends by Wednesday of a LATER covered week -> wed on that week", () => {
    // Started 17 Aug, ends Wed 26 Aug: on the 24 Aug panel it is a
    // by-Wednesday commitment even though its own week is the earlier one.
    expect(slotBand("2026-08-24", "2026-08-26")).toBe("wed");
    expect(slotBand("2026-08-17", "2026-08-26")).toBe("fri");
  });
});

describe("effectiveBand", () => {
  it("keeps a stored band — deriving never moves a hand-placed card", () => {
    expect(
      effectiveBand({ plan: "wed", epic: "E", week: "2026-08-24", day: "2026-08-28" }),
    ).toBe("wed");
  });
  it("derives for a band-less slot from its own end week", () => {
    expect(
      effectiveBand({ epic: "E", week: "2026-08-24", day: "2026-08-26" }),
    ).toBe("wed");
    expect(
      effectiveBand({ epic: "E", week: "2026-08-24", day: "2026-08-28" }),
    ).toBe("fri");
  });
  it("gives a band-less non-slot nothing", () => {
    expect(effectiveBand({ week: "2026-08-24" })).toBeUndefined();
    expect(effectiveBand({ epic: "E", week: "2026-08-24" })).toBeUndefined();
  });
});

// The weekly panel's × clears a band. A slot has none to clear — its span
// puts it on the panel — so pressing it would leave the card exactly where
// it was: the panel offers no × there.
describe("planRemoveOffered", () => {
  it("is not offered for a slot, which the panel derives from its span", () => {
    expect(planRemoveOffered({ epic: "Cozystack", week: "2026-08-03", day: "2026-08-28" })).toBe(
      false,
    );
    // A stored band does not change that: clearing it leaves the derivation.
    expect(
      planRemoveOffered({ epic: "Cozystack", week: "2026-08-03", day: "2026-08-28", plan: "fri" }),
    ).toBe(false);
  });

  it("is offered where the band is the only thing holding the card", () => {
    expect(planRemoveOffered({ plan: "fri", week: "2026-08-03" })).toBe(true);
    // An epic without both boundaries is not a slot: its band is stored.
    expect(planRemoveOffered({ epic: "Cozystack", week: "2026-08-03", plan: "wed" })).toBe(true);
  });
});

// A DEBT is drawn in the by-Wednesday band of the week it is late into:
// already late, so what it faces is that week's nearest deadline. Its own
// band belongs to the week it missed and still describes it there. Mirrors
// pkg/board WeeklyPlanAt — the panel groups by this and the card's stripe
// marks it, so the two must answer alike or a card sits under one deadline
// wearing the other.
describe("the band a debt is shown in", () => {
  const week = "2026-08-24";
  const last = "2026-08-17";

  it("is Wednesday on the panel it is late into", () => {
    expect(effectiveBand({ plan: "fri", week: last }, week)).toBe("wed");
    expect(effectiveBand({ plan: "wed", week: last }, week)).toBe("wed");
    // A slot is owed in the week its span ENDS in.
    expect(effectiveBand({ epic: "E", week: last, day: "2026-08-21" }, week)).toBe("wed");
  });

  it("is the card's own band on its own week", () => {
    expect(effectiveBand({ plan: "fri", week }, week)).toBe("fri");
    expect(effectiveBand({ plan: "wed", week }, week)).toBe("wed");
    // And on the panel of a week it has moved on FROM, the card is history:
    // the derived rule applies, not the debt one.
    expect(effectiveBand({ plan: "wed", week: "2026-08-31" }, week)).toBe("wed");
  });

  it("is the card's own band with no panel to judge against", () => {
    expect(effectiveBand({ plan: "fri", week: last })).toBe("fri");
  });

  it("names the week a card was owed in", () => {
    expect(owedIn({ plan: "fri", week: last })).toBe(last);
    expect(owedIn({ epic: "E", week: last, day: "2026-08-21" })).toBe(last);
    expect(owedIn({})).toBe("");
  });
});
