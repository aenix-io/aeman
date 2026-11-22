interface GroupBacklogDialogProps {
  /** The plan item the group is drawn for ("Ungrouped" for none). */
  title: string;
  /** The group's cards in this cell that go: those the × may send back. */
  sending: number;
  /** The group's cards in this cell that stay: finished, or with no backlog
   *  answer of their own on this board. */
  staying: number;
  onClose: () => void;
  onSubmit: () => void;
}

/** GroupBacklogDialog is the × of a "group by plan" head: it sends the
 *  group's work in this cell back to the backlog — each card by its own
 *  backlog answer, the one its × offers (nobody, no day, no sprint; a review
 *  card's reviewer taken off) — and says first how much goes and what
 *  stays. Nothing is destroyed, so the act is the focused button. */
export function GroupBacklogDialog({ title, sending, staying, onClose, onSubmit }: GroupBacklogDialogProps) {
  const many = (n: number, one: string) => `${n} ${one}${n === 1 ? "" : "s"}`;
  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div
        className="modal modal-narrow"
        role="dialog"
        aria-modal="true"
        aria-label="Send the group back to the backlog?"
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => {
          if (e.key === "Escape") {
            onClose();
          }
        }}
      >
        <div className="modal-header">
          <h2 className="modal-title">Send it back to the backlog?</h2>
          <button type="button" className="modal-close" onClick={onClose} aria-label="Close">
            ✕
          </button>
        </div>
        <div className="modal-body">
          <p className="sprint-choice-text">
            {sending > 0
              ? `${many(sending, "task")} of «${title}» here go out of work and back to their team's backlog — nobody, no day, no sprint. Nothing is lost.`
              : `Nothing of «${title}» here can go back to the backlog.`}
            {staying > 0 ? ` ${many(staying, "other task")} stay${staying === 1 ? "s" : ""} where ${staying === 1 ? "it is" : "they are"}: finished, or not this board's to take off.` : ""}
          </p>
          <div className="take-dialog-actions">
            <button type="button" className="btn" onClick={onClose}>
              Cancel
            </button>
            {sending > 0 && (
              <button
                type="button"
                className="btn btn-primary"
                autoFocus
                onClick={() => {
                  onSubmit();
                  onClose();
                }}
              >
                Send to backlog
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
