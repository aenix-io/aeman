// What the Strategy board draws: the hierarchy as a tree of rows and a timeline of
// bars, card by card. It lives in a file of its own for the reason every
// other mirror does — the tree is assembled from `parent`, the status of a
// planning card is the server's derived one, and a bar is a card's own
// dates — so a component re-deciding any of it would drift from what the
// listing says (docs/design/hierarchy.md, "The Strategy board tab").
import { keepPlaced, type PlaceFields } from "./backlog";
import { addDays, mondayOf } from "./date";
import { isComplete } from "./stages";
import { workStatus } from "./workstatus";
import { lockTitle } from "./dependencies";
import { isPlanning, isSubtask } from "./subtasks";
import type { Card, CardType, HierarchyStatus, MoveResult, Provider } from "./providers/types";

/** StrategyKind is what a row IS: the three planning levels, a task, or a
 *  subtask (a task under a task). */
export type StrategyKind = "goal" | "initiative" | "epic" | "task" | "subtask";

/** LEVELS orders the kinds top-down; the depth selector picks a range of
 *  them. */
export const LEVELS: readonly StrategyKind[] = ["goal", "initiative", "epic", "task", "subtask"];

/** levelOf is a kind's rank in LEVELS: 0 for a goal, 4 for a subtask.
 *  Mirrors board.Level, inverted. */
export function levelOf(kind: StrategyKind): number {
  return LEVELS.indexOf(kind);
}

/** kindOf is a card's kind on the Strategy board: its planning type, else a
 *  subtask by the parent AS A TASK, else a task. */
export function kindOf(c: Pick<Card, "type" | "parent" | "lineage">): StrategyKind {
  if (isPlanning(c)) {
    return c.type as StrategyKind;
  }
  return isSubtask(c) ? "subtask" : "task";
}

/** StrategyNode is one card in the tree, with the rows under it. */
export interface StrategyNode {
  card: Card;
  kind: StrategyKind;
  children: StrategyNode[];
  /** The card's place in the listing — the board's order. */
  at?: number;
}

/** buildTree is the forest the listing makes: a card goes under the card its
 *  `parent` names when that card is in the listing, and is a root otherwise.
 *  The strategy listing is closed under ancestors (a card is on it because a
 *  planning card is above it), so a root with a parent is a card whose
 *  parent the reader may not see — it is drawn at the top rather than
 *  dropped, since a hole is worse than a flat row. Children keep listing
 *  order: the listing is board order, and the Ordering frames keep it so.
 *  Roots come planning cards first, by level, then in listing order. */
