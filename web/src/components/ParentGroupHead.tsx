import { useRef, useState } from "react";

import type { GroupHead } from "../grouping";
import type { Avatars, Names } from "../users";
import { KindIcon } from "./KindIcon";
import { teamColor, teamInitial } from "../avatar";
import { displayName } from "../users";
import { Avatar } from "./Avatar";
import { PresenceMarks } from "./Card";
import { Dropdown } from "./Dropdown";

interface ParentGroupHeadProps {
  head: GroupHead;
  expanded: boolean;
  onToggle: () => void;
  /** Opens the add form for a task inside this plan item; absent where the
   *  cell takes no new work (a record). */
  onAdd?: () => void;
  /** The ×: send the group's work in this cell back to the backlog (the
   *  board asks first). Absent where nothing may go. */
  onRemove?: () => void;
  /** Whether the head is the board's selection. */
  selected?: boolean;
  /** The others who have the plan item selected (presence, #290). */
  selectedBy?: string[];
  /** The team avatar drawn dimmed, as the board's cards draw theirs. */
  dimAvatar?: boolean;
  /** Drawn on the right before the count — the Backlog's chain of the plan
   *  items above this one. */
  aside?: React.ReactNode;
  /** A group of review cards only reads "review: …" as a review card does
   *  (the Backlog); the day boards leave the stripe alone to say it. */
  reviewPrefix?: boolean;
  /** A click on the head, as on a card: the first selects it, the second
   *  opens the plan item (S15). The board decides which this one is. */
  onPress?: () => void;
  /** The avatar's menu: hand the group's work in this cell to a team or a
   *  person (grouping.reassignable). It never changes the plan item's own
   *  owner, which is what the avatar shows. Absent where nothing may move. */
  assign?: GroupAssign;
  avatars?: Avatars;
  names?: Names;
}

/** GroupAssign is what a head's avatar may hand the group's tasks to: the
 *  Team column given onTeam, the Person column given onPerson — as a task
 *  card's own menu offers on that board. */
export interface GroupAssign {
  teams: readonly string[];
  people?: readonly string[];
  /** The team every task of the group stands in, marked in the menu. */
  team?: string;
  /** The person every task of the group is with (the Team board's column). */
  person?: string;
  onTeam: (team: string | null) => void;
  onPerson?: (login: string | null) => void;
}

// A press on a control of the head is the control's: it must not start a
// drag of the head.
const own = (e: React.PointerEvent) => e.stopPropagation();

/** ParentGroupHead is the head of a "group by plan" group on the Team and Me
 *  boards: the plan item the cards below it stand in, with its kind's icon,
 *  the plan item above it named in the tooltip only (the head is short of
 *  room), whose the plan item is (its team
 *  and owner) and how much of the group in this cell is done. It folds and
 *  unfolds like a task's subtasks (folded by default), its + adds a task
 *  inside the plan item, and dragging it moves
 *  the group's cards in this cell together. A group of review cards only
 *  wears the review card's stripe. */
