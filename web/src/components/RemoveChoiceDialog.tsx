import type { Outcome } from "../removal";

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
  /** What the × will actually do. The dialog NAMES it rather than asking
   *  "delete?" in front of something that keeps the card: a project card
   *  goes back to Unassigned, a grouped one comes out of its group, and only
   *  a card with nowhere else to be is taken off the board. */
  outcome?: Outcome;
  onClose: () => void;
  /** true = take the card off the board, false = keep it on `keepOn`. */
  onSubmit: (hardDelete: boolean) => void;
}

/** What the one option says, per outcome: the button names the act and the
 *  line under it says where the card ends up. */
function act(outcome: Outcome, keepOn?: string | null): { label: string; note: string } {
  if (outcome === "leave") {
    return {
      label: "Move it to Unassigned",
      note: "It comes off the day — nobody's, no dates — and stands in the week it is scheduled for.",
    };
  }
  if (outcome === "ungroup") {
    return {
      label: "Take it out of the group",
      note: "It stops being a subtask and stays where it is, in its own column.",
    };
  }
  return {
    label: "Take it off the board",
    note: keepOn
      ? "The card, its notes and its log are gone for good."
      : "The day it stood on keeps it — step back to that day to see it. Today’s board is done with it.",
  };
}

/** RemoveChoiceDialog asks before the ×, and says what it is about to do.
 *
 *  Every × puts this question, whatever it would do — the exception is a
 *  card made today that nobody has touched, which goes without ceremony
 *  (`asksFirst`). A TEAM card the × destroys is taken off for good: the day
 *  it stood on holds it — flip back to that day to see it. A PERSONAL card
 *  has no such day (a personal board keeps no records), so its × still
 *  offers to leave the card on yesterday instead. */
export function RemoveChoiceDialog({
  title,
  progress,
  keepOn,
  subtasks,
  outcome = "delete",
  onClose,
  onSubmit,
}: RemoveChoiceDialogProps) {
  const pick = (hardDelete: boolean) => {
    onSubmit(hardDelete);
    onClose();
  };
  const destroys = outcome === "delete";
  const family =
    subtasks > 0 && destroys
      ? ` Its ${subtasks} subtask${subtasks > 1 ? "s" : ""} go${subtasks > 1 ? "" : "es"} with it.`
      : "";
  const choice = act(outcome, keepOn);

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
          <h2 className="modal-title">
            {destroys ? "Remove from the board?" : "Take it off the day?"}
          </h2>
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
            {keepOn && destroys && (
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
              className={`btn sprint-choice-btn${destroys ? " sprint-choice-btn-danger" : ""}`}
              autoFocus={!keepOn || !destroys}
              onClick={() => pick(true)}
            >
              <strong>{choice.label}</strong>
              <span>{choice.note}</span>
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
