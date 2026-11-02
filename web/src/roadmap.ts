// What the Roadmap draws: the hierarchy as a tree of rows and a timeline of
// bars, card by card. It lives in a file of its own for the reason every
// other mirror does — the tree is assembled from `parent`, the status of a
// planning card is the server's derived one, and a bar is a card's own
// dates — so a component re-deciding any of it would drift from what the
// listing says (docs/design/hierarchy.md, "The Roadmap tab").
import { addDays, mondayOf } from "./date";
import { isComplete, isInProgress } from "./stages";
import { isPlanning, isSubtask } from "./subtasks";
import type { Card } from "./providers/types";

/** RoadmapKind is what a row IS: the three planning levels, a task, a subtask
 *  (a task under a task), or a project card (a task in a Scope column). The
 *  last three are one level — the tree's leaves — told apart for the glyph. */
export type RoadmapKind = "goal" | "initiative" | "epic" | "task" | "subtask" | "project";

/** LEVELS orders the kinds top-down; the depth selector picks a range of
 *  them. A project card is a task's level, a subtask one below. */
export const LEVELS: readonly RoadmapKind[] = ["goal", "initiative", "epic", "task", "subtask"];

/** levelOf is a kind's rank in LEVELS: 0 for a goal, 4 for a subtask. A
 *  project card sits at the task level. Mirrors board.Level, inverted. */
export function levelOf(kind: RoadmapKind): number {
  if (kind === "project") {
    return LEVELS.indexOf("task");
  }
  return LEVELS.indexOf(kind);
}

/** kindOf is a card's kind on the Roadmap: its planning type, else a project
 *  card by its column, a subtask by the parent AS A TASK, else a task. */
export function kindOf(c: Pick<Card, "type" | "scope" | "parent" | "lineage">): RoadmapKind {
  if (isPlanning(c)) {
    return c.type as RoadmapKind;
  }
  if (c.scope) {
    return "project";
  }
  return isSubtask(c) ? "subtask" : "task";
}

/** RoadmapNode is one card in the tree, with the rows under it. */
export interface RoadmapNode {
  card: Card;
  kind: RoadmapKind;
  children: RoadmapNode[];
}

/** buildTree is the forest the listing makes: a card goes under the card its
 *  `parent` names when that card is in the listing, and is a root otherwise.
 *  The roadmap listing is closed under ancestors (a card is on it because a
 *  planning card is above it), so a root with a parent is a card whose
 *  parent the reader may not see — it is drawn at the top rather than
 *  dropped, since a hole is worse than a flat row. Children keep listing
 *  order: the listing is board order, and the Ordering frames keep it so.
 *  Roots come planning cards first, by level, then in listing order. */
export function buildTree(cards: readonly Card[]): RoadmapNode[] {
  const byId = new Map<string, RoadmapNode>();
  for (const card of cards) {
    byId.set(card.itemId, { card, kind: kindOf(card), children: [] });
  }
  const roots: RoadmapNode[] = [];
  for (const card of cards) {
    const node = byId.get(card.itemId)!;
    const up = card.parent ? byId.get(card.parent) : undefined;
    if (up && up !== node) {
      up.children.push(node);
    } else {
      roots.push(node);
    }
  }
  return roots.sort((a, b) => levelOf(a.kind) - levelOf(b.kind) || 0);
}

/** DepthRange is the depth selector: the kinds drawn, inclusive, from a top
 *  level to a bottom one ("initiative to subtask"). Default: initiative to
 *  task — the row a lead reads, with the work under it, and no subtasks. */
export interface DepthRange {
  from: RoadmapKind;
  to: RoadmapKind;
}

export const DEFAULT_DEPTH: DepthRange = { from: "initiative", to: "task" };

/** visibleLevels is the set of kinds a depth range draws. A project card is
 *  drawn whenever tasks are. */
export function visibleLevels(range: DepthRange): Set<RoadmapKind> {
  const lo = Math.min(levelOf(range.from), levelOf(range.to));
  const hi = Math.max(levelOf(range.from), levelOf(range.to));
  const out = new Set<RoadmapKind>();
  for (const k of LEVELS) {
    const l = levelOf(k);
    if (l >= lo && l <= hi) {
      out.add(k);
    }
  }
  if (out.has("task")) {
    out.add("project");
  }
  return out;
}

/** Row is one line of the Roadmap: a node at a depth, and whether it has
 *  rows under it that the reader can unfold. */
