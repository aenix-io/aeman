import type { ReactNode } from "react";

/** ViewToggle is the app's view toggle: an icon button in a toolbar that is
 *  either pressed or not — the Me board's eye and focus buttons, the
 *  Roadmap's Project cards. One component, so every such toggle reads and
 *  behaves the same, pressed and unpressed. */
export function ViewToggle({
  on,
  onToggle,
  title,
  children,
}: {
  on: boolean;
  onToggle: () => void;
  title: string;
  children: ReactNode;
}) {
  return (
    <button type="button" className={`team-eye${on ? " team-eye-on" : ""}`} onClick={onToggle} aria-pressed={on} title={title}>
      {children}
    </button>
  );
}
