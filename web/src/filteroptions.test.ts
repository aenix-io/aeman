import { describe, expect, it } from "vitest";

import { customerOptions, projectOptions, teamOptions } from "./filteroptions";

// What each board's filter dropdown offers. The no-team group is a team like
// any other (O11): "No team" is an option, the empty value, last — on every
// board that filters by team. A project filter offers "No project" only
// where loose columns exist.
describe("the filter dropdowns' options", () => {
  it("offer the teams and No team", () => {
    expect(teamOptions(["portal", "ops"])).toEqual([
      { value: "portal", label: "portal" },
      { value: "ops", label: "ops" },
      { value: "", label: "No team" },
    ]);
  });

  it("offer the customers by name, keyed by id", () => {
    expect(customerOptions([{ id: "cu1", name: "Hidora" }])).toEqual([{ value: "cu1", label: "Hidora" }]);
    expect(customerOptions(undefined)).toEqual([]);
  });

  it("offer No project only when loose columns exist", () => {
    expect(projectOptions(["core"], false)).toEqual([{ value: "core", label: "core" }]);
    expect(projectOptions(["core"], true)).toEqual([
      { value: "core", label: "core" },
      { value: "", label: "No project" },
    ]);
  });
});
