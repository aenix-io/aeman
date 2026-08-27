import { describe, expect, it } from "vitest";
import { removalKind } from "./removal";

const ctx = { current: "2026-08-26", previous: "2026-08-25", today: "2026-08-26" };
const card = { sprintStart: "2026-08-26", startDate: "2026-08-20", progress: 0 };

describe("removalKind", () => {
  it("demotes a card with sprint history behind it", () => {
    expect(removalKind(card, ctx)).toBe("demote");
  });

  it("deletes a card created today — there is no history worth keeping", () => {
    expect(removalKind({ ...card, startDate: "2026-08-26" }, ctx)).toBe("delete");
  });

  it("deletes when the team has no previous sprint to fall back to", () => {
    expect(removalKind(card, { ...ctx, previous: undefined })).toBe("delete");
  });

  it("deletes a card that is not in the current sprint", () => {
    expect(removalKind({ ...card, sprintStart: "2026-08-20" }, ctx)).toBe("delete");
  });

  // The choice: a demote silently takes the card (and its subtasks) off
  // today's board, which reads as deletion. When there is work to lose, the
  // person decides instead of the rule.
  it("asks when the card would be demoted AND carries progress", () => {
    expect(removalKind({ ...card, progress: 40 }, ctx)).toBe("ask");
  });

  it("does not ask for an untouched card — nothing is lost either way", () => {
    expect(removalKind({ ...card, progress: 0 }, ctx)).toBe("demote");
  });

  it("does not ask when the answer is a plain delete", () => {
    expect(removalKind({ ...card, startDate: "2026-08-26", progress: 40 }, ctx)).toBe("delete");
  });
});

// The two rules meet here. A card filed under a Project-board column must
// never be destroyed by the ×, so the choice dialog must not offer deletion
// for one: with a column there is nothing to choose, and the server hands the
// card back (demote, or release to the plan) either way.
describe("removalKind on a card that belongs to a column", () => {
  const worked = { sprintStart: "2026-08-26", startDate: "2026-08-20", progress: 40 };

  it("never asks for a card with an epic", () => {
    expect(removalKind({ ...worked, epic: "Cozystack Core" }, ctx)).toBe("demote");
  });

  it("never asks for a card with a project — a column is the (project, epic) pair", () => {
    expect(removalKind({ ...worked, project: "freedom" }, ctx)).toBe("demote");
  });

  it("still asks for a worked card that belongs to no column", () => {
    expect(removalKind(worked, ctx)).toBe("ask");
  });
})
