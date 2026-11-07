import { useMemo, useState, type ReactNode } from "react";

import type { Card as CardModel, CardType } from "../providers/types";
import { ownerLabel, type Owner } from "../owner";
import { roadmapOwnerCell } from "../ownership";
import { childLevels, KIND_LABEL, kindOf, statusOf, STATUS_LABEL, subtreeRows, type Row } from "../roadmap";
import { isPlanning } from "../subtasks";
import { displayName, type Avatars, type Names } from "../users";
import { AddCard } from "./AddCard";
import { KindIcon } from "./KindIcon";
import { OwnerBadge } from "./OwnerBadge";

// The Roadmap's tree, shared: the title cell of a row (indent guides, the
// fold chevron, the kind, the title, the count, the row's + and ×), its
// status pill and its owner cell are drawn by the Roadmap (RoadmapBoard, with
// its timeline, dates and drag) and by the card drawer's "Under it" (PlanTree,
// with neither) from the same pieces, so the two cannot drift apart.

/** The width of one level of indent, in px. */
export const INDENT_PX = 18;

/** The height of a row of the tree, in px. */
export const ROW_PX = 30;

/** TreeTitle is a row's title cell: the guides of the levels above, the
 *  chevron that folds what the row has under it (a leaf keeps its width),
 *  the kind, the title, a plan item's done/total, and the row's + and ×. A
 *  control's press never reaches the row. */
export function TreeTitle({
  row,
  open,
  onToggle,
  onAdd,
  onRemove,
}: {
  row: Row;
  open: boolean;
  onToggle: () => void;
  onAdd: (() => void) | null;
  onRemove: (() => void) | null;
}) {
  const { node, depth, foldable } = row;
  const card = node.card;
  const stop = (fn: () => void) => ({
    onClick: (e: { stopPropagation: () => void }) => {
      e.stopPropagation();
      fn();
    },
    onPointerDown: (e: { stopPropagation: () => void }) => e.stopPropagation(),
  });
  return (
    <span className="roadmap-cell roadmap-cell-title" style={{ paddingLeft: 6 + depth * INDENT_PX }}>
      {Array.from({ length: depth }, (_, i) => (
        <span key={i} className="roadmap-guide" style={{ left: 6 + i * INDENT_PX + 10 }} aria-hidden="true" />
      ))}
      {foldable > 0 ? (
        <button
          type="button"
          className={`roadmap-toggle${open ? " roadmap-toggle-open" : ""}`}
          aria-label={open ? "Fold" : `Unfold ${foldable}`}
          aria-expanded={open}
          {...stop(onToggle)}
        >
          <svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true">
            <path d="M5.5 3.5 10 8l-4.5 4.5" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </button>
      ) : (
        // A leaf keeps the chevron's width so the indent lines up, and
        // nothing else: not a button, nothing to hover or focus.
        <span className="roadmap-toggle-spacer" aria-hidden="true" />
      )}
      <KindIcon kind={node.kind} size={15} />
      <span className="roadmap-title" title={card.title}>
        {card.title}
      </span>
      {isPlanning(card) && card.hierarchy && card.hierarchy.total > 0 && (
        <span className="roadmap-count" title={`${card.hierarchy.done} of ${card.hierarchy.total} tasks done`}>
          {card.hierarchy.done}/{card.hierarchy.total}
        </span>
      )}
      {onAdd && (
        <button type="button" className="roadmap-row-add" title={`Add under «${card.title}»`} aria-label={`Add under ${card.title}`} {...stop(onAdd)}>
          +
        </button>
      )}
      {onRemove && (
        <button
          type="button"
          className="roadmap-row-x"
          title={`Delete «${card.title}», or take it out from under its parent`}
          aria-label={`Delete ${card.title}`}
          {...stop(onRemove)}
        >
          ×
        </button>
      )}
    </span>
  );
}

/** StatusPill is a row's status: a plan item's derived standing, a task's
 *  own (roadmap.statusOf). */
export function StatusPill({ card }: { card: CardModel }) {
  const status = statusOf(card);
  return <span className={`roadmap-status roadmap-status-${status}`}>{STATUS_LABEL[status]}</span>;
}

/** OwnerCell is what a row's Owner column shows: the owner badge (the
 *  team's avatar, the person's over it, one label); a team taken from above
 *  (O3) greyed, saying where from. */
export function OwnerCell({ owner, avatars, names, from }: { owner: Owner; avatars?: Avatars; names?: Names; from?: string }) {
  const label = ownerLabel(owner, (l) => displayName(l, names));
  const inherited = from !== undefined;
  const title = inherited ? `${label} — ${from ? `from «${from}»` : "inherited"}` : label;
  return (
    <span
      className={`roadmap-owner${owner.team || owner.person ? "" : " roadmap-owner-none"}${inherited ? " roadmap-owner-inherited" : ""}`}
      title={title}
    >
      <OwnerBadge owner={owner} avatars={avatars} names={names} />
    </span>
  );
}

