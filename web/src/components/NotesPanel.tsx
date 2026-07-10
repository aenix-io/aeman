import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { Card as CardModel, CardEvent, Note } from "../providers/types";
import { eventLabel } from "../eventlog";

// Persisted, clamped sizes for the resizable Notes pane: a width on the desktop
// side pane, a height when it stacks under the board on narrow screens.
const NOTES_WIDTH_KEY = "aeman.notesWidth";
const NOTES_HEIGHT_KEY = "aeman.notesHeight";
const NOTES_GROUP_KEY = "aeman.notesGroup";
const NOTES_SHOWLOG_KEY = "aeman.notesShowLog";
const NOTES_PANE_KEY = "aeman.notesPane";
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

/** PaneMode picks what the panel shows: the selected card (its description,
 *  editable in place, with the card's own feed below) or the whole day's log. */
type PaneMode = "card" | "log";

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
  /** Saves the selected card's description (the Card pane's in-place edit). */
  onSetDescription: (card: CardModel, text: string) => void;
  /** Fold to just the header bar (used on narrow screens). */
  collapsed: boolean;
  onToggleCollapse: () => void;
}

// Shared stroke-icon props so the header's toggles (gear, alarm, pencil) read
// as one consistent icon set instead of mixed text glyphs and emoji.
const iconProps = {
  width: 12,
  height: 12,
  viewBox: "0 0 24 24",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 2,
  strokeLinecap: "round",
  strokeLinejoin: "round",
  "aria-hidden": true,
} as const;

const GearIcon = () => (
  <svg {...iconProps}>
    <circle cx="12" cy="12" r="3" />
    <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z" />
  </svg>
);

const AlarmIcon = () => (
  <svg {...iconProps}>
    <circle cx="12" cy="13" r="8" />
    <path d="M12 9v4l2 2" />
    <path d="M5 3 2 6" />
    <path d="m22 6-3-3" />
  </svg>
);

/** A card with a folded corner — the "grouped by card" face of the timeline
 *  toggle. */
const CardIcon = () => (
  <svg {...iconProps}>
    <path d="M16 3H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2V8Z" />
    <path d="M15 3v4a2 2 0 0 0 2 2h4" />
  </svg>
);

