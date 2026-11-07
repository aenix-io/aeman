import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

import type { Card } from "../providers/types";
import { PlanTree } from "./PlanTree";

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
