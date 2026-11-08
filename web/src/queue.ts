// The ORDERING MODEL's rules, mirrored at the level of one team's queue
// (docs/design/ordering.md; pkg/board/queue.go is the server's copy, and
// pkg/board/testdata/ordering_cases.json the case table both sides run).
//
// A queue is one team's open, parked, own-place tasks in board order. A
// task's EPIC is its nearest epic ancestor (none: a neutral task). The epic
// order is the board's epics in board order — global, the same for every
// team. The server computes the keys; the client mirrors where a card lands,
// so what a board shows the moment a gesture is made is what the server's
// answer will say.

/** One task of a queue: its id, its epic (null: neutral) and the plan item
 *  it hangs under when it has no epic. */
export interface QueueItem {
  id: string;
  epic: string | null;
  planParent?: string | null;
}

/** bestGap is where an epic's block — or an empty epic's first task — goes
 *  in a queue (Appendix A, board.BestGap): the gap contradicting the epic
 *  order least. Gap g costs the tasks before it whose epic comes AFTER the
 *  epic plus the tasks from it on whose epic comes BEFORE; neutral tasks and
 *  the epic's own count nothing. The earliest minimal run of equal-cost gaps
 *  wins; within it the first gap when the run starts right after a task of
 *  an earlier epic, else its last. */
export function bestGap(q: readonly QueueItem[], epic: string, order: ReadonlyMap<string, number>): number {
  const me = order.get(epic);
  const rel = q.map((t) => {
    if (me === undefined || !t.epic || t.epic === epic) {
      return 0;
    }
    const p = order.get(t.epic);
    if (p === undefined) {
      return 0;
    }
    return p < me ? -1 : p > me ? 1 : 0;
  });
  let before = rel.filter((r) => r < 0).length;
  let after = 0;
  const cost: number[] = [];
  for (let g = 0; g <= q.length; g++) {
    cost.push(after + before);
    if (g < q.length) {
      if (rel[g] > 0) {
        after++;
      }
      if (rel[g] < 0) {
        before--;
      }
    }
  }
  let best = 0;
  for (let g = 0; g < cost.length; g++) {
    if (cost[g] < cost[best]) {
      best = g;
    }
  }
  let end = best;
  while (end + 1 < cost.length && cost[end + 1] === cost[best]) {
    end++;
  }
  return best > 0 && rel[best - 1] < 0 ? best : end;
}

/** placeIndex is THE default place (Q3, board.PlaceSpot): the index in the
 *  queue (without the item) the item lands at — right after its epic's last
 *  task, else the epic's best gap; a neutral task right after its plan
 *  parent's last task, else at the end. */
export function placeIndex(q: readonly QueueItem[], item: QueueItem, order: ReadonlyMap<string, number>): number {
  const rest = q.filter((t) => t.id !== item.id);
  if (item.epic) {
    for (let i = rest.length - 1; i >= 0; i--) {
      if (rest[i].epic === item.epic) {
        return i + 1;
      }
    }
    return bestGap(rest, item.epic, order);
  }
  if (item.planParent) {
    for (let i = rest.length - 1; i >= 0; i--) {
      if (!rest[i].epic && rest[i].planParent === item.planParent) {
        return i + 1;
      }
    }
  }
  return rest.length;
}

/** placed is the queue with the item put at its default place. */
export function placed(q: readonly QueueItem[], item: QueueItem, order: ReadonlyMap<string, number>): QueueItem[] {
  const rest = q.filter((t) => t.id !== item.id);
  const at = placeIndex(rest, item, order);
  return [...rest.slice(0, at), item, ...rest.slice(at)];
}

/** gathered is the queue after an epic move (Q5, board.GatherPlan): the
 *  epic's tasks as one block, their order kept, at the best gap of the
 *  queue without them under the NEW epic order; nothing else moves. */
export function gathered(q: readonly QueueItem[], epic: string, order: ReadonlyMap<string, number>): QueueItem[] {
  const block = q.filter((t) => t.epic === epic);
  const rest = q.filter((t) => t.epic !== epic);
  if (block.length === 0) {
    return [...q];
  }
  const g = bestGap(rest, epic, order);
  // A block already together in a gap costing no more than the best one
  // stays where it is (board.GatherPlan).
  const first = q.findIndex((t) => t.epic === epic);
  const together = q.slice(first, first + block.length).every((t) => t.epic === epic);
  if (together && gapCost(rest, first, epic, order) <= gapCost(rest, g, epic, order)) {
    return [...q];
  }
  return [...rest.slice(0, g), ...block, ...rest.slice(g)];
}

