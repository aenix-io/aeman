import { useEffect, useMemo, useRef, useState } from "react";
import type { Card as CardModel, LineageRef } from "../providers/types";
import { KIND_LABEL, allowedParentKinds, kindOf, type RoadmapKind } from "../roadmap";
import { Dropdown } from "./Dropdown";
import { KindIcon } from "./KindIcon";

const ORDER: RoadmapKind[] = ["goal", "initiative", "epic"];

interface ParentOptionListProps {
  /** The kind of the card being filed: it decides which levels are offered
   *  (roadmap.allowedParentKinds, H2). */
  kind: RoadmapKind;
  /** The card's parent now, marked in the list. */
  current?: string;
  /** Every card the list may choose from (the roadmap listing); null while
   *  it is still being fetched. The list filters by level itself and never
   *  offers the card its own descendants. */
  options: CardModel[] | null;
  /** The card being filed, so it is never offered itself or a card under it. */
  self: string;
  onPick: (parent: CardModel | null) => void;
}

/** ParentOptionList is the body of every "file it under" menu: a search
 *  box, "None", and the goals, initiatives and epics the card may sit under,
 *  grouped by level, highest first. The drawer's Under picker and the
 *  Project board's "Attach to…" draw the same list. */
export function ParentOptionList({ kind, current, options, self, onPick }: ParentOptionListProps) {
  const [query, setQuery] = useState("");
  const input = useRef<HTMLInputElement | null>(null);
  const allowed = allowedParentKinds(kind);
  useEffect(() => {
    // The list is portalled; focus lands once it is in the document.
    const t = window.setTimeout(() => input.current?.focus(), 0);
    return () => window.clearTimeout(t);
  }, []);

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

  return (
    <>
      <input
        ref={input}
        type="text"
        className="drawer-pick-search"
        placeholder="Search the roadmap…"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        aria-label="Search"
      />
      <button type="button" className="add-card-team-item drawer-pick-none" onClick={() => onPick(null)}>
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
              className={`add-card-team-item${current === c.itemId ? " drawer-pick-on" : ""}`}
              onClick={() => onPick(c)}
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
    </>
  );
}

interface ParentPickerProps {
  kind: RoadmapKind;
  /** The parent as the card shows it now — the last crumb of its lineage. */
  current?: LineageRef;
  options: CardModel[] | null;
  self: string;
  disabled?: boolean;
  /** The picker opened: the caller may fetch the options now. */
  onOpen?: () => void;
  onPick: (parent: CardModel | null) => void;
}

/** ParentPicker is the one control that files a card under a goal, an
 *  initiative or an epic — "Under", in the drawer's sidebar. One choice, or
 *  none: a card sits under at most one parent (H2). The server holds the
 *  rule and says so in its own words when it refuses, which the caller
 *  shows. */
export function ParentPicker({ kind, current, options, self, disabled, onOpen, onPick }: ParentPickerProps) {
  const [open, setOpen] = useState(false);
  const anchor = useRef<HTMLSpanElement | null>(null);

  useEffect(() => {
    if (open) {
      onOpen?.();
    }
  }, [open, onOpen]);

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
        {open && <ParentOptionList kind={kind} current={current?.uid} options={options} self={self} onPick={pick} />}
      </Dropdown>
    </span>
  );
}
