import { describe, expect, it } from "vitest";
import { dayIsOverFor, sprintForDate } from "./sprint";
import type { Board, SprintState } from "./providers/types";

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

  it("keeps the team's current sprint for a day inside it", () => {
    expect(sprintForDate(board, "portal", "2026-09-02", today)).toBe("2026-09-02");
  });

  // The PREVIOUS sprint has closed, and a carry-over moves the closing
  // sprint's own cards and nothing older — so a card re-dated into it would
  // never be picked up again: drawn while it stayed open, gone from the
  // team's board the moment somebody finished it.
  it("does not park a card in a sprint that closed, however near", () => {
    expect(sprintForDate(board, "portal", "2026-09-01", today)).toBe("2026-09-02");
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

// A day is a RECORD for one team and live for the next, and the add boxes ask
// it per team: while a sprint is open, every day of it is still that team's to
// add to. Mirrors board.TeamsPast.
describe("dayIsOverFor", () => {
  const live = {
    portal: { current: "2026-09-09", previous: "2026-09-08" },
    backoffice: { current: "2026-09-07", previous: "2026-09-02" },
    "": { current: "2026-09-09" },
  } as unknown as Record<string, SprintState>;

  it("is over for a team whose sprint has moved past the day", () => {
    expect(dayIsOverFor(live, "portal", "2026-09-08")).toBe(true);
  });

  it("is NOT over while the team is still inside that sprint", () => {
    // backoffice opened this sprint on the 7th: the 7th and the 8th are days
    // of a sprint still running, and a card can be added on either.
    expect(dayIsOverFor(live, "backoffice", "2026-09-07")).toBe(false);
    expect(dayIsOverFor(live, "backoffice", "2026-09-08")).toBe(false);
  });

  it("answers for the no-team group like any other team", () => {
    expect(dayIsOverFor(live, null, "2026-09-08")).toBe(true);
    expect(dayIsOverFor(live, null, "2026-09-09")).toBe(false);
  });

  it("says nothing about a team with no sprint at all", () => {
    expect(dayIsOverFor(live, "sales", "2026-01-01")).toBe(false);
  });
});
