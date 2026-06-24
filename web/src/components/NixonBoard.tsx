import { useMemo } from "react";
import type {
  Board,
  Card as CardModel,
  Provider,
  ZoneKey,
} from "../providers/types";
import { ZONES, ZONE_ORDER, optionIdForZone } from "../zones";
import { fieldRoles } from "../providers/fields";
import { Card } from "./Card";

interface NixonBoardProps {
  board: Board;
  provider: Provider;
  patchCard: (itemId: string, patch: Partial<CardModel>) => void;
  reload: () => void;
  onError: (message: string) => void;
}

type GroupKey = ZoneKey | "unzoned";

const GROUP_ORDER: GroupKey[] = [...ZONE_ORDER, "unzoned"];

function groupTitle(group: GroupKey): string {
  return group === "unzoned" ? "Unzoned" : ZONES[group].title;
}

/** NixonBoard is the planning backlog: every card grouped by zone. */
export function NixonBoard({
  board,
  provider,
  patchCard,
  reload,
  onError,
}: NixonBoardProps) {
  const roles = useMemo(() => fieldRoles(board), [board]);

  const grouped = useMemo(() => {
    const buckets: Record<GroupKey, CardModel[]> = {
      gray: [],
      green: [],
      yellow: [],
      red: [],
      unzoned: [],
    };
    for (const card of board.cards) {
      const group: GroupKey = card.zone ?? "unzoned";
      buckets[group].push(card);
    }
    return buckets;
  }, [board.cards]);

  const fail = (err: unknown) => {
    onError(err instanceof Error ? err.message : String(err));
    reload();
  };

  const handleProgress = (card: CardModel, value: number) => {
    void provider
      .setProgress(board, card, value)
      .then(() => patchCard(card.itemId, { progress: value }))
      .catch(fail);
  };

  const handleZone = (card: CardModel, value: ZoneKey | "") => {
    if (value === "") {
      void provider
        .setZone(board, card, null)
        .then(() => patchCard(card.itemId, { zone: undefined, zoneOptionId: undefined }))
        .catch(fail);
      return;
    }
    const optionId = optionIdForZone(roles.zone, value);
    if (!optionId) {
      onError(`Project has no Zone option for ${value}`);
      return;
    }
    void provider
      .setZone(board, card, optionId)
      .then(() => patchCard(card.itemId, { zone: value, zoneOptionId: optionId }))
      .catch(fail);
  };

  const handleDay = (card: CardModel, value: string) => {
    const day = value || null;
    void provider
      .setDay(board, card, day)
      .then(() => patchCard(card.itemId, { day: day ?? undefined }))
      .catch(fail);
  };

  return (
    <div className="nixon">
      {GROUP_ORDER.map((group) => {
        const cards = grouped[group];
        const accent = group === "unzoned" ? "#d0d7de" : ZONES[group].accent;
        return (
          <section className="nixon-group" key={group}>
            <header className="nixon-group-header">
              <span className="column-accent" style={{ backgroundColor: accent }} />
              <span className="column-title">{groupTitle(group)}</span>
              <span className="column-count">{cards.length}</span>
            </header>
            <div className="nixon-group-body">
              {cards.map((card) => (
                <Card
                  key={card.itemId}
                  card={card}
                  mode="nixon"
                  onProgress={handleProgress}
                  zoneSelect={
                    <label className="field field-inline">
                      <span>Zone</span>
                      <select
                        value={card.zone ?? ""}
                        onChange={(e) =>
                          handleZone(card, e.target.value as ZoneKey | "")
                        }
                      >
                        <option value="">—</option>
                        {ZONE_ORDER.map((z) => (
                          <option key={z} value={z}>
                            {ZONES[z].title}
                          </option>
                        ))}
                      </select>
                    </label>
                  }
                  daySelect={
                    <label className="field field-inline">
                      <span>Day</span>
                      <input
                        type="date"
                        value={card.day ?? ""}
                        onChange={(e) => handleDay(card, e.target.value)}
                      />
                    </label>
                  }
                />
              ))}
              {cards.length === 0 && <p className="column-empty">No cards</p>}
            </div>
          </section>
        );
      })}
    </div>
  );
}
