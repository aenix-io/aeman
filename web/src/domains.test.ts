import { describe, expect, it } from "vitest";

import {
  cardDomainBadge,
  declareDomain,
  isMultiDomain,
  primaryDomain,
  reviewerCandidates,
  writableDomains,
  type DomainInfo,
} from "./domains";

const single: DomainInfo[] = [
  { name: "acme/board", writable: true, members: ["ann", "bob"] },
];

const multi: DomainInfo[] = [
  { name: "acme/board", writable: true, members: ["ann", "bob", "cid"] },
  { name: "acme/secret", writable: true, members: ["ann"] },
  { name: "acme/archive", writable: false, members: ["ann", "bob"] },
];

describe("isMultiDomain", () => {
  it("is false for no domains (an old server) and for exactly one", () => {
    expect(isMultiDomain([])).toBe(false);
    expect(isMultiDomain(single)).toBe(false);
  });

  it("is true from the second domain on", () => {
    expect(isMultiDomain(multi)).toBe(true);
  });
});

describe("primaryDomain", () => {
  it("is the first configured domain", () => {
    expect(primaryDomain(multi)).toBe("acme/board");
  });

  it("is empty when the server names no domains", () => {
    expect(primaryDomain([])).toBe("");
  });
});

describe("cardDomainBadge", () => {
  it("shows nothing on a single-domain board, whatever the card says", () => {
    expect(cardDomainBadge(single, "acme/board")).toBeNull();
    expect(cardDomainBadge(single, "acme/elsewhere")).toBeNull();
    expect(cardDomainBadge([], "acme/board")).toBeNull();
  });

  it("shows nothing for a card in the primary domain", () => {
    expect(cardDomainBadge(multi, "acme/board")).toBeNull();
  });

  it("treats a card without a domain (old server) as living in the primary", () => {
    expect(cardDomainBadge(multi, undefined)).toBeNull();
    expect(cardDomainBadge(multi, "")).toBeNull();
  });

  it("names the domain for a card outside the primary", () => {
    expect(cardDomainBadge(multi, "acme/secret")).toBe("acme/secret");
    expect(cardDomainBadge(multi, "acme/archive")).toBe("acme/archive");
  });
});

describe("writableDomains", () => {
  it("keeps only the domains the visitor may write, in board order", () => {
    expect(writableDomains(multi).map((d) => d.name)).toEqual([
      "acme/board",
      "acme/secret",
    ]);
  });

  it("is empty when there is nothing to choose from", () => {
    expect(writableDomains([])).toEqual([]);
    expect(
      writableDomains([{ name: "ro", writable: false, members: [] }]),
    ).toEqual([]);
  });
});

describe("reviewerCandidates", () => {
  const people = ["ann", "bob", "cid", "dan"];

  it("offers everyone when the server names no domains", () => {
    expect(reviewerCandidates(people, [], "acme/board")).toEqual(people);
  });

  it("offers only the people who can read the card's domain, in the given order", () => {
    expect(reviewerCandidates(people, multi, "acme/secret")).toEqual(["ann"]);
    expect(reviewerCandidates(people, multi, "acme/archive")).toEqual([
      "ann",
      "bob",
    ]);
  });

  it("drops a non-member (a free-text assignee) who cannot read the domain", () => {
    // dan is on the board's cards but in no domain's member list.
    expect(reviewerCandidates(people, multi, "acme/board")).toEqual([
      "ann",
      "bob",
      "cid",
    ]);
  });

  it("uses the primary for a card without a domain", () => {
    expect(reviewerCandidates(people, multi, undefined)).toEqual([
      "ann",
      "bob",
      "cid",
    ]);
  });

  it("falls back to everyone when the card's domain is unknown", () => {
    expect(reviewerCandidates(people, multi, "acme/gone")).toEqual(people);
  });

  it("falls back to everyone when the domain carries no membership", () => {
    const noInfo: DomainInfo[] = [
      { name: "a", writable: true, members: [] },
      { name: "b", writable: true, members: [] },
    ];
    expect(reviewerCandidates(people, noInfo, "b")).toEqual(people);
  });
});

describe("declareDomain", () => {
  it("leaves the choice to the server (the primary) while there is nothing to choose", () => {
    expect(declareDomain([], "")).toBeUndefined();
    expect(declareDomain(single, "acme/board")).toBeUndefined();
    // One writable domain among several read-only ones is still no choice.
    expect(
      declareDomain(
        [
          { name: "ro", writable: false, members: [] },
          { name: "rw", writable: true, members: [] },
        ],
        "rw",
      ),
    ).toBeUndefined();
  });

  it("passes the pick through when the visitor can write to several domains", () => {
    expect(declareDomain(multi, "acme/secret")).toBe("acme/secret");
  });

  it("defaults an empty pick to the first writable domain — what the selector shows", () => {
    expect(declareDomain(multi, "")).toBe("acme/board");
    expect(
      declareDomain(
        [
          { name: "ro", writable: false, members: [] },
          { name: "rw1", writable: true, members: [] },
          { name: "rw2", writable: true, members: [] },
        ],
        "",
      ),
    ).toBe("rw1");
  });
});
