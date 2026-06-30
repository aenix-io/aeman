import { useEffect, useMemo, useRef, useState, type Ref } from "react";
import type {
  Board,
  Card as CardModel,
  Note,
  Provider,
  StageKey,
  ZoneKey,
} from "../providers/types";
import { ZONES, ZONE_ORDER, optionIdForZone } from "../zones";
import { fieldRoles } from "../providers/fields";
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
}

/** Per-group metadata for the Me board: just the destination zone. */
interface MeMeta {
  zone: ZoneKey;
}

const errMessage = (err: unknown) =>
  err instanceof Error ? err.message : String(err);

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
}: MeBoardProps) {
  const [selectedDate, setSelectedDate] = useState<string>(todayIso());
  const [selectedCardId, setSelectedCardId] = useState<string | null>(null);
  // Eye toggle by the team chips: when on, show only the selected teams' cards.
  // Deliberately not persisted — resets to off (show all) on reload.
  const [teamFocus, setTeamFocus] = useState(false);
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

  const roles = useMemo(() => fieldRoles(board), [board]);

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
        if (teamFocus && teamFilter && !teamFilter.includes(c.team ?? "")) {
          return false;
        }
        const as = activeSprint(board, c.team ?? null, selectedDate);
        return (
          as !== "" &&
          c.sprintStart === as &&
          (!c.startDate || c.startDate <= selectedDate)
        );
      }),
    [mine, board, selectedDate, teamFocus, teamFilter],
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

  const selectedCard =
    myCards.find((c) => c.itemId === selectedCardId) ?? null;

  const fail = (err: unknown) => {
    onError(err instanceof Error ? err.message : String(err));
    reload();
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
  // yellow/unplanned zone on the Me board) and put the original on review.
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
      zoneOptionId: optionIdForZone(roles.zone, zone),
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

  const handleDrop = ({ card, fromMeta, toMeta, groups: g }: DropResult<MeMeta>) => {
    const zoneChanged = fromMeta.zone !== toMeta.zone;

    // Resolve the new zone option up front; abort the whole drop if missing.
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
    if (zoneChanged) {
      patchCard(card.itemId, { zone: toMeta.zone, zoneOptionId: optionId });
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
        await provider.moveCard(board, card, afterId);
      } catch (err: unknown) {
        onError(errMessage(err));
        reload();
      }
    })();
  };

  const handleCreate = (zone: ZoneKey, title: string, team?: string | null) => {
    const tempId = `tmp-${new Date().toISOString()}`;
    // The card is scheduled for the viewed day (startDate = selectedDate) and joins
    // the team's current sprint (sprintStart), falling back to the viewed day when
    // the team has no sprint yet.
    const sprintStart = currentSprint(board, team ?? null) ?? selectedDate;
    const optimistic: CardModel = {
      itemId: tempId,
      title,
      isDraft: true,
      assignees: viewMe ? [viewMe] : [],
      zone,
      day: selectedDate,
      startDate: selectedDate,
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
        day: selectedDate,
        start: selectedDate,
        sprintStart,
        assigneeLogin: viewMe || null,
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
    patchCard(selectedCard.itemId, {
      notes: [...(selectedCard.notes ?? []), optimistic],
    });
    void provider.addNote(board, selectedCard, text).catch(fail);
  };

  const handleEditNote = (note: Note, card: CardModel, text: string) => {
    patchCard(card.itemId, {
      notes: (card.notes ?? []).map((n) =>
        n.id === note.id ? { ...n, body: text } : n,
      ),
    });
    void provider.editNote(board, card, note, text).catch(fail);
  };

  const handleDeleteNote = (note: Note, card: CardModel) => {
    patchCard(card.itemId, {
      notes: (card.notes ?? []).filter((n) => n.id !== note.id),
    });
    void provider.deleteNote(board, card, note).catch(fail);
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
        />

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
