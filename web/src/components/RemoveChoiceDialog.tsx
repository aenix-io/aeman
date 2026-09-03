import type { RemoveChoice } from "../removal";

interface RemoveChoiceDialogProps {
  title: string;
  /** How far along the card is — what is at stake. */
  progress: number;
  /** What the × may do to this card where it stands (removal.removeChoices),
   *  most destructive first. The dialog offers exactly these and nothing
   *  else: a card that cannot be destroyed is never shown a button that
   *  says it can. */
  choices: RemoveChoice[];
  /** The day a PERSONAL card would be left behind on, named on the "keep"
   *  option. */
  keepOn?: string | null;
  /** How many subtasks go with it, if any. */
  subtasks: number;
  onClose: () => void;
  onSubmit: (choice: RemoveChoice) => void;
}

/** What each choice says: the button names the act, the line under it says
 *  where the card ends up. */
function say(choice: RemoveChoice, keepOn?: string | null) {
  switch (choice) {
    case "unassign":
      return {
        label: "Move it to Unassigned",
        note: "It comes off the day — nobody’s, no dates — and stands in the week it is scheduled for.",
        danger: false,
      };
    case "ungroup":
      return {
        label: "Take it out of the group",
        note: "It stops being a subtask and stays where it is, in its own column.",
        danger: false,
      };
    case "keep":
      return {
        label: `Keep it on ${keepOn}`,
        note: "Off today’s board, but the card stays — find it by stepping back a day.",
        danger: false,
      };
    default:
      return {
        label: "Take it off the board",
        note: keepOn
          ? "The card, its notes and its log are gone for good."
          : "The day it stood on keeps it — step back to that day to see it. Today’s board is done with it.",
        danger: true,
      };
  }
}

/** RemoveChoiceDialog asks before the ×, and says what it is about to do.
 *
 *  Every × puts this question, whatever it would do — the exception is a card
 *  made today that nobody has touched, which goes without ceremony
 *  (`asksFirst`). What it offers is the card's own list: an ordinary card can
 *  be taken off the board, and moved to Unassigned when it has a week or a
 *  column to be left in; a project card or a process turn can only be moved
 *  there, since neither is this board's to destroy. */
export function RemoveChoiceDialog({
  title,
  progress,
  choices,
  keepOn,
  subtasks,
  onClose,
  onSubmit,
}: RemoveChoiceDialogProps) {
  const pick = (choice: RemoveChoice) => {
    onSubmit(choice);
    onClose();
  };
  const destroys = choices.includes("off-board");
  const family =
    subtasks > 0 && destroys
      ? ` Its ${subtasks} subtask${subtasks > 1 ? "s" : ""} go${subtasks > 1 ? "" : "es"} with it.`
      : "";
  // The safe option takes the focus where there is one: a dialog that opens
  // with the destructive button armed answers itself on a stray Enter.
  const focusOn = choices.find((c) => !say(c, keepOn).danger) ?? choices[0];

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
            {choices.map((choice) => {
              const { label, note, danger } = say(choice, keepOn);
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
