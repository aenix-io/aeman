// The Backlog board: one column per selected team, each that team's ranked
// list of the tasks it has accepted and not taken into work
// (docs/design/backlog.md). Top is next.
//
// The board decides nothing itself — what a column holds, the order, what a
// drop means, what the owner picker does and the optimistic shape of each
// are backlogboard.ts's — and it keeps no copy of the cards: the columns are
// recomputed from the board's cards, which the listing fills and the watch
// keeps current (K6). A card taken into work, finished or re-teamed out of
// the selection by anybody simply stops being one of them.
import { useCallback, useMemo, useRef, useState } from "react";
import type { Board, Card as CardModel, Provider, SizeKey } from "../providers/types";
import { teamColor } from "../avatar";
import {
  backlogColumns,
  columnTeams,
  dropPlan,
  dueLabel,
  dueOf,
  handedOverLocally,
  nearestPlanning,
  ownPlan,
  spliceOrder,
  takenIntoWorkLocally,
  type Anchor,
} from "../backlogboard";
import { justMade, noteMade } from "../justmade";
import { ownerOf, type Owner } from "../owner";
import { asksFirst, freeSubtasks, removeChoices, removeIntent, type RemoveChoice } from "../removal";
import { SIZES, SIZE_ORDER } from "../size";
import { currentSprint, previousSprint } from "../sprint";
import { todayIso } from "../date";
import { displayName, type Avatars, type Names } from "../users";
import { AddCard } from "./AddCard";
import { Avatar } from "./Avatar";
import { Dropdown } from "./Dropdown";
import { KindIcon } from "./KindIcon";
import { OwnerPicker } from "./OwnerPicker";
import { RemoveChoiceDialog } from "./RemoveChoiceDialog";
import { SizeChip } from "./SizeChip";
import { TeamChips } from "./TeamChips";

/** How far the pointer must travel before a press on a card becomes a drag;
 *  a press that stays put opens the card. */
const DRAG_SLOP = 4;

interface BacklogBoardProps {
  board: Board;
  provider: Provider;
  roster: string[];
  teamFilter: string[] | null;
  onSetFilter: (keys: string[] | null) => void;
  avatars: Avatars;
  names: Names;
  patchCard: (
    itemId: string,
    patch: Partial<CardModel> | ((c: CardModel) => Partial<CardModel>),
  ) => void;
  addCard: (card: CardModel) => void;
  removeCard: (uid: string) => void;
  /** The board's one order, re-sorted locally for a drop until the server's
   *  Ordering frame says the same. */
  reorderCards: (orderedItemIds: string[]) => void;
  reload: () => void;
  onOpen: (card: CardModel) => void;
  onError: (message: string) => void;
}

/** Spot is where a dragged card would land: whose column, and the place
 *  among the cards drawn in it. */
interface Spot {
  team: string;
  at: number;
}

/** dropSpot reads the board for a point: the column under it and the place
 *  in it, by which card's middle the point has passed — what the reader is
 *  actually aiming at. Off the DOM rather than tracked: a column carries its
 *  team the way a card carries its id. */
function dropSpot(x: number, y: number, carried: string): Spot | null {
  const column = document.elementFromPoint(x, y)?.closest<HTMLElement>("[data-backlog-team]");
  if (!column) {
    return null;
  }
  // The card in hand is drawn where it would land, not where it was, so it
  // is not a place of its own to count.
  const rows = [...column.querySelectorAll<HTMLElement>("[data-card]")].filter(
    (r) => r.dataset.card !== carried,
  );
  let at = rows.length;
  for (let i = 0; i < rows.length; i++) {
    const r = rows[i].getBoundingClientRect();
    if (y < r.top + r.height / 2) {
      at = i;
      break;
    }
  }
  return { team: column.dataset.backlogTeam ?? "", at };
}

const errText = (err: unknown) => (err instanceof Error ? err.message : String(err));

