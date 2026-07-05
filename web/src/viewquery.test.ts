import { describe, expect, it } from "vitest";

import { queryString, viewQueries, watchQuery } from "./viewquery";

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
