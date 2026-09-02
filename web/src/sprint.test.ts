import { describe, expect, it } from "vitest";
import { sprintForDate } from "./sprint";
import type { Board } from "./providers/types";

// The board this rule reads: one team, its two tracked sprints.
const board = {
  sprintStates: {
    portal: { current: "2026-09-02", previous: "2026-09-01" },
    fresh: { current: null, previous: null },
  },
} as unknown as Board;

const today = "2026-09-02";

// Which sprint the calendar's dates put a card in — the mirror of
// boardservice.SetDates. The case that matters is the last one: a day older
// than the team's reach used to make a sprint of its own, starting there, and
// a card in a sprint that closed is drawn by no board and moved by no
// carry-over. Three cards went that way in one working day on the production
// board before the rule was changed.
describe("sprintForDate", () => {
  it("parks a card dated into the future: no sprint covers that day yet", () => {
    expect(sprintForDate(board, "portal", "2026-09-10", today)).toBe(null);
  });

  it("takes the day's own sprint while the team can still reach it", () => {
    expect(sprintForDate(board, "portal", "2026-09-02", today)).toBe("2026-09-02");
    expect(sprintForDate(board, "portal", "2026-09-01", today)).toBe("2026-09-01");
  });

  it("keeps the team's current sprint for a day older than its reach", () => {
    expect(sprintForDate(board, "portal", "2026-08-24", today)).toBe("2026-09-02");
    expect(sprintForDate(board, "portal", "2026-06-29", today)).toBe("2026-09-02");
  });

  it("seeds from the day itself when the team has no sprint at all", () => {
    expect(sprintForDate(board, "fresh", "2026-08-24", today)).toBe("2026-08-24");
  });

  it("clears the sprint when the dates are cleared", () => {
    expect(sprintForDate(board, "portal", "", today)).toBe(null);
  });
});
