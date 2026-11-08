import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { BacklogEpics } from "./BacklogEpics";
import { epicRows } from "../backlogboard";
import type { Card } from "../providers/types";

// The Backlog's Epics mode draws one row per epic of the shown teams in the
// epic order — the empty ones too — with its plan above, its waiting tasks
// and Split. Markup only: the drag is epicDrop's, pinned in
// backlogboard.test.ts.

const card = (over: Partial<Card>): Card =>
  ({ itemId: "c", title: "a card", assignees: [], progress: 0, ...over }) as Card;
const I = { uid: "i", type: "initiative" as const, title: "ТС2-7 · Kubernetes и S3" };
const ep = (itemId: string, title: string) => card({ itemId, title, type: "epic", team: "portal", parent: "i", lineage: [I] });
const under = (itemId: string, epic: string) =>
  card({ itemId, team: "portal", parked: true, parent: epic, lineage: [I, { uid: epic, type: "epic", title: epic }] });

describe("the Backlog's Epics mode", () => {
  const A = ep("A", "Object storage");
  const B = ep("B", "Kubernetes upgrade");
  const C = ep("C", "Not decomposed yet");
  const a1 = under("a1", "A");
  const b1 = under("b1", "B");
  const a2 = under("a2", "A");
  const rows = epicRows([A, B, C, a1, b1, a2], [a1, b1, a2], ["portal"], ["B", "A", "C", "a1", "b1", "a2"]);
  const html = renderToStaticMarkup(<BacklogEpics rows={rows} loading={false} onMove={vi.fn()} onOpen={vi.fn()} onOpenParent={vi.fn()} />);

  it("draws the epics in the epic order, the empty one included", () => {
    const at = (t: string) => html.indexOf(t);
    expect(at("Kubernetes upgrade")).toBeLessThan(at("Object storage"));
    expect(at("Object storage")).toBeLessThan(at("Not decomposed yet"));
    expect(html.match(/data-epic-row=""/g)?.length).toBe(3);
  });

  it("says how many tasks wait, and empty for none", () => {
    expect(html).toContain("2 waiting");
    expect(html).toContain("1 waiting");
    expect(html).toContain(">empty<");
  });

  it("marks the scattered epic Split and carries the initiative on the right", () => {
    expect(html.match(/class="split-badge"/g)?.length).toBe(1);
    expect(html).toContain("ТС2-7 · Kubernetes и S3");
  });

  it("is a list of draggable rows", () => {
    expect(html.match(/draggable="true"/g)?.length).toBe(3);
  });
});

// The right side is two aligned columns, the count and the chain, the chain
// the Tasks rows' own component.
describe("the Epics rows' layout", () => {
  it("puts the count and the chain in their own cells, the chain the Tasks rows' nav", () => {
    const html = renderToStaticMarkup(
      <BacklogEpics
        rows={epicRows([ep("A", "Object storage"), under("a1", "A")], [under("a1", "A")], ["portal"])}
        loading={false}
        onMove={vi.fn()}
        onOpen={vi.fn()}
        onOpenParent={vi.fn()}
      />,
    );
    expect(html).toMatch(/<span class="backlog-epic-count"[^>]*>1 waiting<\/span><span class="backlog-epic-ctx backlog-epic-ctx-goal"><\/span><span class="backlog-epic-ctx backlog-epic-ctx-initiative"><button/);
    expect(html).toContain('title="The initiative «ТС2-7 · Kubernetes и S3»"');
  });
});
