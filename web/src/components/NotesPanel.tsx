import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { Card as CardModel, CardEvent, Note } from "../providers/types";
import { eventLabel } from "../eventlog";

// Persisted, clamped sizes for the resizable Notes pane: a width on the desktop
// side pane, a height when it stacks under the board on narrow screens.
const NOTES_WIDTH_KEY = "aeman.notesWidth";
const NOTES_HEIGHT_KEY = "aeman.notesHeight";
const NOTES_GROUP_KEY = "aeman.notesGroup";
const NOTES_SHOWLOG_KEY = "aeman.notesShowLog";
const NOTES_WIDTH_MIN = 220;
const NOTES_WIDTH_MAX = 640;
const NOTES_HEIGHT_MIN = 140;
const NOTES_HEIGHT_MAX = 700;
// Below this viewport width the pane stacks under the board and resizes by height
// (mirrors the 820px responsive breakpoint in styles.css).
const STACK_BREAKPOINT = 820;

const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v));

function readStored(key: string): number | null {
  const raw = localStorage.getItem(key);
  if (raw === null) {
    return null;
  }
  const n = Number(raw);
  return Number.isFinite(n) ? n : null;
}

/** DayNote is a note paired with the card it belongs to, for the day's list. */
export interface DayNote {
  note: Note;
  card: CardModel;
}

/** DayEvent is a recorded activity event paired with its card. */
export interface DayEvent {
  event: CardEvent;
  card: CardModel;
}

type GroupMode = "time" | "card";

interface NotesPanelProps {
  selectedDate: string;
  notes: DayNote[];
  /** The day's recorded activity events, interleaved with the notes. */
  events: DayEvent[];
  /** Card item ids in board (display) order, for the "by card" grouping. */
  cardOrder: string[];
  selectedCard: CardModel | null;
  onSelectCard: (card: CardModel) => void;
  onAddNote: (text: string) => void;
  onEditNote: (note: Note, card: CardModel, text: string) => void;
  onDeleteNote: (note: Note, card: CardModel) => void;
  /** Fold to just the header bar (used on narrow screens). */
  collapsed: boolean;
  onToggleCollapse: () => void;
}

/** localTime formats an ISO timestamp as a local HH:MM string. */
function localTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) {
    return "";
  }
  return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", hour12: false });
}

/** useNotesResize makes the pane draggable: horizontally as a desktop side pane
 *  (drag its left edge), vertically when it stacks under the board on narrow
 *  screens (drag its top edge). The chosen size persists per axis. */
