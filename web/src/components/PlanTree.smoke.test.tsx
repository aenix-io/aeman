import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

import type { Card } from "../providers/types";
import { PlanTree, addPlanFor, initialFolded, nextAdding } from "./PlanTree";

// The drawer's "Under it" is the Roadmap's tree rooted one level below the
// card the drawer shows: the Roadmap's rows — kind, fold, title, owner,
// status — with no timeline and no Start/End; a plan item's + and the add
// row at the foot, which offers the next level down first.
const card = (over: Partial<Card>): Card => ({ itemId: "c", title: "a card", assignees: [], progress: 0, ...over }) as Card;
const goal = card({ itemId: "g", title: "Grow", type: "goal", team: "portal" });
const ini = card({ itemId: "i", title: "Billing v2", type: "initiative", parent: "g" });
const epic = card({ itemId: "e", title: "Invoices", type: "epic", parent: "i" });
const task = card({ itemId: "t", title: "Render PDF", parent: "e", lineage: [{ uid: "e", type: "epic", title: "Invoices" }] });
const draw = (root: Card, extra: Record<string, unknown> = {}) =>
  renderToStaticMarkup(<PlanTree cards={[goal, ini, epic, task]} root={root} onOpen={vi.fn()} onCreate={vi.fn()} onRemove={vi.fn()} {...extra} />);

describe("the drawer's tree", () => {
  it("is rooted one level below the card, with every level under it", () => {
    const html = draw(goal);
    expect(html).not.toContain(">Grow<");
    expect(html.indexOf(">Billing v2<")).toBeLessThan(html.indexOf(">Invoices<"));
    expect(html.indexOf(">Invoices<")).toBeLessThan(html.indexOf(">Render PDF<"));
    expect(html.match(/role="treeitem"/g)?.length).toBe(3);
  });

  it("draws the Roadmap's rows, without dates or a timeline", () => {
    const html = draw(goal);
    expect(html).toContain("roadmap-cell-title");
    expect(html).toContain("roadmap-cell-owner");
    expect(html).toContain("roadmap-status");
    expect(html).not.toContain("roadmap-cell-date");
    expect(html).not.toContain("roadmap-timeline");
    // The inherited team shows on a row with none of its own (O3).
    expect(html).toContain("roadmap-owner-inherited");
  });

  it("offers a + on a plan item, and an add row for the card's next level first", () => {
    const html = draw(goal);
    expect(html).toMatch(/aria-label="Add under Billing v2"/);
    expect(html).toMatch(/>\+ Add initiative or epic<\/button>/);
    expect(draw(epic)).toMatch(/>\+ Add task<\/button>/);
  });

  it("offers nothing to add on a record", () => {
    const html = draw(goal, { readOnly: true });
    expect(html).not.toContain("+ Add");
    expect(html).not.toContain("Add under");
  });
});

// Every row of the drawer's tree has its × — a plan item's, a task's, a
// project card's, a subtask's — answered by the dialog with that card's
// choices; a record has none.
describe("the × on the drawer's rows", () => {
  it("is on every row", () => {
    const slot = card({ itemId: "p", title: "Q2 plan", scope: "S", project: "P", parent: "i" });
    const sub = card({ itemId: "s", title: "Fonts", parent: "t", lineage: [{ uid: "t", title: "Render PDF" }] });
    const html = renderToStaticMarkup(
      <PlanTree cards={[goal, ini, epic, task, sub, slot]} root={goal} onOpen={vi.fn()} onCreate={vi.fn()} onRemove={vi.fn()} />,
    );
    for (const title of ["Billing v2", "Invoices", "Render PDF", "Fonts", "Q2 plan"]) {
      expect(html).toContain(`aria-label="Delete ${title}"`);
    }
  });
});

// A row opens from the keyboard: it is focusable, Enter or Space opens it,
// and it says whether it is unfolded.
describe("the drawer's rows and the keyboard", () => {
  it("are focusable tree items that say whether they are open", () => {
    const html = draw(goal);
    expect(html).toMatch(/role="treeitem"[^>]*tabindex="0"[^>]*aria-expanded="true"/);
    expect(html).toMatch(/class="roadmap-rowgroup" role="none"/);
  });
});

// What is being made stands under the row it is made under, not at the
// foot of the tree.
describe("a card being made", () => {
  it("stands under the row whose + made it", () => {
    const html = draw(goal, { pending: [{ id: "p1", title: "New epic", parent: "i" }] });
    expect(html.indexOf(">New epic<")).toBeGreaterThan(html.indexOf(">Billing v2<"));
    expect(html.indexOf(">New epic<")).toBeLessThan(html.indexOf(">Invoices<"));
  });
});

describe("the add form's rules", () => {
  it("offers a nested row its own levels, the next first", () => {
    expect(addPlanFor(ini, [goal, ini]).levels).toEqual(["epic"]);
    expect(addPlanFor(goal, [goal]).levels).toEqual(["initiative", "epic"]);
  });

  it("says where a task made under a plan item stands", () => {
    expect(addPlanFor(epic, [goal, ini, epic]).hint).toMatch(/stands under «portal»/);
    const lone = card({ itemId: "le", title: "Lone", type: "epic" });
    expect(addPlanFor(lone, [lone]).hint).toMatch(/no-team backlog/);
    expect(addPlanFor(goal, [goal]).hint).toBeNull();
  });

  it("opens again, empty, after a card is made, and closes when it was not", () => {
    expect(nextAdding({ under: "i", round: 0 }, true)).toEqual({ under: "i", round: 1 });
    expect(nextAdding({ under: "i", round: 3 }, false)).toBeNull();
    expect(nextAdding(null, true)).toBeNull();
  });

  it("starts a big tree folded below its first level", () => {
    const many = Array.from({ length: 40 }, (_, k) => card({ itemId: `t${k}`, title: `T${k}`, parent: "e", lineage: [{ uid: "e", type: "epic", title: "Invoices" }] }));
    const cards = [goal, ini, epic, ...many];
    expect([...initialFolded(cards, "g")]).toEqual(["i"]);
    expect([...initialFolded([goal, ini, epic, task], "g")]).toEqual([]);
  });
});
