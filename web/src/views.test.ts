import { describe, expect, it } from "vitest";

import { createView } from "./views";

// The board is the vocabulary now: the server takes it as a path segment and
// fills in what that board means, so a create made from the wrong board is
// answered with a card that is not the one the person typed.
describe("which board a create belongs to", () => {
  it("is the Backlog board when it is parked", () => {
    expect(createView({ parked: true })).toBe("backlog");
  });

  // A task typed into a Backlog column is parked on that team, last (K3):
  // the board the reader stands on says so whether or not the add box does.
  it("is the Backlog board for anything typed while standing on it", () => {
    expect(createView({}, "backlog")).toBe("backlog");
    expect(createView({ parked: true }, "backlog")).toBe("backlog");
  });

  // A planning card is the Strategy board's whatever board is open, and so is a task
  // born under one from its parent's own pane: no day board takes a create
  // with no day and a parent of that kind (H7).
  it("is the Strategy board for a planning card, and for a task born under one", () => {
    expect(createView({ type: "epic" }, "me")).toBe("strategy");
    expect(createView({ underPlanning: true }, "team")).toBe("strategy");
    expect(createView({ underPlanning: false })).toBe("team");
  });

  // K9: there is no Triage board to create on, and a week names no board —
  // a task has none (K7). A day card is a day board's.
  it("never answers Triage", () => {
    expect(createView({ start: "2026-09-08", day: "2026-09-08" })).toBe("team");
    expect(createView({}, "team")).not.toBe("triage");
  });

  // A day card is the board the reader is STANDING on: the Me board and the
  // team's grid send the same shape and do not mean the same thing, and the
  // open view is the only thing that knows which one was typed into.
  it("is the day board the reader is standing on", () => {
    expect(createView({}, "me")).toBe("me");
    expect(createView({ start: "2026-09-11", day: "2026-09-11" }, "me")).toBe("me");
    expect(createView({}, "team")).toBe("team");
    expect(createView({})).toBe("team");
  });

  // The shape still wins where it names a board of its own.
  it("lets the shape win over the open board", () => {
    expect(createView({ parked: true }, "team")).toBe("backlog");
    expect(createView({ type: "epic" }, "backlog")).toBe("strategy");
  });
});