/** ownerOfRow is a row's owner as the Owner column reads it: a plan item's
 *  team and owner, a task's team and person, a team taken from above greyed
 *  (roadmapOwnerCell); a project card has none. */
export function ownerOfRow(card: CardModel, cards: readonly CardModel[]): { owner: Owner; from?: string } | null {
  const kind = kindOf(card);
  if (kind === "project") {
    return null;
  }
  const planning = kind === "goal" || kind === "initiative" || kind === "epic";
  const shown = roadmapOwnerCell(card, cards, planning);
  return { owner: shown.owner, from: shown.from };
}

const levelOptions = (levels: (CardType | "task")[]) => levels.map((k) => ({ key: k, label: KIND_LABEL[k] }));

/** PlanTree is the card drawer's "Under it": the Roadmap's tree rooted ONE
 *  LEVEL BELOW the card the drawer shows (roadmap.subtreeRows) — every level
 *  under it, folded as the reader folds it — with the Roadmap's rows (kind,
 *  fold, title, owner, status) and no timeline, no dates and no drag. A row
 *  opens its card; a plan item's + opens an add form under it (the levels it
 *  may hold, the next one first) that stays open for the next one; the add
 *  row at the foot makes a direct child of the card, the next level down
 *  first. */
export function PlanTree({
  cards,
  root,
  onOpen,
  onCreate,
  onRemove,
  avatars,
  names,
  readOnly = false,
}: {
  cards: readonly CardModel[];
  /** The card the drawer shows: the tree is what stands under it. */
  root: CardModel;
  onOpen: (card: CardModel) => void;
  /** Make a card of a level under a parent. */
  onCreate: (title: string, level: CardType | "task", parent: string) => void;
  onRemove: ((card: CardModel) => void) | null;
  avatars?: Avatars;
  names?: Names;
  /** A record of a past day, or a card not made yet: nothing to add. */
  readOnly?: boolean;
}) {
  const [folded, setFolded] = useState<Set<string>>(() => new Set());
  // The open add form — under a row, or the foot's ("") — and its round, so
  // a form that just made a card opens again, empty, for the next one.
  const [adding, setAdding] = useState<{ under: string; round: number } | null>(null);
  const lines = useMemo(() => subtreeRows(cards, root.itemId, (id) => !folded.has(id)), [cards, root.itemId, folded]);
  const toggle = (id: string) =>
    setFolded((f) => {
      const next = new Set(f);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  const form = (parent: CardModel, depth: number, key: string) => {
    const kids = childLevels(kindOf(parent));
    return (
      <div className="roadmap-row roadmap-row-form plan-tree-form" style={{ paddingLeft: 30 + depth * INDENT_PX }}>
        <AddCard
          key={`${key}-${adding?.round ?? 0}`}
          autoOpen
          compact
          placeholder={`New ${KIND_LABEL[kids[0]].toLowerCase()} under «${parent.title}»…`}
          forcedTeam={null}
          picker={kids.length > 1 ? { title: "Level", options: levelOptions(kids), initial: kids[0] } : undefined}
          onCreate={(title, _team, picked) => onCreate(title, (picked as CardType | "task") || kids[0], parent.itemId)}
          onClosed={(created) => setAdding((a) => (created && a ? { ...a, round: a.round + 1 } : null))}
        />
      </div>
    );
  };
  const rootKids = childLevels(kindOf(root));
  return (
    <div className="plan-tree" role="tree" aria-label={`Under «${root.title}»`}>
      {lines.map((r) => {
        const card = r.node.card;
        const planning = isPlanning(card);
        const kids = childLevels(r.node.kind);
        const owner = ownerOfRow(card, cards);
        const rowOwner: ReactNode = owner ? <OwnerCell owner={owner.owner} from={owner.from} avatars={avatars} names={names} /> : null;
        return (
          <div key={card.itemId} className="roadmap-rowgroup">
            <div className="roadmap-row plan-tree-row" style={{ height: ROW_PX }} role="treeitem" aria-level={r.depth + 1} onClick={() => onOpen(card)}>
              <TreeTitle
                row={r}
                open={!folded.has(card.itemId)}
                onToggle={() => toggle(card.itemId)}
                onAdd={!readOnly && planning && kids.length > 0 ? () => setAdding(adding?.under === card.itemId ? null : { under: card.itemId, round: 0 }) : null}
                onRemove={!readOnly && planning && onRemove ? () => onRemove(card) : null}
              />
              <span className="roadmap-cell roadmap-cell-owner">{rowOwner}</span>
              <span className="roadmap-cell roadmap-cell-status">
                <StatusPill card={card} />
              </span>
            </div>
            {adding?.under === card.itemId && form(card, r.depth + 1, card.itemId)}
          </div>
        );
      })}
      {!readOnly && rootKids.length > 0 &&
        (adding?.under === "" ? (
          form(root, 0, "foot")
        ) : (
          <button type="button" className="drawer-add-open plan-tree-add" onClick={() => setAdding({ under: "", round: 0 })}>
            + Add {rootKids.map((l) => KIND_LABEL[l].toLowerCase()).join(" or ")}
          </button>
        ))}
    </div>
  );
}
