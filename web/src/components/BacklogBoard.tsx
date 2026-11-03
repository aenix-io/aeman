// The Backlog board: ONE ranked list of the tasks the selected teams have
// accepted and not taken into work, mixed in one order, each card wearing a
// chip of its team (docs/design/backlog.md). Top is next.
//
// The board decides nothing itself — what the list holds, the order, what a
// drop means, what the owner picker does, what the add box offers and the
// optimistic shape of each are backlogboard.ts's — and it keeps no copy of
// the cards: the list is recomputed from the board's cards, which the
// listing fills and the watch keeps current (K6). A card taken into work, finished or re-teamed out of
// the selection by anybody simply stops being one of them.
import { useCallback, useMemo, useRef, useState } from "react";
import type { Board, Card as CardModel, Provider, SizeKey } from "../providers/types";
import {
  addBox,
  backlogSections,
  dropPlan,
  dueLabel,
  dueOf,
  handedOverLocally,
  nearestPlanning,
  openPlanning,
  ownPlan,
  parentChain,
  reviewOwnPlan,
  shownTeams,
  spliceOrder,
  takenIntoWorkLocally,
  type Anchor,
} from "../backlogboard";
import type { LineageRef } from "../providers/types";
import { justMade, noteMade } from "../justmade";
import { ownerOf, type Owner } from "../owner";
import { asksFirst, freeSubtasks, removeChoices, removeIntent, type RemoveChoice } from "../removal";
import { SIZES, SIZE_ORDER } from "../size";
import { currentSprint, previousSprint } from "../sprint";
import { todayIso } from "../date";
import type { Avatars, Names } from "../users";
import { AddCard } from "./AddCard";
import { Dropdown } from "./Dropdown";
import { KindIcon } from "./KindIcon";
import { OwnerBadge } from "./OwnerBadge";
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

/** dropSpot reads the list for a point: the place among the cards drawn in
 *  it, by which card's middle the point has passed — what the reader is
 *  actually aiming at — or null off the list. */
