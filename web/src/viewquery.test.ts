import { describe, expect, it } from "vitest";

import { addDays, todayIso } from "./date";
import {
  queryString,
  snapshotDay,
  storedView,
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
    const qs = viewQueries("me", TODAY, ["alpha", "beta"], undefined, TODAY);
    expect(qs).toEqual([{ view: "me", day: TODAY, reviews: "true" }]);
  });

  it("sends the impersonated user explicitly on the Me board (view-as)", () => {
    const qs = viewQueries("me", TODAY, [], "lllamnyp", TODAY);
    expect(qs).toEqual([
      { view: "me", day: TODAY, reviews: "true", user: "lllamnyp" },
    ]);
  });

  it("fetches the Team board as the day grid of the shown teams", () => {
    const qs = viewQueries("team", TODAY, ["alpha", "beta"], undefined, TODAY);
    expect(qs).toEqual([
      { view: "team", team: "alpha,beta", day: TODAY, reviews: "true" },
    ]);
  });

  it("sends an empty team set when the Team board shows no teams", () => {
    expect(viewQueries("team", TODAY, [], undefined, TODAY)[0].team).toBe("");
  });
});

// Going back a day asks for the board OF that day — the server reads it from
// the board's history instead of filtering today's cards by that day's dates.
describe("snapshot selectors", () => {
  const PAST = "2026-07-01";

  it("asks for a past day as it was", () => {
    const qs = viewQueries("team", PAST, ["portal"], undefined, TODAY);
    expect(qs).toEqual([
      { view: "team", team: "portal", day: PAST, reviews: "true", snapshot: "1" },
    ]);
  });

  it("leaves today and tomorrow live — one is happening, the other has not", () => {
    for (const day of [TODAY, "2026-07-05"]) {
      for (const q of viewQueries("team", day, ["portal"], undefined, TODAY)) {
        expect(q.snapshot).toBeUndefined();
      }
      for (const q of viewQueries("me", day, [], undefined, TODAY)) {
        expect(q.snapshot).toBeUndefined();
      }
    }
  });

  it("knows which days have a snapshot at all", () => {
    expect(snapshotDay("team", PAST, TODAY)).toBe(true);
    expect(snapshotDay("me", PAST, TODAY)).toBe(true);
    expect(snapshotDay("me", TODAY, TODAY)).toBe(false);
    expect(snapshotDay("me", "2026-07-05", TODAY)).toBe(false);
    // The Project board is not a day board: a day means nothing
    // there, so a snapshot of one would be a claim about nothing.
    expect(snapshotDay("project", PAST, TODAY)).toBe(false);
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
    expect(viewQueries("me", yesterday, [])[0].snapshot).toBe("1");
  });

  // The watch is a live stream; a snapshot is not watched at all (the day is
  // over), so its selectors stay as they were.
  it("does not put the flag on a watch selector", () => {
    for (const q of watchQueries("team", PAST, ["portal"], undefined)) {
      expect(q.snapshot).toBeUndefined();
    }
  });
});

describe("watchQueries", () => {
  it("watches the view's own selection", () => {
    expect(watchQueries("me", TODAY, [])).toEqual([watchQuery("me", TODAY, [])]);
  });
});

