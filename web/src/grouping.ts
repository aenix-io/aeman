// "Group by plan" on the Team and Me boards: instead of each task standing
// alone, the tasks of one cell are gathered under the plan item they stand in
// — their DIRECT parent, whichever level it is. An epic inside an initiative
// and the initiative's own tasks are two groups side by side; the levels
// above a group are only named, never drawn. Subtasks are not drawn at all in
// this mode. It is a way of drawing the cell the board already holds: the
// filters, the day rule and the order are the board's, and nothing here is
// sent to the server.
import { isComplete } from "./stages";
import type { Card, CardType, LineageRef } from "./providers/types";

/** The plan item a group is drawn for. The cards in no plan are a group of
 *  their own, "Ungrouped": uid "", kind "none". */
export interface ParentGroup {
  uid: string;
  type: CardType | "none";
  title: string;
  /** The title of the plan item above it, when it has one. */
  above?: string;
  /** Whose the plan item is: its team (inherited) and its own owner. */
  team?: string;
  owner?: string;
}

/** The head of a group in one cell. */
export interface GroupHead extends ParentGroup {
  /** The cards of the group in this cell, and how many of them are done. */
  count: number;
  done: number;
  /** Every card of the group in this cell is a review card: the head is
   *  drawn as one, with the review card's stripe. */
  reviewOnly: boolean;
}

/** parentGroupOf is the plan item a card is drawn under: the nearest entry of
 *  its lineage that has a level. A task's lineage ends at its parent; a review
 *  card carries the reviewed task's lineage, which ends at that task when it
 *  is a subtask — hence the nearest one WITH a level. Null for a card in no
 *  plan, which stays a card of its own. */
export function parentGroupOf(c: Pick<Card, "lineage">): ParentGroup | null {
  const line = c.lineage ?? [];
  for (let i = line.length - 1; i >= 0; i--) {
    const at = line[i];
    if (at.type) {
      const g: ParentGroup = { uid: at.uid, type: at.type, title: at.title };
      if (at.team) {
        g.team = at.team;
      }
      if (at.owner) {
        g.owner = at.owner;
      }
      const up = line[i - 1];
      if (up) {
        g.above = up.title;
      }
      return g;
    }
  }
  return null;
}

/** The group of the cards that stand in no plan item. */
export const UNGROUPED: ParentGroup = { uid: "", type: "none", title: "Ungrouped" };

/** arrangeByParent orders one cell's cards for drawing in groups: each group
 *  gathers where its first card stood, its cards in the cell's order after
 *  it — the cards in no plan as one more, "Ungrouped". heads is keyed by the
 *  first card of each group (the head is drawn above it); member holds every
 *  card drawn inside a group. */
export function arrangeByParent(cells: readonly Card[]): {
  cards: Card[];
  heads: Map<string, GroupHead>;
  member: Set<string>;
} {
  const order: (Card | string)[] = [];
  const byGroup = new Map<string, { g: ParentGroup; cards: Card[] }>();
  for (const c of cells) {
    const g = parentGroupOf(c) ?? UNGROUPED;
    const got = byGroup.get(g.uid);
    if (got) {
      got.cards.push(c);
    } else {
      byGroup.set(g.uid, { g, cards: [c] });
      order.push(g.uid);
    }
  }
  const cards: Card[] = [];
  const heads = new Map<string, GroupHead>();
  const member = new Set<string>();
  for (const at of order) {
    if (typeof at !== "string") {
      cards.push(at);
      continue;
    }
    const { g, cards: kids } = byGroup.get(at)!;
    heads.set(kids[0].itemId, {
      ...g,
      count: kids.length,
      done: kids.filter((c) => isComplete(c)).length,
      reviewOnly: kids.every((c) => !!c.reviewOf),
    });
    for (const c of kids) {
      cards.push(c);
      member.add(c.itemId);
    }
  }
  return { cards, heads, member };
}

/** The head of a group is drawn as a row of the cell's list — a row nothing
 *  drags, that the cell's cards nest under the way a subtask nests under its
 *  task — so the board's own drag machinery (the indent, the middle-band
 *  drop, the exit gesture) is what moves a task between plan items. Its id is
 *  the cell's and the plan item's: one plan item heads a group in each cell
 *  it has cards in. */
const PLAN_ROW = "plan:";

export const planRowId = (cell: string, uid: string): string => `${PLAN_ROW}${cell}:${uid}`;
export const isPlanRow = (id: string): boolean => id.startsWith(PLAN_ROW);
/** The plan item a head row stands for (a uid holds no colon). */
export const planRowUid = (id: string): string => id.slice(id.lastIndexOf(":") + 1);

/** A head row: the group it heads and the lineage its cards carry down to
 *  the plan item — what a card dropped into it takes. */
export interface PlanRow {
  head: GroupHead;
  lineage: LineageRef[];
  /** Every card of the group in this cell, in the cell's order, folded or
   *  not — what moves when the head is dragged. */
  cards: string[];
  /** The team the group's cards stand under: a task created in the group
   *  joins that team's sprint. */
  team?: string;
}

/** groupedRows is one cell's list as drawn grouped: each group is its head
 *  row followed by its cards when the group is expanded (folded, the head
 *  stands alone), and a card in no plan keeps its own place. parentOf names
 *  the head row each drawn card nests under. */
