import { useEffect, useRef, useState } from "react";
import { teamColor, teamInitial } from "../avatar";
import { Dropdown } from "./Dropdown";

/** One option of the aside picker: the dot that stands for it, what it is
 *  called in the list, and — where a name is not enough — what it means. */
export interface AddCardOption {
  key: string;
  label: string;
  color?: string;
  hint?: string;
}

interface AddCardProps {
  /** Nothing can be added here: the board is showing a day that ended,
   *  and a card created on it would land on TODAY's board instead. */
  hidden?: boolean;
  onCreate: (title: string, team?: string | null, picked?: string) => void;
  placeholder?: string;
  /** Roster of known teams to offer in the picker. */
  teams?: string[];
  /** When set, skip the picker and always create with this team. */
  forcedTeam?: string | null;
  /** Offer the "no team" option; when false, the picker defaults to the first
   *  team instead of "no team". */
  allowNoTeam?: boolean;
  /** Start with the form already open (input focused). */
  autoOpen?: boolean;
  /** The form closed; created reports whether a card was submitted. */
  onClosed?: (created: boolean) => void;
  /** An aside picker of something OTHER than a team, for a board where the
   *  team is not the question: the Triage board picks a zone, because a
   *  column there is a person and the team comes with them. Its answer
   *  arrives as the third argument of onCreate. */
  picker?: { title: string; options: AddCardOption[]; initial: string };
  /** Draw the pickers as their colour and nothing else. A form that stands
   *  inside a card's width has no room for a word, and the menus name what
   *  the dots mean. */
  compact?: boolean;
}

