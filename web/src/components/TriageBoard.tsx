// The Triage board: who does what, and in which week.
//
// The strip on the left holds the cards nobody has given a week — the point
// of the board, and on a board that has never been triaged, most of it. The
// grid on the right is the Project board's own grid (WeekGrid) with a
// different pair of axes: PEOPLE across, WEEKS down. Dragging a card from the
// strip into a cell is the whole gesture — it says who and when at once,
// which is what triaging a card means.
//
// A card is a plain box of ONE WEEK. A card whose work takes longer is drawn
// once per week, each saying which part of the whole it is — (1/2), (2/2) —
// and each counting against what the teams on screen can close that week: two
// weeks stretched is two weeks of work, not one filed early. Drawing them as
// separate boxes is what lets a week's cards stand one under the next at the
// full column width, instead of the column being sliced into slivers.
//
// What is overdue is shown as owed NOW: the grid opens on this week, and an
// open card from a week gone by stands in the first row. That is the weekly
// plan's own rule (planShowsInWeekAt), and this board must not disagree.
import { useCallback, useMemo, useRef, useState } from "react";
import type { Board, Card as CardModel, Provider, ZoneKey } from "../providers/types";
import { registerPendingCard } from "../api/pending";
import { addDays, mondayOf, todayIso } from "../date";
import {
  anchorFor,
  byPile,
  gripOf,
  needsTriage,
  orderWith,
  placedIn,
  removableOnTriage,
} from "../triage";
import { asksFirst, freeSubtasks, removeChoices, type RemoveChoice } from "../removal";
import { RemoveChoiceDialog } from "./RemoveChoiceDialog";
import { isPersonalDomain } from "../domains";
import { markOf } from "../placements";
import { ADD_ZONE, acceptsNewCard } from "../meboard";
import { displayName, type Avatars, type Names } from "../users";
import { ZONES, ZONE_ORDER } from "../zones";
import { type Laned, extentOf, laneStyle, packLanes, weekLabel } from "../weekgrid";
import { barColor } from "../stages";
import { teamColor } from "../avatar";
import { AddCard } from "./AddCard";
import { Avatar } from "./Avatar";
import { TeamChips } from "./TeamChips";
import { WeekGrid } from "./WeekGrid";
import { ZoomControl } from "./ZoomControl";
import { useWeekGrid } from "./useWeekGrid";

// The column a card with no assignee stands in. An empty login is a real
// value on a card, so the key is something no login can be.
const NOBODY = " nobody";

// The board's own zoom and column widths, both this browser's.
const LS_ZOOM = "aeman.triage.zoom";
const LS_WIDTHS = "aeman.triage.colWidths";
/** The order the reader dragged the columns into. People have no order on
 *  the board — nothing on the server says one person comes before another —
 *  so it is this browser's, beside the widths. */
const LS_PEOPLE = "aeman.triage.people";

/** How far the pointer must travel before a press on a card becomes a drag. */
const DRAG_SLOP = 4;

interface TriageBoardProps {
  board: Board;
  provider: Provider;
  /** The viewer's own login. A person adds work FOR THEMSELVES only as
   *  unplanned — the other zones are the plan, and the plan is made with the
   *  team (boardservice.planningYourOwnWork) — so their own column offers the
   *  one zone the server will accept. */
  me?: string;
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
  replaceCard: (tempId: string, card: CardModel) => void;
  removeCard: (uid: string) => void;
  /** The board's own order, for a card dropped between two others. */
  reorderCards: (orderedItemIds: string[]) => void;
  /** Fetch the board again — the answer to a write the board cannot undo
   *  locally. */
  reload: () => void;
  onOpen: (card: CardModel) => void;
  onError: (message: string) => void;
}

interface Slot extends Laned {
  card: CardModel;
  /** Which week of the card this box is, out of how many. A card of one week
   *  is 0 of 1 and says nothing; a longer one says (1/2), (2/2). */
  part: number;
  parts: number;
  /** A turn a process is going to file, drawn before it exists. There is no
   *  card behind it yet, so nothing can be done to it — but the week it
   *  falls in is already spoken for, and a board that plans weeks ahead has
   *  to say so. */
  projected?: boolean;
}

function isDone(c: CardModel): boolean {
  return c.stage === "done" || (c.progress ?? 0) >= 100;
}

function whoOf(c: CardModel): string {
  return c.assignees[0] || NOBODY;
}

function readPeopleOrder(): string[] | null {
  try {
    const v: unknown = JSON.parse(localStorage.getItem(LS_PEOPLE) ?? "null");
    return Array.isArray(v) && v.every((k) => typeof k === "string") ? (v as string[]) : null;
  } catch {
    // A corrupt entry is not worth a broken board.
    return null;
  }
}

