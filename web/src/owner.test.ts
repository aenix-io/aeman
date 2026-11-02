import { describe, expect, it } from "vitest";

import { ownerLabel, ownerOf, ownerStep, peopleOfTeam } from "./owner";

// The owner picker's rule, press by press: a team is not an answer by
// itself, a person needs a team, a team alone and nobody at all are answers.

describe("the owner picker's presses", () => {
  it("keeps the menu open on a team: a team is not an answer by itself", () => {
    const s = ownerStep("", { kind: "team", team: "core" });
    expect(s).toEqual({ team: "core", answer: null });
  });

  it("refuses a person before a team", () => {
    const s = ownerStep("", { kind: "person", person: "ann" });
    expect(s.answer).toBeNull();
    expect(s.refused).toMatch(/team/);
    expect(ownerStep("", { kind: "no-person" }).answer).toBeNull();
  });

  it("answers with the team and the person once both are picked", () => {
    expect(ownerStep("core", { kind: "person", person: "ann" })).toEqual({
      team: "core",
      answer: { team: "core", person: "ann" },
    });
  });

  it("answers with the team alone on no person", () => {
    expect(ownerStep("core", { kind: "no-person" }).answer).toEqual({ team: "core", person: "" });
  });

  it("answers with nobody on no owner, whatever team was picked", () => {
    expect(ownerStep("core", { kind: "no-owner" }).answer).toEqual({ team: "", person: "" });
  });
});

describe("reading an owner", () => {
  it("is the team and the first assignee", () => {
    expect(ownerOf({ team: "core", assignees: ["ann", "bob"] })).toEqual({ team: "core", person: "ann" });
    expect(ownerOf({ assignees: [] })).toEqual({ team: "", person: "" });
  });

  it("labels team · person, the team alone, or a dash", () => {
    expect(ownerLabel({ team: "core", person: "ann" })).toBe("core · ann");
    expect(ownerLabel({ team: "core", person: "" })).toBe("core");
    expect(ownerLabel({ team: "", person: "" })).toBe("—");
    expect(ownerLabel({ team: "core", person: "ann" }, (l) => l.toUpperCase())).toBe("core · ANN");
  });

  it("puts the people with work on the team first", () => {
    const cards = [
      { team: "core", assignees: ["bob"], progress: 0 },
      { team: "web", assignees: ["ann"], progress: 0 },
    ];
    expect(peopleOfTeam(["ann", "bob", "cid"], "core", cards)).toEqual(["bob", "ann", "cid"]);
    expect(peopleOfTeam(["ann", "bob"], "none", cards)).toEqual(["ann", "bob"]);
  });
});
