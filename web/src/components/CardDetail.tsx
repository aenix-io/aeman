import { useEffect, useMemo, useRef, useState } from "react";
import type {
  Board,
  Card as CardModel,
  CardEvent,
  CardLog,
  Note,
  Provider,
  SizeKey,
  ZoneKey,
} from "../providers/types";
import { ZONES, ZONE_ORDER } from "../zones";
import { SIZES, SIZE_ORDER } from "../size";
import { Dropdown } from "./Dropdown";
import { eventLabel } from "../eventlog";
import { localDateIso } from "../date";
import {
  GLYPH,
  KIND_LABEL,
  STATUS_LABEL,
  allowedParentKinds,
  kindOf,
  statusOf,
  type RoadmapKind,
} from "../roadmap";
import { isPlanning, isSubtask } from "../subtasks";

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
  /** A breadcrumb was followed: show that card on the Roadmap. */
  onReveal?: (uid: string) => void;
  /** A row under a planning card was clicked: open that card here. */
  onOpenCard?: (card: CardModel) => void;
  /** A write the server refused, in its own words (a parent of the wrong
   *  level, another repository, a project card). */
  onError?: (message: string) => void;
}

const errMessage = (err: unknown) => (err instanceof Error ? err.message : String(err));

/** CardDetail is a centered modal for editing a card's title and details.
 *  On a RECORD — a card as it stood on a day its team has moved past — it
 *  reads instead: the boxes are disabled and there is nothing to save, since
 *  a change made from there would land on today's card while the reader is
 *  looking at a picture of a day that ended (G60). */