export function BacklogBoard({
  board,
  provider,
  roster,
  teamFilter,
  onSetFilter,
  avatars,
  names,
  patchCard,
  addCard,
  removeCard,
  reorderCards,
  reload,
  onOpen,
  onError,
}: BacklogBoardProps) {
  const today = todayIso();
  const teams = useMemo(() => columnTeams(teamFilter, roster), [teamFilter, roster]);
  const columns = useMemo(() => backlogColumns(board.cards, teams), [board.cards, teams]);
  const members = useMemo(() => board.members.map((m) => m.login), [board.members]);

  // ---- writing a place: a move by its neighbour ----
  const persistAnchor = useCallback(
    (id: string, anchor: Anchor) => {
      if (!anchor) {
        return Promise.resolve();
      }
      return "after" in anchor ? provider.moveCard(id, anchor.after) : provider.moveCardBefore(id, anchor.before);
    },
    [provider],
  );

  // ---- handing a card to another team's backlog, still parked (K5) ----
  // `own` with the team and nobody: the server's door for "this team's,
  // nobody's yet", which keeps a parked card parked and drops the person.
  const handOver = useCallback(
    (card: CardModel, team: string, order: string[] | null, anchor: Anchor) => {
      const before: Partial<CardModel> = { team: card.team, assignees: card.assignees, parked: card.parked };
      patchCard(card.itemId, handedOverLocally(team));
      if (order) {
        reorderCards(spliceOrder(board.cards.map((c) => c.itemId), order));
      }
      void provider
        .ownCard(card.itemId, team, "")
        .then((updated) => {
          addCard(updated);
          return persistAnchor(card.itemId, anchor);
        })
        .catch((err: unknown) => {
          patchCard(card.itemId, before);
          onError(errText(err));
          reload();
        });
    },
    [board.cards, patchCard, reorderCards, provider, addCard, persistAnchor, onError, reload],
  );

  // ---- the owner picker: a person takes it into work (K4) ----
  const own = useCallback(
    (card: CardModel, owner: Owner) => {
      const plan = ownPlan(card, owner);
      if (plan === "none") {
        return;
      }
      if (plan === "hand-over") {
        handOver(card, owner.team, null, null);
        return;
      }
      // It leaves the backlog at once — the optimistic card is in work, so
      // the column stops drawing it — and stands on the Team and Me boards
      // today when the reader goes there.
      const before: Partial<CardModel> = {
        team: card.team,
        assignees: card.assignees,
        parked: card.parked,
        startDate: card.startDate,
        day: card.day,
        zone: card.zone,
        sprintStart: card.sprintStart,
      };
      patchCard(card.itemId, takenIntoWorkLocally(owner, today, currentSprint(board, owner.team || null)));
      void provider
        .ownCard(card.itemId, owner.team, owner.person)
        .then(addCard)
        .catch((err: unknown) => {
          patchCard(card.itemId, before);
          onError(errText(err));
        });
    },
    [board, today, patchCard, provider, addCard, onError, handOver],
  );

  // ---- dragging: within a column a reorder, onto another a hand-over ----
  const [carried, setCarried] = useState<CardModel | null>(null);
  const [over, setOver] = useState<Spot | null>(null);
  const press = useRef<{ card: CardModel; x: number; y: number; moved: boolean } | null>(null);

  const drop = useCallback(
    (card: CardModel, spot: Spot | null) => {
      if (!spot) {
        return;
      }
      const target = columns.find((c) => c.team === spot.team);
      if (!target) {
        return;
      }
      const plan = dropPlan(
        card.itemId,
        card.team ?? "",
        spot.team,
        target.cards.map((c) => c.itemId),
        spot.at,
      );
      if (plan.kind === "none") {
        return;
      }
      if (plan.kind === "hand-over") {
        handOver(card, plan.team, plan.order, plan.anchor);
        return;
      }
      reorderCards(spliceOrder(board.cards.map((c) => c.itemId), plan.order));
      void persistAnchor(card.itemId, plan.anchor).catch((err: unknown) => {
        onError(errText(err));
        reload();
      });
    },
    [columns, board.cards, handOver, reorderCards, persistAnchor, onError, reload],
  );

  const beginDrag = useCallback(
    (card: CardModel) => (e: React.PointerEvent) => {
      if ((e.target as HTMLElement).closest("button, input, a")) {
        return;
      }
      e.preventDefault();
      press.current = { card, x: e.clientX, y: e.clientY, moved: false };
      const onMove = (ev: PointerEvent) => {
        const p = press.current;
        if (!p) {
          return;
        }
        if (!p.moved && Math.abs(ev.clientX - p.x) < DRAG_SLOP && Math.abs(ev.clientY - p.y) < DRAG_SLOP) {
          return;
        }
        p.moved = true;
        setCarried(p.card);
        setOver(dropSpot(ev.clientX, ev.clientY, p.card.itemId));
      };
      const done = (ev: PointerEvent, dropped: boolean) => {
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", up);
        window.removeEventListener("pointercancel", cancel);
        const p = press.current;
        press.current = null;
        setCarried(null);
        setOver(null);
        if (!p) {
          return;
        }
        if (!p.moved) {
          onOpen(p.card);
        } else if (dropped) {
          drop(p.card, dropSpot(ev.clientX, ev.clientY, p.card.itemId));
        }
      };
      const up = (ev: PointerEvent) => done(ev, true);
      const cancel = (ev: PointerEvent) => done(ev, false);
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", up);
      window.addEventListener("pointercancel", cancel);
    },
    [drop, onOpen],
  );

  // ---- adding: born in this column's backlog, parked on its team, last ----
  const addTo = useCallback(
    (team: string, title: string) => {
      void provider
        .createCard({ title, team: team || null, parked: true })
        .then((card) => {
          noteMade(card.itemId);
          addCard(card);
        })
        .catch((err: unknown) => onError(errText(err)));
    },
    [provider, addCard, onError],
  );

  // ---- the size: weighed where the work is being weighed up ----
  const [sizing, setSizing] = useState<CardModel | null>(null);
  const sizeAnchor = useRef<HTMLElement | null>(null);
  const openSizer = useCallback((card: CardModel, anchor: HTMLElement) => {
    sizeAnchor.current = anchor;
    setSizing((cur) => (cur?.itemId === card.itemId ? null : card));
  }, []);
  const setSize = (card: CardModel, size: SizeKey | "") => {
    setSizing(null);
    if ((card.size ?? "") === size) {
      return;
    }
    patchCard(card.itemId, { size: size || undefined });
    void provider.patchCard(card.itemId, { size }).catch((err: unknown) => {
      patchCard(card.itemId, { size: card.size });
      onError(errText(err));
    });
  };

  // ---- the ×: the same × as everywhere else (removal.ts) ----
  // On a card in its backlog the one answer it has is "off the board"; the
  // backlog answer is withheld from a card already there.
  const choicesFor = useCallback(
    (card: CardModel) =>
      removeChoices(card, {
        today,
        current: currentSprint(board, card.team ?? null) ?? undefined,
        previous: previousSprint(board, card.team ?? null) ?? undefined,
      }),
    [board, today],
  );
  const doRemove = useCallback(
    (card: CardModel, choice: RemoveChoice) => {
      const intent = removeIntent(choice);
      if (!intent) {
        return;
      }
      if (intent === "off-board") {
        for (const freed of freeSubtasks(board.cards, card.itemId)) {
          patchCard(freed.itemId, freed.patch);
        }
        removeCard(card.itemId);
      }
      void provider.removeCard(card.itemId, intent).catch((err: unknown) => {
        onError(errText(err));
        reload();
      });
    },
    [board.cards, patchCard, removeCard, provider, onError, reload],
  );
  const [asking, setAsking] = useState<CardModel | null>(null);
  const remove = (card: CardModel) => {
    if (asksFirst(card, justMade(card.itemId))) {
      setAsking(card);
      return;
    }
    const choice = choicesFor(card)[0];
    if (choice) {
      doRemove(card, choice);
    }
  };

  return (
    <div className="backlog-board">
      <div className="board-toolbar">
        <TeamChips
          label="Team"
          teams={roster}
          selectedKeys={teamFilter}
          onSelect={onSetFilter}
          domains={board.domains}
          // The no-team group has a backlog like any team's, so its chip is
          // always there to select it by.
          noneChip="No team"
          canManage={false}
          onAdd={() => {}}
          onRemove={() => {}}
        />
      </div>
      <div className="backlog-columns">
        {columns.map(({ team, cards }) => {
          const held = cards.filter((c) => c.itemId !== carried?.itemId);
          const landing = carried && over?.team === team ? carried : null;
          return (
            <section className="backlog-column" key={team || "\nnone"} aria-label={`${team || "No team"} backlog`}>
              <header className="backlog-column-head">
                <span className="backlog-column-dot" style={{ background: teamColor(team) }} aria-hidden="true" />
                <span className="backlog-column-name" title={team || "no team"}>
                  {team || "No team"}
                </span>
                <span className="backlog-column-count">{cards.length}</span>
              </header>
              <div
                className={`backlog-column-body${landing ? " backlog-column-over" : ""}`}
                data-backlog-team={team}
              >
                {held.map((c, i) => (
                  <div key={c.itemId}>
                    {landing && over?.at === i && <Landing card={landing} />}
                    <BacklogRow
                      card={c}
                      onPointerDown={beginDrag(c)}
                      onRemove={() => remove(c)}
                      onPickSize={openSizer}
                      owner={
                        <OwnerPicker
                          current={ownerOf(c)}
                          teams={roster}
                          people={members}
                          cards={board.cards}
                          avatars={avatars}
                          names={names}
                          onPick={(o) => own(c, o)}
                          title={`Who takes «${c.title}» into work`}
                        >
                          <OwnerFace card={c} avatars={avatars} names={names} />
                        </OwnerPicker>
                      }
                    />
                  </div>
                ))}
                {landing && (over?.at ?? 0) >= held.length && <Landing card={landing} />}
                {held.length === 0 && !landing && (
                  <p className="backlog-column-empty">Nothing waiting</p>
                )}
                <AddCard
                  placeholder="Add to the backlog…"
                  forcedTeam={team || null}
                  allowNoTeam={false}
                  onCreate={(title) => addTo(team, title)}
                />
              </div>
            </section>
          );
        })}
      </div>
      <Dropdown open={!!sizing} anchorRef={sizeAnchor} onClose={() => setSizing(null)} className="backlog-size-menu">
        {SIZE_ORDER.map((sz) => (
          <button
            key={sz}
            type="button"
            className={`backlog-size-item${sizing?.size === sz ? " backlog-size-item-on" : ""}`}
            title={SIZES[sz].hint}
            onClick={() => sizing && setSize(sizing, sizing.size === sz ? "" : sz)}
          >
            <span className="backlog-size-key">{sz}</span>
            <span className="backlog-size-pts">{SIZES[sz].points}</span>
          </button>
        ))}
      </Dropdown>
      {asking && (
        <RemoveChoiceDialog
          title={asking.title}
          progress={asking.progress ?? 0}
          choices={choicesFor(asking)}
          subtasks={board.cards.filter((c) => c.parent === asking.itemId).length}
          onClose={() => setAsking(null)}
          onSubmit={(choice) => doRemove(asking, choice)}
        />
      )}
    </div>
  );
}

