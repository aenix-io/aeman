// The backlog drawer: the teams' shelves, standing beside the weeks.
//
// It sits on the Triage board rather than being a board of its own, because
// triaging IS the traffic between the two. A card comes off a shelf into a
// week, or off the weeks onto a shelf, and both halves have to be in view at
// once for either move to be a decision rather than a guess.
//
// One shelf per team on screen, stacked in a single column — a shelf is a list
// of titles and reads down, and side by side they were two narrow gutters
// neither of which could show a whole title. Each folds to its header, and
// remembers that in this browser: a lead watching two teams reads one of them
// most days.
//
// It folds and is dragged wider the way the Me board's notes pane is — the
// same pane, in the same corner of the screen, so it is one thing to learn.
//
// The drawer decides nothing. It reports where a card was let go and lets the
// board say what that means — the board is the one that knows where its weeks
// and its people are.
import { useCallback, useEffect, useRef, useState } from "react";
import type { Card as CardModel } from "../providers/types";
import { parked } from "../backlog";
import { teamColor } from "../avatar";
import { ZONES } from "../zones";
import { AddCard } from "./AddCard";

/** How far the pointer must travel before a press becomes a drag. The board's
 *  own number — a press that stays put opens the card here too. */
const DRAG_SLOP = 4;

// The pane's width and which shelves are folded, both this browser's.
const WIDTH_KEY = "aeman.triage.backlogWidth";
const FOLDED_KEY = "aeman.triage.backlogTeams";
const WIDTH_MIN = 200;
const WIDTH_MAX = 720;

const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v));

/** zoneMark is the colour a card wears down its left edge here: its ZONE, the
 *  way every other board draws one.
 *
 *  It used to be the card's STAGE colour, which for a card with no stage is
 *  the default green every progress bar wears while it is being worked on —
 *  a lie twice over on a shelf, where nobody is working on anything and the
 *  one thing worth saying is what KIND of work it is. Everything parked is
 *  planned work (boardservice.SetBacklog), so a shelf reads grey; anything
 *  else there is a card written straight into the repository. */
function zoneMark(card: Pick<CardModel, "zone">): string {
  return card.zone ? ZONES[card.zone].accent : "var(--line)";
}

/** Spot is where a dragged card would land in the drawer: whose shelf, and the
 *  place among the cards already on it. */
export interface Spot {
  team: string;
  at: number;
}

/** dropSpot reads the drawer for a point: the shelf under it and where in that
 *  shelf the card would go.
 *
 *  Off the DOM rather than from anything tracked while the pointer travels —
 *  the shelf carries its team the way the grid's cells carry their week, and
 *  the place is decided by which card's middle the point has passed, which is
 *  what the reader is actually aiming at. */
export function dropSpot(x: number, y: number): Spot | null {
  const shelf = document.elementFromPoint(x, y)?.closest<HTMLElement>("[data-shelf]");
  if (!shelf) {
    return null;
  }
  const rows = shelf.querySelectorAll<HTMLElement>("[data-card]");
  let at = rows.length;
  for (let i = 0; i < rows.length; i++) {
    const r = rows[i].getBoundingClientRect();
    if (y < r.top + r.height / 2) {
      at = i;
      break;
    }
  }
  return { team: shelf.dataset.shelf ?? "", at };
}

/** useDrawerWidth makes the pane draggable by its left edge, the way the notes
 *  pane is. A folded pane keeps no width of its own — it is a rail. */
