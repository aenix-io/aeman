import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { RemoveChoiceDialog } from "./RemoveChoiceDialog";

// The one choice dialog every × puts: the Team board's. The hierarchy's ×
// borrows it with its own two answers, so this pins that the dialog names
// them and says what deleting does to the rows under a planning card.
describe("the choice dialog, asked by the hierarchy's ×", () => {
  function draw(choices: Parameters<typeof RemoveChoiceDialog>[0]["choices"], note?: string) {
    return renderToStaticMarkup(
      <RemoveChoiceDialog
        title="Billing v2"
        progress={50}
        choices={choices}
        subtasks={0}
        note={note}
        onClose={vi.fn()}
        onSubmit={vi.fn()}
      />,
    );
  }

  it("offers delete and detach, delete as the danger, and asks which", () => {
    const html = draw(["delete", "detach"], "The 2 cards under it move under «Grow revenue».");
    expect(html).toContain("Delete it, or just unlink it?");
    expect(html).toContain("Delete it");
    expect(html).toContain("Take it out from under its parent");
    expect(html).toContain("sprint-choice-btn-danger");
    expect(html).toContain("The 2 cards under it move under «Grow revenue».");
  });

  it("offers delete alone at the top of the tree", () => {
    const html = draw(["delete"]);
    expect(html).toContain("Delete it");
    expect(html).not.toContain("Take it out from under its parent");
  });

  it("still asks the day boards' question for the day boards' answers", () => {
    const html = draw(["off-board", "backlog"]);
    expect(html).toContain("Remove from the board?");
  });
});
