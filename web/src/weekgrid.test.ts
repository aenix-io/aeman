import { describe, expect, it } from "vitest";
import {
  BASE_COL,
  BASE_ROW,
  type Laned,
  extentOf,
  isoWeekNo,
  packLanes,
  rowPx,
  sharedColumnPx,
  weekLabel,
  weekWindow,
} from "./weekgrid";

// Every date here is real: 2026-08-31 is a Monday, and the weeks around it
// are the ones the grid would draw on the day this was written.
const MON = "2026-08-31";

describe("weekWindow", () => {
  it("opens two weeks back and eight ahead of today", () => {
    const weeks = weekWindow([], MON, 0, 0);
    expect(weeks[0]).toBe("2026-08-17");
    expect(weeks[weeks.length - 1]).toBe("2026-10-26");
    expect(weeks).toHaveLength(11);
  });

  it("runs from Monday to Monday, with no week missing", () => {
    const weeks = weekWindow([], MON, 0, 0);
    expect(weeks.slice(0, 4)).toEqual(["2026-08-17", "2026-08-24", "2026-08-31", "2026-09-07"]);
  });

  it("widens back to hold a card standing before the window", () => {
    const weeks = weekWindow([{ week: "2026-06-01" }], MON, 0, 0);
    expect(weeks[0]).toBe("2026-06-01");
    expect(weeks[weeks.length - 1]).toBe("2026-10-26");
  });

  it("widens forward past a card that ends beyond it, with a runway to drag into", () => {
    // The card ends in the week of 2026-11-02; the window runs two weeks on.
    const weeks = weekWindow([{ week: "2026-10-19", day: "2026-11-06" }], MON, 0, 0);
    expect(weeks[weeks.length - 1]).toBe("2026-11-16");
  });

  it("measures the runway from a card's end, not from its week", () => {
    const short = weekWindow([{ week: "2026-11-30" }], MON, 0, 0);
    expect(short[short.length - 1]).toBe("2026-12-14");
  });

  it("takes a week that is not a Monday for the Monday of its week", () => {
    // A card filed by a tool that wrote a Wednesday still lands on its Monday.
    expect(weekWindow([{ week: "2026-06-03" }], MON, 0, 0)[0]).toBe("2026-06-01");
  });

  it("leaves a card that already fits alone", () => {
    expect(weekWindow([{ week: "2026-09-07", day: "2026-09-11" }], MON, 0, 0)).toEqual(
      weekWindow([], MON, 0, 0),
    );
  });

  it("ignores a card with no dates at all", () => {
    expect(weekWindow([{}], MON, 0, 0)).toEqual(weekWindow([], MON, 0, 0));
  });

  it("adds the weeks the reader asked for on top", () => {
    const weeks = weekWindow([], MON, 8, 8);
    expect(weeks[0]).toBe("2026-06-22");
    expect(weeks[weeks.length - 1]).toBe("2026-12-21");
  });

  it("keeps whichever reaches further — the reader's weeks or the cards'", () => {
    // Asking for eight more weeks back reaches 2026-06-22; the card reaches
    // further, and the card wins. Neither ever shortens the window.
    const weeks = weekWindow([{ week: "2026-06-01" }], MON, 8, 0);
    expect(weeks[0]).toBe("2026-06-01");
  });
});

describe("weekLabel", () => {
  it("names a week by the day its Monday falls on", () => {
    expect(weekLabel("2026-08-31")).toBe("31 Aug");
  });

  it("pads a single-digit day, so the labels line up", () => {
    expect(weekLabel("2026-09-07")).toBe("07 Sep");
  });
});

describe("isoWeekNo", () => {
  it("counts the weeks of an ordinary year", () => {
    expect(isoWeekNo("2026-01-05")).toBe(2);
    expect(isoWeekNo("2026-08-31")).toBe(36);
  });

  it("gives the first week to the one holding the year's first Thursday", () => {
    // 2026-01-01 is a Thursday, so the week that starts 2025-12-29 is 2026-W01.
    expect(isoWeekNo("2025-12-29")).toBe(1);
  });

  it("hands a December week to the next year when it holds January's Thursday", () => {
    // 2019-12-30 is 2020-W01: its Thursday, 2020-01-02, belongs to 2020.
    expect(isoWeekNo("2019-12-30")).toBe(1);
  });

  it("counts a fifty-third week in a year that has one", () => {
    expect(isoWeekNo("2015-12-28")).toBe(53);
  });
});

