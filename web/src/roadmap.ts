// What the Roadmap draws: the hierarchy as a tree of rows and a timeline of
// bars, card by card. It lives in a file of its own for the reason every
// other mirror does — the tree is assembled from `parent`, the status of a
// planning card is the server's derived one, and a bar is a card's own
// dates — so a component re-deciding any of it would drift from what the
// listing says (docs/design/hierarchy.md, "The Roadmap tab").
import { addDays, mondayOf } from "./date";
import { isComplete, isInProgress } from "./stages";
import { isPlanning, isSubtask } from "./subtasks";
import type { Card, CardPatch } from "./providers/types";

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

/** RoadmapSection is where a root of the forest is drawn. The TREE is the
 *  hierarchy: every planning card and everything under one. Below it, after
 *  everything else, stands the open work that is not in the tree yet — the
 *  roadmap listing carries it so a lead can file it (H9): IN WORK is what
 *  the day boards draw (a card with a day, a week or a sprint), BACKLOG is
 *  what is parked on a team's shelf, UNSORTED is the strip — a card with no
 *  plan and no team, nobody's yet. */
export type RoadmapSection = "tree" | "in-work" | "backlog" | "unsorted";

export const SECTION_LABEL: Record<Exclude<RoadmapSection, "tree">, string> = {
  "in-work": "In work",
  backlog: "Backlog",
  unsorted: "Unsorted",
};

/** isPlanned reports a card somebody has said WHEN for — a day, a week or a
 *  sprint — which is what puts it on a day board. Mirrors the server's
 *  "unplanned" in the own gesture (hierarchy.md, "Owner"). */
export function isPlanned(c: Pick<Card, "startDate" | "day" | "week" | "sprintStart">): boolean {
  return !!(c.startDate || c.day || c.week || c.sprintStart);
}

/** sectionOf is the section a ROOT card is drawn in: the tree for a planning
 *  card or a card with a planning ancestor (its lineage names one — a root
 *  with such a lineage is a card whose parent the reader may not see, and
 *  it stays in the tree rather than reading as loose work); else the shelf
 *  it is parked on, the day boards it is planned for, or the strip. A
 *  subtask is never asked: it rides its task into whichever section. */
export function sectionOf(
  c: Pick<Card, "type" | "lineage" | "parked" | "startDate" | "day" | "week" | "sprintStart">,
): RoadmapSection {
  if (isPlanning(c) || (c.lineage ?? []).some((up) => !!up.type)) {
    return "tree";
  }
  if (c.parked) {
    return "backlog";
  }
  return isPlanned(c) ? "in-work" : "unsorted";
}

/** RoadmapSections is the forest split by section: the tree's roots, then
 *  the loose roots — in work, on a shelf (grouped by team, the way the
 *  Triage board draws the shelves; "" is the no-team shelf), unsorted. */
export interface RoadmapSections {
  tree: RoadmapNode[];
  inWork: RoadmapNode[];
  backlog: { team: string; nodes: RoadmapNode[] }[];
  unsorted: RoadmapNode[];
}

