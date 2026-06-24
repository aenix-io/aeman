import { useEffect, useRef, useState } from "react";
import { teamColor } from "../avatar";

interface AddCardProps {
  onCreate: (title: string, team?: string | null) => void;
  placeholder?: string;
  /** Roster of known teams to offer in the picker. */
  teams?: string[];
  /** When set, skip the picker and always create with this team. */
  forcedTeam?: string | null;
}

/** AddCard expands into a title input with an integrated team picker. */
export function AddCard({
  onCreate,
  placeholder = "Add a card…",
  teams,
  forcedTeam,
}: AddCardProps) {
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState("");
  const [team, setTeam] = useState<string | null>(null);
  const [menuOpen, setMenuOpen] = useState(false);
  const formRef = useRef<HTMLDivElement | null>(null);

  // The team picker is shown only when a roster is supplied and no team is forced.
  const showPicker = forcedTeam === undefined && teams !== undefined;

  const close = () => {
    setOpen(false);
    setValue("");
    setTeam(null);
    setMenuOpen(false);
  };

  // Close (cancel) the whole form when a click lands outside it.
  useEffect(() => {
    if (!open) {
      return;
    }
    const onDocDown = (e: MouseEvent) => {
      if (formRef.current && !formRef.current.contains(e.target as Node)) {
        close();
      }
    };
    document.addEventListener("mousedown", onDocDown);
    return () => document.removeEventListener("mousedown", onDocDown);
  }, [open]);

  const submit = () => {
    const title = value.trim();
    if (title) {
      onCreate(title, forcedTeam !== undefined ? forcedTeam : team);
    }
    close();
  };

  if (!open) {
    return (
      <button type="button" className="add-card" onClick={() => setOpen(true)}>
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
        value={value}
        placeholder={placeholder}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            submit();
          } else if (e.key === "Escape") {
            close();
          }
        }}
      />
      {showPicker && (
        <div className="add-card-team">
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
          {menuOpen && (
            <div className="add-card-team-menu">
              <button
                type="button"
                className="add-card-team-item"
                onClick={() => {
                  setTeam(null);
                  setMenuOpen(false);
                }}
              >
                no team
              </button>
              {(teams ?? []).map((t) => (
                <button
                  key={t}
                  type="button"
                  className="add-card-team-item"
                  onClick={() => {
                    setTeam(t);
                    setMenuOpen(false);
                  }}
                >
                  <span className="team-dot" style={{ background: teamColor(t) }} />
                  {t}
                </button>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