describe("watchQuery", () => {
  it("watches every card of the shown teams in Team mode (grid + plan)", () => {
    expect(watchQuery("team", TODAY, ["alpha", "beta"])).toEqual({
      view: "all",
      team: "alpha,beta",
    });
  });

  it("watches the Me day selection, honouring view-as", () => {
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

// The Backlog board lists each selected team's parked open tasks, in the
// server's BacklogOrder (K2) — and it WATCHES what the Team board watches,
// every card of those teams (K6): a card parked, taken into work, re-teamed or
// finished by anybody is a card that enters or leaves a backlog, and only a
// watch wider than the listing hears the leaving.
describe("the Backlog board", () => {
  it("lists the backlog view of the shown teams, and nothing about a day", () => {
    expect(viewQueries("backlog", TODAY, ["alpha", "beta"], undefined, TODAY)).toEqual([
      { view: "backlog", team: "alpha,beta" },
    ]);
  });

  it("names the no-team group as the empty member of the team set", () => {
    const [q] = viewQueries("backlog", TODAY, ["", "portal"], undefined, TODAY);
    expect(q.team).toBe(",portal");
  });

  it("is never a snapshot: a past day changes nothing about a backlog", () => {
    const [q] = viewQueries("backlog", "2026-06-01", ["alpha"], undefined, TODAY);
    expect(q.snapshot).toBeUndefined();
    expect(snapshotDay("backlog", "2026-06-01", TODAY)).toBe(false);
  });

  it("watches every card of its teams, the Team board's watch (K6)", () => {
    expect(watchQuery("backlog", TODAY, ["alpha", "beta"])).toEqual({ view: "all", team: "alpha,beta" });
    expect(watchQueries("backlog", TODAY, ["alpha"])).toEqual(watchQueries("team", TODAY, ["alpha"]));
  });

  // K9: there is no Triage view to ask for any more.
  it("asks for no week window: from and weeks are gone (K9)", () => {
    for (const q of viewQueries("backlog", TODAY, ["alpha"], undefined, TODAY)) {
      expect(q.from).toBeUndefined();
      expect(q.weeks).toBeUndefined();
    }
  });
});

// The board is a path segment, and what narrows it stays a query — the fetch
// and the watch address the same board the same way, off the same string.
describe("where a selector is fetched", () => {
  it("puts the board in the path and leaves the rest", () => {
    expect(viewPath(queryString({ view: "backlog", team: "portal" }), "cards")).toBe(
      "/views/backlog/cards?team=portal",
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

// The tab this browser remembers. A name a board used to have still lands on
// the board that carries it now — and the retired Triage lands on the Backlog
// that replaced it (K9), not silently on Me.
describe("the remembered tab", () => {
  it("lands a remembered Triage on the Backlog (K9)", () => {
    expect(storedView("triage")).toBe("backlog");
    expect(storedView("backlog")).toBe("backlog");
  });

  it("keeps the older renames, and falls back to Me", () => {
    expect(storedView("plan")).toBe("project");
    expect(storedView("nixon")).toBe("team");
    expect(storedView("roadmap")).toBe("roadmap");
    expect(storedView(null)).toBe("me");
    expect(storedView("whatever")).toBe("me");
  });
});

// The Roadmap's "Project cards" toggle is a SELECTOR the server answers
// (projectCards), for the listing and the watch alike — not a filter the SPA
// runs over what it was sent. Off, nothing is asked and the server leaves
// them out; on, both ask for them, so turning it on refetches and
// re-subscribes rather than redrawing what the board already holds.
describe("the Roadmap's project cards", () => {
  it("asks for nothing while the toggle is off", () => {
    expect(viewQueries("roadmap", TODAY, [], undefined, TODAY)).toEqual([{ view: "roadmap" }]);
    expect(watchQuery("roadmap", TODAY, [])).toEqual({ view: "roadmap" });
  });

  it("asks the listing and the watch for them while it is on", () => {
    expect(viewQueries("roadmap", TODAY, [], undefined, TODAY, { projectCards: true })).toEqual([
      { view: "roadmap", projectCards: "true" },
    ]);
    expect(watchQueries("roadmap", TODAY, [], undefined, { projectCards: true })).toEqual([
      { view: "roadmap", projectCards: "true" },
    ]);
  });

  it("changes the keys the board refetches and re-subscribes on", () => {
    const off = queryString(viewQueries("roadmap", TODAY, [], undefined, TODAY)[0]);
    const on = queryString(viewQueries("roadmap", TODAY, [], undefined, TODAY, { projectCards: true })[0]);
    expect(on).not.toBe(off);
  });

  it("means nothing on any other board", () => {
    expect(viewQueries("backlog", TODAY, ["alpha"], undefined, TODAY, { projectCards: true })).toEqual([
      { view: "backlog", team: "alpha" },
    ]);
    expect(watchQuery("project", TODAY, [], undefined, { projectCards: true })).toEqual({ view: "project" });
  });
});

// The Roadmap's team, customer and owner filters (ownership.md, O7, O8) are
// the server's: they ride the listing and the watch as selectors, so a
// change refetches and re-subscribes; nothing is filtered locally.
describe("the Roadmap's filters", () => {
  it("ask nothing while none is chosen", () => {
    expect(viewQueries("roadmap", TODAY, [], undefined, TODAY, { filters: { teams: [], customers: [], owners: [] } })).toEqual([
      { view: "roadmap" },
    ]);
  });

  it("ask the listing and the watch for the chosen ones, each a set", () => {
    const opts = { filters: { teams: ["portal", "ops"], customers: ["cu1"], owners: ["ann"] } };
    const want = { view: "roadmap", team: "portal,ops", customer: "cu1", owner: "ann" };
    expect(viewQueries("roadmap", TODAY, [], undefined, TODAY, opts)).toEqual([want]);
    expect(watchQuery("roadmap", TODAY, [], undefined, opts)).toEqual(want);
  });

  it("ride along with the project cards", () => {
    expect(watchQuery("roadmap", TODAY, [], undefined, { projectCards: true, filters: { teams: [], customers: [], owners: ["ann"] } })).toEqual({
      view: "roadmap",
      projectCards: "true",
      owner: "ann",
    });
  });
});

// O11 — the no-team group is the empty member of every team set, so the set
// holding it ALONE is written "," — an empty `team=` would ask for every
// team. And the Team board's and the Backlog's Customer filter rides their
// listing and watch; the Me board filters what it holds itself, since it
// counts what its filters hide.
describe("the day boards' team and customer sets", () => {
  it("write the Roadmap's No team alone as ',' too", () => {
    expect(watchQuery("roadmap", TODAY, [], undefined, { filters: { teams: [""], customers: [], owners: [] } }).team).toBe(",");
  });

  it("write the no-team group alone as ','", () => {
    expect(viewQueries("backlog", TODAY, [""], undefined, TODAY)[0].team).toBe(",");
    expect(viewQueries("team", TODAY, [""], undefined, TODAY)[0].team).toBe(",");
    expect(watchQuery("team", TODAY, [""])).toEqual({ view: "all", team: "," });
  });

  it("ask the Team board's and the Backlog's listing and watch for the chosen customers", () => {
    const opts = { customers: ["cu1", "cu2"] };
    expect(viewQueries("backlog", TODAY, ["alpha"], undefined, TODAY, opts)).toEqual([
      { view: "backlog", team: "alpha", customer: "cu1,cu2" },
    ]);
    expect(viewQueries("team", TODAY, ["alpha"], undefined, TODAY, opts)[0].customer).toBe("cu1,cu2");
    expect(watchQuery("backlog", TODAY, ["alpha"], undefined, opts)).toEqual({ view: "all", team: "alpha", customer: "cu1,cu2" });
  });

  it("leave the Me board's listing and watch whole", () => {
    const opts = { customers: ["cu1"] };
    expect(viewQueries("me", TODAY, ["alpha"], undefined, TODAY, opts)[0].customer).toBeUndefined();
    expect(watchQuery("me", TODAY, ["alpha"], undefined, opts).customer).toBeUndefined();
  });

  it("ask nothing about customers while none is chosen", () => {
    expect(viewQueries("backlog", TODAY, ["alpha"], undefined, TODAY, { customers: [] })[0].customer).toBeUndefined();
  });
});
