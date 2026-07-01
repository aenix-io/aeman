import {
  Fragment,
  useEffect,
  useMemo,
  useRef,
  useState,
  type Ref,
} from "react";
import type {
  Board,
  Card as CardModel,
  Provider,
  StageKey,
  ZoneKey,
} from "../providers/types";
import { ZONES, ZONE_ORDER, optionIdForZone } from "../zones";
import { fieldRoles } from "../providers/fields";
import { todayIso, addDays, mondayOf } from "../date";
import { activeSprint, currentSprint, previousSprint } from "../sprint";
import { teamColor } from "../avatar";
import { avatarUrlFor, displayName, type GhUser } from "../users";
import { Card } from "./Card";
import { AddCard } from "./AddCard";
import { Dropdown } from "./Dropdown";
import { TeamChips } from "./TeamChips";
import { TeamsModal } from "./TeamsModal";
import { SortableBoard, type BoardGroup, type DropResult } from "./SortableBoard";
import { globalOrderFromGroups, afterIdFor } from "./dndOrder";

interface TeamBoardProps {
  board: Board;
  provider: Provider;
  me: string;
  /** GitHub profiles (name + avatar) for assignees, keyed by login. */
  users: Record<string, GhUser>;
  /** Known teams (the roster), shown as filter chips. */
  roster: string[];
  /** Single-select team filter: null = all, "" = no team, else a team name. */
  teamFilter: string[] | null;
  onSetFilter: (keys: string[] | null) => void;
  onAddTeam: (team: string) => void;
  onRemoveTeam: (team: string) => void;
  onRenameTeam: (from: string, to: string) => void;
  onReorderTeams: (ordered: string[]) => void;
  patchCard: (itemId: string, patch: Partial<CardModel>) => void;
  addCard: (card: CardModel) => void;
  removeCard: (itemId: string) => void;
  reorderCards: (orderedItemIds: string[]) => void;
  reload: () => void;
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

/** TeamBoard is the team as a people × zones grid for one day, filtered by team. */
export function TeamBoard({
  board,
  provider,
  me,
  users,
  roster,
  teamFilter,
  onSetFilter,
  onAddTeam,
  onRemoveTeam,
  onRenameTeam,
  onReorderTeams,
  patchCard,
  addCard,
  removeCard,
  reorderCards,
  reload,
  onError,
  onOpen,
}: TeamBoardProps) {
  const [selectedDate, setSelectedDate] = useState<string>(todayIso());
  const [selectedCardId, setSelectedCardId] = useState<string | null>(null);
  const [sprintMenuOpen, setSprintMenuOpen] = useState(false);
  const sprintRef = useRef<HTMLDivElement | null>(null);
  const [carryWeekOpen, setCarryWeekOpen] = useState(false);
  const carryWeekRef = useRef<HTMLDivElement | null>(null);
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

  // A tab left open past midnight keeps a stale "today", so the weekly plan and
  // newly-planned cards (week = mondayOf(selectedDate)) would land on the old
  // week and vanish from the real current week. When the tab regains focus,
  // catch the selected day up to the real today — unless the user navigated to a
  // specific other day. (The grid create already reads the live date directly.)
  const lastToday = useRef(todayIso());
  useEffect(() => {
    const sync = () => {
      const now = todayIso();
      if (now === lastToday.current) {
        return;
      }
      setSelectedDate((d) => (d === lastToday.current ? now : d));
      lastToday.current = now;
    };
    document.addEventListener("visibilitychange", sync);
    window.addEventListener("focus", sync);
    return () => {
      document.removeEventListener("visibilitychange", sync);
      window.removeEventListener("focus", sync);
    };
  }, []);

  const roles = useMemo(() => fieldRoles(board), [board]);

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
        const eff =
          c.startDate && c.startDate > todayIso() ? c.startDate : c.sprintStart;
        if (eff === selectedDate) {
          return true;
        }
        // A future-deferred card only lives on its own day.
        if (c.startDate && c.startDate > todayIso()) {
          return false;
        }
        // A carried-over card also shows on the previous sprint's start day —
        // its origin — so scrolling back shows that sprint in full.
        const prev = previousSprint(board, c.team ?? null);
        if (
          !prev ||
          selectedDate !== prev ||
          !c.sprintStart ||
          c.sprintStart <= prev
        ) {
          return false;
        }
        const origin = activeSprint(
          board,
          c.team ?? null,
          c.startDate ?? c.sprintStart,
        );
        return origin <= prev;
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
    for (const c of board.cards) {
      if (!c.plan || c.week !== currentWeek || !passesFilter(c)) {
        continue;
      }
      (c.plan === "fri" ? fri : wed).push(c);
    }
    return { wed, fri };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [board.cards, currentWeek, teamFilter]);

