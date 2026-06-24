import { useMemo, useState } from "react";
import type {
  Board,
  Card as CardModel,
  Provider,
  ZoneKey,
} from "../providers/types";
import { ZONES, ZONE_ORDER, optionIdForZone } from "../zones";
import { fieldRoles } from "../providers/fields";
import { Card } from "./Card";

interface FordBoardProps {
  board: Board;
  provider: Provider;
  patchCard: (itemId: string, patch: Partial<CardModel>) => void;
  reload: () => void;
  onError: (message: string) => void;
}

function todayIso(): string {
  const now = new Date();
  const off = now.getTimezoneOffset();
  return new Date(now.getTime() - off * 60_000).toISOString().slice(0, 10);
}

/** FordBoard is the per-day Ford board: four zone columns for the chosen date. */
export function FordBoard({
  board,
  provider,
  patchCard,
  reload,
  onError,
}: FordBoardProps) {
  const [selectedDate, setSelectedDate] = useState<string>(todayIso());
  const [dragOver, setDragOver] = useState<ZoneKey | null>(null);

  const roles = useMemo(() => fieldRoles(board), [board]);

  // Cards planned for the selected day, bucketed by zone (no zone -> gray).
  const byZone = useMemo(() => {
    const buckets: Record<ZoneKey, CardModel[]> = {
      gray: [],
      green: [],
      yellow: [],
      red: [],
    };
    for (const card of board.cards) {
      if (card.day !== selectedDate) {
        continue;
      }
      const zone: ZoneKey = card.zone ?? "gray";
      buckets[zone].push(card);
    }
    return buckets;
  }, [board.cards, selectedDate]);

  const handleProgress = (card: CardModel, value: number) => {
    void provider
      .setProgress(board, card, value)
      .then(() => patchCard(card.itemId, { progress: value }))
      .catch((err: unknown) => {
        onError(err instanceof Error ? err.message : String(err));
        reload();
      });
  };

  const handleDrop = (targetZone: ZoneKey, e: React.DragEvent<HTMLElement>) => {
    e.preventDefault();
    setDragOver(null);
    const itemId = e.dataTransfer.getData("text/plain");
    if (!itemId) {
      return;
    }
    const card = board.cards.find((c) => c.itemId === itemId);
    if (!card || card.zone === targetZone) {
      return;
    }
    const optionId = optionIdForZone(roles.zone, targetZone);
    if (!optionId) {
      onError(`Project has no Zone option for ${targetZone}`);
      return;
    }
    void provider
      .setZone(board, card, optionId)
      .then(() => patchCard(card.itemId, { zone: targetZone, zoneOptionId: optionId }))
      .catch((err: unknown) => {
        onError(err instanceof Error ? err.message : String(err));
        reload();
      });
  };

  return (
    <div className="ford">
      <div className="ford-toolbar">
        <label className="field">
          <span>Day</span>
          <input
            type="date"
            value={selectedDate}
            onChange={(e) => setSelectedDate(e.target.value || todayIso())}
          />
        </label>
      </div>

      <div className="ford-grid">
        {ZONE_ORDER.map((zone) => {
          const def = ZONES[zone];
          const cards = byZone[zone];
          return (
            <section
              key={zone}
              className={`column${dragOver === zone ? " column-dragover" : ""}`}
              style={{ backgroundColor: def.background, borderTopColor: def.accent }}
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
              <header className="column-header" title={def.description}>
                <span className="column-accent" style={{ backgroundColor: def.accent }} />
                <span className="column-title">{def.title}</span>
                <span className="column-count">{cards.length}</span>
              </header>
              <div className="column-body">
                {cards.map((card) => (
                  <Card
                    key={card.itemId}
                    card={card}
                    mode="ford"
                    onProgress={handleProgress}
                  />
                ))}
                {cards.length === 0 && <p className="column-empty">No cards</p>}
              </div>
            </section>
          );
        })}
      </div>
    </div>
  );
}