function useDrawerWidth(collapsed: boolean) {
  const paneRef = useRef<HTMLElement>(null);
  const [width, setWidth] = useState<number | null>(() => {
    const raw = localStorage.getItem(WIDTH_KEY);
    const n = raw === null ? NaN : Number(raw);
    return Number.isFinite(n) ? n : null;
  });
  const drag = useRef<{ start: number; base: number } | null>(null);

  const onPointerMove = useCallback((e: PointerEvent) => {
    const d = drag.current;
    if (d) {
      // The pane is on the right, so dragging its left edge LEFT grows it.
      setWidth(clamp(d.base + (d.start - e.clientX), WIDTH_MIN, WIDTH_MAX));
    }
  }, []);

  const onPointerUp = useCallback(() => {
    drag.current = null;
    window.removeEventListener("pointermove", onPointerMove);
    window.removeEventListener("pointerup", onPointerUp);
    const rect = paneRef.current?.getBoundingClientRect();
    if (rect) {
      localStorage.setItem(WIDTH_KEY, String(Math.round(rect.width)));
    }
  }, [onPointerMove]);

  const onHandleDown = useCallback(
    (e: React.PointerEvent) => {
      e.preventDefault();
      const rect = paneRef.current?.getBoundingClientRect();
      if (!rect) {
        return;
      }
      drag.current = { start: e.clientX, base: rect.width };
      window.addEventListener("pointermove", onPointerMove);
      window.addEventListener("pointerup", onPointerUp);
    },
    [onPointerMove, onPointerUp],
  );

  useEffect(
    () => () => {
      window.removeEventListener("pointermove", onPointerMove);
      window.removeEventListener("pointerup", onPointerUp);
    },
    [onPointerMove, onPointerUp],
  );

  return {
    paneRef,
    style: collapsed || width === null ? {} : { width },
    onHandleDown,
  };
}

/** useFoldedShelves remembers which shelves are folded, in this browser. */
function useFoldedShelves() {
  const [folded, setFolded] = useState<Set<string>>(() => {
    try {
      const v: unknown = JSON.parse(localStorage.getItem(FOLDED_KEY) ?? "[]");
      return new Set(Array.isArray(v) ? v.filter((t) => typeof t === "string") : []);
    } catch {
      // A corrupt entry is not worth a broken drawer.
      return new Set();
    }
  });
  const toggle = useCallback((team: string) => {
    setFolded((cur) => {
      const next = new Set(cur);
      if (!next.delete(team)) {
        next.add(team);
      }
      try {
        localStorage.setItem(FOLDED_KEY, JSON.stringify([...next]));
      } catch {
        // A browser that will not remember it is not a reason to refuse it.
      }
      return next;
    });
  }, []);
  return { folded, toggle };
}

interface BacklogDrawerProps {
  /** Folded to a rail, which still says how much is parked. */
  collapsed: boolean;
  onToggleCollapse: () => void;
  /** The teams whose shelves are shown — the board's own team filter. */
  teams: string[];
  /** The parked cards of those teams, in the order the server sent them. */
  cards: CardModel[];
  /** Where a dragged card would land, from either side of the board; null when
   *  what is in hand could not land here at all. */
  over: Spot | null;
  /** The card in hand, wherever the drag began. It is drawn where it would
   *  land and nowhere else — the same reading the grid gives a card being
   *  carried across it. */
  carried: CardModel | null;
  /** A card was let go at this point, having travelled. */
  onDrop: (card: CardModel, x: number, y: number) => void;
  /** The pointer moved mid-drag, so the board can preview where it lands. */
  onDragOver: (card: CardModel, x: number, y: number) => void;
  /** The gesture ended without a drop — nothing lands, nothing stays lit. */
  onDragEnd: () => void;
  /** Start a card on this team's shelf, born there rather than born in the
   *  strip and moved. It carries no zone: everything on a shelf is planned
   *  work, and the server says so on every door into the backlog. */
  onAddCard: (team: string, title: string) => void;
  /** The board's own × on a parked card. It is the SAME × as everywhere else —
   *  the shared rule says what it means and asks where the answer destroys
   *  work — and on a card that is on a shelf and in no week the only answer it
   *  has is "off the board" (removal.removeChoices).
   *
   *  There is deliberately no button for coming back to the strip. Giving the
   *  card a week is what brings it back, and that is a drag onto the grid. */
  onRemove: (card: CardModel) => void;
  onOpenCard: (card: CardModel) => void;
}

