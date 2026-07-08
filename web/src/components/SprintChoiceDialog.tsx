interface SprintChoiceDialogProps {
  title: string;
  /** The day the card is being created on. */
  day: string;
  /** The team's current sprint day the card would otherwise join. */
  sprint: string;
  onClose: () => void;
  /** Called with the choice: false = join the current sprint, true = wait for
   *  the next one (no sprint; the next carry-over to reach the day adopts it). */
  onSubmit: (noSprint: boolean) => void;
}

/** SprintChoiceDialog asks where a card created ahead of the team's current
 *  sprint belongs: only the lead knows whether the day is a later day of the
 *  running sprint or the start of the next one. */
export function SprintChoiceDialog({
  title,
  day,
  sprint,
  onClose,
  onSubmit,
}: SprintChoiceDialogProps) {
  const pick = (noSprint: boolean) => {
    onSubmit(noSprint);
    onClose();
  };

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
          <h2 className="modal-title">Which sprint?</h2>
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
            “{title}” is scheduled for {day}, ahead of the current sprint (
            {sprint}).
          </p>
          <div className="sprint-choice-options">
            <button
              type="button"
              className="btn sprint-choice-btn"
              autoFocus
              onClick={() => pick(false)}
            >
              <strong>Current sprint</strong>
              <span>
                A later day of the running sprint — it shows with the sprint's
                work right away.
              </span>
            </button>
            <button
              type="button"
              className="btn sprint-choice-btn"
              onClick={() => pick(true)}
            >
              <strong>Next sprint</strong>
              <span>
                Waits on its own day and joins the sprint the next carry over
                starts.
              </span>
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
