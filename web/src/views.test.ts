import { describe, expect, it } from "vitest";

import { createView } from "./views";

// The board is the vocabulary now: the server takes it as a path segment and
// fills in what that board means, so a create made from the wrong board is
// answered with a card that is not the one the person typed.
describe("which board a create belongs to", () => {
  it("is the Project grid when it names a column", () => {
    expect(createView({ scope: "Auth", week: "2026-09-07" })).toBe("project");
  });

  it("is the drawer when it is parked", () => {
    expect(createView({ parked: true })).toBe("backlog");
  });

  // A planning card is the Roadmap's whatever board is open, and so is a task
  // born under one from its parent's own pane: no day board takes a create
  // with no day and a parent of that kind (H7).
  it("is the Roadmap for a planning card, and for a task born under one", () => {
    expect(createView({ type: "epic" }, "me")).toBe("roadmap");
    expect(createView({ underPlanning: true }, "team")).toBe("roadmap");
    expect(createView({ underPlanning: false })).toBe("team");
  });

  // The week is that board's whole gesture, days or no days: a card started
  // in the row that IS NOW carries today's dates as well and is still a card
  // of the Triage board (the server takes them for the current week alone).
  it("is a Triage cell whenever the card is a week's", () => {
    expect(createView({ week: "2026-09-07" })).toBe("triage");
    expect(createView({ week: "2026-09-07", start: "2026-09-08", day: "2026-09-08" })).toBe(
      "triage",
    );
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

  // The shape still wins where it names a board of its own: the backlog
  // drawer stands on another board's screen.
  it("lets the shape win over the open board", () => {
    expect(createView({ parked: true }, "triage")).toBe("backlog");
    expect(createView({ week: "2026-09-07" }, "triage")).toBe("triage");
    expect(createView({ scope: "Auth" }, "project")).toBe("project");
  });
});
