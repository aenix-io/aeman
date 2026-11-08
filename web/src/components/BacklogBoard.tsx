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
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { Board, Card as CardModel, Provider } from "../providers/types";
import {
  insertSpot,
  byReadiness,
  epicTakeOwner,
  type Readiness,
  epicRows,
  epicsByReadiness,
  type BacklogMode,
  insertedOrder,
  backlogList,
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
  takeOwner,
  spliceOrder,
  takenIntoWorkLocally,
  type Anchor,
} from "../backlogboard";
import type { LineageRef } from "../providers/types";
import { BacklogEpics } from "./BacklogEpics";
import { readMode } from "../viewprefs";
import { justMade, noteMade } from "../justmade";
import { ownerOf, planOwnerOf, planOwnPatch, type Owner } from "../owner";
import {
  asksFirst,
  freeSubtasks,
  removeChoices,
  removeIntent,
  unaskedRemoval,
  withdrawPatch,
  type RemoveChoice,
} from "../removal";
import { currentSprint, previousSprint } from "../sprint";
import { todayIso } from "../date";
import type { Avatars, Names } from "../users";
import { AddCard } from "./AddCard";
import type { OrderArg } from "../cardorder";
import { PresenceMarks } from "./Card";
import { clickOn, selectedByOf, type PresenceMap } from "../presence";
import { Avatar } from "./Avatar";
import { displayName } from "../users";
import { InsertGap } from "./InsertGap";
import { KindIcon } from "./KindIcon";
import { markOf } from "../placements";
import { OwnerBadge } from "./OwnerBadge";
import { OwnerPicker } from "./OwnerPicker";
import { RemoveChoiceDialog } from "./RemoveChoiceDialog";
import { FilterSelect } from "./FilterSelect";
import { customerOptions, teamOptions } from "../filteroptions";
import { bornCustomers, customerPasses, sprintTeamOf } from "../ownership";
import { useKeptCreated } from "../keptcreated";

/** How far the pointer must travel before a press on a card becomes a drag;
 *  a press that stays put opens the card. */
const DRAG_SLOP = 4;

