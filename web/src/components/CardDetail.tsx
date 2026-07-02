import { useEffect, useState } from "react";
import type { Board, Card as CardModel, Provider } from "../providers/types";

interface CardDetailProps {
  card: CardModel;
  board: Board;
  provider: Provider;
  onClose: () => void;
  reload: () => void;
  patchCard: (itemId: string, patch: Partial<CardModel>) => void;
}

/** CardDetail is a centered modal for editing a card's title and details. */
export function CardDetail({
  card,
  board,
  provider,
  onClose,
  reload,
  patchCard,
}: CardDetailProps) {
  const [title, setTitle] = useState(card.title);
  const [editingTitle, setEditingTitle] = useState(false);
  const [description, setDescription] = useState(card.description ?? "");

  // Reset local edit state whenever a different card is opened.
  useEffect(() => {
    setTitle(card.title);
    setDescription(card.description ?? "");
    setEditingTitle(false);
  }, [card.itemId, card.title, card.description]);

  const fail = (err: unknown) => {
    onClose();
    reload();
    // Surface the error via the same banner channel as the boards.
    if (err instanceof Error) {
      console.error(err);
    }
  };

  const commitTitle = () => {
    const next = title.trim();
    setEditingTitle(false);
    if (!next || next === card.title) {
      setTitle(card.title);
      return;
    }
    patchCard(card.itemId, { title: next });
    void provider
      .patchCard(board, card.itemId, { title: next })
      .catch((err: unknown) => {
        patchCard(card.itemId, { title: card.title });
        setTitle(card.title);
        fail(err);
      });
  };

  const handleSave = () => {
    const next = description;
    // The description live-syncs with the linked counterpart (original <->
    // review card) server-side; mirror it locally, since our own watch echo
    // is suppressed. Notes stay per-card.
    const counterpart = board.cards.find((c) =>
      card.reviewOf ? c.itemId === card.reviewOf : c.reviewOf === card.itemId,
    );
    // Apply immediately and close; the backend runs in the background.
    patchCard(card.itemId, { description: next });
    if (counterpart) {
      patchCard(counterpart.itemId, { description: next });
    }
    onClose();
    void provider
      .patchCard(board, card.itemId, { description: next })
      .catch((err: unknown) => {
        patchCard(card.itemId, { description: card.description ?? "" });
        if (counterpart) {
          patchCard(counterpart.itemId, {
            description: counterpart.description ?? "",
          });
        }
        if (err instanceof Error) {
          console.error(err);
        }
      });
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
          {editingTitle ? (
            <input
              type="text"
              className="modal-title-input"
              autoFocus
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  commitTitle();
                } else if (e.key === "Escape") {
                  setTitle(card.title);
                  setEditingTitle(false);
                }
              }}
              onBlur={commitTitle}
            />
          ) : (
            <h2
              className="modal-title modal-title-click"
              onClick={() => {
                setTitle(card.title);
                setEditingTitle(true);
              }}
              title="Click to rename"
            >
              {card.title}
              <span className="modal-title-edit" aria-hidden="true">
                ✎
              </span>
            </h2>
          )}
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
            <span>Details</span>
            <textarea
              className="modal-textarea"
              value={description}
              placeholder="Card details…"
              onChange={(e) => setDescription(e.target.value)}
            />
          </label>
        </div>

        <div className="modal-footer">
          <button type="button" className="btn" onClick={onClose}>
            Close
          </button>
          <button type="button" className="btn btn-primary" onClick={handleSave}>
            Save
          </button>
        </div>
      </div>
    </div>
  );
}
