import { useMemo, useState } from "react";
import type { Card as CardModel, CardEvent, Note } from "../providers/types";
import { eventLabel } from "../eventlog";

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
  const [group, setGroup] = useState<GroupMode>("time");
  // The system log (recorded activity events) is opt-in: notes only by default.
  const [showLog, setShowLog] = useState(false);

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
    <aside className={`notes${collapsed ? " notes-collapsed" : ""}`}>
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

      <div className="notes-list">
        {feed.length === 0 && (
          <p className="notes-empty">No activity for this day.</p>
        )}
        {group === "time"
          ? feed.map((item) => renderItem(item, true))
          : groups.map((g) => (
              <div className="note-group" key={g.card.itemId}>
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