function useNotesResize(collapsed: boolean) {
  const paneRef = useRef<HTMLElement>(null);
  const [stacked, setStacked] = useState(
    () => window.matchMedia(`(max-width: ${STACK_BREAKPOINT}px)`).matches,
  );
  const [width, setWidth] = useState<number | null>(() => readStored(NOTES_WIDTH_KEY));
  const [height, setHeight] = useState<number | null>(() => readStored(NOTES_HEIGHT_KEY));
  const drag = useRef<{ start: number; base: number } | null>(null);

  useEffect(() => {
    const mq = window.matchMedia(`(max-width: ${STACK_BREAKPOINT}px)`);
    const onChange = () => setStacked(mq.matches);
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, []);

  const onPointerMove = useCallback(
    (e: PointerEvent) => {
      const d = drag.current;
      if (!d) {
        return;
      }
      if (stacked) {
        // Pane sits below the board: dragging the top edge up grows it.
        setHeight(clamp(d.base + (d.start - e.clientY), NOTES_HEIGHT_MIN, NOTES_HEIGHT_MAX));
      } else {
        // Pane sits to the right: dragging the left edge left grows it.
        setWidth(clamp(d.base + (d.start - e.clientX), NOTES_WIDTH_MIN, NOTES_WIDTH_MAX));
      }
    },
    [stacked],
  );

  const onPointerUp = useCallback(() => {
    drag.current = null;
    window.removeEventListener("pointermove", onPointerMove);
    window.removeEventListener("pointerup", onPointerUp);
    const rect = paneRef.current?.getBoundingClientRect();
    if (!rect) {
      return;
    }
    if (stacked) {
      localStorage.setItem(NOTES_HEIGHT_KEY, String(Math.round(rect.height)));
    } else {
      localStorage.setItem(NOTES_WIDTH_KEY, String(Math.round(rect.width)));
    }
  }, [stacked, onPointerMove]);

  const onHandleDown = useCallback(
    (e: React.PointerEvent) => {
      e.preventDefault();
      const rect = paneRef.current?.getBoundingClientRect();
      if (!rect) {
        return;
      }
      drag.current = stacked
        ? { start: e.clientY, base: rect.height }
        : { start: e.clientX, base: rect.width };
      window.addEventListener("pointermove", onPointerMove);
      window.addEventListener("pointerup", onPointerUp);
    },
    [stacked, onPointerMove, onPointerUp],
  );

  // Apply only the axis in play; a collapsed pane folds to its header, so no
  // custom size is imposed then.
  const style: React.CSSProperties = collapsed
    ? {}
    : stacked
      ? height !== null
        ? { height, maxHeight: "none", flex: "none" }
        : {}
      : width !== null
        ? { width }
        : {};

  return { paneRef, style, onHandleDown };
}

/** NotesPanel lists the day's notes and offers a composer for the selected card. */
export function NotesPanel({
  selectedDate,
  notes,
  events,
  cardOrder,
  selectedCard,
  onSelectCard,
  onAddNote,
  onEditNote,
  onDeleteNote,
  collapsed,
  onToggleCollapse,
}: NotesPanelProps) {
  const [draft, setDraft] = useState("");
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editDraft, setEditDraft] = useState("");
  // Grouping (time/card) and the system-log toggle persist across sessions.
  const [group, setGroup] = useState<GroupMode>(
    () => (localStorage.getItem(NOTES_GROUP_KEY) === "card" ? "card" : "time"),
  );
  const [showLog, setShowLog] = useState(
    () => localStorage.getItem(NOTES_SHOWLOG_KEY) === "1",
  );
  useEffect(() => {
    localStorage.setItem(NOTES_GROUP_KEY, group);
  }, [group]);
  useEffect(() => {
    localStorage.setItem(NOTES_SHOWLOG_KEY, showLog ? "1" : "0");
  }, [showLog]);
  const { paneRef, style: resizeStyle, onHandleDown } = useNotesResize(collapsed);
  const listRef = useRef<HTMLDivElement>(null);

  const submit = () => {
    const text = draft.trim();
    if (!selectedCard || !text) {
      return;
    }
    onAddNote(text);
    setDraft("");
  };

  const startEdit = (note: Note) => {
    setEditingId(note.id);
    setEditDraft(note.body);
  };

  const commitEdit = (note: Note, card: CardModel) => {
    const text = editDraft.trim();
    setEditingId(null);
    if (text && text !== note.body) {
      onEditNote(note, card, text);
    }
  };

  // The unified timeline: notes and events merged by timestamp.
  type FeedItem =
    | { at: string; note: Note; card: CardModel }
    | { at: string; event: CardEvent; card: CardModel };
  const feed = useMemo<FeedItem[]>(() => {
    const out: FeedItem[] = [
      ...notes.map(({ note, card }) => ({ at: note.createdAt, note, card })),
      ...(showLog
        ? events.map(({ event, card }) => ({ at: event.at, event, card }))
        : []),
    ];
    out.sort((a, b) => a.at.localeCompare(b.at));
    return out;
  }, [notes, events, showLog]);

  // In "by card" mode, group the day's feed under their card, ordered the way
  // the cards appear on the board (entries within a card stay in time order).
  const groups = useMemo(() => {
    const byCard = new Map<string, { card: CardModel; items: FeedItem[] }>();
    for (const item of feed) {
      let g = byCard.get(item.card.itemId);
      if (!g) {
        g = { card: item.card, items: [] };
        byCard.set(item.card.itemId, g);
      }
      g.items.push(item);
    }
    const rank = (id: string) => {
      const i = cardOrder.indexOf(id);
      return i === -1 ? Number.MAX_SAFE_INTEGER : i;
    };
    return [...byCard.values()].sort(
      (a, b) => rank(a.card.itemId) - rank(b.card.itemId),
    );
  }, [feed, cardOrder]);

  // In "by card" grouping, selecting a card scrolls its group into view within
  // the list (only the list scrolls, not the page), so the card you picked on
  // the board lines up with its notes.
  useEffect(() => {
    if (group !== "card" || !selectedCard || collapsed || !listRef.current) {
      return;
    }
    const list = listRef.current;
    const target = list.querySelector<HTMLElement>(
      `[data-card-id="${selectedCard.itemId}"]`,
    );
    if (target) {
      list.scrollTop +=
        target.getBoundingClientRect().top - list.getBoundingClientRect().top - 8;
    }
  }, [selectedCard, group, collapsed, groups]);

  const renderEvent = (event: CardEvent, card: CardModel, showCard: boolean) => (
    <div className="note note-event" key={event.id}>
      <div className="note-meta">
        <span className="note-time">{localTime(event.at)}</span>
        {showCard && (
          <button
            type="button"
            className="note-card"
            onClick={() => onSelectCard(card)}
            title={card.title}
          >
            {card.title}
          </button>
        )}
      </div>
      <div className="note-event-body">
        {event.actor ? `@${event.actor} · ` : ""}
        {eventLabel(event)}
      </div>
    </div>
  );

  const renderItem = (item: FeedItem, showCard: boolean) =>
    "note" in item
      ? renderNote(item.note, item.card, showCard)
      : renderEvent(item.event, item.card, showCard);

  const renderNote = (note: Note, card: CardModel, showCard: boolean) => (
    <div className="note" key={note.id}>
      <div className="note-meta">
        <span className="note-time">{localTime(note.createdAt)}</span>
        {note.author && <span className="note-author">@{note.author}</span>}
        {showCard && (
          <button
            type="button"
            className="note-card"
            onClick={() => onSelectCard(card)}
            title={card.title}
          >
            {card.title}
          </button>
        )}
        {editingId !== note.id && (
          <span className="note-actions">
            <button
              type="button"
              className="note-action"
              title="Edit note"
              onClick={() => startEdit(note)}
            >
              ✎
            </button>
            <button
              type="button"
              className="note-action note-action-delete"
              title="Delete note"
              onClick={() => onDeleteNote(note, card)}
            >
              ×
            </button>
          </span>
        )}
      </div>
      {editingId === note.id ? (
        <div className="note-edit">
          <textarea
            className="notes-textarea"
            rows={2}
            autoFocus
            value={editDraft}
            onChange={(e) => setEditDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                commitEdit(note, card);
              } else if (e.key === "Escape") {
                setEditingId(null);
              }
            }}
          />
          <div className="note-edit-actions">
            <button
              type="button"
              className="btn btn-primary"
              onClick={() => commitEdit(note, card)}
            >
              Save
            </button>
            <button type="button" className="btn" onClick={() => setEditingId(null)}>
              Cancel
            </button>
          </div>
        </div>
      ) : (
        <div className="note-body">{note.body}</div>
      )}
    </div>
  );

  return (
    <aside
      ref={paneRef}
      className={`notes${collapsed ? " notes-collapsed" : ""}`}
      style={resizeStyle}
    >
      <div
        className="notes-resizer"
        onPointerDown={onHandleDown}
        role="separator"
        aria-label="Resize notes"
      />
      <header className="notes-header">
        <span>Notes — {selectedDate}</span>
        <div className="notes-header-right">
          <button
            type="button"
            className={`notes-log-toggle${showLog ? " notes-log-toggle-on" : ""}`}
            aria-pressed={showLog}
            onClick={() => setShowLog((v) => !v)}
            title={showLog ? "Hide the system log" : "Show the system log"}
          >
            ⚙
          </button>
          <div className="notes-group-toggle" role="tablist" aria-label="Group notes">
            <button
              type="button"
              role="tab"
              aria-selected={group === "time"}
              className={`notes-group-btn${group === "time" ? " notes-group-btn-on" : ""}`}
              onClick={() => setGroup("time")}
              title="Group by timeline"
            >
              Time
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={group === "card"}
              className={`notes-group-btn${group === "card" ? " notes-group-btn-on" : ""}`}
              onClick={() => setGroup("card")}
              title="Group by card"
            >
              Card
            </button>
          </div>
          <button
            type="button"
            className="notes-toggle"
            onClick={onToggleCollapse}
            aria-label={collapsed ? "Expand notes" : "Collapse notes"}
            title={collapsed ? "Expand" : "Collapse"}
          >
            {collapsed ? "▲" : "▼"}
          </button>
        </div>
      </header>

      <div className="notes-list" ref={listRef}>
        {feed.length === 0 && (
          <p className="notes-empty">No activity for this day.</p>
        )}
        {group === "time"
          ? feed.map((item) => renderItem(item, true))
          : groups.map((g) => (
              <div
                className={`note-group${g.card.itemId === selectedCard?.itemId ? " note-group-selected" : ""}`}
                data-card-id={g.card.itemId}
                key={g.card.itemId}
              >
                <button
                  type="button"
                  className="note-group-head"
                  onClick={() => onSelectCard(g.card)}
                  title={g.card.title}
                >
                  {g.card.title}
                </button>
                {g.items.map((item) => renderItem(item, false))}
              </div>
            ))}
      </div>

      <div className="notes-composer">
        <div className="notes-on">
          {selectedCard ? (
            <>
              On: <span className="notes-on-title">{selectedCard.title}</span>
            </>
          ) : (
            "Select a card on the left"
          )}
        </div>
        <textarea
          className="notes-textarea"
          rows={3}
          value={draft}
          placeholder="Write a note… (Enter to send, Shift+Enter for a new line)"
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              submit();
            }
          }}
        />
        <button
          type="button"
          className="btn btn-primary notes-submit"
          onClick={submit}
          disabled={!selectedCard || draft.trim() === ""}
        >
          Add note
        </button>
      </div>
    </aside>
  );
}