export interface Row {
  node: RoadmapNode;
  depth: number;
  /** Children the depth range would draw — what the toggle unfolds. */
  foldable: number;
}

/** rows flattens the tree into the lines drawn, top-down. A level ABOVE the
 *  range is skipped and its children stand in its place (hiding goals shows
 *  their initiatives as roots); a level BELOW the range is cut with its
 *  subtree. A collapsed node draws itself and nothing under it. */
export function rows(
  tree: readonly RoadmapNode[],
  range: DepthRange,
  expanded: (id: string) => boolean,
): Row[] {
  const visible = visibleLevels(range);
  const lo = Math.min(levelOf(range.from), levelOf(range.to));
  const out: Row[] = [];
  const walk = (nodes: readonly RoadmapNode[], depth: number) => {
    for (const node of nodes) {
      const level = levelOf(node.kind);
      if (level < lo) {
        // Above the range: the rows under it are drawn in its place.
        walk(node.children, depth);
        continue;
      }
      if (!visible.has(node.kind)) {
        continue; // below the range, with everything under it
      }
      const foldable = node.children.filter((k) => visible.has(k.kind) || levelOf(k.kind) < lo).length;
      out.push({ node, depth, foldable });
      if (expanded(node.card.itemId)) {
        walk(node.children, depth + 1);
      }
    }
  };
  walk(tree, 0);
  return out;
}

/** RoadmapStatus is the pill a row wears: a planning card's derived
 *  standing, or a task's own — done, in progress, to do. */
export type RoadmapStatus = "backlog" | "todo" | "in-progress" | "done";

/** statusOf is a row's status: for a planning card the server's derived one
 *  (`hierarchy.status`, H5 — backlog until a task under it starts); for a
 *  task, done when complete, in progress when its bar has moved or it waits
 *  on somebody (review, locked), to do otherwise. */
export function statusOf(c: Pick<Card, "type" | "hierarchy" | "stage" | "progress">): RoadmapStatus {
  if (isPlanning(c)) {
    return c.hierarchy?.status ?? "backlog";
  }
  if (isComplete(c)) {
    return "done";
  }
  if (isInProgress(c) || (c.progress ?? 0) > 0 || c.stage === "review" || c.stage === "locked") {
    return "in-progress";
  }
  return "todo";
}

export const STATUS_LABEL: Record<RoadmapStatus, string> = {
  backlog: "Backlog",
  todo: "To do",
  "in-progress": "In progress",
  done: "Done",
};

/** Bar is a card's span on the timeline: from its start to its end, both
 *  inclusive; `point` when the card has one date only. */
export interface Bar {
  start: string;
  end: string;
  point: boolean;
}

/** barOf is a card's bar: `start`..`day` for every kind alike (a goal and a
 *  task are placed by the same two fields); one date is a point; no date is
 *  no bar — the row stands without one. An end before the start is a
 *  point at the start: a plan that reads backwards is not drawn backwards. */
export function barOf(c: Pick<Card, "startDate" | "day">): Bar | null {
  const start = c.startDate || "";
  const end = c.day || "";
  if (!start && !end) {
    return null;
  }
  if (!start || !end || end < start) {
    const at = start || end;
    return { start: at, end: at, point: true };
  }
  return { start, end, point: start === end };
}

export type Zoom = "3m" | "1y" | "fit";

export interface DateRange {
  from: string;
  to: string;
}

/** daysBetween is b − a in whole days (negative when b is earlier). */
export function daysBetween(a: string, b: string): number {
  const [ay, am, ad] = a.split("-").map(Number);
  const [by, bm, bd] = b.split("-").map(Number);
  return Math.round((Date.UTC(by, bm - 1, bd) - Date.UTC(ay, am - 1, ad)) / 86_400_000);
}

/** rangeFor is the window the timeline shows. Three months and a year open
 *  two weeks before the anchor's Monday — the recent past is context, the
 *  future is the plan. Fit is the span of every bar with a week of margin
 *  each side, and three months when there is nothing to fit. */
export function rangeFor(zoom: Zoom, anchor: string, bars: readonly (Bar | null)[] = []): DateRange {
  const monday = mondayOf(anchor);
  if (zoom === "fit") {
    let lo = "";
    let hi = "";
    for (const b of bars) {
      if (!b) {
        continue;
      }
      lo = !lo || b.start < lo ? b.start : lo;
      hi = !hi || b.end > hi ? b.end : hi;
    }
    if (lo && hi) {
      return { from: addDays(lo, -7), to: addDays(hi, 7) };
    }
  }
  const from = addDays(monday, -14);
  return { from, to: addDays(from, zoom === "1y" ? 365 : 91) };
}