/** gapCost is what bestGap weighs gap g by (board.GapCost). */
export function gapCost(q: readonly QueueItem[], g: number, epic: string, order: ReadonlyMap<string, number>): number {
  const me = order.get(epic);
  if (me === undefined) {
    return 0;
  }
  let n = 0;
  q.forEach((t, i) => {
    if (!t.epic || t.epic === epic) {
      return;
    }
    const p = order.get(t.epic);
    if (p === undefined) {
      return;
    }
    if ((i < g && p > me) || (i >= g && p < me)) {
      n++;
    }
  });
  return n;
}

/** reorderedInEpic is a task moved within its epic (Q6,
 *  board.ReorderInEpicPlan): the epic's tasks keep the slots they hold in
 *  the queue and take them in the new order — the task right after
 *  `after`, or right before `before`, among the epic's tasks. */
export function reorderedInEpic(
  q: readonly QueueItem[],
  task: string,
  anchor: { after: string } | { before: string },
): QueueItem[] {
  const moved = q.find((t) => t.id === task);
  if (!moved || !moved.epic) {
    return [...q];
  }
  const mine = q.filter((t) => t.epic === moved.epic && t.id !== task);
  let at = mine.length;
  if ("after" in anchor) {
    const i = mine.findIndex((t) => t.id === anchor.after);
    at = i < 0 ? mine.length : i + 1;
  } else {
    const i = mine.findIndex((t) => t.id === anchor.before);
    at = i < 0 ? 0 : i;
  }
  const order = [...mine.slice(0, at), moved, ...mine.slice(at)];
  let k = 0;
  return q.map((t) => (t.epic === moved.epic ? order[k++] : t));
}

/** epicOrderOf is the epic order a board reads: its epics in board order. */
export function epicOrderOf(epicIds: readonly string[]): Map<string, number> {
  return new Map(epicIds.map((id, i) => [id, i]));
}

/** SlotCard is what epicSlotOrder reads of a card of the board's listing. */
export interface SlotCard {
  itemId: string;
  parent?: string;
  /** In its team's queue: a parked, open, own-place task (onBacklog). */
  queued: boolean;
  /** The team it stands under (O13). */
  team: string;
  /** A task of the epic as the Strategy board draws it among the epic's
   *  children (not a plan item, a review or a subtask). */
  task: boolean;
}

/** epicSlotOrder is the BOARD's order after a task is moved within its epic
 *  on the Strategy board (Q6, board.ReorderInEpicPlan) — what the acting
 *  tab shows at once, since the server does not echo its own write back:
 *  the epic's tasks as drawn with the task put right after `after` (or
 *  right before `before`), and then the epic's PARKED tasks of the task's
 *  team take the places they held in the board's order, in that new order.
 *  Every other card — another team's task of the epic, a task in work —
 *  stays where it is. Null for a task that is in no queue: its move is an
 *  ordinary one. `cards` is the listing in board order. */
export function epicSlotOrder(
  cards: readonly SlotCard[],
  draggedId: string,
  anchor: { after?: string; before?: string },
): string[] | null {
  const dragged = cards.find((c) => c.itemId === draggedId);
  if (!dragged || !dragged.queued || !dragged.parent) {
    return null;
  }
  const drawn = cards.filter((c) => c.task && c.parent === dragged.parent && c.itemId !== draggedId).map((c) => c.itemId);
  let at = 0;
  if (anchor.after) {
    const i = drawn.indexOf(anchor.after);
    at = i < 0 ? drawn.length : i + 1;
  } else if (anchor.before) {
    const i = drawn.indexOf(anchor.before);
    at = i < 0 ? 0 : i;
  }
  const order = [...drawn.slice(0, at), draggedId, ...drawn.slice(at)];
  const slot = new Set(
    cards.filter((c) => c.task && c.parent === dragged.parent && c.queued && c.team === dragged.team).map((c) => c.itemId),
  );
  const moved = order.filter((id) => slot.has(id));
  let k = 0;
  return cards.map((c) => (slot.has(c.itemId) ? moved[k++] : c.itemId));
}
