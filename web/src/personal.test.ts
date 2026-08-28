import { describe, expect, it } from "vitest";

import {
  isPersonalCard,
  personalRepoName,
  personalShows,
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
