import { useEffect, useMemo, useRef, useState, type Ref } from "react";
import {
  cancelPendingCard,
  consumePendingCancel,
  registerPendingCard,
} from "../api/pending";
import { isWorkable } from "../stages";
import type {
  Board,
  Card as CardModel,
  Note,
  Provider,
  StageKey,
  ZoneKey,
} from "../providers/types";
import { ZONES, ZONE_ORDER } from "../zones";
import { todayIso, localDateIso, addDays } from "../date";
import { activeSprint, currentSprint } from "../sprint";
import { avatarUrlFor, displayName, type GhUser } from "../users";
import { Card } from "./Card";
import { AddCard } from "./AddCard";
import { Dropdown } from "./Dropdown";
import { TeamChips } from "./TeamChips";
import { NotesPanel, type DayNote } from "./NotesPanel";
import { ConnectDialog } from "./ConnectDialog";
import { SortableBoard, type BoardGroup, type DropResult } from "./SortableBoard";
import { globalOrderFromGroups, afterIdFor } from "./dndOrder";

interface MeBoardProps {
  board: Board;
  provider: Provider;
  me: string;
  /** GitHub user details (avatars / names) for the impersonate picker. */
  users: Record<string, GhUser>;
  /** Known teams to offer in the team selector. */
  teams: string[];
  /** Shared single-select team (also the team for new cards); null = none. */
  teamFilter: string[] | null;
  onSetFilter: (keys: string[] | null) => void;
  onAddTeam: (team: string) => void;
  onRemoveTeam: (team: string) => void;
  onRenameTeam: (from: string, to: string) => void;
  patchCard: (itemId: string, patch: Partial<CardModel>) => void;
  addCard: (card: CardModel) => void;
  removeCard: (itemId: string) => void;
  reorderCards: (orderedItemIds: string[]) => void;
  reload: () => void;
  onError: (message: string) => void;
  onOpen: (card: CardModel) => void;
  /** Share the live selection with other boards ("" clears on deselect). */
  onPresence?: (card: string | null) => void;
}

/** Per-group metadata for the Me board: just the destination zone. */
interface MeMeta {
  zone: ZoneKey;
}

const errMessage = (err: unknown) =>
  err instanceof Error ? err.message : String(err);

// isGone reports a "card not found" failure: the card no longer exists on the
// server, so an optimistic removal must NOT be rolled back (re-adding it would
// resurrect a phantom copy).
const isGone = (err: unknown) => errMessage(err).includes("card not found");

