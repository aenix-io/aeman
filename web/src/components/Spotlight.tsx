import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";

import { highlight, searchCards } from "../search";
import { kindOf, KIND_LABEL } from "../strategy";
import { workStatus } from "../workstatus";
import { Avatar } from "./Avatar";
import { KindIcon } from "./KindIcon";
import type { Card } from "../providers/types";

interface SpotlightProps {
  /** Every card the visitor can read (the view=all listing); null while it
   *  is first being fetched. */
  cards: Card[] | null;
  /** The listing could not be fetched. */
  error: string | null;
  onPick: (c: Card) => void;
  onClose: () => void;
  avatars: Record<string, string>;
  names: Record<string, string>;
}

const STATUS_LABEL = { "in-progress": "In progress", backlog: "Backlog", done: "Done" } as const;

/** Spotlight is the ⌘K search: a launcher over every card on the board. It
 *  opens over whatever board is on screen, ranks as you type (search.ts), and
 *  Enter — or a click — opens the card's drawer. ↑/↓ move, Esc closes. */
export function Spotlight({ cards, error, onPick, onClose, avatars, names }: SpotlightProps) {
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const input = useRef<HTMLInputElement>(null);
  const list = useRef<HTMLUListElement>(null);
  const hits = useMemo(() => searchCards(cards ?? [], query), [cards, query]);

  useEffect(() => {
    input.current?.focus();
  }, []);
  useEffect(() => {
    setActive(0);
  }, [query]);
  useLayoutEffect(() => {
    list.current?.children[active]?.scrollIntoView({ block: "nearest" });
  }, [active, hits]);

  const onKey = (e: React.KeyboardEvent) => {
    if (e.nativeEvent.isComposing) {
      return;
    }
    if (e.key === "Escape") {
      e.preventDefault();
      onClose();
    } else if (e.key === "ArrowDown" || (e.key === "n" && e.ctrlKey)) {
      e.preventDefault();
      setActive((a) => (hits.length ? (a + 1) % hits.length : 0));
    } else if (e.key === "ArrowUp" || (e.key === "p" && e.ctrlKey)) {
      e.preventDefault();
      setActive((a) => (hits.length ? (a - 1 + hits.length) % hits.length : 0));
    } else if (e.key === "Enter" && hits[active]) {
      e.preventDefault();
      onPick(hits[active]);
    }
  };

  let empty: string | null = null;
  if (error) {
    empty = error;
  } else if (!cards) {
    empty = "Loading cards…";
  } else if (!query.trim()) {
    empty = `Search ${cards.length} card${cards.length === 1 ? "" : "s"} by title or by the plan they stand under`;
  } else if (!hits.length) {
    empty = "No cards match";
  }

  return (
    <div className="spotlight-backdrop" onMouseDown={onClose}>
      <div
        className="spotlight"
        role="dialog"
        aria-label="Search cards"
        onMouseDown={(e) => e.stopPropagation()}
        onKeyDown={onKey}
      >
        <div className="spotlight-field">
          <svg className="spotlight-glass" viewBox="0 0 24 24" width="22" height="22" aria-hidden="true">
            <circle cx="10.5" cy="10.5" r="6.5" fill="none" stroke="currentColor" strokeWidth="2" />
            <path d="m15.5 15.5 5 5" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
          </svg>
          <input
            ref={input}
            className="spotlight-input"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search cards"
            spellCheck={false}
            autoComplete="off"
            role="combobox"
            aria-expanded={hits.length > 0}
            aria-controls="spotlight-results"
            aria-activedescendant={hits[active] ? `spotlight-${hits[active].itemId}` : undefined}
          />
          <kbd className="spotlight-esc">esc</kbd>
        </div>
        {empty ? (
          <div className="spotlight-empty">{empty}</div>
        ) : (
          <ul className="spotlight-results" id="spotlight-results" role="listbox" ref={list}>
            {hits.map((c, i) => {
              const kind = kindOf(c);
              const status = kind === "task" || kind === "subtask" ? workStatus(c) : null;
              const plan = (c.lineage ?? []).map((l) => l.title).join(" › ");
              const team = c.effectiveTeam || c.team;
              const who = c.assignees[0] ?? c.owner;
              return (
                <li
                  key={c.itemId}
                  id={`spotlight-${c.itemId}`}
                  role="option"
                  aria-selected={i === active}
                  className={`spotlight-row${i === active ? " spotlight-row-active" : ""}${status === "done" ? " spotlight-row-done" : ""}`}
                  onMouseMove={() => i !== active && setActive(i)}
                  onClick={() => onPick(c)}
                >
                  <KindIcon kind={kind} size={18} className="spotlight-kind" />
                  <div className="spotlight-text">
                    <div className="spotlight-title">
                      {highlight(c.title, query).map((r, k) =>
                        r.hit ? <mark key={k}>{r.text}</mark> : <span key={k}>{r.text}</span>,
                      )}
                    </div>
                    <div className="spotlight-meta">
                      {status ? null : <span>{KIND_LABEL[kind]}</span>}
                      {plan && <span className="spotlight-plan">{plan}</span>}
                      {team && <span>{team}</span>}
                    </div>
                  </div>
                  {status && <span className={`spotlight-status spotlight-status-${status}`}>{STATUS_LABEL[status]}</span>}
                  {who && <Avatar login={who} avatars={avatars} names={names} className="spotlight-avatar" />}
                </li>
              );
            })}
          </ul>
        )}
        <div className="spotlight-foot">
          <span><kbd>↑</kbd><kbd>↓</kbd> to move</span>
          <span><kbd>↵</kbd> to open</span>
        </div>
      </div>
    </div>
  );
}
