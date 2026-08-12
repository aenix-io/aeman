import { describe, expect, it } from "vitest";
import { daysSince, localDateIso } from "./date";

// The age counter reads "days on the board" (its own tooltip). A card
// scheduled ahead is not on the board while it waits, so those days must not
// count — colleagues reported a card created today for a future date already
// showing three weeks of age on arrival. This pins the rule Card.tsx applies:
// count from whichever came later, the card's creation or its scheduled day.
function ageFrom(createdAt: string, startDate?: string): string {
  return startDate && createdAt && startDate > localDateIso(createdAt)
    ? startDate
    : createdAt;
}

describe("card age", () => {
  it("counts a card scheduled ahead from its own day, not from creation", () => {
    const created = "2026-08-11T09:00:00Z";
    const start = "2026-09-01";
    expect(daysSince(ageFrom(created, start), "2026-09-01")).toBe(0);
    expect(daysSince(ageFrom(created, start), "2026-09-04")).toBe(3);
  });

  it("leaves an ordinary card counting from creation", () => {
    const created = "2026-08-01T09:00:00Z";
    // Carried over daily: the start day stays where it was created.
    expect(daysSince(ageFrom(created, "2026-08-01"), "2026-08-11")).toBe(10);
  });

  it("does not go backwards for a card whose start precedes its creation", () => {
    const created = "2026-08-11T09:00:00Z";
    expect(daysSince(ageFrom(created, "2026-08-05"), "2026-08-13")).toBe(2);
  });
});
