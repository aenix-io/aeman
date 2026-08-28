import { describe, expect, it } from "vitest";

import {
  isPersonalCard,
  personalRepoName,
  personalShows,
  recurrenceLabel,
  recurrenceTitle,
  splitPersonal,
  type PersonalBoard,
} from "./personal";
import type { Card } from "./providers/types";

const personal: PersonalBoard = {
  domain: "~kvaps",
  url: "https://github.com/kvaps/aeman-personal.git",
};

const card = (itemId: string, extra: Partial<Card> = {}): Card => ({
  itemId,
  title: itemId,
  assignees: [],
  ...extra,
});

describe("personalRepoName", () => {
  it("names an https repository owner/name, with or without .git or a trailing slash", () => {
    expect(personalRepoName("https://github.com/kvaps/aeman-personal.git")).toBe(
      "kvaps/aeman-personal",
    );
    expect(personalRepoName("https://github.com/kvaps/aeman-personal")).toBe(
      "kvaps/aeman-personal",
    );
    expect(personalRepoName("https://github.com/kvaps/aeman-personal/")).toBe(
      "kvaps/aeman-personal",
    );
  });

  it("understands both ssh forms", () => {
    expect(personalRepoName("git@github.com:kvaps/aeman-personal.git")).toBe(
      "kvaps/aeman-personal",
    );
    expect(personalRepoName("ssh://git@github.com/kvaps/aeman-personal.git")).toBe(
      "kvaps/aeman-personal",
    );
  });

  it("keeps the last two path segments of a nested (grouped) repository", () => {
    expect(personalRepoName("https://gitlab.example.com/group/sub/repo.git")).toBe(
      "sub/repo",
    );
  });

  it("falls back to the one segment there is, and to the input when nothing parses", () => {
    expect(personalRepoName("https://example.com/repo")).toBe("repo");
    expect(personalRepoName("kvaps/aeman-personal")).toBe("kvaps/aeman-personal");
    expect(personalRepoName("  ")).toBe("");
  });
});

describe("isPersonalCard", () => {
  it("is the card whose domain is the personal one", () => {
    expect(isPersonalCard(card("a", { domain: "~kvaps" }), personal)).toBe(true);
  });

  it("is false without a personal board, for a card without a domain, and for another person's ~domain", () => {
    expect(isPersonalCard(card("a", { domain: "~kvaps" }), undefined)).toBe(false);
    expect(isPersonalCard(card("a", { domain: "~kvaps" }), null)).toBe(false);
    expect(isPersonalCard(card("a"), personal)).toBe(false);
    expect(isPersonalCard(card("a", { domain: "~bob" }), personal)).toBe(false);
    expect(isPersonalCard(card("a", { domain: "acme/board" }), personal)).toBe(false);
  });
});

describe("splitPersonal", () => {
  const cards = [
    card("t1", { domain: "acme/board" }),
    card("p1", { domain: "~kvaps" }),
    card("t2"),
    card("p2", { domain: "~kvaps" }),
  ];

  it("splits the list in place order: the day board's cards and the personal ones", () => {
    const { team, personal: mine } = splitPersonal(cards, personal);
    expect(team.map((c) => c.itemId)).toEqual(["t1", "t2"]);
    expect(mine.map((c) => c.itemId)).toEqual(["p1", "p2"]);
  });

  it("hands everything to the day board when no personal board is linked", () => {
    const { team, personal: mine } = splitPersonal(cards, undefined);
    expect(team).toEqual(cards);
    expect(mine).toEqual([]);
  });
});

describe("personalShows", () => {
  const today = "2026-08-28";

  it("shows an open card", () => {
    expect(personalShows(card("a"), today)).toBe(true);
  });

  it("shows a card finished today, and hides one finished before today", () => {
    expect(personalShows(card("a", { doneAt: "2026-08-28" }), today)).toBe(true);
    expect(personalShows(card("a", { doneAt: "2026-08-27" }), today)).toBe(false);
  });

  it("keeps a card whose done day is ahead of this clock (a replica's day)", () => {
    expect(personalShows(card("a", { doneAt: "2026-08-29" }), today)).toBe(true);
  });
});

describe("recurrence labels", () => {
  it("reads the default cycle as the day on a personal card and the sprint on a team card", () => {
    expect(recurrenceLabel("", true)).toBe("Every day");
    expect(recurrenceLabel("", false)).toBe("Every sprint");
    expect(recurrenceTitle("", true)).toBe("Recurrent (daily)");
    expect(recurrenceTitle(undefined, true)).toBe("Recurrent (daily)");
    expect(recurrenceTitle(undefined, false)).toBe("Recurrent");
  });

  it("names the calendar cycles the same on both boards", () => {
    for (const personal of [true, false]) {
      expect(recurrenceLabel("week", personal)).toBe("Weekly");
      expect(recurrenceLabel("month", personal)).toBe("Monthly");
      expect(recurrenceTitle("week", personal)).toBe("Recurrent (weekly)");
      expect(recurrenceTitle("month", personal)).toBe("Recurrent (monthly)");
    }
  });
});

describe("personalShows and planning", () => {
  const today = "2026-08-28";
  const card = (extra: Partial<Card>): Card => ({ itemId: "p", title: "p", assignees: [], ...extra });

  it("hides a card planned for a later day until that day", () => {
    const later = card({ startDate: "2026-08-29" });
    expect(personalShows(later, today)).toBe(false);
    expect(personalShows(later, "2026-08-29")).toBe(true);
    expect(personalShows(later, "2026-09-05")).toBe(true);
  });

  it("shows a card planned for today or earlier, and one without a start", () => {
    expect(personalShows(card({ startDate: today }), today)).toBe(true);
    expect(personalShows(card({ startDate: "2026-08-20", progress: 20 }), today)).toBe(true);
    expect(personalShows(card({}), today)).toBe(true);
  });

  it("keeps the done-today rule alongside the start rule", () => {
    expect(personalShows(card({ startDate: "2026-08-29", progress: 100, doneAt: today }), today)).toBe(false);
    expect(personalShows(card({ startDate: "2026-08-20", progress: 100, doneAt: today }), today)).toBe(true);
    expect(personalShows(card({ startDate: "2026-08-20", progress: 100, doneAt: "2026-08-27" }), today)).toBe(false);
  });
});

describe("personalShows and the left-behind card", () => {
  const today = "2026-08-28";
  const card = (extra: Partial<Card>): Card => ({ itemId: "p", title: "p", assignees: [], ...extra });
  it("shows a card on the day it was left and before, hides it from the next day", () => {
    const left = card({ startDate: "2026-08-20", progress: 40, leftAt: "2026-08-27" });
    expect(personalShows(left, today)).toBe(false);
    expect(personalShows(left, "2026-08-27")).toBe(true);
    expect(personalShows(left, "2026-08-25")).toBe(true);
    expect(personalShows(card({ progress: 40, leftAt: today }), today)).toBe(true);
  });
});
