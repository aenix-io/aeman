import { useState } from "react";
import type { PersonalPointer } from "../personal";
import { samePointer } from "../personal";

interface PersonalDialogProps {
  /** The signed-in login: the personal project must live under it. */
  login: string;
  /** The currently attached pointer, if any. */
  current: PersonalPointer | null;
  /** The work board address, to refuse attaching the same project. */
  workBoard: { owner: string; number: number } | null;
  onAttach: (ptr: PersonalPointer) => void;
  onDetach: () => void;
  onClose: () => void;
}

/**
 * PersonalDialog attaches a personal board: a GitHub Project in the user's OWN
 * account (make it private — that is what keeps personal cards invisible to
 * everyone else; aeman adds no sharing of its own). Only the project number is
 * asked for; the owner is fixed to the signed-in login on purpose — a personal
 * board under an org owner would be readable by that org's members.
 */
export function PersonalDialog({
  login,
  current,
  workBoard,
  onAttach,
  onDetach,
  onClose,
}: PersonalDialogProps) {
  const [number, setNumber] = useState<string>(
    current ? String(current.number) : "",
  );
  const [problem, setProblem] = useState<string | null>(null);

  const commit = () => {
    const n = Number(number);
    if (!Number.isInteger(n) || n <= 0) {
      setProblem("Enter a positive project number.");
      return;
    }
    const ptr = { owner: login, number: n };
    if (workBoard && samePointer(ptr, workBoard)) {
      setProblem(
        "That is the work board itself — attach a separate project in your own account.",
      );
      return;
    }
    onAttach(ptr);
    onClose();
  };

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div
        className="modal personal-modal"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-label="Personal board"
      >
        <div className="modal-header">
          <h2 className="modal-title">Personal board</h2>
          <button
            type="button"
            className="modal-close"
            onClick={onClose}
            aria-label="Close"
          >
            ×
          </button>
        </div>
        <div className="modal-body">
          <p className="personal-hint">
            A second board for your own tasks, shown beside the work board —
            visible to you only. It is a GitHub Project in <b>your</b> account
            (<code>{login}</code>). Create one once, keep it{" "}
            <b>private</b>:
          </p>
          <pre className="personal-cmd">
            gh project create --owner @me --title "aeman personal"
          </pre>
          <label className="field">
            <span>Project #</span>
            <input
              type="number"
              min={1}
              value={number}
              placeholder="1"
              autoFocus
              onChange={(e) => {
                setNumber(e.target.value);
                setProblem(null);
              }}
              onKeyDown={(e) => e.key === "Enter" && commit()}
            />
          </label>
          {problem && <p className="personal-problem">{problem}</p>}
        </div>
        <div className="modal-footer">
          {current && (
            <button
              type="button"
              className="btn personal-detach"
              onClick={() => {
                onDetach();
                onClose();
              }}
              title="Detach the personal board (the project itself is untouched)"
            >
              Detach
            </button>
          )}
          <button type="button" className="btn" onClick={onClose}>
            Cancel
          </button>
          <button type="button" className="btn btn-primary" onClick={commit}>
            {current ? "Save" : "Attach"}
          </button>
        </div>
      </div>
    </div>
  );
}
