import type { RemoveChoice } from "../removal";

interface RemoveChoiceDialogProps {
  title: string;
  /** How far along the card is — what is at stake. */
  progress: number;
  /** What the × may do to this card where it stands (removal.removeChoices),
   *  in its own order. The dialog offers exactly these and nothing
   *  else: a card that cannot be destroyed is never shown a button that
   *  says it can. */
  choices: RemoveChoice[];
  /** A REVIEW card: its backlog answer takes the reviewer off (K10), and
   *  the dialog says so. */
  review?: boolean;
  /** The title of the task a review card reviews: the withdrawal says what
   *  becomes of it. */
  reviewing?: string;
  /** How many subtasks go with it, if any. */
  subtasks: number;
  /** What a destroying answer does to what sits under the card, said in
   *  place of the subtask sentence — the hierarchy's × moves children UP
   *  rather than taking them along, and the dialog must say which. */
  note?: string;
  onClose: () => void;
  onSubmit: (choice: RemoveChoice) => void;
}

/** What each choice says: the button names the act, the line under it says
 *  where the card ends up. */
function say(choice: RemoveChoice, review = false) {
  switch (choice) {
    case "detach":
      return {
        label: "Take it out from under its parent",
        note: "It keeps everything else — its boards, its dates, what sits under it — and simply stops being filed here.",
        danger: false,
      };
    case "delete":
      return {
        label: "Delete it",
        note: "The card goes, with its description and its history.",
        danger: true,
      };
    case "unassign":
      return {
        label: "Move it to Unassigned",
        note: "Nobody holds it any more; it stays in its column on the Project board.",
        danger: false,
      };
    case "ungroup":
      return {
        label: "Take it out of the group",
        note: "It stops being a subtask and stays where it is, in its own column.",
        danger: false,
      };
    case "finished-earlier":
      return {
        label: "It was finished in the previous sprint",
        note: "The work goes back to the sprint it was done in, and counts there instead of here.",
        danger: false,
      };
    case "backlog":
      if (review) {
        return {
          label: "Back to backlog",
          note: "The reviewer is taken off; it waits at the top of its task's team backlog for somebody to review it. The task stays on review.",
          danger: false,
        };
      }
      return {
        label: "Send it back to the backlog",
        note: "Out of work and back in its team's backlog — nobody, no day, no sprint. Nothing is lost.",
        danger: false,
      };
    default:
      return {
        label: "Take it off the board",
        note: "The day it stood on keeps it — step back to that day to see it. Today’s board is done with it.",
        danger: true,
      };
  }
}

/** RemoveChoiceDialog asks before the ×, and says what it is about to do.
 *
 *  Every × puts this question, whatever it would do — the exception is a card
 *  made today that nobody has touched, which goes without ceremony
 *  (`asksFirst`). What it offers is the card's own list: a task can be taken
 *  off the board or sent back to its team's backlog (K3); a project card can
 *  only be moved to Unassigned, in its column, since it is not this board's
 *  to destroy. */
export function RemoveChoiceDialog({
  title,
  progress,
  choices,
  review = false,
  reviewing,
  subtasks,
  note,
  onClose,
  onSubmit,
}: RemoveChoiceDialogProps) {
  const pick = (choice: RemoveChoice) => {
    onSubmit(choice);
    onClose();
  };
  // A review nobody has taken has one answer, and it is about the TASK:
  // withdrawing the review takes it off review, back to In progress (K10).
  // The question says that, and Cancel is the armed button.
  if (choices.length === 1 && choices[0] === "withdraw") {
    return (
      <div className="modal-backdrop" onClick={onClose}>
        <div
          className="modal modal-narrow"
          role="dialog"
          aria-modal="true"
          aria-label="Withdraw the review?"
          onClick={(e) => e.stopPropagation()}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              onClose();
            }
          }}
        >
          <div className="modal-header">
            <h2 className="modal-title">Withdraw the review?</h2>
            <button type="button" className="modal-close" onClick={onClose} aria-label="Close">
              ✕
            </button>
          </div>
          <div className="modal-body">
            <p className="sprint-choice-text">«{reviewing ?? title.replace(/^review: /, "")}» goes back to In progress.</p>
            <div className="take-dialog-actions">
              <button type="button" className="btn" autoFocus onClick={onClose}>
                Cancel
              </button>
              <button type="button" className="btn btn-primary" onClick={() => pick("withdraw")}>
                Withdraw
              </button>
            </div>
          </div>
        </div>
      </div>
    );
  }
  const hierarchy = choices.includes("delete");
  const destroys = choices.includes("off-board") || hierarchy;
  const family = note
    ? ` ${note}`
    : subtasks > 0 && destroys
      ? ` Its ${subtasks} subtask${subtasks > 1 ? "s" : ""} go${subtasks > 1 ? "" : "es"} with it.`
      : "";
  // The safe option takes the focus where there is one: a dialog that opens
  // with the destructive button armed answers itself on a stray Enter.
  const focusOn = choices.find((c) => !say(c, review).danger) ?? choices[0];

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
            {hierarchy ? "Delete it, or just unlink it?" : destroys ? "Remove from the board?" : "Take it off the day?"}
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
            {choices.map((choice) => {
              const { label, note, danger } = say(choice, review);
              return (
                <button
                  key={choice}
                  type="button"
                  className={`btn sprint-choice-btn${danger ? " sprint-choice-btn-danger" : ""}`}
                  autoFocus={choice === focusOn}
                  onClick={() => pick(choice)}
                >
                  <strong>{label}</strong>
                  <span>{note}</span>
                </button>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}