export function BacklogDrawer({
  collapsed,
  onToggleCollapse,
  teams,
  cards,
  over,
  carried,
  onDrop,
  onDragOver,
  onDragEnd,
  onAddCard,
  onRemove,
  onOpenCard,
}: BacklogDrawerProps) {
  const [adding, setAdding] = useState<string | null>(null);
  const press = useRef<{ card: CardModel; x: number; y: number; moved: boolean } | null>(null);
  const { paneRef, style, onHandleDown } = useDrawerWidth(collapsed);
  const { folded, toggle } = useFoldedShelves();

  const beginDrag = useCallback(
    (card: CardModel) => (e: React.PointerEvent) => {
      if ((e.target as HTMLElement).closest("button, input")) {
        return;
      }
      e.preventDefault();
      press.current = { card, x: e.clientX, y: e.clientY, moved: false };
      const onMove = (ev: PointerEvent) => {
        const p = press.current;
        if (!p) {
          return;
        }
        if (
          !p.moved &&
          Math.abs(ev.clientX - p.x) < DRAG_SLOP &&
          Math.abs(ev.clientY - p.y) < DRAG_SLOP
        ) {
          return;
        }
        p.moved = true;
        onDragOver(p.card, ev.clientX, ev.clientY);
      };
      const done = (ev: PointerEvent, dropped: boolean) => {
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", up);
        window.removeEventListener("pointercancel", cancel);
        const p = press.current;
        press.current = null;
        if (!p) {
          return;
        }
        // A press that never travelled is a click, and a click opens the
        // card — the same reading the grid gives it.
        if (!p.moved) {
          onOpenCard(p.card);
        } else if (dropped) {
          onDrop(p.card, ev.clientX, ev.clientY);
        } else {
          onDragEnd();
        }
      };
      const up = (ev: PointerEvent) => done(ev, true);
      const cancel = (ev: PointerEvent) => done(ev, false);
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", up);
      window.addEventListener("pointercancel", cancel);
    },
    [onDrop, onDragOver, onDragEnd, onOpenCard],
  );

  const parkedCount = cards.filter(parked).length;

  return (
    <aside
      ref={paneRef}
      className={`backlog-drawer${collapsed ? " backlog-drawer-folded" : ""}`}
      style={style}
    >
      <div
        className="backlog-resizer"
        onPointerDown={onHandleDown}
        role="separator"
        aria-label="Resize the backlog"
      />
      <header className="backlog-drawer-head">
        <span className="backlog-drawer-title">Backlog</span>
        {parkedCount > 0 && <span className="backlog-drawer-total">{parkedCount}</span>}
        <button
          type="button"
          className="backlog-drawer-fold"
          onClick={onToggleCollapse}
          aria-expanded={!collapsed}
          aria-label={collapsed ? "Expand the backlog" : "Collapse the backlog"}
          title={collapsed ? "Expand" : "Collapse"}
        >
          {collapsed ? "◀" : "▶"}
        </button>
      </header>

      {!collapsed && (
        <div className="backlog-drawer-body">
          {teams.map((team) => {
            // The card in hand has already left where it came from: it is
            // drawn where it would land and nowhere else.
            const held = cards.filter(
              (c) => (c.team ?? "") === team && c.itemId !== carried?.itemId,
            );
            const landing = over?.team === team ? carried : null;
            const shut = folded.has(team);
            return (
              <section
                className={`backlog-shelf${shut ? " backlog-shelf-shut" : ""}`}
                key={team || "\nnone"}
              >
                <header className="backlog-shelf-head">
                  <button
                    type="button"
                    className="backlog-shelf-fold"
                    onClick={() => {
                      // Folding answers the open form too: the reader shut the
                      // shelf, and a form re-appearing on the next unfold is a
                      // question they already walked away from.
                      setAdding((cur) => (cur === team ? null : cur));
                      toggle(team);
                    }}
                    aria-expanded={!shut}
                    title={shut ? "Show this shelf" : "Fold this shelf"}
                  >
                    <Caret open={!shut} />
                  </button>
                  <span
                    className="backlog-shelf-dot"
                    style={{ background: teamColor(team) }}
                    aria-hidden="true"
                  />
                  <span className="backlog-shelf-name" title={team || "no team"}>
                    {team || "No team"}
                  </span>
                  <span className="backlog-shelf-count">{held.length}</span>
                  {/* Nothing to add TO while the shelf is shut: the form
                      belongs among the cards, and a + over a folded header
                      offers to put a card somewhere the reader cannot see. */}
                  {!shut && (
                    <button
                      type="button"
                      className="backlog-shelf-add"
                      title={`Start a card on ${team || "the no-team group"}'s backlog`}
                      onClick={() => setAdding((cur) => (cur === team ? null : team))}
                    >
                      +
                    </button>
                  )}
                </header>

                {!shut && (
                  <div
                    className={`backlog-shelf-body${landing ? " backlog-shelf-over" : ""}`}
                    data-shelf={team}
                  >
                    {/* No zone to pick: everything on a shelf is planned
                        work. The other three zones are statements about
                        TODAY — critical means today, unplanned means it
                        turned up today, "if time left" is a day's spare
                        capacity — and none of them can be true of a card
                        nobody is doing. Parking says so wherever it happens
                        (boardservice.SetBacklog), so the form has one
                        question fewer rather than one answer that is
                        overwritten. */}
                    {adding === team && (
                      <AddCard
                        autoOpen
                        compact
                        placeholder="Add to the backlog"
                        forcedTeam={team || null}
                        allowNoTeam={false}
                        onCreate={(title) => onAddCard(team, title)}
                        onClosed={() => setAdding(null)}
                      />
                    )}
                    {held.map((c, i) => (
                      <div key={c.itemId}>
                        {landing && over?.at === i && <Landing card={landing} />}
                        <div
                          className="backlog-card"
                          data-card={c.itemId}
                          onPointerDown={beginDrag(c)}
                          title={c.title}
                          style={{ borderLeftColor: zoneMark(c) }}
                        >
                          <span className="backlog-card-title">{c.title}</span>
                          <button
                            type="button"
                            className="backlog-card-remove"
                            title="Take this off the board"
                            aria-label="Take this off the board"
                            onClick={() => onRemove(c)}
                          >
                            ×
                          </button>
                        </div>
                      </div>
                    ))}
                    {landing && (over?.at ?? 0) >= held.length && <Landing card={landing} />}
                    {held.length === 0 && !landing && adding !== team && (
                      <p className="backlog-shelf-empty">Drag work here to park it</p>
                    )}
                  </div>
                )}
              </section>
            );
          })}
        </div>
      )}
    </aside>
  );
}

/** Caret is the shelf's fold mark. Drawn rather than typed: the ▸/▾ glyphs
 *  render as a smudge at the size a header row wants, and at any size that
 *  fixes that they are heavier than everything around them. A stroked
 *  chevron takes the colour of the text beside it and stays a chevron. */
function Caret({ open }: { open: boolean }) {
  return (
    <svg
      className={`backlog-caret${open ? " backlog-caret-open" : ""}`}
      viewBox="0 0 16 16"
      width="12"
      height="12"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="m6 3.5 5 4.5-5 4.5" />
    </svg>
  );
}

/** Landing is the card in hand, drawn where it would come to rest. It is not
 *  there yet, so it is drawn as an outline: the reader is looking at a
 *  promise, not at what the shelf holds. */
function Landing({ card }: { card: CardModel }) {
  return (
    <div
      className="backlog-card backlog-card-landing"
      style={{ borderLeftColor: zoneMark(card) }}
    >
      <span className="backlog-card-title">{card.title}</span>
    </div>
  );
}
