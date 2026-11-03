import type { ReactNode } from "react";

/** InsertGap is one gap of a list of cards — above the first, between two,
 *  below the last — and the insert "+" it shows at its left edge on hover:
 *  pressed, the gap opens the add form right there (insertgap.ts says where
 *  the card lands). Shared by the Backlog, the Team grid and the Me board.
 *
 *  `empty` is the gap of a list with no card: its "+" shows on hover of the
 *  list's own area (a cell, a band); `always` shows it at rest (an empty
 *  backlog, which has nothing else to hover). */
export function InsertGap({
  at,
  open,
  empty = false,
  always = false,
  onOpen,
  children,
}: {
  at: number;
  open: boolean;
  empty?: boolean;
  always?: boolean;
  onOpen: () => void;
  /** The add form, drawn while the gap is open. */
  children?: ReactNode;
}) {
  const cls = `insert-gap${empty ? " insert-gap-empty" : ""}${always ? " insert-gap-always" : ""}${open ? " insert-gap-open" : ""}`;
  return (
    <div className={cls} data-insert-gap={at}>
      {open ? (
        children
      ) : (
        <button type="button" className="insert-gap-btn" aria-label="Add a card here" title="Add a card here" onClick={onOpen}>
          +
        </button>
      )}
    </div>
  );
}
