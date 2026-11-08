import { useEffect, useMemo, useRef, useState } from "react";
import type { Card as CardModel, LineageRef } from "../providers/types";
import { blocksOf, dependencyOptions, dependencyRows } from "../dependencies";
import { kindOf, type RoadmapKind } from "../roadmap";
import { Dropdown } from "./Dropdown";
import { KindIcon } from "./KindIcon";
import { ParentChain } from "./ParentPicker";
import { StatusPill } from "./PlanTree";

interface DependsOnProps {
  card: CardModel;
  /** Every card the drawer knows: the board's and the roadmap listing's. */
  known: CardModel[];
  /** The candidates for a new dependency (the roadmap listing); null while
   *  it is still being fetched. */
  options: CardModel[] | null;
  readOnly?: boolean;
  /** The picker opened: the caller may fetch the options now. */
  onWantOptions: () => void;
  onAdd: (target: CardModel) => void;
  onRemove: (uid: string) => void;
  onOpen: (uid: string) => void;
}

const kindOfRef = (r: LineageRef & { card?: CardModel }): RoadmapKind =>
  r.card ? kindOf(r.card) : ((r.type ?? "task") as RoadmapKind);

/** DependsOn is the drawer's "Depends on" section (docs/design/
 *  dependencies.md) — what this card waits for, each with its status, a
 *  search to add one and × to take one off — and below it the read-only
 *  "Blocks": the cards that wait for this one. The lock it derives is the
 *  server's; this draws the list and the picker. */
export function DependsOn({ card, known, options, readOnly, onWantOptions, onAdd, onRemove, onOpen }: DependsOnProps) {
  const rows = dependencyRows(card, known);
  const blocks = blocksOf(card, known);
  const [open, setOpen] = useState(false);
  const anchor = useRef<HTMLSpanElement | null>(null);
  useEffect(() => {
    if (open) {
      onWantOptions();
    }
  }, [open, onWantOptions]);
  return (
    <>
      <section className="drawer-children drawer-depends" aria-label="Depends on">
        <div className="drawer-section-title">
          Depends on
          {rows.length > 0 && <span className="modal-level-count"> {rows.length}</span>}
        </div>
        {rows.length === 0 && <p className="drawer-hint">Waits for nothing.</p>}
        {rows.map((r) => (
          <div key={r.uid} className="drawer-child">
            <button type="button" className="drawer-child-open" onClick={() => onOpen(r.uid)}>
              <KindIcon kind={kindOfRef(r)} />
              <span className="drawer-child-title">{r.title}</span>
            </button>
            {r.card && <StatusPill card={r.card} />}
            {!readOnly && (
              <button
                type="button"
                className="drawer-child-x"
                aria-label={`Stop waiting for «${r.title}»`}
                title="Stop waiting for it"
                onClick={() => onRemove(r.uid)}
              >
                ×
              </button>
            )}
          </div>
        ))}
        {!readOnly && (
          <span className="drawer-picker" ref={anchor}>
            <button type="button" className="drawer-add-open" aria-haspopup="listbox" aria-expanded={open} onClick={() => setOpen((o) => !o)}>
              + Add dependency
            </button>
            <Dropdown open={open} anchorRef={anchor} onClose={() => setOpen(false)} className="add-card-team-menu drawer-pick-menu">
              {open && (
                <DependencyOptionList
                  card={card}
                  options={options}
                  onPick={(c) => {
                    setOpen(false);
                    onAdd(c);
                  }}
                />
              )}
            </Dropdown>
          </span>
        )}
      </section>
      {blocks.length > 0 && (
        <section className="drawer-children drawer-blocks" aria-label="Blocks">
          <div className="drawer-section-title">
            Blocks<span className="modal-level-count"> {blocks.length}</span>
          </div>
          {blocks.map((b) => (
            <div key={b.uid} className="drawer-child">
              <button type="button" className="drawer-child-open" onClick={() => onOpen(b.uid)}>
                <KindIcon kind={(b.type ?? "task") as RoadmapKind} />
                <span className="drawer-child-title">{b.title}</span>
              </button>
            </div>
          ))}
        </section>
      )}
    </>
  );
}

/** DependencyOptionList is the picker's body: a search over the roadmap and
 *  every card this one may wait for (dependencies.dependencyOptions), each
 *  with its chain. */
export function DependencyOptionList({
  card,
  options,
  onPick,
}: {
  card: CardModel;
  options: CardModel[] | null;
  onPick: (c: CardModel) => void;
}) {
  const [query, setQuery] = useState("");
  const input = useRef<HTMLInputElement | null>(null);
  useEffect(() => {
    const t = window.setTimeout(() => input.current?.focus(), 0);
    return () => window.clearTimeout(t);
  }, []);
  const offered = useMemo(() => (options ? dependencyOptions(options, card, query) : null), [options, card, query]);
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
      {offered === null && <span className="add-card-team-item modal-parent-loading">Loading…</span>}
      {offered?.map(({ card: c, chain }) => (
        <button
          key={c.itemId}
          type="button"
          className="add-card-team-item"
          aria-label={chain.length ? `${c.title}, under ${chain.map((x) => x.title).join(" › ")}` : c.title}
          onClick={() => onPick(c)}
        >
          <KindIcon kind={kindOf(c)} />
          <span className="drawer-pick-option">
            <ParentChain chain={chain} />
            <span className="drawer-pick-text">{c.title}</span>
          </span>
        </button>
      ))}
      {offered && offered.length === 0 && (
        <span className="add-card-team-item modal-parent-loading">{query ? "Nothing on the roadmap matches" : "Nothing to wait for"}</span>
      )}
    </>
  );
}
