import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
  type Ref,
} from "react";
import {
  cancelPendingCard,
  consumePendingCancel,
  registerPendingCard,
} from "../api/pending";
import { isWorkable } from "../stages";
import { mergeNotes, sameNotes } from "../notes";
import type {
  Board,
  Card as CardModel,
  CardPatch,
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
import { NotesPanel, type DayEvent, type DayNote } from "./NotesPanel";
import { ConnectDialog } from "./ConnectDialog";
import { SortableBoard, type BoardGroup, type DropResult } from "./SortableBoard";
import { globalOrderFromGroups, afterIdFor } from "./dndOrder";

interface MeBoardProps {
  board: Board;
  provider: Provider;
  me: string;
  /** Viewed day, owned by the App (drives the lazy view fetch + scoped watch). */
  selectedDate: string;
  onSelectDate: (day: string) => void;
  /** "View as" impersonation, owned by the App: the Me fetch carries it as an
   *  explicit user (null = the caller themselves). */
  viewAs: string | null;
  onViewAs: (login: string | null) => void;
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
  patchCard: (
    itemId: string,
    patch: Partial<CardModel> | ((c: CardModel) => Partial<CardModel>),
  ) => void;
  addCard: (card: CardModel) => void;
  /** Swap an optimistic card for its server twin in place (keeps the slot). */
  replaceCard: (itemId: string, card: CardModel) => void;
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

// The notes pane folds differently per width mode (stacked under the board when
// narrow, a side pane when wide), so its collapsed choice is remembered per
// mode. With nothing stored, the default is collapsed when narrow, open when
// wide.
const NOTES_STACK_MQ = "(max-width: 820px)";
const notesCollapsedKey = (narrow: boolean) =>
  `aeman.notesCollapsed.${narrow ? "narrow" : "wide"}`;
const storedNotesCollapsed = (narrow: boolean): boolean => {
  const v = localStorage.getItem(notesCollapsedKey(narrow));
  return v === null ? narrow : v === "1";
};

// isGone reports a "card not found" failure: the card no longer exists on the
// server, so an optimistic removal must NOT be rolled back (re-adding it would
// resurrect a phantom copy).
const isGone = (err: unknown) => errMessage(err).includes("card not found");

// isComplete mirrors board.Complete: an explicit done, or 100% with no stage
// (or recurrent).
const isComplete = (c: CardModel) =>
  c.stage === "done" ||
  ((!c.stage || c.stage === "recurrent") && (c.progress ?? 0) >= 100);

/** MeBoard is the personal day view: my cards stacked in zone bands + notes. */
export function MeBoard({
  board,
  provider,
  me,
  selectedDate,
  onSelectDate,
  viewAs,
  onViewAs,
  users,
  teams,
  teamFilter,
  onSetFilter,
  onAddTeam,
  onRemoveTeam,
  onRenameTeam,
  patchCard,
  addCard,
  replaceCard,
  removeCard,
  reorderCards,
  reload,
  onError,
  onOpen,
  onPresence,
}: MeBoardProps) {
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
  // Subtask UI state: manually expanded parents, the card a drag would group
  // under (its middle band is hovered — the card highlights), and the parent
  // whose add-subtask form is open (the + button flow).
  const [expandedSubs, setExpandedSubs] = useState<Set<string>>(new Set());
  const [groupHover, setGroupHover] = useState<string | null>(null);
  const [addingSub, setAddingSub] = useState<string | null>(null);
  // Impersonate: view (and act on) the board as another person.
  const impersonated = viewAs;
  const setImpersonated = onViewAs;
  const [impOpen, setImpOpen] = useState(false);
  // MCP / API connect dialog.
  const [connectOpen, setConnectOpen] = useState(false);

  // Notes fold to a header bar on narrow screens (like the Team weekly plan)
  // and to a slim strip on wide ones. The collapsed choice persists per width
  // mode; crossing the breakpoint restores that mode's remembered (or default)
  // state. The breakpoint matches .me-panes.
  const [notesCollapsed, setNotesCollapsed] = useState(() =>
    storedNotesCollapsed(window.matchMedia(NOTES_STACK_MQ).matches),
  );
  useEffect(() => {
    const mq = window.matchMedia(NOTES_STACK_MQ);
    const onChange = (e: MediaQueryListEvent) =>
      setNotesCollapsed(storedNotesCollapsed(e.matches));
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, []);
  const toggleNotesCollapsed = useCallback(() => {
    const narrow = window.matchMedia(NOTES_STACK_MQ).matches;
    setNotesCollapsed((c) => {
      const next = !c;
      localStorage.setItem(notesCollapsedKey(narrow), next ? "1" : "0");
      return next;
    });
  }, []);
  const impRef = useRef<HTMLDivElement | null>(null);
  const viewMe = impersonated ?? me;
  // Other people with cards — offered in the "View as" impersonate picker.
  const others = useMemo(
    () =>
      [...new Set([...board.members, ...board.cards.flatMap((c) => c.assignees)])]
        .filter((p) => p && p !== me)
        .sort(),
    [board.members, board.cards, me],
  );

  // People to offer when picking a reviewer: everyone seen on the board, plus
  // me — the same roster the Team board's assign menu uses.
  const people = useMemo(() => {
    const set = new Set<string>(board.members);
    for (const card of board.cards) {
      for (const login of card.assignees) {
        set.add(login);
      }
    }
    if (me) {
      set.add(me);
    }
    set.delete("");
    return [...set].sort((a, b) => a.localeCompare(b));
  }, [board.members, board.cards, me]);

  // My cards (any day); when me is empty, everyone's. A parent counts as mine
  // when any of its subtasks is mine, and a delivered parent brings all its
  // subtasks along (mirrors MeView + withSubtasks server-side).
  const mine = useMemo(() => {
    if (!viewMe) {
      return board.cards;
    }
    const owned = new Set<string>();
    for (const c of board.cards) {
      if (c.assignees.includes(viewMe)) {
        owned.add(c.parent ?? c.itemId);
      }
    }
    return board.cards.filter((c) => owned.has(c.parent ?? c.itemId));
  }, [board.cards, viewMe]);

  // In Me a card shows when it belongs to the sprint that was active on the viewed
  // day (activeSprint) and its scheduled day has arrived (startDate empty or on or
  // before the viewed day). Today shows the current sprint; rolling back into the
  // previous sprint's days shows that sprint's cards. A team with no active sprint
  // on the day, or a card deferred to the future, never shows.
  const myCards = useMemo(
    () =>
      mine.filter((c) => {
        // Subtasks render nested under their parent, never as zone rows.
        if (c.parent) {
          return false;
        }
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
        // A sprint-less day card (a "next sprint" create) stays visible from
        // its scheduled day on — the sprint gate below would otherwise hide it
        // right when its day arrives, until a carry-over adopts it. Only cards
        // scheduled into the sprint active on the viewed day (or later)
        // qualify: an old sprint-less stray stays on its own past days.
        if (
          !c.sprintStart &&
          !c.plan &&
          c.startDate &&
          c.startDate <= selectedDate &&
          c.startDate >= as
        ) {
          return true;
        }
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

  // The day's recorded activity events, feeding the same panel as the notes.
  const dayEvents = useMemo<DayEvent[]>(() => {
    const out: DayEvent[] = [];
    for (const card of myCards) {
      for (const event of card.events ?? []) {
        if (localDateIso(event.at) === selectedDate) {
          out.push({ event, card });
        }
      }
    }
    out.sort((a, b) => a.event.at.localeCompare(b.event.at));
    return out;
  }, [myCards, selectedDate]);

  // Notes and events live in the log subresource, not on the Card resource:
  // lazily load them for the day's visible cards. A card's loaded notes are the
  // "fetched" marker (mutations and the watch keep them fresh); a re-list
  // clears them, so they refetch. A failed request stays marked, not retried.
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
        .listLog(board, c.itemId)
        .then(({ notes, events }) => {
          notesRequested.current.delete(c.itemId);
          patchCard(c.itemId, { notes, events });
        })
        .catch(() => {});
    }
  }, [myCards, board, provider, patchCard]);

  // Refresh a card's log after an own mutation: our watch echo is suppressed,
  // so the freshly recorded event won't stream back — refetch it.
  const refreshLog = (itemId: string) => {
    if (itemId.startsWith("tmp-")) {
      return;
    }
    void provider
      .listLog(board, itemId)
      .then(({ notes, events }) => patchCard(itemId, { notes, events }))
      .catch(() => {});
  };

  // Subtasks are selectable rows too but never appear in myCards: fall back
  // to the full board state so the notes composer works on them.
  const selectedCard =
    myCards.find((c) => c.itemId === selectedCardId) ??
    board.cards.find((c) => c.itemId === selectedCardId && c.parent) ??
    null;

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

  // Subtasks grouped by parent. The Me board's team-focus filter applies to
  // subtask rows the same way it applies to cards.
  // Mirrors the server's subtaskOnDay: a subtask keeps its own day
  // visibility even though it rides its parent — deferred to the future it
  // hides until its day, and one left behind in an earlier sprint stays on
  // that sprint's days. Without this an acked defer (addCard) would put the
  // row right back under the parent on today's board.
  const subtaskOnDay = (c: CardModel): boolean => {
    const today = todayIso();
    if (c.startDate && c.startDate > today && selectedDate < c.startDate) {
      return false;
    }
    if (c.sprintStart) {
      const as = activeSprint(board, c.team ?? null, selectedDate);
      if (as && as > c.sprintStart) {
        return false;
      }
    }
    return true;
  };

  const childrenOf = useMemo(() => {
    const m = new Map<string, CardModel[]>();
    for (const c of mine) {
      if (!c.parent || !subtaskOnDay(c)) {
        continue;
      }
      if (teamFocus && teamFilter && !teamFilter.includes(c.team ?? "")) {
        continue;
      }
      const list = m.get(c.parent) ?? [];
      list.push(c);
      m.set(c.parent, list);
    }
    return m;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mine, teamFocus, teamFilter, selectedDate, board]);

  const subsOpen = (id: string) => expandedSubs.has(id);

  // A parent dragged with its list open folds for the flight (the whole
  // block moving with the ghost is unwieldy) and unfolds where it lands.
  const foldedForDrag = useRef<string | null>(null);
  const handleDragActive = (id: string | null) => {
    if (id && expandedSubs.has(id)) {
      foldedForDrag.current = id;
      setExpandedSubs((cur) => {
        const next = new Set(cur);
        next.delete(id);
        return next;
      });
    } else if (id === null && foldedForDrag.current) {
      const back = foldedForDrag.current;
      foldedForDrag.current = null;
      setExpandedSubs((cur) => new Set(cur).add(back));
    }
  };

  // A create ack swaps the optimistic tmp id for the real one; UI state keyed
  // by the tmp id (selection, expanded lists, an open add-subtask form) must
  // follow, or the + flow collapses mid-typing.
  const migrateCardId = (tempId: string, realId: string) => {
    setSelectedCardId((cur) => (cur === tempId ? realId : cur));
    setExpandedSubs((cur) => {
      if (!cur.has(tempId)) {
        return cur;
      }
      const next = new Set(cur);
      next.delete(tempId);
      next.add(realId);
      return next;
    });
    setAddingSub((cur) => (cur === tempId ? realId : cur));
  };


  // A drag parked on a collapsed parent unfolds it so the drop target is
  // visible; when the drag leaves, it folds back (manual expands stay).
  const autoExpanded = useRef<string | null>(null);
  useEffect(() => {
    const id = groupHover;
    const prev = autoExpanded.current;
    if (prev && prev !== id) {
      autoExpanded.current = null;
      setExpandedSubs((cur) => {
        const next = new Set(cur);
        next.delete(prev);
        return next;
      });
    }
    if (id && (childrenOf.get(id) ?? []).length > 0 && !expandedSubs.has(id)) {
      autoExpanded.current = id;
      setExpandedSubs((cur) => new Set(cur).add(id));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [groupHover]);


  const toggleSubs = (id: string) =>
    setExpandedSubs((cur) => {
      const next = new Set(cur);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });

  // A drag dwelling over a card opens its subtask drop area; leaving closes it.
  // A card may take the dragged card as a subtask unless depth would exceed
  // one level (the target is already a subtask, or the dragged card has its
  // own subtasks) — the middle-band group drop only offers valid targets.
  const canGroup = (active: CardModel, target: CardModel) =>
    !target.parent &&
    target.itemId !== active.parent &&
    !(childrenOf.get(active.itemId) ?? []).length;

  // itemId → card, for resolving a drop's neighbours.
  const cardsById = useMemo(
    () => new Map(board.cards.map((c) => [c.itemId, c])),
    [board.cards],
  );

  // Space expands/collapses the selected card's subtask list.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== " " || !selectedCardId) {
        return;
      }
      const t = e.target as HTMLElement;
      if (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable) {
        return;
      }
      if (!(childrenOf.get(selectedCardId) ?? []).length) {
        return;
      }
      e.preventDefault();
      toggleSubs(selectedCardId);
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedCardId, childrenOf]);

  // Group a dropped card as a subtask; the server enforces depth and moves a
  // weekly card's plan slot onto the parent (which replaces it in the plan).
  const handleGroup = (card: CardModel, parentId: string) => {
    if (card.itemId === parentId || card.parent === parentId) {
      return;
    }
    const prev: Partial<CardModel> = { parent: card.parent, plan: card.plan, week: card.week };
    patchCard(card.itemId, { parent: parentId, plan: undefined, week: undefined });
    autoExpanded.current = null; // the drop keeps the target unfolded
    setExpandedSubs((cur) => new Set(cur).add(parentId));
    void provider
      .patchCard(board, card.itemId, { parent: parentId })
      .then((c) => {
        addCard(c);
        reload();
      })
      .catch((err: unknown) => {
        patchCard(card.itemId, prev);
        onError(errMessage(err));
      });
  };

  // Create a card directly as a subtask (inherits the parent's team and zone,
  // assigned to the viewer so it stays on this board). An optimistic copy
  // shows at the end of the list instantly; the server card replaces it.
  const handleCreateSubtask = (parent: CardModel, title: string) => {
    const tempId = `tmp-${new Date().toISOString()}`;
    addCard({
      itemId: tempId,
      title,
      isDraft: true,
      assignees: viewMe ? [viewMe] : [],
      zone: parent.zone ?? "gray",
      team: parent.team,
      parent: parent.itemId,
      startDate: selectedDate,
      day: selectedDate,
      sprintStart: parent.sprintStart,
      progress: 0,
    });
    // The parent's derived bar counts the newcomer at 0% right away
    // (mirrors the server's syncParentProgress — including reopening a
    // complete parent, since the fresh subtask is open work).
    const kids = board.cards.filter((c) => c.parent === parent.itemId);
    const sum = kids.reduce(
      (acc, k) => acc + (isComplete(k) ? 100 : k.progress ?? 0),
      0,
    );
    const derived = Math.floor((sum * 90) / ((kids.length + 1) * 100));
    const reopen = isComplete(parent);
    if (derived !== (parent.progress ?? 0) || reopen) {
      patchCard(parent.itemId, {
        progress: derived,
        ...(reopen ? { stage: undefined } : {}),
      });
    }

    const created = provider.createCard(board, {
      title,
      team: parent.team ?? null,
      zone: parent.zone ?? "gray",
      start: selectedDate,
      day: selectedDate,
        assigneeLogin: viewMe || null,
      parent: parent.itemId,
    });
    // Mutations fired against the tmp id (a quick reorder, a rename) wait in
    // the pending registry for the real uid instead of erroring out.
    registerPendingCard(tempId, created.then((c) => c.itemId));
    created
      .then((c) => {
        if (consumePendingCancel(tempId)) {
          removeCard(tempId);
          void provider.deleteCard(board, c.itemId).catch(() => {});
          return;
        }
        replaceCard(tempId, c);
        migrateCardId(tempId, c.itemId);
      })
      .catch((err: unknown) => {
        removeCard(tempId);
        onError(errMessage(err));
      });
  };

  // Mirror the server's derived-progress rule (board.DerivedProgress) so a
  // parent's bar moves the instant a subtask's does; the server converges it.
  const syncParentBar = (child: CardModel, childValue: number) => {
    if (!child.parent) {
      return;
    }
    const parent = cardsById.get(child.parent);
    if (!parent || isComplete(parent)) {
      return;
    }
    const kids = board.cards.filter((c) => c.parent === child.parent);
    if (kids.length === 0) {
      return;
    }
    const sum = kids.reduce(
      (acc, k) =>
        acc +
        (k.itemId === child.itemId
          ? childValue
          : isComplete(k)
            ? 100
            : k.progress ?? 0),
      0,
    );
    const derived = Math.floor((sum * 90) / (kids.length * 100));
    if (derived !== (parent.progress ?? 0)) {
      patchCard(parent.itemId, { progress: derived });
    }
  };

  const handleProgress = (card: CardModel, raw: number) => {
    let value =
      card.stage === "review" || card.stage === "locked"
        ? Math.min(90, Math.max(10, raw))
        : raw;
    // A parent's bar cannot be dragged to done while subtasks are open (the
    // server guard); it tops out at 90 until every subtask is closed.
    if (
      value >= 100 &&
      board.cards.some((k) => k.parent === card.itemId && !isComplete(k))
    ) {
      value = 90;
    }
    const prev: Partial<CardModel> = { progress: card.progress, stage: card.stage };
    const patch: Partial<CardModel> = { progress: value };
    if (value < 100 && card.stage === "done") {
      patch.stage = undefined;
    }
    patchCard(card.itemId, patch);
    syncParentBar(card, value);
    void provider
      .patchCard(board, card.itemId, { progress: value })
      .then((updated) => {
        addCard(updated);
        refreshLog(card.itemId);
        // A review card's progress drives the original's stage server-side,
        // and a subtask's drives its parent's derived bar.
        if (card.reviewOf || card.parent) {
          reload();
        }
      })
      .catch((err: unknown) => {
        patchCard(card.itemId, prev);
        if (card.parent) {
          reload(); // the parent's optimistic derived bar needs the truth back
        }
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
    // Done on a recurrent card completes the iteration but keeps the
    // recurrence (mirrors the server): only the progress fills.
    const patch: Partial<CardModel> = {
      stage:
        stage === "done"
          ? card.stage === "recurrent"
            ? "recurrent"
            : undefined
          : stage ?? undefined,
    };
    if (stage === "done") {
      patch.progress = 100;
    }
    if (stage === "review" || stage === "locked") {
      // The 10-90 clamp is stored on stage pick (mirrors board.ApplyStage).
      patch.progress = Math.min(90, Math.max(10, card.progress ?? 0));
    }
    patchCard(card.itemId, patch);
    syncParentBar(card, stage === "done" ? 100 : patch.progress ?? card.progress ?? 0);
    const leavingReview = card.stage === "review" && stage !== "review";
    // Entering review re-review reactivates a completed linked review card
    // server-side (progress → 0, round bumped); re-list so it converges.
    const enteringReview = stage === "review" && card.stage !== "review";
    const hasLinkedReview = board.cards.some((c) => c.reviewOf === card.itemId);
    void provider
      .patchCard(board, card.itemId, { stage: stage ?? "" })
      .then((updated) => {
        addCard(updated);
        refreshLog(card.itemId);
        if (
          leavingReview ||
          (enteringReview && hasLinkedReview) ||
          card.reviewOf ||
          card.parent
        ) {
          reload();
        }
      })
      .catch((err: unknown) => {
        patchCard(card.itemId, prev);
        if (card.parent) {
          reload(); // the parent's optimistic derived bar needs the truth back
        }
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
    syncParentBar(card, value);
    void provider
      .setInProgress(board, card.itemId)
      .then((updated) => {
        addCard(updated);
        refreshLog(card.itemId);
        if (card.stage === "review" || card.reviewOf || card.parent) {
          reload();
        }
      })
      .catch((err: unknown) => {
        patchCard(card.itemId, prev);
        if (card.parent) {
          reload(); // the parent's optimistic derived bar needs the truth back
        }
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
    // The server releases the subtasks (they return standalone, sliding into
    // the parent's slot); mirror both locally while the delete round-trips.
    const freed = board.cards.filter((c) => c.parent === card.itemId);
    for (const c of freed) {
      patchCard(c.itemId, { parent: undefined });
    }
    if (freed.length > 0) {
      const freedIds = new Set(freed.map((c) => c.itemId));
      const order: string[] = [];
      for (const c of board.cards) {
        if (freedIds.has(c.itemId)) {
          continue;
        }
        if (c.itemId === card.itemId) {
          order.push(...freed.map((f) => f.itemId));
          continue;
        }
        order.push(c.itemId);
      }
      reorderCards(order);
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
      .sendToReview(board, card.itemId, login, selectedDate, "yellow")
      .then((updated) => {
        addCard(updated);
        // Re-sending a passed card to the same reviewer reactivates their
        // review card server-side (progress reset to 0, round bumped, the
        // original put back on review). Those effects touch more than the
        // returned card, so re-list to converge them in the UI.
        reload();
      })
      .catch((err: unknown) => {
        patchCard(reviewCard.itemId, { assignees: prev });
        onError(errMessage(err));
      });
  };

  // Send a card to review: one action — the server creates the linked review
  // card and puts the original on the review stage. From the Me board the
  // review card lands in the reviewer's unplanned zone: for them it popped up
  // during the day. Both effects are mirrored optimistically; the re-list
  // converges the original's server-side state.
  const handleSendToReview = (card: CardModel, reviewerLogin: string) => {
    const team = card.team ?? null;
    const zone: ZoneKey = "yellow";
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
      .sendToReview(board, card.itemId, reviewerLogin, selectedDate, zone)
      .then((created) => {
        replaceCard(tempId, created);
        migrateCardId(tempId, created.itemId);
        reload();
      })
      .catch((err: unknown) => {
        removeCard(tempId);
        patchCard(card.itemId, prevOriginal);
        onError(errMessage(err));
      });
  };

  // The 4 sortable groups: one per zone, in ZONE_ORDER (top → bottom).
  // An expanded parent's subtasks follow it as indented rows of the same zone
  // band (a subtask has no zone placement of its own).
  const groups = useMemo<BoardGroup<MeMeta>[]>(
    () =>
      ZONE_ORDER.map((zone) => ({
        key: zone,
        meta: { zone },
        cards: byZone[zone].flatMap((c) =>
          subsOpen(c.itemId) ? [c, ...(childrenOf.get(c.itemId) ?? [])] : [c],
        ),
      })),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [byZone, childrenOf, expandedSubs],
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

  const handleDrop = ({ card, toMeta, groups: g, groupUnder }: DropResult<MeMeta>) => {
    // The board committed exactly what the placeholder previewed: groupUnder
    // is the already-validated parent to nest under (null = standalone).
    const parentTo = groupUnder;
    const parentChanged = (card.parent ?? "") !== (parentTo ?? "");

    // 1) Optimistic local state first.
    const optimistic: Partial<CardModel> = {};
    const patch: CardPatch = {};
    if (parentChanged) {
      optimistic.parent = parentTo ?? undefined;
      patch.parent = parentTo ?? "";
      if (parentTo) {
        optimistic.plan = undefined;
        optimistic.week = undefined;
        autoExpanded.current = null; // the drop keeps the target unfolded
        setExpandedSubs((cur) => new Set(cur).add(parentTo as string));
      }
    }
    if (!parentTo && (card.zone ?? "gray") !== toMeta.zone) {
      optimistic.zone = toMeta.zone;
      patch.zone = toMeta.zone;
    }
    if (Object.keys(optimistic).length > 0) {
      patchCard(card.itemId, optimistic);
    }
    const order = globalOrderFromGroups(
      board,
      g.map((x) => x.ids),
    );
    reorderCards(order);

    // 2) Persist in the background; revert via reload() on any error. The
    // move anchors on a neighbour from the card's OWN group: the flattened
    // cross-group order is a view artefact, and a card landing first in its
    // group would otherwise anchor on another group's tail — a card whose
    // global position may be past the visible neighbour below.
    const entryIds = g.find((x) => x.ids.includes(card.itemId))?.ids ?? [];
    const at = entryIds.indexOf(card.itemId);
    const afterId = afterIdFor(order, card.itemId);
    void (async () => {
      try {
        if (Object.keys(patch).length > 0) {
          await provider.patchCard(board, card.itemId, patch);
        }
        if (at > 0) {
          await provider.moveCard(board, card.itemId, entryIds[at - 1]);
        } else if (at === 0 && entryIds.length > 1) {
          await provider.moveCardBefore(board, card.itemId, entryIds[1]);
        } else {
          await provider.moveCard(board, card.itemId, afterId);
        }
        if (parentChanged) {
          reload();
        }
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
        if (consumePendingCancel(tempId)) {
          removeCard(tempId);
          // Deleted while the create was in flight: drop the server twin.
          void provider.deleteCard(board, card.itemId).catch(() => undefined);
          return;
        }
        // Swap in place: append-on-ack would reshuffle a quick burst of adds.
        replaceCard(tempId, card);
        migrateCardId(tempId, card.itemId);
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
    // Functional updates: rapid Enter-Enter-Enter adds must each build on the
    // card's CURRENT notes, not on this render's stale copy. The server's
    // response list is merged the same way — it may predate optimistic notes
    // added after this request left, and must not wipe them.
    patchCard(uid, (c) => ({
      notes: [...(c.notes ?? []), optimistic],
    }));
    void provider
      .addNote(board, uid, text)
      .then((notes) =>
        patchCard(uid, (c) => { const merged = mergeNotes(notes, c.notes); return sameNotes(c.notes, merged) ? {} : { notes: merged }; }),
      )
      .catch(fail);
  };

  const handleEditNote = (note: Note, card: CardModel, text: string) => {
    patchCard(card.itemId, (c) => ({
      notes: (c.notes ?? []).map((n) =>
        n.id === note.id ? { ...n, body: text } : n,
      ),
    }));
    void provider
      .editNote(board, card.itemId, note.id, text)
      .then((notes) =>
        patchCard(card.itemId, (c) => { const merged = mergeNotes(notes, c.notes); return sameNotes(c.notes, merged) ? {} : { notes: merged }; }),
      )
      .catch(fail);
  };

  const handleDeleteNote = (note: Note, card: CardModel) => {
    patchCard(card.itemId, (c) => ({
      notes: (c.notes ?? []).filter((n) => n.id !== note.id),
    }));
    void provider
      .deleteNote(board, card.itemId, note.id)
      .then((notes) =>
        patchCard(card.itemId, (c) => { const merged = mergeNotes(notes, c.notes); return sameNotes(c.notes, merged) ? {} : { notes: merged }; }),
      )
      .catch(fail);
  };

  // Saves the Card pane's in-place description edit. The description
  // live-syncs with the linked counterpart (original <-> review card)
  // server-side; mirror it locally, since our own watch echo is suppressed
  // (same as CardDetail's save).
  const handleSetDescription = (card: CardModel, text: string) => {
    const counterpart = board.cards.find((c) =>
      card.reviewOf ? c.itemId === card.reviewOf : c.reviewOf === card.itemId,
    );
    patchCard(card.itemId, { description: text });
    if (counterpart) {
      patchCard(counterpart.itemId, { description: text });
    }
    void provider
      .patchCard(board, card.itemId, { description: text })
      .catch((err: unknown) => {
        patchCard(card.itemId, { description: card.description ?? "" });
        if (counterpart) {
          patchCard(counterpart.itemId, {
            description: counterpart.description ?? "",
          });
        }
        fail(err);
      });
  };

  // renderMeCard is the zone-band card used both for top-level rows and for
  // subtask rows (a subtask works exactly like any card).
  const renderMeCard = (card: CardModel): ReactNode => (
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
      dimAvatar={teamFilter === null || !teamFilter.includes(card.team ?? "")}
      subCount={(childrenOf.get(card.itemId) ?? []).length}
      expanded={subsOpen(card.itemId)}
      onToggleExpand={(c) => toggleSubs(c.itemId)}
      onAddSubtask={(c) => {
        setExpandedSubs((cur) => new Set(cur).add(c.itemId));
        setAddingSub(c.itemId);
      }}
      groupTarget={groupHover === card.itemId}
    />
  );

  // The inline add-subtask form (the + flow), indented like a subtask row.
  const subtaskAddForm = (parent: CardModel): ReactNode => (
    <div className="subtask-add" onPointerDown={(e) => e.stopPropagation()}>
      <AddCard
        autoOpen
        placeholder="Add a subtask…"
        onCreate={(title) => handleCreateSubtask(parent, title)}
        onClosed={() => setAddingSub(null)}
      />
    </div>
  );

  // Wraps a rendered card: subtask rows are indented under their parent, and
  // the add-subtask form (the + flow) hangs after the LAST subtask row — or
  // right under the parent while it has none visible yet.
  const withSubs = (card: CardModel, node: ReactNode): ReactNode => {
    if (card.parent) {
      const wrapped = <div className="subtask-indent">{node}</div>;
      const subs = childrenOf.get(card.parent) ?? [];
      const parent = cardsById.get(card.parent);
      if (
        addingSub === card.parent &&
        parent &&
        subs[subs.length - 1]?.itemId === card.itemId
      ) {
        return (
          <>
            {wrapped}
            {subtaskAddForm(parent)}
          </>
        );
      }
      return wrapped;
    }
    if (
      addingSub !== card.itemId ||
      (subsOpen(card.itemId) && (childrenOf.get(card.itemId) ?? []).length > 0)
    ) {
      return node;
    }
    return (
      <>
        {node}
        {subtaskAddForm(card)}
      </>
    );
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
              onClick={() => onSelectDate(addDays(selectedDate, -1))}
              aria-label="Previous day"
              title="Previous day"
            >
              ‹
            </button>
            <input
              type="date"
              value={selectedDate}
              onChange={(e) => onSelectDate(e.target.value || todayIso())}
            />
            <button
              type="button"
              className="day-arrow"
              onClick={() => onSelectDate(addDays(selectedDate, 1))}
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
          className="btn me-today"
          onClick={() => onSelectDate(todayIso())}
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
              onGroupDrop={handleGroup}
              onHoverCard={setGroupHover}
              onDragActiveCard={handleDragActive}
              canGroup={canGroup}
              renderCard={(card) => withSubs(card, renderMeCard(card))}
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
                    style={
                      {
                        background: def.background,
                        borderLeftColor: def.accent,
                        "--zone-accent": def.accent,
                      } as CSSProperties
                    }
                  >
                    <span className="zone-spine">{def.spine}</span>
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
          events={dayEvents}
          cardOrder={noteCardOrder}
          selectedCard={selectedCard}
          onSelectCard={(c) => setSelectedCardId(c.itemId)}
          onAddNote={handleAddNote}
          onEditNote={handleEditNote}
          onDeleteNote={handleDeleteNote}
          onSetDescription={handleSetDescription}
          collapsed={notesCollapsed}
          onToggleCollapse={toggleNotesCollapsed}
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
