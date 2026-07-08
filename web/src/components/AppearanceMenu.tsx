import { useEffect, useRef, useState } from "react";
import { Dropdown } from "./Dropdown";
import {
  type Appearance,
  type Palette,
  type ThemeMode,
} from "../theme";

interface AppearanceMenuProps {
  /** GitHub login to show as the trigger, or null when not signed in. */
  login: string | null;
  appearance: Appearance;
  onChange: (next: Appearance) => void;
  /** Sign-out link target (OAuth mode); when set the menu offers it. */
  logoutUrl?: string | null;
}

// On-screen labels are intentionally plain "appearance" wording. The palette
// options carry the colour-vision support (see theme.ts) but nothing here names
// it, so picking one says nothing about the person picking it.
const THEME_OPTIONS: { value: ThemeMode; label: string }[] = [
  { value: "light", label: "Light" },
  { value: "dark", label: "Dark" },
  { value: "system", label: "System" },
];

const PALETTE_OPTIONS: { value: Palette; label: string }[] = [
  { value: "default", label: "Default" },
  { value: "vivid", label: "Vivid" },
  { value: "marine", label: "Marine" },
];

/**
 * AppearanceMenu turns the header nickname into the trigger for a small
 * appearance picker (theme mode + colour palette), rendered through the shared
 * Dropdown so it is never clipped by the header.
 */
export function AppearanceMenu({
  login,
  appearance,
  onChange,
  logoutUrl,
}: AppearanceMenuProps) {
  const [open, setOpen] = useState(false);
  const anchorRef = useRef<HTMLDivElement | null>(null);

  // Close on Escape while open. A document listener (rather than onKeyDown on
  // the menu) works regardless of where focus sits, since opening does not move
  // focus into the portalled menu.
  useEffect(() => {
    if (!open) {
      return;
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setOpen(false);
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open]);

  return (
    <div className="appearance" ref={anchorRef}>
      <button
        type="button"
        className={`login appearance-trigger${login ? "" : " login-anon"}`}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        {login ? `@${login}` : "not signed in"}
        <span className="appearance-caret" aria-hidden="true">
          ▾
        </span>
      </button>
      <Dropdown
        open={open}
        anchorRef={anchorRef}
        onClose={() => setOpen(false)}
        className="appearance-menu"
      >
        <div role="menu" aria-label="Appearance" className="appearance-menu-inner">
        <div className="appearance-group" role="group" aria-label="Theme">
          <div className="appearance-label">Theme</div>
          {THEME_OPTIONS.map((o) => (
            <button
              key={o.value}
              type="button"
              role="menuitemradio"
              aria-checked={appearance.mode === o.value}
              className="appearance-item"
              onClick={() => onChange({ ...appearance, mode: o.value })}
            >
              <span className="appearance-check" aria-hidden="true">
                {appearance.mode === o.value ? "✓" : ""}
              </span>
              {o.label}
            </button>
          ))}
        </div>
        <div className="appearance-group" role="group" aria-label="Colours">
          <div className="appearance-label">Colours</div>
          {PALETTE_OPTIONS.map((o) => (
            <button
              key={o.value}
              type="button"
              role="menuitemradio"
              aria-checked={appearance.palette === o.value}
              className="appearance-item"
              onClick={() => onChange({ ...appearance, palette: o.value })}
            >
              <span className="appearance-check" aria-hidden="true">
                {appearance.palette === o.value ? "✓" : ""}
              </span>
              {o.label}
            </button>
          ))}
        </div>
        {logoutUrl && (
          <div className="appearance-group" role="group" aria-label="Account">
            <div className="appearance-sep" aria-hidden="true" />
            <a role="menuitem" className="appearance-item" href={logoutUrl}>
              <span className="appearance-check" aria-hidden="true" />
              Sign out
            </a>
          </div>
        )}
        </div>
      </Dropdown>
    </div>
  );
}
