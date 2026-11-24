import { describe, expect, it } from "vitest";

import { counterpartOf } from "./counterpart";
import type { Card } from "./providers/types";

const card = (itemId: string, over: Partial<Card> = {}): Card =>
  ({ itemId, title: itemId, assignees: [], progress: 0, ...over }) as Card;

// The small avatar on a card is its counterpart: on a review card the person
// whose work it reviews, on a task the person reviewing it. The linked card
// when the board holds it — it is the freshest — else what the server said
// about it on the card itself: a Me board holds only the reader's cards, so a
// review of somebody else's task found no task there and drew no avatar.
describe("counterpartOf", () => {
  const task = card("t", { assignees: ["androndo"] });
  const review = card("r", { assignees: ["kvaps"], reviewOf: "t", reviewed: { uid: "t", title: "T", assignees: ["androndo"] } });
  it("reads the linked card when the board holds it", () => {
    expect(counterpartOf(review, [task, review])).toEqual(["androndo"]);
    expect(counterpartOf(task, [task, review])).toEqual(["kvaps"]);
  });
  it("falls back to what the server said when the board does not", () => {
    expect(counterpartOf(review, [review])).toEqual(["androndo"]);
    expect(counterpartOf(card("t2", { reviewedBy: "lexfrei" }), [])).toEqual(["lexfrei"]);
  });
  it("has none on a card nothing reviews", () => {
    expect(counterpartOf(card("x"), [])).toEqual([]);
  });
});
