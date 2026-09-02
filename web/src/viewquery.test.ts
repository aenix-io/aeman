import { describe, expect, it } from "vitest";

import { addDays, todayIso } from "./date";
import {
  queryString,
  snapshotDay,
  viewQueries,
  watchQueries,
  watchQuery,
} from "./viewquery";

// The day these tests call "today": the selectors of a LIVE board are what
// most of them are about, and a fixed today keeps them from turning into
// snapshot selectors as the calendar moves past the dates they use.
const TODAY = "2026-07-04";

describe("viewQueries", () => {
  it("scopes the Me board to the day, with reviews and no user or team", () => {
    const qs = viewQueries("me", TODAY, ["alpha", "beta"], undefined, false, TODAY);
    expect(qs).toEqual([{ view: "me", day: TODAY, reviews: "true" }]);
  });

  it("sends the impersonated user explicitly on the Me board (view-as)", () => {
    const qs = viewQueries("me", TODAY, [], "lllamnyp", false, TODAY);
    expect(qs).toEqual([
      { view: "me", day: TODAY, reviews: "true", user: "lllamnyp" },
    ]);
  });

  it("fetches the Team board as the day grid of the shown teams", () => {
    const qs = viewQueries("team", TODAY, ["alpha", "beta"], undefined, false, TODAY);
    expect(qs).toEqual([
      { view: "team", team: "alpha,beta", day: TODAY, reviews: "true" },
    ]);
  });

  it("sends an empty team set when the Team board shows no teams", () => {
    expect(viewQueries("team", TODAY, [], undefined, false, TODAY)[0].team).toBe("");
  });

  it("fetches the personal board beside the Me view when one is linked, on the same day", () => {
    // The personal column follows the day being looked at: a card sent to
    // tomorrow shows up when the board is flipped to tomorrow.
    expect(viewQueries("me", TODAY, [], undefined, true, TODAY)).toEqual([
      { view: "me", day: TODAY, reviews: "true" },
      { view: "personal", day: TODAY },
    ]);
  });

  it("leaves the personal board out while impersonating — it is the viewer's own, not theirs", () => {
    expect(viewQueries("me", TODAY, [], "lllamnyp", true, TODAY)).toEqual([
      { view: "me", day: TODAY, reviews: "true", user: "lllamnyp" },
    ]);
  });

  it("fetches nothing personal without a personal board, or off the Me board", () => {
    expect(viewQueries("me", TODAY, [], undefined, false, TODAY)).toHaveLength(1);
    expect(
      viewQueries("team", TODAY, ["alpha"], undefined, true, TODAY),
    ).toEqual(viewQueries("team", TODAY, ["alpha"], undefined, false, TODAY));
  });
});

// Going back a day asks for the board OF that day — the server reads it from
// the board's history instead of filtering today's cards by that day's dates.
describe("snapshot selectors", () => {
  const PAST = "2026-07-01";

  it("asks for a past day as it was", () => {
    const qs = viewQueries("team", PAST, ["portal"], undefined, false, TODAY);
    expect(qs).toEqual([
      { view: "team", team: "portal", day: PAST, reviews: "true", snapshot: "1" },
    ]);
  });

  it("asks for the Me board of a past day, personal column included", () => {
    const qs = viewQueries("me", PAST, [], undefined, true, TODAY);
    expect(qs[0]).toMatchObject({ view: "me", day: PAST, snapshot: "1" });
    expect(qs[1]).toMatchObject({ view: "personal", day: PAST, snapshot: "1" });
  });

  it("leaves today and tomorrow live — one is happening, the other has not", () => {
    for (const day of [TODAY, "2026-07-05"]) {
      for (const q of viewQueries("team", day, ["portal"], undefined, false, TODAY)) {
        expect(q.snapshot).toBeUndefined();
      }
      for (const q of viewQueries("me", day, [], undefined, true, TODAY)) {
        expect(q.snapshot).toBeUndefined();
      }
    }
  });

  it("knows which days have a snapshot at all", () => {
    expect(snapshotDay("team", PAST, TODAY)).toBe(true);
    expect(snapshotDay("me", PAST, TODAY)).toBe(true);
    expect(snapshotDay("me", TODAY, TODAY)).toBe(false);
    expect(snapshotDay("me", "2026-07-05", TODAY)).toBe(false);
    // The Project and Process boards are not day boards: a day means nothing
    // there, so a snapshot of one would be a claim about nothing.
    expect(snapshotDay("project", PAST, TODAY)).toBe(false);
    expect(snapshotDay("process", PAST, TODAY)).toBe(false);
  });

  // The app calls these without a `today` — the fixed one above keeps the
  // other cases honest, but it would also hide a broken default. Yesterday
  // is a past day by the real clock too.
  it("uses the real today when none is given", () => {
    const yesterday = addDays(todayIso(), -1);
    expect(snapshotDay("team", yesterday)).toBe(true);
    expect(snapshotDay("me", yesterday)).toBe(true);
    expect(snapshotDay("me", todayIso())).toBe(false);
    expect(viewQueries("team", yesterday, ["portal"])[0].snapshot).toBe("1");
    expect(viewQueries("me", yesterday, [], undefined, true)[1].snapshot).toBe("1");
  });

  // The watch is a live stream; a snapshot is not watched at all (the day is
  // over), so its selectors stay as they were.
  it("does not put the flag on a watch selector", () => {
    for (const q of watchQueries("team", PAST, ["portal"], undefined, true)) {
      expect(q.snapshot).toBeUndefined();
    }
  });
});

describe("watchQueries", () => {
  it("watches the Me selection alone without a personal board", () => {
    expect(watchQueries("me", TODAY, [], undefined, false)).toEqual([
      watchQuery("me", TODAY, []),
    ]);
  });

  it("adds the personal selection on the Me board when one is linked, on the same day", () => {
    expect(watchQueries("me", TODAY, [], undefined, true)).toEqual([
      watchQuery("me", TODAY, []),
      { view: "personal", day: TODAY },
    ]);
  });

  it("does not watch it while impersonating or on the other boards", () => {
    expect(watchQueries("me", TODAY, [], "lllamnyp", true)).toHaveLength(1);
    expect(watchQueries("team", TODAY, ["alpha"], undefined, true)).toEqual([
      watchQuery("team", TODAY, ["alpha"]),
    ]);
  });
});

describe("watchQuery", () => {
  it("watches every card of the shown teams in Team mode (grid + plan)", () => {
    expect(watchQuery("team", TODAY, ["alpha", "beta"])).toEqual({
      view: "all",
      team: "alpha,beta",
    });
  });

  it("watches the personal day selection in Me mode, honouring view-as", () => {
    expect(watchQuery("me", TODAY, [], "lllamnyp")).toEqual({
      view: "me",
      day: TODAY,
      reviews: "true",
      user: "lllamnyp",
    });
  });
});

describe("queryString", () => {
  it("serialises with a stable (sorted) key order and encodes values", () => {
    const q = watchQuery("team", TODAY, ["a b", "c"]);
    expect(queryString(q)).toBe("team=a%20b%2Cc&view=all");
  });

  it("is identical for equal selectors, so the watch does not re-subscribe", () => {
    const a = queryString(watchQuery("me", TODAY, []));
    const b = queryString(watchQuery("me", TODAY, ["ignored-for-me"]));
    expect(a).toBe(b);
  });
});
