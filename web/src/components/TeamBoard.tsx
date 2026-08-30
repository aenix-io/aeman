import { optimisticTitle } from "../links";
import {
  Fragment,
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
import type {
  Board,
  Card as CardModel,
  CardPatch,
  CarryReport,
  Provider,
  StageKey,
  ZoneKey,
} from "../providers/types";
import { ZONES, ZONE_ORDER } from "../zones";
import { todayIso, addDays, localDateIso, mondayOf, weeksBetween } from "../date";
import { activeSprint, currentSprint, previousSprint } from "../sprint";
import { teamColor } from "../avatar";
import { displayName, type Avatars, type Names } from "../users";
import { Avatar } from "./Avatar";
import { cardDomainBadge, reviewerCandidates } from "../domains";
import { Card } from "./Card";
import { AddCard } from "./AddCard";
import { TeamChips } from "./TeamChips";
import { TeamsModal } from "./TeamsModal";
import { SprintChoiceDialog } from "./SprintChoiceDialog";
import { SortableBoard, type BoardGroup, type DropResult } from "./SortableBoard";
import { globalOrderFromGroups, afterIdFor } from "./dndOrder";
import { isSlot, owedIn, planRemoveOffered, slotBand } from "../weekly";
import { subtaskShows } from "../subtasks";
import {
  boardAsksAbout,
  deleteWarning,
  freeSubtasks,
  gridGesture,
  gridRemoval,
  hasColumn,
  planRemoval,
  removalKind,
} from "../removal";
import {
  columnFollows,
  makeCardPlacements,
  rosterOf,
  type CardPlacements,
} from "../placements";
import { RemoveChoiceDialog } from "./RemoveChoiceDialog";

interface TeamBoardProps {
  board: Board;
  provider: Provider;
  me: string;
  /** Viewed day, owned by the App (drives the lazy view fetch + scoped watch). */
  selectedDate: string;
  onSelectDate: (day: string) => void;
  /** Avatars by login (the board roster). */
  avatars: Avatars;
  /** Display names by login (the board roster); a login without one is shown
   *  as is. */
  names: Names;
  /** Known teams (the roster), shown as filter chips. */
  roster: string[];
  /** Single-select team filter: null = all, "" = no team, else a team name. */
  teamFilter: string[] | null;
  onSetFilter: (keys: string[] | null) => void;
  onAddTeam: (team: string, domain?: string) => void;
  onRemoveTeam: (team: string) => void;
  onRenameTeam: (from: string, to: string) => void;
  onReorderTeams: (ordered: string[]) => void;
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
  /** Wraps a slow server call so the App's progress bar shows while it runs. */
  track: <T>(p: Promise<T>) => Promise<T>;
  /** Other users' live selections (login -> card uid) shown as avatars. */
  presence?: Record<string, string>;
  onError: (message: string) => void;
  onOpen: (card: CardModel) => void;
}

/** Per-group metadata for the Team board: the destination engineer + zone. */
type TeamMeta =
  | { kind: "cell"; engineer: string; zone: ZoneKey }
  | { kind: "band"; band: "wed" | "fri" };

const UNASSIGNED = "";

const errMessage = (err: unknown) =>
  err instanceof Error ? err.message : String(err);

// isGone reports a "card not found" failure: the card no longer exists on the
// server, so an optimistic removal must NOT be rolled back (re-adding it would
// resurrect a phantom copy).
const isGone = (err: unknown) => errMessage(err).includes("card not found");

// isComplete mirrors board.Complete: an explicit done, or 100% with no stage
// (derived done) or on the recurrent stage (a finished recurrent card stays
// behind — Carry Over/Week reseed a fresh copy instead of dragging it).
const isComplete = (c: CardModel) =>
  c.stage === "done" ||
  ((!c.stage || c.stage === "recurrent") && (c.progress ?? 0) >= 100);

/** TeamBoard is the team as a people × zones grid for one day, filtered by team. */
export function TeamBoard({
  board,
  provider,
  me,
  selectedDate,
  onSelectDate,
  avatars,
  names,
  roster,
  teamFilter,
  onSetFilter,
  onAddTeam,
  onRemoveTeam,
  onRenameTeam,
  onReorderTeams,
  patchCard,
  addCard,
  replaceCard,
  removeCard,
  reorderCards,
  reload,
  track,
  presence,
  onError,
  onOpen,
}: TeamBoardProps) {
  const [selectedCardId, setSelectedCardId] = useState<string | null>(null);
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
  } | null>(null);
  const [columnOrder, setColumnOrder] = useState<string[]>(() => {
    try {
      const v = localStorage.getItem("aeman.columnOrder");
      return v ? (JSON.parse(v) as string[]) : [];
    } catch {
      return [];
    }
  });
  const [dragCol, setDragCol] = useState<string | null>(null);
  const [teamsModalOpen, setTeamsModalOpen] = useState(false);
  const [planCollapsed, setPlanCollapsed] = useState<boolean>(
    () => localStorage.getItem("aeman.planCollapsed") !== "false",
  );
  // Custom (drag-set) height of the expanded weekly plan; null = default.
  const [planHeight, setPlanHeight] = useState<number | null>(() => {
    const v = localStorage.getItem("aeman.planHeight");
    return v ? Number(v) : null;
  });

  // Drag the top edge of the weekly plan to resize its height.
  const startPlanResize = (e: React.MouseEvent) => {
    e.preventDefault();
    const onMove = (ev: MouseEvent) => {
      const h = window.innerHeight - ev.clientY;
      setPlanHeight(Math.max(120, Math.min(window.innerHeight * 0.85, h)));
    };
    const onUp = () => {
      document.removeEventListener("mousemove", onMove);
      document.removeEventListener("mouseup", onUp);
      document.body.style.userSelect = "";
    };
    document.body.style.userSelect = "none";
    document.addEventListener("mousemove", onMove);
    document.addEventListener("mouseup", onUp);
  };

  // Remember the weekly plan's expanded/collapsed state and height in the browser.
  useEffect(() => {
    localStorage.setItem("aeman.planCollapsed", String(planCollapsed));
  }, [planCollapsed]);
  useEffect(() => {
    if (planHeight === null) {
      localStorage.removeItem("aeman.planHeight");
    } else {
      localStorage.setItem("aeman.planHeight", String(planHeight));
    }
  }, [planHeight]);

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

  // Single-select: no filter shows all; otherwise match the card's group.
  const passesFilter = (card: CardModel): boolean =>
    teamFilter === null || teamFilter.includes(card.team ?? "");

  // Cards passing the team filter (the scope before applying the sprint).
  const inFilter = useMemo(
    () => board.cards.filter((c) => passesFilter(c)),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [board.cards, teamFilter],
  );

  // The Team grid places a card on its effective day: its sprint (sprintStart)
  // once materialized, but its scheduled day (startDate) while that is still in the
  // future. So a materialized card sits on its sprint's start date (including ones
  // created on later days), and a deferred card shows on its own future day,
  // rejoining the sprint day once today catches up.
  const filteredCards = useMemo(
    () =>
      inFilter.filter((c) => {
        // Subtasks render nested under their parent, never as grid rows.
        if (c.parent) {
          return false;
        }
        // A Project slot lives on the Project board until it joins a sprint —
        // its multi-week dates would otherwise put it in the day grid's
        // Unassigned column for every day it spans. The server's TeamGrid has
        // said so all along; this mirror of it did not, and the weekly fetch
        // brings the slots into the shared card pool where the mirror ran.
        if (c.epic && !c.sprintStart) {
          return false;
        }
        const today = todayIso();
        // A card with an end date spans a range: it shows on every day from its
        // start through its end (the calendar sets start…end).
        const inRange =
          !!c.startDate &&
          !!c.day &&
          c.day >= c.startDate &&
          selectedDate >= c.startDate &&
          selectedDate <= c.day;
        // A deferred / future-scheduled card (startDate past today) lives on
        // its own day (or range), and a CLOSED sprint's day keeps it as
        // history; it is hidden everywhere else until that day arrives. The
        // team's CURRENT sprint is never history: deferring is precisely the
        // act of taking the card out of the sprint in progress, so it must
        // leave that day at once (mirrors board.TeamGrid).
        if (c.startDate && c.startDate > today) {
          const pastSprintDay =
            !!c.sprintStart &&
            selectedDate === c.sprintStart &&
            c.sprintStart < today &&
            c.sprintStart !== currentSprint(board, c.team ?? null);
          return selectedDate === c.startDate || inRange || pastSprintDay;
        }
        if (c.sprintStart === selectedDate) {
          return true;
        }
        // A materialized card also shows on its scheduled day (and through its
        // range when it has an end date), so a card created on a later day of
        // its sprint appears both on the sprint's start day and on its own days.
        if (inRange || (c.startDate && c.startDate === selectedDate)) {
          return true;
        }
        // A card also shows on a sprint day it passed through — a sprint-pointer
        // day S (current or previous) with origin <= S < sprintStart — so
        // carried-over and deferred cards keep their sprint history.
        const ss = c.sprintStart;
        if (!ss) {
          return false;
        }
        const teamKey = c.team ?? null;
        const origin = activeSprint(board, teamKey, c.startDate ?? ss);
        return [
          currentSprint(board, teamKey),
          previousSprint(board, teamKey),
        ].some((s) => !!s && selectedDate === s && s < ss && origin <= s);
      }),
    [inFilter, selectedDate, board],
  );

  // The sprint-jump button. Standing on the current sprint, it offers the
  // previous one ("« Last sprint"). Standing anywhere else, it returns to the
  // current sprint, the arrow pointing the way ("Current sprint »" when it is
  // ahead in time, "« Current sprint" when it is behind). Uses the filtered
  // team's sprint, or the latest across teams when unfiltered. Null = nowhere to
  // jump.
  const sprintJump = useMemo<{
    target: string;
    label: string;
    dir: "back" | "fwd";
  } | null>(() => {
    let cur: string | null;
    let prev: string | null;
    if (teamFilter?.length === 1) {
      cur = currentSprint(board, teamFilter[0]);
      prev = previousSprint(board, teamFilter[0]);
    } else {
      cur = null;
      prev = null;
      for (const s of Object.values(board.sprintStates)) {
        if (s.current && (cur === null || s.current > cur)) {
          cur = s.current;
        }
        if (s.previous && (prev === null || s.previous > prev)) {
          prev = s.previous;
        }
      }
    }
    if (cur === null) {
      return null;
    }
    if (selectedDate === cur) {
      return prev ? { target: prev, label: "Last sprint", dir: "back" } : null;
    }
    return selectedDate < cur
      ? { target: cur, label: "Current sprint", dir: "fwd" }
      : { target: cur, label: "Current sprint", dir: "back" };
  }, [board, teamFilter, selectedDate]);

  const currentWeek = useMemo(() => mondayOf(selectedDate), [selectedDate]);

  // Founders' weekly-plan cards for the filtered team and current week, split
  // into the two bands (by Wednesday / by Friday).
  const weekly = useMemo(() => {
    const wed: CardModel[] = [];
    const fri: CardModel[] = [];
    // A card shows in its own week — and, mirroring the day grid's sprint
    // history, in every past week it was actually worked in: taken into work
    // (it has a start date) and carried forward past that week. Carrying the
    // plan forward does not erase the weeks it was worked in.
    const showsInWeek = (c: CardModel): boolean => {
      if (c.week === currentWeek) {
        return true;
      }
      // A debt follows you: a card from an earlier week, still open past the
      // day it was owed by, belongs on the CURRENT week's panel beside that
      // week's own work. The server decided it is overdue (board.Overdue)
      // and served it; this mirror of the server's week rule used to drop
      // it on the floor — which is what happens when a rule is copied.
      if (
        c.overdue &&
        !!c.week &&
        c.week < currentWeek &&
        currentWeek === mondayOf(todayIso())
      ) {
        return true;
      }
      // A Project-board slot spans weeks by design: it belongs to every
      // week between its two boundaries, not only the one it starts in.
      if (
        isSlot(c) &&
        (c.week as string) <= currentWeek &&
        currentWeek <= mondayOf(c.day as string)
      ) {
        return true;
      }
      // History only in weeks whose working days are over (past the week's
      // Friday): while a week runs, a card pushed to a future week leaves its
      // panel — it is not this week's work anymore.
      if (todayIso() <= addDays(currentWeek, 4)) {
        return false;
      }
      // The "began work" anchor is the earliest of the start date and the
      // sprint join — take-into-plan sets the sprint, not always a start date.
      let started = c.startDate ?? "";
      if (c.sprintStart && (!started || c.sprintStart < started)) {
        started = c.sprintStart;
      }
      return (
        !!started &&
        !!c.week &&
        c.week > currentWeek &&
        mondayOf(started) <= currentWeek
      );
    };
    for (const c of board.cards) {
      // A band-less Project-board slot is still the week's work — its span
      // is its plan and its band derives from the end date. Any other
      // band-less card stays off the panel.
      if ((!c.plan && !isSlot(c)) || !showsInWeek(c) || !passesFilter(c)) {
        continue;
      }
      // A DEBT — owed in a week already past, shown here beside this
      // week's own work — stands in the by-Wednesday band: its own band
      // belonged to the week it missed, and what it faces now is the
      // nearest deadline of the week it is standing in.
      const owed = owedIn(c);
      if (owed !== "" && owed < currentWeek) {
        wed.push(c);
        continue;
      }
      if (!c.plan) {
        (slotBand(currentWeek, c.day as string) === "wed" ? wed : fri).push(c);
        continue;
      }
      // A week-history entry (the card has moved on to a later week) sits in
      // the by-Friday band of the past week: it stayed open through that
      // week's end; its current band describes the week it lives in now.
      (c.plan === "fri" || c.week !== currentWeek ? fri : wed).push(c);
    }
    return { wed, fri };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [board.cards, currentWeek, teamFilter]);

  // Overall completion across all plan cards (a done card counts as 100%).
  const planProgress = useMemo(() => {
    // Recurrent plan cards are excluded: the bar describes the week's one-off
    // work, while recurrent tasks restart every week and would skew it.
    const all = [...weekly.wed, ...weekly.fri].filter(
      (c) => c.stage !== "recurrent",
    );
    if (all.length === 0) {
      return 0;
    }
    const sum = all.reduce(
      (s, c) => s + (c.stage === "done" ? 100 : c.progress ?? 0),
      0,
    );
    return Math.round(sum / all.length);
  }, [weekly]);

  const handleCreatePlan = (
    plan: "wed" | "fri",
    title: string,
    team?: string | null,
  ) => {
    const tempId = `tmp-${new Date().toISOString()}`;
    const optimistic: CardModel = {
      itemId: tempId,
      // A bare GitHub reference reads as its readable label at once; the
      // server's background resolve renames it to the real title shortly.
      title: optimisticTitle(title),
      assignees: [],
      plan,
      week: currentWeek,
      team: team ?? undefined,
      createdAt: new Date().toISOString(),
      description: "",
      notes: [],
    };
    addCard(optimistic);
    const creating = provider.createCard({
      title,
      plan,
      week: currentWeek,
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
          void provider.deleteCard(card.itemId).catch(() => undefined);
          return;
        }
        // Swap in place: append-on-ack would reshuffle a quick burst of adds.
        replaceCard(tempId, card);
        migrateCardId(tempId, card.itemId);
      })
      .catch((err: unknown) => {
        consumePendingCancel(tempId);
        removeCard(tempId);
        onError(errMessage(err));
      });
  };

  // Move a plan card between the two bands (changes its Wed/Fri deadline).
  const handleSetPlan = (card: CardModel, plan: "wed" | "fri") => {
    const prev = card.plan;
    patchCard(card.itemId, { plan });
    void provider
      .patchCard(card.itemId, { plan: { band: plan } })
      .then(addCard)
      .catch((err: unknown) => {
        patchCard(card.itemId, { plan: prev });
        onError(errMessage(err));
      });
  };

  // Move a single plan card to another plan week (+1/+2 weeks, or a picked one).
  const handleSetWeek = (card: CardModel, week: string | null) => {
    const prev = card.week ?? null;
    // A SLOT has no week of its own — its row is the week of its start date —
    // so moving one to another week means moving its dates there. Sending
    // plan.week for it is refused (422), which turned this control into an
    // error message for every slot that a team assignment put in the plan.
    if (card.epic && week) {
      // How many weeks the slot spans, so moving it keeps its length.
      const span =
        card.startDate && card.day
          ? Math.max(0, weeksBetween(mondayOf(card.startDate), mondayOf(card.day)))
          : 0;
      const prevDates = { startDate: card.startDate, day: card.day };
      const end = addDays(week, span * 7 + 4);
      patchCard(card.itemId, { week, startDate: week, day: end });
      void provider
        .patchCard(card.itemId, { dates: { start: week, end } })
        .then(addCard)
        .catch((err: unknown) => {
          patchCard(card.itemId, prevDates);
          onError(errMessage(err));
        });
      return;
    }
    patchCard(card.itemId, { week: week ?? undefined });
    void provider
      .patchCard(card.itemId, { plan: { week: week ?? "" } })
      .then(addCard)
      .catch((err: unknown) => {
        patchCard(card.itemId, { week: prev ?? undefined });
        onError(errMessage(err));
      });
  };

  // Take a grid card into the weekly plan: mark it for the dropped band and the
  // current week, while it stays assigned on the board (the same card). It then
  // shows the weekly stripe in the grid and the "taken" tint in the plan.
  const takeIntoPlan = (card: CardModel, band: "wed" | "fri") => {
    const prev: Partial<CardModel> = { plan: card.plan, week: card.week };
    patchCard(card.itemId, { plan: band, week: currentWeek });
    void provider
      .patchCard(card.itemId, { plan: { band, week: currentWeek } })
      .then(addCard)
      .catch((err: unknown) => {
        patchCard(card.itemId, prev);
        onError(errMessage(err));
      });
  };

  // Take a plan card into work: assign it to the column's person and add it to
  // today's daily sprint, while it stays in the weekly plan (the same card).
  // One server action; dropping on the Unassigned column has no engineer to
  // send, so it falls back to a plain spec patch with the same outcome.
  const takePlanCard = (card: CardModel, engineer: string, dropZone?: ZoneKey) => {
    const login = engineer === UNASSIGNED ? null : engineer;
    const zone = dropZone ?? card.zone ?? "gray";
    // The optimistic sprint mirrors the server: the team's current sprint,
    // falling back to the selected day when the team has none yet.
    const sprintStart = currentSprint(board, card.team ?? null) ?? selectedDate;
    const prev: Partial<CardModel> = {
      assignees: card.assignees,
      zone: card.zone,
      sprintStart: card.sprintStart,
      startDate: card.startDate,
      day: card.day,
    };
    // A card scheduled for a later day is deferred, and the deferral outranks
    // the sprint in the grid: taking it into work moves its near end onto the
    // day it was taken, or the row would land nowhere and the drop would look
    // like it did nothing. The server does the same (TakeIntoPlan).
    const deferred = !!card.startDate && card.startDate > selectedDate;
    patchCard(card.itemId, {
      assignees: login ? [login] : [],
      zone,
      sprintStart,
      ...(deferred
        ? {
            startDate: selectedDate,
            ...(card.day && card.day < selectedDate ? { day: selectedDate } : {}),
          }
        : {}),
    });
    const request = login
      ? provider.takeIntoPlan(card.itemId, login, zone, selectedDate)
      : provider.patchCard(card.itemId, {
          assignees: [],
          zone,
          dates: { sprint: sprintStart },
        });
    void request.then(addCard).catch((err: unknown) => {
      patchCard(card.itemId, prev);
      onError(errMessage(err));
    });
  };

  // Columns are PEOPLE: the distinct assignees among the filtered cards (me
  // first). Columns come from everyone with a card in the selected teams in ANY
  // sprint (past or future), so a person's column stays (empty) on days they
  // have no cards. The Unassigned column is always shown for triage.
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

  // "No team" is offered only when the no-team group is actually displayed
  // on the board — a card created into a hidden group would just vanish.
  const pickerNoTeam = (teamFilter ?? roster).includes("");

  // People to offer when reassigning a card: the BOARD's member roster plus
  // everyone seen on a card. Cards alone are not enough — in a team-filtered
  // view they only name that team's people, and handing a weekly-plan card
  // to someone outside the filter (the whole point of assigning) offered an
  // empty seat. MeBoard already does it this way.
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
      if (c.parent && subtaskOnDay(c)) {
        const list = m.get(c.parent) ?? [];
        list.push(c);
        m.set(c.parent, list);
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
  const bandKey = (band: "wed" | "fri") => `band::${band}`;

  // Sortable groups: one per grid cell, plus the two weekly-plan bands. The bands
  // share the same dnd engine (namespaced "plan:" ids) so a plan card can reorder
  // in its band, move between bands, and be dragged into the grid with a live
  // preview — the same card just stays in the plan.
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
    for (const band of ["wed", "fri"] as const) {
      out.push({
        key: bandKey(band),
        meta: { kind: "band", band },
        // An expanded weekly parent shows its subtask rows nested under it
        // (they are not plan cards — they just ride along visibly).
        cards: weekly[band].flatMap((c) =>
          subsOpen(c.itemId) ? [c, ...(childrenOf.get(c.itemId) ?? [])] : [c],
        ),
      });
    }
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filteredCards, orderedEngineers, weekly, childrenOf, expandedSubs]);

  // Reorder a subset of cards (a band's plan cards) within the global card order
  // and persist the dragged card's new position.
  const reorderPlanCards = (card: CardModel, order: string[]) => {
    const subset = new Set(order);
    let i = 0;
    const next = board.cards.map((c) =>
      subset.has(c.itemId) ? order[i++] : c.itemId,
    );
    reorderCards(next);
    // Persist anchored on a BAND neighbour, never on the local global order:
    // the board list here is a merge of the grid and weekly fetches, so
    // non-band neighbours don't reflect the true project order. After the
    // new predecessor — or, for the top slot, before the card now second.
    const idx = order.indexOf(card.itemId);
    const persist =
      idx > 0
        ? provider.moveCard(card.itemId, order[idx - 1])
        : order.length > 1
          ? provider.moveCardBefore(card.itemId, order[1])
          : null;
    void persist?.catch((err: unknown) => {
      onError(errMessage(err));
      reload();
    });
  };

  const handleDrop = ({
    card,
    fromMeta,
    toMeta,
    groups: g,
    groupUnder,
    groupPositional,
  }: DropResult<TeamMeta>) => {
    // Drops that involve a weekly-plan band.
    if (fromMeta.kind === "band" || toMeta.kind === "band") {
      // A drop whose slot nests it under a weekly parent groups it — but
      // ONLY as a held gesture (indent / middle-band dwell): a band drop
      // that merely lands inside an expanded block aims at the PLAN, and
      // silently grouping it was how "take into plan" seemed broken.
      if (!card.parent && toMeta.kind === "band" && groupUnder && !groupPositional) {
        handleGroup(card, groupUnder);
        return;
      }
      if (card.parent && toMeta.kind === "band") {
        if (groupUnder && groupUnder !== card.parent) {
          handleGroup(card, groupUnder); // regroup under another weekly parent
          return;
        }
        const entry = g.find((x) => x.ids.includes(`plan:${card.itemId}`));
        const ids = (entry?.ids ?? []).map((id) => id.replace(/^plan:/, ""));
        if (!groupUnder) {
          // Pulled out INSIDE the weekly panel: the subtask becomes a plan
          // card of the dropped band in its own right, keeping its slot. A
          // GRID parent's subtask has no business here — it snaps back.
          const parentCard = card.parent ? cardsById.get(card.parent) : undefined;
          if (!parentCard?.plan) {
            return;
          }
          // A slot's week derives from its start date: the pull-out must
          // not write the PARENT's week onto it — that conflicting write is
          // refused, and refusing the whole patch kept the subtask stuck.
          const slot = isSlot(card);
          const week = parentCard.week ?? currentWeek;
          const prev: Partial<CardModel> = {
            parent: card.parent,
            plan: card.plan,
            week: card.week,
          };
          patchCard(card.itemId, {
            parent: undefined,
            plan: toMeta.band,
            ...(slot ? {} : { week }),
          });
          void provider
            .patchCard(card.itemId, {
              parent: "",
              plan: slot ? { band: toMeta.band } : { band: toMeta.band, week },
            })
            .then((c) => {
              addCard(c);
              reorderPlanCards(c, ids);
              reload();
            })
            .catch((err: unknown) => {
              patchCard(card.itemId, prev);
              onError(errMessage(err));
            });
          return;
        }
        // Reorder within the own block, anchored on the final sibling order.
        const idx = ids.indexOf(card.itemId);
        const above = idx > 0 ? cardsById.get(ids[idx - 1]) : undefined;
        const below =
          idx >= 0 && idx + 1 < ids.length
            ? cardsById.get(ids[idx + 1])
            : undefined;
        const persist =
          above && above.parent === card.parent
            ? provider.moveCard(card.itemId, above.itemId)
            : below && below.parent === card.parent
              ? provider.moveCardBefore(card.itemId, below.itemId)
              : null;
        void persist
          ?.then(reload)
          .catch((err: unknown) => {
            onError(errMessage(err));
            reload();
          });
        return;
      }
      if (!card.parent && fromMeta.kind === "band" && toMeta.kind === "cell") {
        // Take the plan card into work, in the dropped cell's zone.
        takePlanCard(card, toMeta.engineer, toMeta.zone);
        return;
      } else if (fromMeta.kind === "band" && toMeta.kind === "band") {
        if (toMeta.band !== fromMeta.band) {
          handleSetPlan(card, toMeta.band);
        }
        const entry = g.find(
          (x) => x.meta.kind === "band" && x.meta.band === toMeta.band,
        );
        if (entry) {
          reorderPlanCards(
            card,
            entry.ids.map((id) => id.replace(/^plan:/, "")),
          );
        }
      } else if (!card.parent && fromMeta.kind === "cell" && toMeta.kind === "band") {
        // Take a grid card into the weekly plan; it stays on the board.
        takeIntoPlan(card, toMeta.band);
      }
      // A subtask never leaves the weekly panel for the day grid by drag:
      // dedent it within the band to make it a plan card first.
      return;
    }
    if (toMeta.kind !== "cell") {
      return; // narrows the type: everything below places into a grid cell
    }

    // From here both ends are grid cells. The board committed exactly what
    // the placeholder previewed: groupUnder is the already-validated parent
    // to nest under (null = standalone).
    const parentTo = groupUnder;
    const parentChanged = (card.parent ?? "") !== (parentTo ?? "");

    // 1) Optimistic local state first. A grouped card is not cell-placed, so
    // zone/assignee only apply to standalone drops (including a pull-out).
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
        optimistic.assignees = wantAssignees;
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
      selectedBy={selectedByFor(card)}
      onLoadLinks={loadCardLinks}
      selected={card.itemId === selectedCardId}
      onSelect={(c) => setSelectedCardId(c.itemId)}
      onProgress={handleProgress}
      onDelete={handleGridDelete}
      placements={placementsFor(card)}
      // No exception for a subtask: gridRemoval already answers for it, and
      // hard-coding "ask nothing" here put a "Delete «…»?" in front of an ×
      // that ungroups the card and keeps it — the very reading of the ×
      // this shared rule exists to prevent.
      boardAsks={
        boardAsksAbout(
          card,
          removalKind(card, gridCtx(card)) === "ask"
            ? "ask"
            : gridRemoval(card, gridCtx(card)),
          reviewOf(card),
        )
      }
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
      const par = cardsById.get(card.parent);
      // Under a taken weekly parent the subtask rows dim with it.
      const dim = !!par?.plan && (par?.assignees.length ?? 0) > 0;
      const wrapped = (
        <div className={`subtask-indent${dim ? " subtask-indent-taken" : ""}`}>
          {node}
        </div>
      );
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

  const selectedByFor = (card: CardModel): string[] | undefined => {
    if (!presence) {
      return undefined;
    }
    const logins = Object.entries(presence)
      .filter(([, uid]) => {
        if (uid === card.itemId) {
          return true;
        }
        // A collapsed parent wears its subtasks' presence; expanded, the marks
        // sit on the subtask rows themselves.
        if (!card.parent && !subsOpen(card.itemId)) {
          return (childrenOf.get(card.itemId) ?? []).some((c) => c.itemId === uid);
        }
        return false;
      })
      .map(([login]) => login);
    return logins.length > 0 ? logins : undefined;
  };

  // Resolve a card's description links (GitHub refs get titles) for the menu.
  const loadCardLinks = (card: CardModel) =>
    provider.listLinks(card.itemId);

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
      .patchCard(card.itemId, { progress: value })
      .then((updated) => {
        addCard(updated);
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
      .setInProgress(card.itemId)
      .then((updated) => {
        addCard(updated);
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

  // The team's explicit previous sprint — where deleting a grid card demotes it
  // (back to last sprint) instead of removing it.
  const previousSprintFor = (card: CardModel): string | null =>
    previousSprint(board, card.team ?? null);

  // The grid ×: one remove intent — the server demotes a card still in the
  // team's current sprint, releases a taken plan card back to plan-only, or
  // deletes for real (cascading the linked review card). The optimistic patch
  // mirrors those rules locally; the re-list converges the server's outcome.
  // The × asks the same question on every board: gridCtx names the sprints
  // it is asked in, reviewOf the card a delete would take along.
  const gridCtx = (card: CardModel) => ({
    current: currentSprint(board, card.team ?? null) ?? undefined,
    previous: previousSprintFor(card) ?? undefined,
    today: todayIso(),
  // Whether the column a subtask carries can come with it out of the group:
  // the answer decides whether the × ungroups the card or hands it to the
  // ordinary law (demote, or delete), and only the roster knows. A review
  // card's link outranks its own team and project, so the card it points at
  // answers for it.
    columnFollows: columnFollows(card, { ...rosterOf(board), epics: board.epics, teamDomains: board.teamDomains }, linkedDomainOf(card)),
  });
  // The repository of the card a subtask would still point at once its
  // parent is gone: its original, or the task it iterates.
  const linkedDomainOf = (card: CardModel): string | undefined => {
    const ref = card.reviewOf || card.task;
    if (!ref) {
      return undefined;
    }
    return board.cards.find((c) => c.itemId === ref)?.domain ?? undefined;
  };
  const reviewOf = (card: CardModel) =>
    board.cards.find((c) => c.reviewOf === card.itemId)?.title ?? null;

  // placementsFor: the assign menu's attach/mirror section — one shared
  // factory (makeCardPlacements), so the boards cannot drift apart.
  const placementsFor = (card: CardModel): CardPlacements | undefined => {
    // A subtask needs no check here: placementTargets refuses one for
    // every board, so the rule cannot drift between the three.
    return makeCardPlacements(card, board, {
      provider,
      patchCard,
      reload,
      onError,
      errMessage,
    });
  };

  const handleGridDelete = (card: CardModel, forced?: "demote") => {
    if (card.itemId.startsWith("tmp-")) {
      cancelPendingCard(card.itemId);
      removeCard(card.itemId);
      return;
    }
    // A worked-on card that would be demoted leaves today's board silently,
    // subtasks and all — that reads as deletion. Ask which one they mean.
    // A SUBTASK reaches a demote only when the column it carried cannot
    // follow it out of the group, and the question is the same one then:
    // gridGesture answers for both, so the two boards ask alike.
    if (!forced && !card.plan) {
      const kind = card.parent
        ? gridGesture(card, gridCtx(card))
        : removalKind(card, gridCtx(card));
      if (kind === "ask") {
        setRemoveChoice(card);
        return;
      }
    }
    // A subtask with nowhere else to be has no sprint history of its own:
    // the × deletes it outright, gone from under its parent immediately.
    // One standing in a COLUMN is a different card (G57): the server
    // ungroups it and leaves it there, so this board asks gridRemoval like
    // it does for everything else instead of sending a DELETE past the
    // rule — which destroyed work the Me board would have kept.
    if (card.parent && gridRemoval(card, gridCtx(card)) === "delete") {
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
    if (card.parent) {
      // Out of the group — into its column, or, when that column belongs to
      // the parent's repository and cannot follow, back a sprint with the
      // column dropped (the server's answer, gridRemoval). Either way the
      // row must stop being drawn under its parent at once, or the × looks
      // inert; and the demote must show the SAME optimistic state the Me
      // board shows, or one gesture reads two ways on two boards.
      const demoted = gridRemoval(card, gridCtx(card)) === "demote";
      const back = demoted ? previousSprintFor(card) ?? undefined : undefined;
      const prev: Partial<CardModel> = {
        assignees: card.assignees,
        sprintStart: card.sprintStart,
        startDate: card.startDate,
        parent: card.parent,
        epic: card.epic,
        project: card.project,
      };
      patchCard(card.itemId, {
        assignees: [],
        sprintStart: back,
        parent: undefined,
        ...(demoted ? { startDate: back, epic: undefined, project: undefined } : {}),
      });
      void provider
        .removeCard(card.itemId, "grid")
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
    let rollback: () => void;
    if (!card.plan) {
      const cur = currentSprint(board, card.team ?? null);
      const prevSprint = previousSprintFor(card);
      const inCurrent = !!card.sprintStart && !!cur && card.sprintStart === cur;
      // A card created today has no sprint history worth keeping: delete for
      // real instead of demoting (mirrors boardservice.Remove).
      if (inCurrent && cur && prevSprint && prevSprint < cur && card.startDate !== todayIso()) {
        // Demote to the previous sprint, all dates pulled along (the end date
        // too, or the [start…end] range would keep the card on its old day).
        const prev: Partial<CardModel> = {
          startDate: card.startDate,
          sprintStart: card.sprintStart,
          day: card.day,
        };
        patchCard(card.itemId, {
          startDate: prevSprint,
          sprintStart: prevSprint,
          ...(card.day ? { day: prevSprint } : {}),
        });
        rollback = () => patchCard(card.itemId, prev);
      } else {
        // Nothing to demote into. A card has two homes — the working area and
        // the weekly plan — and this × empties the first (mirrors
        // boardservice.Remove). With a plan band or a Project-board column to
        // fall back on it goes there, leaving the working area entirely,
        // dates and all: a card whose start is the viewed day shows on the
        // grid whatever its sprint says, which is how a handed-back card used
        // to sit in two places at once. With nowhere else to be, the working
        // area was the only place it was, and removing it from there deletes
        // it — after a question when the card carries work or a review card,
        // which the delete takes with it. Subtasks survive as standalone
        // cards and are not asked about.
        if (gridRemoval(card, gridCtx(card)) === "delete") {
          const linkedReview = board.cards.find((c) => c.reviewOf === card.itemId);
          const warning = deleteWarning(card, linkedReview?.title ?? null);
          if (warning && !window.confirm(warning)) {
            return;
          }
          // The subtasks are freed, not lost: locally too, or they vanish
          // until a refresh — the watch skips this tab's own changes.
          for (const f of freeSubtasks(board.cards, card.itemId)) {
            patchCard(f.itemId, f.patch);
          }
          removeCard(card.itemId);
          if (linkedReview) {
            removeCard(linkedReview.itemId);
          }
          void provider
            .removeCard(card.itemId, "grid")
            .then(() => reload())
            .catch((err: unknown) => {
              if (!isGone(err)) {
                onError(errMessage(err));
                reload();
              }
            });
          return;
        }
        const prev: Partial<CardModel> = {
          assignees: card.assignees,
          sprintStart: card.sprintStart,
          startDate: card.startDate,
          day: card.day,
        };
        patchCard(card.itemId, {
          assignees: [],
          sprintStart: undefined,
          // A slot keeps its dates: they are its row on the Project board.
          // Only the epic side makes one — a bare project name is a label.
          ...(hasColumn(card) ? {} : { startDate: undefined, day: undefined }),
        });
        rollback = () => patchCard(card.itemId, prev);
      }
    } else {
      // The card is also in the weekly plan: this × takes it out of the
      // working area and leaves it there, whatever it carries. (The grid ×
      // used to do the opposite for a worked card — shed the band, stay on
      // the grid — so one gesture meant two things depending on the bar.)
      const prev: Partial<CardModel> = {
        assignees: card.assignees,
        sprintStart: card.sprintStart,
        startDate: card.startDate,
        day: card.day,
      };
      patchCard(card.itemId, {
        assignees: [],
        sprintStart: undefined,
        ...(hasColumn(card) ? {} : { startDate: undefined, day: undefined }),
      });
      rollback = () => patchCard(card.itemId, prev);
    }
    void provider
      .removeCard(card.itemId, "grid")
      .then(() => reload())
      .catch((err: unknown) => {
        if (isGone(err)) {
          return;
        }
        rollback();
        onError(errMessage(err));
      });
  };

  // The plan-band ×: it empties one of the card's homes. A card that is
  // somewhere else — in a Project-board column, or in the working area by
  // its sprint or its dates — only loses its band and week; with the plan
  // its last home, it is deleted, whatever it carries. planRemoval is that
  // rule; the optimistic patch mirrors it and the re-list converges.
  const removeFromPlan = (card: CardModel) => {
    if (card.itemId.startsWith("tmp-")) {
      cancelPendingCard(card.itemId);
      removeCard(card.itemId);
      return;
    }
    let rollback: () => void;
    if (planRemoval(card) === "delete") {
      // The plan was this card's last home, so the × deletes it (a
      // previous-week demote would boomerang back on the next carry-week);
      // the server cascades to a linked review card and frees the subtasks.
      // A card in a Project-board column is never deleted here — that column
      // is where it goes home.
      const linkedReview = board.cards.find((c) => c.reviewOf === card.itemId);
      // The plan × deletes here, so the loss is named the way the grid ×
      // names it — the anonymous "Delete?" said nothing about the progress
      // it was taking.
      const warning = deleteWarning(card, linkedReview?.title ?? null);
      if (warning && !window.confirm(warning)) {
        return;
      }
      for (const f of freeSubtasks(board.cards, card.itemId)) {
        patchCard(f.itemId, f.patch);
      }
      removeCard(card.itemId);
      if (linkedReview) {
        removeCard(linkedReview.itemId);
      }
      rollback = () => {
        addCard(card);
        if (linkedReview) {
          addCard(linkedReview);
        }
      };
    } else {
      const prev: Partial<CardModel> = { plan: card.plan, week: card.week };
      // A slot's week is its row on the Project board: the server keeps it
      // (clearPlanWeek is only reached for a card with no column), and
      // clearing it here made the slot jump out of its row until the re-list
      // put it back.
      patchCard(card.itemId, {
        plan: undefined,
        ...(card.epic ? {} : { week: undefined }),
      });
      rollback = () => patchCard(card.itemId, prev);
    }
    void provider
      .removeCard(card.itemId, "plan")
      .then(() => reload())
      .catch((err: unknown) => {
        if (isGone(err)) {
          return;
        }
        rollback();
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
    // personal boards.
    const target = card.parent ? cardsById.get(card.parent) ?? card : card;
    const next = card.parent
      ? target.assignees.slice(0, 1)
      : login
        ? [login]
        : [];
    const family = card.parent
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
    const sprintStart = currentSprint(board, team) ?? selectedDate;
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
      // The 10-90 clamp is stored on the review pick (mirrors board.ApplyStage).
      progress: Math.min(90, Math.max(10, card.progress ?? 0)),
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
    // A future day has no sprint yet: the card waits sprint-less and the
    // carry-over reaching its day adopts it (mirrors boardservice.SetDates).
    const sprint = !start
      ? start
      : start > todayIso()
        ? null
        : activeSprint(board, card.team ?? null, start) || start;
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
    const full = !!card.createdAt && localDateIso(card.createdAt) === today;
    const newDay =
      full && card.day && card.day < newStart ? newStart : card.day;
    const prev = {
      startDate: card.startDate,
      sprintStart: card.sprintStart,
      day: card.day,
    };
    patchCard(card.itemId, {
      startDate: newStart,
      ...(full ? { sprintStart: newStart, day: newDay } : {}),
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
  ) => {
    const sprint = currentSprint(board, team);
    const firstSprint = !noSprint && sprint === null;
    const tempId = `tmp-${new Date().toISOString()}`;
    const optimistic: CardModel = {
      itemId: tempId,
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
    const creating = provider.createCard({
      title,
      zone,
      day,
      start: startDate,
      assigneeLogin: engineer || null,
      team,
      noSprint: noSprint || undefined,
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
  ) => {
    const sprint = currentSprint(board, team ?? null);
    if (sprint !== null && selectedDate > sprint) {
      setSprintChoice({ engineer, zone, title, team: team ?? null, sprint });
      return;
    }
    createTeamCard(
      engineer,
      zone,
      title,
      team ?? null,
      selectedDate,
      selectedDate,
    );
  };

  // Carry over: advance the team's sprint to today (the prior current becomes
  // the previous) and pull its unfinished cards forward — one server action; a
  // dry run feeds the confirm count. Always advances, even with nothing to
  // carry. `team` is null for the no-team group.
  const startSprint = async (team: string | null) => {
    setSprintMenuOpen(false);
    const label = team ?? "no team";
    const old = currentSprint(board, team);
    const today = todayIso();
    // Idempotent: if the sprint is already today's, do not re-advance — that would
    // overwrite the previous sprint, making previous = current = today. Still land
    // on today, so pressing Carry over always brings the current sprint into view.
    if (old === today) {
      onSelectDate(today);
      onError(`«${label}» is already on today's sprint.`);
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
    for (const c of board.cards) {
      const sameTeam = team === null ? c.team == null : c.team === team;
      if (!sameTeam || isComplete(c) || c.itemId.startsWith("tmp-")) {
        continue;
      }
      // Sprint-less day cards scheduled past the closing sprint whose day has
      // arrived ("next sprint" creates) are adopted by the sprint being
      // started, mirroring CarryOver; older sprint-less strays stay put.
      const adopted =
        !c.sprintStart &&
        !c.plan &&
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

  return (
    <div className="team">
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
          teams={roster}
          selectedKeys={teamFilter}
          onSelect={onSetFilter}
          domains={board.domains}
          onAdd={onAddTeam}
          onRemove={onRemoveTeam}
          onRename={onRenameTeam}
          noneChip={board.cards.some((c) => !c.team) ? "No team" : undefined}
          canManage={false}
          onManage={() => setTeamsModalOpen(true)}
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
        <button
          type="button"
          className="btn"
          onClick={() => {
            if (sprintJump) {
              onSelectDate(sprintJump.target);
            }
          }}
          disabled={!sprintJump}
          title="Jump to a sprint's day"
          aria-label="Jump to sprint"
        >
          {sprintJump?.dir === "fwd"
            ? `${sprintJump.label} »`
            : `« ${sprintJump?.label ?? "Last sprint"}`}
        </button>
        <button
          type="button"
          className="btn"
          onClick={() => onSelectDate(todayIso())}
          disabled={selectedDate === todayIso()}
          title="Jump to today"
        >
          Today
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
        idForCard={(c, g) =>
          g.meta.kind === "band" ? `plan:${c.itemId}` : c.itemId
        }
        onDrop={handleDrop}
        onGroupDrop={handleGroup}
        onHoverCard={setGroupHover}
        onDragActiveCard={handleDragActive}
        canGroup={canGroup}
        renderCard={(card, group) =>
          withSubs(
            card,
            group.meta.kind === "band" ? (
            <Card
              card={card}
              selectedBy={selectedByFor(card)}
              onLoadLinks={loadCardLinks}
              selected={card.itemId === selectedCardId}
              onSelect={(c) => setSelectedCardId(c.itemId)}
              onProgress={handleProgress}
              // The panel's × is the PLAN's gesture for every card on it,
              // subtasks included: `from` picks which home is emptied
              // (G57), and routing subtasks to the grid handler made one ×
              // mean the other's — it ungrouped the card and emptied the
              // working area in answer to a click on the plan. A subtask
              // standing in a column is a slot like any other and gets no
              // × at all: its plan membership is derived from its dates,
              // so there is nothing here to empty.
              onDelete={removeFromPlan}
              placements={placementsFor(card)}
              deletable={planRemoveOffered(card)}
              boardAsks={boardAsksAbout(card, planRemoval(card), reviewOf(card))}
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
              weekMode
              onSetWeek={handleSetWeek}
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
            ) : (
              renderGridCard(card)
            ),
          )
        }
          renderOverlay={(card) => (
            <Card
              card={card}
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
          renderGroup={(group, body, { isOver, dropRef }) => {
            if (group.meta.kind === "band") {
              const band = group.meta.band;
              return (
                <div
                  ref={dropRef as Ref<HTMLDivElement>}
                  className={`team-weekly-band team-weekly-${band}${
                    isOver ? " team-weekly-band-drop" : ""
                  }`}
                >
                  {body}
                  <AddCard
                    forcedTeam={forcedTeam}
                    teams={pickerTeams}
                    allowNoTeam={pickerNoTeam}
                    placeholder="Plan task…"
                    onCreate={(title, team) =>
                      handleCreatePlan(band, title, team)
                    }
                  />
                </div>
              );
            }
            const { engineer, zone } = group.meta;
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
                <div className="zone-cards">
                  {body}
                  <AddCard
                    teams={forcedTeam === undefined ? pickerTeams : undefined}
                    forcedTeam={forcedTeam}
                    allowNoTeam={pickerNoTeam}
                    onCreate={(title, team) =>
                      handleCreate(engineer, zone, title, team)
                    }
                  />
                </div>
              </div>
            );
          }}
          renderLayout={(nodes) => (
            <>
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

              <div
                className={`team-weekly${planCollapsed ? " team-weekly-collapsed" : ""}`}
                style={
                  !planCollapsed && planHeight !== null
                    ? { height: planHeight, maxHeight: "none" }
                    : undefined
                }
              >
                <div className="team-weekly-top">
                  {!planCollapsed && (
                    <div
                      className="team-weekly-resize"
                      onMouseDown={startPlanResize}
                      title="Drag to resize"
                    />
                  )}
                  <div
                    className="team-weekly-progress"
                    title={`${planProgress}% done across the plan`}
                  >
                    <div
                      className="team-weekly-progress-fill"
                      style={{ width: `${planProgress}%` }}
                    />
                  </div>
                  <div className="team-weekly-head">
                    <span className="team-weekly-title">
                      Weekly plan · {currentWeek}
                    </span>
                    <div className="team-weekly-actions">
                      <button
                        type="button"
                        className="team-weekly-toggle"
                        onClick={() => setPlanCollapsed((c) => !c)}
                        aria-label={
                          planCollapsed
                            ? "Expand weekly plan"
                            : "Collapse weekly plan"
                        }
                        title={planCollapsed ? "Expand" : "Collapse"}
                      >
                        {planCollapsed ? "▲" : "▼"}
                      </button>
                    </div>
                  </div>
                </div>
                {!planCollapsed && (
                  <div className="team-weekly-bands">
                    {nodes.get(bandKey("wed"))}
                    {nodes.get(bandKey("fri"))}
                  </div>
                )}
              </div>
            </>
          )}
        />
      {teamsModalOpen && (
        <TeamsModal
          teams={roster}
          domains={board.domains}
          onAdd={onAddTeam}
          onRename={onRenameTeam}
          onRemove={onRemoveTeam}
          onReorder={onReorderTeams}
          onClose={() => setTeamsModalOpen(false)}
        />
      )}
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
            )
          }
        />
      )}
      {removeChoice && (
        <RemoveChoiceDialog
          title={removeChoice.title}
          progress={removeChoice.progress ?? 0}
          previous={previousSprintFor(removeChoice) ?? ""}
          subtasks={(childrenOf.get(removeChoice.itemId) ?? []).length}
          onClose={() => setRemoveChoice(null)}
          onSubmit={(hardDelete) => {
            const card = removeChoice;
            if (hardDelete) {
              for (const f of freeSubtasks(board.cards, card.itemId)) {
                patchCard(f.itemId, f.patch);
              }
              removeCard(card.itemId);
              void provider
                .deleteCard(card.itemId)
                .then(() => reload())
                .catch((err: unknown) => {
                  addCard(card);
                  onError(errMessage(err));
                  reload();
                });
              return;
            }
            handleGridDelete(card, "demote");
          }}
        />
      )}
    </div>
  );
}
