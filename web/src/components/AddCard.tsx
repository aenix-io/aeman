import { useEffect, useRef, useState } from "react";
import { teamColor } from "../avatar";
import { Dropdown } from "./Dropdown";

interface AddCardProps {
  /** Nothing can be added here: the board is showing a day that ended,
   *  and a card created on it would land on TODAY's board instead. */
  hidden?: boolean;
  onCreate: (title: string, team?: string | null) => void;
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
}: AddCardProps) {
  // With "no team" disabled the picker starts on the first team, so a filtered
  // create lands on a real team by default instead of "no team".
  const defaultTeam =
    allowNoTeam || !teams || teams.length === 0 ? null : teams[0];
  const [open, setOpen] = useState(autoOpen ?? false);
  const [value, setValue] = useState("");
  const [team, setTeam] = useState<string | null>(defaultTeam);
  const [menuOpen, setMenuOpen] = useState(false);
  const formRef = useRef<HTMLDivElement | null>(null);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const teamRef = useRef<HTMLDivElement | null>(null);

  // The team picker is shown only when a roster is supplied and no team is forced.
  const showPicker = forcedTeam === undefined && teams !== undefined;

  const close = (created: boolean) => {
    setOpen(false);
    setValue("");
    setTeam(defaultTeam);
    setMenuOpen(false);
    onClosed?.(created);
  };

  const submit = () => {
    const title = value.trim();
    if (title) {
      onCreate(title, forcedTeam !== undefined ? forcedTeam : team);
    }
    close(Boolean(title));
  };

  // A live ref so the outside-click handler always sees the latest input.
  const submitRef = useRef(submit);
  submitRef.current = submit;

  // A click outside the form saves the card (if it has a title) rather than
  // discarding it.
  useEffect(() => {
    if (!open) {
      return;
    }
    const onDocDown = (e: MouseEvent) => {
      const t = e.target as Element;
      // The team menu is portalled to <body> (so overflow ancestors cannot
      // clip it) — a click inside it is part of the form, not an outside save.
      if (t.closest?.(".add-card-team-menu")) {
        return;
      }
      if (formRef.current && !formRef.current.contains(t)) {
        submitRef.current();
      }
    };
    document.addEventListener("mousedown", onDocDown);
    return () => document.removeEventListener("mousedown", onDocDown);
  }, [open]);

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
      {showPicker && (
        <div className="add-card-team" ref={teamRef}>
          <button
            type="button"
            className="add-card-team-btn"
            onClick={() => setMenuOpen((o) => !o)}
            title="Team"
          >
            {team && (
              <span className="team-dot" style={{ background: teamColor(team) }} />
            )}
            <span className="add-card-team-label">{team ?? "no team"}</span>
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
