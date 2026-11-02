import { useEffect, useMemo, useRef, useState } from "react";
import type { Card as CardModel, LineageRef } from "../providers/types";
import { KIND_LABEL, allowedParentKinds, kindOf, type RoadmapKind } from "../roadmap";
import { Dropdown } from "./Dropdown";
import { KindIcon } from "./KindIcon";

interface ParentPickerProps {
  /** The kind of the card being filed: it decides which levels are offered
   *  (roadmap.allowedParentKinds, H2). */
  kind: RoadmapKind;
  /** The parent as the card shows it now — the last crumb of its lineage. */
  current?: LineageRef;
  /** Every card the picker may choose from (the roadmap listing); null while
   *  it is still being fetched. The picker filters by level itself and never
   *  offers the card its own descendants. */
  options: CardModel[] | null;
  /** The card being filed, so it is never offered itself or a card under it. */
  self: string;
  disabled?: boolean;
  /** The picker opened: the caller may fetch the options now. */
  onOpen?: () => void;
  onPick: (parent: CardModel | null) => void;
}

const ORDER: RoadmapKind[] = ["goal", "initiative", "epic"];

/** ParentPicker is the one control that files a card under a goal, an
 *  initiative or an epic — "Under", in the drawer's sidebar. One choice, or
 *  none: a card sits under at most one parent (H2). It searches by title and
 *  groups what it offers by level, highest first, so a long roadmap stays
 *  readable; the server holds the rule and says so in its own words when it
 *  refuses, which the caller shows. */
export function ParentPicker({ kind, current, options, self, disabled, onOpen, onPick }: ParentPickerProps) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const anchor = useRef<HTMLSpanElement | null>(null);
  const input = useRef<HTMLInputElement | null>(null);
  const allowed = allowedParentKinds(kind);

  useEffect(() => {
    if (open) {
      onOpen?.();
      setQuery("");
      // The list is portalled; focus lands once it is in the document.
      const t = window.setTimeout(() => input.current?.focus(), 0);
      return () => window.clearTimeout(t);
    }
    return undefined;
  }, [open, onOpen]);

  const groups = useMemo(() => {
    if (!options) {
      return null;
    }
    const q = query.trim().toLowerCase();
    const offered = options.filter((c) => {
      const k = kindOf(c);
      if (!allowed.includes(k) || c.itemId === self) {
        return false;
      }
      if ((c.lineage ?? []).some((up) => up.uid === self)) {
        return false; // a card under this one cannot also be above it
      }
      return !q || c.title.toLowerCase().includes(q);
    });
    return ORDER.filter((k) => allowed.includes(k))
      .map((k) => ({ kind: k, cards: offered.filter((c) => kindOf(c) === k) }))
      .filter((g) => g.cards.length > 0);
  }, [options, query, allowed, self]);

  const pick = (c: CardModel | null) => {
    setOpen(false);
    onPick(c);
  };

  return (
    <span className="drawer-picker" ref={anchor}>
      <button
        type="button"
        className={`drawer-pick${current ? "" : " drawer-pick-empty"}`}
        disabled={disabled}
        aria-haspopup="listbox"
        aria-expanded={open}
        title="The goal, initiative or epic this card is filed under"
        onClick={() => setOpen((o) => !o)}
      >
        {current ? (
          <>
            <KindIcon kind={(current.type ?? "task") as RoadmapKind} />
            <span className="drawer-pick-text">{current.title}</span>
          </>
        ) : (
          <span className="drawer-pick-text">None</span>
        )}
        {!disabled && <span className="add-card-team-caret">▾</span>}
      </button>
      <Dropdown open={open} anchorRef={anchor} onClose={() => setOpen(false)} className="add-card-team-menu drawer-pick-menu">
        <input
          ref={input}
          type="text"
          className="drawer-pick-search"
          placeholder="Search the roadmap…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              setOpen(false);
            }
          }}
          aria-label="Search"
        />
        <button type="button" className="add-card-team-item drawer-pick-none" onClick={() => pick(null)}>
          None
        </button>
        {groups === null && <span className="add-card-team-item modal-parent-loading">Loading…</span>}
        {groups?.map((g) => (
          <div key={g.kind} className="drawer-pick-group" role="group" aria-label={KIND_LABEL[g.kind]}>
            <div className="drawer-pick-group-label">{KIND_LABEL[g.kind]}s</div>
            {g.cards.map((c) => (
              <button
                key={c.itemId}
                type="button"
                className={`add-card-team-item${current?.uid === c.itemId ? " drawer-pick-on" : ""}`}
                onClick={() => pick(c)}
              >
                <KindIcon kind={g.kind} />
                <span className="drawer-pick-text">{c.title}</span>
              </button>
            ))}
          </div>
        ))}
        {groups && groups.length === 0 && (
          <span className="add-card-team-item modal-parent-loading">
            {query ? "Nothing on the roadmap matches" : "Nothing of a higher level on the roadmap yet"}
          </span>
        )}
      </Dropdown>
    </span>
  );
}
