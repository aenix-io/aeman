import { useState } from "react";
import type { Card as CardModel, Note } from "../providers/types";

/** DayNote is a note paired with the card it belongs to, for the day's list. */
export interface DayNote {
  note: Note;
  card: CardModel;
}

interface NotesPanelProps {
  selectedDate: string;
  notes: DayNote[];
  selectedCard: CardModel | null;
  onSelectCard: (card: CardModel) => void;
  onAddNote: (text: string) => void;
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
  selectedCard,
  onSelectCard,
  onAddNote,
}: NotesPanelProps) {
  const [draft, setDraft] = useState("");

  const submit = () => {
    const text = draft.trim();
    if (!selectedCard || !text) {
      return;
    }
    onAddNote(text);
    setDraft("");
  };

  return (
    <aside className="notes">
      <header className="notes-header">Notes — {selectedDate}</header>

      <div className="notes-list">
        {notes.length === 0 && <p className="notes-empty">No notes for this day.</p>}
        {notes.map(({ note, card }) => (
          <div className="note" key={note.id}>
            <div className="note-meta">
              <span className="note-time">{localTime(note.createdAt)}</span>
              <button
                type="button"
                className="note-card"
                onClick={() => onSelectCard(card)}
                title={card.title}
              >
                {card.title}
              </button>
            </div>
            <div className="note-body">{note.body}</div>
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
          placeholder="Write a note…"
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
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
