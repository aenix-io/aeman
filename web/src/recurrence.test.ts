import { describe, expect, it } from "vitest";

import { recurrenceLabel, recurrenceTitle, repeatOf, repeatPatch } from "./recurrence";

describe("recurrence labels", () => {
  it("names the board's own turn a sprint", () => {
    expect(recurrenceLabel("")).toBe("Every sprint");
    expect(recurrenceTitle("")).toBe("Recurrent");
    expect(recurrenceTitle(undefined)).toBe("Recurrent");
  });

  it("names the calendar cycles", () => {
    expect(recurrenceLabel("week")).toBe("Weekly");
    expect(recurrenceLabel("month")).toBe("Monthly");
    expect(recurrenceTitle("week")).toBe("Recurrent (weekly)");
    expect(recurrenceTitle("month")).toBe("Recurrent (monthly)");
  });
});

// The card pane's Repeat: the stage menu's Recurrent and its cycle, as one
// choice — "no" takes the recurrence off and leaves the card plain; a cycle
// makes it recurrent with that cycle, whatever stage it had. Choosing what it
// already is writes nothing.
describe("the card pane's Repeat", () => {
  it("reads a card's repeat", () => {
    expect(repeatOf({ stage: "recurrent", recurrence: "week" })).toBe("week");
    expect(repeatOf({ stage: "recurrent" })).toBe("");
    expect(repeatOf({ stage: "locked", recurrence: "" })).toBe("no");
    expect(repeatOf({})).toBe("no");
  });
  it("makes a card recurrent on a cycle", () => {
    expect(repeatPatch({}, "month")).toEqual({ stage: "recurrent", recurrence: "month" });
    expect(repeatPatch({ stage: "locked" }, "")).toEqual({ stage: "recurrent", recurrence: "" });
    expect(repeatPatch({ stage: "recurrent", recurrence: "" }, "week")).toEqual({ stage: "recurrent", recurrence: "week" });
  });
  it("takes the recurrence off", () => {
    expect(repeatPatch({ stage: "recurrent", recurrence: "week" }, "no")).toEqual({ stage: "", recurrence: "" });
  });
  it("writes nothing for what the card already is", () => {
    expect(repeatPatch({ stage: "recurrent", recurrence: "week" }, "week")).toBeNull();
    expect(repeatPatch({ stage: "locked" }, "no")).toBeNull();
  });
});