describe("sharedColumnPx", () => {
  it("splits the room left after the week column and the gutter", () => {
    // 1000 - 54 - 34 = 912, over four columns.
    expect(sharedColumnPx(1000, 4, 1, 60)).toBe(228);
  });

  it("never squeezes a column below what its text needs", () => {
    // Ten columns would take 91px each; a column stays readable instead and
    // the board scrolls sideways.
    expect(sharedColumnPx(1000, 10, 1, 60)).toBe(BASE_COL);
  });

  it("scales the shared width by the zoom", () => {
    expect(sharedColumnPx(1000, 4, 2, 60)).toBe(456);
    expect(sharedColumnPx(1000, 10, 0.5, 60)).toBe(70);
  });

  it("stops at the floor, however far the reader zooms out", () => {
    expect(sharedColumnPx(1000, 10, 0.3, 60)).toBe(60);
  });

  it("has a width even with no columns to share it", () => {
    expect(sharedColumnPx(1000, 0, 1, 60)).toBe(BASE_COL);
  });

  it("survives a board that has not been measured yet", () => {
    expect(sharedColumnPx(0, 4, 1, 60)).toBe(BASE_COL);
  });
});

describe("rowPx", () => {
  it("is the base height at rest", () => {
    expect(rowPx(1)).toBe(BASE_ROW);
  });

  it("rounds to whole pixels, so the rows do not drift apart", () => {
    expect(rowPx(1.5)).toBe(42);
    expect(rowPx(0.55)).toBe(15);
  });

  it("never lets a row collapse to nothing", () => {
    expect(rowPx(0.1)).toBe(12);
  });
});

describe("extentOf", () => {
  const weeks = weekWindow([], MON, 0, 0); // 2026-08-17 … 2026-10-26

  it("puts a card in the row of its week", () => {
    expect(extentOf({ week: "2026-08-31" }, weeks)).toEqual({ row: 2, span: 1 });
  });

  it("starts a card where its start date says, not where its week does", () => {
    // The start date is the truth; the week is only a fallback for a card
    // that never got one.
    expect(extentOf({ week: "2026-08-31", startDate: "2026-09-14" }, weeks)).toEqual({
      row: 4,
      span: 1,
    });
  });

  it("covers every week a stretched card reaches", () => {
    expect(extentOf({ week: "2026-08-31", day: "2026-09-18" }, weeks)).toEqual({
      row: 2,
      span: 3,
    });
  });

  it("gives one row to a card whose end falls in its own week", () => {
    expect(extentOf({ week: "2026-08-31", day: "2026-09-04" }, weeks)).toEqual({
      row: 2,
      span: 1,
    });
  });

  it("gives one row to a card whose end was left behind its start", () => {
    expect(extentOf({ week: "2026-08-31", day: "2026-08-20" }, weeks)).toEqual({
      row: 2,
      span: 1,
    });
  });

  it("stops a card that would run past the last week at the edge", () => {
    expect(extentOf({ week: "2026-10-19", day: "2026-12-25" }, weeks)).toEqual({
      row: 9,
      span: 2,
    });
  });

  it("does not place a card standing before the window", () => {
    expect(extentOf({ week: "2026-08-10" }, weeks)).toBeNull();
  });

  it("does not place a card standing after it", () => {
    expect(extentOf({ week: "2026-11-02" }, weeks)).toBeNull();
  });

  it("does not place a card with no dates", () => {
    expect(extentOf({}, weeks)).toBeNull();
  });

  it("places nothing on a grid with no weeks", () => {
    expect(extentOf({ week: MON }, [])).toBeNull();
  });

  it("takes the first day of the week a mid-week date falls in", () => {
    expect(extentOf({ startDate: "2026-09-02" }, weeks)).toEqual({ row: 2, span: 1 });
  });
});