interface BacklogBoardProps {
  board: Board;
  provider: Provider;
  roster: string[];
  teamFilter: string[] | null;
  onSetFilter: (keys: string[] | null) => void;
  /** The day boards' Customer filter (O11): [] = none, else ids (OR). */
  customerFilter?: string[];
  onSetCustomerFilter?: (next: string[]) => void;
  /** Open the team and customer rosters' dialogs, from the bottom of the
   *  Team and Customer filters. */
  onManageTeams?: () => void;
  onManageCustomers?: () => void;
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
  reorderCards: (order: OrderArg) => void;
  /** Move a card right beside its anchor in the board's order the list is
   *  sorted by (Board.boardOrder), as the server is about to. */
  moveInBoardOrder?: (id: string, anchor: Anchor) => void;
  reload: () => void;
  onOpen: (card: CardModel) => void;
  onError: (message: string) => void;
  /** The viewer's login: TAKE makes the card theirs. */
  me: string;
  /** Others' live selections (login → card uid), drawn on the cards. */
  presence?: PresenceMap;
  /** Broadcast this board's selection as shared presence (#290). */
  onPresence?: (card: string | null) => void;
  /** Tasks | Epics, the app's: drawn in its top bar (ViewControls),
   *  remembered per viewer. Left out, the board reads the remembered one. */
  mode?: BacklogMode;
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
  customerFilter = [],
  onSetCustomerFilter,
  onManageTeams,
  onManageCustomers,
  onSetFilter,
  avatars,
  names,
  patchCard,
  addCard,
  removeCard,
  reorderCards,
  moveInBoardOrder,
  reload,
  onOpen,
  onError,
  me,
  presence,
  onPresence,
  mode: modeProp,
}: BacklogBoardProps) {
  const today = todayIso();
  const teams = useMemo(() => shownTeams(teamFilter, roster), [teamFilter, roster]);
  // The Customer filter by effective customers — the server's rule for the
  // listing and the watch (O11), applied to what the board holds.
  const { kept, keep } = useKeptCreated(customerFilter.join(","));
  // All / Ready / Blocked: a filter, never an order (ordering.md).
  const [readiness, setReadiness] = useState<Readiness>("all");
  const whole = useMemo(
    () => backlogList(board.cards, teams, board.boardOrder).filter((c) => customerPasses(c, board.cards, customerFilter, kept)),
    [board.cards, board.boardOrder, teams, customerFilter, kept],
  );
  const list = useMemo(() => byReadiness(whole, readiness), [whole, readiness]);
  const members = useMemo(() => board.members.map((m) => m.login), [board.members]);

  // ---- Tasks | Epics (ordering.md): one order, two readings ----
  // The mode is the viewer's own and switching writes nothing. The Epics
  // mode reads the epics — empty ones too, which no backlog listing holds —
  // off the Strategy listing, and orders them by the board's order.
  const [storedMode] = useState<BacklogMode>(readMode);
  const mode = modeProp ?? storedMode;
  const [scope, setScope] = useState<CardModel[] | null>(null);
  const [scopeTick, setScopeTick] = useState(0);
  useEffect(() => {
    if (mode !== "epics") {
      return;
    }
    let live = true;
    provider
      .listCards({ view: "strategy" })
      .then((l) => {
        if (live) {
          setScope(l.cards);
        }
      })
      .catch((err: unknown) => {
        if (live) {
          onError(err instanceof Error ? err.message : String(err));
        }
      });
    return () => {
      live = false;
    };
  }, [mode, provider, scopeTick, onError]);
  const [epicOrder, setEpicOrder] = useState<string[] | null>(null);
  const epics = useMemo(() => {
    // The counts are of every waiting task; All / Ready / Blocked reads the
    // epics' own lock (own or inherited), the same rule as for tasks.
    const rows = epicsByReadiness(epicRows(scope ?? [], whole, teams, board.boardOrder), readiness);
    if (!epicOrder) {
      return rows;
    }
    const at = new Map(epicOrder.map((id, i) => [id, i]));
    return [...rows].sort((a, b) => (at.get(a.epic.itemId) ?? 1e9) - (at.get(b.epic.itemId) ?? 1e9));
  }, [scope, whole, teams, board.boardOrder, epicOrder, readiness]);
  const moveEpic = useCallback(
    (epicId: string, anchor: { after: string } | { before: string }, order: string[]) => {
      // Shown at once; the server gathers the epic's tasks, and the board
      // reads back both the epics and the tasks once it has.
      setEpicOrder(order);
      void provider
        .moveTo(epicId, anchor)
        .catch((err: unknown) => onError(errText(err)))
        .finally(() => {
          setEpicOrder(null);
          setScopeTick((n) => n + 1);
          reload();
        });
    },
    [provider, onError, reload],
  );

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
      patchCard(card.itemId, takenIntoWorkLocally(owner, today, currentSprint(board, sprintTeamOf(card, board.cards, owner.team))));
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

  // ---- Take: the backlog's own gesture, "I take this into work" (K11) ----
  // The server gives the card to whoever asks, on its own team; the
  // optimistic card is the same answer (takeOwner), so it leaves the
  // backlog at once, and a refusal (work already in hand, say) puts it back
  // and shows the server's words.
  const take = useCallback(
    (card: CardModel) => {
      const owner = takeOwner(card, me);
      const before: Partial<CardModel> = {
        assignees: card.assignees,
        parked: card.parked,
        startDate: card.startDate,
        day: card.day,
        zone: card.zone,
        sprintStart: card.sprintStart,
      };
      // Taken on its own team, in the sprint of the team it stands under
      // (O13): an inheriting task joins its plan item's team's sprint.
      patchCard(card.itemId, takenIntoWorkLocally(owner, today, currentSprint(board, sprintTeamOf(card, board.cards))));
      void provider
        .takeCard(card.itemId)
        .then(addCard)
        .catch((err: unknown) => {
          patchCard(card.itemId, before);
          onError(errText(err));
        });
    },
    [me, board, today, patchCard, provider, addCard, onError],
  );

  // ---- selecting: shared live through presence (#290) ----
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const selectedRef = useRef<string | null>(null);
  selectedRef.current = selectedId;
  useEffect(() => {
    onPresence?.(selectedId);
  }, [selectedId]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => () => onPresence?.(null), []); // eslint-disable-line react-hooks/exhaustive-deps

  // ---- dragging: a reorder in the one list, by its neighbours there ----
  const [carried, setCarried] = useState<CardModel | null>(null);
  const [over, setOver] = useState<number | null>(null);
  const press = useRef<{ card: CardModel; x: number; y: number; moved: boolean } | null>(null);

  const drop = useCallback(
    (card: CardModel, at: number | null) => {
      if (at === null) {
        return;
      }
      const plan = dropPlan(card.itemId, list, at);
      // A review nobody has taken is never dragged (K10): the drag showed
      // it as forbidden, and nothing moves.
      if (plan.kind !== "reorder") {
        return;
      }
      reorderCards(spliceOrder(board.cards.map((c) => c.itemId), plan.order));
      // The board's order the list is sorted by moves the same way the
      // server will move it: right beside its anchor.
      moveInBoardOrder?.(card.itemId, plan.anchor);
      void persistAnchor(card.itemId, plan.anchor).catch((err: unknown) => {
        onError(errText(err));
        reload();
      });
    },
    [list, board.cards, reorderCards, moveInBoardOrder, persistAnchor, onError, reload],
  );
  // Where the card in hand would land is not a place it may go: next to
  // a review nobody has taken, whose place is its task's (K10).
  const forbidden = useMemo(
    () => !!carried && over !== null && dropPlan(carried.itemId, list, over).kind === "forbidden",
    [carried, over, list],
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
          // The first click selects the card — the others see it — and a
          // click on the selected card opens it (#290).
          if (clickOn(selectedRef.current === p.card.itemId) === "open") {
            onOpen(p.card);
          } else {
            setSelectedId(p.card.itemId);
          }
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

  // ---- adding: the insert "+" in a gap, born there in ONE create ----
  // The create names its place (after the card above the gap, or before the
  // card below it at the top) and the server places it in the same request;
  // the board shows it in place the moment the answer arrives.
  const [inserting, setInserting] = useState<number | null>(null);
  // The inherited epic's × (Q10): the gap it was taken off in.
  const [noEpicAt, setNoEpicAt] = useState<number | null>(null);
  const insertAt = useCallback(
    (at: number, team: string, title: string) => {
      const { anchor, epic } = insertSpot(list, at, teams);
      const parent = epic && noEpicAt !== at ? epic.uid : null;
      // Typed in under the Customer filter: born for its one customer, or —
      // with several chosen — kept shown until the filter changes, so it
      // does not vanish the moment it is made (O11).
      const born = bornCustomers(customerFilter);
      void provider
        .createCard({
          title,
          team: team || null,
          parked: true,
          customers: born ?? null,
          parent,
          after: anchor && "after" in anchor ? anchor.after : null,
          before: anchor && "before" in anchor ? anchor.before : null,
        })
        .then((card) => {
          noteMade(card.itemId);
          if (customerFilter.length > 1) {
            keep(card.itemId);
          }
          addCard(card);
          reorderCards((cur) => insertedOrder(cur, card.itemId, anchor));
        })
        .catch((err: unknown) => onError(errText(err)));
    },
    [list, teams, provider, addCard, reorderCards, onError, customerFilter, keep, noEpicAt],
  );
  const gap = (at: number, empty = false) => {
    const { box, epic } = insertSpot(list, at, teams);
    return (
      <InsertGap
        key={`gap-${at}`}
        at={at}
        open={inserting === at}
        always={empty}
        onOpen={() => {
          setNoEpicAt(null);
          setInserting(at);
        }}
      >
        <AddCard
          placeholder="Add to the backlog…"
          autoOpen
          chip={epic && noEpicAt !== at ? { label: epic.title, onClear: () => setNoEpicAt(at) } : null}
          {...box}
          onCreate={(title, team) => insertAt(at, "forcedTeam" in box ? (box.forcedTeam ?? "") : (team ?? ""), title)}
          onClosed={() => setInserting(null)}
        />
      </InsertGap>
    );
  };


  // ---- an epic's team and owner, set from the Epics mode (ownership.md, O9):
  // the Strategy board's gesture; the list is read again after the answer ----
  const ownEpic = (epic: CardModel, next: Owner) => {
    const change = planOwnPatch(planOwnerOf(epic), next);
    if (!change) {
      return;
    }
    setScope((cur) => cur && cur.map((c) => (c.itemId === epic.itemId ? { ...c, team: next.team || undefined, owner: next.person || undefined } : c)));
    void provider
      .patchCard(epic.itemId, change)
      .catch((err: unknown) => onError(errText(err)))
      .finally(() => setScopeTick((n) => n + 1));
  };

  // ---- a parent in a card's chain: that planning card's drawer ----
  const openParent = useCallback(
    (uid: string) => void openPlanning(provider, uid, onOpen, onError),
    [provider, onOpen, onError],
  );

  // ---- the ×: the same × as everywhere else (removal.ts) ----
  // On a card in its backlog the one answer it has is "off the board"; the
  // backlog answer is withheld from a card already there. A review nobody
  // has taken is withdrawn instead (removal.removeChoices).
  const choicesFor = useCallback(
    (card: CardModel) =>
      removeChoices(card, {
        today,
        current: currentSprint(board, sprintTeamOf(card, board.cards)) ?? undefined,
        previous: previousSprint(board, sprintTeamOf(card, board.cards)) ?? undefined,
      }),
    [board, today],
  );
  const doRemove = useCallback(
    (card: CardModel, choice: RemoveChoice) => {
      const intent = removeIntent(choice);
      if (!intent) {
        return;
      }
      // Withdrawing a review nobody has taken: the card goes and its task
      // leaves review, back to In progress (K10) — both shown at once.
      if (choice === "withdraw") {
        const task = board.cards.find((c) => c.itemId === card.reviewOf);
        if (task) {
          patchCard(task.itemId, withdrawPatch(task));
        }
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
    // The withdrawal always asks: it changes the TASK, which is not the card
    // the × was pressed on.
    if (asksFirst(card, justMade(card.itemId)) || choicesFor(card).includes("withdraw")) {
      setAsking(card);
      return;
    }
    const choice = unaskedRemoval(choicesFor(card));
    if (choice) {
      doRemove(card, choice);
    }
  };

  // Whose the card is: the team's avatar (and the person's over it), which
  // is the owner picker — assign to whom. TAKE is a button of its own, the
  // right-most on the card while nobody has it: it asks first, and makes the
  // card the viewer's (takeOwner).
  const ownerControls = (c: CardModel) => (
    <OwnerPicker
      current={ownerOf(c)}
      teams={roster}
      people={members}
      cards={board.cards}
      avatars={avatars}
      names={names}
      onPick={(o) => own(c, o)}
      className="backlog-row-badge"
      title={
        c.reviewOf
          ? `Who reviews «${c.title}»`
          : `Whose «${c.title}» is: a team keeps it in the backlog, a person takes it into work`
      }
    >
      <OwnerBadge owner={ownerOf(c)} avatars={avatars} names={names} label={false} />
    </OwnerPicker>
  );
  // A review card's counterpart — whose the reviewed task is — as the Team
  // and Me boards draw it on a review card: the small round avatar on the
  // card's own avatar (Card.tsx). The people are the server's
  // (status.reviewed), since the backlog does not hold the task.
  const counterpart = (c: CardModel) => {
    const people = c.reviewOf ? (c.reviewed?.assignees ?? []) : [];
    if (people.length === 0) {
      return null;
    }
    return (
      <button
        type="button"
        className="card-counterpart-avatar-btn"
        title={`In implementation: ${people.map((p) => displayName(p, names)).join(", ")}`}
        onClick={() => c.reviewed && openParent(c.reviewed.uid)}
      >
        <Avatar login={people[0]} avatars={avatars} names={names} className="card-counterpart-avatar" />
      </button>
    );
  };
  const [taking, setTaking] = useState<CardModel | null>(null);
  // Take on an epic (Epics mode): the viewer becomes its owner.
  const [takingEpic, setTakingEpic] = useState<CardModel | null>(null);

  return (
    <div className="backlog-board">
      <div className="board-toolbar">
        <FilterSelect
          label="Team"
          anyLabel="all"
          options={teamOptions(roster)}
          selected={teamFilter ?? []}
          onChange={(next) => onSetFilter(next.length ? next : null)}
          manage={onManageTeams ? { label: "Manage teams", onClick: onManageTeams } : undefined}
          title="Show only these teams' backlogs"
        />
        <FilterSelect
          label="Customer"
          options={customerOptions(board.customers)}
          selected={customerFilter}
          onChange={(next) => onSetCustomerFilter?.(next)}
          manage={onManageCustomers ? { label: "Manage customers", onClick: onManageCustomers } : undefined}
          title="Show only the work for these customers"
        />
        <div className="segmented segmented-compact backlog-readiness" role="group" aria-label="Readiness">
          {(["all", "ready", "blocked"] as const).map((m) => (
            <button
              key={m}
              type="button"
              className={`segment${readiness === m ? " segment-active" : ""}`}
              aria-pressed={readiness === m}
              onClick={() => setReadiness(m)}
            >
              {m === "all" ? "All" : m === "ready" ? "Ready" : "Blocked"}
            </button>
          ))}
        </div>
      </div>
      {mode === "epics" && (
        <BacklogEpics
          rows={epics}
          loading={scope === null}
          onMove={moveEpic}
          onOpen={(uid) => openParent(uid)}
          onOpenParent={openParent}
          avatars={avatars}
          names={names}
          onTake={(epic) => setTakingEpic(epic)}
          ownerPicker={(epic, badge) => (
            <OwnerPicker
              planning
              current={planOwnerOf(epic)}
              teams={roster}
              people={members}
              cards={board.cards}
              avatars={avatars}
              names={names}
              onPick={(o) => ownEpic(epic, o)}
              className="backlog-row-badge"
              title={`Who carries «${epic.title}» out (a team) and who answers for it (its owner)`}
            >
              {badge}
            </OwnerPicker>
          )}
        />
      )}
      {mode === "tasks" && (
      <section className="backlog-list" aria-label="Backlog">
        <div className={`backlog-list-body${carried ? " backlog-list-over" : ""}${forbidden ? " backlog-list-forbid" : ""}`} data-backlog-list="">
          {(() => {
            const held = list.filter((c) => c.itemId !== carried?.itemId);
            const landing = carried && over !== null ? carried : null;
            return (
              <>
                {!carried && gap(0, held.length === 0)}
                {held.map((c, i) => (
                  <div key={c.itemId} className="backlog-slot">
                    {landing && over === i && <Landing card={landing} forbidden={forbidden} />}
                    <BacklogRow
                      card={c}
                      selected={c.itemId === selectedId}
                      selectedBy={selectedByOf(presence, c, undefined, me)}
                      avatars={avatars}
                      names={names}
                      onPointerDown={beginDrag(c)}
                      onRemove={() => remove(c)}
                      onOpenParent={openParent}
                      owner={
                        counterpart(c) ? (
                          // As a review card on the Team and Me boards draws
                          // it: the main avatar and the counterpart in one
                          // .card-assign, the small one over its corner.
                          <div className="card-assign">
                            {ownerControls(c)}
                            {counterpart(c)}
                          </div>
                        ) : (
                          ownerControls(c)
                        )
                      }
                      onTake={c.assignees.length === 0 ? () => setTaking(c) : undefined}
                    />
                    {!carried && gap(i + 1)}
                  </div>
                ))}
                {landing && (over ?? 0) >= held.length && <Landing card={landing} forbidden={forbidden} />}
                {held.length === 0 && !landing && <p className="backlog-list-empty">Nothing waiting</p>}
              </>
            );
          })()}
        </div>
      </section>
      )}
      {takingEpic && (
        <TakeDialog
          title={takingEpic.title}
          heading="Take this epic?"
          text={`«${takingEpic.title}»: you become its owner. Its team stays as it is.`}
          onClose={() => setTakingEpic(null)}
          onTake={() => ownEpic(takingEpic, epicTakeOwner(takingEpic, me ?? ""))}
        />
      )}
      {taking && (
        <TakeDialog
          title={taking.title}
          onClose={() => setTaking(null)}
          onTake={() => take(taking)}
        />
      )}
      {asking && (
        <RemoveChoiceDialog
          title={asking.title}
          review={!!asking.reviewOf}
          reviewing={board.cards.find((c) => c.itemId === asking.reviewOf)?.title}
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
  /** This board's selection (#290). */
  selected?: boolean;
  /** Who else has it selected (presence), drawn at its left edge. */
  selectedBy?: string[];
  avatars?: Avatars;
  names?: Names;
  owner: React.ReactNode;
  onPointerDown: (e: React.PointerEvent) => void;
  onRemove: () => void;
  onOpenParent: (uid: string) => void;
  /** Take, while nobody has the card: asks first (TakeDialog). */
  onTake?: () => void;
}

/** BacklogRow is one wide card, drawn as the Me and Team boards draw a card
 *  (`card`, a review card with its review mark): the title first; on the
 *  right the planning cards it sits under, the due date when the nearest
 *  has an end, the size, its team's avatar (the owner picker) and,
 *  right-most while nobody has it, Take. The × stands left of all of that,
 *  in a slot that takes no room: it shows over the title's end on hover. */
function BacklogRow({
  card,
  selected,
  selectedBy,
  avatars,
  names,
  owner,
  onPointerDown,
  onRemove,
  onOpenParent,
  onTake,
}: BacklogRowProps) {
  const up = nearestPlanning(card);
  const due = dueOf(card);
  const mark = markOf(card);
  return (
    <div
      className={`card backlog-card${mark ? ` card-${mark}` : ""}${selected ? " card-selected" : ""}${(selectedBy?.length ?? 0) > 0 ? " card-peer-selected" : ""}`}
      data-card={card.itemId}
      onPointerDown={onPointerDown}
      title={card.title}
    >
      <PresenceMarks selectedBy={selectedBy} avatars={avatars} names={names} />
      <span className="card-title backlog-card-title">{card.title}</span>
      <span className="backlog-row-remove-slot">
        <button
          type="button"
          className="backlog-row-remove"
          title="Take this off the board"
          aria-label="Take this off the board"
          onClick={onRemove}
        >
          ×
        </button>
      </span>
      <ParentChain chain={parentChain(card)} onOpen={onOpenParent} />
      {due && (
        <span className="backlog-row-due" title={`Due ${due}: the end of «${up?.title ?? ""}»`}>
          due {dueLabel(due)}
        </span>
      )}
      <span className="backlog-row-owner">{owner}</span>
      {onTake && (
        <button
          type="button"
          className="backlog-take-btn"
          aria-haspopup="dialog"
          title={card.reviewOf ? "Review this yourself" : "Take this card into work yourself"}
          onClick={onTake}
        >
          Take
        </button>
      )}
    </div>
  );
}

/** TakeDialog asks before TAKE makes a card the viewer's and takes it into
 *  work. */
export function TakeDialog({
  title,
  onClose,
  onTake,
  heading = "Take this card into work?",
  text,
}: {
  title: string;
  onClose: () => void;
  onTake: () => void;
  heading?: string;
  text?: string;
}) {
  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div
        className="modal modal-narrow"
        role="dialog"
        aria-modal="true"
        aria-label={heading}
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => {
          if (e.key === "Escape") {
            onClose();
          }
        }}
      >
        <div className="modal-header">
          <h2 className="modal-title">{heading}</h2>
          <button type="button" className="modal-close" onClick={onClose} aria-label="Close">
            ✕
          </button>
        </div>
        <div className="modal-body">
          <p className="take-dialog-text">{text ?? `«${title}» becomes yours, today, and leaves the backlog.`}</p>
          <div className="take-dialog-actions">
            <button type="button" className="btn" onClick={onClose}>
              Cancel
            </button>
            <button
              type="button"
              className="btn btn-primary"
              onClick={() => {
                onTake();
                onClose();
              }}
            >
              Take
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

/** Landing is the card in hand, drawn where it would come to rest — an
 *  outline, since it is not there yet, but otherwise the card being dragged:
 *  a review keeps its review stripe. Where it may not come to rest (next to
 *  a review nobody has taken, K10) the outline says so. */
export function Landing({ card, forbidden = false }: { card: CardModel; forbidden?: boolean }) {
  const mark = markOf(card);
  return (
    <div
      className={`card backlog-card${mark ? ` card-${mark}` : ""} backlog-card-landing${forbidden ? " backlog-card-landing-forbid" : ""}`}
      title={forbidden ? "A review nobody has taken stands on top until somebody takes it" : undefined}
    >
      <span className="card-title backlog-card-title">{card.title}</span>
    </div>
  );
}

/** ParentChain is a card's parent chain, root first — goal, initiative,
 *  epic — on the right side of the card, each with its kind icon as the
 *  Strategy draws it. Every name opens that planning card's drawer; a long one
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
            title={r.type ? `The ${r.type} «${r.title}»` : `The task it reviews, «${r.title}»`}
            onClick={() => onOpen(r.uid)}
          >
            <KindIcon kind={r.type ?? "task"} size={12} />
            <span className="backlog-row-chain-text">{r.title}</span>
          </button>
        </span>
      ))}
    </nav>
  );
}
