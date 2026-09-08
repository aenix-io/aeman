// What a card WEIGHS, on the face of the card, wherever a board plans a
// week: the Triage grid, and the backlog beside it — work is sized where it
// is being weighed up, and walking to a card's own page for one letter is
// what stops anyone doing it.
//
// A card nobody has sized wears the letter it is COUNTED as — M, or S for a
// review — in an empty dashed chip rather than a solid one: that letter is
// what the numbers on the board are made of, and a blank said only that
// nobody had spoken while hiding what the board was doing about it. Dashed
// and quiet, it is still the thing a sync scans for, and it is not a claim
// anybody made.
import type { Card as CardModel } from "../providers/types";
import { SIZES, assumedSize } from "../size";

interface SizeChipProps {
  card: CardModel;
  /** Open the picker under this chip. The menu itself belongs to the board,
   *  which has one for every chip on it. */
  onPick: (card: CardModel, anchor: HTMLElement) => void;
}

export function SizeChip({ card, onPick }: SizeChipProps) {
  const shown = assumedSize(card);
  return (
    <button
      type="button"
      className={`size-chip${card.size ? "" : " size-chip-unset"}`}
      title={
        card.size
          ? `${card.size} — ${SIZES[card.size].hint}. Click to change`
          : `Nobody has sized this: counted as ${shown} — ${SIZES[shown].hint}. Click to say`
      }
      aria-label={card.size ? `Size ${card.size}` : `Unsized, counted as ${shown}`}
      // A chip lives on things that are dragged and double-clicked; neither
      // gesture is meant for it.
      onPointerDown={(e) => e.stopPropagation()}
      onDoubleClick={(e) => e.stopPropagation()}
      onClick={(e) => {
        e.stopPropagation();
        onPick(card, e.currentTarget);
      }}
    >
      {shown}
    </button>
  );
}
