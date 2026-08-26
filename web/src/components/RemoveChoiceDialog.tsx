interface RemoveChoiceDialogProps {
  title: string;
  /** How far along the card is — what is at stake in a delete. */
  progress: number;
  /** The sprint the card would be kept in. */
  previous: string;
  /** How many subtasks ride along with it, if any. */
  subtasks: number;
  onClose: () => void;
  /** true = delete the card outright, false = keep it in the previous sprint. */
  onSubmit: (hardDelete: boolean) => void;
}

/** RemoveChoiceDialog asks what the × means for a card that has been worked
 *  on. The board's own answer is to keep it in the previous sprint, but that
 *  takes the card — and every subtask riding it — off today's board with no
 *  trace, which reads exactly like deletion. When there is progress to lose,
 *  the person decides. */
export function RemoveChoiceDialog({
  title,
  progress,
  previous,
  subtasks,
  onClose,
  onSubmit,
}: RemoveChoiceDialogProps) {
  const pick = (hardDelete: boolean) => {
    onSubmit(hardDelete);
    onClose();
  };
  const family =
    subtasks > 0
      ? ` Its ${subtasks} subtask${subtasks > 1 ? "s" : ""} follow${subtasks > 1 ? "" : "s"} it either way.`
      : "";

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div
        className="modal modal-narrow"
        role="dialog"
        aria-modal="true"
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => {
          if (e.key === "Escape") {
            onClose();
          }
        }}
      >
        <div className="modal-header">
          <h2 className="modal-title">Remove from the board?</h2>
          <button
            type="button"
            className="modal-close"
            onClick={onClose}
            aria-label="Close"
          >
            ✕
          </button>
        </div>

        <div className="modal-body">
          <p className="sprint-choice-text">
            “{title}” is {progress}% done.{family}
          </p>
          <div className="sprint-choice-options">
            <button
              type="button"
              className="btn sprint-choice-btn"
              autoFocus
              onClick={() => pick(false)}
            >
              <strong>Keep it in {previous}</strong>
              <span>
                Off today’s board, but the card and its history stay — find it
                by stepping back a day.
              </span>
            </button>
            <button
              type="button"
              className="btn sprint-choice-btn sprint-choice-btn-danger"
              onClick={() => pick(true)}
            >
              <strong>Delete it</strong>
              <span>The card, its notes and its log are gone for good.</span>
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