const PencilIcon = () => (
  <svg {...iconProps}>
    <path d="M17 3a2.828 2.828 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5L17 3z" />
  </svg>
);

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

  return { paneRef, style, onHandleDown, stacked };
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
  onSetDescription,
  collapsed,
  onToggleCollapse,
}: NotesPanelProps) {
  const [draft, setDraft] = useState("");
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editDraft, setEditDraft] = useState("");
  // The pane switch (description/notes), grouping (time/card) and the
  // system-log toggle persist across sessions.
  const [pane, setPane] = useState<PaneMode>(() =>
    localStorage.getItem(NOTES_PANE_KEY) === "card" ? "card" : "log",
  );
  const [group, setGroup] = useState<GroupMode>(
    () => (localStorage.getItem(NOTES_GROUP_KEY) === "card" ? "card" : "time"),
  );
  const [showLog, setShowLog] = useState(
    () => localStorage.getItem(NOTES_SHOWLOG_KEY) === "1",
  );
  useEffect(() => {
    localStorage.setItem(NOTES_PANE_KEY, pane);
  }, [pane]);
  useEffect(() => {
    localStorage.setItem(NOTES_GROUP_KEY, group);
  }, [group]);
  useEffect(() => {
    localStorage.setItem(NOTES_SHOWLOG_KEY, showLog ? "1" : "0");
  }, [showLog]);
  const {
    paneRef,
    style: resizeStyle,
    onHandleDown,
    stacked,
  } = useNotesResize(collapsed);
  const listRef = useRef<HTMLDivElement>(null);

  // In-place description editing on the Card pane. The draft resets whenever
  // the selection changes — a half-typed edit must not land on another card.
  const [editingDesc, setEditingDesc] = useState(false);
  const [descDraft, setDescDraft] = useState("");
  const selectedId = selectedCard?.itemId;
  useEffect(() => {
    setEditingDesc(false);
  }, [selectedId]);
  const startDescEdit = () => {
    if (!selectedCard) {
      return;
    }
    setDescDraft(selectedCard.description ?? "");
    setEditingDesc(true);
  };
  const saveDescEdit = () => {
    if (selectedCard) {
      onSetDescription(selectedCard, descDraft);
    }
    setEditingDesc(false);
  };

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

  // The Card pane's feed: the selected card's slice of the day (the ⚙ toggle
  // filters its system events through `feed` the same way).
  const cardFeed = useMemo<FeedItem[]>(
    () =>
      selectedCard
        ? feed.filter((item) => item.card.itemId === selectedCard.itemId)
        : [],
    [feed, selectedCard],
  );

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
  const scrolledTo = useRef<string | null>(null);
  useEffect(() => {
    if (
      pane !== "log" ||
      group !== "card" ||
      !selectedCard ||
      collapsed ||
      !listRef.current
    ) {
      return;
    }
    // Scroll only when the SELECTION changes — the feed itself changes on
    // every added note, and chasing it would yank the list around while the
    // user is typing.
    if (scrolledTo.current === selectedCard.itemId) {
      return;
    }
    const list = listRef.current;
    const target = list.querySelector<HTMLElement>(
      `[data-card-id="${selectedCard.itemId}"]`,
    );
    if (target) {
      scrolledTo.current = selectedCard.itemId;
      list.scrollTop +=
        target.getBoundingClientRect().top - list.getBoundingClientRect().top - 8;
    }
  }, [selectedCard, pane, group, collapsed, groups]);

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
            maxLength={4096}
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
        <div className="notes-head-left">
          <div className="notes-icon-seg" role="tablist" aria-label="Panel content">
            <button
              type="button"
              role="tab"
              aria-selected={pane === "card"}
              className={`notes-log-toggle${pane === "card" ? " notes-log-toggle-on" : ""}`}
              onClick={() => setPane("card")}
              title="The selected card: description and its activity"
            >
              Card
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={pane === "log"}
              className={`notes-log-toggle${pane === "log" ? " notes-log-toggle-on" : ""}`}
              onClick={() => setPane("log")}
              title="The whole day's notes and activity"
            >
              Log
            </button>
          </div>
          {(collapsed || pane === "log") && (
            <span className="notes-date">
              {collapsed ? `Notes — ${selectedDate}` : selectedDate}
            </span>
          )}
        </div>
        <div className="notes-header-right">
          <button
            type="button"
            className={`notes-log-toggle${showLog ? " notes-log-toggle-on" : ""}`}
            aria-pressed={showLog}
            onClick={() => setShowLog((v) => !v)}
            title={showLog ? "Hide the system log" : "Show the system log"}
          >
            <GearIcon />
          </button>
          {pane === "log" ? (
            <div className="notes-icon-seg" role="tablist" aria-label="Group notes">
              <button
                type="button"
                role="tab"
                aria-selected={group === "time"}
                className={`notes-log-toggle${group === "time" ? " notes-log-toggle-on" : ""}`}
                onClick={() => setGroup("time")}
                title="The day in time order"
              >
                <AlarmIcon />
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={group === "card"}
                className={`notes-log-toggle${group === "card" ? " notes-log-toggle-on" : ""}`}
                onClick={() => setGroup("card")}
                title="Group by card"
              >
                <CardIcon />
              </button>
            </div>
          ) : editingDesc ? (
            <div className="notes-group-toggle">
              <button
                type="button"
                className="notes-group-btn notes-group-btn-on"
                onClick={saveDescEdit}
                title="Save the description"
              >
                Save
              </button>
              <button
                type="button"
                className="notes-group-btn"
                onClick={() => setEditingDesc(false)}
                title="Discard the edit"
              >
                Cancel
              </button>
            </div>
          ) : (
            <button
              type="button"
              className="notes-log-toggle"
              onClick={startDescEdit}
              disabled={!selectedCard}
              title="Edit the description"
            >
              <PencilIcon />
            </button>
          )}
          <button
            type="button"
            className="notes-toggle"
            onClick={onToggleCollapse}
            aria-label={collapsed ? "Expand notes" : "Collapse notes"}
            title={collapsed ? "Expand" : "Collapse"}
          >
            {/* The arrow points where the pane will go: down/up when stacked
                under the board, right/left as a desktop side pane. */}
            {collapsed ? (stacked ? "▲" : "◀") : stacked ? "▼" : "▶"}
          </button>
        </div>
      </header>

      {pane === "card" && (
        <div className={`notes-desc${editingDesc ? " notes-desc-editing" : ""}`}>
          {selectedCard ? (
            <>
              <div className="notes-desc-on">{selectedCard.title}</div>
              {editingDesc ? (
                <textarea
                  className="notes-desc-edit"
                  autoFocus
                  maxLength={16384}
                  value={descDraft}
                  onChange={(e) => setDescDraft(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Escape") {
                      setEditingDesc(false);
                    }
                  }}
                />
              ) : selectedCard.description ? (
                <div className="notes-desc-body">{selectedCard.description}</div>
              ) : (
                <p className="notes-empty">No description on this card.</p>
              )}
            </>
          ) : (
            <p className="notes-empty">Select a card to see its description.</p>
          )}
        </div>
      )}

      {pane === "card" && selectedCard && (
        <div className="notes-list notes-card-feed">
          {cardFeed.length === 0 && (
            <p className="notes-empty">No activity on this card today.</p>
          )}
          {cardFeed.map((item) => renderItem(item, false))}
        </div>
      )}

      {pane === "log" && (
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
      )}

      {(pane === "log" || selectedCard) && (
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
          maxLength={4096}
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
      )}
    </aside>
  );
}
