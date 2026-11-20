import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { ParentGroupHead } from "./ParentGroupHead";
import type { GroupHead } from "../grouping";

// A group head shows whose the plan item is; its avatar is a press only where
// the board lets the group's work be handed over, and then on every head
// alike — one with a team and owner, and one with neither.
describe("a group head's avatar", () => {
  const head = (over: Partial<GroupHead>): GroupHead =>
    ({ uid: "e", type: "epic", title: "Billing", count: 2, done: 0, reviewOnly: false, ...over }) as GroupHead;
  const assign = { teams: ["portal"], onTeam: vi.fn(), onPerson: vi.fn(), people: ["ann"] };

  it("is a press on an owned plan item and on an unowned one alike", () => {
    for (const h of [head({ team: "portal", owner: "ann" }), head({})]) {
      const html = renderToStaticMarkup(<ParentGroupHead head={h} expanded={false} onToggle={() => {}} assign={assign} />);
      expect(html).toContain('class="card-assign group-head-assign"');
      expect(html).toContain('role="button"');
    }
  });

  it("is only a picture where nothing may be handed over", () => {
    const html = renderToStaticMarkup(<ParentGroupHead head={head({ team: "portal" })} expanded={false} onToggle={() => {}} />);
    expect(html).toContain('class="card-assign"');
    expect(html).not.toContain("role=");
  });
});