/** MeBoard is the personal day view: my cards stacked in zone bands + notes. */
export function MeBoard({
  board,
  provider,
  me,
  users,
  teams,
  teamFilter,
  onSetFilter,
  onAddTeam,
  onRemoveTeam,
  onRenameTeam,
  patchCard,
  addCard,
  removeCard,
  reorderCards,
  reload,
  onError,
  onOpen,
  onPresence,
}: MeBoardProps) {
  const [selectedDate, setSelectedDate] = useState<string>(todayIso());
  const [selectedCardId, setSelectedCardId] = useState<string | null>(null);
  // Broadcast the selection as shared presence: teammates' Team boards
  // highlight this card with our avatar. Cleared on deselect and unmount.
  useEffect(() => {
    onPresence?.(selectedCardId);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedCardId]);
  useEffect(() => () => onPresence?.(null), []); // eslint-disable-line react-hooks/exhaustive-deps
  // Eye toggle by the team chips: when on, show only the selected teams' cards.
  // Deliberately not persisted — resets to off (show all) on reload.
  const [teamFocus, setTeamFocus] = useState(false);
  // Focus toggle (the meditation glyph by the View-as picker): show only cards
  // that can be worked on right now — drops locked/review/done. Ephemeral,
  // resets to off on reload, like the team-focus eye.
  const [focus, setFocus] = useState(false);
  // Impersonate: view (and act on) the board as another person.
  const [impersonated, setImpersonated] = useState<string | null>(null);
  const [impOpen, setImpOpen] = useState(false);
  // MCP / API connect dialog.
  const [connectOpen, setConnectOpen] = useState(false);

  // Notes fold to a header bar on narrow screens (like the Team weekly plan) and
  // stay open as a side pane on wide ones; the breakpoint matches .me-panes.
  const [notesCollapsed, setNotesCollapsed] = useState(
    () => window.matchMedia("(max-width: 820px)").matches,
  );
  useEffect(() => {
    const mq = window.matchMedia("(max-width: 820px)");
    const onChange = (e: MediaQueryListEvent) => setNotesCollapsed(e.matches);
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, []);
  const impRef = useRef<HTMLDivElement | null>(null);
  const viewMe = impersonated ?? me;
  // Other people with cards — offered in the "View as" impersonate picker.
  const others = useMemo(
    () =>
      [...new Set(board.cards.flatMap((c) => c.assignees))]
        .filter((p) => p && p !== me)
        .sort(),
    [board.cards, me],
  );

  // People to offer when picking a reviewer: everyone seen on the board, plus
  // me — the same roster the Team board's assign menu uses.
  const people = useMemo(() => {
    const set = new Set<string>();
    for (const card of board.cards) {
      for (const login of card.assignees) {
        set.add(login);
      }
    }
    if (me) {
      set.add(me);
    }
    return [...set].sort((a, b) => a.localeCompare(b));
  }, [board.cards, me]);

  // My cards (any day); when me is empty, everyone's.
  const mine = useMemo(
    () => board.cards.filter((c) => (viewMe ? c.assignees.includes(viewMe) : true)),
    [board.cards, viewMe],
  );

  // In Me a card shows when it belongs to the sprint that was active on the viewed
  // day (activeSprint) and its scheduled day has arrived (startDate empty or on or
  // before the viewed day). Today shows the current sprint; rolling back into the
  // previous sprint's days shows that sprint's cards. A team with no active sprint
  // on the day, or a card deferred to the future, never shows.
  const myCards = useMemo(
    () =>
      mine.filter((c) => {
        if (focus && !isWorkable(c)) {
          return false;
        }
        if (teamFocus && teamFilter && !teamFilter.includes(c.team ?? "")) {
          return false;
        }
        const today = todayIso();
        // A deferred / future-scheduled card (startDate past today) is hidden
        // until that day, then shows from it on (Carry Over re-syncs its sprint).
        if (c.startDate && c.startDate > today) {
          return selectedDate >= c.startDate;
        }
        // A card with an end date spans a range: it shows on every day from its
        // start through its end regardless of sprint boundaries.
        if (
          c.startDate &&
          c.day &&
          c.day >= c.startDate &&
          selectedDate >= c.startDate &&
          selectedDate <= c.day
        ) {
          return true;
        }
        const as = activeSprint(board, c.team ?? null, selectedDate);
        const ss = c.sprintStart;
        // A card shows on every day of the sprints it spans — from the one it
        // started in up to the sprint it now belongs to — so a carried-over card
        // still appears on the previous sprint's days it came from.
        return (
          as !== "" &&
          ss !== undefined &&
          as <= ss &&
          (!c.startDate || c.startDate <= selectedDate)
        );
      }),
    [mine, board, selectedDate, teamFocus, teamFilter, focus],
  );

  const byZone = useMemo(() => {
    const buckets: Record<ZoneKey, CardModel[]> = {
      gray: [],
      green: [],
      yellow: [],
      red: [],
    };
    for (const card of myCards) {
      buckets[card.zone ?? "gray"].push(card);
    }
    return buckets;
  }, [myCards]);

  // Overall completion across the day's cards (a done card counts as 100%) — the
  // thin bar under the zones, mirroring the weekly plan's progress strip.
  const dayProgress = useMemo(() => {
    if (myCards.length === 0) {
      return 0;
    }
    const sum = myCards.reduce(
      (s, c) => s + (c.stage === "done" ? 100 : c.progress ?? 0),
      0,
    );
    return Math.round(sum / myCards.length);
  }, [myCards]);

  // Closed/total counts for the day — overall and per zone — for the status bar.
  const dayStats = useMemo(() => {
    const stat = (cards: CardModel[]) => ({
      done: cards.filter((c) => c.stage === "done").length,
      total: cards.length,
    });
    return {
      total: stat(myCards),
      red: stat(byZone.red),
      yellow: stat(byZone.yellow),
      gray: stat(byZone.gray),
      green: stat(byZone.green),
    };
  }, [myCards, byZone]);

  // Card item ids in board (display) order, for grouping notes by card.
  const noteCardOrder = useMemo(
    () => ZONE_ORDER.flatMap((z) => byZone[z].map((c) => c.itemId)),
    [byZone],
  );

  const dayNotes = useMemo<DayNote[]>(() => {
    const out: DayNote[] = [];
    for (const card of myCards) {
      for (const note of card.notes ?? []) {
        if (localDateIso(note.createdAt) === selectedDate) {
          out.push({ note, card });
        }
      }
    }
    out.sort((a, b) => a.note.createdAt.localeCompare(b.note.createdAt));
    return out;
  }, [myCards, selectedDate]);

  // Notes live in the notes subresource, not on the Card resource: lazily load
  // them for the day's visible cards. A card's loaded notes are the "fetched"
  // marker (mutations and the watch keep them fresh); a re-list clears them,
  // so they refetch. A request that failed stays marked and is not retried.
  const notesRequested = useRef<Set<string>>(new Set());
  useEffect(() => {
    for (const c of myCards) {
      if (
        c.notes !== undefined ||
        c.itemId.startsWith("tmp-") ||
        notesRequested.current.has(c.itemId)
      ) {
        continue;
      }
      notesRequested.current.add(c.itemId);
      void provider
        .listNotes(board, c.itemId)
        .then((notes) => {
          notesRequested.current.delete(c.itemId);
          patchCard(c.itemId, { notes });
        })
        .catch(() => {});
    }
  }, [myCards, board, provider, patchCard]);

  const selectedCard =
    myCards.find((c) => c.itemId === selectedCardId) ?? null;

  const fail = (err: unknown) => {
    onError(err instanceof Error ? err.message : String(err));
    reload();
  };

  // Progress is one intent; the server clamps (review/locked stay in 10–90),
  // clears a legacy stored done below full, and — when this is a review card —
  // drives the original's review stage. The optimistic patch mirrors the
  // clamps; the re-list converges the linked original.
  // Resolve a card's description links (GitHub refs get titles) for the menu.
  const loadCardLinks = (card: CardModel) =>
    provider.listLinks(board, card.itemId);

  const handleProgress = (card: CardModel, raw: number) => {
    const value =
      card.stage === "review" || card.stage === "locked"
        ? Math.min(90, Math.max(10, raw))
        : raw;
    const prev: Partial<CardModel> = { progress: card.progress, stage: card.stage };
    const patch: Partial<CardModel> = { progress: value };
    if (value < 100 && card.stage === "done") {
      patch.stage = undefined;
    }
    patchCard(card.itemId, patch);
    void provider
      .patchCard(board, card.itemId, { progress: value })
      .then((updated) => {
        addCard(updated);
        // A review card's progress drives the original's stage server-side.
        if (card.reviewOf) {
          reload();
        }
      })
      .catch((err: unknown) => {
        patchCard(card.itemId, prev);
        onError(errMessage(err));
      });
  };

  // Stage is one intent; the server derives done (clears stage + fills 100),
  // knocks a full review/locked card to 90, and cancels the linked review card
  // when the original leaves review. The optimistic patch mirrors the local
  // effects; the re-list converges the linked-card cascade.
  const handleStage = (card: CardModel, stage: StageKey | null) => {
    const prev: Partial<CardModel> = {
      stage: card.stage,
      progress: card.progress,
    };
    const patch: Partial<CardModel> = {
      stage: stage === "done" ? undefined : stage ?? undefined,
    };
    if (stage === "done") {
      patch.progress = 100;
    }
    if (stage === "review" || stage === "locked") {
      // The 10-90 clamp is stored on stage pick (mirrors board.ApplyStage).
      patch.progress = Math.min(90, Math.max(10, card.progress ?? 0));
    }
    patchCard(card.itemId, patch);
    const leavingReview = card.stage === "review" && stage !== "review";
    void provider
      .patchCard(board, card.itemId, { stage: stage ?? "" })
      .then((updated) => {
        addCard(updated);
        if (leavingReview || card.reviewOf) {
          reload();
        }
      })
      .catch((err: unknown) => {
        patchCard(card.itemId, prev);
        onError(errMessage(err));
      });
  };

  // "In Progress" is the implicit status (no stage, progress in [10, 90]) —
  // one action; the server clears the stage, nudges progress into the band,
  // cancels a linked review card and syncs a review card's original.
  const handleInProgress = (card: CardModel) => {
    const cur = card.progress ?? 0;
    let value = cur;
    if (cur < 10) {
      value = 10;
    } else if (card.stage === "done" || cur >= 100) {
      value = 90;
    }
    const prev: Partial<CardModel> = { stage: card.stage, progress: card.progress };
    patchCard(card.itemId, { stage: undefined, progress: value });
    void provider
      .setInProgress(board, card.itemId)
      .then((updated) => {
        addCard(updated);
        if (card.stage === "review" || card.reviewOf) {
          reload();
        }
      })
      .catch((err: unknown) => {
        patchCard(card.itemId, prev);
        onError(errMessage(err));
      });
  };

  // Moving a card between teams also joins the new team's current sprint
  // (server-side), so it stays visible instead of dropping off when its old
  // sprint predates the new team's current one. Mirror both optimistically.
  const handleSetTeam = (card: CardModel, team: string | null) => {
    const prev: Partial<CardModel> = {
      team: card.team,
      sprintStart: card.sprintStart,
    };
    const sprintStart = currentSprint(board, team) ?? selectedDate;
    patchCard(card.itemId, { team: team ?? undefined, sprintStart });
    void provider
      .patchCard(board, card.itemId, { team: team ?? "" })
      .then(addCard)
      .catch((err: unknown) => {
        patchCard(card.itemId, prev);
        onError(errMessage(err));
      });
  };

  const handleRename = (card: CardModel, title: string) => {
    const prev = card.title;
    patchCard(card.itemId, { title });
    void provider
      .patchCard(board, card.itemId, { title })
      .then(addCard)
      .catch((err: unknown) => {
        patchCard(card.itemId, { title: prev });
        onError(errMessage(err));
      });
  };

  const handleDelete = (card: CardModel) => {
    // A just-created optimistic card has no server twin yet: drop it locally
    // (deleting it via the API would 404 and resurrect a phantom copy).
    if (card.itemId.startsWith("tmp-")) {
      cancelPendingCard(card.itemId);
      removeCard(card.itemId);
      return;
    }
    // The server cascades the delete to a linked review card; one confirm for
    // both, one request, both removed optimistically.
    const linkedReview = board.cards.find((c) => c.reviewOf === card.itemId);
    if (
      linkedReview &&
      !window.confirm(
        `Delete this card and its linked review card «${linkedReview.title}»?`,
      )
    ) {
      return;
    }
    removeCard(card.itemId);
    if (linkedReview) {
      removeCard(linkedReview.itemId);
    }
    void provider.deleteCard(board, card.itemId).catch((err: unknown) => {
      if (isGone(err)) {
        return;
      }
      addCard(card);
      if (linkedReview) {
        addCard(linkedReview);
      }
      onError(errMessage(err));
    });
  };

  // Item ids of cards that already have a linked review card (delete cascades).
  const reviewedItemIds = useMemo(() => {
    const s = new Set<string>();
    for (const c of board.cards) {
      if (c.reviewOf) {
        s.add(c.reviewOf);
      }
    }
    return s;
  }, [board.cards]);

  // Reviewer suggestions: people on the same team as the card, minus its own
  // assignee(s). The picker also accepts a free-text login.
  // Assignees of a card's linked review counterpart: an original's reviewer(s),
  // or a review card's implementer(s).
  const counterpartAssigneesFor = (card: CardModel): string[] => {
    const linked = card.reviewOf
      ? board.cards.find((c) => c.itemId === card.reviewOf)
      : board.cards.find((c) => c.reviewOf === card.itemId);
    return linked?.assignees ?? [];
  };

  // Reassign the linked review card to another person, or (login = null) delete
  // it — driven from the counterpart avatar's menu. One intent either way; the
  // server resolves the linked card itself (send-to-review reassigns when a
  // review card already exists).
  const handleSetReviewAssignee = (card: CardModel, login: string | null) => {
    const reviewCard = board.cards.find((c) => c.reviewOf === card.itemId);
    if (login === null) {
      if (!reviewCard) {
        return;
      }
      removeCard(reviewCard.itemId);
      void provider
        .removeReviewer(board, card.itemId)
        .then(addCard)
        .catch((err: unknown) => {
          addCard(reviewCard);
          onError(errMessage(err));
        });
      return;
    }
    if (!reviewCard) {
      // No review yet — assigning a reviewer sends the card to review.
      handleSendToReview(card, login);
      return;
    }
    const prev = reviewCard.assignees;
    patchCard(reviewCard.itemId, { assignees: [login] });
    void provider
      .sendToReview(board, card.itemId, login, selectedDate)
      .then(addCard)
      .catch((err: unknown) => {
        patchCard(reviewCard.itemId, { assignees: prev });
        onError(errMessage(err));
      });
  };

  // Send a card to review: one action — the server creates the linked review
  // card (in the original's zone/team) and puts the original on the review
  // stage. Both effects are mirrored optimistically; the re-list converges
  // the original's server-side state.
  const handleSendToReview = (card: CardModel, reviewerLogin: string) => {
    const team = card.team ?? null;
    const zone: ZoneKey = card.zone ?? "gray";
    const sprintStart = currentSprint(board, team) ?? selectedDate;
    const title = `review: ${card.title}`;
    const tempId = `tmp-${new Date().toISOString()}`;
    const optimistic: CardModel = {
      itemId: tempId,
      title,
      isDraft: true,
      assignees: [reviewerLogin],
      zone,
      day: selectedDate,
      startDate: selectedDate,
      sprintStart,
      team: team ?? undefined,
      reviewOf: card.itemId,
      createdAt: new Date().toISOString(),
      description: "",
      notes: [],
    };
    addCard(optimistic);
    const prevOriginal: Partial<CardModel> = {
      stage: card.stage,
      progress: card.progress,
    };
    patchCard(card.itemId, {
      stage: "review",
      // review/locked can't sit at full: the server knocks a 100% card to 90.
      ...(card.progress === 100 ? { progress: 90 } : {}),
    });
    void provider
      .sendToReview(board, card.itemId, reviewerLogin, selectedDate)
      .then((created) => {
        removeCard(tempId);
        addCard(created);
        reload();
      })
      .catch((err: unknown) => {
        removeCard(tempId);
        patchCard(card.itemId, prevOriginal);
        onError(errMessage(err));
      });
  };

  // The 4 sortable groups: one per zone, in ZONE_ORDER (top → bottom).
  const groups = useMemo<BoardGroup<MeMeta>[]>(
    () =>
      ZONE_ORDER.map((zone) => ({
        key: zone,
        meta: { zone },
        cards: byZone[zone],
      })),
    [byZone],
  );

  // Keyboard navigation over the visible day list (zone bands top to bottom):
  // arrows move the selection, Shift+arrows reorder the selected card, Escape
  // deselects. Ignored while typing in an input.
  const flatCards = useMemo(() => groups.flatMap((g) => g.cards), [groups]);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null;
      if (
        target &&
        (target.tagName === "INPUT" ||
          target.tagName === "TEXTAREA" ||
          target.isContentEditable)
      ) {
        return;
      }
      if (e.key === "Escape") {
        setSelectedCardId(null);
        return;
      }
      if (e.key !== "ArrowDown" && e.key !== "ArrowUp") {
        return;
      }
      if (flatCards.length === 0) {
        return;
      }
      e.preventDefault();
      const idx = flatCards.findIndex((c) => c.itemId === selectedCardId);
      const dir = e.key === "ArrowDown" ? 1 : -1;
      if (!e.shiftKey) {
        const next =
          idx < 0
            ? dir === 1
              ? 0
              : flatCards.length - 1
            : Math.min(Math.max(idx + dir, 0), flatCards.length - 1);
        setSelectedCardId(flatCards[next].itemId);
        return;
      }
      // Shift+arrow: reorder the selected card past its visible neighbour.
      if (idx < 0) {
        return;
      }
      const swap = idx + dir;
      if (swap < 0 || swap >= flatCards.length) {
        return;
      }
      const card = flatCards[idx];
      const afterId =
        dir === 1
          ? flatCards[swap].itemId
          : swap - 1 >= 0
            ? flatCards[swap - 1].itemId
            : null;
      const order = board.cards
        .map((c) => c.itemId)
        .filter((id) => id !== card.itemId);
      const pos = afterId ? order.indexOf(afterId) + 1 : 0;
      order.splice(pos, 0, card.itemId);
      reorderCards(order);
      void provider.moveCard(board, card.itemId, afterId).catch((err: unknown) => {
        reload();
        onError(errMessage(err));
      });
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [flatCards, selectedCardId, board, provider, reorderCards, reload, onError]);

  const handleDrop = ({ card, fromMeta, toMeta, groups: g }: DropResult<MeMeta>) => {
    const zoneChanged = fromMeta.zone !== toMeta.zone;

    // 1) Optimistic local state first.
    if (zoneChanged) {
      patchCard(card.itemId, { zone: toMeta.zone });
    }
    const order = globalOrderFromGroups(
      board,
      g.map((x) => x.ids),
    );
    reorderCards(order);

    // 2) Persist in the background; revert via reload() on any error.
    const afterId = afterIdFor(order, card.itemId);
    void (async () => {
      try {
        if (zoneChanged) {
          await provider.patchCard(board, card.itemId, { zone: toMeta.zone });
        }
        await provider.moveCard(board, card.itemId, afterId);
      } catch (err: unknown) {
        onError(errMessage(err));
        reload();
      }
    })();
  };

  // The card is scheduled for the viewed day as a one-day range; the server
  // joins the team's current sprint (recording a first sprint when the team
  // has none yet — reload picks the new pointer up in that case).
  const handleCreate = (zone: ZoneKey, title: string, team?: string | null) => {
    const tempId = `tmp-${new Date().toISOString()}`;
    const sprint = currentSprint(board, team ?? null);
    const firstSprint = sprint === null;
    const optimistic: CardModel = {
      itemId: tempId,
      title,
      isDraft: true,
      assignees: viewMe ? [viewMe] : [],
      zone,
      day: selectedDate,
      startDate: selectedDate,
      sprintStart: sprint ?? selectedDate,
      team: team ?? undefined,
      createdAt: new Date().toISOString(),
      description: "",
      notes: [],
    };
    addCard(optimistic);
    const creating = provider.createCard(board, {
      title,
      zone,
      day: selectedDate,
      start: selectedDate,
      assigneeLogin: viewMe || null,
      team: team ?? null,
    });
    registerPendingCard(
      tempId,
      creating.then((c) => c.itemId),
    );
    void creating
      .then((card) => {
        removeCard(tempId);
        if (consumePendingCancel(tempId)) {
          // Deleted while the create was in flight: drop the server twin.
          void provider.deleteCard(board, card.itemId).catch(() => undefined);
          return;
        }
        addCard(card);
        if (firstSprint) {
          reload();
        }
      })
      .catch((err: unknown) => {
        consumePendingCancel(tempId);
        removeCard(tempId);
        onError(errMessage(err));
      });
  };

  const handleAddNote = (text: string) => {
    if (!selectedCard) {
      return;
    }
    const optimistic: Note = {
      id: `tmp-${new Date().toISOString()}`,
      body: text,
      createdAt: new Date().toISOString(),
      author: viewMe || undefined,
      source: selectedCard.isDraft ? "draft" : "comment",
    };
    const uid = selectedCard.itemId;
    patchCard(uid, {
      notes: [...(selectedCard.notes ?? []), optimistic],
    });
    void provider
      .addNote(board, uid, text)
      .then((notes) => patchCard(uid, { notes }))
      .catch(fail);
  };

  const handleEditNote = (note: Note, card: CardModel, text: string) => {
    patchCard(card.itemId, {
      notes: (card.notes ?? []).map((n) =>
        n.id === note.id ? { ...n, body: text } : n,
      ),
    });
    void provider
      .editNote(board, card.itemId, note.id, text)
      .then((notes) => patchCard(card.itemId, { notes }))
      .catch(fail);
  };

  const handleDeleteNote = (note: Note, card: CardModel) => {
    patchCard(card.itemId, {
      notes: (card.notes ?? []).filter((n) => n.id !== note.id),
    });
    void provider
      .deleteNote(board, card.itemId, note.id)
      .then((notes) => patchCard(card.itemId, { notes }))
      .catch(fail);
  };

  return (
    <div className="me">
      <div className="board-toolbar">
        <div className="field field-inline">
          <span>Day</span>
          <div className="day-nav">
            <button
              type="button"
              className="day-arrow"
              onClick={() => setSelectedDate((d) => addDays(d, -1))}
              aria-label="Previous day"
              title="Previous day"
            >
              ‹
            </button>
            <input
              type="date"
              value={selectedDate}
              onChange={(e) => setSelectedDate(e.target.value || todayIso())}
            />
            <button
              type="button"
              className="day-arrow"
              onClick={() => setSelectedDate((d) => addDays(d, 1))}
              aria-label="Next day"
              title="Next day"
            >
              ›
            </button>
          </div>
        </div>

        <TeamChips
          label="Team"
          teams={teams}
          selectedKeys={teamFilter}
          onSelect={onSetFilter}
          onAdd={onAddTeam}
          onRemove={onRemoveTeam}
          onRename={onRenameTeam}
          canManage={false}
          noTeamChip
          filterToggle={{ on: teamFocus, onToggle: () => setTeamFocus((v) => !v) }}
          focusToggle={{ on: focus, onToggle: () => setFocus((v) => !v) }}
        />

        <button
          type="button"
          className="btn"
          onClick={() => setSelectedDate(todayIso())}
          disabled={selectedDate === todayIso()}
          title="Jump to today"
        >
          Today
        </button>

        <div className="field field-inline impersonate" ref={impRef}>
          <button
            type="button"
            className={`btn${impersonated ? " impersonate-active" : ""}`}
            onClick={() => setImpOpen((o) => !o)}
            title="View the board as another person"
          >
            {impersonated
              ? `👁 ${displayName(impersonated, users[impersonated])}`
              : "View as ▾"}
          </button>
          {impersonated && (
            <button
              type="button"
              className="impersonate-reset"
              onClick={() => setImpersonated(null)}
              title="Back to me"
            >
              ×
            </button>
          )}
          <Dropdown
            open={impOpen}
            anchorRef={impRef}
            onClose={() => setImpOpen(false)}
            className="card-stage-menu"
          >
            {impersonated && (
              <button
                type="button"
                className="card-stage-item card-stage-clear"
                onClick={() => {
                  setImpersonated(null);
                  setImpOpen(false);
                }}
              >
                ← Back to me
              </button>
            )}
            {others.map((p) => (
              <button
                key={p}
                type="button"
                className="card-stage-item"
                onClick={() => {
                  setImpersonated(p);
                  setImpOpen(false);
                }}
              >
                <img
                  className="avatar-img"
                  src={avatarUrlFor(p, users[p])}
                  alt=""
                  draggable={false}
                />
                {displayName(p, users[p])}
              </button>
            ))}
            {others.length === 0 && (
              <div className="sprint-empty">No other people with cards</div>
            )}
          </Dropdown>
        </div>
      </div>

      <div className="me-panes">
        <div className="me-left">
          <div className="me-zones">
            <SortableBoard<MeMeta>
              groups={groups}
              onDrop={handleDrop}
              renderCard={(card) => (
                <Card
                  card={card}
                  onLoadLinks={loadCardLinks}
                  selected={card.itemId === selectedCardId}
                  onSelect={(c) => setSelectedCardId(c.itemId)}
                  onProgress={handleProgress}
                  onDelete={handleDelete}
                  onStage={handleStage}
                  onInProgress={handleInProgress}
                  onRename={handleRename}
                  onOpen={onOpen}
                  teams={teams}
                  people={people}
                  users={users}
                  onSetTeam={handleSetTeam}
                  hasLinkedReview={reviewedItemIds.has(card.itemId)}
                  counterpartAssignees={counterpartAssigneesFor(card)}
                  onSetReviewAssignee={handleSetReviewAssignee}
                  asOf={selectedDate}
                  dimAvatar={
                    teamFilter === null || !teamFilter.includes(card.team ?? "")
                  }
                />
              )}
              renderOverlay={(card) => (
                <Card
                  card={card}
                  onLoadLinks={loadCardLinks}
                  selected={false}
                  onSelect={() => {}}
                  onProgress={() => {}}
                  onDelete={() => {}}
                  onStage={() => {}}
                  onInProgress={() => {}}
                  onRename={() => {}}
                  onOpen={() => {}}
                />
              )}
              renderGroup={(group, body, { isOver, dropRef }) => {
                const def = ZONES[group.meta.zone];
                return (
                  <section
                    key={group.key}
                    ref={dropRef as Ref<HTMLElement>}
                    className={`zone-area${isOver ? " zone-area-dragover" : ""}`}
                    style={{ background: def.background, borderLeftColor: def.accent }}
                  >
                    <span className="zone-spine" style={{ color: def.accent }}>
                      {def.spine}
                    </span>
                    <div className="zone-cards">
                      {body}
                      <AddCard
                        forcedTeam={
                          teamFilter?.length === 1
                            ? teamFilter[0] || null
                            : undefined
                        }
                        teams={teams}
                        onCreate={(title, team) =>
                          handleCreate(group.meta.zone, title, team)
                        }
                      />
                    </div>
                  </section>
                );
              }}
            />
          </div>
        </div>

        <NotesPanel
          selectedDate={selectedDate}
          notes={dayNotes}
          cardOrder={noteCardOrder}
          selectedCard={selectedCard}
          onSelectCard={(c) => setSelectedCardId(c.itemId)}
          onAddNote={handleAddNote}
          onEditNote={handleEditNote}
          onDeleteNote={handleDeleteNote}
          collapsed={notesCollapsed}
          onToggleCollapse={() => setNotesCollapsed((c) => !c)}
        />
      </div>
      <div className="me-day-progress" title={`${dayProgress}% done today`}>
        <div
          className="me-day-progress-fill"
          style={{ width: `${dayProgress}%` }}
        />
      </div>
      <div className="me-day-stats">
        <span className="me-day-stat">
          total: {dayStats.total.done}/{dayStats.total.total}
        </span>
        <span className="me-day-stat">
          urgent: {dayStats.red.done}/{dayStats.red.total}
        </span>
        <span className="me-day-stat">
          unplanned: {dayStats.yellow.done}/{dayStats.yellow.total}
        </span>
        <span className="me-day-stat">
          planned: {dayStats.gray.done}/{dayStats.gray.total}
        </span>
        <span className="me-day-stat">
          nice to have: {dayStats.green.done}/{dayStats.green.total}
        </span>
        <button
          type="button"
          className="connect-link"
          onClick={() => setConnectOpen(true)}
        >
          MCP / API
        </button>
      </div>
      {connectOpen && <ConnectDialog onClose={() => setConnectOpen(false)} />}
    </div>
  );
}