export function buildTree(cards: readonly Card[]): StrategyNode[] {
  const byId = new Map<string, StrategyNode>();
  cards.forEach((card, at) => {
    byId.set(card.itemId, { card, kind: kindOf(card), children: [], at });
  });
  const roots: StrategyNode[] = [];
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

/** StrategySection is where a root of the forest is drawn. The TREE is the
 *  hierarchy: every planning card and everything under one. Below it, after
 *  everything else, stands the open work that is not in the tree yet — the
 *  strategy listing carries it so a lead can file it (H9) — in the two places
 *  a task can be (K1): IN WORK is what the day boards draw (a card with a
 *  date or a sprint), BACKLOG is what is parked in a team's backlog. There
 *  is no third: the Triage strip ("unsorted") is gone, and every door that
 *  left a task there parks it instead (K3). */
export type StrategySection = "tree" | "in-work" | "backlog";

export const SECTION_LABEL: Record<Exclude<StrategySection, "tree">, string> = {
  "in-work": "In work",
  backlog: "Backlog",
};

/** sectionOf is the section a ROOT card is drawn in: the tree for a planning
 *  card or a card with a planning ancestor (its lineage names one — a root
 *  with such a lineage is a card whose parent the reader may not see, and
 *  it stays in the tree rather than reading as loose work); else the
 *  backlog it is parked in, or in work. Parked is read as board.InBacklog
 *  reads it — the flag — so a card a direct writer left with the flag AND
 *  dates is drawn once, in the backlog. A subtask is never asked: it rides
 *  its task into whichever section. */
export function sectionOf(c: Pick<Card, "type" | "lineage" | "parked">): StrategySection {
  if (isPlanning(c) || (c.lineage ?? []).some((up) => !!up.type)) {
    return "tree";
  }
  return c.parked ? "backlog" : "in-work";
}

/** StrategySections is the forest split by section: the tree's roots, then
 *  the loose roots — in work, then each team's backlog (grouped by team, the
 *  way the Backlog board draws its columns; "" is the no-team group). */
export interface StrategySections {
  tree: StrategyNode[];
  inWork: StrategyNode[];
  backlog: { team: string; nodes: StrategyNode[] }[];
}

export function sectionsOf(roots: readonly StrategyNode[]): StrategySections {
  const out: StrategySections = { tree: [], inWork: [], backlog: [] };
  for (const node of roots) {
    switch (sectionOf(node.card)) {
      case "tree":
        out.tree.push(node);
        break;
      case "in-work":
        out.inWork.push(node);
        break;
      default: {
        const team = node.card.team ?? "";
        let shelf = out.backlog.find((s) => s.team === team);
        if (!shelf) {
          shelf = { team, nodes: [] };
          out.backlog.push(shelf);
        }
        shelf.nodes.push(node);
      }
    }
  }
  return out;
}

/** DepthRange is the depth selector: the kinds drawn, inclusive, from a top
 *  level to a bottom one ("initiative to subtask"). Default: initiative to
 *  task — the row a lead reads, with the work under it, and no subtasks. */
export interface DepthRange {
  from: StrategyKind;
  to: StrategyKind;
}

export const DEFAULT_DEPTH: DepthRange = { from: "initiative", to: "task" };

/** visibleLevels is the set of kinds a depth range draws. */
export function visibleLevels(range: DepthRange): Set<StrategyKind> {
  const lo = Math.min(levelOf(range.from), levelOf(range.to));
  const hi = Math.max(levelOf(range.from), levelOf(range.to));
  const out = new Set<StrategyKind>();
  for (const k of LEVELS) {
    const l = levelOf(k);
    if (l >= lo && l <= hi) {
      out.add(k);
    }
  }
  return out;
}

/** Row is one line of the Strategy board: a node at a depth, and whether it has
 *  rows under it that the reader can unfold. */
export interface Row {
  node: StrategyNode;
  depth: number;
  /** Children the depth range would draw — what the toggle unfolds. */
  foldable: number;
}

/** rows flattens the tree into the lines drawn, top-down. A level ABOVE the
 *  range is skipped and its children stand in its place (hiding goals shows
 *  their initiatives as roots); a level BELOW the range is cut with its
 *  subtree. A collapsed node draws itself and nothing under it. */
export function rows(
  tree: readonly StrategyNode[],
  range: DepthRange,
  expanded: (id: string) => boolean,
): Row[] {
  const visible = visibleLevels(range);
  const lo = Math.min(levelOf(range.from), levelOf(range.to));
  if (isFlatRange(range)) {
    return flatRows(tree, range, expanded);
  }
  const out: Row[] = [];
  const walk = (nodes: readonly StrategyNode[], depth: number) => {
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

/** isFlatRange is a range that starts at the epic level: it draws the epics
 *  FLAT, in the board-wide epic order (Q1, docs/design/ordering.md) — an
 *  epic of a lower initiative may stand above one of a higher — rather than
 *  grouped under the plan items it hides. Switching to it writes nothing. */
export function isFlatRange(range: DepthRange): boolean {
  return Math.min(levelOf(range.from), levelOf(range.to)) === levelOf("epic");
}

/** flatRows is a flat range's lines: every epic in the listing's order (the
 *  board's), each with what is under it as the range and the folds say;
 *  then what the grouped walk would have drawn at the top that is no epic
 *  and stands under none (a task straight under an initiative). */
function flatRows(tree: readonly StrategyNode[], range: DepthRange, expanded: (id: string) => boolean): Row[] {
  const visible = visibleLevels(range);
  const epics: StrategyNode[] = [];
  const others: StrategyNode[] = [];
  const collect = (nodes: readonly StrategyNode[], under: boolean) => {
    for (const node of nodes) {
      if (node.kind === "epic") {
        epics.push(node);
        continue;
      }
      if (levelOf(node.kind) < levelOf("epic")) {
        collect(node.children, under);
      } else if (!under && visible.has(node.kind)) {
        others.push(node);
      }
    }
  };
  collect(tree, false);
  epics.sort((a, b) => (a.at ?? 0) - (b.at ?? 0));
  const out: Row[] = [];
  const walk = (node: StrategyNode, depth: number) => {
    if (!visible.has(node.kind)) {
      return;
    }
    const foldable = node.children.filter((k) => visible.has(k.kind)).length;
    out.push({ node, depth, foldable });
    if (expanded(node.card.itemId)) {
      for (const k of node.children) {
        walk(k, depth + 1);
      }
    }
  };
  for (const node of [...epics, ...others]) {
    walk(node, 0);
  }
  return out;
}

/** ALL_LEVELS is every level, for a tree that shows everything under a card. */
const ALL_LEVELS: DepthRange = { from: "goal", to: "subtask" };

/** subtreeRows is the Strategy board's tree rooted ONE LEVEL BELOW a card — the
 *  drawer's "Under it": every level under the card (plan items, tasks,
 *  subtasks), at depth 0 for its children,
 *  folded as `expanded` says. Empty for a card with nothing under it, or
 *  one the cards do not hold. */
export function subtreeRows(cards: readonly Card[], rootId: string, expanded: (id: string) => boolean): Row[] {
  const find = (nodes: readonly StrategyNode[]): StrategyNode | null => {
    for (const n of nodes) {
      if (n.card.itemId === rootId) {
        return n;
      }
      const deeper = find(n.children);
      if (deeper) {
        return deeper;
      }
    }
    return null;
  };
  const root = find(buildTree(cards));
  return root ? rows(root.children, ALL_LEVELS, expanded) : [];
}

/** childLevels is what may be made under a card of a kind, the next level
 *  first — what the Strategy board's per-row "+" and the drawer's add row offer:
 *  the levels below a plan item's own, and under an epic a TASK (born with
 *  no team and no day, parked in the no-team backlog until somebody takes
 *  it into work, H7, K3). */
export function childLevels(kind: StrategyKind): (CardType | "task")[] {
  if (kind === "goal") {
    return ["initiative", "epic"];
  }
  if (kind === "initiative") {
    return ["epic"];
  }
  if (kind === "epic") {
    return ["task"];
  }
  return [];
}

/** parentChainOf is a card's ancestors, root first — what a picker shows
 *  beside a plan item so two of one name under different parents are told
 *  apart: the cards held when the whole chain is, else the server's lineage
 *  (only kind and title then). */
export function parentChainOf(c: Pick<Card, "itemId" | "parent" | "lineage">, cards: readonly Card[]): Pick<Card, "itemId" | "title" | "type">[] {
  const byId = new Map(cards.map((x) => [x.itemId, x]));
  const out: Card[] = [];
  const seen = new Set([c.itemId]);
  let broken = false;
  for (let id = c.parent; id; ) {
    const p = byId.get(id);
    if (!p) {
      broken = true;
      break;
    }
    if (seen.has(p.itemId) || out.length > 6) {
      break;
    }
    seen.add(p.itemId);
    out.unshift(p);
    id = p.parent;
  }
  if (broken && c.lineage?.length) {
    return c.lineage.map((l) => ({ itemId: l.uid, title: l.title, type: l.type }));
  }
  return out;
}

/** ParentOption is one entry of the Under picker: a card and its chain of
 *  ancestors, root first. */
export interface ParentOption {
  card: Card;
  chain: Pick<Card, "itemId" | "title" | "type">[];
}

const PICK_ORDER: StrategyKind[] = ["goal", "initiative", "epic"];

/** parentOptions is the Under picker's list for a card of `kind` (`self`):
 *  the levels it may sit under (allowedParentKinds), grouped highest first,
 *  never the card itself or a card under it — whatever the search says —
 *  each with its chain, and found by its own title or an ancestor's. The
 *  cards are read once (one map, one chain per card), and the list and its
 *  labels share it. */
export function parentOptions(
  kind: StrategyKind,
  cards: readonly Card[],
  self: string,
  query: string,
): { kind: StrategyKind; options: ParentOption[] }[] {
  const allowed = allowedParentKinds(kind);
  const q = query.trim().toLowerCase();
  const offered: ParentOption[] = [];
  for (const c of cards) {
    const k = kindOf(c);
    if (!allowed.includes(k) || c.itemId === self) {
      continue;
    }
    const chain = parentChainOf(c, cards);
    if (chain.some((up) => up.itemId === self) || (c.lineage ?? []).some((up) => up.uid === self)) {
      continue; // a card under this one cannot also be above it
    }
    if (q && ![c, ...chain].some((x) => x.title.toLowerCase().includes(q))) {
      continue;
    }
    offered.push({ card: c, chain });
  }
  return PICK_ORDER.filter((k) => allowed.includes(k))
    .map((k) => ({ kind: k, options: offered.filter((o) => kindOf(o.card) === k) }))
    .filter((g) => g.options.length > 0);
}

/** cardsAfterDelete is the hierarchy once a card is deleted: what was under
 *  it moves up to its own parent — its lineage the deleted card's own — as
 *  the server moves it (reparentChildren); the card itself is gone. */
export function cardsAfterDelete(cards: readonly Card[], id: string): Card[] {
  const gone = cards.find((c) => c.itemId === id);
  const up = gone?.parent || undefined;
  return cards
    .filter((c) => c.itemId !== id)
    .map((c) => (c.parent === id ? { ...c, parent: up, lineage: up ? gone?.lineage : undefined } : c));
}

/** pickerMatches is a picker's search over a plan item: its own title or
 *  any of its ancestors', case aside; an empty query matches all. */
export function pickerMatches(c: Card, cards: readonly Card[], query: string): boolean {
  const q = query.trim().toLowerCase();
  if (!q) {
    return true;
  }
  return [c, ...parentChainOf(c, cards)].some((x) => x.title.toLowerCase().includes(q));
}

/** StrategyStatus is the work-status half of a row's badge: a planning
 *  card's derived standing (no items, backlog, in progress, done — H5), or a
 *  task's own: done, a stage that holds it (review, locked, refused), in
 *  progress while it is in work, backlog otherwise. */
export type StrategyStatus = "no-items" | "backlog" | "in-progress" | "done" | "review" | "locked" | "refused";

/** statusOf is a row's work status: for a planning card the server's
 *  derived one (`hierarchy.status`, H5); for a task, its work status
 *  (workStatus — what the Status filter reads, by placement), except that a
 *  card not done that is held in a stage — review, locked, refused — names
 *  the stage, which says who it waits on. The badge may say "Review" on a
 *  parked card the filter counts as backlog; everywhere else the two agree.
 *  The drawer and the Strategy board column read the same answer. */
export function statusOf(
  c: Pick<Card, "type" | "hierarchy" | "stage" | "progress"> &
    Partial<Pick<Card, "parked" | "startDate" | "day" | "sprintStart" | "locked">>,
): StrategyStatus {
  if (isPlanning(c)) {
    const rolled = c.hierarchy?.status ?? "no-items";
    // A plan item waiting for work not done yet (status.locked, derived)
    // reads LOCKED until its own work is done.
    return c.locked && rolled !== "done" ? "locked" : rolled;
  }
  const placed = workStatus(c);
  if (placed === "done") {
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
  // Waiting for a dependency not done yet (status.locked, derived —
  // docs/design/dependencies.md): LOCKED, whatever its placement. The Status
  // filter keeps reading placement.
  if (c.locked) {
    return "locked";
  }
  return placed;
}

export const STATUS_LABEL: Record<StrategyStatus, string> = {
  "no-items": "No items",
  backlog: "Backlog",
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

/** DELETE_INTENT is what the strategy `remove` gesture must be asked for. The
 *  server's default intent is the day boards' "unassign", which a planning
 *  card refuses (it has no working area to be left in) — so a × that sent no
 *  intent got a 422 back and looked like a × that did nothing. */
export const DELETE_INTENT = "off-board" as const;

/** RemovalPlan is what one answer of the hierarchy's × does: the optimistic
 *  patches the board applies at once (the children moving up, or the link
 *  going), and the one request that makes it so. */
export interface RemovalPlan {
  patches: { itemId: string; parent: string | undefined; parked?: true }[];
  request: { kind: "remove"; uid: string; intent: typeof DELETE_INTENT } | { kind: "unlink"; uid: string };
  /** The card leaves the board (delete) — the row goes with it. */
  gone: boolean;
}

/** planningRemovalPlan turns an answer into the plan above. Delete moves the
 *  children up (planningRemoval) and sends the remove gesture with the
 *  intent the server needs; detach clears the card's own parent and sends
 *  that one patch. */
export function planningRemovalPlan(
  cards: readonly (Pick<Card, "itemId" | "parent"> & Partial<PlaceFields>)[],
  id: string,
  choice: HierarchyChoice,
): RemovalPlan {
  if (choice === "detach") {
    // A task with no place of its own detached from its plan is parked, as
    // the server parks it (K1, keepPlaced).
    const card = cards.find((c) => c.itemId === id);
    return {
      patches: [{ itemId: id, parent: undefined, ...(card ? keepPlaced(card, false) : {}) }],
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
 *  A goal under nothing. */
export function allowedParentKinds(kind: StrategyKind): StrategyKind[] {
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
// may land where (H2), and a card never changes its type by being dragged.
//
// While a row is dragged the list does not move: the target is read off the
// rows as they stood when the drag began (dropTargetAt), and where the card
// would land is a mark drawn OVER the rows (dropMark) — a line in the gap,
// or the row it would go under — that takes no room. A preview that moved
// the rows to where the card would land put other rows under the pointer,
// which read as another place, which moved the rows again: the list jumped
// under a slow aim (H13).

/** DropZone is where on the hovered row the pointer stands: its top part
 *  (before it), its bottom part (after it), or its middle (under it). */
export type DropZone = "before" | "into" | "after";

/** AIM_SLACK is how far, as a fraction of a row's height, the pointer must
 *  go past a boundary — between two zones of a row, or between two rows —
 *  before the target changes: a slow aim that wobbles a pixel or two across
 *  a line keeps what it had. */
export const AIM_SLACK = 0.12;

const BEFORE_LINE = 0.28;
const AFTER_LINE = 0.72;

/** zoneOf reads the zone off the pointer as a fraction of the hovered row's
 *  height (0 at its top, 1 at its bottom). Given the zone it stands in on
 *  that row, the boundaries around that zone are pushed out by AIM_SLACK. */
export function zoneOf(fraction: number, held?: DropZone): DropZone {
  const before = BEFORE_LINE + (held === "before" ? AIM_SLACK : held === "into" ? -AIM_SLACK : 0);
  const after = AFTER_LINE + (held === "after" ? -AIM_SLACK : held === "into" ? AIM_SLACK : 0);
  if (fraction < before) {
    return "before";
  }
  if (fraction > after) {
    return "after";
  }
  return "into";
}

/** RowBox is a tree row's place in the list when the drag began: its top
 *  and height in one fixed frame (the pointer is given in the same one). */
export interface RowBox {
  id: string;
  top: number;
  height: number;
}

/** DropTarget is what the pointer stands on: a row and the zone of it. */
export interface DropTarget {
  overId: string;
  zone: DropZone;
}

/** dropTargetAt is the row and zone under the pointer, read off the boxes of
 *  the rows as they stood when the drag began — so the answer depends on
 *  where the pointer is and on nothing the drag itself draws. The target it
 *  stood on (prev) is held until the pointer is AIM_SLACK of a row past its
 *  edge or its zone's boundary. The rows the drag carries (held: the dragged
 *  row and its subtree) are a target for nothing. Just off either end of the
 *  tree the nearest row reads (before the first, after the last), up to a
 *  row's height away; further off, and with no rows, nothing does. */
export function dropTargetAt(
  boxes: readonly RowBox[],
  y: number,
  prev: DropTarget | null,
  held: ReadonlySet<string>,
): DropTarget | null {
  if (boxes.length === 0) {
    return null;
  }
  let box: RowBox | undefined;
  const kept = prev ? boxes.find((b) => b.id === prev.overId) : undefined;
  if (kept) {
    const slack = AIM_SLACK * kept.height;
    if (y >= kept.top - slack && y < kept.top + kept.height + slack) {
      box = kept;
    }
  }
  box ??= boxes.find((b) => y >= b.top && y < b.top + b.height);
  if (!box) {
    const first = boxes[0];
    const last = boxes[boxes.length - 1];
    if (y < first.top) {
      box = first.top - y <= first.height ? first : undefined;
    } else if (y >= last.top + last.height) {
      box = y - (last.top + last.height) <= last.height ? last : undefined;
    } else {
      // A gap between rows (a form open under one): the nearest row.
      let best = Infinity;
      for (const b of boxes) {
        const d = Math.min(Math.abs(y - b.top), Math.abs(y - (b.top + b.height)));
        if (d < best) {
          best = d;
          box = b;
        }
      }
    }
  }
  if (!box || held.has(box.id)) {
    return null;
  }
  const same = prev && prev.overId === box.id ? prev : null;
  const zone = zoneOf((y - box.top) / Math.max(box.height, 1), same?.zone);
  return same && same.zone === zone ? same : { overId: box.id, zone };
}

/** boxesInContent puts rows measured in the viewport (getBoundingClientRect)
 *  into the list's content frame: their top plus how far the board and the
 *  page are scrolled (scroll, the sum of the two) at the moment of
 *  measuring. A row keeps its box however either scrolls after, so a
 *  snapshot re-taken mid-drag is in the same frame as the first. */
export function boxesInContent(measured: readonly RowBox[], scroll: number): RowBox[] {
  return measured.map((b) => ({ id: b.id, top: b.top + scroll, height: b.height }));
}

/** pointerContentY is the pointer in the same content frame: its viewport y
 *  plus the scroll now. Null without a pointer position (a keyboard drag
 *  has no clientY) and over the board's sticky header (above headBottom, in
 *  the viewport), where a row scrolled under the header is hidden and no
 *  target. */
export function pointerContentY(clientY: number | null, scroll: number, headBottom: number): number | null {
  if (clientY === null || !Number.isFinite(clientY) || clientY < headBottom) {
    return null;
  }
  return clientY + scroll;
}

/** revealStep is one pass of bringing a row followed to the Strategy board (a
 *  breadcrumb) into view: pending is the row still owed a scroll. It is
 *  scrolled to once it is drawn and no drag runs, and then nothing is owed
 *  — later re-renders do not pull the list back to it. */
export function revealStep(pending: string | null, dragging: boolean, drawn: boolean): { scroll: boolean; pending: string | null } {
  if (!pending || dragging || !drawn) {
    return { scroll: false, pending };
  }
  return { scroll: true, pending: null };
}

/** DropMark is where the landing place is drawn: the row it is drawn on,
 *  on which edge of it ("inside" marks the row itself — the card goes under
 *  it), and the depth the line is indented to. */
export interface DropMark {
  row: string;
  edge: "top" | "bottom" | "inside";
  depth: number;
}

/** dropMark says where to draw a target's landing place. Before a row is a
 *  line on its top edge at its depth; after a row a line on its bottom edge
 *  — one step deeper when that place is the first under it (an unfolded
 *  row, see placementFor); into a row is the row itself. Null when the row
 *  is not drawn. */
export function dropMark(drawn: readonly Row[], target: DropTarget, placement: Placement): DropMark | null {
  const over = drawn.find((r) => r.node.card.itemId === target.overId);
  if (!over) {
    return null;
  }
  if (target.zone === "into") {
    return { row: target.overId, edge: "inside", depth: over.depth };
  }
  if (target.zone === "before") {
    return { row: target.overId, edge: "top", depth: over.depth };
  }
  return { row: target.overId, edge: "bottom", depth: placement.parent === target.overId ? over.depth + 1 : over.depth };
}

/** Placement is where a dragged card would come to rest: under which parent
 *  (null for the top of the tree), at which position among that parent's
 *  children in listing order. */
export interface Placement {
  parent: string | null;
  index: number;
  /** A drop of an epic among the epics of a FLAT range (isFlatRange): the
   *  epic keeps its parent and moves in the epic order, right after or
   *  right before the epic it was dropped by (Q7: a flat drop never
   *  re-parents). */
  flat?: { after: string } | { before: string };
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
  flat = false,
): Placement | null {
  const at = rows.findIndex((r) => r.node.card.itemId === overId);
  if (at < 0 || overId === draggedId) {
    return null;
  }
  const over = rows[at];
  const dragged = rows.find((r) => r.node.card.itemId === draggedId);
  if (flat && zone !== "into" && over.node.kind === "epic" && dragged?.node.kind === "epic") {
    const own = cards.find((c) => c.itemId === draggedId);
    return { parent: own ? parentOf(own) : null, index: -1, flat: zone === "before" ? { before: overId } : { after: overId } };
  }
  if (flat && dragged?.node.kind === "epic") {
    // In the flat range an epic moves among the epics only: by any other
    // row it would take that row's parent, and a flat drop never re-parents.
    return null;
  }
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
 *  own) — never itself or anything under it, and the top of the tree for planning cards only: a task
 *  dropped there would leave the Strategy board altogether, which is not a drop
 *  but a detach, and the × asks for that. A card never changes its type by
 *  being dragged. */
export function droppable(
  cards: readonly Pick<Card, "itemId" | "parent" | "type" | "lineage">[],
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
 *  own parent links. What a drop commits is read off it (dropCommit). */
export function previewCards(cards: readonly Card[], draggedId: string, placement: Placement): Card[] {
  const dragged = cards.find((c) => c.itemId === draggedId);
  if (!dragged) {
    return [...cards];
  }
  if (placement.flat) {
    // The epic order alone: the epic beside the one it was dropped by, its
    // parent unchanged.
    const rest = cards.filter((c) => c.itemId !== draggedId);
    const anchor = "after" in placement.flat ? placement.flat.after : placement.flat.before;
    const i = rest.findIndex((c) => c.itemId === anchor);
    const at = i < 0 ? rest.length : "after" in placement.flat ? i + 1 : i;
    return [...rest.slice(0, at), dragged, ...rest.slice(at)];
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

/** DropShown is what the board changes the moment a drop is let go: the
 *  dragged card's parent and lineage when its parent changed (null when it
 *  did not), and the listing's order. */
export interface DropShown {
  patch: { parent: string | undefined; lineage: Card["lineage"] } | null;
  order: string[];
}

/** dropShown is the optimistic answer to a drop, applied at once whatever
 *  filter is on. The listing is what the server sent for the chosen
 *  filters, the drop is anchored on the cards shown, and the watch never
 *  echoes a tab's own writes back to it — so an order left out here is not
 *  shown until a reload (a reordered goal stood still with a team filter
 *  on). */
export function dropShown(cards: readonly Card[], draggedId: string, placement: Placement): DropShown {
  const after = previewCards(cards, draggedId, placement);
  const dragged = cards.find((c) => c.itemId === draggedId);
  const moved = after.find((c) => c.itemId === draggedId);
  const changed = !!dragged && !!moved && parentOf(dragged) !== placement.parent;
  return {
    patch: changed && moved ? { parent: moved.parent, lineage: moved.lineage } : null,
    order: after.map((c) => c.itemId),
  };
}

export function dropCommit(cards: readonly Card[], draggedId: string, placement: Placement): DropCommit {
  const dragged = cards.find((c) => c.itemId === draggedId);
  const after = previewCards(cards, draggedId, placement);
  if (placement.flat) {
    return {
      parent: dragged ? parentOf(dragged) : null,
      parentChanged: false,
      after: "after" in placement.flat ? placement.flat.after : null,
      before: "before" in placement.flat ? placement.flat.before : null,
      order: after.map((c) => c.itemId),
    };
  }
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

/** DropRequest is the ONE request a drop is (docs/design/ordering.md, Q6,
 *  Q7): a task moved among its EPIC's tasks is a reorder within the epic —
 *  the epic's parked tasks of its team permute over the queue slots they
 *  hold, so other epics' tasks never move; anything else is a move, its new
 *  parent named when it changed. A task dropped into ANOTHER epic names the
 *  parent and no place: the server puts it after that epic's last parked
 *  task in its team's queue. An epic's move gathers its tasks there too. */
export type DropRequest =
  | { kind: "reorder-in-epic"; after?: string; before?: string }
  | { kind: "move"; parent?: string; after?: string; before?: string };

export function dropRequest(
  cards: readonly Pick<Card, "itemId" | "parent" | "type" | "lineage">[],
  draggedId: string,
  commit: DropCommit,
): DropRequest {
  const dragged = cards.find((c) => c.itemId === draggedId);
  const parent = commit.parent ? cards.find((c) => c.itemId === commit.parent) : undefined;
  const place = commit.after ? { after: commit.after } : commit.before ? { before: commit.before } : {};
  const isTask = !!dragged && !isPlanning(dragged);
  if (isTask && parent?.type === "epic") {
    if (!commit.parentChanged) {
      return { kind: "reorder-in-epic", ...place };
    }
    return { kind: "move", parent: commit.parent ?? "" };
  }
  return commit.parentChanged ? { kind: "move", parent: commit.parent ?? "", ...place } : { kind: "move", ...place };
}

/** writeDrop sends a drop's one request and answers what it moved — the
 *  action. One request is one action on the server: there
 *  is no half-written drop to roll back. */
export async function writeDrop(
  id: string,
  request: DropRequest,
  provider: Pick<Provider, "moveTo" | "reorderInEpic">,
): Promise<MoveResult> {
  if (request.kind === "reorder-in-epic") {
    return provider.reorderInEpic(id, { after: request.after, before: request.before });
  }
  return provider.moveTo(id, { parent: request.parent, after: request.after, before: request.before });
}

export const KIND_LABEL: Record<StrategyKind, string> = {
  goal: "Goal",
  initiative: "Initiative",
  epic: "Epic",
  task: "Task",
  subtask: "Subtask",
};

/** StrategyLine is one line of the Strategy board's table: a row, in the section it
 *  is drawn in, or a section's header. */
export type StrategyLine =
  | { kind: "row"; row: Row; section: StrategySection }
  | { kind: "header"; key: string; label: string };

/** strategyLines is the table's lines, ONE function of what decides them —
 *  the listing, the depth range and the folds — so the board's memo of it is
 *  keyed on exactly these. The listing is what the server sent. The tree's
 *  rows come first, then each section
 *  below it: a header line and its rows. The sections draw tasks and their
 *  subtasks (when the depth range reaches them); the range's upper levels
 *  are the tree's business. */
export function strategyLines(
  cards: readonly Card[],
  opts: { depth: DepthRange; expanded: (id: string) => boolean },
): StrategyLine[] {
  const sections = sectionsOf(buildTree(cards));
  const sub = visibleLevels(opts.depth).has("subtask");
  const flat: DepthRange = { from: "task", to: sub ? "subtask" : "task" };
  const out: StrategyLine[] = rows(sections.tree, opts.depth, opts.expanded).map((row) => ({
    kind: "row",
    row,
    section: "tree" as const,
  }));
  const add = (section: Exclude<StrategySection, "tree">, label: string, nodes: readonly StrategyNode[], key: string) => {
    if (nodes.length === 0) {
      return;
    }
    out.push({ kind: "header", key, label });
    for (const row of rows(nodes, flat, opts.expanded)) {
      out.push({ kind: "row", row, section });
    }
  };
  add("in-work", SECTION_LABEL["in-work"], sections.inWork, "in-work");
  for (const shelf of sections.backlog) {
    add("backlog", `${SECTION_LABEL.backlog} · ${shelf.team || "no team"}`, shelf.nodes, `backlog-${shelf.team}`);
  }
  return out;
}

// ---- the badge and its menu: UNASSIGNED (K16) ----
//
// The badge of a row is its work status. Its menu offers UNASSIGNED, which
// takes the work in hand under the row back to its team's backlog: on a task
// the task itself, on a plan item every task under it at any depth (its leaf
// tasks: the tasks whose parent is a plan item in its subtree). Mirrors
// board.UnassignTargets. (Priorities were removed: the backlog's order is the
// place in the Strategy board tree, K14.)

/** leafTasks is the tasks under a plan item through every level — a subtask
 *  (its parent is a task) and a review card not among them — in board order. */
function leafTasks(cards: readonly Card[], id: string): Card[] {
  const subtree = new Set([id]);
  for (let grew = true; grew; ) {
    grew = false;
    for (const c of cards) {
      if (isPlanning(c) && c.parent && subtree.has(c.parent) && !subtree.has(c.itemId)) {
        subtree.add(c.itemId);
        grew = true;
      }
    }
  }
  return cards.filter((c) => !isPlanning(c) && !c.reviewOf && !!c.parent && subtree.has(c.parent));
}

/** ridesATask reports a subtask, read off the listing the Strategy board holds: its
 *  parent is a task here. */
function ridesATask(cards: readonly Card[], t: Card): boolean {
  const up = t.parent ? cards.find((c) => c.itemId === t.parent) : undefined;
  return !!up && !isPlanning(up);
}

/** unassignTargets is what UNASSIGNED takes out of work: open tasks in hand —
 *  a date or a sprint, not parked — that answer for their own place (no
 *  review card or subtask). Finished work never. */
export function unassignTargets(cards: readonly Card[], c: Card): Card[] {
  const inHand = (t: Card) =>
    !isPlanning(t) &&
    !t.reviewOf &&
    !ridesATask(cards, t) &&
    !t.parked &&
    !!(t.startDate || t.day || t.sprintStart) &&
    !isComplete(t);
  if (!isPlanning(c)) {
    return inHand(c) ? [c] : [];
  }
  return leafTasks(cards, c.itemId).filter(inHand);
}

/** StatusMenu is what a row's badge menu offers: whether UNASSIGNED has
 *  anything to take. A plan item
 *  says so by its SERVER roll-up — IN PROGRESS is work in hand under it —
 *  because a filtered Strategy holds fewer of its tasks than the board does;
 *  a task by its own place. How many it would take is the server's answer
 *  too (a dry run), asked as the confirm opens. */
export interface StatusMenu {
  unassign: boolean;
}

export function statusMenu(cards: readonly Card[], c: Card): StatusMenu {
  const unassign = isPlanning(c) ? c.hierarchy?.status === "in-progress" : unassignTargets(cards, c).length > 0;
  return { unassign };
}

/** Badge is one row's badge: its work status; `lock` is what a card LOCKED
 *  by its dependencies waits for, the badge's tooltip. */
export interface Badge {
  status: StrategyStatus;
  lock?: string;
}

export function badgeOf(c: Card): Badge {
  const status = statusOf(c);
  const lock = status === "locked" ? lockTitle(c, new Map((c.lineage ?? []).map((l) => [l.uid, l.title]))) : null;
  return {
    status,
    ...(lock ? { lock } : {}),
  };
}

/** latestWins numbers the requests made per card, so an answer is applied
 *  only when it answers the newest one: an older response arriving late
 *  must not overwrite a newer choice. */
export function latestWins(): { next: (id: string) => number; isLatest: (id: string, n: number) => boolean } {
  const latest = new Map<string, number>();
  let counter = 0;
  return {
    next: (id) => {
      counter++;
      latest.set(id, counter);
      return counter;
    },
    isLatest: (id, n) => latest.get(id) === n,
  };
}

/** refusalRollback is what a refused press puts back: the state
 *  before it — but only when it is still the latest press on the card. An
 *  older press failing after a newer one was shown must not undo the newer
 *  choice; the newer press settles it with its own answer. */
export function refusalRollback<T>(seq: { isLatest: (id: string, n: number) => boolean }, id: string, n: number, before: T[]): T[] {
  return seq.isLatest(id, n) ? before : [];
}

/** unassignPreview is the tasks UNASSIGNED will name to the server — the
 *  ones it would take, which the confirm counts — or null when there are
 *  none: the board moved since the menu opened, and "Take 0 tasks out of
 *  work?" is no question to ask. */
export function unassignPreview(targets: readonly Card[]): string[] | null {
  return targets.length > 0 ? targets.map((t) => t.itemId) : null;
}

/** NOTHING_IN_WORK is what UNASSIGNED says of a row with nothing in work
 *  under it — the disabled menu item's title, and the notice a press gets
 *  when the server found nothing after the menu opened. */
export const NOTHING_IN_WORK = "Nothing in work here";

/** unassignNotice is what a press of UNASSIGNED says when there is no
 *  confirm to open (unassignPreview found nothing), else null. */
export function unassignNotice(uids: readonly string[] | null): string | null {
  return uids ? null : NOTHING_IN_WORK;
}

/** unassignQuestion is what the confirm asks before UNASSIGNED. */
export function unassignQuestion(n: number): string {
  return n === 1
    ? "Take 1 task out of work? It leaves Team and Me and goes back to its team's backlog."
    : `Take ${n} tasks out of work? They leave Team and Me and go back to their team's backlog.`;
}

/** unassignedLocally is what UNASSIGNED leaves on a task until the server's
 *  answer: back in its backlog, nobody's, undated, out of the sprint. */
export const UNASSIGNED_LOCALLY: Partial<Card> = {
  parked: true,
  assignees: [],
  startDate: undefined,
  day: undefined,
  sprintStart: undefined,
};


/** rollUp is the standing a plan item's leaf tasks give it — mirrors
 *  board.hierarchyStatus (H5, amended with K15/K16): no items with none;
 *  done when every one is complete; otherwise read off the OPEN ones — in
 *  progress when any is in work (a date or a sprint, not parked), ready (TO
 *  backlog when there are open ones and none is.
 *  Progress, a person or a stage do not count by themselves. */
export function rollUp(leaves: readonly Card[]): HierarchyStatus {
  let done = 0;
  let inWork = 0;
  for (const t of leaves) {
    if (isComplete(t)) {
      done++;
    } else if (!t.parked && !!(t.startDate || t.day || t.sprintStart)) {
      inWork++;
    }
  }
  const total = leaves.length;
  if (total === 0) {
    return { status: "no-items", done: 0, total: 0 };
  }
  if (done === total) {
    return { status: "done", done, total };
  }
  if (inWork > 0) {
    return { status: "in-progress", done, total };
  }
  return { status: "backlog", done, total };
}

/** rolledUpAfter is the new standing of every plan item above the tasks a
 *  gesture changed (`changed`), read off the cards as they stand after it —
 *  what the acting tab shows before the server's frames for those plan
 *  items arrive. */
export function rolledUpAfter(cards: readonly Card[], changed: readonly string[]): Map<string, HierarchyStatus> {
  const byId = new Map(cards.map((c) => [c.itemId, c]));
  const out = new Map<string, HierarchyStatus>();
  for (const id of changed) {
    const seen = new Set<string>();
    for (let up = byId.get(byId.get(id)?.parent ?? ""); up && !seen.has(up.itemId); up = byId.get(up.parent ?? "")) {
      seen.add(up.itemId);
      if (isPlanning(up) && !out.has(up.itemId)) {
        out.set(up.itemId, rollUp(leafTasks(cards, up.itemId)));
      }
    }
  }
  return out;
}

/** workPatchOf is what a status gesture changed on a task, read off the task
 *  as it stands (the server's answer, WorkChange.items): where it is — in
 *  its backlog or in work, whose, which days, which sprint — every field
 *  present so one the server cleared is cleared locally too. */
export function workPatchOf(c: Card): Partial<Card> {
  return {
    parked: c.parked,
    assignees: c.assignees ?? [],
    startDate: c.startDate,
    day: c.day,
    sprintStart: c.sprintStart,
  };
}


/** foldableIds is every row of the board that has rows under it at this
 *  depth — what "Collapse all" folds. "Expand all" folds none. Neither
 *  changes the order: folding is the reader's own view. */
export function foldableIds(cards: readonly Card[], depth: DepthRange): Set<string> {
  const out = new Set<string>();
  for (const line of strategyLines(cards, { depth, expanded: () => true })) {
    if (line.kind === "row" && line.row.foldable > 0) {
      out.add(line.row.node.card.itemId);
    }
  }
  return out;
}
