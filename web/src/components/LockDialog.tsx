import { useState } from "react";
import type { Card as CardModel } from "../providers/types";

interface LockDialogProps {
  card: CardModel;
  onClose: () => void;
  /** Called with a non-empty reason note when the user confirms the lock. */
  onSubmit: (card: CardModel, note: string) => void;
}

/** LockDialog is a centered modal asking why a card is being locked. */
export function LockDialog({ card, onClose, onSubmit }: LockDialogProps) {
  const [note, setNote] = useState("");

  const submit = () => {
    const trimmed = note.trim();
    if (!trimmed) {
      return;
    }
    onSubmit(card, trimmed);
    onClose();
  };

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div
        className="modal"
        role="dialog"
        aria-modal="true"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="modal-header">
          <h2 className="modal-title">Why locked?</h2>
          <button
            type="button"
            className="modal-close"
            onClick={onClose}
            aria-label="Close"
          >
            ✕
          </button>
        </div>

        <div className="modal-body">
          <label className="modal-field">
            <span>Reason (posted as a note on “{card.title}”)</span>
            <textarea
              className="modal-textarea"
              autoFocus
              value={note}
              placeholder="Why is this card blocked?"
              onChange={(e) => setNote(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Escape") {
                  onClose();
                }
              }}
            />
          </label>
        </div>

        <div className="modal-footer">
          <button type="button" className="btn" onClick={onClose}>
            Cancel
          </button>
          <button
            type="button"
            className="btn btn-primary"
            disabled={!note.trim()}
            onClick={submit}
          >
            Lock
          </button>
        </div>
      </div>
    </div>
  );
}