function dropSpot(x: number, y: number, carried: string): number | null {
  const list = document.elementFromPoint(x, y)?.closest<HTMLElement>("[data-backlog-list]");
  if (!list) {
    return null;
  }
  // The card in hand is drawn where it would land, not where it was, so it
  // is not a place of its own to count.
  const rows = [...list.querySelectorAll<HTMLElement>("[data-card]")].filter((r) => r.dataset.card !== carried);
  for (let i = 0; i < rows.length; i++) {
    const r = rows[i].getBoundingClientRect();
    if (y < r.top + r.height / 2) {
      return i;
    }
  }
  return rows.length;
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
  const teams = useMemo(() => shownTeams(teamFilter, roster), [teamFilter, roster]);
  const { reviews, tasks: list } = useMemo(() => backlogSections(board.cards, teams), [board.cards, teams]);
  const adding = useMemo(() => addBox(teams), [teams]);
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
  // The card's team chip is its owner picker; `own` with a team and nobody
  // is the server's door for "this team's, nobody's yet", which keeps a
  // parked card parked and drops the person. The card keeps its place.
  const handOver = useCallback(
    (card: CardModel, team: string) => {
      const before: Partial<CardModel> = { team: card.team, assignees: card.assignees, parked: card.parked };
      patchCard(card.itemId, handedOverLocally(team));
      void provider
        .ownCard(card.itemId, team, "")
        .then(addCard)
        .catch((err: unknown) => {
          patchCard(card.itemId, before);
          onError(errText(err));
          reload();
        });
    },
    [patchCard, provider, addCard, onError, reload],
  );

  // ---- the owner picker: a person takes it into work (K4) ----
  const own = useCallback(
    (card: CardModel, owner: Owner) => {
      // A review nobody has taken takes a person and nothing else (K10):
      // the person is its reviewer, and it keeps the reviewed task's team.
      if (card.reviewOf) {
        if (reviewOwnPlan(owner) === "none") {
          return;
        }
        owner = { team: card.team ?? "", person: owner.person };
      }
      const plan = card.reviewOf ? "take" : ownPlan(card, owner);
      if (plan === "none") {
        return;
      }
      if (plan === "hand-over") {
        handOver(card, owner.team);
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

  // ---- dragging: a reorder in the one list, by its neighbours there ----
  const [carried, setCarried] = useState<CardModel | null>(null);
  const [over, setOver] = useState<number | null>(null);
  const press = useRef<{ card: CardModel; x: number; y: number; moved: boolean } | null>(null);

  const drop = useCallback(
    (card: CardModel, at: number | null) => {
      if (at === null) {
        return;
      }
      const plan = dropPlan(
        card.itemId,
        list.map((c) => c.itemId),
        at,
      );
      if (plan.kind === "none") {
        return;
      }
      reorderCards(spliceOrder(board.cards.map((c) => c.itemId), plan.order));
      void persistAnchor(card.itemId, plan.anchor).catch((err: unknown) => {
        onError(errText(err));
        reload();
      });
    },
    [list, board.cards, reorderCards, persistAnchor, onError, reload],
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

  // ---- adding: born in the chosen team's backlog, parked, last ----
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

  // ---- a parent in a card's chain: that planning card's drawer ----
  const openParent = useCallback(
    (uid: string) => void openPlanning(provider, uid, onOpen, onError),
    [provider, onOpen, onError],
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

  // Whose the card is: the owner badge (the team's avatar, the person's over
  // it) behind the owner picker; and, while nobody has the card, a separate
  // Take button LEFT of it that opens the same picker. On a review nobody has
  // taken the picker asks for its reviewer.
  const ownerControls = (c: CardModel) => {
    const picker = (child: React.ReactNode, className: string, title: string) => (
      <OwnerPicker
        current={ownerOf(c)}
        teams={roster}
        people={members}
        cards={board.cards}
        avatars={avatars}
        names={names}
        onPick={(o) => own(c, o)}
        className={className}
        title={title}
      >
        {child}
      </OwnerPicker>
    );
    const person = c.assignees[0];
    return (
      <>
        {!person &&
          picker(
            <span className="backlog-take-label">Take</span>,
            "backlog-take",
            c.reviewOf ? `Who reviews «${c.title}»` : `Who takes «${c.title}» into work`,
          )}
        {picker(
          <OwnerBadge owner={ownerOf(c)} avatars={avatars} names={names} />,
          "backlog-row-badge",
          c.reviewOf
            ? `Who reviews «${c.title}»`
            : `Whose «${c.title}» is: a team keeps it in the backlog, a person takes it into work`,
        )}
      </>
    );
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
      <section className="backlog-list" aria-label="Backlog">
        {reviews.length > 0 && (
          // The reviews nobody has taken, pinned on top and out of the drag:
          // asked for now, and not the lead's to rank (K10).
          <div className="backlog-pinned" aria-label="Needs a reviewer">
            {reviews.map((c) => (
              <BacklogRow
                key={c.itemId}
                card={c}
                pinned
                onPointerDown={(e) => {
                  if (!(e.target as HTMLElement).closest("button, input, a")) {
                    onOpen(c);
                  }
                }}
                onRemove={() => remove(c)}
                onPickSize={openSizer}
                onOpenParent={openParent}
                owner={ownerControls(c)}
              />
            ))}
          </div>
        )}
        <div className={`backlog-list-body${carried ? " backlog-list-over" : ""}`} data-backlog-list="">
          {(() => {
            const held = list.filter((c) => c.itemId !== carried?.itemId);
            const landing = carried && over !== null ? carried : null;
            return (
              <>
                {held.map((c, i) => (
                  <div key={c.itemId}>
                    {landing && over === i && <Landing card={landing} />}
                    <BacklogRow
                      card={c}
                      onPointerDown={beginDrag(c)}
                      onRemove={() => remove(c)}
                      onPickSize={openSizer}
                      onOpenParent={openParent}
                      owner={ownerControls(c)}
                    />
                  </div>
                ))}
                {landing && (over ?? 0) >= held.length && <Landing card={landing} />}
                {held.length === 0 && !landing && <p className="backlog-list-empty">Nothing waiting</p>}
              </>
            );
          })()}
          <AddCard
            placeholder="Add to the backlog…"
            {...adding}
            onCreate={(title, team) =>
              addTo("forcedTeam" in adding ? (adding.forcedTeam ?? "") : (team ?? ""), title)
            }
          />
        </div>
      </section>
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
  /** A review nobody has taken, pinned on top: marked, and out of the drag. */
  pinned?: boolean;
  owner: React.ReactNode;
  onPointerDown: (e: React.PointerEvent) => void;
  onRemove: () => void;
  onPickSize: (card: CardModel, anchor: HTMLElement) => void;
  onOpenParent: (uid: string) => void;
}

/** BacklogRow is one wide card: the title first; on the right the planning
 *  cards it sits under, the due date when the nearest has an end, the size,
 *  and its team and owner (the owner picker). */
function BacklogRow({ card, pinned, owner, onPointerDown, onRemove, onPickSize, onOpenParent }: BacklogRowProps) {
  const up = nearestPlanning(card);
  const due = dueOf(card);
  return (
    <div
      className={`backlog-row${pinned ? " backlog-row-pinned" : ""}`}
      data-card={pinned ? undefined : card.itemId}
      onPointerDown={onPointerDown}
      title={card.title}
    >
      {pinned && <span className="backlog-row-needs">needs a reviewer</span>}
      <span className="backlog-row-title">{card.title}</span>
      <ParentChain chain={parentChain(card)} onOpen={onOpenParent} />
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

/** Landing is the card in hand, drawn where it would come to rest — an
 *  outline, since it is not there yet. */
function Landing({ card }: { card: CardModel }) {
  return (
    <div className="backlog-row backlog-row-landing">
      <span className="backlog-row-title">{card.title}</span>
    </div>
  );
}

/** ParentChain is a card's parent chain, root first — goal, initiative,
 *  epic — on the right side of the card, each with its kind icon as the
 *  Roadmap draws it. Every name opens that planning card's drawer; a long one
 *  is cut short and named whole in its tooltip. */
export function ParentChain({ chain, onOpen }: { chain: readonly LineageRef[]; onOpen: (uid: string) => void }) {
  if (chain.length === 0) {
    return null;
  }
  return (
    <nav className="backlog-row-chain" aria-label="Sits under">
      {chain.map((r, i) => (
        <span className="backlog-row-chain-step" key={r.uid}>
          {i > 0 && (
            <span className="backlog-row-chain-sep" aria-hidden="true">
              ›
            </span>
          )}
          <button
            type="button"
            className="backlog-row-chain-link"
            title={`The ${r.type} «${r.title}»`}
            onClick={() => onOpen(r.uid)}
          >
            {r.type && <KindIcon kind={r.type} size={14} />}
            <span className="backlog-row-chain-text">{r.title}</span>
          </button>
        </span>
      ))}
    </nav>
  );
}