export function groupedRows(
  cell: string,
  cards: readonly Card[],
  expanded: (rowId: string) => boolean,
): { rows: Card[]; heads: Map<string, PlanRow>; parentOf: Map<string, string> } {
  const a = arrangeByParent(cards);
  const rows: Card[] = [];
  const heads = new Map<string, PlanRow>();
  const parentOf = new Map<string, string>();
  let current: string | null = null;
  for (const c of a.cards) {
    const head = a.heads.get(c.itemId);
    if (head) {
      current = planRowId(cell, head.uid);
      const line = c.lineage ?? [];
      const upTo = line.findIndex((l) => l.uid === head.uid);
      heads.set(current, {
        head,
        lineage: line.slice(0, upTo + 1),
        cards: [],
        team: c.effectiveTeam || c.team || undefined,
      });
      rows.push({ itemId: current, title: head.title, assignees: [], progress: 0 } as Card);
    }
    if (!a.member.has(c.itemId)) {
      current = null;
      rows.push(c);
      continue;
    }
    if (current) {
      heads.get(current)!.cards.push(c.itemId);
    }
    if (current && expanded(current)) {
      parentOf.set(c.itemId, current);
      rows.push(c);
    }
  }
  return { rows, heads, parentOf };
}

/** planDrop is what a drop does to a card's plan item, by the head row the
 *  drag left it nested under (null: dropped out of every group). Null when
 *  nothing changes — the same plan item, or a review card, whose place is
 *  its task's (H2) and is never moved by a drag. An empty parent takes the
 *  task out of its plan item. */
export function planDrop(
  card: Card,
  underRow: string | null,
  heads: ReadonlyMap<string, PlanRow>,
): { parent: string; lineage: LineageRef[] } | null {
  if (card.reviewOf) {
    return null;
  }
  const from = parentGroupOf(card)?.uid ?? "";
  const to = underRow ? heads.get(underRow) : undefined;
  const toUid = to?.head.uid ?? "";
  if (underRow && !to) {
    return null;
  }
  if (toUid === from) {
    return null;
  }
  return { parent: toUid, lineage: to ? to.lineage : [] };
}

/** expandRows turns a cell's drawn ids back into its cards: a head row
 *  stands for every card of its group the list does not show (a folded
 *  group), in its place. What the board order is computed over — a drop
 *  among folded groups, and a head dragged with all its cards. */
export function expandRows(ids: readonly string[], heads: ReadonlyMap<string, PlanRow>): string[] {
  const seen = new Set(ids.filter((id) => !isPlanRow(id)));
  const out: string[] = [];
  for (const id of ids) {
    if (!isPlanRow(id)) {
      out.push(id);
      continue;
    }
    for (const c of heads.get(id)?.cards ?? []) {
      if (!seen.has(c)) {
        seen.add(c);
        out.push(c);
      }
    }
  }
  return out;
}

/** A run of the Backlog drawn grouped: the neighbouring cards of one plan
 *  item (or of none, "Ungrouped") and the head drawn above them. */
export interface PlanRun {
  /** The head row's id: the plan item and the run's first card, so a plan
   *  item split in two places heads two runs. */
  id: string;
  head: GroupHead;
  cards: Card[];
}

/** adjacentRuns is the Backlog's list as drawn grouped: one queue, so a group
 *  is the cards of one plan item STANDING TOGETHER — a card of another plan
 *  item between them splits the plan item into two runs, and bringing them
 *  together again makes one. The cards in no plan item run under
 *  "Ungrouped" the same way. Nothing is reordered: the runs are the list,
 *  cut where the plan item changes. */
export function adjacentRuns(cards: readonly Card[]): PlanRun[] {
  const runs: PlanRun[] = [];
  let open: { g: ParentGroup; cards: Card[] } | null = null;
  const close = () => {
    if (!open) {
      return;
    }
    const kids = open.cards;
    runs.push({
      id: `${PLAN_ROW}backlog:${open.g.uid}:${kids[0].itemId}`,
      head: {
        ...open.g,
        count: kids.length,
        done: kids.filter((c) => isComplete(c)).length,
        reviewOnly: kids.every((c) => !!c.reviewOf),
      },
      cards: kids,
    });
  };
  for (const c of cards) {
    const g = parentGroupOf(c) ?? UNGROUPED;
    if (open && open.g.uid === g.uid) {
      open.cards.push(c);
      continue;
    }
    close();
    open = { g, cards: [c] };
  }
  close();
  return runs;
}

/** reassignable is what a group head's avatar hands over (Plans on the Team
 *  and Me boards): the group's OPEN tasks in that cell. A finished task keeps
 *  who did it, and a review card is its reviewer's — the review's own menu
 *  changes that. The plan item's own owner is not touched: the head shows
 *  whose the plan item is, its menu moves the work. */
export function reassignable(cards: readonly Card[]): Card[] {
  return cards.filter((c) => !c.reviewOf && !isComplete(c));
}

/** blockAnchor is where a moved group stands, as ONE anchor for the whole
 *  block (S21, provider.moveTogether): behind the card of its cell in front
 *  of it; leading the cell, before the cell's card behind it; the whole cell,
 *  by the board's order in front of its first card (null: the top). */
export function blockAnchor(
  cell: readonly string[],
  block: readonly string[],
  order: readonly string[],
): { after: string | null } | { before: string } {
  const at = cell.indexOf(block[0]);
  if (at > 0) {
    return { after: cell[at - 1] };
  }
  const behind = cell.find((id, i) => i >= at && !block.includes(id));
  if (at === 0 && behind) {
    return { before: behind };
  }
  const i = order.indexOf(block[0]);
  return { after: i > 0 ? order[i - 1] : null };
}

/** takeable is what a Backlog run's Take takes (K11): every open card of the
 *  run nobody has, a review nobody has taken included — Take makes it the
 *  viewer's review, as on its own row. A finished card, and one somebody
 *  has, stay. */
export function takeable(cards: readonly Card[]): Card[] {
  return cards.filter((c) => !isComplete(c) && c.assignees.length === 0);
}
