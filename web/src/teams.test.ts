import { describe, expect, it } from "vitest";

import { pruneTeamFilter, settlePendingTeams, teamRoster } from "./teams";

describe("pruneTeamFilter", () => {
  const teams = ["alpha", "beta"];

  it("drops filter entries no team on the board backs, keeping the order of the rest", () => {
    expect(pruneTeamFilter(["beta", "cozystack", "alpha"], teams)).toEqual(["beta", "alpha"]);
  });

  it('keeps "" — the no-team group is always a valid selection', () => {
    expect(pruneTeamFilter(["", "gone"], teams)).toEqual([""]);
    expect(pruneTeamFilter([""], [])).toEqual([""]);
  });

  it("turns a filter emptied by pruning into null (= all)", () => {
    expect(pruneTeamFilter(["cozystack"], teams)).toBeNull();
    expect(pruneTeamFilter(["cozystack"], [])).toBeNull();
  });

  it("leaves null (= all) alone", () => {
    expect(pruneTeamFilter(null, teams)).toBeNull();
    expect(pruneTeamFilter(null, [])).toBeNull();
  });

  it("returns the very same array when nothing is dropped, so a state setter can bail out", () => {
    const filter = ["alpha", ""];
    expect(pruneTeamFilter(filter, teams)).toBe(filter);
  });

  it("an empty filter is null too, not an empty selection that would blank the board", () => {
    expect(pruneTeamFilter([], teams)).toBeNull();
  });
});

describe("teamRoster", () => {
  it("is the board's declared teams, in the server's order", () => {
    expect(teamRoster(["beta", "alpha"], [])).toEqual(["beta", "alpha"]);
  });

  it("follows with the teams just added here that the board does not declare yet, in the order they were added", () => {
    expect(teamRoster(["alpha"], ["new", "newer"])).toEqual(["alpha", "new", "newer"]);
  });

  it("names a team once — a pending team the board already declares keeps the server's slot", () => {
    expect(teamRoster(["alpha", "new"], ["new", "alpha", "other", "other"])).toEqual([
      "alpha",
      "new",
      "other",
    ]);
  });

  it('skips blanks — "" is the no-team group, not a team', () => {
    expect(teamRoster(["alpha", ""], [""])).toEqual(["alpha"]);
  });

  it("is empty for an empty board with nothing added", () => {
    expect(teamRoster([], [])).toEqual([]);
  });
});

describe("settlePendingTeams", () => {
  it("drops the teams the board now declares — the server has caught up", () => {
    expect(settlePendingTeams(["new", "later"], ["alpha", "new"])).toEqual(["later"]);
  });

  it("returns the very same array while none has landed, so a state setter can bail out", () => {
    const pending = ["new"];
    expect(settlePendingTeams(pending, ["alpha"])).toBe(pending);
    expect(settlePendingTeams(pending, [])).toBe(pending);
  });

  it("is empty once everything landed", () => {
    expect(settlePendingTeams(["new"], ["new"])).toEqual([]);
    expect(settlePendingTeams([], ["new"])).toEqual([]);
  });
});
