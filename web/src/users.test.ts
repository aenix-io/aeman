import { describe, expect, it } from "vitest";

import { avatarUrlFor, avatarsFrom, displayName, namesFrom } from "./users";

describe("avatarsFrom", () => {
  it("maps each member's login onto its avatar URL", () => {
    expect(
      avatarsFrom([
        { login: "ann", avatarUrl: "https://avatars.githubusercontent.com/ann?size=48" },
        { login: "bob", avatarUrl: "https://avatars.githubusercontent.com/bob?size=48" },
      ]),
    ).toEqual({
      ann: "https://avatars.githubusercontent.com/ann?size=48",
      bob: "https://avatars.githubusercontent.com/bob?size=48",
    });
  });

  it("skips members without a URL and blank logins", () => {
    expect(
      avatarsFrom([
        { login: "ann" },
        { login: "", avatarUrl: "https://example.invalid/x" },
        { login: "bob", avatarUrl: "https://example.invalid/bob" },
      ]),
    ).toEqual({ bob: "https://example.invalid/bob" });
  });
});

describe("avatarUrlFor", () => {
  const avatars = avatarsFrom([
    { login: "ann", avatarUrl: "https://example.invalid/ann" },
  ]);

  it("answers with the member's URL", () => {
    expect(avatarUrlFor("ann", avatars)).toBe("https://example.invalid/ann");
  });

  it("has no URL for a login outside the roster (an assignee who is not a member) — the caller falls back to initials", () => {
    expect(avatarUrlFor("guest", avatars)).toBeUndefined();
    expect(avatarUrlFor("ann", undefined)).toBeUndefined();
  });
});

describe("namesFrom", () => {
  it("maps each member's login onto its display name", () => {
    expect(
      namesFrom([
        { login: "ann", name: "Ann Arbor", avatarUrl: "https://example.invalid/ann" },
        { login: "bob", name: "Bob Builder" },
      ]),
    ).toEqual({ ann: "Ann Arbor", bob: "Bob Builder" });
  });

  it("skips members without a name (a GitHub roster carries none), blank names and blank logins", () => {
    expect(
      namesFrom([
        { login: "ann" },
        { login: "bob", name: "" },
        { login: "cid", name: "   " },
        { login: "", name: "Nobody" },
        { login: "dee", name: "Dee Dee" },
      ]),
    ).toEqual({ dee: "Dee Dee" });
  });

  it("trims a padded name but keeps it otherwise verbatim", () => {
    expect(namesFrom([{ login: "ann", name: "  Ann  Arbor " }])).toEqual({ ann: "Ann  Arbor" });
  });
});

describe("displayName", () => {
  const names = namesFrom([{ login: "ann", name: "Ann Arbor" }]);

  it("is the member's name when the roster has one", () => {
    expect(displayName("ann", names)).toBe("Ann Arbor");
  });

  it("is the login for a person outside the roster and when there are no names at all — the login stays the identifier", () => {
    expect(displayName("guest", names)).toBe("guest");
    expect(displayName("ann", {})).toBe("ann");
    expect(displayName("ann", undefined)).toBe("ann");
  });
});
