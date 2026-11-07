import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

import type { Card } from "../providers/types";
import { ParentOptionList } from "./ParentPicker";

// The Under picker names each option's place: its ancestors, root first,
// small and muted, with their kinds — so two epics of one name under two
// goals are told apart — a long chain cut, in full in the tooltip.
const card = (over: Partial<Card>): Card => ({ itemId: "c", title: "a card", assignees: [], progress: 0, ...over }) as Card;
const goal = card({ itemId: "g", title: "Завершение проекта Freedom", type: "goal" });
const ini = card({ itemId: "i", title: "ТС-2", type: "initiative", parent: "g" });
const epic = card({ itemId: "e", title: "Billing", type: "epic", parent: "i" });

describe("the Under picker's options", () => {
  it("show each one's chain of ancestors, root first", () => {
    const html = renderToStaticMarkup(<ParentOptionList kind="task" options={[goal, ini, epic]} self="x" onPick={vi.fn()} />);
    const billing = html.slice(html.indexOf("drawer-pick-group\" role=\"group\" aria-label=\"Epic\""));
    expect(billing).toMatch(/class="drawer-pick-chain" title="Завершение проекта Freedom › ТС-2"/);
    expect(billing.indexOf("ТС-2")).toBeLessThan(billing.indexOf(">Billing<"));
    // A root has no chain to show.
    expect(html.match(/class="drawer-pick-chain"/g)?.length).toBe(2);
  });
});
