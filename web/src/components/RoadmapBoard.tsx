import { useEffect, useMemo, useRef, useState } from "react";
import { registerPendingCard } from "../api/pending";
import { todayIso } from "../date";
import type { Board, Card as CardModel, CardType, Provider } from "../providers/types";
import {
  DEFAULT_DEPTH,
  GLYPH,
  KIND_LABEL,
  LEVELS,
  STATUS_LABEL,
  barOf,
  buildTree,
  daysBetween,
  fractionOf,
  place,
  planningRemoval,
  rangeFor,
  rows as rowsOf,
  statusOf,
  ticks as ticksOf,
  type DepthRange,
  type RoadmapKind,
  type Row,
  type Zoom,
} from "../roadmap";
import { barColor } from "../stages";
import { isPlanning } from "../subtasks";
import { AddCard } from "./AddCard";

// The Roadmap: the hierarchy as a table of rows on the left and a timeline
// of bars on the right, one row per card from a goal down to a subtask
// (docs/design/hierarchy.md, "The Roadmap tab"). What a row IS, which rows a
// depth range draws, what a row's status is and where its bar lands are
// roadmap.ts's answers; this component draws them and makes the two
// gestures the board has — a planning card's create, and its x.

interface RoadmapBoardProps {
  board: Board;
  provider: Provider;
  /** The board's teams, for the filter chips. */
  roster: string[];
  patchCard: (itemId: string, patch: Partial<CardModel>) => void;
  addCard: (card: CardModel) => void;
  replaceCard: (itemId: string, card: CardModel) => void;
  removeCard: (itemId: string) => void;
  reload: () => void;
  onError: (message: string) => void;
  onOpen: (card: CardModel) => void;
  /** A card to REVEAL: a breadcrumb was followed here. Its ancestors unfold
   *  and its row is marked; onFocused says it has been. */
  focusId?: string | null;
  onFocused?: () => void;
}

const LS_DEPTH = "aeman.roadmap.depth";
const LS_ZOOM = "aeman.roadmap.zoom";
const LS_FOLDED = "aeman.roadmap.folded";

/** ROW_PX is the height of one row, on both sides, so the table and the
 *  timeline stay aligned without ever measuring each other. */
const ROW_PX = 28;
const HEAD_PX = 26;
const TABLE_PX = 520;

/** pxPerDay is how wide a day is drawn at a zoom: three months read a week
 *  at a glance, a year reads a month, and Fit stretches its span to about
 *  the width of a screen without going below a few pixels a day. */
function pxPerDay(zoom: Zoom, days: number): number {
  if (zoom === "3m") {
    return 12;
  }
  if (zoom === "1y") {
    return 4;
  }
  return Math.max(3, Math.min(16, Math.floor(1000 / Math.max(days, 1))));
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/** shortDate is a date as a row shows it: "10 Feb", the year only when it
 *  is not this one. */
function shortDate(iso: string | undefined, today: string): string {
  if (!iso) {
    return "";
  }
  const [y, m, d] = iso.split("-").map(Number);
  const label = `${d} ${MONTHS[m - 1]}`;
  return iso.slice(0, 4) === today.slice(0, 4) ? label : `${label} ${y}`;
}

function readDepth(): DepthRange {
  try {
    const raw = localStorage.getItem(LS_DEPTH);
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<DepthRange>;
      if (LEVELS.includes(parsed.from as RoadmapKind) && LEVELS.includes(parsed.to as RoadmapKind)) {
        return { from: parsed.from as RoadmapKind, to: parsed.to as RoadmapKind };
      }
    }
  } catch {
    // no storage, or a value from another build: the default
  }
  return DEFAULT_DEPTH;
}

function readZoom(): Zoom {
  try {
    const raw = localStorage.getItem(LS_ZOOM);
    if (raw === "3m" || raw === "1y" || raw === "fit") {
      return raw;
    }
  } catch {
    // the default
  }
  return "3m";
}

function readFolded(): Set<string> {
  try {
    const raw = localStorage.getItem(LS_FOLDED);
    const parsed = raw ? (JSON.parse(raw) as unknown) : [];
    return new Set(Array.isArray(parsed) ? parsed.filter((x): x is string => typeof x === "string") : []);
  } catch {
    return new Set();
  }
}

