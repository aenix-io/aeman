import { useMemo, useState } from "react";
import type { Card as CardModel, Note } from "../providers/types";

/** DayNote is a note paired with the card it belongs to, for the day's list. */
export interface DayNote {
  note: Note;
  card: CardModel;
}

type GroupMode = "time" | "card";

interface NotesPanelProps {
  selectedDate: string;
  notes: DayNote[];
  /** Card item ids in board (display) order, for the "by card" grouping. */
  cardOrder: string[];
  selectedCard: CardModel | null;
  onSelectCard: (card: CardModel) => void;
  onAddNote: (text: string) => void;
  onEditNote: (note: Note, card: CardModel, text: string) => void;
  onDeleteNote: (note: Note, card: CardModel) => void;
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
  cardOrder,
  selectedCard,
  onSelectCard,
  onAddNote,
  onEditNote,
  onDeleteNote,
}: NotesPanelProps) {
  const [draft, setDraft] = useState("");
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editDraft, setEditDraft] = useState("");
  const [group, setGroup] = useState<GroupMode>("time");

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

  // In "by card" mode, group the day's notes under their card, ordered the way
  // the cards appear on the board (notes within a card stay in time order).
  const groups = useMemo(() => {
    const byCard = new Map<string, { card: CardModel; notes: Note[] }>();
    for (const { note, card } of notes) {
      let g = byCard.get(card.itemId);
      if (!g) {
        g = { card, notes: [] };
        byCard.set(card.itemId, g);
      }
      g.notes.push(note);
    }
    const rank = (id: string) => {
      const i = cardOrder.indexOf(id);
      return i === -1 ? Number.MAX_SAFE_INTEGER : i;
    };
    return [...byCard.values()].sort(
      (a, b) => rank(a.card.itemId) - rank(b.card.itemId),
    );
  }, [notes, cardOrder]);

  const renderNote = (note: Note, card: CardModel, showCard: boolean) => (
    <div className="note" key={note.id}>
      <div className="note-meta">
        <span className="note-time">{localTime(note.createdAt)}</span>
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
    <aside className="notes">
      <header className="notes-header">
        <span>Notes — {selectedDate}</span>
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
      </header>

      <div className="notes-list">
        {notes.length === 0 && <p className="notes-empty">No notes for this day.</p>}
        {group === "time"
          ? notes.map(({ note, card }) => renderNote(note, card, true))
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
                {g.notes.map((note) => renderNote(note, g.card, false))}
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
