import { describe, expect, it } from "vitest";

import { queryString, viewQueries, watchQueries, watchQuery } from "./viewquery";

describe("viewQueries", () => {
  it("scopes the Me board to the day, with reviews and no user or team", () => {
    const qs = viewQueries("me", "2026-07-04", ["alpha", "beta"]);
    expect(qs).toEqual([{ view: "me", day: "2026-07-04", reviews: "true" }]);
  });

  it("sends the impersonated user explicitly on the Me board (view-as)", () => {
    const qs = viewQueries("me", "2026-07-04", [], "lllamnyp");
    expect(qs).toEqual([
      { view: "me", day: "2026-07-04", reviews: "true", user: "lllamnyp" },
    ]);
  });

  it("fetches the Team board as the day grid PLUS the weekly plan", () => {
    const qs = viewQueries("team", "2026-07-04", ["alpha", "beta"]);
    expect(qs).toEqual([
      { view: "team", team: "alpha,beta", day: "2026-07-04", reviews: "true" },
      { view: "weekly", team: "alpha,beta", week: "2026-06-29" },
    ]);
  });

  it("sends an empty team set when the Team board shows no teams", () => {
    expect(viewQueries("team", "2026-07-04", [])[0].team).toBe("");
  });

  it("fetches the personal board beside the Me view when one is linked, on the same day", () => {
    // The personal column follows the day being looked at: a card sent to
    // tomorrow shows up when the board is flipped to tomorrow.
    expect(viewQueries("me", "2026-07-04", [], undefined, true)).toEqual([
      { view: "me", day: "2026-07-04", reviews: "true" },
      { view: "personal", day: "2026-07-04" },
    ]);
  });

  it("keeps the viewer's own personal board beside someone else's day while impersonating", () => {
    // The personal query names no user: the server answers with the
    // caller's own board, whoever the day board is viewed as.
    expect(viewQueries("me", "2026-07-04", [], "lllamnyp", true)).toEqual([
      { view: "me", day: "2026-07-04", reviews: "true", user: "lllamnyp" },
      { view: "personal", day: "2026-07-04" },
    ]);
    expect(watchQueries("me", "2026-07-04", [], "lllamnyp", true)).toEqual([
      watchQuery("me", "2026-07-04", [], "lllamnyp"),
      { view: "personal", day: "2026-07-04" },
    ]);
  });

  it("fetches nothing personal without a personal board, or off the Me board", () => {
    expect(viewQueries("me", "2026-07-04", [], undefined, false)).toHaveLength(1);
    expect(viewQueries("team", "2026-07-04", ["alpha"], undefined, true)).toEqual(
      viewQueries("team", "2026-07-04", ["alpha"]),
    );
  });
});

describe("watchQueries", () => {
  it("watches the Me selection alone without a personal board", () => {
    expect(watchQueries("me", "2026-07-04", [], undefined, false)).toEqual([
      watchQuery("me", "2026-07-04", []),
    ]);
  });

  it("adds the personal selection on the Me board when one is linked, on the same day", () => {
    expect(watchQueries("me", "2026-07-04", [], undefined, true)).toEqual([
      watchQuery("me", "2026-07-04", []),
      { view: "personal", day: "2026-07-04" },
    ]);
  });

  it("does not watch it on the other boards", () => {
    expect(watchQueries("team", "2026-07-04", ["alpha"], undefined, true)).toEqual([
      watchQuery("team", "2026-07-04", ["alpha"]),
    ]);
    expect(watchQueries("project", "2026-07-04", [], undefined, true)).toHaveLength(1);
  });
});

describe("watchQuery", () => {
  it("watches every card of the shown teams in Team mode (grid + plan)", () => {
    expect(watchQuery("team", "2026-07-04", ["alpha", "beta"])).toEqual({
      view: "all",
      team: "alpha,beta",
    });
  });

  it("watches the personal day selection in Me mode, honouring view-as", () => {
    expect(watchQuery("me", "2026-07-04", [], "lllamnyp")).toEqual({
      view: "me",
      day: "2026-07-04",
      reviews: "true",
      user: "lllamnyp",
    });
  });
});

describe("queryString", () => {
  it("serialises with a stable (sorted) key order and encodes values", () => {
    const q = watchQuery("team", "2026-07-04", ["a b", "c"]);
    expect(queryString(q)).toBe("team=a%20b%2Cc&view=all");
  });

  it("is identical for equal selectors, so the watch does not re-subscribe", () => {
    const a = queryString(watchQuery("me", "2026-07-04", []));
    const b = queryString(watchQuery("me", "2026-07-04", ["ignored-for-me"]));
    expect(a).toBe(b);
  });
});