export function sectionsOf(roots: readonly RoadmapNode[]): RoadmapSections {
  const out: RoadmapSections = { tree: [], inWork: [], backlog: [], unsorted: [] };
  for (const node of roots) {
    switch (sectionOf(node.card)) {
      case "tree":
        out.tree.push(node);
        break;
      case "in-work":
        out.inWork.push(node);
        break;
      case "backlog": {
        const team = node.card.team ?? "";
        let shelf = out.backlog.find((s) => s.team === team);
        if (!shelf) {
          shelf = { team, nodes: [] };
          out.backlog.push(shelf);
        }
        shelf.nodes.push(node);
        break;
      }
      default:
        out.unsorted.push(node);
    }
  }
  return out;
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
 *  standing (no items, to do, in progress, done — H5), or a task's own:
 *  done, a stage that holds it (review, locked, refused), to do before its
 *  bar has moved, in progress otherwise. */
export type RoadmapStatus = "no-items" | "todo" | "in-progress" | "done" | "review" | "locked" | "refused";

/** statusOf is a row's status: for a planning card the server's derived one
 *  (`hierarchy.status`, H5 — no items until something is filed under it,
 *  in progress once a task under it is somebody's or has moved); for a
 *  task, in this order: done when complete; else the stage it is held in —
 *  review, locked, refused — which says who it waits on; else to do while
 *  its bar has not moved (nothing started); else in progress. The drawer
 *  and the Roadmap column read the same answer. */
export function statusOf(c: Pick<Card, "type" | "hierarchy" | "stage" | "progress">): RoadmapStatus {
  if (isPlanning(c)) {
    return c.hierarchy?.status ?? "no-items";
  }
  if (isComplete(c)) {
    return "done";
  }
  if (c.stage === "review") {
    return "review";
  }
  if (c.stage === "locked") {
    return "locked";
  }
  if (c.stage === "refuse") {
    return "refused";
  }
  if (!isInProgress(c) && (c.progress ?? 0) === 0) {
    return "todo";
  }
  return "in-progress";
}

export const STATUS_LABEL: Record<RoadmapStatus, string> = {
  "no-items": "No items",
  todo: "To do",
  "in-progress": "In progress",
  done: "Done",
  review: "Review",
  locked: "Locked",
  refused: "Refused",
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

/** HierarchyChoice is what the hierarchy's × may mean: delete the card, or
 *  only take it out from under its parent. The Team board's own choice
 *  dialog puts the question. */
export type HierarchyChoice = "delete" | "detach";

/** hierarchyChoices is the list that × offers, most destructive first (the
 *  dialog reads it that way): always delete; detach only where there is a
 *  parent to come out from under — a root offered "detach" would be offered
 *  a no-op dressed as a choice. */
export function hierarchyChoices(c: Pick<Card, "parent">): HierarchyChoice[] {
  return c.parent ? ["delete", "detach"] : ["delete"];
}

/** DELETE_INTENT is what the roadmap `remove` gesture must be asked for. The
 *  server's default intent is the day boards' "unassign", which a planning
 *  card refuses (it has no working area to be left in) — so a × that sent no
 *  intent got a 422 back and looked like a × that did nothing. */
export const DELETE_INTENT = "off-board" as const;

/** RemovalPlan is what one answer of the hierarchy's × does: the optimistic
 *  patches the board applies at once (the children moving up, or the link
 *  going), and the one request that makes it so. */
export interface RemovalPlan {
  patches: { itemId: string; parent: string | undefined }[];
  request: { kind: "remove"; uid: string; intent: typeof DELETE_INTENT } | { kind: "unlink"; uid: string };
  /** The card leaves the board (delete) — the row goes with it. */
  gone: boolean;
}

/** planningRemovalPlan turns an answer into the plan above. Delete moves the
 *  children up (planningRemoval) and sends the remove gesture with the
 *  intent the server needs; detach clears the card's own parent and sends
 *  that one patch. */
export function planningRemovalPlan(
  cards: readonly Pick<Card, "itemId" | "parent">[],
  id: string,
  choice: HierarchyChoice,
): RemovalPlan {
  if (choice === "detach") {
    return {
      patches: [{ itemId: id, parent: undefined }],
      request: { kind: "unlink", uid: id },
      gone: false,
    };
  }
  return {
    patches: planningRemoval(cards, id),
    request: { kind: "remove", uid: id, intent: DELETE_INTENT },
    gone: true,
  };
}

/** childrenNote says, for the dialog, what deleting the card does to what
 *  sits under it — nothing when nothing does. */
export function childrenNote(
  cards: readonly Pick<Card, "itemId" | "parent" | "title">[],
  id: string,
): string {
  const moved = planningRemoval(cards, id);
  if (moved.length === 0) {
    return "";
  }
  const gone = cards.find((c) => c.itemId === id);
  const up = gone?.parent ? cards.find((c) => c.itemId === gone.parent) : undefined;
  const where = up ? `under «${up.title}»` : "to the top";
  return moved.length === 1
    ? `The card under it moves ${where}.`
    : `The ${moved.length} cards under it move ${where}.`;
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

// ---- drag and drop on the tree ----
//
// A row is dragged onto another row (it goes UNDER it) or between two rows
// (it joins that gap's parent at that place). The level rule decides what
// may land where (H2), a card never changes its type by being dragged, and
// the tree shows the subtree where it WOULD land before the pointer is
// released — the preview is the moved rows themselves, as the Triage board
// draws a card where it would land.

/** DropZone is where on the hovered row the dragged row stands: its top
 *  quarter (before it), its bottom quarter (after it), or its middle (under
 *  it). */
export type DropZone = "before" | "into" | "after";

/** zoneOf reads the zone off the dragged row's centre as a fraction of the
 *  hovered row's height (0 at its top, 1 at its bottom). */
export function zoneOf(fraction: number): DropZone {
  if (fraction < 0.28) {
    return "before";
  }
  if (fraction > 0.72) {
    return "after";
  }
  return "into";
}

/** Placement is where a dragged card would come to rest: under which parent
 *  (null for the top of the tree), at which position among that parent's
 *  children in listing order. */
export interface Placement {
  parent: string | null;
  index: number;
}

const parentOf = (c: Pick<Card, "parent">): string | null => c.parent || null;

/** siblingsUnder is a parent's children in listing order — what the tree
 *  draws under it — without the dragged card, which is leaving its place. */
function siblingsUnder<T extends Pick<Card, "itemId" | "parent">>(
  cards: readonly T[],
  parent: string | null,
  except: string,
): T[] {
  return cards.filter((c) => c.itemId !== except && parentOf(c) === parent);
}

/** placementFor is where a drop on a row in a zone lands, read off the rows
 *  as drawn: before a row is its parent's child at the row's place; after a
 *  row is the next place — unless the row is unfolded with rows under it,
 *  where "after" is the first place UNDER it, as the eye reads the list;
 *  into a row is the last place under it. Null when the hovered row is the
 *  dragged card itself. */
export function placementFor(
  cards: readonly Pick<Card, "itemId" | "parent">[],
  rows: readonly Row[],
  draggedId: string,
  overId: string,
  zone: DropZone,
): Placement | null {
  const at = rows.findIndex((r) => r.node.card.itemId === overId);
  if (at < 0 || overId === draggedId) {
    return null;
  }
  const over = rows[at];
  const overParent = parentOf(over.node.card);
  const siblings = siblingsUnder(cards, overParent, draggedId);
  const index = siblings.findIndex((c) => c.itemId === overId);
  if (zone === "into") {
    return { parent: overId, index: siblingsUnder(cards, overId, draggedId).length };
  }
  if (zone === "before") {
    return { parent: overParent, index: Math.max(index, 0) };
  }
  const next = rows[at + 1];
  if (next && next.depth > over.depth) {
    return { parent: overId, index: 0 };
  }
  return { parent: overParent, index: index + 1 };
}

/** lineageOf is a card's ancestors root first, read off the parent links of
 *  the listing — and the card itself last, which is what a child filed under
 *  it carries as its lineage. Walked rather than copied from the card's own
 *  lineage, which an optimistic card may not have yet. Bounded against a
 *  cycle written straight into the repository. */
export function lineageOf(
  cards: readonly Pick<Card, "itemId" | "parent" | "type" | "title">[],
  id: string,
): { uid: string; type?: Card["type"]; title: string }[] {
  const byId = new Map(cards.map((c) => [c.itemId, c]));
  const up: { uid: string; type?: Card["type"]; title: string }[] = [];
  const seen = new Set<string>();
  for (let cur = byId.get(id); cur && !seen.has(cur.itemId) && up.length < 8; cur = cur.parent ? byId.get(cur.parent) : undefined) {
    seen.add(cur.itemId);
    up.unshift({ uid: cur.itemId, type: cur.type, title: cur.title });
  }
  return up;
}

/** isUnder reports a card somewhere below another in the tree, by parent
 *  links — the dragged subtree, which may not become its own parent. */
export function isUnder(
  cards: readonly Pick<Card, "itemId" | "parent">[],
  id: string,
  ancestor: string,
): boolean {
  const byId = new Map(cards.map((c) => [c.itemId, c]));
  const seen = new Set<string>();
  for (let cur = byId.get(id); cur && cur.parent && !seen.has(cur.itemId); cur = byId.get(cur.parent)) {
    seen.add(cur.itemId);
    if (cur.parent === ancestor) {
      return true;
    }
  }
  return false;
}

/** droppable reports whether a card may come to rest under a parent (null
 *  for the top of the tree): the level rule (H2) — a parent of a higher
 *  level, a task's one exception being another task, which makes it a
 *  subtask (one level deep: not under a subtask, not with subtasks of its
 *  own) — never itself or anything under it, a project card under planning
 *  cards only (H4), and the top of the tree for planning cards only: a task
 *  dropped there would leave the Roadmap altogether, which is not a drop
 *  but a detach, and the × asks for that. A card never changes its type by
 *  being dragged. */
export function droppable(
  cards: readonly Pick<Card, "itemId" | "parent" | "type" | "scope" | "lineage">[],
  draggedId: string,
  parent: string | null,
): boolean {
  const dragged = cards.find((c) => c.itemId === draggedId);
  if (!dragged) {
    return false;
  }
  const dk = kindOf(dragged);
  if (parent === null) {
    return isPlanning(dragged);
  }
  if (parent === draggedId || isUnder(cards, parent, draggedId)) {
    return false;
  }
  const target = cards.find((c) => c.itemId === parent);
  if (!target) {
    return false;
  }
  const pk = kindOf(target);
  if (dk === "task" || dk === "subtask") {
    if (pk === "task") {
      // A subtask: the target is a task that is not itself a subtask, and
      // the dragged task has no subtasks of its own (one level deep).
      return !cards.some((c) => parentOf(c) === draggedId && !isPlanning(c));
    }
    return allowedParentKinds("task").includes(pk);
  }
  return allowedParentKinds(dk).includes(pk);
}

/** previewCards is the listing as it would read after the drop: the dragged
 *  card under its new parent, at its place among the siblings, with the
 *  lineage the breadcrumb would show. The subtree under it follows by its
 *  own parent links. The tree built from this IS the preview. */
export function previewCards(cards: readonly Card[], draggedId: string, placement: Placement): Card[] {
  const dragged = cards.find((c) => c.itemId === draggedId);
  if (!dragged) {
    return [...cards];
  }
  const rest = cards.filter((c) => c.itemId !== draggedId);
  const parent = placement.parent ? rest.find((c) => c.itemId === placement.parent) : undefined;
  const moved: Card = {
    ...dragged,
    parent: placement.parent ?? undefined,
    lineage: parent ? lineageOf(rest, parent.itemId) : undefined,
  };
  const siblings = siblingsUnder(rest, placement.parent, draggedId);
  let at: number;
  if (siblings.length === 0) {
    const p = parent ? rest.indexOf(parent) : -1;
    at = p < 0 ? rest.length : p + 1;
  } else if (placement.index >= siblings.length) {
    at = rest.indexOf(siblings[siblings.length - 1]) + 1;
  } else {
    at = rest.indexOf(siblings[placement.index]);
  }
  return [...rest.slice(0, at), moved, ...rest.slice(at)];
}

/** DropCommit is what a drop writes: the new parent when it changed, and
 *  the card's place among its new siblings as the one move the server's
 *  ordering takes — after the sibling before it, or before the first one
 *  when it is first — plus the listing order the board applies at once. */
export interface DropCommit {
  parent: string | null;
  parentChanged: boolean;
  after: string | null;
  before: string | null;
  order: string[];
}

export function dropCommit(cards: readonly Card[], draggedId: string, placement: Placement): DropCommit {
  const dragged = cards.find((c) => c.itemId === draggedId);
  const after = previewCards(cards, draggedId, placement);
  const siblings = after.filter((c) => parentOf(c) === placement.parent);
  const i = siblings.findIndex((c) => c.itemId === draggedId);
  return {
    parent: placement.parent,
    parentChanged: (dragged ? parentOf(dragged) : null) !== placement.parent,
    after: i > 0 ? siblings[i - 1].itemId : null,
    before: i === 0 && siblings.length > 1 ? siblings[1].itemId : null,
    order: after.map((c) => c.itemId),
  };
}

/** DropWriteError is a drop's write that failed, and how far it had got:
 *  `parentWritten` says the server took the new parent before the move
 *  among the siblings failed. */
export class DropWriteError extends Error {
  readonly parentWritten: boolean;
  readonly cause: unknown;
  constructor(cause: unknown, parentWritten: boolean) {
    super(cause instanceof Error ? cause.message : String(cause));
    this.name = "DropWriteError";
    this.cause = cause;
    this.parentWritten = parentWritten;
  }
}

/** writeDrop sends a drop's writes in order — the parent first, then the
 *  place among the new siblings — and, when one fails, says how far it got,
 *  so the board rolls back only what the server did not take. The two are
 *  separate requests (a card PATCH carries no position); a parent taken and
 *  a move refused leaves the card under its new parent, which is where the
 *  board must keep drawing it. */
export async function writeDrop(
  id: string,
  commit: DropCommit,
  provider: {
    patchCard(uid: string, patch: CardPatch): Promise<unknown>;
    moveCard(uid: string, afterId: string | null): Promise<void>;
    moveCardBefore(uid: string, beforeId: string): Promise<void>;
  },
): Promise<void> {
  let parentWritten = false;
  try {
    if (commit.parentChanged) {
      await provider.patchCard(id, { parent: commit.parent ?? "" });
      parentWritten = true;
    }
    if (commit.after) {
      await provider.moveCard(id, commit.after);
    } else if (commit.before) {
      await provider.moveCardBefore(id, commit.before);
    }
  } catch (err) {
    throw new DropWriteError(err, parentWritten);
  }
}

export const KIND_LABEL: Record<RoadmapKind, string> = {
  goal: "Goal",
  initiative: "Initiative",
  epic: "Epic",
  task: "Task",
  subtask: "Subtask",
  project: "Project card",
};
