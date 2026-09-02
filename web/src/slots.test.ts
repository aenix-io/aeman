import { describe, expect, it } from "vitest";
import { isSlot, slotWeekPatch } from "./slots";

// A slot is a card that has taken a ROW on the Project board, and it takes
// one only when the column and both boundaries are there: a card half-way
// through being placed is not one, and drawing it as one would give it a
// span it has not got.
describe("what makes a card a slot", () => {
  it("takes a column and both boundaries", () => {
    expect(isSlot({ epic: "Webinars", week: "2026-09-28", day: "2026-10-02" })).toBe(true);
  });

  it("is not one without the column", () => {
    expect(isSlot({ week: "2026-09-28", day: "2026-10-02" })).toBe(false);
  });

  it("is not one with only a start", () => {
    expect(isSlot({ epic: "Webinars", week: "2026-09-28" })).toBe(false);
  });

  it("is not one with only an end", () => {
    expect(isSlot({ epic: "Webinars", day: "2026-10-02" })).toBe(false);
  });
});

// A card in a COLUMN has no week of its own: the server derives it from
// the start date. A client that moved the dates and left the week behind
// kept drawing the card in the week it left — a card pushed to the end of
// September stayed on THIS week's row until the next full load.
describe("the week that follows a slot's dates", () => {
  it("moves with the start date", () => {
    expect(slotWeekPatch({ epic: "Webinars" }, "2026-09-28")).toEqual({
      week: "2026-09-28",
    });
    // Any day of that week is the same row: the Monday.
    expect(slotWeekPatch({ epic: "Webinars" }, "2026-10-02")).toEqual({
      week: "2026-09-28",
    });
  });

  it("leaves a card outside every column alone — its week is its own", () => {
    expect(slotWeekPatch({}, "2026-09-28")).toEqual({});
  });

  it("has nothing to move when the dates are cleared", () => {
    expect(slotWeekPatch({ epic: "Webinars" }, null)).toEqual({});
  });
});
