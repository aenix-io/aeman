import { useMemo, useState } from "react";
import type { Board, Card as CardModel, Note, Provider, ZoneKey } from "../providers/types";
import { ZONES, ZONE_ORDER, optionIdForZone } from "../zones";
import { fieldRoles } from "../providers/fields";
import { todayIso, localDateIso } from "../date";
import { Card } from "./Card";
import { AddCard } from "./AddCard";
import { NotesPanel, type DayNote } from "./NotesPanel";

interface MeBoardProps {
  board: Board;
  provider: Provider;
  me: string;
  patchCard: (itemId: string, patch: Partial<CardModel>) => void;
  addCard: (card: CardModel) => void;
  removeCard: (itemId: string) => void;
  reload: () => void;
  onError: (message: string) => void;
}

/** MeBoard is the personal day view: my cards stacked in zone bands + notes. */
export function MeBoard({
  board,
  provider,
  me,
  patchCard,
  addCard,
  removeCard,
  reload,
  onError,
}: MeBoardProps) {
  const [selectedDate, setSelectedDate] = useState<string>(todayIso());
  const [selectedCardId, setSelectedCardId] = useState<string | null>(null);
  const [dragOver, setDragOver] = useState<ZoneKey | null>(null);

  const roles = useMemo(() => fieldRoles(board), [board]);

  // My cards for the selected day. When me is empty, show everyone's cards.
  const myCards = useMemo(() => {
    return board.cards.filter((c) => {
      if (c.day !== selectedDate) {
        return false;
      }
      return me ? c.assignees.includes(me) : true;
    });
  }, [board.cards, me, selectedDate]);

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
    patchCard(card.itemId, { progress: value });
    void provider.setProgress(board, card, value).catch(fail);
  };

  const handleDelete = (card: CardModel) => {
    void provider
      .deleteCard(board, card)
      .then(() => removeCard(card.itemId))
      .catch(fail);
  };

  const handleDrop = (zone: ZoneKey, e: React.DragEvent<HTMLElement>) => {
    e.preventDefault();
    setDragOver(null);
    const itemId = e.dataTransfer.getData("text/plain");
    if (!itemId) {
      return;
    }
    const card = board.cards.find((c) => c.itemId === itemId);
    if (!card || card.zone === zone) {
      return;
    }
    const optionId = optionIdForZone(roles.zone, zone);
    if (!optionId) {
      onError(`Project has no Zone option for ${zone}`);
      return;
    }
    void provider
      .setZone(board, card, optionId)
      .then(() => patchCard(card.itemId, { zone, zoneOptionId: optionId }))
      .catch(fail);
  };

  const handleCreate = (zone: ZoneKey, title: string) => {
    void provider
      .createCard(board, { title, zone, day: selectedDate, assigneeLogin: me || null })
      .then((card) => addCard(card))
      .catch(fail);
  };

  const handleAddNote = (text: string) => {
    if (!selectedCard) {
      return;
    }
    const optimistic: Note = {
      id: `tmp-${new Date().toISOString()}`,
      body: text,
      createdAt: new Date().toISOString(),
      author: me || undefined,
      source: selectedCard.isDraft ? "draft" : "comment",
    };
    patchCard(selectedCard.itemId, {
      notes: [...(selectedCard.notes ?? []), optimistic],
    });
    void provider.addNote(board, selectedCard, text).catch(fail);
  };

  return (
    <div className="me">
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

      <div className="me-panes">
        <div className="me-left">
          {ZONE_ORDER.map((zone) => {
            const def = ZONES[zone];
            const cards = byZone[zone];
            return (
              <section
                key={zone}
                className={`zone-area${dragOver === zone ? " zone-area-dragover" : ""}`}
                style={{ background: def.background, borderLeftColor: def.accent }}
                onDragOver={(e) => {
                  e.preventDefault();
                  e.dataTransfer.dropEffect = "move";
                  if (dragOver !== zone) {
                    setDragOver(zone);
                  }
                }}
                onDragLeave={() => setDragOver((z) => (z === zone ? null : z))}
                onDrop={(e) => handleDrop(zone, e)}
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
                  <AddCard onCreate={(title) => handleCreate(zone, title)} />
                </div>
              </section>
            );
          })}
        </div>

        <NotesPanel
          selectedDate={selectedDate}
          notes={dayNotes}
          selectedCard={selectedCard}
          onSelectCard={(c) => setSelectedCardId(c.itemId)}
          onAddNote={handleAddNote}
        />
      </div>
    </div>
  );
}