export function CardDetail({
  card,
  board,
  provider,
  onClose,
  reload,
  patchCard,
  onReveal,
  onOpenCard,
  onError,
}: CardDetailProps) {
  // A record: what the card was on a day its team has moved past. Nothing
  // here is editable, and the pane says so rather than pretending.
  const isRecord = !!card.asOf;
  const [title, setTitle] = useState(card.title);
  const planning = isPlanning(card);
  const kind = kindOf(card);

  // The hierarchy around this card: what it may sit under, and what sits
  // under it. Both come from the roadmap listing, which holds every planning
  // card and every card under one — the day boards never deliver a planning
  // card, so a detail pane opened from one has only the lineage until this
  // arrives. Fetched once per card, when the pane needs it: a planning
  // card's children at once, a task's parent options when the picker opens.
  const [hierarchy, setHierarchy] = useState<{ forCard: string; cards: CardModel[] } | null>(null);
  const [parentOpen, setParentOpen] = useState(false);
  const parentRef = useRef<HTMLSpanElement | null>(null);
  const wantsHierarchy = planning || parentOpen;
  useEffect(() => {
    if (!wantsHierarchy || card.itemId.startsWith("tmp-") || hierarchy?.forCard === card.itemId) {
      return;
    }
    let dropped = false;
    void provider
      .listCards({ view: "roadmap" })
      .then((l) => {
        if (!dropped) {
          setHierarchy({ forCard: card.itemId, cards: l.cards });
        }
      })
      .catch(() => {
        if (!dropped) {
          setHierarchy({ forCard: card.itemId, cards: [] });
        }
      });
    return () => {
      dropped = true;
    };
  }, [wantsHierarchy, card.itemId, provider, hierarchy?.forCard]);
  const around = hierarchy?.forCard === card.itemId ? hierarchy.cards : null;

  // The parent as the pane shows it: the last crumb of the lineage.
  const parentCrumb = card.lineage?.[card.lineage.length - 1];
  // What this card may be filed under (H2): planning cards of a higher
  // level, never itself or anything under it. A goal is under nothing; a
  // subtask's parent is the group gesture's, not this picker's.
  const parentKinds = allowedParentKinds(kind);
  const offersParent = !isRecord && parentKinds.length > 0 && !isSubtask(card);
  const parentOptions = useMemo(() => {
    if (!around) {
      return null;
    }
    return around.filter(
      (c) =>
        isPlanning(c) &&
        parentKinds.includes(kindOf(c)) &&
        c.itemId !== card.itemId &&
        !(c.lineage ?? []).some((up) => up.uid === card.itemId),
    );
  }, [around, parentKinds, card.itemId]);

  const setParent = (next: CardModel | null) => {
    setParentOpen(false);
    const nextId = next?.itemId ?? "";
    if ((card.parent ?? "") === nextId) {
      return;
    }
    const before = { parent: card.parent, lineage: card.lineage };
    // The optimistic lineage: the parent's own plus the parent, so the
    // breadcrumb and the Roadmap's tree read right before the server answers.
    patchCard(card.itemId, {
      parent: nextId || undefined,
      lineage: next ? [...(next.lineage ?? []), { uid: next.itemId, type: next.type, title: next.title }] : undefined,
    });
    void provider
      .patchCard(card.itemId, { parent: nextId })
      .then((updated) => patchCard(card.itemId, { parent: updated.parent, lineage: updated.lineage }))
      .catch((err: unknown) => {
        patchCard(card.itemId, before);
        onError?.(errMessage(err));
      });
  };

  // A planning card's dates are its bar on the Roadmap; nothing else about
  // its placement is its own (the server refuses the rest), so this is the
  // one pair of boxes the pane offers it.
  const setDates = (start: string, end: string) => {
    if (start === (card.startDate ?? "") && end === (card.day ?? "")) {
      return;
    }
    const before = { startDate: card.startDate, day: card.day };
    patchCard(card.itemId, { startDate: start || undefined, day: end || undefined });
    void provider
      .patchCard(card.itemId, { dates: { start, end } })
      .then((updated) => patchCard(card.itemId, { startDate: updated.startDate, day: updated.day }))
      .catch((err: unknown) => {
        patchCard(card.itemId, before);
        onError?.(errMessage(err));
      });
  };

  // What sits under a planning card, grouped as the pane lists it.
  const children = useMemo(() => {
    const under = (around ?? board.cards).filter((c) => c.parent === card.itemId);
    const groups: { label: string; kinds: RoadmapKind[]; cards: CardModel[] }[] = [
      { label: "Planning", kinds: ["goal", "initiative", "epic"], cards: [] },
      { label: "Tasks", kinds: ["task", "subtask"], cards: [] },
      { label: "Project cards", kinds: ["project"], cards: [] },
    ];
    for (const c of under) {
      const k = kindOf(c);
      groups.find((g) => g.kinds.includes(k))?.cards.push(c);
    }
    return groups.filter((g) => g.cards.length > 0);
  }, [around, board.cards, card.itemId]);

  const [editingTitle, setEditingTitle] = useState(false);
  // Esc closes this card — the pane is a reader as often as an editor, and
  // reaching for the mouse to leave a card you only opened to look at is a
  // small tax paid over and over. The key is taken on the BACKDROP rather
  // than on the window: a dialog opened over the card answers its own Esc,
  // and the card underneath must not close at the same time. The title
  // editor answers first (it cancels the rename), so a card being renamed is
  // not closed out from under the typing.
  const backdropRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    const node = backdropRef.current;
    if (node && !node.contains(document.activeElement)) {
      node.focus();
    }
  }, []);
  // What kind of work this is. It is set by which area of the Team board a
  // card stands in, which is no help when the card is met anywhere else —
  // and on the Triage board a column is a person, so there is no area to
  // drop it into at all.
  const zoneRef = useRef<HTMLSpanElement | null>(null);
  const [zoneOpen, setZoneOpen] = useState(false);
  const setZone = (zone: ZoneKey) => {
    setZoneOpen(false);
    if (zone === card.zone) {
      return;
    }
    const before = card.zone;
    patchCard(card.itemId, { zone });
    void provider.patchCard(card.itemId, { zone }).catch((err: unknown) => {
      patchCard(card.itemId, { zone: before });
      fail(err);
    });
  };

  // What the card WEIGHS — the decision a daily sync makes so the person
  // leaves with a plan that fits. The same letter again takes it back.
  const setSize = (size: SizeKey) => {
    const next: SizeKey | "" = size === card.size ? "" : size;
    const before = card.size;
    patchCard(card.itemId, { size: next || undefined });
    void provider.patchCard(card.itemId, { size: next }).catch((err: unknown) => {
      patchCard(card.itemId, { size: before });
      fail(err);
    });
  };

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
    <div
      className="modal-backdrop"
      onClick={onClose}
      ref={backdropRef}
      tabIndex={-1}
      onKeyDown={(e) => {
        if (e.key === "Escape" && !editingTitle) {
          e.stopPropagation();
          onClose();
        }
      }}
    >
      <div
        className="modal"
        role="dialog"
        aria-modal="true"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Where this card stands in the hierarchy, root first: the goal,
            the initiative, the epic — and for a subtask, its task. A
            planning crumb leads to the Roadmap, which is the one board that
            draws it; a task crumb is a name, since the day boards are where a
            task is met. */}
        {card.lineage && card.lineage.length > 0 && (
          <nav className="modal-crumbs" aria-label="Hierarchy">
            {card.lineage.map((up, i) => (
              <span key={up.uid} className="modal-crumb">
                {i > 0 && (
                  <span className="modal-crumb-sep" aria-hidden="true">
                    ›
                  </span>
                )}
                <span className={`roadmap-glyph roadmap-glyph-${up.type ?? "task"}`} aria-hidden="true">
                  {GLYPH[up.type ?? "task"]}
                </span>
                {up.type && onReveal ? (
                  <button
                    type="button"
                    className="modal-crumb-link"
                    title={`Show «${up.title}» on the Roadmap`}
                    onClick={() => onReveal(up.uid)}
                  >
                    {up.title}
                  </button>
                ) : (
                  <span className="modal-crumb-name">{up.title}</span>
                )}
              </span>
            ))}
          </nav>
        )}
        <div className="modal-header">
          {editingTitle && !isRecord ? (
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
              className={isRecord ? "modal-title" : "modal-title modal-title-click"}
              onClick={() => {
                if (isRecord) {
                  return;
                }
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

        <div className="modal-toolbar">
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
          {/* Where this card comes from. A slot of a project looks like an
              ordinary card on a day board, and the first question about one
              is always "what is this part of?". */}
          <div className="modal-origin">
            {/* A planning card's level and standing: a goal, an initiative or
                an epic, and what the tasks under it add up to (H5). */}
            {planning && (
              <span className="modal-origin-item modal-level" title={KIND_LABEL[kind]}>
                <span className="modal-origin-kind">{KIND_LABEL[kind].toLowerCase()}</span>
                <span className={`roadmap-status roadmap-status-${statusOf(card)}`}>
                  {STATUS_LABEL[statusOf(card)]}
                </span>
                {card.hierarchy && card.hierarchy.total > 0 && (
                  <span className="modal-level-count" title="Tasks done, of those under it">
                    {card.hierarchy.done}/{card.hierarchy.total}
                  </span>
                )}
              </span>
            )}
            {/* What kind of work it is, and the one thing here that can be
                said from anywhere: a card met on a board with no zone areas
                still has a zone, and this is where it is given one. A
                planning card has no band (the server refuses one). */}
            {!planning && (
            <span className="modal-origin-item modal-zone" ref={zoneRef}>
              <span className="modal-origin-kind">zone</span>
              <button
                type="button"
                className="modal-zone-btn"
                disabled={isRecord}
                onClick={() => setZoneOpen((o) => !o)}
                title="What kind of work this is"
              >
                {card.zone && (
                  <span
                    className="team-dot"
                    style={{ background: ZONES[card.zone].accent }}
                  />
                )}
                {card.zone ? ZONES[card.zone].spine.toLowerCase() : "none"}
                {!isRecord && <span className="add-card-team-caret">▾</span>}
              </button>
              <Dropdown
                open={zoneOpen}
                anchorRef={zoneRef}
                onClose={() => setZoneOpen(false)}
                className="add-card-team-menu"
              >
                {ZONE_ORDER.map((z) => (
                  <button
                    key={z}
                    type="button"
                    className="add-card-team-item"
                    onClick={() => setZone(z)}
                  >
                    <span className="team-dot" style={{ background: ZONES[z].accent }} />
                    {ZONES[z].spine.toLowerCase()}
                  </button>
                ))}
              </Dropdown>
            </span>
            )}
            {/* What the card weighs. Four chips, one lit: the decision a daily
                sync makes so the person leaves with a plan that fits their
                week, and the number the board sums against their capacity. */}
            <span className="modal-origin-item modal-size" title="What the card weighs">
              <span className="modal-origin-kind">size</span>
              <span className="modal-size-chips" role="group" aria-label="Size">
                {SIZE_ORDER.map((s) => (
                  <button
                    key={s}
                    type="button"
                    className={`modal-size-chip${card.size === s ? " modal-size-chip-on" : ""}`}
                    disabled={isRecord}
                    aria-pressed={card.size === s}
                    title={`${s} — ${SIZES[s].hint} (${SIZES[s].points} pt)`}
                    onClick={() => setSize(s)}
                  >
                    {s}
                  </button>
                ))}
              </span>
            </span>
              {card.project && (
                <span className="modal-origin-item" title="Part of this project">
                  <span className="modal-origin-kind">project</span>
                  {card.project}
                </span>
              )}
            {card.scope && (
              <span className="modal-origin-item" title="In this column of the plan">
                <span className="modal-origin-kind">scope</span>
                {card.scope}
              </span>
            )}
            {/* What this card sits UNDER — the one link of the hierarchy. A
                subtask's parent is its task and is changed by the group
                gesture on the boards; everything else picks a planning card
                of a higher level here, or none. The server holds the rule
                (H2, H6) and says so in its own words when it refuses. */}
            {isSubtask(card) && parentCrumb && (
              <span className="modal-origin-item" title="A subtask of this card">
                <span className="modal-origin-kind">subtask of</span>
                {parentCrumb.title}
              </span>
            )}
            {offersParent && (
              <span className="modal-origin-item modal-parent" ref={parentRef}>
                <span className="modal-origin-kind">under</span>
                <button
                  type="button"
                  className="modal-zone-btn"
                  onClick={() => setParentOpen((o) => !o)}
                  title="The goal, initiative or epic this card is filed under"
                >
                  {parentCrumb ? (
                    <>
                      <span className={`roadmap-glyph roadmap-glyph-${parentCrumb.type ?? "task"}`} aria-hidden="true">
                        {GLYPH[parentCrumb.type ?? "task"]}
                      </span>
                      {parentCrumb.title}
                    </>
                  ) : (
                    "none"
                  )}
                  <span className="add-card-team-caret">▾</span>
                </button>
                <Dropdown
                  open={parentOpen}
                  anchorRef={parentRef}
                  onClose={() => setParentOpen(false)}
                  className="add-card-team-menu modal-parent-menu"
                >
                  <button type="button" className="add-card-team-item" onClick={() => setParent(null)}>
                    none
                  </button>
                  {parentOptions === null && <span className="add-card-team-item modal-parent-loading">Loading…</span>}
                  {parentOptions?.map((c) => (
                    <button
                      key={c.itemId}
                      type="button"
                      className="add-card-team-item"
                      title={KIND_LABEL[kindOf(c)]}
                      onClick={() => setParent(c)}
                    >
                      <span className={`roadmap-glyph roadmap-glyph-${kindOf(c)}`} aria-hidden="true">
                        {GLYPH[kindOf(c)]}
                      </span>
                      {c.title}
                    </button>
                  ))}
                  {parentOptions && parentOptions.length === 0 && (
                    <span className="add-card-team-item modal-parent-loading">
                      Nothing of a higher level on the roadmap yet
                    </span>
                  )}
                </Dropdown>
              </span>
            )}
          </div>
        </div>

        {tab === "details" && (
          <div className="modal-body">
            {planning && (
              <div className="modal-dates" role="group" aria-label="Dates">
                <label className="modal-date">
                  <span>Start</span>
                  <input
                    type="date"
                    value={card.startDate ?? ""}
                    disabled={isRecord}
                    onChange={(e) => setDates(e.target.value, card.day ?? "")}
                  />
                </label>
                <label className="modal-date">
                  <span>End</span>
                  <input
                    type="date"
                    value={card.day ?? ""}
                    disabled={isRecord}
                    onChange={(e) => setDates(card.startDate ?? "", e.target.value)}
                  />
                </label>
              </div>
            )}
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
                readOnly={isRecord}
                disabled={isRecord || (!bodyLoaded && !card.itemId.startsWith("tmp-"))}
                onChange={(e) => {
                  setDirty(true);
                  setDescription(e.target.value);
                }}
              />
            </label>
            {/* What sits under a planning card, as the Roadmap groups it: the
                levels below, the tasks, the project cards. A row opens that
                card here. */}
            {planning && (
              <div className="modal-children">
                {around === null && !card.itemId.startsWith("tmp-") && (
                  <p className="notes-empty">Loading what sits under it…</p>
                )}
                {around !== null && children.length === 0 && (
                  <p className="notes-empty">
                    Nothing under it yet. File a card here from its own detail pane, or add a level
                    under it on the Roadmap.
                  </p>
                )}
                {children.map((g) => (
                  <div key={g.label} className="modal-children-group">
                    <div className="modal-children-label">
                      {g.label} <span className="modal-level-count">{g.cards.length}</span>
                    </div>
                    {g.cards.map((c) => (
                      <button
                        key={c.itemId}
                        type="button"
                        className="modal-child"
                        onClick={() => onOpenCard?.(c)}
                        disabled={!onOpenCard}
                      >
                        <span className={`roadmap-glyph roadmap-glyph-${kindOf(c)}`} aria-hidden="true">
                          {GLYPH[kindOf(c)]}
                        </span>
                        <span className="modal-child-title">{c.title}</span>
                        <span className={`roadmap-status roadmap-status-${statusOf(c)}`}>
                          {STATUS_LABEL[statusOf(c)]}
                        </span>
                      </button>
                    ))}
                  </div>
                ))}
              </div>
            )}
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
          {tab === "details" && !isRecord && (
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
