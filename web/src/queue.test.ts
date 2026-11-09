import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import { bestGap, epicOrderOf, epicSlotOrder, gathered, placed, reorderedInEpic, type QueueItem, type SlotCard } from "./queue";

// The ordering model's case table, shared with pkg/board
// (TestTheOrderingRulesMatchTheSharedCases): the same queues must give the
// same answers on both sides, or the board a person sees the moment they
// drop a card is not the board the server writes.
interface Cases {
  bestGap: { queue: string[]; epic: string; want: number }[];
  create: { queue: string[]; creates: string[]; want: string[] }[];
  moveEpic: { queue: string[]; epicOrder: string[]; epic: string; want: string[] }[];
  reorderInEpic: { queue: string[]; task: string; after?: string; before?: string; want: string[] }[];
}

const cases: Cases = JSON.parse(
  readFileSync(fileURLToPath(new URL("../../pkg/board/testdata/ordering_cases.json", import.meta.url)), "utf8"),
);

const item = (id: string): QueueItem => ({ id, epic: id[0] === "L" || id[0] === "N" ? null : id[0] });
const ids = (q: QueueItem[]) => q.map((t) => t.id);
const ORDER = epicOrderOf(["A", "B", "C", "X"]);

describe("the ordering rules, shared with the server", () => {
  it("puts an empty epic's first task in its best gap (Appendix A)", () => {
    for (const c of cases.bestGap) {
      expect(bestGap(c.queue.map(item), c.epic, ORDER), `${c.queue.join(" ")} / ${c.epic}`).toBe(c.want);
    }
  });

  it("places each new task in its epic's area, in creation order (Q3)", () => {
    for (const c of cases.create) {
      let q = c.queue.map(item);
      for (const id of c.creates) {
        q = placed(q, item(id), ORDER);
      }
      expect(ids(q), c.creates.join(" ")).toEqual(c.want);
    }
  });

  it("gathers a moved epic's tasks into one block, their order kept (Q5)", () => {
    for (const c of cases.moveEpic) {
      expect(ids(gathered(c.queue.map(item), c.epic, epicOrderOf(c.epicOrder))), c.queue.join(" ")).toEqual(c.want);
    }
  });

  it("permutes an epic's tasks over the slots they hold (Q6)", () => {
    for (const c of cases.reorderInEpic) {
      const anchor = c.after ? { after: c.after } : { before: c.before ?? "" };
      expect(ids(reorderedInEpic(c.queue.map(item), c.task, anchor)), c.task).toEqual(c.want);
    }
  });

  it("places a neutral task after its plan parent's last task, else at the end", () => {
    const q: QueueItem[] = [
      { id: "L1", epic: null, planParent: "I" },
      { id: "N1", epic: null },
    ];
    expect(ids(placed(q, { id: "L2", epic: null, planParent: "I" }, ORDER))).toEqual(["L1", "L2", "N1"]);
    expect(ids(placed(q, { id: "N2", epic: null }, ORDER))).toEqual(["L1", "N1", "N2"]);
  });
});

// What the acting tab shows after a reorder within an epic (Q6): the
// server's slot permutation, mirrored, since its own write is not echoed
// back. The epic's tasks of ANOTHER team keep their places.
describe("the board order after a reorder within an epic", () => {
  const t = (itemId: string, team: string, queued = true): SlotCard => ({ itemId, parent: "A", queued, team, task: true });
  const cards: SlotCard[] = [{ itemId: "A", queued: false, team: "", task: false }, t("A1", "T"), t("X", "U"), t("A2", "T")];

  it("permutes the team's slots and leaves the other team's task where it stands", () => {
    // Drawn A1 X A2; A2 dragged before A1 → the server writes A2 X A1.
    expect(epicSlotOrder(cards, "A2", { before: "A1" })).toEqual(["A", "A2", "X", "A1"]);
  });

  it("reads a drop next to the other team's task among the drawn children", () => {
    // A1 dropped right after X still stands before A2 among T's tasks: no slot changes hands.
    expect(epicSlotOrder(cards, "A1", { after: "X" })).toEqual(["A", "A1", "X", "A2"]);
    // A1 dropped after A2 does.
    expect(epicSlotOrder(cards, "A1", { after: "A2" })).toEqual(["A", "A2", "X", "A1"]);
  });

  it("is no slot move for a task in work", () => {
    expect(epicSlotOrder([...cards, t("W", "T", false)], "W", { before: "A1" })).toBeNull();
  });
});