/** Tick is one line of the timeline's header: where it stands and what it
 *  says. */
export interface Tick {
  date: string;
  label: string;
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/** ticks are the header's lines: one a week (its Monday, "16 Feb") for a
 *  window of up to four months, one a month ("Feb 2026") beyond that — the
 *  same rule for every zoom, so Fit picks the scale its span needs. */
export function ticks(range: DateRange): Tick[] {
  const span = daysBetween(range.from, range.to);
  const out: Tick[] = [];
  if (span <= 124) {
    for (let d = mondayOf(range.from); d <= range.to; d = addDays(d, 7)) {
      if (d >= range.from) {
        const [, m, day] = d.split("-").map(Number);
        out.push({ date: d, label: `${day} ${MONTHS[m - 1]}` });
      }
    }
    return out;
  }
  const [fy, fm] = range.from.split("-").map(Number);
  for (let y = fy, m = fm; ; ) {
    const d = `${y}-${String(m).padStart(2, "0")}-01`;
    if (d > range.to) {
      break;
    }
    if (d >= range.from) {
      out.push({ date: d, label: `${MONTHS[m - 1]} ${y}` });
    }
    m++;
    if (m > 12) {
      m = 1;
      y++;
    }
  }
  return out;
}

/** fractionOf is where a date stands in the window, 0 at its start and 1 a
 *  day past its end — the timeline multiplies it by its width. A date
 *  outside the window lands outside [0, 1], which is how a bar is clipped. */
export function fractionOf(date: string, range: DateRange): number {
  const span = daysBetween(range.from, range.to) + 1;
  return daysBetween(range.from, date) / span;
}

/** Placed is a bar's position in the window, as fractions of its width, or
 *  null when the bar lies wholly outside it. A point is given a day. */
export interface Placed {
  left: number;
  width: number;
  clippedLeft: boolean;
  clippedRight: boolean;
}

export function place(bar: Bar, range: DateRange): Placed | null {
  if (bar.end < range.from || bar.start > range.to) {
    return null;
  }
  const left = Math.max(0, fractionOf(bar.start, range));
  const right = Math.min(1, fractionOf(addDays(bar.end, 1), range));
  return {
    left,
    width: Math.max(right - left, 0),
    clippedLeft: bar.start < range.from,
    clippedRight: bar.end > range.to,
  };
}

/** PlanningReparent is what deleting a planning card does to the rows under
 *  it on the server (reparentChildren): each moves up to the deleted card's
 *  own parent, or to the top. The optimistic client applies the same, or the
 *  children — pointing at a card that is gone — would vanish from the tree
 *  until the next listing. */
export interface PlanningReparent {
  itemId: string;
  parent: string | undefined;
}

export function planningRemoval(
  cards: readonly Pick<Card, "itemId" | "parent">[],
  id: string,
): PlanningReparent[] {
  const gone = cards.find((c) => c.itemId === id);
  const up = gone?.parent || undefined;
  return cards.filter((c) => c.parent === id).map((c) => ({ itemId: c.itemId, parent: up }));
}

/** allowedParentKinds is what a card of a kind may sit under (H2): a level
 *  strictly above it, with a task's one exception — another task, which
 *  makes it a subtask (that flow is the group gesture's, not the picker's).
 *  A project card may sit under a planning card only (H4). A goal under
 *  nothing. */
export function allowedParentKinds(kind: RoadmapKind): RoadmapKind[] {
  switch (kind) {
    case "goal":
      return [];
    case "initiative":
      return ["goal"];
    case "epic":
      return ["initiative", "goal"];
    default:
      return ["epic", "initiative", "goal"];
  }
}

/** GLYPH is the one-character mark a row's kind wears, beside its title. */
export const GLYPH: Record<RoadmapKind, string> = {
  goal: "◎",
  initiative: "◆",
  epic: "▣",
  task: "▪",
  subtask: "↳",
  project: "▫",
};

export const KIND_LABEL: Record<RoadmapKind, string> = {
  goal: "Goal",
  initiative: "Initiative",
  epic: "Epic",
  task: "Task",
  subtask: "Subtask",
  project: "Project card",
};