export function TriageBoard({
  board,
  provider,
  me,
  roster,
  teamFilter,
  onSetFilter,
  avatars,
  names,
  patchCard,
  addCard,
  replaceCard,
  removeCard,
  reorderCards,
  reload,
  onOpen,
  onError,
}: TriageBoardProps) {
  const today = todayIso();
  const thisWeek = mondayOf(today);
  // What each person is carrying altogether, whatever team it is in: the
  // board is read through a filter and a person is not, so somebody with
  // four cards here may have eleven, and the column says which.
  const carrying = useMemo(() => {
    const out: Record<string, number> = {};
    for (const m of board.members) {
      if (m.carrying) {
        out[m.login] = m.carrying;
      }
    }
    return out;
  }, [board.members]);
  const teams = useMemo(() => teamFilter ?? roster, [teamFilter, roster]);

  // A project card's weeks belong to the Project board, and this board holds
  // them still so that nobody re-dates another board's commitment by brushing
  // past it. The catch lifts that, for as long as the reader means to: it is
  // deliberately NOT remembered — a guard that stays open is not a guard, and
  // the next visit starts closed again.
  const [unlocked, setUnlocked] = useState(false);

  // The order the reader dragged the columns into, if they have.
  const [order, setOrder] = useState<string[] | null>(readPeopleOrder);
  // The tasks whose turns are MEANT to pile up: with the catch lifted those
  // are the ones a turn may be carried out of its own cycle for (gripOf).
  const accumulating = useMemo(() => {
    const out = new Set<string>();
    for (const p of board.processes) {
      for (const t of p.tasks) {
        if (t.accumulate) {
          out.add(t.uid);
        }
      }
    }
    return out;
  }, [board.processes]);

  // The work the weeks ahead are already carrying, drawn before it exists:
  // the turns the processes are going to file, and the repeating cards that
  // are going to come round. A paused process sends none, and a week whose
  // turn is already filed is a card and is drawn as one — the server leaves
  // those out (board.UpcomingTurns / UpcomingRecurrences), so nothing is
  // counted twice.
  const projected = useMemo(() => {
    const out: { key: string; week: string; who: string; card: CardModel }[] = [];
    // A repeating card that came from no process comes round all the same:
    // carry-over reseeds it, and the weeks it will land in are as spoken for
    // as a turn's. The server says which (status.due) — the calendar is its.
    for (const c of board.cards) {
      if (!c.due?.length || !teams.includes(c.team ?? "")) {
        continue;
      }
      for (const week of c.due) {
        out.push({
          key: `~again/${c.itemId}/${week}`,
          week,
          who: whoOf(c),
          card: { ...c, itemId: `~again/${c.itemId}/${week}`, week, day: undefined },
        });
      }
    }
    for (const p of board.processes) {
      for (const t of p.tasks) {
        if (!teams.includes(t.team ?? "")) {
          continue;
        }
        for (const week of t.due ?? []) {
          out.push({
            key: `${p.name}/${t.uid}/${week}`,
            week,
            who: t.assignee || NOBODY,
            // A stand-in, so a projection draws like everything else here.
            card: {
              itemId: `~turn/${t.uid}/${week}`,
              title: t.title,
              assignees: t.assignee ? [t.assignee] : [],
              team: t.team,
              week,
              task: t.uid,
              process: p.name,
            } as CardModel,
          });
        }
      }
    }
    return out;
  }, [board.cards, board.processes, teams]);

  // The cards of the teams on screen: the ones with a week, and the ones
  // nobody has dated, which stand in the first row alongside them.
  const { placed, waiting, people } = useMemo(() => {
    const placed: CardModel[] = [];
    const waiting: CardModel[] = [];
    const seen = new Set<string>();
    for (const c of board.cards) {
      if (!teams.includes(c.team ?? "")) {
        continue;
      }
      const week = placedIn(c);
      // What counts as waiting is what needsTriage says — the server's own
      // rule, so the two never disagree. A review and a subtask follow the
      // card they belong to and are nobody's decision to make.
      if (!week && !needsTriage(c)) {
        continue;
      }
      // A card that HAS a week still has to be work someone is doing.
      if (week && (c.parent || isPersonalDomain(c.domain ?? "") || isDone(c))) {
        continue;
      }
      seen.add(whoOf(c));
      (week ? placed : waiting).push(c);
    }
    // Nobody's column stands first — it is where a card waits for an owner —
    // and the rest by name, until the reader drags them into an order of
    // their own. Somebody who appears afterwards joins the end rather than
    // disturbing what was arranged.
    for (const t of projected) {
      seen.add(t.who);
    }
    const named = [...seen].filter((w) => w !== NOBODY).sort();
    const all = [NOBODY, ...named];
    const at = (k: string) => {
      const i = order ? order.indexOf(k) : -1;
      return i < 0 ? order?.length ?? 0 : i;
    };
    return {
      placed,
      waiting,
      people: (order ? [...all].sort((a, b) => at(a) - at(b)) : all).map((key) => ({ key })),
    };
  }, [board.cards, teams, order, projected]);


  // What the reader can see of each person here, against what that person is
  // holding altogether: the board is one team's slice and the count beside a
  // name says how much of the whole it is.
  const shown = useMemo(() => {
    const out: Record<string, number> = {};
    for (const c of [...placed, ...waiting]) {
      out[whoOf(c)] = (out[whoOf(c)] ?? 0) + 1;
    }
    return out;
  }, [placed, waiting]);

  // Somewhere to start a card: a press on the empty part of a cell opens the
  // form there, and what it creates lands in that week, in that person's
  // hands. The zone is the one thing the cell cannot say for itself.
  const [composing, setComposing] = useState<{ row: number; col: number } | null>(null);

  // Where a card stands on THIS board, which is not where its dates put it on
  // any other. The row is the week triage gave it and nothing else: a card
  // waiting in the strip carries whatever the day board left on it, and a
  // start date in some week gone by would otherwise win over the week it was
  // just dropped in — and take the card off the grid the moment it was
  // placed. The end date stays, because it is what says how many weeks the
  // work takes. A debt is owed NOW, so a week gone by reads as this one; only
  // the ROW moves, and the card keeps the week it was given, so a failed
  // write rolls back to the truth.
  const rowDates = useCallback(
    (c: CardModel): { week: string; day?: string } => {
      const week = placedIn(c) ?? "";
      return { week: week && week < thisWeek ? thisWeek : week, day: c.day };
    },
    [thisWeek],
  );
  const dated = useMemo(() => placed.map(rowDates), [placed, rowDates]);

  const grid = useWeekGrid({
    dated,
    columns: people.length,
    store: { zoom: LS_ZOOM, widths: LS_WIDTHS },
    // A week's cards stand one under the next at the full width, and the
    // week grows to hold them — this board is read down a person's column.
    rows: "grow",
    selection: teams.join("\0"),
    // No past rows: what was owed in a week gone by is owed in the first one.
    back: 0,
  });
  const { weeks, rowAt, rowSpotAt, columnAt } = grid;

  // ---- the gestures. A press on a card becomes either a MOVE (to another
  // week, another person, or both) or a STRETCH of its bottom edge; a press
  // that never travels stays a click that opens the card. Both listen on the
  // window for their whole life, because the live preview re-lanes and
  // remounts the very element the press started on.
  const [move, setMove] = useState<{
    card: CardModel;
    row: number;
    span: number;
    col: number;
    /** Where in the row's stack the card would land — cards standing one
     *  under the next are also standing in an ORDER, and dropping between
     *  two of them is how that order is set. */
    at: number;
  } | null>(null);
  const [stretch, setStretch] = useState<{ card: CardModel; to: number } | null>(null);
  const press = useRef<{
    card: CardModel;
    row: number;
    span: number;
    grab: number;
    /** How far into the card, in card heights, the reader took hold of it.
     *  A card is carried by that point, so where it LOOKS like it will land
     *  is where it lands — without this it followed the cursor itself and
     *  came to rest a card away from where it was let go. */
    hold: number;
    pinned: boolean;
    /** The rows the card may be dropped in, when its grip is a window of
     *  weeks rather than all of them or none (triage.gripOf). */
    bounds: { first: number; last: number } | null;
  } | null>(null);
  const moveRef = useRef<typeof move>(null);
  const stretchRef = useRef<typeof stretch>(null);

  const arm = useCallback((onMove: (e: PointerEvent) => void, onUp: () => void) => {
    const up = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", up);
      window.removeEventListener("pointercancel", cancel);
      onUp();
    };
    const cancel = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", up);
      window.removeEventListener("pointercancel", cancel);
      press.current = null;
      moveRef.current = null;
      stretchRef.current = null;
      setMove(null);
      setStretch(null);
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", up);
    window.addEventListener("pointercancel", cancel);
  }, []);

  const fail = useCallback(
    (card: CardModel, before: Partial<CardModel>) => (err: Error) => {
      patchCard(card.itemId, before);
      onError(err.message);
    },
    [patchCard, onError],
  );

  // Placing a card: who first, then when. A card sent to a week ahead leaves
  // the day board until that week begins — the server clears its dates, and
  // the row shows that at once.
  const place = useCallback(
    (card: CardModel, week: string, who: string) => {
      const before = {
        week: card.week,
        assignees: card.assignees,
        triage: card.triage,
        startDate: card.startDate,
        day: card.day,
        sprintStart: card.sprintStart,
      };
      const to = who === NOBODY ? [] : [who];
      patchCard(card.itemId, (c) => ({
        week,
        triage: false,
        assignees: to,
        ...(week > thisWeek && !c.epic
          ? { startDate: undefined, day: undefined, sprintStart: undefined }
          : {}),
      }));
      const assign =
        whoOf(card) === who
          ? Promise.resolve(undefined)
          : provider.patchCard(card.itemId, { assignees: to });
      void assign
        .then(() => provider.placeCard(card.itemId, week))
        .then(addCard)
        .catch(fail(card, before));
    },
    [provider, patchCard, addCard, fail, thisWeek],
  );

  // The ids of one cell's cards, in the order they stand. The stack is drawn
  // in board order, so this is that order filtered down to the cell.
  const cellOrder = useCallback(
    (row: number, who: string): string[] => {
      const week = weeks[row] ?? "";
      const here: CardModel[] = [];
      for (const c of board.cards) {
        if (whoOf(c) !== who) {
          continue;
        }
        const at = placedIn(c) ? extentOf(rowDates(c), weeks) : { row: 0, span: 1 };
        if (at && row >= at.row && row < at.row + at.span && (placedIn(c) ? true : week === weeks[0])) {
          here.push(c);
        }
      }
      // Sorted the way the reader sees it, or the place they aimed at is not
      // the place the write would mean.
      return here.sort(byPile((c) => c)).map((c) => c.itemId);
    },
    [board.cards, weeks, rowDates],
  );

  // Putting a card down between two others: the cell's order with the card
  // at the place the pointer chose, kept here at once and written down as a
  // NEIGHBOUR — the board's order is global and a cell is a slice of it, so
  // a position in the slice means nothing to the server.
  const reorder = useCallback(
    (card: CardModel, row: number, who: string, at: number) => {
      const ids = orderWith(cellOrder(row, who), card.itemId, at);
      const inCell = new Set(ids);
      let i = 0;
      reorderCards(board.cards.map((c) => (inCell.has(c.itemId) ? ids[i++] : c.itemId)));
      const anchor = anchorFor(ids, card.itemId);
      if (!anchor) {
        return;
      }
      const persist =
        "after" in anchor
          ? provider.moveCard(card.itemId, anchor.after)
          : provider.moveCardBefore(card.itemId, anchor.before);
      void persist.catch((err: Error) => {
        onError(err.message);
      });
    },
    [board.cards, cellOrder, reorderCards, provider, onError],
  );

  // Handing a card to somebody and saying nothing about when. A project
  // card's week is its row on the Project board — its dates ARE its week —
  // so this board must not move it in time; who does the work is still this
  // board's to say.
  const assignTo = useCallback(
    (card: CardModel, who: string) => {
      const to = who === NOBODY ? [] : [who];
      const before = { assignees: card.assignees };
      patchCard(card.itemId, { assignees: to });
      void provider
        .patchCard(card.itemId, { assignees: to })
        .then(addCard)
        .catch(fail(card, before));
    },
    [provider, patchCard, addCard, fail],
  );


  // The × is the SAME × as everywhere else: the shared rule says what it
  // means for this card — the card goes back to its column, out of its
  // group, or off the board — and the person is asked only where the answer
  // destroys work (removal.ts, mirroring boardservice.Remove). A board that
  // answered this on its own is how one of them came to hard-delete what
  // another kept.
  const choicesFor = useCallback(
    (card: CardModel) =>
      removeChoices(card, {
        today,
        current: board.sprintStates[card.team ?? ""]?.current ?? undefined,
        previous: board.sprintStates[card.team ?? ""]?.previous ?? undefined,
      }),
    [board.sprintStates, today],
  );

  // What the server does, done here at once: a card taken off the board is
  // gone, and its subtasks are loose rather than left pointing at a parent
  // that no longer exists.
  const doRemove = useCallback(
    (card: CardModel, choice: RemoveChoice) => {
      if (choice === "off-board") {
        for (const freed of freeSubtasks(board.cards, card.itemId)) {
          patchCard(freed.itemId, freed.patch);
        }
        removeCard(card.itemId);
      } else if (choice === "ungroup") {
        patchCard(card.itemId, { parent: undefined });
      }
      void provider
        .removeCard(card.itemId, choice === "off-board" ? "off-board" : "unassign")
        .catch((err: Error) => {
          onError(err.message);
          reload();
        });
    },
    [board.cards, provider, patchCard, removeCard, onError, reload],
  );

  // Asked wherever the × DESTROYS something, and nowhere else: a card the ×
  // hands back to its column or takes out of a group is still on the board,
  // and a "Delete?" in front of that is how an × comes to be read as
  // deletion. The dialog is the one every board asks with — what is at
  // stake, and how many subtasks go along.
  const [asking, setAsking] = useState<CardModel | null>(null);
  const remove = useCallback(
    (card: CardModel) => {
      if (asksFirst(card, today)) {
        setAsking(card);
        return;
      }
      // Unasked, the card's own first answer stands — the same one the
      // dialog would have put at the top.
      const choice = choicesFor(card)[0];
      if (choice) {
        doRemove(card, choice);
      }
    },
    [today, choicesFor, doRemove],
  );

  // Stretching: the card's end date moves to the Friday of the week the
  // pointer let go over. The dates ARE the reach — the same two fields a
  // Project slot uses — so nothing new is stored for it.
  const stretchTo = useCallback(
    (card: CardModel, week: string) => {
      const from = placedIn(card);
      if (!from || week < from) {
        return;
      }
      const end = week === from ? "" : addDays(week, 4);
      if ((card.day ?? "") === end) {
        return;
      }
      const before = { day: card.day };
      patchCard(card.itemId, { day: end || undefined });
      void provider
        .patchCard(card.itemId, { dates: { start: card.startDate ?? "", end } })
        .then(addCard)
        .catch(fail(card, before));
    },
    [provider, patchCard, addCard, fail],
  );

  // ---- what the grid draws.
  //
  // A card taking more than one week is drawn as ONE CARD PER WEEK, each
  // saying which part of the whole it is — (1/2), (2/2). That is what lets
  // every card here be a plain box of one row: the week's cards then stand
  // one under the next at the full column width, and the week grows to hold
  // them, rather than the column being sliced into slivers nobody can read.
  const { slots, load } = useMemo(() => {
    const slots = new Map<string, Slot[]>();
    const load = new Map<string, number>();
    // A card nobody has dated stands in the first row — now — beside this
    // week's own work. It is one box of one week: there is no end date to
    // stretch it over, and it takes a week only once it has been given one.
    for (const c of waiting) {
      const col = move?.card.itemId === c.itemId ? (people[move.col]?.key ?? whoOf(c)) : whoOf(c);
      const row = move?.card.itemId === c.itemId ? move.row : 0;
      const list = slots.get(col) ?? [];
      list.push({
        card: c,
        row,
        span: 1,
        part: 0,
        parts: 1,
        lane: 0,
        lanes: 1,
        width: 1,
        stack: 0,
        stacked: 1,
      });
      slots.set(col, list);
      const w = weeks[row];
      load.set(w, (load.get(w) ?? 0) + 1);
    }
    for (const c of placed) {
      const at = extentOf(rowDates(c), weeks);
      if (!at) {
        continue;
      }
      let { row, span } = at;
      let col = whoOf(c);
      // A card under the pointer is drawn WHERE IT WOULD LAND, not where it
      // came from: the stack is what makes room for it, and building it from
      // the old place drew it on top of whatever already sat in the new one.
      if (move?.card.itemId === c.itemId) {
        row = move.row;
        span = Math.max(1, Math.min(move.span, weeks.length - row));
        col = people[move.col]?.key ?? col;
      } else if (stretch?.card.itemId === c.itemId) {
        span = Math.max(1, Math.min(stretch.to - row + 1, weeks.length - row));
      }
      const list = slots.get(col) ?? [];
      for (let i = 0; i < span; i++) {
        list.push({
          card: c,
          row: row + i,
          span: 1,
          part: i,
          parts: span,
          lane: 0,
          lanes: 1,
          width: 1,
          stack: 0,
          stacked: 1,
        });
        const w = weeks[row + i];
        load.set(w, (load.get(w) ?? 0) + 1);
      }
      slots.set(col, list);
    }
    for (const t of projected) {
      const row = weeks.indexOf(t.week);
      if (row < 0) {
        continue;
      }
      const list = slots.get(t.who) ?? [];
      list.push({
        card: t.card,
        row,
        span: 1,
        part: 0,
        parts: 1,
        projected: true,
        lane: 0,
        lanes: 1,
        width: 1,
        stack: 0,
        stacked: 1,
      });
      slots.set(t.who, list);
      load.set(t.week, (load.get(t.week) ?? 0) + 1);
    }
    // A week is read top down, so it is stacked in the order somebody
    // triaging wants to meet it: debts, the project's own work, then the
    // zones. Cards of one rank keep the board's order, which is the order a
    // reader set by hand.
    for (const list of slots.values()) {
      list.sort(byPile((s) => ({ ...s.card, projected: s.projected })));
    }
    // While a card is under the pointer it is drawn WHERE IT WOULD LAND —
    // including where among its new neighbours — so what the reader sees is
    // the order they are choosing, not the one they started from. The stack
    // is drawn in list order, so moving the slot in the list is the preview.
    if (move) {
      const key = people[move.col]?.key;
      const list = key ? slots.get(key) : undefined;
      for (const held of list?.filter((s) => s.card.itemId === move.card.itemId) ?? []) {
        const rest = (list as Slot[]).filter((o) => o !== held);
        const sameRow = rest.reduce<number[]>((out, o, i) => {
          if (o.row === held.row) {
            out.push(i);
          }
          return out;
        }, []);
        const place = Math.max(0, Math.min(move.at, sameRow.length));
        const insert =
          place < sameRow.length
            ? sameRow[place]
            : sameRow.length > 0
              ? sameRow[sameRow.length - 1] + 1
              : rest.length;
        rest.splice(insert, 0, held);
        (list as Slot[]).splice(0, rest.length + 1, ...rest);
      }
    }
    packLanes(slots.values(), undefined, grid.rowFit);
    return { slots, load };
  }, [placed, waiting, projected, rowDates, weeks, people, move, stretch, grid.rowFit]);


  const beginMove = useCallback(
    (card: CardModel, slot: Slot, col: number) => (e: React.PointerEvent) => {
      // A projection is not a card: there is nothing to take hold of, and
      // nothing a drop could be written to.
      if (slot.projected || (e.target as HTMLElement).closest("button, .triage-slot-resize")) {
        return;
      }
      e.preventDefault();
      e.stopPropagation();
      // How far this card may be carried in TIME (triage.gripOf); the hand it
      // is in is never in question.
      const grip = gripOf(card, {
        unlocked,
        accumulates: !!card.task && accumulating.has(card.task),
      });
      press.current = {
        card,
        row: slot.row - slot.part,
        span: slot.parts,
        pinned: grip === "pinned",
        // The weeks the grip allows, as row indexes into the board's own
        // scale. Absent when the card may go anywhere.
        bounds:
          typeof grip === "object"
            ? {
                first: weeks.findIndex((w) => w >= grip.from),
                last: weeks.findIndex((w) => w > grip.to) - 1,
              }
            : null,
        // Which week of the card was taken hold of, so a card grabbed by its
        // second week does not jump a week up under the pointer.
        grab: slot.part,
        hold: Math.min(0.99, Math.max(0, rowSpotAt(e.clientY).into - slot.stack)),
      };
      const start = { x: e.clientX, y: e.clientY };
      const onMove = (ev: PointerEvent) => {
        const p = press.current;
        if (!p) {
          return;
        }
        if (
          !moveRef.current &&
          Math.abs(ev.clientX - start.x) < DRAG_SLOP &&
          Math.abs(ev.clientY - start.y) < DRAG_SLOP
        ) {
          return;
        }
        const spot = rowSpotAt(ev.clientY);
        let row = p.pinned
          ? p.row
          : Math.max(0, Math.min(spot.row - p.grab, weeks.length - p.span));
        if (p.bounds) {
          // Inside its own cycle and no further: the card follows the pointer
          // to the edge of the window and stops there, rather than being
          // dragged out and springing back on release.
          const { first, last } = p.bounds;
          const lo = first < 0 ? 0 : first;
          const hi = last < lo ? lo : Math.min(last, weeks.length - p.span);
          row = Math.max(lo, Math.min(row, hi));
        }
        const next = {
          card: p.card,
          row,
          span: p.span,
          col: columnAt(ev.clientX) ?? col,
          // Where the card's own TOP edge is, not where the cursor is: the
          // reader is placing the card they can see, and it lands between
          // the two cards its edge lies between.
          at: Math.max(0, Math.round(spot.into - p.hold)),
        };
        moveRef.current = next;
        setMove(next);
      };
      arm(onMove, () => {
        const m = moveRef.current;
        press.current = null;
        moveRef.current = null;
        setMove(null);
        if (!m) {
          return;
        }
        const who = people[m.col]?.key ?? NOBODY;
        // Where it came from, so a card merely put back among its own
        // neighbours is a reorder and nothing more.
        const moved = m.row !== slot.row - slot.part || who !== whoOf(card);
        if (moved && grip === "pinned") {
          // Its row is not this board's to set; only the hand it is in
          // changed.
          if (who !== whoOf(card)) {
            assignTo(m.card, who);
          }
        } else if (moved) {
          place(m.card, weeks[m.row], who);
        }
        reorder(m.card, m.row, who, m.at);
      });
    },
    [
      arm,
      rowSpotAt,
      columnAt,
      weeks,
      people,
      place,
      assignTo,
      reorder,
      unlocked,
      accumulating,
    ],
  );

  // Dragging a column header sideways puts the people in the order the
  // reader wants to read them in. The press must not start on the width
  // grip, or sizing a column would shuffle the board instead.
  const colDrag = useRef<{ key: string; x: number; moved: boolean } | null>(null);
  const [dragCol, setDragCol] = useState<string | null>(null);

  const beginColDrag = useCallback(
    (key: string) => (e: React.PointerEvent) => {
      if ((e.target as HTMLElement).closest("button, input, .project-col-resize")) {
        return;
      }
      e.preventDefault();
      (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
      colDrag.current = { key, x: e.clientX, moved: false };
    },
    [],
  );

  const moveColDrag = useCallback(
    (e: React.PointerEvent) => {
      const d = colDrag.current;
      if (!d || (!d.moved && Math.abs(e.clientX - d.x) < DRAG_SLOP)) {
        return;
      }
      d.moved = true;
      setDragCol(d.key);
      const over = columnAt(e.clientX);
      const keys = people.map((p) => p.key);
      const from = keys.indexOf(d.key);
      if (over === null || from < 0 || from === over) {
        return;
      }
      keys.splice(over, 0, ...keys.splice(from, 1));
      setOrder(keys);
    },
    [columnAt, people],
  );

  const endColDrag = useCallback(() => {
    const moved = colDrag.current?.moved;
    colDrag.current = null;
    setDragCol(null);
    if (!moved) {
      return;
    }
    try {
      localStorage.setItem(LS_PEOPLE, JSON.stringify(people.map((p) => p.key)));
    } catch {
      // A browser without storage keeps the order for the session.
    }
  }, [people]);

  const beginStretch = useCallback(
    (card: CardModel, slot: Slot) => (e: React.PointerEvent) => {
      e.preventDefault();
      e.stopPropagation();
      const from = slot.row - slot.part;
      const onMove = (ev: PointerEvent) => {
        const to = Math.max(from, rowAt(ev.clientY));
        const next = { card, to };
        stretchRef.current = next;
        setStretch(next);
      };
      arm(onMove, () => {
        const s = stretchRef.current;
        stretchRef.current = null;
        setStretch(null);
        if (s) {
          stretchTo(card, weeks[s.to]);
        }
      });
    },
    [arm, rowAt, weeks, stretchTo],
  );


  // The card in the reader's hand, however they took hold of it: dragged to
  // another week, or stretched over more of them.
  const held = move?.card.itemId ?? stretch?.card.itemId;

  // How many boxes each cell holds: where the composer stands in it, and how
  // much room the cell keeps free below them.
  const filled = useMemo(() => {
    const out = new Map<string, number>();
    for (const [who, list] of slots) {
      for (const s of list) {
        const key = `${who}/${s.row}`;
        out.set(key, (out.get(key) ?? 0) + 1);
      }
    }
    return out;
  }, [slots]);

  // Starting a card here says three things at once: the week it is for, whose
  // it is, and what kind of work. The team comes from the filter when it says
  // one thing; a card of no team is still the board's to show.
  const create = useCallback(
    (row: number, who: string, title: string, zone: ZoneKey, team: string | null) => {
      const week = weeks[row];
      const now = row === 0;
      const tempId = `tmp-${new Date().toISOString()}`;
      addCard({
        itemId: tempId,
        title,
        assignees: who === NOBODY ? [] : [who],
        zone,
        week,
        team: team ?? undefined,
        // A card started in the row that IS now belongs to today as well; one
        // started in a week ahead waits for its Monday (B1).
        ...(now ? { startDate: today, day: today } : {}),
        createdAt: new Date().toISOString(),
        description: "",
        notes: [],
      } as CardModel);
      const creating = provider.createCard({
        title,
        zone,
        week,
        team,
        assigneeLogin: who === NOBODY ? null : who,
        ...(now ? { start: today, day: today } : {}),
      });
      registerPendingCard(
        tempId,
        creating.then((c) => c.itemId),
      );
      void creating
        .then((card) => replaceCard(tempId, card))
        .catch((err: Error) => {
          removeCard(tempId);
          onError(err.message);
        });
    },
    [weeks, today, provider, addCard, replaceCard, removeCard, onError],
  );

  const deadlines = useMemo(
    () => board.deadlines.filter((d) => weeks.includes(d.week)),
    [board.deadlines, weeks],
  );

  return (
    <div className="triage" ref={grid.wrapRef}>
      <div className="board-toolbar">
        <TeamChips
          label="Team"
          teams={roster}
          selectedKeys={teamFilter}
          onSelect={onSetFilter}
          domains={board.domains}
          noneChip={board.cards.some((c) => !c.team) ? "No team" : undefined}
          canManage={false}
          onAdd={() => {}}
          onRemove={() => {}}
        />
        <button
          type="button"
          className={`triage-lock${unlocked ? " triage-lock-open" : ""}`}
          aria-pressed={unlocked}
          onClick={() => setUnlocked(!unlocked)}
          title={unlocked ? "Project cards unlocked" : "Project cards locked"}
        >
          <Padlock open={unlocked} />
        </button>
        <ZoomControl zoom={grid.zoom} onChange={grid.setZoom} />
      </div>

      <WeekGrid
          grid={grid}
          columns={people}
            earlier={false}
          later="↓ later weeks"
          corner={
            people.some((p) => grid.colFactors[p.key] !== undefined) ? (
              <button
                type="button"
                className="project-cols-reset"
                title="Give the columns on this board the same width again"
                onClick={() => grid.resetColFactors(people.map((p) => p.key))}
              >
                ⇥⇤
              </button>
            ) : null
          }
          head={(p) => (
            <div
              key={p.key}
              className={`project-epic-head triage-person project-epic-head-movable${
                dragCol === p.key ? " project-epic-head-dragging" : ""
              }`}
              title={`${p.key === NOBODY ? "Unassigned" : displayName(p.key, names)} — drag to reorder`}
              onPointerDown={beginColDrag(p.key)}
              onPointerMove={moveColDrag}
              onPointerUp={endColDrag}
              onPointerCancel={endColDrag}
            >
              {p.key === NOBODY ? (
                <span className="triage-person-none">Unassigned</span>
              ) : (
                <>
                  <Avatar login={p.key} avatars={avatars} names={names} />
                  <span className="project-epic-name">{displayName(p.key, names)}</span>
                  {!!carrying[p.key] && (
                    <span
                      className="triage-person-load"
                      title={`${shown[p.key] ?? 0} on this board, ${carrying[p.key]} altogether in every team`}
                    >
                      {shown[p.key] ?? 0}
                      <span className="triage-person-all">/{carrying[p.key]}</span>
                    </span>
                  )}
                </>
              )}
              {/* The border is THIS column's grip: dragging it widens this
                  column alone and remembers the size as a ratio to the rest,
                  so it survives a zoom. Dragged back near the others it lets
                  the ratio go. */}
              <span
                className="project-col-resize"
                title="Drag to size this column · double-click to match the rest"
                {...grid.columnResizer(p.key)}
              />
            </div>
          )}
          cellProps={(p, col, _w, row) => ({
            // Half a card of room under the last one, so there is always
            // somewhere to press.
            style: {
              minHeight: `${
                ((filled.get(`${p.key}/${row}`) ?? 0) +
                  (composing?.row === row && composing.col === col ? 1.5 : 0.5)) *
                grid.rowH
              }px`,
            },
            // On the CLICK, not the press: the form saves itself when a
            // press lands outside it, and opening on the press meant it
            // heard the very one that opened it and closed again at once.
            onClick: () => setComposing({ row, col }),
          })}
          weekProps={(w) => {
            // What the week carries, a card of several weeks counting in each
            // of them.
            const n = load.get(w) ?? 0;
            return {
              title: `${n} cards`,
              label: (
                <>
                  <span className="project-week-date">{w === thisWeek ? "now" : weekLabel(w)}</span>
                  <span className="triage-count">{n}</span>
                </>
              ),
            };
          }}
        >
          {/* While a card of several weeks is being CARRIED — or stretched
              over more of them — the space BETWEEN its boxes is filled in, so
              the reader can see at a glance how much of the plan the thing in
              their hand takes. Not the cells: the gap. Below the first box to
              the end of its week, whole weeks it passes over, and the top of
              the last week down to the last box. At rest, nothing. */}
          {held &&
            people.map((p, col) => {
              const boxes = (slots.get(p.key) ?? [])
                .filter((s) => s.card.itemId === held)
                .sort((a, b) => a.row - b.row);
              if (boxes.length < 2) {
                return null;
              }
              return boxes.map((s, j) => {
                const last = j === boxes.length - 1;
                if (last && s.stack === 0) {
                  return null;
                }
                return (
                  <div
                    key={`${p.key}/${s.card.itemId}/gap${s.row}`}
                    className="triage-span"
                    style={{
                      gridColumn: col + 2,
                      gridRow: s.row + 2,
                      // The first week is filled in below the box; the last
                      // above it; a week passed over entirely, all of it.
                      ...(j === 0 ? { marginTop: `${(s.stack + 1) * grid.rowH}px` } : {}),
                      ...(last ? { height: `${s.stack * grid.rowH}px` } : {}),
                    }}
                    aria-hidden="true"
                  />
                );
              });
            })}

          {people.map((p, col) =>
            (slots.get(p.key) ?? []).map((slot) => {
              const { card, row, part, parts } = slot;
              const done = isDone(card);
              const progress = done ? 100 : (card.progress ?? 0);
              // The stripe says where the card came FROM, and it is the same
              // answer the day boards draw (placements.markOf): a PROJECT card
              // wears the project mark and a PROCESS TURN its process's, since
              // what decides their week is another board and not a zone anyone
              // set here. Every other card wears its zone.
              const mark = markOf(card);
              return (
                <div
                  key={`${p.key}/${card.itemId}/${part}`}
                  className={`project-slot triage-slot${slot.projected ? " triage-slot-coming" : ""}${done ? " project-slot-done" : ""}${
                    card.overdue ? " project-slot-late" : ""
                  }${
                    mark
                      ? ` triage-slot-${mark}`
                      : card.zone
                        ? ` triage-slot-zone-${card.zone}`
                        : ""
                  }${
                    move?.card.itemId === card.itemId ? " project-slot-moving" : ""
                  }`}
                  style={{
                    gridColumn: col + 2,
                    gridRow: row + 2,
                    ...laneStyle(slot, grid.rowFit, grid.rowH),
                  }}
                  title={
                    slot.projected
                      ? `${card.title} — ${card.process} files this turn that week`
                      : parts > 1
                        ? `${card.title} — week ${part + 1} of ${parts}`
                        : card.title
                  }
                  onPointerDown={beginMove(card, slot, col)}
                  onDoubleClick={() => onOpen(card)}
                >
                  <span className="project-slot-title">
                    {card.title}
                    {parts > 1 && <span className="triage-slot-part"> ({part + 1}/{parts})</span>}
                  </span>
                  {!slot.projected &&
                    removableOnTriage(card, unlocked, choicesFor(card)) && (
                    <span className="project-slot-actions">
                      <button
                        type="button"
                        className="card-action card-action-delete"
                        title={card.epic ? "Take off this board" : "Remove"}
                        onClick={(e) => {
                          e.stopPropagation();
                          remove(card);
                        }}
                      >
                        ×
                      </button>
                    </span>
                  )}
                  <span className="triage-slot-bar" aria-label={`${progress}%`}>
                    {/* The stage's own colour, as on Team and Me: a card
                        locked or in review says so by its bar there, and
                        must not say something else here. */}
                    <i style={{ width: `${progress}%`, background: barColor(card.stage) }} />
                  </span>
                  {/* Only a PROJECT card has a span to pull: the weeks it
                      takes are its row on the Project board. Every other card
                      here is one week's work, and stretching one said
                      something about it that nothing else on the board would
                      have said. And only with the catch lifted, since what
                      the grip changes is the Project board's own dates. */}
                  {part === parts - 1 && card.epic && unlocked && !slot.projected && (
                    <div
                      className="project-slot-resize triage-slot-resize"
                      title="Drag down to give the card more weeks"
                      onPointerDown={beginStretch(card, slot)}
                    />
                  )}
                </div>
              );
            }),
          )}

          {composing &&
            (() => {
              const who = people[composing.col]?.key ?? NOBODY;
              const under = filled.get(`${who}/${composing.row}`) ?? 0;
              return (
                <div
                  className="triage-compose"
                  style={{
                    gridColumn: composing.col + 2,
                    gridRow: composing.row + 2,
                    marginTop: `${under * grid.rowH}px`,
                  }}
                >
                  <AddCard
                    autoOpen
                    compact
                    placeholder="Add card"
                    // One team on screen answers the question by itself; with
                    // several the form asks, starting on the first.
                    teams={teams.length > 1 ? teams : undefined}
                    forcedTeam={teams.length > 1 ? undefined : (teams[0] ?? null)}
                    allowNoTeam={false}
                    picker={{
                      title: "Zone",
                      // In your OWN column the zones the plan owns are not
                      // offered: the server refuses work you file for
                      // yourself outside the unplanned one, so offering them
                      // was offering a card that comes straight back as an
                      // error — and "planned" was the default.
                      options: ZONE_ORDER.filter(
                        (z) => who !== me || acceptsNewCard(z),
                      ).map((z) => ({
                        key: z,
                        label: ZONES[z].title,
                        color: ZONES[z].accent,
                        hint: ZONES[z].description,
                      })),
                      initial: who === me ? ADD_ZONE : "gray",
                    }}
                    onCreate={(title, team, zone) => {
                      const fallback = who === me ? ADD_ZONE : "gray";
                      create(composing.row, who, title, (zone || fallback) as ZoneKey, team ?? null);
                    }}
                    onClosed={() => setComposing(null)}
                  />
                </div>
              );
            })()}

          {/* Deadlines: a line at the end of the week they fall in, as on the
              Project board — what stands above it is due by then.
              This board shows every project of the team at once, so a bare
              line said only "something is due" and left the reader to guess
              whose: each carries its project's NAME in the week column, in
              that project's own colour, the way the Project board colours its
              lines when several plans are on screen. Two segments for the
              reason the Project board has two — one element cannot both cross
              the sticky week column and sit behind the cards. */}
          {deadlines.map((d) => {
            const colour = d.project ? teamColor(d.project) : undefined;
            const at = weeks.indexOf(d.week) + 2;
            const label = d.project || "no project";
            return [
              <div
                key={`${d.project}/${d.week}/head`}
                className="project-deadline project-deadline-head triage-deadline triage-deadline-name"
                style={{ gridRow: at, gridColumn: 1 }}
              >
                <span
                  className="triage-deadline-label"
                  style={colour ? { background: colour } : undefined}
                  title={`Deadline of ${label}, end of that week`}
                >
                  {label}
                </span>
              </div>,
              <div
                key={`${d.project}/${d.week}/body`}
                className="project-deadline project-deadline-body triage-deadline"
                style={{
                  gridRow: at,
                  gridColumn: "2 / -2",
                  ...(colour ? { borderTopColor: colour } : {}),
                }}
                title={`Deadline of ${label}, end of that week`}
              />,
            ];
          })}
      </WeekGrid>
      {asking && (
        <RemoveChoiceDialog
          title={asking.title}
          progress={asking.progress ?? 0}
          choices={choicesFor(asking)}
          keepOn={null}
          subtasks={board.cards.filter((c) => c.parent === asking.itemId).length}
          onClose={() => setAsking(null)}
          onSubmit={(choice) => doRemove(asking, choice)}
        />
      )}
    </div>
  );
}

/** Padlock is the catch's own glyph — drawn rather than an emoji, so it takes
 *  the colour of the text around it and reads the same in either theme. */
function Padlock({ open }: { open: boolean }) {
  return (
    <svg
      className="triage-lock-glyph"
      viewBox="0 0 16 16"
      width="12"
      height="12"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      aria-hidden="true"
    >
      <rect x="2.75" y="7.25" width="8.5" height="6" rx="1.5" />
      {open ? <path d="M9 7.25V5a2 2 0 0 1 4 0" /> : <path d="M5 7.25V5a2 2 0 0 1 4 0v2.25" />}
    </svg>
  );
}
