import { describe, expect, it } from "vitest";

import { addDays, todayIso } from "./date";
import { WEEKS_FWD } from "./weekgrid";
import {
  queryString,
  snapshotDay,
  TRIAGE_WEEKS,
  viewPath,
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

// The Triage board asks for exactly the weeks it draws.
//
// They were two separate numbers — six fetched, nine drawn — and the rows past
// the window were a trap: a card dropped in one carried a week the listing
// does not return, so after the next reload it stood on no board at all. Not
// Triage (outside the window), not the day boards (a week ahead is on none),
// not Project (no column). A live card openable only by its uid.
describe("the Triage window", () => {
  it("covers every row the grid draws", () => {
    // The grid runs from this Monday through WEEKS_FWD after it, inclusive.
    expect(TRIAGE_WEEKS).toBe(WEEKS_FWD + 1);
  });

  it("asks the server for that window, starting at this Monday", () => {
    const [q] = viewQueries("triage", TODAY, ["alpha"], undefined, false, TODAY);
    expect(q.view).toBe("triage");
    expect(q.weeks).toBe(String(TRIAGE_WEEKS));
  });

  it("asks for the review cards too, and hides them on the board instead", () => {
    // The board offers to draw reviews. Fetching them once and hiding them
    // client-side is what makes the toggle instant — and keeps the watch
    // scope from changing under a board that is being read.
    const [q] = viewQueries("triage", TODAY, ["alpha"], undefined, false, TODAY);
    expect(q.reviews).toBe("true");
  });

  it("asks for the parked work as well — no week puts it in no window", () => {
    // A parked card has no week at all, so the weeks query cannot reach it
    // however wide the window is. The drawer is fetched beside the grid
    // rather than when it opens, so its counts are right before it does.
    const qs = viewQueries("triage", TODAY, ["alpha", "beta"], undefined, false, TODAY);
    const parked = qs.find((q) => q.view === "backlog");
    expect(parked).toBeDefined();
    expect(parked?.team).toBe("alpha,beta");
    // And it asks for every list the teams have, not one of them.
    expect(parked?.backlog).toBeUndefined();
  });
});

// The board is a path segment, and what narrows it stays a query — the fetch
// and the watch address the same board the same way, off the same string.
describe("where a selector is fetched", () => {
  it("puts the board in the path and leaves the rest", () => {
    expect(viewPath(queryString({ view: "triage", team: "portal", weeks: "9" }), "cards")).toBe(
      "/views/triage/cards?team=portal&weeks=9",
    );
    expect(viewPath(queryString({ view: "me", day: "2026-09-11" }), "watch")).toBe(
      "/views/me/watch?day=2026-09-11",
    );
  });

  it("needs no query at all", () => {
    expect(viewPath(queryString({ view: "project" }), "cards")).toBe("/views/project/cards");
  });

  // Nothing in the app sends a selector with no board; answering "all" is what
  // the bare collection used to do, minus the guessing.
  it("falls back to the escape hatch", () => {
    expect(viewPath("", "cards")).toBe("/views/all/cards");
  });
});
