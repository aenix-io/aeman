import { describe, expect, it } from "vitest";

import { recurrenceLabel, recurrenceTitle } from "./recurrence";

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
