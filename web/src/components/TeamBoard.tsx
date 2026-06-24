import { useMemo, useState } from "react";
import type { Board, Card as CardModel, Provider, ZoneKey } from "../providers/types";
import { ZONES, ZONE_ORDER, optionIdForZone } from "../zones";
import { fieldRoles } from "../providers/fields";
import { todayIso } from "../date";
import { Card } from "./Card";
import { AddCard } from "./AddCard";

interface TeamBoardProps {
  board: Board;
  provider: Provider;
  me: string;
  patchCard: (itemId: string, patch: Partial<CardModel>) => void;
  addCard: (card: CardModel) => void;
  removeCard: (itemId: string) => void;
  reload: () => void;
  onError: (message: string) => void;
}

const UNASSIGNED = "";

/** TeamBoard is the whole team as an engineers × zones grid for one day. */
export function TeamBoard({
  board,
  provider,
  me,
  patchCard,
  addCard,
  removeCard,
  reload,
  onError,
}: TeamBoardProps) {
  const [selectedDate, setSelectedDate] = useState<string>(todayIso());
  const [selectedCardId, setSelectedCardId] = useState<string | null>(null);
  const [dragOver, setDragOver] = useState<string | null>(null);

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

  const handleProgress = (card: CardModel, value: number) => {
    patchCard(card.itemId, { progress: value });
    void provider.setProgress(board, card, value).catch(fail);
  };

  const handleDelete = (card: CardModel) => {
    void provider
      .deleteCard(board, card)
      .then(() => removeCard(card.itemId))
      .catch(fail);
  };

  const cellKey = (engineer: string, zone: ZoneKey) => `${engineer}::${zone}`;

  const handleDrop = async (
    engineer: string,
    zone: ZoneKey,
    e: React.DragEvent<HTMLElement>,
  ) => {
    e.preventDefault();
    setDragOver(null);
    const itemId = e.dataTransfer.getData("text/plain");
    if (!itemId) {
      return;
    }
    const card = board.cards.find((c) => c.itemId === itemId);
    if (!card) {
      return;
    }
    const zoneChanged = card.zone !== zone;
    const isAssigned =
      engineer === UNASSIGNED
        ? card.assignees.length === 0
        : card.assignees.length === 1 && card.assignees[0] === engineer;
    const engineerChanged = !isAssigned;
    if (!zoneChanged && !engineerChanged) {
      return;
    }

    let optionId = card.zoneOptionId;
    if (zoneChanged) {
      const resolved = optionIdForZone(roles.zone, zone);
      if (!resolved) {
        onError(`Project has no Zone option for ${zone}`);
        return;
      }
      optionId = resolved;
    }

    try {
      if (zoneChanged && optionId) {
        await provider.setZone(board, card, optionId);
      }
      if (engineerChanged) {
        await provider.setAssignee(board, card, engineer || null);
      }
      patchCard(card.itemId, {
        zone,
        zoneOptionId: optionId,
        assignees: engineer ? [engineer] : [],
      });
    } catch (err: unknown) {
      fail(err);
    }
  };

  const handleCreate = (engineer: string, zone: ZoneKey, title: string) => {
    void provider
      .createCard(board, {
        title,
        zone,
        day: selectedDate,
        assigneeLogin: engineer || null,
      })
      .then((card) => addCard(card))
      .catch(fail);
  };

  return (
    <div className="team">
      <div className="board-toolbar">
        <label className="field field-inline">
          <span>Day</span>
          <input
            type="date"
            value={selectedDate}
            onChange={(e) => setSelectedDate(e.target.value || todayIso())}
          />
        </label>
      </div>

      <div className="team-grid">
        {engineers.map((engineer) => (
          <section className="team-col" key={engineer || "__unassigned__"}>
            <header className="team-col-header">
              {engineer === UNASSIGNED ? (
                <span className="team-col-name team-col-unassigned">Unassigned</span>
              ) : (
                <span
                  className={`team-col-name${engineer === me ? " team-col-me" : ""}`}
                >
                  {engineer}
                </span>
              )}
            </header>
            <div className="team-col-zones">
              {ZONE_ORDER.map((zone) => {
                const def = ZONES[zone];
                const cards = cellCards(engineer, zone);
                const key = cellKey(engineer, zone);
                return (
                  <div
                    key={zone}
                    className={`zone-area${dragOver === key ? " zone-area-dragover" : ""}`}
                    style={{ background: def.background, borderLeftColor: def.accent }}
                    onDragOver={(e) => {
                      e.preventDefault();
                      e.dataTransfer.dropEffect = "move";
                      if (dragOver !== key) {
                        setDragOver(key);
                      }
                    }}
                    onDragLeave={() =>
                      setDragOver((k) => (k === key ? null : k))
                    }
                    onDrop={(e) => void handleDrop(engineer, zone, e)}
                  >
                    <div className="zone-cards">
                      {cards.map((card) => (
                        <Card
                          key={card.itemId}
                          card={card}
                          me={me}
                          selected={card.itemId === selectedCardId}
                          onSelect={(c) => setSelectedCardId(c.itemId)}
                          onProgress={handleProgress}
                          onDelete={handleDelete}
                        />
                      ))}
                      <AddCard
                        onCreate={(title) => handleCreate(engineer, zone, title)}
                      />
                    </div>
                  </div>
                );
              })}
            </div>
          </section>
        ))}
      </div>
    </div>
  );
}
