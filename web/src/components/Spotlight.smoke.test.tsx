import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { Spotlight } from "./Spotlight";
import type { Card } from "../providers/types";

const noop = () => {};
const render = (cards: Card[] | null, error: string | null = null) =>
  renderToStaticMarkup(
    <Spotlight cards={cards} error={error} onPick={noop} onClose={noop} avatars={{}} names={{}} />,
  );

// The ⌘K search opens on a field and says what it is about to search; the
// ranking itself is search.ts's (search.test.ts).
describe("the ⌘K search", () => {
  it("is a dialog with one search field", () => {
    const html = render([]);
    expect(html).toContain('role="dialog"');
    expect(html).toContain('class="spotlight-input"');
    expect(html).toContain('role="combobox"');
  });

  it("says it is loading until the listing arrives", () => {
    expect(render(null)).toContain("Loading cards…");
  });

  it("says how many cards it searches before anything is typed", () => {
    const cards = [{ itemId: "a", title: "Backups", assignees: [] }] as unknown as Card[];
    expect(render(cards)).toContain("Search 1 card by title");
  });

  it("shows why the listing could not be fetched", () => {
    expect(render(null, "network down")).toContain("network down");
  });
});
