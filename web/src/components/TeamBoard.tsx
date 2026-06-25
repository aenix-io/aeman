import { Fragment, useEffect, useMemo, useRef, useState, type Ref } from "react";
import type {
  Board,
  Card as CardModel,
  Provider,
  StageKey,
  ZoneKey,
} from "../providers/types";
import { ZONES, ZONE_ORDER, optionIdForZone } from "../zones";
import { fieldRoles } from "../providers/fields";
import { todayIso, addDays, activeOnDay, mondayOf } from "../date";
import { teamColor } from "../avatar";
import { avatarUrlFor, displayName, type GhUser } from "../users";
import { Card } from "./Card";
import { AddCard } from "./AddCard";
import { TeamChips } from "./TeamChips";
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
  teamFilter: string | null;
  onSetFilter: (key: string | null) => void;
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
  onRequestLock: (card: CardModel) => void;
}

/** Per-group metadata for the Team board: the destination engineer + zone. */
interface TeamMeta {
  engineer: string;
  zone: ZoneKey;
}

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
  patchCard,
  addCard,
  removeCard,
  reorderCards,
  reload,
  onError,
  onOpen,
  onRequestLock,
}: TeamBoardProps) {
  const [selectedDate, setSelectedDate] = useState<string>(todayIso());
  const [selectedCardId, setSelectedCardId] = useState<string | null>(null);
  const [sprintMenuOpen, setSprintMenuOpen] = useState(false);
  const sprintRef = useRef<HTMLDivElement | null>(null);
  const [columnOrder, setColumnOrder] = useState<string[]>([]);
  const [dragCol, setDragCol] = useState<string | null>(null);
  // A weekly-plan card currently being dragged into a person column.
  const [draggedPlan, setDraggedPlan] = useState<CardModel | null>(null);
  const [planCollapsed, setPlanCollapsed] = useState(false);

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

  const roles = useMemo(() => fieldRoles(board), [board]);

  // Single-select: no filter shows all; otherwise match the card's group.
  const passesFilter = (card: CardModel): boolean =>
    teamFilter === null || (card.team ?? "") === teamFilter;

  // Cards passing the team filter (the scope before applying the sprint).
  const inFilter = useMemo(
    () => board.cards.filter((c) => passesFilter(c)),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [board.cards, teamFilter],
  );

  // A card shows on every day from its start through its current sprint day, so
  // past days keep long-running cards while days after the last sprint go empty
  // (the cue for the lead to start a new sprint).
  const filteredCards = useMemo(
    () =>
      inFilter.filter((c) =>
        activeOnDay(c.startDate, c.sprintStart, selectedDate),
      ),
    [inFilter, selectedDate],
  );

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

  // Weekly carry over: move unfinished plan cards of the current week to next
  // week (its own cycle, separate from the daily Carry over).
  const handleCarryWeek = () => {
    const nextWeek = addDays(currentWeek, 7);
    const carry = board.cards.filter(
      (c) =>
        c.plan &&
        c.week === currentWeek &&
        c.stage !== "done" &&
        passesFilter(c),
    );
    if (carry.length === 0) {
      onError("No unfinished plan cards to carry to next week.");
      return;
    }
    if (
      !window.confirm(
        `Carry over ${carry.length} unfinished plan card(s) to the week of ${nextWeek}?`,
      )
    ) {
      return;
    }
    for (const card of carry) {
      const prev = card.week;
      patchCard(card.itemId, { week: nextWeek });
      void provider.setWeek(board, card, nextWeek).catch((err: unknown) => {
        patchCard(card.itemId, { week: prev });
        onError(errMessage(err));
      });
    }
  };

  // Take a plan card into work: assign it to the column's person and add it to
  // today's daily sprint, while it stays in the weekly plan (the same card).
  const takePlanCard = (card: CardModel, engineer: string) => {
    const login = engineer === UNASSIGNED ? null : engineer;
    const zone = card.zone ?? "gray";
    const optionId = card.zoneOptionId ?? optionIdForZone(roles.zone, zone);
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
      sprintStart: selectedDate,
    });
    void (async () => {
      try {
        await provider.setAssignee(board, card, login);
        if (!card.zone && optionId) {
          await provider.setZone(board, card, optionId);
        }
        await provider.setSprintStart(board, card, selectedDate);
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

  // New cards default to the filtered team; null = all (show picker), "" = no team.
  const forcedTeam = useMemo(
    () => (teamFilter === null ? undefined : teamFilter || null),
    [teamFilter],
  );

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

  // One sortable group per grid cell, in a stable engineer→zone order so the
  // derived global card order is coherent across drops.
  const groups = useMemo<BoardGroup<TeamMeta>[]>(() => {
    const out: BoardGroup<TeamMeta>[] = [];
    for (const engineer of orderedEngineers) {
      for (const zone of ZONE_ORDER) {
        out.push({
          key: cellKey(engineer, zone),
          meta: { engineer, zone },
          cards: cellCards(engineer, zone),
        });
      }
    }
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filteredCards, orderedEngineers]);

  const handleDrop = ({
    card,
    fromMeta,
    toMeta,
    groups: g,
  }: DropResult<TeamMeta>) => {
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
      g.map((x) => x.ids),
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

  const handleProgress = (card: CardModel, value: number) => {
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
    patchCard(card.itemId, patch);
    void provider.setStage(board, card, stage).catch((err: unknown) => {
      patchCard(card.itemId, prev);
      onError(errMessage(err));
    });
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
    removeCard(card.itemId);
    void provider.deleteCard(board, card).catch((err: unknown) => {
      addCard(card);
      onError(errMessage(err));
    });
  };

  // Deleting a taken plan card from the grid releases it (clears assignee + the
  // daily sprint) instead of deleting it, so it stays in the weekly plan.
  const handleGridDelete = (card: CardModel) => {
    if (!card.plan) {
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

  // Move a card's start date; if it passes the finish date, push finish too.
  const handleSetTeam = (card: CardModel, team: string | null) => {
    const prev = card.team;
    patchCard(card.itemId, { team: team ?? undefined });
    void provider.setTeam(board, card, team).catch((err: unknown) => {
      patchCard(card.itemId, { team: prev });
      onError(errMessage(err));
    });
  };

  const handleSetAssignee = (card: CardModel, login: string | null) => {
    const prev = card.assignees;
    patchCard(card.itemId, { assignees: login ? [login] : [] });
    void provider.setAssignee(board, card, login).catch((err: unknown) => {
      patchCard(card.itemId, { assignees: prev });
      onError(errMessage(err));
    });
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

  const handleCreate = (
    engineer: string,
    zone: ZoneKey,
    title: string,
    team?: string | null,
  ) => {
    const tempId = `tmp-${new Date().toISOString()}`;
    // On Team, a new card joins the sprint of the day being viewed: adding a
    // card on an empty day is how a fresh sprint is started.
    const sprintStart = selectedDate;
    const optimistic: CardModel = {
      itemId: tempId,
      title,
      isDraft: true,
      assignees: engineer ? [engineer] : [],
      zone,
      day: selectedDate,
      startDate: selectedDate,
      sprintStart,
      team: team ?? undefined,
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
        assigneeLogin: engineer || null,
        team: team ?? null,
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

  // Carry over a team's unfinished cards from earlier sprints into the selected
  // day's sprint. `team` is null for the no-team group.
  const startSprint = (team: string | null) => {
    setSprintMenuOpen(false);
    const label = team ?? "no team";
    const carry = board.cards.filter(
      (c) =>
        (team === null ? c.team == null : c.team === team) &&
        c.stage !== "done" &&
        (c.sprintStart == null || c.sprintStart < selectedDate),
    );
    if (carry.length === 0) {
      onError(
        `Nothing to carry over for "${label}" — add cards on ${selectedDate} to start the sprint.`,
      );
      return;
    }
    if (
      !window.confirm(
        `Carry over ${carry.length} unfinished card(s) for "${label}" into ${selectedDate}?`,
      )
    ) {
      return;
    }
    for (const card of carry) {
      const prev = card.sprintStart;
      patchCard(card.itemId, { sprintStart: selectedDate });
      void provider
        .setSprintStart(board, card, selectedDate)
        .catch((err: unknown) => {
          patchCard(card.itemId, { sprintStart: prev });
          onError(errMessage(err));
        });
    }
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
          label="Teams"
          teams={roster}
          selectedKey={teamFilter}
          onSelect={onSetFilter}
          onAdd={onAddTeam}
          onRemove={onRemoveTeam}
          onRename={onRenameTeam}
          noTeamChip={board.cards.some((c) => !c.team)}
        />

        <button
          type="button"
          className="btn shuffle-btn"
          onClick={shuffleColumns}
          title="Shuffle columns"
          aria-label="Shuffle columns"
        >
          ⇄
        </button>
        <div className="sprint-wrap" ref={sprintRef}>
          <button
            type="button"
            className="btn sprint-btn"
            onClick={() => setSprintMenuOpen((o) => !o)}
            title={`Carry unfinished cards into the ${selectedDate} sprint`}
          >
            Carry over ▾
          </button>
          {sprintMenuOpen && (
            <div className="card-stage-menu sprint-menu">
              {roster.map((t) => (
                <button
                  key={t}
                  type="button"
                  className="card-stage-item"
                  onClick={() => startSprint(t)}
                >
                  <span className="team-dot" style={{ background: teamColor(t) }} />
                  {t}
                </button>
              ))}
              <button
                type="button"
                className="card-stage-item card-stage-clear"
                onClick={() => startSprint(null)}
              >
                no team
              </button>
            </div>
          )}
        </div>
      </div>

      <div className="team-grid">
        <SortableBoard<TeamMeta>
          groups={groups}
          onDrop={handleDrop}
          renderCard={(card) => (
            <Card
              card={card}
              selected={card.itemId === selectedCardId}
              onSelect={(c) => setSelectedCardId(c.itemId)}
              onProgress={handleProgress}
              onDelete={handleGridDelete}
              onStage={handleStage}
              onRename={handleRename}
              onOpen={onOpen}
              onRequestLock={onRequestLock}
              teams={roster}
              people={people}
              users={users}
              onSetTeam={handleSetTeam}
              onSetAssignee={handleSetAssignee}
              asOf={selectedDate}
              onSetDates={handleSetDates}
            />
          )}
          renderOverlay={(card) => (
            <Card
              card={card}
              selected={false}
              onSelect={() => {}}
              onProgress={() => {}}
              onDelete={() => {}}
              onStage={() => {}}
              onRename={() => {}}
              onOpen={() => {}}
              onRequestLock={() => {}}
            />
          )}
          renderGroup={(group, body, { isOver, dropRef }) => {
            const def = ZONES[group.meta.zone];
            return (
              <div
                ref={dropRef as Ref<HTMLDivElement>}
                className={`zone-area${isOver ? " zone-area-dragover" : ""}`}
                style={{ background: def.background, borderLeftColor: def.accent }}
              >
                <div className="zone-cards">
                  {body}
                  <AddCard
                    teams={forcedTeam === undefined ? roster : undefined}
                    forcedTeam={forcedTeam}
                    onCreate={(title, team) =>
                      handleCreate(group.meta.engineer, group.meta.zone, title, team)
                    }
                  />
                </div>
              </div>
            );
          }}
          renderLayout={(nodes) =>
            orderedEngineers.length === 0 ? (
              <p className="placeholder">No cards match the selected teams.</p>
            ) : (
              orderedEngineers.map((engineer) => (
                <section
                  className={`team-col${draggedPlan ? " team-col-droptarget" : ""}`}
                  key={engineer || "__unassigned__"}
                  onDragOver={(e) => {
                    if (draggedPlan) {
                      e.preventDefault();
                    }
                  }}
                  onDrop={() => {
                    if (draggedPlan) {
                      takePlanCard(draggedPlan, engineer);
                    }
                    setDraggedPlan(null);
                  }}
                >
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
            )
          }
        />
      </div>

      <div className={`team-weekly${planCollapsed ? " team-weekly-collapsed" : ""}`}>
        <div className="team-weekly-head">
          <span className="team-weekly-title">Weekly plan · {currentWeek}</span>
          <div className="team-weekly-actions">
            <button
              type="button"
              className="btn"
              onClick={handleCarryWeek}
              title="Move unfinished plan cards to next week"
            >
              Carry over week →
            </button>
            <button
              type="button"
              className="team-weekly-toggle"
              onClick={() => setPlanCollapsed((c) => !c)}
              aria-label={planCollapsed ? "Expand weekly plan" : "Collapse weekly plan"}
              title={planCollapsed ? "Expand" : "Collapse"}
            >
              {planCollapsed ? "▴" : "▾"}
            </button>
          </div>
        </div>
        {!planCollapsed &&
          (["wed", "fri"] as const).map((band) => (
          <div key={band} className={`team-weekly-band team-weekly-${band}`}>
            {weekly[band].map((card) => (
              <div
                key={card.itemId}
                className="plan-card"
                draggable
                onDragStart={() => setDraggedPlan(card)}
                onDragEnd={() => setDraggedPlan(null)}
                title="Drag onto a person to take it into work"
              >
                <Card
                  card={card}
                  selected={card.itemId === selectedCardId}
                  onSelect={(c) => setSelectedCardId(c.itemId)}
                  onProgress={handleProgress}
                  onDelete={handleDelete}
                  onStage={handleStage}
                  onRename={handleRename}
                  onOpen={onOpen}
                  onRequestLock={onRequestLock}
                  teams={roster}
                  people={people}
                  users={users}
                  onSetTeam={handleSetTeam}
                  onSetAssignee={handleSetAssignee}
                  asOf={selectedDate}
                  onSetDates={handleSetDates}
                />
              </div>
            ))}
            <AddCard
              forcedTeam={forcedTeam}
              teams={roster}
              placeholder="Plan task…"
              onCreate={(title, team) => handleCreatePlan(band, title, team)}
            />
          </div>
        ))}
      </div>
    </div>
  );
}
