import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { RemoveChoiceDialog } from "./RemoveChoiceDialog";

// The one choice dialog every × puts: the Team board's. The hierarchy's ×
// borrows it with its own two answers, so this pins that the dialog names
// them and says what deleting does to the rows under a planning card.
describe("the choice dialog, asked by the hierarchy's ×", () => {
  function draw(choices: Parameters<typeof RemoveChoiceDialog>[0]["choices"], note?: string, review = false) {
    return renderToStaticMarkup(
      <RemoveChoiceDialog
        title="Billing v2"
        review={review}
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
    const html = draw(["backlog", "off-board"]);
    expect(html).toContain("Remove from the board?");
  });

  // K3: the keeping answer of a task's × is its team's backlog, and the
  // dialog says so — not "back in its week", which a task no longer has.
  it("says the backlog answer puts the task back in its team's backlog, with no week", () => {
    const html = draw(["backlog", "off-board"]);
    expect(html).toContain("Send it back to the backlog");
    expect(html).toContain("its team&#x27;s backlog");
    expect(html).not.toMatch(/week/i);
  });

  // The backlog answer is listed ABOVE taking the card off the board.
  it("lists the backlog above taking it off the board", () => {
    const html = draw(["backlog", "off-board"]);
    expect(html.indexOf("Send it back to the backlog")).toBeLessThan(html.indexOf("Take it off the board"));
  });

  // K10: on a review card the backlog answer takes the reviewer off; the
  // review waits in the reviewed task's team backlog and the task stays on
  // review.
  it("offers a review card Back to backlog, and says the task stays on review", () => {
    const html = draw(["backlog", "off-board"], undefined, true);
    expect(html).toContain("Back to backlog");
    expect(html).not.toContain("Send it back to the backlog");
    expect(html).toMatch(/stays on review/);
    expect(html.indexOf("Back to backlog")).toBeLessThan(html.indexOf("Take it off the board"));
  });

  it("says a project card's Unassigned answer leaves it in its column", () => {
    const html = draw(["unassign"]);
    expect(html).toContain("Move it to Unassigned");
    expect(html).toContain("its column");
    expect(html).not.toMatch(/week/i);
  });
});