export function ParentGroupHead({
  head,
  expanded,
  onToggle,
  onAdd,
  onRemove,
  selected,
  selectedBy,
  dimAvatar,
  aside,
  reviewPrefix = false,
  onPress,
  assign,
  avatars,
  names,
}: ParentGroupHeadProps) {
  const [copied, setCopied] = useState(false);
  const [assignOpen, setAssignOpen] = useState(false);
  const assignRef = useRef<HTMLDivElement | null>(null);
  return (
    <div
      className={`group-head${head.reviewOnly ? " group-head-review" : ""}${selected ? " group-head-selected" : ""}${
        (selectedBy?.length ?? 0) > 0 ? " group-head-peer-selected" : ""
      }${dimAvatar ? " card-dim-avatar" : ""}`}
      onClick={onPress}
    >
      <button
        type="button"
        className="group-head-toggle"
        aria-expanded={expanded}
        aria-label={expanded ? "Fold the group" : "Unfold the group"}
        onPointerDown={own}
        onClick={(e) => {
          e.stopPropagation();
          onToggle();
        }}
      >
        <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true" className={expanded ? "open" : ""}>
          <path d="M3 2l4 3-4 3" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </button>
      {head.type === "none" ? (
        // Ungrouped: the cards in no plan item — an empty dashed box.
        <svg className="group-head-kind group-head-kind-none" width="14" height="14" viewBox="0 0 16 16" aria-hidden="true">
          <rect x="2.5" y="2.5" width="11" height="11" rx="2.5" fill="none" stroke="currentColor" strokeWidth="1.4" strokeDasharray="2.6 2" />
        </svg>
      ) : (
        <KindIcon kind={head.type} size={14} className="group-head-kind" />
      )}
      <PresenceMarks selectedBy={selectedBy} avatars={avatars} names={names} />
      <span className="group-head-text" title={head.above ? `${head.above} › ${head.title}` : head.title}>
        <span className="group-head-title">
          {reviewPrefix && head.reviewOnly ? "review: " : ""}
          {head.title}
        </span>
      </span>
      {/* The hover lane, as a card's: ID, × and +, growing to the left. */}
      <span className="card-actions">
        {head.uid && (
          <button
            type="button"
            className="card-action card-action-id card-hoveronly"
            aria-label="Copy the plan item's id"
            title={copied ? "Copied" : "Copy id"}
            onPointerDown={own}
            onClick={(e) => {
              e.stopPropagation();
              void navigator.clipboard?.writeText(head.uid).then(() => {
                setCopied(true);
                window.setTimeout(() => setCopied(false), 1200);
              });
            }}
          >
            {copied ? "✓" : "ID"}
          </button>
        )}
        {onRemove && (
          <button
            type="button"
            className="card-action card-hoveronly card-action-delete"
            aria-label="Send the group back to the backlog"
            title="Send to backlog"
            onPointerDown={own}
            onClick={(e) => {
              e.stopPropagation();
              onRemove();
            }}
          >
            ×
          </button>
        )}
        {onAdd && (
          <button
            type="button"
            className="card-action card-hoveronly card-subs-add"
            aria-label="Add a task to this plan item"
            title="Add a task"
            onPointerDown={own}
            onClick={(e) => {
              e.stopPropagation();
              onAdd();
            }}
          >
            +
          </button>
        )}
      </span>
      {aside}
      <span className="group-head-count">{head.done > 0 ? `${head.done}/${head.count}` : head.count}</span>
      {/* Whose the plan item is, drawn exactly as a task card draws whose it
          is (Card's card-assign): the team's avatar flush right, the owner's
          small one on its corner. */}
      {(head.team || head.owner || assign) && (
        <div
          className={`card-assign${assign ? " group-head-assign" : ""}`}
          ref={assignRef}
          role={assign ? "button" : undefined}
          title={assign ? "Hand the group's tasks to a team or a person" : undefined}
          onPointerDown={assign ? own : undefined}
          onClick={
            assign
              ? (e) => {
                  e.stopPropagation();
                  setAssignOpen((o) => !o);
                }
              : undefined
          }
        >
          {head.team ? (
            <span className="team-avatar" style={{ backgroundColor: teamColor(head.team) }} title={`Team: ${head.team}`}>
              {teamInitial(head.team)}
            </span>
          ) : (
            <span className="team-avatar team-avatar-empty" title="No team" />
          )}
          {head.owner && (
            <span className="card-counterpart-avatar-btn" title={`Owner: ${displayName(head.owner, names)}`}>
              <Avatar login={head.owner} avatars={avatars} names={names} className="card-counterpart-avatar" />
            </span>
          )}
        </div>
      )}
      {assign && (
        <Dropdown open={assignOpen} anchorRef={assignRef} onClose={() => setAssignOpen(false)} className="card-stage-menu card-assign-menu">
          {/* The menu is portalled, but React still bubbles its events to the
              head: a press in it must neither start a drag nor select. */}
          <div onPointerDown={own} onClick={(e) => e.stopPropagation()}>
          <div className="card-counterpart-head">The group&apos;s open tasks here</div>
          <div className="card-assign-cols">
            <div className="card-assign-col">
              <div className="card-assign-head">Team</div>
              {assign.teams.map((t) => (
                <button
                  key={`t-${t}`}
                  type="button"
                  className={`card-stage-item${assign.team === t ? " card-stage-item-active" : ""}`}
                  onClick={(e) => {
                    e.stopPropagation();
                    setAssignOpen(false);
                    assign.onTeam(t);
                  }}
                >
                  <span className="team-dot" style={{ background: teamColor(t) }} />
                  {t}
                </button>
              ))}
              <button
                type="button"
                className="card-stage-item card-stage-clear"
                onClick={(e) => {
                  e.stopPropagation();
                  setAssignOpen(false);
                  assign.onTeam(null);
                }}
              >
                No team
              </button>
            </div>
            {assign.onPerson && (
              <div className="card-assign-col">
                <div className="card-assign-head">Person</div>
                {(assign.people ?? []).map((p) => (
                  <button
                    key={`p-${p}`}
                    type="button"
                    className={`card-stage-item${assign.person === p ? " card-stage-item-active" : ""}`}
                    onClick={(e) => {
                      e.stopPropagation();
                      setAssignOpen(false);
                      assign.onPerson?.(p);
                    }}
                  >
                    <Avatar login={p} avatars={avatars} names={names} />
                    {displayName(p, names)}
                  </button>
                ))}
                <button
                  type="button"
                  className="card-stage-item card-stage-clear"
                  onClick={(e) => {
                    e.stopPropagation();
                    setAssignOpen(false);
                    assign.onPerson?.(null);
                  }}
                >
                  Unassigned
                </button>
              </div>
            )}
          </div>
          </div>
        </Dropdown>
      )}
    </div>
  );
}
