import { describe, expect, it } from "vitest";

import { createView } from "./views";

// The board is the vocabulary now: the server takes it as a path segment and
// fills in what that board means, so a create made from the wrong board is
// answered with a card that is not the one the person typed.
describe("which board a create belongs to", () => {
  it("is the personal column when the card is the reader's own", () => {
    expect(createView({ personal: true })).toBe("personal");
  });

  it("is the Project grid when it names a column", () => {
    expect(createView({ epic: "Auth", week: "2026-09-07" })).toBe("project");
  });

  it("is the drawer when it is parked", () => {
    expect(createView({ parked: true })).toBe("backlog");
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

  it("is a day board otherwise", () => {
    expect(createView({})).toBe("team");
    expect(createView({ start: "2026-09-11", day: "2026-09-11" })).toBe("team");
  });
});