describe("packLanes", () => {
  interface Slot extends Laned {
    id: string;
  }
  const slot = (id: string, row: number, span: number): Slot => ({
    id,
    row,
    span,
    lane: 0,
    lanes: 1,
    width: 1,
  });
  /** How the column reads once packed: who sits where, out of how many, and
   *  how wide. */
  const packed = (list: Slot[]) =>
    list.map((s) => [s.id, s.lane, s.lanes, s.width] as [string, number, number, number]);

  it("gives the whole column to cards that never share a week", () => {
    const list = [slot("a", 0, 1), slot("b", 2, 1), slot("c", 4, 1)];
    packLanes([list]);
    expect(packed(list)).toEqual([
      ["a", 0, 1, 1],
      ["b", 0, 1, 1],
      ["c", 0, 1, 1],
    ]);
  });

  it("splits the column between two cards that share a week", () => {
    const list = [slot("a", 0, 2), slot("b", 1, 2)];
    packLanes([list]);
    expect(packed(list)).toEqual([
      ["a", 0, 2, 1],
      ["b", 1, 2, 1],
    ]);
  });

  it("splits only the cluster that overlaps, not the whole column", () => {
    // The pair at the top has to share; the card five weeks later does not
    // pay for it.
    const list = [slot("a", 0, 2), slot("b", 1, 1), slot("late", 5, 1)];
    packLanes([list]);
    expect(packed(list)).toEqual([
      ["a", 0, 2, 1],
      ["b", 1, 2, 1],
      ["late", 0, 1, 1],
    ]);
  });

  it("gives the first lane to the longer card when two start together", () => {
    const list = [slot("short", 0, 1), slot("long", 0, 3)];
    packLanes([list]);
    // Sorted longest-first, so the list itself comes back reordered.
    expect(packed(list)).toEqual([
      ["long", 0, 2, 1],
      ["short", 1, 2, 1],
    ]);
  });

  it("grows a card over a lane that is free for every week it covers", () => {
    // Three lanes are needed at the top; the card that starts in week 2 has
    // the third lane to itself for both of its weeks, and takes it — sitting
    // at a third of the width beside empty space is the bug this prevents.
    const list = [slot("long", 0, 5), slot("a", 0, 1), slot("b", 0, 1), slot("later", 2, 2)];
    packLanes([list]);
    expect(packed(list)).toEqual([
      ["long", 0, 3, 1],
      ["a", 1, 3, 1],
      ["b", 2, 3, 1],
      ["later", 1, 3, 2],
    ]);
  });

  it("packs each column on its own", () => {
    const left = [slot("a", 0, 2), slot("b", 0, 2)];
    const right = [slot("c", 0, 2)];
    packLanes([left, right]);
    expect(packed(left)).toEqual([
      ["a", 0, 2, 1],
      ["b", 1, 2, 1],
    ]);
    expect(packed(right)).toEqual([["c", 0, 1, 1]]);
  });

  it("has nothing to say about an empty column", () => {
    const list: Slot[] = [];
    expect(() => packLanes([list])).not.toThrow();
    expect(list).toEqual([]);
  });

  describe("with a card pinned to its lane", () => {
    const pinnedTo = (id: string, lane: number, row: number, span: number) => ({
      is: (s: Slot) => s.id === id,
      lane,
      row,
      span,
    });

    it("keeps the pinned card where it was, and packs the rest around it", () => {
      // While an edge is being pulled the slot must not hop lanes under the
      // pointer: "b" holds lane 0 even though "a" would have taken it.
      const list = [slot("a", 0, 2), slot("b", 0, 2)];
      packLanes([list], pinnedTo("b", 0, 0, 2));
      expect(packed(list)).toEqual([
        ["a", 1, 2, 1],
        ["b", 0, 2, 1],
      ]);
    });

    it("leaves the pinned lane empty for a card that shares its weeks", () => {
      // The pinned card is not in this column's list at all — the lane is
      // still held for it, or the neighbour would be drawn under it.
      const list = [slot("c", 1, 1)];
      packLanes([list], pinnedTo("pinned", 0, 0, 2));
      expect(packed(list)).toEqual([["c", 1, 2, 1]]);
    });

    it("lets a card that shares none of its weeks use the pinned lane", () => {
      const list = [slot("c", 4, 1)];
      packLanes([list], pinnedTo("pinned", 0, 0, 2));
      expect(packed(list)).toEqual([["c", 0, 1, 1]]);
    });
  });
});
