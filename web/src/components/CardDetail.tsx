import { useEffect, useMemo, useState } from "react";
import type {
  Board,
  Card as CardModel,
  CardEvent,
  Note,
  Provider,
} from "../providers/types";
import { eventLabel } from "../eventlog";
import { localDateIso } from "../date";

interface CardDetailProps {
  card: CardModel;
  board: Board;
  provider: Provider;
  onClose: () => void;
  reload: () => void;
  patchCard: (
    itemId: string,
    patch: Partial<CardModel> | ((c: CardModel) => Partial<CardModel>),
  ) => void;
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
  const [log, setLog] = useState<{ notes: Note[]; events: CardEvent[] } | null>(
    null,
  );
  const [tab, setTab] = useState<"details" | "activity">("details");

  // Reset local edit state whenever a different card is opened.
  useEffect(() => {
    setTitle(card.title);
    setDescription(card.description ?? "");
    setEditingTitle(false);
    setTab("details");
    setLog(null);
  }, [card.itemId, card.title, card.description]);

  // The card's full activity feed (events + notes) loads on demand — fetched
  // fresh every time the Activity tab is opened, so the timeline is current.
  useEffect(() => {
    if (tab !== "activity" || card.itemId.startsWith("tmp-")) {
      return;
    }
    let cancelled = false;
    void provider
      .listLog(board, card.itemId)
      .then((l) => {
        if (!cancelled) {
          setLog(l);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setLog({ notes: [], events: [] });
        }
      });
    return () => {
      cancelled = true;
    };
    // log deliberately not a dep: refetch is keyed to opening the tab.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab, card.itemId, board, provider]);

  // Timeline grouped by day, newest day first (entries inside stay in order).
  const timeline = useMemo(() => {
    if (!log) {
      return [];
    }
    type Entry = { at: string; note?: Note; event?: CardEvent };
    const entries: Entry[] = [
      ...log.notes.map((n) => ({ at: n.createdAt, note: n })),
      ...log.events.map((e) => ({ at: e.at, event: e })),
    ];
    entries.sort((a, b) => a.at.localeCompare(b.at));
    const days = new Map<string, Entry[]>();
    for (const e of entries) {
      const day = localDateIso(e.at) || "—";
      const list = days.get(day) ?? [];
      list.push(e);
      days.set(day, list);
    }
    return [...days.entries()].sort((a, b) => b[0].localeCompare(a[0]));
  }, [log]);

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

        <div className="modal-tabs" role="tablist" aria-label="Card sections">
          <button
            type="button"
            role="tab"
            aria-selected={tab === "details"}
            className={`modal-tab${tab === "details" ? " modal-tab-on" : ""}`}
            onClick={() => setTab("details")}
          >
            Details
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={tab === "activity"}
            className={`modal-tab${tab === "activity" ? " modal-tab-on" : ""}`}
            onClick={() => setTab("activity")}
          >
            Activity
          </button>
        </div>

        {tab === "details" && (
          <div className="modal-body">
            <label className="modal-field">
              <span>Details</span>
              <textarea
                className="modal-textarea"
                value={description}
                placeholder="Card details…"
                maxLength={16384}
                onChange={(e) => setDescription(e.target.value)}
              />
            </label>
          </div>
        )}

        {tab === "activity" && (
          <div className="modal-log">
            {log === null && <p className="notes-empty">Loading…</p>}
            {log !== null && timeline.length === 0 && (
              <p className="notes-empty">No activity yet.</p>
            )}
            {timeline.map(([day, entries]) => (
              <div className="modal-log-day" key={day}>
                <div className="modal-log-date">{day}</div>
                {entries.map((e) =>
                  e.event ? (
                    <div className="modal-log-row modal-log-event" key={e.event.id}>
                      <span className="modal-log-time">
                        {new Date(e.at).toLocaleTimeString([], {
                          hour: "2-digit",
                          minute: "2-digit",
                          hour12: false,
                        })}
                      </span>
                      {e.event.actor ? `@${e.event.actor} · ` : ""}
                      {eventLabel(e.event)}
                    </div>
                  ) : (
                    <div className="modal-log-row" key={e.note?.id}>
                      <span className="modal-log-time">
                        {new Date(e.at).toLocaleTimeString([], {
                          hour: "2-digit",
                          minute: "2-digit",
                          hour12: false,
                        })}
                      </span>
                      {e.note?.author ? `@${e.note.author} · ` : ""}
                      {e.note?.body}
                    </div>
                  ),
                )}
              </div>
            ))}
          </div>
        )}

        <div className="modal-footer">
          <button type="button" className="btn" onClick={onClose}>
            Close
          </button>
          {tab === "details" && (
            <button type="button" className="btn btn-primary" onClick={handleSave}>
              Save
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
