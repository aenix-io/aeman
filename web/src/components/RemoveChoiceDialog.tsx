interface RemoveChoiceDialogProps {
  title: string;
  /** How far along the card is — what is at stake. */
  progress: number;
  /** The day a PERSONAL card would be left behind on, and with it the offer
   *  to keep it there. A team card has no such offer: its × takes it off the
   *  board and the day it stood on is what keeps it (G60). */
  keepOn?: string | null;
  /** How many subtasks go with it, if any. */
  subtasks: number;
  onClose: () => void;
  /** true = take the card off the board, false = keep it on `keepOn`. */
  onSubmit: (hardDelete: boolean) => void;
}

/** RemoveChoiceDialog asks before an × that takes work off the board.
 *
 *  A TEAM card is taken off for good: the day it stood on holds it — flip
 *  back to that day to see it — so there is one action to confirm and the
 *  words say where the card goes. A PERSONAL card has no such day (a
 *  personal board keeps no records), so its × still offers to leave the card
 *  on yesterday instead. */
export function RemoveChoiceDialog({
  title,
  progress,
  keepOn,
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
      ? ` Its ${subtasks} subtask${subtasks > 1 ? "s" : ""} go${subtasks > 1 ? "" : "es"} with it.`
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
            {keepOn && (
              <button
                type="button"
                className="btn sprint-choice-btn"
                autoFocus
                onClick={() => pick(false)}
              >
                <strong>Keep it on {keepOn}</strong>
                <span>
                  Off today’s board, but the card stays — find it by stepping
                  back a day.
                </span>
              </button>
            )}
            <button
              type="button"
              className="btn sprint-choice-btn sprint-choice-btn-danger"
              autoFocus={!keepOn}
              onClick={() => pick(true)}
            >
              <strong>Take it off the board</strong>
              <span>
                {keepOn
                  ? "The card, its notes and its log are gone for good."
                  : "The day it stood on keeps it — step back to that day to see it. Today’s board is done with it."}
              </span>
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