interface BacklogRowProps {
  card: CardModel;
  owner: React.ReactNode;
  onPointerDown: (e: React.PointerEvent) => void;
  onRemove: () => void;
  onPickSize: (card: CardModel, anchor: HTMLElement) => void;
}

/** BacklogRow is one wide card: the title first; beside it the nearest
 *  planning card as a tag, the due date when that card has an end, the size
 *  and the owner. */
function BacklogRow({ card, owner, onPointerDown, onRemove, onPickSize }: BacklogRowProps) {
  const up = nearestPlanning(card);
  const due = dueOf(card);
  return (
    <div className="backlog-row" data-card={card.itemId} onPointerDown={onPointerDown} title={card.title}>
      <span className="backlog-row-title">{card.title}</span>
      {up?.type && (
        <span className="backlog-row-tag" title={`Under the ${up.type} «${up.title}»`}>
          <KindIcon kind={up.type} size={12} />
          <span className="backlog-row-tag-text">{up.title}</span>
        </span>
      )}
      {due && (
        <span className="backlog-row-due" title={`Due ${due}: the end of «${up?.title ?? ""}»`}>
          due {dueLabel(due)}
        </span>
      )}
      <SizeChip card={card} onPick={onPickSize} />
      <span className="backlog-row-owner">{owner}</span>
      <button
        type="button"
        className="backlog-row-remove"
        title="Take this off the board"
        aria-label="Take this off the board"
        onClick={onRemove}
      >
        ×
      </button>
    </div>
  );
}

/** OwnerFace is what the owner picker's trigger shows: the person when the
 *  card has one, and otherwise an invitation to take it. */
function OwnerFace({ card, avatars, names }: { card: CardModel; avatars: Avatars; names: Names }) {
  const person = card.assignees[0];
  if (!person) {
    return <span className="backlog-row-take">Take</span>;
  }
  return (
    <span className="backlog-row-person" title={displayName(person, names)}>
      <Avatar login={person} avatars={avatars} names={names} className="avatar-img backlog-row-avatar" />
      <span className="backlog-row-person-name">{displayName(person, names)}</span>
    </span>
  );
}

/** Landing is the card in hand, drawn where it would come to rest — an
 *  outline, since it is not there yet. */
function Landing({ card }: { card: CardModel }) {
  return (
    <div className="backlog-row backlog-row-landing">
      <span className="backlog-row-title">{card.title}</span>
    </div>
  );
}
