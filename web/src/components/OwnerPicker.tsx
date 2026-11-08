import { useRef, useState, type ReactNode } from "react";
import { teamColor } from "../avatar";
import { ownerStep, peopleOfTeam, pickerTeams, type Owner } from "../owner";
import type { Card as CardModel } from "../providers/types";
import { displayName, type Avatars, type Names } from "../users";
import { Avatar } from "./Avatar";
import { Dropdown } from "./Dropdown";

interface OwnerPickerProps {
  /** The owner as the card has it now. */
  current: Owner;
  teams: readonly string[];
  people: readonly string[];
  /** The board's cards, to put the people with work on a team first. */
  cards: readonly Pick<CardModel, "team" | "assignees" | "stage" | "progress">[];
  avatars?: Avatars;
  names?: Names;
  disabled?: boolean;
  /** The answer: a team and a person, a team alone, or nobody. */
  onPick: (owner: Owner) => void;
  /** What the trigger shows; the picker wraps it in the button. */
  children: ReactNode;
  className?: string;
  title?: string;
  /** A goal, an initiative or an epic: the person is its OWNER, who answers
   *  for it, and needs no team (ownership.md, O9). */
  planning?: boolean;
}

/** OwnerPicker is the Team board's "whose is this?" menu — a team column
 *  on the left, a person column on the right — asked the Strategy board's way: a
 *  team is picked first and the menu stays open on it, the people come up
 *  on the right (those with work on the team first), a person cannot be
 *  picked before a team, and a team with "No person" is an answer of its
 *  own. "No owner" clears both. The rule is owner.ts's; the menu draws it. */
export function OwnerPicker({ current, teams, people, cards, avatars, names, disabled, onPick, children, className, title, planning }: OwnerPickerProps) {
  const [open, setOpen] = useState(false);
  const [team, setTeam] = useState(current.team);
  const [typed, setTyped] = useState("");
  const [refused, setRefused] = useState<string | null>(null);
  const anchor = useRef<HTMLSpanElement | null>(null);

  const toggle = () => {
    if (!open) {
      setTeam(current.team);
      setTyped("");
      setRefused(null);
    }
    setOpen((o) => !o);
  };
  const press = (pick: Parameters<typeof ownerStep>[1]) => {
    const step = ownerStep(team, pick, { planning });
    setTeam(step.team);
    setRefused(step.refused ?? null);
    if (step.answer) {
      setOpen(false);
      setTyped("");
      onPick(step.answer);
    }
  };
  const ordered = team ? peopleOfTeam(people, team, cards) : people;

  return (
    <span className={`owner-picker${className ? ` ${className}` : ""}`} ref={anchor}>
      <button
        type="button"
        className="owner-picker-btn"
        disabled={disabled}
        aria-haspopup="menu"
        aria-expanded={open}
        title={title ?? "Whose is this: a team, and somebody on it"}
        onClick={(e) => {
          e.stopPropagation();
          toggle();
        }}
        onPointerDown={(e) => e.stopPropagation()}
      >
        {children}
      </button>
      <Dropdown open={open} anchorRef={anchor} onClose={() => setOpen(false)} className="card-stage-menu card-assign-menu owner-menu">
        <div className="card-assign-cols" onPointerDown={(e) => e.stopPropagation()}>
          <div className="card-assign-col">
            <div className="card-assign-head">Team</div>
            {pickerTeams(teams).map((t) => (
              <button
                key={`t-${t}`}
                type="button"
                className={`card-stage-item${team === t ? " card-stage-item-active" : ""}`}
                onClick={(e) => {
                  e.stopPropagation();
                  press({ kind: "team", team: t });
                }}
              >
                <span className="team-dot" style={{ background: teamColor(t) }} />
                {t}
              </button>
            ))}
            <button
              type="button"
              className="card-stage-item card-stage-clear"
              onClick={(e) => {
                e.stopPropagation();
                press({ kind: "no-owner" });
              }}
            >
              No owner
            </button>
          </div>
          <div className={`card-assign-col${team || planning ? "" : " owner-col-waiting"}`} aria-disabled={!team && !planning}>
            <div className="card-assign-head">{planning ? "Owner" : "Person"}{team ? ` · ${team}` : ""}</div>
            {!team && !planning && <span className="card-stage-item owner-col-hint">Pick a team first</span>}
            {(team || planning) && (
              <button
                type="button"
                className={`card-stage-item card-stage-clear${current.team === team && !current.person ? " card-stage-item-active" : ""}`}
                onClick={(e) => {
                  e.stopPropagation();
                  press({ kind: "no-person" });
                }}
              >
                No person
              </button>
            )}
            {(team || planning) &&
              ordered.map((p) => (
                <button
                  key={`p-${p}`}
                  type="button"
                  className={`card-stage-item${current.person === p && current.team === team ? " card-stage-item-active" : ""}`}
                  onClick={(e) => {
                    e.stopPropagation();
                    press({ kind: "person", person: p });
                  }}
                >
                  <Avatar login={p} avatars={avatars} names={names} />
                  {displayName(p, names)}
                </button>
              ))}
            {(team || planning) && (
              <input
                type="text"
                className="add-card-input card-assign-input"
                placeholder="login…"
                value={typed}
                aria-label="Person by login"
                onClick={(e) => e.stopPropagation()}
                onChange={(e) => setTyped(e.target.value)}
                onKeyDown={(e) => {
                  e.stopPropagation();
                  if (e.key === "Enter" && typed.trim()) {
                    press({ kind: "person", person: typed.trim() });
                  } else if (e.key === "Escape") {
                    setOpen(false);
                  }
                }}
              />
            )}
          </div>
        </div>
        {refused && <div className="owner-refused">{refused}</div>}
      </Dropdown>
    </span>
  );
}
