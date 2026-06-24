import { Fragment, useMemo, useState, type Ref } from "react";
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
import { todayIso, addDays } from "../date";
import { initials } from "../avatar";
import { Card } from "./Card";
import { AddCard } from "./AddCard";
import { SortableBoard, type BoardGroup, type DropResult } from "./SortableBoard";
import { globalOrderFromGroups, afterIdFor } from "./dndOrder";

interface TeamBoardProps {
  board: Board;
  provider: Provider;
  me: string;
  patchCard: (itemId: string, patch: Partial<CardModel>) => void;
  addCard: (card: CardModel) => void;
  removeCard: (itemId: string) => void;
  reorderCards: (orderedItemIds: string[]) => void;
  reload: () => void;
  onError: (message: string) => void;
  onOpen: (card: CardModel) => void;
}

/** Per-group metadata for the Team board: the destination engineer + zone. */
interface TeamMeta {
  engineer: string;
  zone: ZoneKey;
}

const UNASSIGNED = "";

const errMessage = (err: unknown) =>
  err instanceof Error ? err.message : String(err);

/** TeamBoard is the whole team as an engineers × zones grid for one day. */
export function TeamBoard({
  board,
  provider,
  me,
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

  const roles = useMemo(() => fieldRoles(board), [board]);

  // Distinct engineer logins across all cards, me first, then an Unassigned col.
  const engineers = useMemo(() => {
    const set = new Set<string>();
    for (const card of board.cards) {
      for (const login of card.assignees) {
        set.add(login);
      }
    }
    if (me) {
      set.add(me);
    }
    const rest = [...set].filter((l) => l !== me).sort((a, b) => a.localeCompare(b));
    const ordered = me ? [me, ...rest] : rest;
    return [...ordered, UNASSIGNED];
  }, [board.cards, me]);

  const fail = (err: unknown) => {
    onError(err instanceof Error ? err.message : String(err));
    reload();
  };

  const cellCards = (engineer: string, zone: ZoneKey): CardModel[] =>
    board.cards.filter((c) => {
      if (c.day !== selectedDate || c.zone !== zone) {
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
    for (const engineer of engineers) {
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
  }, [board.cards, engineers, selectedDate]);

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

  // Locking posts a note (the reason) to the card's log.
  const handleLock = (card: CardModel, note: string) => {
    const prevStage = card.stage;
    const optimisticNote: Note = {
      id: `tmp-${new Date().toISOString()}`,
      body: note,
      createdAt: new Date().toISOString(),
      author: me || undefined,
      source: card.isDraft ? "draft" : "comment",
    };
    patchCard(card.itemId, {
      stage: "locked",
      notes: [...(card.notes ?? []), optimisticNote],
    });
    void (async () => {
      try {
        await provider.setStage(board, card, "locked");
        await provider.addNote(board, card, note);
      } catch (err: unknown) {
        patchCard(card.itemId, { stage: prevStage });
        onError(errMessage(err));
        reload();
      }
    })();
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
    void provider
      .deleteCard(board, card)
      .then(() => removeCard(card.itemId))
      .catch(fail);
  };

  const handleCreate = (engineer: string, zone: ZoneKey, title: string) => {
    const tempId = `tmp-${new Date().toISOString()}`;
    const optimistic: CardModel = {
      itemId: tempId,
      title,
      isDraft: true,
      assignees: engineer ? [engineer] : [],
      zone,
      day: selectedDate,
      description: "",
      notes: [],
    };
    addCard(optimistic);
    void provider
      .createCard(board, {
        title,
        zone,
        day: selectedDate,
        assigneeLogin: engineer || null,
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
              onDelete={handleDelete}
              onStage={handleStage}
              onRename={handleRename}
              onOpen={onOpen}
              onLock={handleLock}
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
              onLock={() => {}}
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
                    onCreate={(title) =>
                      handleCreate(group.meta.engineer, group.meta.zone, title)
                    }
                  />
                </div>
              </div>
            );
          }}
          renderLayout={(nodes) =>
            engineers.map((engineer) => (
              <section className="team-col" key={engineer || "__unassigned__"}>
                <header className="team-col-header">
                  {engineer === UNASSIGNED ? (
                    <span className="team-col-name team-col-unassigned">
                      Unassigned
                    </span>
                  ) : (
                    <>
                      <span
                        className={`avatar${engineer === me ? " avatar-me" : ""}`}
                        title={engineer}
                      >
                        {initials(engineer)}
                      </span>
                      <span
                        className={`team-col-name${engineer === me ? " team-col-me" : ""}`}
                      >
                        {engineer}
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
          }
        />
      </div>
    </div>
  );
}
