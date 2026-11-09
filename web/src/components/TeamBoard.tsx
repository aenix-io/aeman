import { optimisticTitle } from "../links";
import { Fragment, type CSSProperties, type ReactNode, type Ref, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  cancelPendingCard,
  consumePendingCancel,
  registerPendingCard,
} from "../api/pending";
import { justMade } from "../justmade";
import type { Board, Card as CardModel, CardPatch, CarryReport, Provider, SprintState, StageKey, ZoneKey } from "../providers/types";
import { ZONES, ZONE_ORDER } from "../zones";
import { clampProgress, clampsProgress, isComplete } from "../stages";
import { inHandOn } from "../teamgrid";
import { todayIso, addDays, localDateIso } from "../date";
import { currentSprint, dayIsOverFor, previousSprint, sprintForDate } from "../sprint";
import { teamColor } from "../avatar";
import { displayName, type Avatars, type Names } from "../users";
import { Avatar } from "./Avatar";
import { cardDomainBadge, reviewerCandidates } from "../domains";
import { Card } from "./Card";
import { AddCard } from "./AddCard";
import type { OrderArg } from "../cardorder";
import { selectedByOf, type PresenceMap } from "../presence";
import { InsertGap } from "./InsertGap";
import { cellGaps, gapAnchor, gapsInZone } from "../insertgap";
import { insertedOrder, type Anchor } from "../backlogboard";
import { FilterSelect } from "./FilterSelect";
import { customerOptions, teamOptions } from "../filteroptions";
import { bornCustomers, customerPasses, placerOf, sprintTeamOf } from "../ownership";
import { useKeptCreated } from "../keptcreated";
import { SprintChoiceDialog } from "./SprintChoiceDialog";
import { SortableBoard, type BoardGroup, type DropResult } from "./SortableBoard";
import { globalOrderFromGroups, afterIdFor } from "./dndOrder";
import { isSubtask, subtaskParent, subtaskShows } from "../subtasks";
import {
  asksFirst,
  offersRemoval,
  removeChoices,
  backlogPatch,
  withdrawPatch,
  reviewAssigneePatch,
  unaskedRemoval,
  type RemoveChoice,
  deleteWarning,
  subtaskRemovalPatch,
  subtaskRemovalUndo,
} from "../removal";
import { makeCardPlacements, type CardPlacements } from "../placements";
import { RemoveChoiceDialog } from "./RemoveChoiceDialog";

interface TeamBoardProps {
  board: Board;
  provider: Provider;
  me: string;
  /** Viewed day, owned by the App (drives the lazy view fetch + scoped watch). */
  selectedDate: string;
  onSelectDate: (day: string) => void;
  /** The sprint pointers as they stand TODAY (a record's own are that day's).
   *  What decides, per team, whether the day being read is theirs to add to. */
  liveSprints: Record<string, SprintState>;
  /** A carry-over the APP asked for, rather than the button: somebody reached
   *  into a day that is over, and the board answers by leaving the record for
   *  today and offering that team its new sprint (App.leaveTheRecord). The
   *  `at` stamp is what makes a repeat ask a second time. */
  carryRequest?: { team: string | null; at: number };
  /** Avatars by login (the board roster). */
  avatars: Avatars;
  /** Display names by login (the board roster); a login without one is shown
   *  as is. */
  names: Names;
  /** Known teams (the roster), offered by the Team filter. */
  roster: string[];
  /** The Team filter, shared with the Backlog and the Me board: null = all,
   *  else the chosen teams ("" being the no-team group), several being OR. */
  teamFilter: string[] | null;
  onSetFilter: (keys: string[] | null) => void;
  /** The day boards' Customer filter (O11): [] = none, else ids (OR). */
  customerFilter?: string[];
  onSetCustomerFilter?: (next: string[]) => void;
  /** Open the team and customer rosters' dialogs, from the bottom of the
   *  Team and Customer filters. */
  onManageTeams?: () => void;
  onManageCustomers?: () => void;
  patchCard: (
    itemId: string,
    patch: Partial<CardModel> | ((c: CardModel) => Partial<CardModel>),
  ) => void;
  addCard: (card: CardModel) => void;
  /** Swap an optimistic card for its server twin in place (keeps the slot). */
  replaceCard: (itemId: string, card: CardModel) => void;
  removeCard: (itemId: string) => void;
  reorderCards: (order: OrderArg) => void;
  reload: () => void;
  /** Wraps a slow server call so the App's progress bar shows while it runs. */
  track: <T>(p: Promise<T>) => Promise<T>;
  /** Other users' live selections (login -> card uid) shown as avatars. */
  presence?: PresenceMap;
  /** Broadcast this board's selection as shared presence (#290). */
  onPresence?: (card: string | null) => void;
  onError: (message: string) => void;
  onOpen: (card: CardModel) => void;
}

/** Per-group metadata for the Team board: the destination engineer + zone. */
type TeamMeta = { kind: "cell"; engineer: string; zone: ZoneKey };

const UNASSIGNED = "";

const errMessage = (err: unknown) =>
  err instanceof Error ? err.message : String(err);

// isGone reports a "card not found" failure: the card no longer exists on the
// server, so an optimistic removal must NOT be rolled back (re-adding it would
// resurrect a phantom copy).
const isGone = (err: unknown) => errMessage(err).includes("card not found");

