import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { CardIdentity, copyText } from "./CardIdentity";

// The drawer gives its card away in two chips: "Copy ID" and "Copy link" —
// the card's link on this board, with this view and this card. The id itself
// is not drawn: it is a thing to paste, not to read. "copied" stays on the
// chip pressed until the other is pressed or the drawer opens another card:
// no timer to leak.
describe("the drawer's card identity", () => {
  it("offers two chips, Copy ID and Copy link, and draws no id", () => {
    const html = renderToStaticMarkup(<CardIdentity uid="01JABC" link="https://b.example/?view=backlog&card=01JABC" />);
    expect(html).toMatch(/<button[^>]*class="drawer-copy-chip"[^>]*aria-label="Copy card id 01JABC"[^>]*>Copy ID<\/button>/);
    expect(html).toMatch(/<button[^>]*class="drawer-copy-chip"[^>]*aria-label="Copy the link to this card"[^>]*>Copy link<\/button>/);
    expect(html).toContain('title="https://b.example/?view=backlog&amp;card=01JABC"');
    // The id is the button's name for a screen reader, not text on screen.
    expect(html.replace(/aria-label="[^"]*"|title="[^"]*"/g, "")).not.toContain("01JABC");
    // What was copied is announced.
    expect(html).toMatch(/role="status"[^>]*aria-live="polite"/);
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