const errText = (err: unknown) => (err instanceof Error ? err.message : String(err));

/** childLevels is what the + on a planning row may make under it: the
 *  levels below its own, down to an epic — a task is a day board's card and
 *  is filed here from its own detail pane, not born here (H7). */
function childLevels(kind: RoadmapKind): CardType[] {
  if (kind === "goal") {
    return ["initiative", "epic"];
  }
  if (kind === "initiative") {
    return ["epic"];
  }
  return [];
}

export function RoadmapBoard({
  board,
  provider,
  roster,
  patchCard,
  addCard,
  replaceCard,
  removeCard,
  reload,
  onError,
  onOpen,
  focusId,
  onFocused,
}: RoadmapBoardProps) {
  const today = todayIso();
  const [depth, setDepthState] = useState<DepthRange>(readDepth);
  const [zoom, setZoomState] = useState<Zoom>(readZoom);
  const [folded, setFoldedState] = useState<Set<string>>(readFolded);
  const [teamFilter, setTeamFilter] = useState<string[] | null>(null);
  const [whoFilter, setWhoFilter] = useState<string | null>(null);
  // The planning row the + was pressed on, whose create form is open.
  const [addingUnder, setAddingUnder] = useState<string | null>(null);
  const [focusRow, setFocusRow] = useState<string | null>(null);
  const rowRefs = useRef(new Map<string, HTMLDivElement>());

  const setDepth = (next: DepthRange) => {
    setDepthState(next);
    try {
      localStorage.setItem(LS_DEPTH, JSON.stringify(next));
    } catch {
      // a preference, not state
    }
  };
  const setZoom = (next: Zoom) => {
    setZoomState(next);
    try {
      localStorage.setItem(LS_ZOOM, next);
    } catch {
      // a preference, not state
    }
  };
  const setFolded = (next: Set<string>) => {
    setFoldedState(next);
    try {
      localStorage.setItem(LS_FOLDED, JSON.stringify([...next]));
    } catch {
      // a preference, not state
    }
  };
  const toggle = (id: string) => {
    const next = new Set(folded);
    if (next.has(id)) {
      next.delete(id);
    } else {
      next.add(id);
    }
    setFolded(next);
  };

  // The teams and people on the Roadmap, for the chips: the roster's teams
  // plus any a card names, and every assignee a card carries.
  const teams = useMemo(() => {
    const out = new Set(roster);
    for (const c of board.cards) {
      if (c.team) {
        out.add(c.team);
      }
    }
    return [...out];
  }, [roster, board.cards]);
  const people = useMemo(() => {
    const out = new Set<string>();
    for (const c of board.cards) {
      for (const a of c.assignees) {
        out.add(a);
      }
    }
    return [...out].sort();
  }, [board.cards]);

  // A filter keeps the cards that match and every card ABOVE one (the
  // lineage names them), so a matching task still stands under its epic
  // and the epic under its goal — a tree with holes is no tree.
  const shown = useMemo(() => {
    if (!teamFilter && !whoFilter) {
      return board.cards;
    }
    const keep = new Set<string>();
    for (const c of board.cards) {
      const teamOk = !teamFilter || teamFilter.includes(c.team ?? "");
      const whoOk = !whoFilter || c.assignees.includes(whoFilter);
      if (teamOk && whoOk) {
        keep.add(c.itemId);
        for (const up of c.lineage ?? []) {
          keep.add(up.uid);
        }
      }
    }
    return board.cards.filter((c) => keep.has(c.itemId));
  }, [board.cards, teamFilter, whoFilter]);

  const tree = useMemo(() => buildTree(shown), [shown]);
  const rows = useMemo(
    () => rowsOf(tree, depth, (id) => !folded.has(id)),
    [tree, depth, folded],
  );
  const bars = useMemo(() => rows.map((r) => barOf(r.node.card)), [rows]);
  const range = useMemo(() => rangeFor(zoom, today, bars), [zoom, today, bars]);
  const days = daysBetween(range.from, range.to) + 1;
  const trackW = days * pxPerDay(zoom, days);
  const ticks = useMemo(() => ticksOf(range), [range]);
  const todayAt = today >= range.from && today <= range.to ? fractionOf(today, range) : null;

  // A breadcrumb followed here: unfold the way down to the card, mark its
  // row and bring it into view. The lineage says which rows to unfold.
  useEffect(() => {
    if (!focusId) {
      return;
    }
    const card = board.cards.find((c) => c.itemId === focusId);
    if (card) {
      const next = new Set(folded);
      for (const up of card.lineage ?? []) {
        next.delete(up.uid);
      }
      if (next.size !== folded.size) {
        setFolded(next);
      }
    }
    setFocusRow(focusId);
    onFocused?.();
    // Unfolding is a preference write, not a dependency of the reveal.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focusId, board.cards]);
  useEffect(() => {
    if (focusRow) {
      rowRefs.current.get(focusRow)?.scrollIntoView({ block: "center" });
    }
  }, [focusRow, rows]);

  // ---- the create: a planning card, at the top or under a planning row ----
  const createPlanning = (title: string, type: CardType, parent?: string) => {
    const trimmed = title.trim();
    if (!trimmed) {
      return;
    }
    const tempId = `tmp-${new Date().toISOString()}`;
    const up = parent ? board.cards.find((c) => c.itemId === parent) : undefined;
    addCard({
      itemId: tempId,
      title: trimmed,
      assignees: [],
      type,
      parent,
      // Its lineage is its parent's plus the parent, so the tree draws the
      // new row where it was made before the server answers.
      lineage: up ? [...(up.lineage ?? []), { uid: up.itemId, type: up.type, title: up.title }] : undefined,
      description: "",
      progress: 0,
    });
    const created = provider.createCard({ title: trimmed, type, parent: parent ?? null });
    registerPendingCard(
      tempId,
      created.then((c) => c.itemId),
    );
    created
      .then((c) => replaceCard(tempId, c))
      .catch((err: unknown) => {
        removeCard(tempId);
        onError(errText(err));
      });
  };

  // ---- the x: a planning card goes, the rows under it move up a level ----
  const deletePlanning = (card: CardModel) => {
    const moved = planningRemoval(board.cards, card.itemId);
    const up = card.parent ? board.cards.find((c) => c.itemId === card.parent) : undefined;
    const where = up ? `under «${up.title}»` : "to the top";
    const under =
      moved.length === 0
        ? ""
        : ` The ${moved.length === 1 ? "card" : `${moved.length} cards`} under it ${moved.length === 1 ? "moves" : "move"} ${where}.`;
    if (!window.confirm(`Delete «${card.title}»?${under}`)) {
      return;
    }
    for (const m of moved) {
      patchCard(m.itemId, { parent: m.parent });
    }
    removeCard(card.itemId);
    void provider.removeCard(card.itemId).catch((err: unknown) => {
      onError(errText(err));
      reload();
    });
  };

  const levelOptions = () =>
    LEVELS.map((k) => (
      <option key={k} value={k}>
        {KIND_LABEL[k]}
      </option>
    ));

  const planningOptions = (levels: CardType[]) =>
    levels.map((k) => ({ key: k, label: KIND_LABEL[k] }));

  const row = (r: Row) => {
    const { node, depth: d, foldable } = r;
    const card = node.card;
    const planning = isPlanning(card);
    const status = statusOf(card);
    const open = !folded.has(card.itemId);
    const kids = childLevels(node.kind);
    return (
      <div
        key={card.itemId}
        ref={(el) => {
          if (el) {
            rowRefs.current.set(card.itemId, el);
          } else {
            rowRefs.current.delete(card.itemId);
          }
        }}
        className={`roadmap-row${focusRow === card.itemId ? " roadmap-row-focus" : ""}`}
        style={{ height: ROW_PX }}
        onClick={() => onOpen(card)}
      >
        <span className="roadmap-cell roadmap-cell-title" style={{ paddingLeft: 6 + d * 16 }}>
          {foldable > 0 ? (
            <button
              type="button"
              className="roadmap-toggle"
              aria-label={open ? "Fold" : `Unfold ${foldable}`}
              aria-expanded={open}
              onClick={(e) => {
                e.stopPropagation();
                toggle(card.itemId);
              }}
            >
              {open ? "▾" : "▸"}
            </button>
          ) : (
            <span className="roadmap-toggle roadmap-toggle-none" aria-hidden="true" />
          )}
          <span className={`roadmap-glyph roadmap-glyph-${node.kind}`} title={KIND_LABEL[node.kind]} aria-hidden="true">
            {GLYPH[node.kind]}
          </span>
          <span className="roadmap-title" title={card.title}>
            {card.title}
          </span>
          {planning && card.hierarchy && card.hierarchy.total > 0 && (
            <span className="roadmap-count" title={`${card.hierarchy.done} of ${card.hierarchy.total} tasks done`}>
              {card.hierarchy.done}/{card.hierarchy.total}
            </span>
          )}
          {planning && kids.length > 0 && (
            <button
              type="button"
              className="roadmap-row-add"
              title={`Add under «${card.title}»`}
              aria-label={`Add under ${card.title}`}
              onClick={(e) => {
                e.stopPropagation();
                setAddingUnder(addingUnder === card.itemId ? null : card.itemId);
              }}
            >
              +
            </button>
          )}
          {planning && (
            <button
              type="button"
              className="roadmap-row-x"
              title={`Delete «${card.title}»`}
              aria-label={`Delete ${card.title}`}
              onClick={(e) => {
                e.stopPropagation();
                deletePlanning(card);
              }}
            >
              ×
            </button>
          )}
        </span>
        <span className="roadmap-cell roadmap-cell-date">{shortDate(card.startDate, today)}</span>
        <span className="roadmap-cell roadmap-cell-date">{shortDate(card.day, today)}</span>
        <span className="roadmap-cell">
          <span className={`roadmap-status roadmap-status-${status}`}>{STATUS_LABEL[status]}</span>
        </span>
      </div>
    );
  };

  const lane = (r: Row, i: number) => {
    const card = r.node.card;
    const bar = bars[i];
    const placed = bar ? place(bar, range) : null;
    const done = statusOf(card) === "done";
    const planning = isPlanning(card);
    const fill = planning ? undefined : done ? "var(--stage-done)" : barColor(card.stage);
    return (
      <div key={card.itemId} className="roadmap-lane" style={{ height: ROW_PX }} onClick={() => onOpen(card)}>
        {placed && bar && (
          <div
            className={[
              "roadmap-bar",
              `roadmap-bar-${r.node.kind}`,
              done ? "roadmap-bar-done" : "",
              bar.point ? "roadmap-bar-point" : "",
              placed.clippedLeft ? "roadmap-bar-clip-l" : "",
              placed.clippedRight ? "roadmap-bar-clip-r" : "",
            ]
              .filter(Boolean)
              .join(" ")}
            style={{
              left: `${placed.left * 100}%`,
              width: bar.point ? undefined : `${placed.width * 100}%`,
              background: fill,
            }}
            title={`${card.title}: ${bar.start}${bar.point ? "" : ` → ${bar.end}`}`}
          />
        )}
      </div>
    );
  };

  // The create form under a planning row sits right after that row.
  const addingRow = rows.find((r) => r.node.card.itemId === addingUnder);

  return (
    <div className="roadmap">
      <div className="board-toolbar roadmap-toolbar">
        <AddCard
          placeholder="Add a goal, an initiative or an epic…"
          forcedTeam={null}
          picker={{
            title: "Level",
            options: planningOptions(["initiative", "epic", "goal"]),
            initial: "initiative",
          }}
          onCreate={(title, _team, picked) => createPlanning(title, (picked as CardType) || "initiative")}
        />
        <label className="roadmap-depth">
          <span className="roadmap-depth-label">Hierarchy</span>
          <select
            value={depth.from}
            onChange={(e) => setDepth({ ...depth, from: e.target.value as RoadmapKind })}
            aria-label="From level"
          >
            {levelOptions()}
          </select>
          <span className="roadmap-depth-to">to</span>
          <select
            value={depth.to}
            onChange={(e) => setDepth({ ...depth, to: e.target.value as RoadmapKind })}
            aria-label="To level"
          >
            {levelOptions()}
          </select>
        </label>
        <div className="segmented roadmap-zoom" role="group" aria-label="Zoom">
          {(["3m", "1y", "fit"] as Zoom[]).map((z) => (
            <button
              key={z}
              type="button"
              className={`segment${zoom === z ? " segment-active" : ""}`}
              onClick={() => setZoom(z)}
            >
              {z === "3m" ? "3M" : z === "1y" ? "1Y" : "Fit"}
            </button>
          ))}
        </div>
        <span className="roadmap-range" title="The window the timeline shows">
          {shortDate(range.from, today)} – {shortDate(range.to, today)}
        </span>
        <div className="team-chips roadmap-chips" aria-label="Team filter">
          {teams.map((t) => {
            const on = !!teamFilter && teamFilter.includes(t);
            return (
              <button
                key={t || "(no team)"}
                type="button"
                className={`team-chip team-filter-chip${on ? " team-chip-on" : ""}`}
                aria-pressed={on}
                onClick={() =>
                  setTeamFilter(
                    on
                      ? teamFilter!.length === 1
                        ? null
                        : teamFilter!.filter((x) => x !== t)
                      : [...(teamFilter ?? []), t],
                  )
                }
              >
                <span className="team-chip-name">{t || "No team"}</span>
              </button>
            );
          })}
        </div>
        {people.length > 0 && (
          <select
            className="roadmap-who"
            value={whoFilter ?? ""}
            onChange={(e) => setWhoFilter(e.target.value || null)}
            aria-label="Assignee filter"
          >
            <option value="">Anyone</option>
            {people.map((p) => (
              <option key={p} value={p}>
                {p}
              </option>
            ))}
          </select>
        )}
      </div>

      {rows.length === 0 ? (
        <p className="placeholder roadmap-empty">
          {board.cards.length === 0
            ? "Nothing on the roadmap yet: add a goal, an initiative or an epic, and file work under it from a card's detail pane."
            : "Nothing to draw at this depth and filter."}
        </p>
      ) : (
        <div className="roadmap-body">
          <div className="roadmap-table" style={{ width: TABLE_PX }}>
            <div className="roadmap-head" style={{ height: HEAD_PX }}>
              <span className="roadmap-cell roadmap-cell-title">Card</span>
              <span className="roadmap-cell roadmap-cell-date">Start</span>
              <span className="roadmap-cell roadmap-cell-date">End</span>
              <span className="roadmap-cell">Status</span>
            </div>
            {rows.map((r) => (
              <div key={r.node.card.itemId} className="roadmap-rowgroup">
                {row(r)}
                {addingRow && addingRow.node.card.itemId === r.node.card.itemId && (
                  <div className="roadmap-row roadmap-row-add" style={{ paddingLeft: 30 + (r.depth + 1) * 16 }}>
                    <AddCard
                      autoOpen
                      compact
                      placeholder={`Under «${r.node.card.title}»…`}
                      forcedTeam={null}
                      picker={{
                        title: "Level",
                        options: planningOptions(childLevels(r.node.kind)),
                        initial: childLevels(r.node.kind)[0],
                      }}
                      onCreate={(title, _team, picked) => {
                        createPlanning(title, (picked as CardType) || childLevels(r.node.kind)[0], r.node.card.itemId);
                      }}
                      onClosed={() => setAddingUnder(null)}
                    />
                  </div>
                )}
              </div>
            ))}
          </div>
          <div className="roadmap-timeline">
            <div className="roadmap-track" style={{ width: trackW }}>
              <div className="roadmap-ticks" style={{ height: HEAD_PX }}>
                {ticks.map((t) => (
                  <span key={t.date} className="roadmap-tick" style={{ left: `${fractionOf(t.date, range) * 100}%` }}>
                    {t.label}
                  </span>
                ))}
              </div>
              {todayAt !== null && (
                <div className="roadmap-today" style={{ left: `${todayAt * 100}%` }} title={`Today, ${today}`} />
              )}
              {rows.map((r, i) => (
                <div key={r.node.card.itemId} className="roadmap-lanegroup">
                  {lane(r, i)}
                  {addingRow && addingRow.node.card.itemId === r.node.card.itemId && (
                    <div className="roadmap-lane" style={{ height: ROW_PX }} />
                  )}
                </div>
              ))}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
