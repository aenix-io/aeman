import { useEffect, useMemo, useState } from "react";
import type {
  Board,
  Card as CardModel,
  CardEvent,
  CardLog,
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
  // dirty marks the draft as the user's: once they typed, nothing arriving
  // from the board (a background re-list, the lazy body fetch, a watch frame)
  // may overwrite it. bodyFailed gates Save when the body never arrived.
  const [dirty, setDirty] = useState(false);
  const [bodyFailed, setBodyFailed] = useState(false);
  const [log, setLog] = useState<CardLog | null>(null);
  const [tab, setTab] = useState<"details" | "activity">("details");
  const bodyLoaded = card.description !== undefined;

  // Reset local edit state ONLY when a different card is opened — the card
  // object itself churns underneath the dialog (watch frames, re-lists, the
  // body fetch below), and none of that may wipe what the user is typing.
  useEffect(() => {
    setTitle(card.title);
    setDescription(card.description ?? "");
    setDirty(false);
    setBodyFailed(false);
    setEditingTitle(false);
    setTab("details");
    setLog(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [card.itemId]);

  // Listings are board rows without the body: fetch it when the dialog opens
  // on one. The result lands in the board (so every pane agrees) and into the
  // draft — unless the user already typed. A failure surfaces and keeps Save
  // locked instead of silently offering to save "" over the real text.
  useEffect(() => {
    if (bodyLoaded || card.itemId.startsWith("tmp-")) {
      return;
    }
    let dropped = false;
    void provider
      .getCard(card.itemId)
      .then((full) => {
        if (dropped) {
          return;
        }
        patchCard(card.itemId, { description: full.description ?? "" });
      })
      .catch(() => {
        if (!dropped) {
          setBodyFailed(true);
        }
      });
    return () => {
      dropped = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [card.itemId, bodyLoaded]);

  // The body arriving (or changing under an untouched dialog — a teammate
  // edited it) refreshes the draft; a dirty draft is the user's and stays.
  useEffect(() => {
    if (!dirty && card.description !== undefined) {
      setDescription(card.description);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [card.description]);

  // The card's full activity feed (events + notes) loads on demand — fetched
  // fresh every time the Activity tab is opened, so the timeline is current.
  useEffect(() => {
    if (tab !== "activity" || card.itemId.startsWith("tmp-")) {
      return;
    }
    let cancelled = false;
    void provider
      .listLog(card.itemId)
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
  }, [tab, card.itemId, provider]);

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
      .patchCard(card.itemId, { title: next })
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
    // Apply immediately and close; the backend runs in the background. The
    // stale server-derived linkRefs drop so the row falls back to parsing the
    // new text — the links icon follows the edit. A failed write restores
    // exactly what was there, including a counterpart body that was never
    // loaded (undefined stays undefined — never invented as "empty").
    patchCard(card.itemId, { description: next, linkRefs: undefined });
    if (counterpart) {
      patchCard(counterpart.itemId, { description: next, linkRefs: undefined });
    }
    onClose();
    void provider
      .patchCard(card.itemId, { description: next })
      .catch((err: unknown) => {
        patchCard(card.itemId, {
          description: card.description,
          linkRefs: card.linkRefs,
        });
        if (counterpart) {
          patchCard(counterpart.itemId, {
            description: counterpart.description,
            linkRefs: counterpart.linkRefs,
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

        {/* Where this card comes from. A turn of a process and a slot of a
            project look like ordinary cards on a day board, and the first
            question about one is always "what is this part of?". */}
        {(card.process || card.epic || card.project) && (
          <div className="modal-origin">
            {card.process && (
              <span className="modal-origin-item" title="A turn of this process">
                <span className="modal-origin-kind">process</span>
                {card.process}
              </span>
            )}
            {card.project && (
              <span className="modal-origin-item" title="Part of this project">
                <span className="modal-origin-kind">project</span>
                {card.project}
              </span>
            )}
            {card.epic && (
              <span className="modal-origin-item" title="In this column of the plan">
                <span className="modal-origin-kind">epic</span>
                {card.epic}
              </span>
            )}
          </div>
        )}

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
                placeholder={
                  bodyLoaded || card.itemId.startsWith("tmp-")
                    ? "Card details…"
                    : bodyFailed
                      ? "Failed to load the description — close and reopen to retry."
                      : "Loading the description…"
                }
                maxLength={16384}
                disabled={!bodyLoaded && !card.itemId.startsWith("tmp-")}
                onChange={(e) => {
                  setDirty(true);
                  setDescription(e.target.value);
                }}
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
            {/* The server's clone reaches only so far back: what is shown is
                everything it has, not everything that happened. */}
            {log?.truncatedBefore && (
              <p className="notes-empty modal-log-notice">
                Older history before {localDateIso(log.truncatedBefore)} is not loaded.
              </p>
            )}
          </div>
        )}

        <div className="modal-footer">
          <button type="button" className="btn" onClick={onClose}>
            Close
          </button>
          {tab === "details" && (
            <button
              type="button"
              className="btn btn-primary"
              onClick={handleSave}
              // Saving over a body that never arrived would write "" over the
              // real text for everyone (and its review counterpart).
              disabled={!bodyLoaded && !card.itemId.startsWith("tmp-")}
            >
              Save
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
