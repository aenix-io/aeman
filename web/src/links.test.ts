import { describe, expect, it } from "vitest";
import { optimisticTitle } from "./links";

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