  // Overall completion across all plan cards (a done card counts as 100%).
  const planProgress = useMemo(() => {
    const all = [...weekly.wed, ...weekly.fri];
    if (all.length === 0) {
      return 0;
    }
    const sum = all.reduce(
      (s, c) => s + (c.stage === "done" ? 100 : c.progress ?? 0),
      0,
    );
    return Math.round(sum / all.length);
  }, [weekly]);

  // Teams (and the no-team group) with unfinished plan cards this week — the
  // only carry-over targets worth offering in the dropdown.
  const carryable = useMemo(() => {
    const teams = new Set<string>();
    let noTeam = false;
    for (const c of [...weekly.wed, ...weekly.fri]) {
      if (c.stage === "done") {
        continue;
      }
      if (c.team) {
        teams.add(c.team);
      } else {
        noTeam = true;
      }
    }
    return { teams: [...teams], noTeam };
  }, [weekly]);

  const handleCreatePlan = (
    plan: "wed" | "fri",
    title: string,
    team?: string | null,
  ) => {
    const tempId = `tmp-${new Date().toISOString()}`;
    const optimistic: CardModel = {
      itemId: tempId,
      title,
      isDraft: true,
      assignees: [],
      plan,
      week: currentWeek,
      team: team ?? undefined,
      createdAt: new Date().toISOString(),
      description: "",
      notes: [],
    };
    addCard(optimistic);
    void provider
      .createCard(board, { title, plan, week: currentWeek, team: team ?? null })
      .then((card) => {
        removeCard(tempId);
        addCard(card);
      })
      .catch((err: unknown) => {
        removeCard(tempId);
        onError(errMessage(err));
      });
  };

  // Weekly carry over: pull a team's unfinished plan cards from earlier weeks
  // into the current (selected) week — the weekly analogue of the daily sprint
  // carry over, so a new week's empty plan can absorb last week's leftovers.
  const handleCarryWeek = (team: string | null) => {
    setCarryWeekOpen(false);
    const label = team ?? "no team";
    const carry = board.cards.filter(
      (c) =>
        c.plan &&
        c.week != null &&
        c.week < currentWeek &&
        c.stage !== "done" &&
        // Skip not-yet-persisted optimistic cards (temporary ids).
        !c.itemId.startsWith("tmp-") &&
        (team === null ? c.team == null : c.team === team),
    );
    if (carry.length === 0) {
      onError(
        `No unfinished plan cards from earlier weeks for "${label}" to carry into the week of ${currentWeek}.`,
      );
      return;
    }
    if (
      !window.confirm(
        `Carry over ${carry.length} unfinished plan card(s) for "${label}" into the week of ${currentWeek}?`,
      )
    ) {
      return;
    }
    for (const card of carry) {
      const prev = card.week;
      patchCard(card.itemId, { week: currentWeek });
      void provider.setWeek(board, card, currentWeek).catch((err: unknown) => {
        patchCard(card.itemId, { week: prev });
        onError(errMessage(err));
      });
    }
  };

  // Move a plan card between the two bands (changes its Wed/Fri deadline).
  const handleSetPlan = (card: CardModel, plan: "wed" | "fri") => {
    const prev = card.plan;
    patchCard(card.itemId, { plan });
    void provider.setPlan(board, card, plan).catch((err: unknown) => {
      patchCard(card.itemId, { plan: prev });
      onError(errMessage(err));
    });
  };

  // Move a single plan card to another plan week (+1/+2 weeks, or a picked one).
  const handleSetWeek = (card: CardModel, week: string | null) => {
    const prev = card.week ?? null;
    patchCard(card.itemId, { week: week ?? undefined });
    void provider.setWeek(board, card, week).catch((err: unknown) => {
      patchCard(card.itemId, { week: prev ?? undefined });
      onError(errMessage(err));
    });
  };

