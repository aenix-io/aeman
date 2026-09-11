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

  // A week and no dates is the Triage cell; a week WITH dates is a day card
  // that also carries its week, which is the day boards' create.
  it("is a Triage cell when the card is a week's and no day's", () => {
    expect(createView({ week: "2026-09-07" })).toBe("triage");
    expect(createView({ week: "2026-09-07", start: "2026-09-08" })).toBe(
      "team",
    );
  });

  it("is a day board otherwise", () => {
    expect(createView({})).toBe("team");
    expect(createView({ start: "2026-09-11", day: "2026-09-11" })).toBe("team");
  });
});