/** TeamBoard is the team as a people × zones grid for one day, filtered by team. */
export function TeamBoard({
  board,
  provider,
  me,
  selectedDate,
  onSelectDate,
  liveSprints,
  carryRequest,
  avatars,
  names,
  roster,
  teamFilter,
  onSetFilter,
  customerFilter = [],
  onSetCustomerFilter,
  onManageTeams,
  onManageCustomers,
  patchCard,
  addCard,
  replaceCard,
  removeCard,
  reorderCards,
  reload,
  track,
  presence,
  onPresence,
  onError,
  onOpen,
}: TeamBoardProps) {
  const [selectedCardId, setSelectedCardId] = useState<string | null>(null);
  // The selection is shared live (#290): the people the cards are on see the
  // lead's selection on their Me boards, as the lead sees theirs here.
  useEffect(() => {
    onPresence?.(selectedCardId);
  }, [selectedCardId]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => () => onPresence?.(null), []); // eslint-disable-line react-hooks/exhaustive-deps
  // The card whose × is waiting on a two-way answer (delete, or keep it in
  // the previous sprint).
  const [removeChoice, setRemoveChoice] = useState<CardModel | null>(null);
  const [sprintMenuOpen, setSprintMenuOpen] = useState(false);
  const sprintRef = useRef<HTMLDivElement | null>(null);
  // Subtask UI state: manually expanded parents, the card a drag would group
  // under (its middle band is hovered — the card highlights), and the parent
  // whose add-subtask form is open (the + button flow).
  const [expandedSubs, setExpandedSubs] = useState<Set<string>>(new Set());
  const [groupHover, setGroupHover] = useState<string | null>(null);
  const [addingSub, setAddingSub] = useState<string | null>(null);
  // A create on a day ahead of the team's current sprint waits here for the
  // lead's current-vs-next-sprint choice (null = no dialog open).
  const [sprintChoice, setSprintChoice] = useState<{
    engineer: string;
    zone: ZoneKey;
    title: string;
    team: string | null;
    sprint: string;
    anchor: Anchor;
  } | null>(null);
  // The gap whose insert "+" is open, as "<cell key>:<gap>" (InsertGap).
  const [inserting, setInserting] = useState<string | null>(null);
  const [columnOrder, setColumnOrder] = useState<string[]>(() => {
    try {
      const v = localStorage.getItem("aeman.columnOrder");
      return v ? (JSON.parse(v) as string[]) : [];
    } catch {
      return [];
    }
  });
  const [dragCol, setDragCol] = useState<string | null>(null);
  // Remember the hand-picked order of the people columns in the browser.
  useEffect(() => {
    localStorage.setItem("aeman.columnOrder", JSON.stringify(columnOrder));
  }, [columnOrder]);

  useEffect(() => {
    if (!sprintMenuOpen) {
      return;
    }
    const onDocClick = (e: MouseEvent) => {
      if (sprintRef.current && !sprintRef.current.contains(e.target as Node)) {
        setSprintMenuOpen(false);
      }
    };
    document.addEventListener("mousedown", onDocClick);
    return () => document.removeEventListener("mousedown", onDocClick);
  }, [sprintMenuOpen]);

  // No team filter shows all; otherwise match the card's group. And the
  // Customer filter by effective customers — the server's rule for the
  // listing and the watch (O11), applied to what the board holds.
  const { kept, keep } = useKeptCreated(customerFilter.join(","));
  // A card is judged by the team it STANDS UNDER (O13): its own, else the
  // plan item's above it.
  const place = useMemo(() => placerOf(board.cards), [board.cards]);
  const passesFilter = (card: CardModel): boolean =>
    (teamFilter === null || teamFilter.includes(place(card))) &&
    customerPasses(card, board.cards, customerFilter, kept);

  const inFilter = useMemo(
    () => board.cards.filter((c) => passesFilter(c)),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [board.cards, teamFilter, customerFilter, kept, place],
  );

  // What the people are WORKING ON on the day being looked at, and nothing
  // else — work in hand (K1), never the backlog. The rule itself is teamgrid.inHandOn, which mirrors
  // board.TeamGrid.
  const filteredCards = useMemo(
    () =>
      inFilter.filter((c) =>
        inHandOn(
          c,
          selectedDate,
          todayIso(),
          currentSprint(board, place(c) || null) ?? "",
        ),
      ),
    [inFilter, selectedDate, board, place],
  );

  // Columns are PEOPLE: the distinct assignees among the filtered cards (me
  // first). Columns come from everyone with a card in the selected teams in ANY
  // sprint (past or future), so a person's column stays (empty) on days they
  // have no cards. The Unassigned column is always shown.
  const engineers = useMemo(() => {
    const set = new Set<string>();
    for (const card of inFilter) {
      for (const login of card.assignees) {
        set.add(login);
      }
    }
    const rest = [...set]
      .filter((t) => t !== me)
      .sort((a, b) => a.localeCompare(b));
    const people = me && set.has(me) ? [me, ...rest] : rest;
    return [...people, UNASSIGNED];
  }, [inFilter, me]);

  // Apply the manual column order (people only); Unassigned always stays last and
  // people missing from the saved order are appended in their default order.
  const orderedEngineers = useMemo(() => {
    const people = engineers.filter((e) => e !== UNASSIGNED);
    const present = new Set(people);
    const ordered = columnOrder.filter((e) => present.has(e));
    const seen = new Set(ordered);
    for (const e of people) {
      if (!seen.has(e)) {
        ordered.push(e);
      }
    }
    return [...ordered, UNASSIGNED];
  }, [engineers, columnOrder]);

  const moveColumn = (from: string, to: string) => {
    if (from === to || from === UNASSIGNED || to === UNASSIGNED) {
      return;
    }
    const people = orderedEngineers.filter((e) => e !== UNASSIGNED);
    const fromIdx = people.indexOf(from);
    const toIdx = people.indexOf(to);
    if (fromIdx < 0 || toIdx < 0) {
      return;
    }
    people.splice(fromIdx, 1);
    people.splice(toIdx, 0, from);
    setColumnOrder(people);
  };

  const shuffleColumns = () => {
    const people = orderedEngineers.filter((e) => e !== UNASSIGNED);
    for (let i = people.length - 1; i > 0; i -= 1) {
      const j = Math.floor(Math.random() * (i + 1));
      [people[i], people[j]] = [people[j], people[i]];
    }
    setColumnOrder(people);
  };

  // The Shuffle button flips to "Ordered" once the columns differ from the
  // default order (you first, then the rest); clicking "Ordered" restores it.
  const isDefaultOrder = useMemo(() => {
    const def = engineers.filter((e) => e !== UNASSIGNED);
    const cur = orderedEngineers.filter((e) => e !== UNASSIGNED);
    return cur.length === def.length && cur.every((e, i) => e === def[i]);
  }, [engineers, orderedEngineers]);

  // New cards default to the filtered team; null = all (show picker), "" = no team.
  const forcedTeam = useMemo(
    () => (teamFilter?.length === 1 ? teamFilter[0] || null : undefined),
    [teamFilter],
  );

  // When several teams are filtered, the create picker and the Carry Over menu
  // offer just those (a single filtered team is forced above; no filter → all).
  const pickerTeams = useMemo(
    () =>
      teamFilter && teamFilter.length > 0
        ? roster.filter((t) => teamFilter.includes(t))
        : roster,
    [teamFilter, roster],
  );

  // A day that ENDED offers nothing to add: a card created there would land
  // on TODAY's board, which is not what the person looking at that day means.
  // The question is asked PER TEAM, the way the server asks it: while a
  // sprint is open its days are still that team's to work — the lead reading
  // the day it began adds a card there, which is where the standup is — and
  // only a team the day is over for loses its box. Taking the boxes off the
  // whole screen because one team had settled is what this replaced.
  const dayOverFor = useCallback(
    (team: string | null) => dayIsOverFor(liveSprints, team, selectedDate),
    [liveSprints, selectedDate],
  );
  // The teams a create may still name here — what the picker offers, so a
  // pick cannot land on a create the server refuses.
  const addTeams = useMemo(
    () => pickerTeams.filter((t) => !dayOverFor(t || null)),
    [pickerTeams, dayOverFor],
  );
  // Nothing anywhere can be added to when every team on screen has settled.
  const holdsRecords =
    forcedTeam === undefined ? addTeams.length === 0 : dayOverFor(forcedTeam);

  // "No team" is offered only when the no-team group is actually displayed
  // on the board — a card created into a hidden group would just vanish.
  const pickerNoTeam = (teamFilter ?? roster).includes("");

  // People to offer when reassigning a card: the BOARD's member roster plus
  // everyone seen on a card. Cards alone are not enough — in a team-filtered
  // view they only name that team's people, and handing a card to someone
  // outside the filter (the whole point of assigning) offered an empty seat.
  // MeBoard already does it this way.
  const people = useMemo(() => {
    const set = new Set<string>(board.members.map((m) => m.login));
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

  // Subtasks grouped by parent, from the full card state (children are
  // delivered alongside their parents by the view).
  // Mirrors the server's subtaskOnDay: a subtask keeps its own day
  // visibility even though it rides its parent — deferred to the future it
  // hides until its day, and one left behind in an earlier sprint stays on
  // that sprint's days. Without this an acked defer (addCard) would put the
  // row right back under the parent on today's board.
  // The shared rule (subtasks.ts) — the same one the Me board applies.
  const subtaskOnDay = (c: CardModel): boolean =>
    subtaskShows(c, { today: todayIso(), day: selectedDate });

  const childrenOf = useMemo(() => {
    const m = new Map<string, CardModel[]>();
    for (const c of board.cards) {
      // Under a TASK only: a task under an epic is a row of its own.
      const under = subtaskParent(c);
      if (under && subtaskOnDay(c)) {
        const list = m.get(under) ?? [];
        list.push(c);
        m.set(under, list);
      }
    }
    return m;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [board.cards, selectedDate, board]);

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

  const cellCards = (engineer: string, zone: ZoneKey): CardModel[] =>
    filteredCards.filter((c) => {
      // Cards without a zone fall into gray, matching the Me board.
      if ((c.zone ?? "gray") !== zone) {
        return false;
      }
      return engineer === UNASSIGNED
        ? c.assignees.length === 0
        : c.assignees.includes(engineer);
    });

  const cellKey = (engineer: string, zone: ZoneKey) =>
    `${engineer || "__unassigned__"}::${zone}`;
  // Sortable groups: one per grid cell. A cell is a (person, zone) pair, and
  // dragging between them is what moves a card across the day grid.
  const groups = useMemo<BoardGroup<TeamMeta>[]>(() => {
    const out: BoardGroup<TeamMeta>[] = [];
    for (const engineer of orderedEngineers) {
      for (const zone of ZONE_ORDER) {
        out.push({
          key: cellKey(engineer, zone),
          meta: { kind: "cell", engineer, zone },
          // An expanded parent's subtasks follow it as indented rows of the
          // same cell (a subtask has no cell placement of its own).
          cards: cellCards(engineer, zone).flatMap((c) =>
            subsOpen(c.itemId) ? [c, ...(childrenOf.get(c.itemId) ?? [])] : [c],
          ),
        });
      }
    }
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filteredCards, orderedEngineers, childrenOf, expandedSubs]);

  const handleDrop = ({
    card,
    fromMeta,
    toMeta,
    groups: g,
    groupUnder,
  }: DropResult<TeamMeta>) => {
    // From here both ends are grid cells. The board committed exactly what
    // the placeholder previewed: groupUnder is the already-validated parent
    // to nest under (null = standalone).
    const parentTo = groupUnder;
    // Judged against the parent AS A TASK: a standalone drop of a task under
    // an epic is no change, not an unlinking from the epic.
    const parentChanged = (subtaskParent(card) ?? "") !== (parentTo ?? "");

    // 1) Optimistic local state first. A grouped card is not cell-placed, so
    // zone/assignee only apply to standalone drops (including a pull-out).
    const optimistic: Partial<CardModel> = {};
    const patch: CardPatch = {};
    if (parentChanged) {
      optimistic.parent = parentTo ?? undefined;
      patch.parent = parentTo ?? "";
      if (parentTo) {
        autoExpanded.current = null; // the drop keeps the target unfolded
        setExpandedSubs((cur) => new Set(cur).add(parentTo as string));
      }
    }
    if (!parentTo) {
      if ((card.zone ?? "gray") !== toMeta.zone) {
        optimistic.zone = toMeta.zone;
        patch.zone = toMeta.zone;
      }
      const wantAssignees = toMeta.engineer ? [toMeta.engineer] : [];
      const sameAssignees =
        card.assignees.length === wantAssignees.length &&
        card.assignees.every((a, i) => a === wantAssignees[i]);
      // Keep multi-assignee cards intact on a plain reorder within the cell.
      if (
        !sameAssignees &&
        (parentChanged ||
          fromMeta.kind !== "cell" ||
          fromMeta.engineer !== toMeta.engineer)
      ) {
        // A review card's person is its reviewer: the server takes or hands
        // it back (K10, K12), and the board shows the same at once.
        Object.assign(optimistic, reviewAssigneePatch(card, wantAssignees));
        patch.assignees = wantAssignees;
      }
    }
    if (Object.keys(optimistic).length > 0) {
      patchCard(card.itemId, optimistic);
    }
    const order = globalOrderFromGroups(
      board,
      g.filter((x) => x.meta.kind === "cell").map((x) => x.ids),
    );
    reorderCards(order);

    // 2) Persist in the background; revert via reload() on any error. The
    // move anchors on a neighbour from the card's OWN cell: the flattened
    // cross-group order is a view artefact, and a card landing first in its
    // cell would otherwise anchor on another cell's tail — a card whose
    // global position may be past the visible neighbour below.
    const entryIds = g.find((x) => x.ids.includes(card.itemId))?.ids ?? [];
    const at = entryIds.indexOf(card.itemId);
    const afterId = afterIdFor(order, card.itemId);
    void (async () => {
      try {
        if (Object.keys(patch).length > 0) {
          await provider.patchCard(card.itemId, patch);
        }
        if (at > 0) {
          await provider.moveCard(card.itemId, entryIds[at - 1]);
        } else if (at === 0 && entryIds.length > 1) {
          await provider.moveCardBefore(card.itemId, entryIds[1]);
        } else {
          await provider.moveCard(card.itemId, afterId);
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

  // Progress is one intent; the server clamps (review/locked stay in 10–90),
  // clears a legacy stored done below full, and — when this is a review card —
  // drives the original's review stage. The optimistic patch mirrors the
  // clamps; the re-list converges the linked original.
  // Who has this card selected in their Me view right now. Own selections
  // from another window count too — the map only ever carries OTHER tabs'
  // marks (this tab's watch echo is suppressed), so nothing self-duplicates.
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

  // A card may take the dragged card as a subtask unless depth would exceed
  // one level (the target is already a subtask, or the dragged card has its
  // own subtasks) — the middle-band group drop only offers valid targets.
  const canGroup = (active: CardModel, target: CardModel) =>
    !isSubtask(target) &&
    target.itemId !== subtaskParent(active) &&
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
  }, [selectedCardId, childrenOf]);

  // Group a dropped card as a subtask; the server enforces depth.
  const handleGroup = (card: CardModel, parentId: string) => {
    if (card.itemId === parentId || subtaskParent(card) === parentId) {
      return;
    }
    const prev: Partial<CardModel> = { parent: card.parent };
    patchCard(card.itemId, { parent: parentId });
    autoExpanded.current = null; // the drop keeps the target unfolded
    setExpandedSubs((cur) => new Set(cur).add(parentId));
    void provider
      .patchCard(card.itemId, { parent: parentId })
      .then((c) => {
        addCard(c);
        reload();
      })
      .catch((err: unknown) => {
        patchCard(card.itemId, prev);
        onError(errMessage(err));
      });
  };

  // Create a card directly as a subtask (inherits the parent's team and
  // zone). An optimistic copy shows at the end of the list instantly; the
  // server card replaces it.
  const handleCreateSubtask = (parent: CardModel, title: string) => {
    const tempId = `tmp-${new Date().toISOString()}`;
    addCard({
      itemId: tempId,
      title: optimisticTitle(title),
      assignees: parent.assignees.slice(0, 1),
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

    const created = provider.createCard({
      title,
      team: parent.team ?? null,
      zone: parent.zone ?? "gray",
      start: selectedDate,
      day: selectedDate,
      // The subtask starts with the parent's person (still reassignable), so
      // it lives in the parent's grid cell if it ever stands alone.
      assigneeLogin: parent.assignees[0] ?? null,
      parent: parent.itemId,
    });
    // Mutations fired against the tmp id (a quick reorder, a rename) wait in
    // the pending registry for the real uid instead of erroring out.
    registerPendingCard(tempId, created.then((c) => c.itemId));
    created
      .then((c) => {
        if (consumePendingCancel(tempId)) {
          removeCard(tempId);
          void provider.deleteCard(c.itemId).catch(() => {});
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

  // renderGridCard is the grid-variant card used both for top-level rows and
  // for subtask rows (a subtask works exactly like any card).
  const renderGridCard = (card: CardModel): ReactNode => (
    <Card
      card={card}
      record={!!card.asOf}
      selectedBy={selectedByFor(card)}
      onLoadLinks={loadCardLinks}
      selected={card.itemId === selectedCardId}
      onSelect={(c) => setSelectedCardId(c.itemId)}
      onProgress={handleProgress}
      onDelete={handleGridDelete}
      placements={placementsFor(card)}
      // The board puts every question this × raises, and names the act it
      // is about to do (RemoveChoiceDialog), so the card's own anonymous
      // "Delete?" never stands in front of one.
      boardAsks
      // The × is drawn only where it has something it could do.
      deletable={offersRemoval(card, gridCtx(card))}
      onStage={handleStage}
      onInProgress={handleInProgress}
      onOpen={onOpen}
      teams={roster}
      people={people}
      reviewers={reviewerCandidates(people, board.domains, card.domain)}
      avatars={avatars}
      names={names}
      domainBadge={cardDomainBadge(board.domains, card.domain)}
      onSetTeam={handleSetTeam}
      onSetAssignee={handleSetAssignee}
      hasLinkedReview={reviewedItemIds.has(card.itemId)}
      counterpartAssignees={counterpartAssigneesFor(card)}
      onSetReviewAssignee={handleSetReviewAssignee}
      asOf={selectedDate}
      onSetDates={handleSetDates}
      onDefer={handleDefer}
      dimAvatar
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
        hidden={holdsRecords}
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
    const under = subtaskParent(card);
    if (under) {
      const wrapped = <div className="subtask-indent">{node}</div>;
      const subs = childrenOf.get(under) ?? [];
      const parent = cardsById.get(under);
      if (
        addingSub === under &&
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

  // Who else has this card selected (presence, #290): one rule for every
  // board (presence.selectedByOf); a folded parent wears its subtasks' marks.
  const selectedByFor = (card: CardModel): string[] | undefined =>
    selectedByOf(presence, card, isSubtask(card) ? undefined : { children: (id) => childrenOf.get(id) ?? [], open: subsOpen }, me);


  // Resolve a card's description links (GitHub refs get titles) for the menu.
  const loadCardLinks = (card: CardModel) =>
    provider.listLinks(card.itemId);

  // Mirror the server's derived-progress rule (board.DerivedProgress) so a
  // parent's bar moves the instant a subtask's does; the server converges it.
  const syncParentBar = (child: CardModel, childValue: number) => {
    const under = subtaskParent(child);
    if (!under) {
      return;
    }
    const parent = cardsById.get(under);
    if (!parent || isComplete(parent)) {
      return;
    }
    const kids = board.cards.filter((c) => c.parent === under);
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
    let value = clampProgress(card.stage, raw);
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
      .patchCard(card.itemId, { progress: value })
      .then((updated) => {
        addCard(updated);
        // A review card's progress drives the original's stage server-side,
        // and a subtask's drives its parent's derived bar.
        if (card.reviewOf || isSubtask(card)) {
          reload();
        }
      })
      .catch((err: unknown) => {
        patchCard(card.itemId, prev);
        if (isSubtask(card)) {
          reload(); // the parent's optimistic derived bar needs the truth back
        }
        onError(errMessage(err));
      });
  };

  // Stage is one intent; the server derives done (clears stage + fills 100),
  // knocks a full review/locked card to 90, and cancels the linked review card
  // when the original leaves review. The optimistic patch mirrors the local
  // effects; the re-list converges the linked-card cascade.
  const handleStage = (
    card: CardModel,
    stage: StageKey | null,
    recurrence?: "" | "week" | "month",
  ) => {
    const prev: Partial<CardModel> = {
      stage: card.stage,
      progress: card.progress,
      recurrence: card.recurrence,
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
    // The recurrent picker carries the cycle; any other stage sheds it
    // (mirrors the server's applyStage).
    if (stage === "recurrent") {
      patch.recurrence = recurrence ?? "";
    } else if (stage !== "done" && card.recurrence) {
      patch.recurrence = "";
    }
    if (stage === "done") {
      patch.progress = 100;
    }
    if (clampsProgress(stage)) {
      // The 10-90 clamp is stored on stage pick (mirrors board.ApplyStage).
      patch.progress = clampProgress(stage, card.progress ?? 0);
    }
    patchCard(card.itemId, patch);
    syncParentBar(card, stage === "done" ? 100 : patch.progress ?? card.progress ?? 0);
    const leavingReview = card.stage === "review" && stage !== "review";
    // Entering review re-review reactivates a completed linked review card
    // server-side (progress → 0, round bumped); re-list so it converges.
    const enteringReview = stage === "review" && card.stage !== "review";
    const hasLinkedReview = board.cards.some((c) => c.reviewOf === card.itemId);
    void provider
      .patchCard(card.itemId, {
        stage: stage ?? "",
        ...(stage === "recurrent" ? { recurrence: recurrence ?? "" } : {}),
      })
      .then((updated) => {
        addCard(updated);
        if (
          leavingReview ||
          (enteringReview && hasLinkedReview) ||
          card.reviewOf ||
          isSubtask(card)
        ) {
          reload();
        }
      })
      .catch((err: unknown) => {
        patchCard(card.itemId, prev);
        if (isSubtask(card)) {
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
      .setInProgress(card.itemId)
      .then((updated) => {
        addCard(updated);
        if (card.stage === "review" || card.reviewOf || isSubtask(card)) {
          reload();
        }
      })
      .catch((err: unknown) => {
        patchCard(card.itemId, prev);
        if (isSubtask(card)) {
          reload(); // the parent's optimistic derived bar needs the truth back
        }
        onError(errMessage(err));
      });
  };

  // The team's explicit previous sprint — where deleting a grid card demotes it
  // (back to last sprint) instead of removing it.
  // The sprints of the team the card stands under (O13).
  const previousSprintFor = (card: CardModel): string | null =>
    previousSprint(board, sprintTeamOf(card, board.cards));

  // The grid ×: one remove intent — the server hands the card back to a home
  // it still has, or deletes it for real (cascading the linked review card,
  // and the subtasks that are pieces of it). The optimistic patch
  // mirrors those rules locally; the re-list converges the server's outcome.
  // The × asks the same question on every board: gridCtx names the sprints
  // it is asked in, reviewOf the card a delete would take along.
  const gridCtx = (card: CardModel) => ({
    current: currentSprint(board, sprintTeamOf(card, board.cards)) ?? undefined,
    previous: previousSprintFor(card) ?? undefined,
    today: todayIso(),
  });
  const reviewOf = (card: CardModel) =>
    board.cards.find((c) => c.reviewOf === card.itemId)?.title ?? null;

  // placementsFor: the assign menu's placement section — one shared
  // factory (makeCardPlacements), so the boards cannot drift apart.
  const placementsFor = (card: CardModel): CardPlacements | undefined => {
    return makeCardPlacements(card, {
      provider,
      patchCard,
      reload,
      onError,
      errMessage,
    });
  };

  // What a week of a team's plan is weighed against. The board derives
  // nothing, so the answer comes back from the server and the reload is it.
  const handleGridDelete = (card: CardModel, chosen?: RemoveChoice) => {
    if (card.itemId.startsWith("tmp-")) {
      cancelPendingCard(card.itemId);
      removeCard(card.itemId);
      return;
    }
    // The × asks BEFORE it acts, whatever it is about to do — the dialog
    // names the act, so a card sent back to its backlog and a card taken
    // off the board are told apart before either happens. The one card it
    // does not ask about is one made today that nobody has touched
    // (asksFirst), which the branch above has already answered for a card
    // that never reached the server at all.
    // Withdrawing a review always asks: it changes the TASK, not this card.
    if (!chosen && (asksFirst(card, justMade(card.itemId)) || removeChoices(card, gridCtx(card)).includes("withdraw"))) {
      setRemoveChoice(card);
      return;
    }
    // Unasked (a card made today that nobody touched), the card's own first
    // answer stands — the same one the dialog would have put at the top.
    const choice = chosen ?? unaskedRemoval(removeChoices(card, gridCtx(card)));
    if (!choice) {
      return;
    }
    // Withdrawing a review: the review card goes and its task leaves review,
    // back to In progress (K10) — both shown at once. The server does the
    // same through the ×'s off-board (boardservice.withdrawReview).
    if (choice === "withdraw") {
      const task = board.cards.find((c) => c.itemId === card.reviewOf);
      removeCard(card.itemId);
      if (task) {
        patchCard(task.itemId, withdrawPatch(task));
      }
      void provider.removeCard(card.itemId, "off-board").catch((err: unknown) => {
        if (!isGone(err)) {
          onError(errMessage(err));
        }
        reload();
      });
      return;
    }
    // Work finished in the sprint before this one, and only marked done
    // now: the card goes back to where the work happened.
    if (choice === "finished-earlier") {
      void provider
        .finishedEarlier(card.itemId)
        .then(() => reload())
        .catch((err: unknown) => {
          onError(errMessage(err));
          reload();
        });
      return;
    }
    // The answer that destroys nothing: the task goes back to its team's
    // backlog — nobody, no day, no sprint (K3) — through the ×'s own action,
    // the unassign intent (removal.removeIntent).
    if (choice === "backlog") {
      patchCard(card.itemId, backlogPatch(card, board.cards));
      void provider.removeCard(card.itemId, "unassign").catch((err: unknown) => {
        if (isGone(err)) {
          return;
        }
        onError(errMessage(err));
        reload();
      });
      return;
    }
    // A subtask has no sprint history of its own: the × deletes it
    // outright, gone from under its parent immediately.
    if (isSubtask(card) && choice === "off-board") {
      // The board took the question on (boardAsks), so the board must put
      // it: the card's own prompt has stood down, and deleting in silence
      // is how a worked subtask went in one click.
      const warning = deleteWarning(card, reviewOf(card));
      if (warning && !window.confirm(warning)) {
        return;
      }
      removeCard(card.itemId);
      void provider.deleteCard(card.itemId).catch((err: unknown) => {
        if (!isGone(err)) {
          onError(errMessage(err));
          reload();
        }
      });
      return;
    }
    if (isSubtask(card)) {
      // Out of the group (the server's answer). The row must stop being drawn under its parent at
      // once, or the × looks inert; and it must show the SAME optimistic
      // state the Me board shows, or one gesture reads two ways on two
      // boards.
      const prev = subtaskRemovalUndo(card) as Partial<CardModel>;
      patchCard(card.itemId, subtaskRemovalPatch(card, gridCtx(card)));
      void provider
        .removeCard(card.itemId)
        .then(() => reload())
        .catch((err: unknown) => {
          if (isGone(err)) {
            return;
          }
          patchCard(card.itemId, prev);
          onError(errMessage(err));
        });
      return;
    }
    // This × empties the working area (mirrors boardservice.Remove). Taken
    // off the board, the card is deleted — after a question when it carries
    // work or a review card, which the delete takes with it — and its
    // subtasks go with it: they are pieces of the same work, and the dialog
    // says how many.
    if (choice === "off-board") {
      const linkedReview = board.cards.find((c) => c.reviewOf === card.itemId);
      const warning = deleteWarning(card, linkedReview?.title ?? null);
      if (warning && !window.confirm(warning)) {
        return;
      }
      // The subtasks are pieces of the same work and go with it —
      // locally too, or they hang under a parent that is gone until a
      // refresh (the watch skips this tab's own changes).
      for (const sub of board.cards.filter((c) => c.parent === card.itemId)) {
        removeCard(sub.itemId);
      }
      removeCard(card.itemId);
      if (linkedReview) {
        removeCard(linkedReview.itemId);
      }
      void provider
        .removeCard(card.itemId, "off-board")
        .then(() => reload())
        .catch((err: unknown) => {
          if (!isGone(err)) {
            onError(errMessage(err));
            reload();
          }
        });
      return;
    }
  };

  // Moving a card between teams also joins the new team's current sprint
  // (server-side), so it stays visible instead of dropping off when its old
  // sprint predates the new team's current one. Mirror both optimistically.
  const handleSetTeam = (card: CardModel, team: string | null) => {
    const prev: Partial<CardModel> = {
      team: card.team,
      sprintStart: card.sprintStart,
    };
    // The sprint of the team it will stand under (O13): the one given, else
    // — its own team taken off — the plan item's above it.
    const sprintStart = currentSprint(board, sprintTeamOf(card, board.cards, team ?? "")) ?? selectedDate;
    patchCard(card.itemId, { team: team ?? undefined, sprintStart });
    void provider
      .patchCard(card.itemId, { team: team ?? "" })
      .then(addCard)
      .catch((err: unknown) => {
        patchCard(card.itemId, prev);
        onError(errMessage(err));
      });
  };

  const handleSetAssignee = (card: CardModel, login: string | null) => {
    // A subtask always belongs to its parent's person (mirrors the server): a
    // direct change follows the parent, and re-assigning a parent hands the
    // whole family over. A family split across two people lands on two
    // people's Me boards.
    const under = subtaskParent(card);
    const target = under ? cardsById.get(under) ?? card : card;
    const next = under
      ? target.assignees.slice(0, 1)
      : login
        ? [login]
        : [];
    const family = under
      ? []
      : board.cards.filter((c) => c.parent === card.itemId);
    const prev = card.assignees;
    const prevFamily = family.map((c) => ({ id: c.itemId, was: c.assignees }));
    patchCard(card.itemId, { assignees: next });
    for (const c of family) {
      patchCard(c.itemId, { assignees: next });
    }
    void provider
      .patchCard(card.itemId, { assignees: next })
      .then(addCard)
      .catch((err: unknown) => {
        patchCard(card.itemId, { assignees: prev });
        for (const f of prevFamily) {
          patchCard(f.id, { assignees: f.was });
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
        .removeReviewer(card.itemId)
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
      .sendToReview(card.itemId, login, selectedDate)
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
  // card (in the original's zone/team) and puts the original on the review
  // stage. Both effects are mirrored optimistically; the re-list converges
  // the original's server-side state.
  const handleSendToReview = (card: CardModel, reviewerLogin: string) => {
    const team = card.team ?? null;
    const zone: ZoneKey = card.zone ?? "gray";
    // The sprint of the team the card stands under (O13).
    const sprintStart = currentSprint(board, sprintTeamOf(card, board.cards)) ?? selectedDate;
    const title = `review: ${card.title}`;
    const tempId = `tmp-${new Date().toISOString()}`;
    const optimistic: CardModel = {
      itemId: tempId,
      // A bare GitHub reference reads as its readable label at once; the
      // server's background resolve renames it to the real title shortly.
      title: optimisticTitle(title),
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
      // The band is stored on the review pick (mirrors board.ApplyStage), and
      // it is asked for rather than written out again: a hand-written copy of
      // this rule is how a stage came to be clamped in some places and not
      // others, and the copy that is forgotten is the one that shows.
      progress: clampProgress("review", card.progress ?? 0),
    });
    const creating = provider.sendToReview(card.itemId,
      reviewerLogin,
      selectedDate,
    );
    registerPendingCard(
      tempId,
      creating.then((c) => c.itemId),
    );
    void creating
      .then((created) => {
        removeCard(tempId);
        if (consumePendingCancel(tempId)) {
          // Deleted while the create was in flight: drop the server twin.
          void provider.deleteCard(created.itemId).catch(() => undefined);
          return;
        }
        addCard(created);
        reload();
      })
      .catch((err: unknown) => {
        consumePendingCancel(tempId);
        removeCard(tempId);
        patchCard(card.itemId, prevOriginal);
        onError(errMessage(err));
      });
  };

  // The calendar relocates the card for real: one dates patch — the server
  // runs the calendar rule (the start day picks the sprint that was active on
  // it, so a date inside the current sprint joins it instead of standing alone
  // on the picked day). The optimistic patch mirrors that rule.
  const handleSetDates = (
    card: CardModel,
    start: string | null,
    end: string | null,
  ) => {
    // Which sprint the dates put the card in — one rule, shared with the
    // server (sprintForDate mirrors boardservice.SetDates): a future day
    // parks it, a day the team can still reach takes that day's sprint, and
    // a day older than that keeps the team's current one rather than a
    // sprint that closed, which would take the card off every board.
    const sprint = sprintForDate(board, sprintTeamOf(card, board.cards), start ?? "");
    const prev = {
      startDate: card.startDate,
      sprintStart: card.sprintStart,
      day: card.day,
    };
    patchCard(card.itemId, {
      startDate: start ?? undefined,
      sprintStart: sprint ?? undefined,
      day: end ?? undefined,
    });
    void provider
      .patchCard(card.itemId, {
        dates: { start: start ?? "", end: end ?? "" },
      })
      .then(addCard)
      .catch((err: unknown) => {
        patchCard(card.itemId, prev);
        onError(errMessage(err));
      });
  };

  // Defer moves the scheduled day (startDate) N days ahead of today (or of an
  // already-deferred slot; presses stack). The server owns the rule — a card
  // created today relocates fully (sprint and a stale end date move along) —
  // and this mirrors it for the optimistic patch.
  const handleDefer = (card: CardModel, days: number) => {
    const today = todayIso();
    const base =
      card.startDate && card.startDate > today ? card.startDate : today;
    const newStart = addDays(base, days);
    // Only a card created today relocates fully — an older one keeps the
    // sprint it was worked in. But an end date is never left behind the new
    // start, whatever the card's age: a card due before it begins is overdue
    // for ever and comes straight back to the week it was just sent out of.
    const full = !!card.createdAt && localDateIso(card.createdAt) === today;
    const newDay = card.day && card.day < newStart ? newStart : card.day;
    const prev = {
      startDate: card.startDate,
      sprintStart: card.sprintStart,
      day: card.day,
    };
    patchCard(card.itemId, {
      startDate: newStart,
      day: newDay,
      ...(full ? { sprintStart: newStart } : {}),
    });
    void provider
      .deferCard(card.itemId, days)
      .then(addCard)
      .catch((err: unknown) => {
        patchCard(card.itemId, prev);
        onError(errMessage(err));
      });
  };

  // Optimistically create a Team grid card scheduled for the given day; the
  // server joins the team's current sprint — recording a first sprint when the
  // team has none yet, in which case a reload picks the new pointer up.
  const createTeamCard = (
    engineer: string,
    zone: ZoneKey,
    title: string,
    team: string | null,
    startDate: string,
    day: string,
    noSprint = false,
    anchor: Anchor = null,
  ) => {
    const sprint = currentSprint(board, team);
    const firstSprint = !noSprint && sprint === null;
    const tempId = `tmp-${new Date().toISOString()}`;
    // Typed in under the Customer filter: born for its one customer, or —
    // with several chosen — kept shown until the filter changes, so it does
    // not vanish the moment it is made (O11).
    const born = bornCustomers(customerFilter);
    if (customerFilter.length > 1) {
      keep(tempId);
    }
    const optimistic: CardModel = {
      itemId: tempId,
      customers: born,
      // A bare GitHub reference reads as its readable label at once; the
      // server's background resolve renames it to the real title shortly.
      title: optimisticTitle(title),
      assignees: engineer ? [engineer] : [],
      zone,
      day,
      startDate,
      // A card scheduled ahead joins no sprint: the sprint for that day does
      // not exist yet, and the carry-over reaching its day adopts it
      // (mirrors boardservice.CreateCard).
      sprintStart:
        noSprint || startDate > todayIso()
          ? undefined
          : (sprint ?? startDate),
      team: team ?? undefined,
      createdAt: new Date().toISOString(),
      description: "",
      notes: [],
    };
    addCard(optimistic);
    // Typed into a gap: shown there at once, and the create names the place
    // (after / before) so the server puts it there in the same request.
    if (anchor) {
      reorderCards((cur) => insertedOrder(cur, tempId, anchor));
    }
    const creating = provider.createCard({
      title,
      zone,
      day,
      start: startDate,
      assigneeLogin: engineer || null,
      team,
      customers: born ?? null,
      noSprint: noSprint || undefined,
      after: anchor && "after" in anchor ? anchor.after : null,
      before: anchor && "before" in anchor ? anchor.before : null,
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
          void provider.deleteCard(card.itemId).catch(() => undefined);
          return;
        }
        // Swap in place: append-on-ack would reshuffle a quick burst of adds.
        if (customerFilter.length > 1) {
          keep(card.itemId);
        }
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

  // Creating a Team card is scheduled for the viewed day as a one-day range —
  // a backdated create with day = today would stretch onto today's board. The
  // sprint join (and the first-sprint record) happens server-side. A day ahead
  // of the team's current sprint is ambiguous — a later day of the running
  // sprint (two-day sprints) or the next one (daily sprints)? Only the lead
  // knows, so ask; "next sprint" creates the card without a sprint and the
  // next carry over to reach its day adopts it.
  const handleCreate = (
    engineer: string,
    zone: ZoneKey,
    title: string,
    team?: string | null,
    anchor: Anchor = null,
  ) => {
    const sprint = currentSprint(board, team ?? null);
    if (sprint !== null && selectedDate > sprint) {
      setSprintChoice({ engineer, zone, title, team: team ?? null, sprint, anchor });
      return;
    }
    createTeamCard(
      engineer,
      zone,
      title,
      team ?? null,
      selectedDate,
      selectedDate,
      false,
      anchor,
    );
  };

  // Carry over: advance the team's sprint to today (the prior current becomes
  // the previous) and pull its unfinished cards forward — one server action; a
  // dry run feeds the confirm count. Always advances, even with nothing to
  // carry. `team` is null for the no-team group.
  const startSprint = async (team: string | null, asked = false) => {
    setSprintMenuOpen(false);
    const label = team ?? "no team";
    const old = currentSprint(board, team);
    const today = todayIso();
    // The sprint is already today's: never re-advance — that would overwrite
    // the previous sprint, making previous = current = today. A second press
    // hands the previous sprint back its finished work instead: the leftovers
    // closed after the carry-over that were in work before today (the server
    // picks them). Still land on today, so pressing Carry over always brings
    // the current sprint into view. Nothing is SAID or asked when the app
    // asked rather than the reader: they pressed nothing.
    if (old === today) {
      onSelectDate(today);
      if (asked) {
        return;
      }
      let back: CarryReport;
      try {
        back = await track(provider.carryOver(team, true));
      } catch (err: unknown) {
        onError(errMessage(err));
        return;
      }
      const n = back.returned ?? 0;
      if (n === 0) {
        onError(`«${label}» is already on today's sprint; no finished card to return to the previous one.`);
        return;
      }
      if (!window.confirm(`«${label}» is already on today's sprint. Return ${n} finished card(s) to the previous sprint?`)) {
        return;
      }
      try {
        await track(provider.carryOver(team));
      } catch (err: unknown) {
        onError(errMessage(err));
      }
      reload();
      return;
    }
    let rep: CarryReport;
    try {
      rep = await track(provider.carryOver(team, true));
    } catch (err: unknown) {
      onError(errMessage(err));
      return;
    }
    if (
      !window.confirm(
        `Start a new sprint for «${label}» (${today})? ${rep.carried} unfinished card(s) carried over.`,
      )
    ) {
      return;
    }
    // Move the cards optimistically so the current view keeps showing them
    // while the (slow) carry runs; the jump to today happens AFTER the server
    // call — changing the day now would refetch the view against the not-yet-
    // advanced sprint and blank the board. Only the closing (current) sprint's
    // unfinished cards carry, mirroring boardservice.CarryOver.
    // The cards standing under the team (O13): its own, else the plan
    // item's above them — as boardservice.CarryOver carries them.
    for (const c of board.cards) {
      const sameTeam = (place(c) || null) === team;
      if (!sameTeam || isComplete(c) || c.itemId.startsWith("tmp-")) {
        continue;
      }
      // Sprint-less day cards scheduled past the closing sprint whose day has
      // arrived ("next sprint" creates) are adopted by the sprint being
      // started, mirroring CarryOver; older sprint-less strays stay put.
      const adopted =
        !c.sprintStart &&
        !!c.startDate &&
        !!old &&
        c.startDate > old &&
        c.startDate <= today;
      if (adopted || (!!old && c.sprintStart === old)) {
        patchCard(c.itemId, { sprintStart: today });
      }
    }
    try {
      await track(provider.carryOver(team));
    } catch (err: unknown) {
      onError(errMessage(err));
    }
    // Land on today and re-read the advanced state (the day-change refetch now
    // sees the post-carry sprint).
    onSelectDate(today);
    reload();
  };

  // A carry the APP asked for (somebody reached into a day that is over).
  // Once per stamp: the same request must not re-fire on every render, and a
  // second reach into the past must ask again.
  const carryAsked = useRef(0);
  useEffect(() => {
    if (!carryRequest || carryRequest.at === carryAsked.current) {
      return;
    }
    carryAsked.current = carryRequest.at;
    void startSprint(carryRequest.team, true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [carryRequest]);

  return (
    <div className="team">
      {/* The day the board is looking at is chosen in the app's toolbar
          (DayNav): it is the app's state, and one control for it keeps the
          boards from disagreeing about the same day. */}
      <div className="board-toolbar">
        <FilterSelect
          label="Team"
          anyLabel="all"
          options={teamOptions(roster)}
          selected={teamFilter ?? []}
          onChange={(next) => onSetFilter(next.length ? next : null)}
          manage={onManageTeams ? { label: "Manage teams", onClick: onManageTeams } : undefined}
          title="Show only these teams' work"
        />
        <FilterSelect
          label="Customer"
          options={customerOptions(board.customers)}
          selected={customerFilter}
          onChange={(next) => onSetCustomerFilter?.(next)}
          manage={onManageCustomers ? { label: "Manage customers", onClick: onManageCustomers } : undefined}
          title="Show only the work for these customers"
        />

        <button
          type="button"
          className="btn shuffle-btn"
          onClick={isDefaultOrder ? shuffleColumns : () => setColumnOrder([])}
          title={
            isDefaultOrder
              ? "Shuffle columns"
              : "Restore default order (you first)"
          }
          aria-label={
            isDefaultOrder ? "Shuffle columns" : "Restore default order"
          }
        >
          {isDefaultOrder ? (
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-hidden="true"
            >
              <path d="M2 18h1.4c1.3 0 2.5-.6 3.3-1.7l6.1-8.6c.7-1.1 2-1.7 3.3-1.7H22" />
              <path d="m18 2 4 4-4 4" />
              <path d="M2 6h1.9c1.5 0 2.9.9 3.6 2.2" />
              <path d="M22 18h-5.9c-1.3 0-2.6-.7-3.3-1.8l-.5-.8" />
              <path d="m18 14 4 4-4 4" />
            </svg>
          ) : (
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-hidden="true"
            >
              <path d="M2 8h16" />
              <path d="m18 4 4 4-4 4" />
              <path d="M2 16h16" />
              <path d="m18 12 4 4-4 4" />
            </svg>
          )}
        </button>
        <div className="sprint-wrap" ref={sprintRef}>
          <button
            type="button"
            className="btn sprint-btn"
            onClick={() => {
              if (teamFilter?.length === 1) {
                void startSprint(teamFilter[0] === "" ? null : teamFilter[0]);
              } else {
                setSprintMenuOpen((o) => !o);
              }
            }}
            title={`Start a new sprint (today) for the selected team`}
          >
            Carry over{teamFilter?.length === 1 ? " →" : " ▾"}
          </button>
          {teamFilter?.length !== 1 && sprintMenuOpen && (
            <div className="card-stage-menu sprint-menu">
              {pickerTeams.map((t) => (
                <button
                  key={t}
                  type="button"
                  className="card-stage-item"
                  onClick={() => void startSprint(t)}
                >
                  <span className="team-dot" style={{ background: teamColor(t) }} />
                  {t}
                </button>
              ))}
              {pickerNoTeam && (
                <button
                  type="button"
                  className="card-stage-item card-stage-clear"
                  onClick={() => void startSprint(null)}
                >
                  no team
                </button>
              )}
            </div>
          )}
        </div>
      </div>

      <SortableBoard<TeamMeta>
        groups={groups}
        isRecord={(c) => !!c.asOf}
        onDrop={handleDrop}
        onGroupDrop={handleGroup}
        onHoverCard={setGroupHover}
        onDragActiveCard={handleDragActive}
        canGroup={canGroup}
        renderCard={(card) => withSubs(card, renderGridCard(card))}
          renderOverlay={(card) => (
            <Card
              card={card}
              record={!!card.asOf}
              selectedBy={selectedByFor(card)}
              onLoadLinks={loadCardLinks}
              selected={false}
              onSelect={() => {}}
              onProgress={() => {}}
              onDelete={() => {}}
              onStage={() => {}}
              onInProgress={() => {}}
              onOpen={() => {}}
            />
          )}
          renderGap={(group, at) => {
            // The insert "+" in every gap of a cell (insertgap.ts): the card
            // typed there is the cell's — its person, its band — and lands
            // there. None inside a parent's subtasks, none on a record.
            if (holdsRecords || !gapsInZone("team", group.meta.zone) || !cellGaps(group.cards).includes(at)) {
              return null;
            }
            const { engineer, zone } = group.meta;
            const key = `${group.key}:${at}`;
            return (
              <InsertGap
                key={`gap-${at}`}
                at={at}
                open={inserting === key}
                empty={group.cards.length === 0}
                onOpen={() => setInserting(key)}
              >
                <AddCard
                  autoOpen
                  teams={forcedTeam === undefined ? addTeams : undefined}
                  forcedTeam={forcedTeam}
                  allowNoTeam={pickerNoTeam}
                  onCreate={(title, team) =>
                    handleCreate(engineer, zone, title, team, gapAnchor(group.cards.map((c) => c.itemId), at))
                  }
                  onClosed={() => setInserting(null)}
                />
              </InsertGap>
            );
          }}
          renderGroup={(group, body, { isOver, dropRef }) => {
            const { zone } = group.meta;
            const def = ZONES[zone];
            return (
              <div
                ref={dropRef as Ref<HTMLDivElement>}
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
                <div className="zone-cards">{body}</div>
              </div>
            );
          }}
          renderLayout={(nodes) => (
            <div className="team-grid">
              {orderedEngineers.length === 0 ? (
                <p className="placeholder">No cards match the selected teams.</p>
              ) : (
                orderedEngineers.map((engineer) => (
              <section className="team-col" key={engineer || "__unassigned__"}>
                <header
                  className="team-col-header"
                  draggable={engineer !== UNASSIGNED}
                  onDragStart={() => setDragCol(engineer)}
                  onDragOver={(e) => {
                    if (dragCol && dragCol !== engineer) {
                      e.preventDefault();
                    }
                  }}
                  onDrop={() => {
                    if (dragCol) {
                      moveColumn(dragCol, engineer);
                    }
                    setDragCol(null);
                  }}
                  onDragEnd={() => setDragCol(null)}
                >
                  {engineer === UNASSIGNED ? (
                    <span className="team-col-name team-col-unassigned">
                      Unassigned
                    </span>
                  ) : (
                    <>
                      <Avatar
                        login={engineer}
                        avatars={avatars}
                        names={names}
                        className={`avatar-img${engineer === me ? " avatar-me" : ""}`}
                        title={displayName(engineer, names)}
                        draggable={false}
                      />
                      <span
                        className={`team-col-name${engineer === me ? " team-col-me" : ""}`}
                      >
                        {displayName(engineer, names)}
                      </span>
                    </>
                  )}
                </header>
                <div className="team-col-zones">
                  {ZONE_ORDER.map((zone) => (
                    <Fragment key={zone}>
                      {nodes.get(cellKey(engineer, zone))}
                    </Fragment>
                  ))}
                </div>
              </section>
                ))
              )}
            </div>
          )}
        />
      {sprintChoice && (
        <SprintChoiceDialog
          title={sprintChoice.title}
          day={selectedDate}
          sprint={sprintChoice.sprint}
          onClose={() => setSprintChoice(null)}
          onSubmit={(noSprint) =>
            createTeamCard(
              sprintChoice.engineer,
              sprintChoice.zone,
              sprintChoice.title,
              sprintChoice.team,
              selectedDate,
              selectedDate,
              noSprint,
              sprintChoice.anchor,
            )
          }
        />
      )}
      {removeChoice && (
        <RemoveChoiceDialog
          title={removeChoice.title}
          review={!!removeChoice.reviewOf}
          reviewing={board.cards.find((c) => c.itemId === removeChoice.reviewOf)?.title}
          progress={removeChoice.progress ?? 0}
          choices={removeChoices(removeChoice, gridCtx(removeChoice))}
          subtasks={(childrenOf.get(removeChoice.itemId) ?? []).length}
          onClose={() => setRemoveChoice(null)}
          onSubmit={(choice) => handleGridDelete(removeChoice, choice)}
        />
      )}
    </div>
  );
}
