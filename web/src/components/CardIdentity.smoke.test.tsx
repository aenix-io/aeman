import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { CardIdentity, copyText } from "./CardIdentity";

// The drawer names its card: the id, in mono, copied with a click, and the
// card's link on this board — the board's address with this view and this
// card — copied beside it. "copied" stays on the one pressed until another is
// pressed or the drawer opens another card: no timer to leak.
describe("the drawer's card identity", () => {
  it("shows the id in mono and offers to copy it and the link", () => {
    const html = renderToStaticMarkup(<CardIdentity uid="01JABC" link="https://b.example/?view=backlog&card=01JABC" />);
    expect(html).toMatch(/<code[^>]*class="drawer-uid"[^>]*>01JABC<\/code>/);
    // The id is in the button's name, not hidden behind it.
    expect(html).toMatch(/<button[^>]*aria-label="Copy card id 01JABC"[^>]*>/);
    // What was copied is announced.
    expect(html).toMatch(/role="status"[^>]*aria-live="polite"/);
    expect(html).toMatch(/<button[^>]*aria-label="Copy the link to this card"[^>]*>Copy link<\/button>/);
    expect(html).toContain('title="https://b.example/?view=backlog&amp;card=01JABC"');
  });

  it("offers nothing for a card not made yet", () => {
    expect(renderToStaticMarkup(<CardIdentity uid="tmp-2026" link="x" />)).toBe("");
  });
});

// "copied" is said only when the copy happened: no clipboard (a plain-http
// board), or a refused write, is "copy failed".
describe("copying", () => {
  it("says whether the text reached the clipboard", async () => {
    let got = "";
    expect(await copyText("x", { writeText: async (t: string) => void (got = t) })).toBe(true);
    expect(got).toBe("x");
    expect(await copyText("x", undefined)).toBe(false);
    expect(await copyText("x", { writeText: () => Promise.reject(new Error("denied")) })).toBe(false);
  });
});
