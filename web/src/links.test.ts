import { describe, expect, it } from "vitest";
import { extractLinks, optimisticTitle } from "./links";

// The card must never sit on screen showing a raw URL: a bare GitHub
// reference reads as the same "Pull: owner/repo#N" label the server falls
// back to, until the background resolve renames it to the real title.
describe("optimisticTitle", () => {
  it("labels a bare PR URL", () => {
    expect(optimisticTitle("https://github.com/acme/webapp/pull/7")).toBe(
      "Pull: acme/webapp#7",
    );
  });

  it("labels a bare issue URL, trimming whitespace", () => {
    expect(optimisticTitle("  https://github.com/acme/webapp/issues/12 ")).toBe(
      "Issue: acme/webapp#12",
    );
  });

  it("labels the owner/repo#N shorthand", () => {
    expect(optimisticTitle("cncf/foundation#1465")).toBe(
      "Issue: cncf/foundation#1465",
    );
  });

  it("leaves the user's own wording alone", () => {
    for (const title of [
      "Fix the flaky e2e test",
      "see https://github.com/acme/webapp/pull/7 for context",
      "https://example.com/docs",
      "a/b/c#1",
    ]) {
      expect(optimisticTitle(title)).toBe(title);
    }
  });
});

// The client mirrors the server's extraction (pkg/board/links.go): markdown
// emphasis around a link must not end up inside the URL, or the links menu
// shows a bare unresolved link instead of the PR.
describe("extractLinks trailing markdown", () => {
  it("strips ** around a PR link", () => {
    const got = extractLinks("- Backend: **https://github.com/acme/repo/pull/1360** (`fix/x`).");
    expect(got).toHaveLength(1);
    expect(got[0].url).toBe("https://github.com/acme/repo/pull/1360");
    expect(got[0].kind).toBe("pull");
    expect(got[0].number).toBe(1360);
  });

  it("strips _ and ~ emphasis too", () => {
    const got = extractLinks("_https://github.com/acme/repo/issues/7_ ~~https://github.com/acme/repo/pull/8~~");
    expect(got.map((l) => l.url)).toEqual([
      "https://github.com/acme/repo/issues/7",
      "https://github.com/acme/repo/pull/8",
    ]);
  });

  it("keeps punctuation that is part of the path", () => {
    const got = extractLinks("https://example.com/a_b/c~d/e*f?q=1");
    expect(got[0].url).toBe("https://example.com/a_b/c~d/e*f?q=1");
  });
});
