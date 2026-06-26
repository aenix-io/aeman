import { useMemo, useRef, useState, type Ref } from "react";
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
import { currentSprintByTeam, sprintForNewCard } from "../sprint";
import { avatarUrlFor, displayName, type GhUser } from "../users";
import { Card } from "./Card";
import { AddCard } from "./AddCard";
import { Dropdown } from "./Dropdown";
import { TeamChips } from "./TeamChips";
import { NotesPanel, type DayNote } from "./NotesPanel";
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
  onRequestLock,
}: MeBoardProps) {
  const [selectedDate, setSelectedDate] = useState<string>(todayIso());
  const [selectedCardId, setSelectedCardId] = useState<string | null>(null);
  // Impersonate: view (and act on) the board as another person.
  const [impersonated, setImpersonated] = useState<string | null>(null);
  const [impOpen, setImpOpen] = useState(false);
  const impRef = useRef<HTMLDivElement | null>(null);
  const viewMe = impersonated ?? me;
  const people = useMemo(
    () =>
      [...new Set(board.cards.flatMap((c) => c.assignees))]
        .filter((p) => p && p !== me)
        .sort(),
    [board.cards, me],
  );

  const roles = useMemo(() => fieldRoles(board), [board]);

  // My cards (any day); when me is empty, everyone's.
  const mine = useMemo(
    () => board.cards.filter((c) => (viewMe ? c.assignees.includes(viewMe) : true)),
    [board.cards, viewMe],
  );

  // Per team, the latest sprint started on or before the selected day.
  const currentSprint = useMemo(
    () => currentSprintByTeam(mine, selectedDate),
    [mine, selectedDate],
  );

  // A card shows within its [start, sprint] range, and keeps showing on later
  // days while it stays its team's current sprint — so in Me cards stay visible
  // the next day until a new sprint is started.
  const myCards = useMemo(
    () =>
      mine.filter((c) => {
        const start = c.startDate ?? c.sprintStart;
        const sprint = c.sprintStart;
        if (!start || !sprint || selectedDate < start) {
          return false;
        }
        return selectedDate <= sprint || currentSprint.get(c.team ?? "") === sprint;
      }),
    [mine, currentSprint, selectedDate],
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

  const handleSetTeam = (card: CardModel, team: string | null) => {
    const prevTeam = card.team;
    const prevSprint = card.sprintStart;
    // Join the new team's running sprint, so a card moved between teams stays
    // visible instead of dropping off when its old sprint predates the new
    // team's current one (mirrors how a freshly created card joins a sprint).
    const sprintStart = sprintForNewCard(board.cards, team ?? null, selectedDate);
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
    removeCard(card.itemId);
    void provider.deleteCard(board, card).catch((err: unknown) => {
      addCard(card);
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
    // Join the team's current sprint instead of starting a new one, so adding a
    // card on a later day does not look like a new sprint and hide the rest. The
    // start date is the day it was created, so a card added during a running
    // sprint does not appear retroactively on the sprint's earlier days.
    const sprintStart = sprintForNewCard(board.cards, team ?? null, selectedDate);
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
        start: sprintStart,
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
          selectedKey={teamFilter}
          onSelect={onSetFilter}
          onAdd={onAddTeam}
          onRemove={onRemoveTeam}
          onRename={onRenameTeam}
          canManage={false}
          noTeamChip
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
            {people.map((p) => (
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
            {people.length === 0 && (
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
                  onRename={handleRename}
                  onOpen={onOpen}
                  onRequestLock={onRequestLock}
                  teams={teams}
                  onSetTeam={handleSetTeam}
                  asOf={selectedDate}
                  offTeam={
                    teamFilter !== null && (card.team ?? "") !== teamFilter
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
                  onRename={() => {}}
                  onOpen={() => {}}
                  onRequestLock={() => {}}
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
                        forcedTeam={teamFilter || null}
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
        />
      </div>
      <div className="me-day-progress" title={`${dayProgress}% done today`}>
        <div
          className="me-day-progress-fill"
          style={{ width: `${dayProgress}%` }}
        />
      </div>
    </div>
  );
}
