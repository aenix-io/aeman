import { describe, expect, it } from "vitest";

import { avatarUrlFor, avatarsFrom } from "./users";

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
