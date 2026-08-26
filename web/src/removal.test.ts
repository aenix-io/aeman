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