  // Take a grid card into the weekly plan: mark it for the dropped band and the
  // current week, while it stays assigned on the board (the same card). It then
  // shows the weekly stripe in the grid and the "taken" tint in the plan.
  const takeIntoPlan = (card: CardModel, band: "wed" | "fri") => {
    const prev: Partial<CardModel> = { plan: card.plan, week: card.week };
    const weekChanged = card.week !== currentWeek;
    patchCard(card.itemId, { plan: band, week: currentWeek });
    void (async () => {
      try {
        await provider.setPlan(board, card, band);
        if (weekChanged) {
          await provider.setWeek(board, card, currentWeek);
        }
      } catch (err: unknown) {
        patchCard(card.itemId, prev);
        onError(errMessage(err));
      }
    })();
  };

  // Take a plan card into work: assign it to the column's person and add it to
  // today's daily sprint, while it stays in the weekly plan (the same card).
  const takePlanCard = (card: CardModel, engineer: string, dropZone?: ZoneKey) => {
    const login = engineer === UNASSIGNED ? null : engineer;
    const zone = dropZone ?? card.zone ?? "gray";
    const optionId = optionIdForZone(roles.zone, zone) ?? card.zoneOptionId;
    const zoneChanged = zone !== card.zone;
    // Join the card's team's current sprint (its explicit state), falling back to
    // the selected day when the team has none yet.
    const sprintStart = currentSprint(board, card.team ?? null) ?? selectedDate;
    const prev: Partial<CardModel> = {
      assignees: card.assignees,
      zone: card.zone,
      zoneOptionId: card.zoneOptionId,
      sprintStart: card.sprintStart,
    };
    patchCard(card.itemId, {
      assignees: login ? [login] : [],
      zone,
      zoneOptionId: optionId,
      sprintStart,
    });
    void (async () => {
      try {
        await provider.setAssignee(board, card, login);
        if (zoneChanged && optionId) {
          await provider.setZone(board, card, optionId);
        }
        await provider.setSprintStart(board, card, sprintStart);
      } catch (err: unknown) {
        patchCard(card.itemId, prev);
        onError(errMessage(err));
      }
    })();
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

  // "No team" is offered like any other team: only when it is explicitly
  // filtered in (or nothing is filtered at all).
  const pickerNoTeam = teamFilter === null || teamFilter.includes("");

  // People to offer when reassigning a card: everyone seen on the board, me first.
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
          cards: cellCards(engineer, zone),
        });
      }
    }
    for (const band of ["wed", "fri"] as const) {
      out.push({
        key: bandKey(band),
        meta: { kind: "band", band },
        cards: weekly[band],
      });
    }
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filteredCards, orderedEngineers, weekly]);

  // Reorder a subset of cards (a band's plan cards) within the global card order
  // and persist the dragged card's new position.
  const reorderPlanCards = (card: CardModel, order: string[]) => {
    const subset = new Set(order);
    let i = 0;
    const next = board.cards.map((c) =>
      subset.has(c.itemId) ? order[i++] : c.itemId,
    );
    reorderCards(next);
    const afterId = afterIdFor(next, card.itemId);
    void provider.moveCard(board, card, afterId).catch((err: unknown) => {
      onError(errMessage(err));
      reload();
    });
  };

  const handleDrop = ({
    card,
    fromMeta,
    toMeta,
    groups: g,
  }: DropResult<TeamMeta>) => {
    // Drops that involve a weekly-plan band.
    if (fromMeta.kind === "band" || toMeta.kind === "band") {
      if (fromMeta.kind === "band" && toMeta.kind === "cell") {
        // Take the plan card into work, in the dropped cell's zone.
        takePlanCard(card, toMeta.engineer, toMeta.zone);
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
      } else if (fromMeta.kind === "cell" && toMeta.kind === "band") {
        // Take a grid card into the weekly plan; it stays on the board.
        takeIntoPlan(card, toMeta.band);
      }
      return;
    }

    // From here both ends are grid cells.
    const zoneChanged = fromMeta.zone !== toMeta.zone;
    const engineerChanged = fromMeta.engineer !== toMeta.engineer;

    let optionId = card.zoneOptionId;
    if (zoneChanged) {
      const resolved = optionIdForZone(roles.zone, toMeta.zone);
      if (!resolved) {
        onError(`Project has no Zone option for ${toMeta.zone}`);
        return;
      }
      optionId = resolved;
    }

    // 1) Optimistic local state first.
    if (zoneChanged || engineerChanged) {
      const patch: Partial<CardModel> = {};
      if (zoneChanged) {
        patch.zone = toMeta.zone;
        patch.zoneOptionId = optionId;
      }
      if (engineerChanged) {
        patch.assignees = toMeta.engineer ? [toMeta.engineer] : [];
      }
      patchCard(card.itemId, patch);
    }
    const order = globalOrderFromGroups(
      board,
      g.filter((x) => x.meta.kind === "cell").map((x) => x.ids),
    );
    reorderCards(order);

    // 2) Persist in the background; revert via reload() on any error.
    const afterId = afterIdFor(order, card.itemId);
    void (async () => {
      try {
        if (zoneChanged && optionId) {
          await provider.setZone(board, card, optionId);
        }
        if (engineerChanged) {
          await provider.setAssignee(board, card, toMeta.engineer || null);
        }
        await provider.moveCard(board, card, afterId);
      } catch (err: unknown) {
        onError(errMessage(err));
        reload();
      }
    })();
  };

  // Drive an original card's review stage from its review card's progress:
  // at 100% the original leaves review; below 100% it (re)enters review.
  const syncOriginalReview = (original: CardModel, reviewProgress: number) => {
    let next: StageKey | null | undefined;
    if (reviewProgress === 100 && original.stage === "review") {
      next = null;
    } else if (reviewProgress < 100 && original.stage !== "review") {
      next = "review";
    }
    if (next === undefined) {
      return;
    }
    const prevStage = original.stage;
    patchCard(original.itemId, { stage: next ?? undefined });
    void provider.setStage(board, original, next).catch((err: unknown) => {
      patchCard(original.itemId, { stage: prevStage });
      onError(errMessage(err));
    });
  };

  const handleProgress = (card: CardModel, raw: number) => {
    // review/locked cards are clamped to a 10–90% band (never 0% or 100%).
    const value =
      card.stage === "review" || card.stage === "locked"
        ? Math.min(90, Math.max(10, raw))
        : raw;
    const prev: Partial<CardModel> = { progress: card.progress, stage: card.stage };
    const patch: Partial<CardModel> = { progress: value };
    // Auto-link progress and "done": 100% sets done (unless review/locked is on),
    // dropping below 100% clears done. review/locked are left untouched.
    let stageChange: StageKey | null | undefined;
    if (roles.stage) {
      if (value === 100 && card.stage == null) {
        stageChange = "done";
      } else if (value < 100 && card.stage === "done") {
        stageChange = null;
      }
    }
    if (stageChange !== undefined) {
      patch.stage = stageChange ?? undefined;
    }
    patchCard(card.itemId, patch);
    void (async () => {
      try {
        await provider.setProgress(board, card, value);
        if (stageChange !== undefined) {
          await provider.setStage(board, card, stageChange);
        }
      } catch (err: unknown) {
        patchCard(card.itemId, prev);
        onError(errMessage(err));
      }
    })();

    // When this is a review card, its progress drives the original's review stage.
    if (card.reviewOf) {
      const original = board.cards.find((c) => c.itemId === card.reviewOf);
      if (original) {
        syncOriginalReview(original, value);
      }
    }
  };

  const handleStage = (card: CardModel, stage: StageKey | null) => {
    const prev: Partial<CardModel> = {
      stage: card.stage,
      progress: card.progress,
    };
    const patch: Partial<CardModel> = { stage: stage ?? undefined };
    if (stage === "done") {
      patch.progress = 100;
    }
    // review/locked cards can never sit at 100%: knock a full card down to 90%.
    const dropTo90 =
      (stage === "review" || stage === "locked") && card.progress === 100;
    if (dropTo90) {
      patch.progress = 90;
    }
    patchCard(card.itemId, patch);
    void provider.setStage(board, card, stage).catch((err: unknown) => {
      patchCard(card.itemId, prev);
      onError(errMessage(err));
    });
    if (dropTo90) {
      void provider.setProgress(board, card, 90).catch((err: unknown) => {
        patchCard(card.itemId, { progress: prev.progress });
        onError(errMessage(err));
      });
    }
  };

  // "In Progress" is the implicit status (no stage, progress in [10, 90]).
  // Picking it clears any stage and clamps progress into that band: under 10
  // becomes 10, a done/full card drops to 90, otherwise the value is kept.
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
    void (async () => {
      try {
        await provider.setStage(board, card, null);
        if (value !== cur) {
          await provider.setProgress(board, card, value);
        }
      } catch (err: unknown) {
        patchCard(card.itemId, prev);
        onError(errMessage(err));
      }
    })();

    // A review card's progress drives its original's review stage; keep that
    // in sync when In Progress changes it (e.g. a done review card reopens it).
    if (card.reviewOf && value !== cur) {
      const original = board.cards.find((c) => c.itemId === card.reviewOf);
      if (original) {
        syncOriginalReview(original, value);
      }
    }
  };

  const handleRename = (card: CardModel, title: string) => {
    const prev = card.title;
    patchCard(card.itemId, { title });
    void provider.renameCard(board, card, title).catch((err: unknown) => {
      patchCard(card.itemId, { title: prev });
      onError(errMessage(err));
    });
  };

  const handleDelete = (card: CardModel) => {
    // If a review card links back to this one, delete both (with one confirm).
    const linkedReview = board.cards.find((c) => c.reviewOf === card.itemId);
    if (linkedReview) {
      if (
        !window.confirm(
          `Delete this card and its linked review card «${linkedReview.title}»?`,
        )
      ) {
        return;
      }
      removeCard(linkedReview.itemId);
      void provider.deleteCard(board, linkedReview).catch((err: unknown) => {
        addCard(linkedReview);
        onError(errMessage(err));
      });
    }
    removeCard(card.itemId);
    void provider.deleteCard(board, card).catch((err: unknown) => {
      addCard(card);
      onError(errMessage(err));
    });
  };

  // The team's explicit previous sprint — where deleting a grid card demotes it
  // (back to last sprint) instead of removing it.
  const previousSprintFor = (card: CardModel): string | null =>
    previousSprint(board, card.team ?? null);

  const previousWeekFor = (card: CardModel): string | null => {
    const team = card.team ?? "";
    const cur = card.week;
    if (!cur) return null;
    let prev: string | null = null;
    for (const c of board.cards) {
      if (c.itemId === card.itemId || (c.team ?? "") !== team) continue;
      if (!c.week || c.week >= cur) continue;
      if (prev === null || c.week > prev) prev = c.week;
    }
    return prev;
  };

  // Deleting a taken plan card from the grid releases it (clears assignee + the
  // daily sprint) instead of deleting it, so it stays in the weekly plan. A plain
  // grid card is demoted to its previous sprint (if any) rather than deleted.
  const handleGridDelete = (card: CardModel) => {
    if (!card.plan) {
      const prevSprint = previousSprintFor(card);
      if (prevSprint) {
        const prev: Partial<CardModel> = {
          startDate: card.startDate,
          sprintStart: card.sprintStart,
        };
        patchCard(card.itemId, {
          startDate: prevSprint,
          sprintStart: prevSprint,
        });
        void (async () => {
          try {
            await provider.setStart(board, card, prevSprint);
            await provider.setSprintStart(board, card, prevSprint);
          } catch (err: unknown) {
            patchCard(card.itemId, prev);
            onError(errMessage(err));
          }
        })();
        return;
      }
      handleDelete(card);
      return;
    }
    const prev: Partial<CardModel> = {
      assignees: card.assignees,
      sprintStart: card.sprintStart,
    };
    patchCard(card.itemId, { assignees: [], sprintStart: undefined });
    void (async () => {
      try {
        await provider.setAssignee(board, card, null);
        await provider.setSprintStart(board, card, null);
      } catch (err: unknown) {
        patchCard(card.itemId, prev);
        onError(errMessage(err));
      }
    })();
  };

  // Remove a card from the weekly plan. If it was taken into work (assigned) it
  // stays on the board — only the weekly marker (Plan + Week) is cleared. A pure
  // plan card is demoted to its previous week (if any) instead of being deleted.
  const removeFromPlan = (card: CardModel) => {
    if (card.assignees.length === 0) {
      const prevWeek = previousWeekFor(card);
      if (prevWeek) {
        const prev: Partial<CardModel> = { week: card.week };
        patchCard(card.itemId, { week: prevWeek });
        void provider.setWeek(board, card, prevWeek).catch((err: unknown) => {
          patchCard(card.itemId, prev);
          onError(errMessage(err));
        });
        return;
      }
      handleDelete(card);
      return;
    }
    const prev: Partial<CardModel> = { plan: card.plan, week: card.week };
    patchCard(card.itemId, { plan: undefined, week: undefined });
    void (async () => {
      try {
        await provider.setPlan(board, card, null);
        await provider.setWeek(board, card, null);
      } catch (err: unknown) {
        patchCard(card.itemId, prev);
        onError(errMessage(err));
      }
    })();
  };

  // Move a card's start date; if it passes the finish date, push finish too.
  const handleSetTeam = (card: CardModel, team: string | null) => {
    const prevTeam = card.team;
    const prevSprint = card.sprintStart;
    // Join the new team's current sprint (its explicit state), so a card moved
    // between teams stays visible instead of dropping off when its old sprint
    // predates the new team's current one. Fall back to the selected day.
    const sprintStart = currentSprint(board, team) ?? selectedDate;
    patchCard(card.itemId, { team: team ?? undefined, sprintStart });
    void provider.setTeam(board, card, team).catch((err: unknown) => {
      patchCard(card.itemId, { team: prevTeam });
      onError(errMessage(err));
    });
    if (sprintStart !== prevSprint) {
      void provider
        .setSprintStart(board, card, sprintStart)
        .catch((err: unknown) => {
          patchCard(card.itemId, { sprintStart: prevSprint });
          onError(errMessage(err));
        });
    }
  };

  const handleSetAssignee = (card: CardModel, login: string | null) => {
    const prev = card.assignees;
    patchCard(card.itemId, { assignees: login ? [login] : [] });
    void provider.setAssignee(board, card, login).catch((err: unknown) => {
      patchCard(card.itemId, { assignees: prev });
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
  // it — driven from the counterpart avatar's menu.
  const handleSetReviewAssignee = (card: CardModel, login: string | null) => {
    const reviewCard = board.cards.find((c) => c.reviewOf === card.itemId);
    if (login === null) {
      if (reviewCard) {
        removeCard(reviewCard.itemId);
        void provider.deleteCard(board, reviewCard).catch((err: unknown) => {
          addCard(reviewCard);
          onError(errMessage(err));
        });
      }
      return;
    }
    if (!reviewCard) {
      // No review yet — assigning a reviewer sends the card to review.
      handleSendToReview(card, login);
      return;
    }
    const prev = reviewCard.assignees;
    patchCard(reviewCard.itemId, { assignees: [login] });
    void provider.setAssignee(board, reviewCard, login).catch((err: unknown) => {
      patchCard(reviewCard.itemId, { assignees: prev });
      onError(errMessage(err));
    });
  };

  // Send a card to review: create a linked review card for the reviewer (in the
  // original's zone on the Team board) and put the original on the review stage.
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
      zoneOptionId: optionIdForZone(roles.zone, zone) ?? card.zoneOptionId,
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
    void provider
      .createCard(board, {
        title,
        zone,
        day: selectedDate,
        start: selectedDate,
        sprintStart,
        assigneeLogin: reviewerLogin,
        team,
        reviewOf: card.itemId,
      })
      .then((created) => {
        removeCard(tempId);
        addCard(created);
      })
      .catch((err: unknown) => {
        removeCard(tempId);
        onError(errMessage(err));
      });

    // Put the original on review. handleStage also drops a 100% card to 90%,
    // since review/locked can't sit at full.
    handleStage(card, "review");
  };

  const handleSetDates = (
    card: CardModel,
    start: string | null,
    end: string | null,
  ) => {
    const prev = { startDate: card.startDate, sprintStart: card.sprintStart };
    patchCard(card.itemId, {
      startDate: start ?? undefined,
      sprintStart: end ?? undefined,
    });
    void (async () => {
      try {
        await provider.setStart(board, card, start);
        await provider.setSprintStart(board, card, end);
      } catch (err: unknown) {
        patchCard(card.itemId, prev);
        onError(errMessage(err));
      }
    })();
  };

  // Optimistically create a Team grid card with explicit sprint dates and push
  // it to the provider.
  const createTeamCard = (
    engineer: string,
    zone: ZoneKey,
    title: string,
    team: string | null,
    startDate: string,
    sprintStart: string,
    day: string,
  ) => {
    const tempId = `tmp-${new Date().toISOString()}`;
    const optimistic: CardModel = {
      itemId: tempId,
      title,
      isDraft: true,
      assignees: engineer ? [engineer] : [],
      zone,
      day,
      startDate,
      sprintStart,
      team: team ?? undefined,
      createdAt: new Date().toISOString(),
      description: "",
      notes: [],
    };
    addCard(optimistic);
    void provider
      .createCard(board, {
        title,
        zone,
        day,
        start: startDate,
        sprintStart,
        assigneeLogin: engineer || null,
        team,
      })
      .then((card) => {
        removeCard(tempId);
        addCard(card);
      })
      .catch((err: unknown) => {
        removeCard(tempId);
        onError(errMessage(err));
      });
  };

  // Creating a Team card joins the team's current sprint (sprintStart) and is
  // scheduled for the viewed day (startDate = selectedDate). A team with no sprint
  // yet records its first one on the state card and the card joins it, so the Me
  // view has a sprint anchor.
  const handleCreate = (
    engineer: string,
    zone: ZoneKey,
    title: string,
    team?: string | null,
  ) => {
    const teamKey = team ?? null;
    const cur = currentSprint(board, teamKey);
    const sprint = cur ?? selectedDate;
    if (cur === null) {
      // First sprint for this team: record it on the state card, then reload to
      // pick it up as the Me sprint anchor.
      void provider
        .setSprintState(board, teamKey, selectedDate, null)
        .then(() => reload())
        .catch((err: unknown) => onError(errMessage(err)));
    }
    createTeamCard(
      engineer,
      zone,
      title,
      teamKey,
      selectedDate,
      sprint,
      todayIso(),
    );
  };

  // Carry over: advance the team's sprint to today (the prior current becomes the
  // previous) and pull its unfinished cards forward. Always advances, even with
  // nothing to carry. `team` is null for the no-team group.
  const startSprint = async (team: string | null) => {
    setSprintMenuOpen(false);
    const label = team ?? "no team";
    const old = currentSprint(board, team);
    const today = todayIso();
    // Idempotent: if the sprint is already today's, do not re-advance — that would
    // overwrite the previous sprint, making previous = current = today.
    if (old === today) {
      onError(`«${label}» is already on today's sprint.`);
      return;
    }
    const carry = board.cards.filter(
      (c) =>
        (team === null ? c.team == null : c.team === team) &&
        c.sprintStart &&
        c.sprintStart < today &&
        c.stage !== "done" &&
        !c.itemId.startsWith("tmp-"),
    );
    if (
      !window.confirm(
        `Start a new sprint for «${label}» (${today})? ${carry.length} unfinished card(s) carried over.`,
      )
    ) {
      return;
    }
    // Optimistically move the cards, then run the whole carry-over as one
    // provider call (the API backend advances the pointer and re-dates the
    // unfinished cards concurrently, streaming a watch event per card).
    carry.forEach((card) => patchCard(card.itemId, { sprintStart: today }));
    try {
      await provider.carryOver(board, team);
    } catch (err: unknown) {
      onError(errMessage(err));
    }
    // Land on the day the sprint was started on, so the new sprint is in view.
    setSelectedDate(today);
    // Re-read the advanced state (and reconcile the carried cards).
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
          teams={roster}
          selectedKeys={teamFilter}
          onSelect={onSetFilter}
          onAdd={onAddTeam}
          onRemove={onRemoveTeam}
          onRename={onRenameTeam}
          noTeamChip={board.cards.some((c) => !c.team)}
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
              setSelectedDate(sprintJump.target);
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
        renderCard={(card, group) =>
          group.meta.kind === "band" ? (
            <Card
              card={card}
              selected={card.itemId === selectedCardId}
              onSelect={(c) => setSelectedCardId(c.itemId)}
              onProgress={handleProgress}
              onDelete={removeFromPlan}
              onStage={handleStage}
              onInProgress={handleInProgress}
              onRename={handleRename}
              onOpen={onOpen}
              teams={roster}
              people={people}
              users={users}
              onSetTeam={handleSetTeam}
              onSetAssignee={handleSetAssignee}
              hasLinkedReview={reviewedItemIds.has(card.itemId)}
              counterpartAssignees={counterpartAssigneesFor(card)}
              onSetReviewAssignee={handleSetReviewAssignee}
              asOf={selectedDate}
              onSetDates={handleSetDates}
              weekMode
              onSetWeek={handleSetWeek}
              dimAvatar
            />
          ) : (
            <Card
              card={card}
              selected={card.itemId === selectedCardId}
              onSelect={(c) => setSelectedCardId(c.itemId)}
              onProgress={handleProgress}
              onDelete={handleGridDelete}
              onStage={handleStage}
              onInProgress={handleInProgress}
              onRename={handleRename}
              onOpen={onOpen}
              teams={roster}
              people={people}
              users={users}
              onSetTeam={handleSetTeam}
              onSetAssignee={handleSetAssignee}
              hasLinkedReview={reviewedItemIds.has(card.itemId)}
              counterpartAssignees={counterpartAssigneesFor(card)}
              onSetReviewAssignee={handleSetReviewAssignee}
              asOf={selectedDate}
              onSetDates={handleSetDates}
              dimAvatar
            />
          )
        }
          renderOverlay={(card) => (
            <Card
              card={card}
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
                style={{ background: def.background, borderLeftColor: def.accent }}
              >
                <span className="zone-spine" style={{ color: def.accent }}>
                  {def.spine}
                </span>
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
                        <img
                          className={`avatar-img${engineer === me ? " avatar-me" : ""}`}
                          src={avatarUrlFor(engineer, users[engineer])}
                          alt={engineer}
                          title={displayName(engineer, users[engineer])}
                          draggable={false}
                        />
                        <span
                          className={`team-col-name${engineer === me ? " team-col-me" : ""}`}
                        >
                          {displayName(engineer, users[engineer])}
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
                      {!planCollapsed && (
                        <div className="sprint-wrap" ref={carryWeekRef}>
                          <button
                            type="button"
                            className="btn"
                            onClick={() => {
                              if (teamFilter?.length === 1) {
                                handleCarryWeek(
                                  teamFilter[0] === "" ? null : teamFilter[0],
                                );
                              } else {
                                setCarryWeekOpen((o) => !o);
                              }
                            }}
                            title="Move unfinished plan cards to next week"
                          >
                            Carry over week{teamFilter?.length === 1 ? " →" : " ▾"}
                          </button>
                          {teamFilter?.length !== 1 && (
                            <Dropdown
                              open={carryWeekOpen}
                              anchorRef={carryWeekRef}
                              onClose={() => setCarryWeekOpen(false)}
                              className="card-stage-menu sprint-menu"
                            >
                              {carryable.teams.map((t) => (
                                <button
                                  key={t}
                                  type="button"
                                  className="card-stage-item"
                                  onClick={() => handleCarryWeek(t)}
                                >
                                  <span
                                    className="team-dot"
                                    style={{ background: teamColor(t) }}
                                  />
                                  {t}
                                </button>
                              ))}
                              {pickerNoTeam && (
                                <button
                                  type="button"
                                  className="card-stage-item card-stage-clear"
                                  onClick={() => handleCarryWeek(null)}
                                >
                                  no team
                                </button>
                              )}
                              {carryable.teams.length === 0 &&
                                !pickerNoTeam && (
                                  <div className="sprint-empty">
                                    Nothing to carry
                                  </div>
                                )}
                            </Dropdown>
                          )}
                        </div>
                      )}
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
          onAdd={onAddTeam}
          onRename={onRenameTeam}
          onRemove={onRemoveTeam}
          onReorder={onReorderTeams}
          onClose={() => setTeamsModalOpen(false)}
        />
      )}
    </div>
  );
}
