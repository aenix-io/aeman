import { useState } from "react";
import type { CustomerChip } from "../ownership";
import type { CustomerRef } from "../providers/types";

/** CustomerField is a card's Customers row (docs/design/ownership.md): the
 *  customers it inherits from its ancestors, grey and saying which ancestor
 *  names them — not removable here, they are that card's — and its own, each
 *  with a ×. The input below adds one: a name the roster has is that
 *  customer; a name it lacks is added to the roster first (onCreate). */
export function CustomerField({
  chips,
  roster,
  disabled,
  onAdd,
  onRemove,
  onCreate,
}: {
  chips: readonly CustomerChip[];
  roster: readonly CustomerRef[];
  disabled?: boolean;
  onAdd: (id: string) => void;
  onRemove: (id: string) => void;
  onCreate: (name: string) => void;
}) {
  const [typed, setTyped] = useState("");
  const nameOf = (id: string) => roster.find((c) => c.id === id)?.name ?? id;
  const shown = new Set(chips.map((c) => c.id));
  const offered = roster.filter((c) => !shown.has(c.id));
  const listId = "customer-roster";
  const submit = () => {
    const name = typed.trim();
    if (!name) {
      return;
    }
    const known = roster.find((c) => c.name.toLowerCase() === name.toLowerCase());
    if (known) {
      if (!shown.has(known.id)) {
        onAdd(known.id);
      }
    } else {
      onCreate(name);
    }
    setTyped("");
  };
  return (
    <span className="customer-field">
      {chips.map((c) =>
        c.from !== undefined ? (
          <span key={c.id} className="customer-chip customer-chip-inherited" title={c.from ? `From «${c.from}»` : "Inherited"}>
            {nameOf(c.id)}
          </span>
        ) : (
          <span key={c.id} className="customer-chip">
            {nameOf(c.id)}
            {!disabled && (
              <button
                type="button"
                className="customer-chip-remove"
                aria-label={`Remove ${nameOf(c.id)}`}
                title={`Remove ${nameOf(c.id)}`}
                onClick={() => onRemove(c.id)}
              >
                ×
              </button>
            )}
          </span>
        ),
      )}
      {!disabled && (
        <>
          <input
            type="text"
            className="add-card-input customer-input"
            placeholder="Add a customer…"
            aria-label="Add a customer"
            list={listId}
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                submit();
              } else if (e.key === "Escape") {
                setTyped("");
              }
            }}
          />
          <datalist id={listId}>
            {offered.map((c) => (
              <option key={c.id} value={c.name} />
            ))}
          </datalist>
        </>
      )}
    </span>
  );
}