/** AddCard expands into a title input with an integrated team picker. */
export function AddCard({
  hidden,
  onCreate,
  placeholder = "Add a card…",
  teams,
  forcedTeam,
  allowNoTeam = true,
  autoOpen,
  onClosed,
  picker,
  compact,
}: AddCardProps) {
  // With "no team" disabled the picker starts on the first team, so a filtered
  // create lands on a real team by default instead of "no team".
  const defaultTeam =
    allowNoTeam || !teams || teams.length === 0 ? null : teams[0];
  const [open, setOpen] = useState(autoOpen ?? false);
  const [value, setValue] = useState("");
  const [team, setTeam] = useState<string | null>(defaultTeam);
  const [menuOpen, setMenuOpen] = useState(false);
  const [pickOpen, setPickOpen] = useState(false);
  const [picked, setPicked] = useState(picker?.initial ?? "");
  const formRef = useRef<HTMLDivElement | null>(null);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const teamRef = useRef<HTMLDivElement | null>(null);
  const pickRef = useRef<HTMLDivElement | null>(null);

  // The team picker is shown when a roster is supplied and no team is
  // forced. A board may ask for one of its own beside it: the Triage board
  // picks a zone, and picks a team too when it is showing more than one.
  const showPicker = forcedTeam === undefined && teams !== undefined;

  const close = (created: boolean) => {
    setOpen(false);
    setValue("");
    setTeam(defaultTeam);
    setPicked(picker?.initial ?? "");
    setMenuOpen(false);
    setPickOpen(false);
    onClosed?.(created);
  };

  const submit = () => {
    const title = value.trim();
    if (title) {
      onCreate(title, forcedTeam !== undefined ? forcedTeam : team, picked);
    }
    close(Boolean(title));
  };

  // A live ref so the outside-click handler always sees the latest input.
  const submitRef = useRef(submit);
  submitRef.current = submit;

  // A press outside the form closes it, saving the card when it has a title
  // rather than discarding what was typed.
  //
  // POINTERDOWN, not mousedown, and captured on the way DOWN. A board that
  // drags with pointer events calls preventDefault on its own pointerdown —
  // which suppresses the compatibility mouse events entirely, so a press on
  // a card, or anywhere else that drags, never reached a mousedown listener
  // and the form stayed open behind whatever was clicked. Capture, because
  // those handlers also stop the event from bubbling.
  //
  // The press that OPENED the form is already past: the form opens on click,
  // which fires after its own pointerdown, and this listener is added only
  // once open is true.
  useEffect(() => {
    if (!open) {
      return;
    }
    const onDocDown = (e: PointerEvent) => {
      const t = e.target as Element;
      // The team menu is portalled to <body> (so overflow ancestors cannot
      // clip it) — a press inside it is part of the form, not an outside save.
      if (t.closest?.(".add-card-team-menu")) {
        return;
      }
      if (formRef.current && !formRef.current.contains(t)) {
        submitRef.current();
      }
    };
    document.addEventListener("pointerdown", onDocDown, true);
    return () => document.removeEventListener("pointerdown", onDocDown, true);
  }, [open]);

  const chosen = picker?.options.find((o) => o.key === picked);

  // Picking a team keeps focus in the title input so Enter still submits.
  const pickTeam = (t: string | null) => {
    setTeam(t);
    setMenuOpen(false);
    inputRef.current?.focus();
  };

  // Nothing can be added to a day that ENDED: the box would only produce
  // an error (the provider refuses the create), so it is not offered —
  // the collapsed "+" included. The check lives here, after the hooks:
  // above them it would change their order the moment the board flips.
  if (hidden) {
    return null;
  }
  if (!open) {
    return (
      <button
        type="button"
        className="add-card"
        onClick={() => {
          setTeam(defaultTeam);
          setOpen(true);
        }}
      >
        + add
      </button>
    );
  }

  return (
    <div className="add-card-form" ref={formRef}>
      <input
        type="text"
        className="add-card-input"
        autoFocus
        ref={inputRef}
        value={value}
        placeholder={placeholder}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            submit();
          } else if (e.key === "Escape") {
            close(false);
          }
        }}
      />
      {picker && (
        <div className="add-card-team" ref={pickRef}>
          <button
            type="button"
            className="add-card-team-btn"
            onClick={() => setPickOpen((o) => !o)}
            title={chosen ? `${picker.title}: ${chosen.label}` : picker.title}
          >
            {/* The colour is the answer — the list is where it is spelled
                out. A word here would be the widest thing in a form that
                stands inside a card's width. */}
            {chosen?.color && (
              <span className="team-dot" style={{ background: chosen.color }} />
            )}
            <span className="add-card-team-caret">▾</span>
          </button>
          <Dropdown
            open={pickOpen}
            anchorRef={pickRef}
            onClose={() => setPickOpen(false)}
            className="add-card-team-menu"
          >
            {picker.options.map((o) => (
              <button
                key={o.key}
                type="button"
                className="add-card-team-item"
                title={o.hint}
                onClick={() => {
                  setPicked(o.key);
                  setPickOpen(false);
                  inputRef.current?.focus();
                }}
              >
                {o.color && <span className="team-dot" style={{ background: o.color }} />}
                {o.label}
              </button>
            ))}
          </Dropdown>
        </div>
      )}
      {showPicker && (
        <div className="add-card-team" ref={teamRef}>
          <button
            type="button"
            className="add-card-team-btn"
            onClick={() => setMenuOpen((o) => !o)}
            title={team ? `Team: ${team}` : "Team"}
          >
            {team && (
              // Compact, the dot is the whole answer — so it carries the
              // team's letter, which is what tells two colours apart at nine
              // pixels.
              <span
                className={`team-dot${compact ? " team-dot-lettered" : ""}`}
                style={{ background: teamColor(team) }}
              >
                {compact ? teamInitial(team) : null}
              </span>
            )}
            {!compact && <span className="add-card-team-label">{team ?? "no team"}</span>}
            <span className="add-card-team-caret">▾</span>
          </button>
          <Dropdown
            open={menuOpen}
            anchorRef={teamRef}
            onClose={() => setMenuOpen(false)}
            className="add-card-team-menu"
          >
            {allowNoTeam && (
              <button
                type="button"
                className="add-card-team-item"
                onClick={() => pickTeam(null)}
              >
                no team
              </button>
            )}
            {(teams ?? []).map((t) => (
              <button
                key={t}
                type="button"
                className="add-card-team-item"
                onClick={() => pickTeam(t)}
              >
                <span className="team-dot" style={{ background: teamColor(t) }} />
                {t}
              </button>
            ))}
          </Dropdown>
        </div>
      )}
    </div>
  );
}
