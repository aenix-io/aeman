// The Backlog's EPICS mode (docs/design/ordering.md, "Backlog Epics mode"):
// one flat list of the epics in the shown teams' scope, in the EPIC ORDER,
// empty ones included — each with the goal and initiative above it, how many
// of its tasks wait in the shown backlogs, and Split when they are scattered.
// Dragging an epic is the epic move, the same as on the Strategy board: its
// new place in the epic order, its parked tasks gathered into one block in
// every team's backlog. The list and what a drop means are backlogboard.ts's
// (epicRows, epicDrop); switching to it writes nothing.
import { useState, type DragEvent } from "react";
import { epicDrop, type EpicRow } from "../backlogboard";
import { KindIcon } from "./KindIcon";
import { ParentChain } from "./BacklogBoard";

/** placeAt is the gap a pointer at clientY names among the drawn rows. */
function placeAt(list: HTMLElement | null, clientY: number): number {
  const rows = list ? Array.from(list.querySelectorAll<HTMLElement>("[data-epic-row]")) : [];
  for (let i = 0; i < rows.length; i++) {
    const r = rows[i].getBoundingClientRect();
    if (clientY < r.top + r.height / 2) {
      return i;
    }
  }
  return rows.length;
}

export function BacklogEpics({
  rows,
  loading,
  onMove,
  onOpen,
  onOpenParent,
}: {
  rows: readonly EpicRow[];
  loading: boolean;
  /** The epic move: right after or right before another epic. */
  onMove: (epicId: string, anchor: { after: string } | { before: string }, order: string[]) => void;
  onOpen: (epicId: string) => void;
  onOpenParent: (uid: string) => void;
}) {
  const [carried, setCarried] = useState<string | null>(null);
  const [over, setOver] = useState<number | null>(null);
  const ids = rows.map((r) => r.epic.itemId);
  const listEl = (e: DragEvent<HTMLElement>) => e.currentTarget.closest<HTMLElement>("[data-epic-list]");
  const drop = (e: DragEvent<HTMLElement>) => {
    e.preventDefault();
    const id = carried;
    setCarried(null);
    setOver(null);
    if (!id) {
      return;
    }
    const at = placeAt(listEl(e), e.clientY);
    const rest = ids.filter((x) => x !== id);
    // The gap among the rows as drawn, the carried one included: count it
    // out when it stands above the gap.
    const from = ids.indexOf(id);
    const plan = epicDrop(ids, id, from >= 0 && from < at ? at - 1 : at);
    if (plan && plan.anchor && rest.length > 0) {
      onMove(id, plan.anchor, plan.order);
    }
  };
  if (loading && rows.length === 0) {
    return <p className="backlog-list-empty">Loading the epics…</p>;
  }
  if (rows.length === 0) {
    return <p className="backlog-list-empty">No epics for these teams</p>;
  }
  return (
    <section className="backlog-list backlog-epics" aria-label="Epics">
      <div
        className={`backlog-list-body${carried ? " backlog-list-over" : ""}`}
        data-epic-list=""
        onDragOver={(e) => {
          e.preventDefault();
          setOver(placeAt(listEl(e), e.clientY));
        }}
        onDrop={drop}
      >
        {rows.map((r, i) => (
          <div
            key={r.epic.itemId}
            data-epic-row=""
            className={`card backlog-card backlog-epic-row${carried === r.epic.itemId ? " backlog-epic-carried" : ""}${
              over === i && carried ? " backlog-epic-drop-above" : ""
            }${over === rows.length && i === rows.length - 1 && carried ? " backlog-epic-drop-below" : ""}`}
            draggable
            onDragStart={(e) => {
              e.dataTransfer.effectAllowed = "move";
              e.dataTransfer.setData("text/plain", r.epic.itemId);
              setCarried(r.epic.itemId);
            }}
            onDragEnd={() => {
              setCarried(null);
              setOver(null);
            }}
            onClick={() => onOpen(r.epic.itemId)}
          >
            <span className="backlog-epic-title">
              <KindIcon kind="epic" size={14} />
              <span className="card-title">{r.epic.title}</span>
              {r.split && (
                <span className="split-badge" title="Its tasks are scattered in the backlog: moving the epic gathers them">
                  Split
                </span>
              )}
            </span>
            <span className="backlog-epic-count" title="Its tasks waiting in these backlogs">
              {r.parked === 0 ? "empty" : `${r.parked} waiting`}
            </span>
            <span className="backlog-epic-chain" onClick={(e) => e.stopPropagation()}>
              <ParentChain chain={r.chain} onOpen={onOpenParent} />
            </span>
          </div>
        ))}
      </div>
    </section>
  );
}
