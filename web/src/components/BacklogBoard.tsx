// The Backlog board: who does what, and in which week.
//
// The strip on the left holds the cards nobody has given a week — the point
// of the board, and on a board that has never been triaged, most of it. The
// grid on the right is the Project board's shape with a different pair of
// axes: PEOPLE across, WEEKS down. Dragging a card from the strip into a cell
// is the whole gesture — it says who and when at once, which is what triaging
// a card means.
//
// The cards keep the marks they carry everywhere else: the by-Wednesday and
// by-Friday line of the weekly plan, the red of a debt, the stage colours. No
// area of its own is needed to tell planned work from the rest.
//
// A card is STRETCHED by its bottom edge, as a Project-board slot is: the
// reach says the work takes that long, and every week it covers counts it —
// two weeks stretched is two weeks of work, not one filed early. The reach is
// the card's own end date; nothing new is stored for it.
import React, { useCallback, useEffect, useMemo, useState, type ReactNode, type Ref } from "react";
import type { Board, Card as CardModel, Provider } from "../providers/types";
import { addDays, mondayOf, todayIso } from "../date";
import { placedIn, reachOf, weeksCovered } from "../backlog";
import { BACKLOG_WEEKS } from "../viewquery";
import { displayName, type Avatars, type Names } from "../users";
import { Avatar } from "./Avatar";
import { TeamChips } from "./TeamChips";
import { SortableBoard, type BoardGroup, type DropResult } from "./SortableBoard";

type BacklogMeta = { kind: "triage" } | { kind: "cell"; week: string; who: string };

// The column a card with no assignee stands in. An empty login is a real
// value on a card, so the key is something no login can be.
const NOBODY = " nobody";

// A local stand-in for the roster's capacity, so a limit can be tried on a
// team before its file says one: kept per team in this browser only.
const LS_CAPACITY = "aeman.backlog.capacity";

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
  onOpen: (card: CardModel) => void;
  onError: (message: string) => void;
}

function isDone(c: CardModel): boolean {
  return c.stage === "done" || (c.progress ?? 0) >= 100;
}

function whoOf(c: CardModel): string {
  return c.assignees[0] || NOBODY;
}

function weekLabel(monday: string): string {
  const d = (iso: string) => {
    const [, m, day] = iso.split("-");
    return `${Number(day)}.${m}`;
  };
  return `${d(monday)}–${d(addDays(monday, 4))}`;
}

function readCapacityOverrides(): Record<string, number> {
  try {
    const raw = localStorage.getItem(LS_CAPACITY);
    return raw ? (JSON.parse(raw) as Record<string, number>) : {};
  } catch {
    return {};
  }
}

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
  onOpen,
  onError,
}: BacklogBoardProps) {
  const today = todayIso();
  const thisWeek = mondayOf(today);
  // The grid starts at this week: a week gone by is not a row of its own,
  // because what was owed in it and is still open is owed NOW — the same
  // rule the weekly panel draws a debt by (planShowsInWeekAt).
  const weeks = useMemo(
    () => Array.from({ length: BACKLOG_WEEKS }, (_, i) => addDays(thisWeek, 7 * i)),
    [thisWeek],
  );
  const teams = useMemo(() => teamFilter ?? roster, [teamFilter, roster]);

  const [overrides, setOverrides] = useState<Record<string, number>>(readCapacityOverrides);
  useEffect(() => {
    try {
      localStorage.setItem(LS_CAPACITY, JSON.stringify(overrides));
    } catch {
      // A browser without storage keeps the override for the session.
    }
  }, [overrides]);

  // A limit is a TEAM's, and a row here can hold several teams at once: what
  // the board can do in a week is what its teams can do together.
  const weekLimit = useMemo(() => {
    let sum = 0;
    for (const team of teams) {
      sum += overrides[team] ?? board.sprintStates[team]?.capacity?.week ?? 0;
    }
    return sum;
  }, [teams, overrides, board.sprintStates]);

  // Everything the board draws: the cards of the shown teams, split into the
  // ones with a week (a cell) and the ones without (the strip).
  const { cells, strip, people, load, covered } = useMemo(() => {
    const cells = new Map<string, CardModel[]>();
    // What each week carries (a stretched card counts in every week it
    // covers) and which cells a stretch reaches INTO, so they can say so.
    const load = new Map<string, number>();
    const covered = new Set<string>();
    const strip: CardModel[] = [];
    const seen = new Set<string>();
    const last = weeks[weeks.length - 1];
    for (const c of board.cards) {
      if (c.parent || (c.domain ?? "").startsWith("~") || isDone(c)) {
        continue;
      }
      if (!teams.includes(c.team ?? "")) {
        continue;
      }
      const week = placedIn(c);
      if (!week) {
        strip.push(c);
        seen.add(whoOf(c));
        continue;
      }
      // A debt stands in THIS week: an open card owed in a week gone by is
      // owed now, and this is where a person meets it.
      const row = week < thisWeek ? thisWeek : week;
      if (row > last) {
        continue;
      }
      const who = whoOf(c);
      seen.add(who);
      const key = cellKey(row, who);
      cells.set(key, [...(cells.get(key) ?? []), c]);
      for (const w of weeksCovered(c)) {
        const at = w < thisWeek ? thisWeek : w;
        load.set(at, (load.get(at) ?? 0) + 1);
        if (at !== row) {
          covered.add(cellKey(at, who));
        }
      }
    }
    // Nobody's column stands first: it is where a card waits for an owner.
    const named = [...seen].filter((w) => w !== NOBODY).sort();
    return { cells, strip, people: [NOBODY, ...named], load, covered };
  }, [board.cards, teams, weeks, thisWeek]);

  const groups = useMemo<BoardGroup<BacklogMeta>[]>(() => {
    const out: BoardGroup<BacklogMeta>[] = [
      { key: "triage", meta: { kind: "triage" }, cards: strip },
    ];
    for (const week of weeks) {
      for (const who of people) {
        out.push({
          key: cellKey(week, who),
          meta: { kind: "cell", week, who },
          cards: cells.get(cellKey(week, who)) ?? [],
        });
      }
    }
    return out;
  }, [weeks, people, cells, strip]);

  const handleDrop = useCallback(
    ({ card, fromMeta, toMeta }: DropResult<BacklogMeta>) => {
      const before = {
        week: card.week,
        assignees: card.assignees,
        backlogWeek: card.backlogWeek,
        triage: card.triage,
        startDate: card.startDate,
        day: card.day,
        sprintStart: card.sprintStart,
      };
      const fail = (err: Error) => {
        patchCard(card.itemId, before);
        onError(err.message);
      };

      if (toMeta.kind === "triage") {
        if (fromMeta.kind === "triage") {
          return;
        }
        patchCard(card.itemId, { week: undefined, backlogWeek: undefined, triage: true });
        provider.untriageCard(card.itemId).then(addCard).catch(fail);
        return;
      }
      if (
        fromMeta.kind === "cell" &&
        fromMeta.week === toMeta.week &&
        fromMeta.who === toMeta.who
      ) {
        return; // ordering inside a cell is not kept yet
      }

      const who = toMeta.who === NOBODY ? [] : [toMeta.who];
      patchCard(card.itemId, (c) => ({
        week: toMeta.week,
        backlogWeek: toMeta.week,
        triage: false,
        assignees: who,
        // A card placed in a week ahead leaves the day board until that week
        // begins — the server clears its dates, and the row shows that at
        // once. A slot keeps them: its dates ARE its row.
        ...(toMeta.week > thisWeek && !c.epic
          ? { startDate: undefined, day: undefined, sprintStart: undefined }
          : {}),
      }));
      // Who first, then when.
      const assign =
        whoOf(card) === toMeta.who
          ? Promise.resolve(undefined)
          : provider.patchCard(card.itemId, { assignees: who });
      void assign
        .then(() => provider.placeCard(card.itemId, toMeta.week))
        .then(addCard)
        .catch(fail);
    },
    [provider, patchCard, addCard, onError, thisWeek],
  );

  // Stretching: the card's end date moves to the Friday of the week the
  // pointer let go over. The dates ARE the reach — the same two fields a slot
  // uses — so nothing new is stored for it.
  const stretchTo = useCallback(
    (card: CardModel, week: string) => {
      if (!card.week || week < card.week) {
        return;
      }
      const end = week === card.week ? "" : addDays(week, 4);
      if ((card.day ?? "") === end) {
        return;
      }
      const before = { day: card.day };
      patchCard(card.itemId, { day: end || undefined });
      void provider
        .patchCard(card.itemId, { dates: { start: card.startDate ?? "", end } })
        .then(addCard)
        .catch((err: Error) => {
          patchCard(card.itemId, before);
          onError(err.message);
        });
    },
    [provider, patchCard, addCard, onError],
  );

  const renderCard = (card: CardModel): ReactNode => (
    <BacklogCard
      card={card}
      avatars={avatars}
      names={names}
      onOpen={onOpen}
      onStretch={card.week ? (week) => stretchTo(card, week) : undefined}
    />
  );

  const renderGroup = (
    group: BoardGroup<BacklogMeta>,
    body: ReactNode,
    { isOver, dropRef }: { isOver: boolean; dropRef: Ref<HTMLElement> },
  ): ReactNode => {
    if (group.meta.kind === "triage") {
      return (
        <div
          key={group.key}
          ref={dropRef as Ref<HTMLDivElement>}
          className={`backlog-triage-cards${isOver ? " backlog-dragover" : ""}`}
        >
          {body}
          {group.cards.length === 0 && (
            <span className="backlog-empty">nothing waiting for a week</span>
          )}
        </div>
      );
    }
    const { week, who } = group.meta;
    const reached = covered.has(cellKey(week, who));
    return (
      <div
        key={group.key}
        ref={dropRef as Ref<HTMLDivElement>}
        data-week={week}
        className={`backlog-cell${week === thisWeek ? " backlog-cell-now" : ""}${
          reached ? " backlog-cell-reached" : ""
        }${isOver ? " backlog-dragover" : ""}`}
        style={{ gridRow: weeks.indexOf(week) + 2, gridColumn: people.indexOf(who) + 2 }}
      >
        {body}
      </div>
    );
  };

  // The grid: a header row of people, a label column of weeks, and the cells
  // the groups render into.
  const renderLayout = (nodes: Map<string, ReactNode>): ReactNode => (
    <div className="backlog-body">
      <aside className="backlog-triage">
        <div className="backlog-triage-head">
          needs triage
          <span className="backlog-count">{strip.length}</span>
        </div>
        {nodes.get("triage")}
      </aside>

      <div className="backlog-scroll">
        <div
          className="backlog-grid"
          style={{
            gridTemplateColumns: `96px repeat(${people.length}, minmax(210px, 1fr))`,
            gridTemplateRows: `auto repeat(${weeks.length}, minmax(90px, auto))`,
          }}
        >
          <div className="backlog-corner" style={{ gridRow: 1, gridColumn: 1 }} />
          {people.map((who, i) => (
            <div key={who} className="backlog-person" style={{ gridRow: 1, gridColumn: i + 2 }}>
              {who === NOBODY ? (
                <span className="backlog-person-none">Unassigned</span>
              ) : (
                <>
                  <Avatar login={who} avatars={avatars} names={names} />
                  <span className="backlog-person-name">{displayName(who, names)}</span>
                </>
              )}
            </div>
          ))}

          {weeks.map((week, i) => {
            // What the week carries, stretched cards included: a card over
            // two weeks is a week of work in each of them.
            const n = load.get(week) ?? 0;
            const over = weekLimit > 0 && n > weekLimit;
            return (
              <div
                key={week}
                className={`backlog-week${week === thisWeek ? " backlog-week-now" : ""}${
                  over ? " backlog-week-over" : ""
                }`}
                style={{ gridRow: i + 2, gridColumn: 1 }}
                title={
                  weekLimit > 0
                    ? `${n} cards; the teams on screen close about ${weekLimit} a week`
                    : `${n} cards`
                }
              >
                <span className="backlog-week-date">
                  {week === thisWeek ? "this week" : weekLabel(week)}
                </span>
                <span className="backlog-count">
                  {n}
                  {weekLimit > 0 && <span className="backlog-limit"> / {weekLimit}</span>}
                </span>
              </div>
            );
          })}

          {weeks.map((week) => people.map((who) => nodes.get(cellKey(week, who))))}

          {/* Deadlines: a line under the week they fall in, as on the Project
              board — what stands above it is what has to be done by then. */}
          {board.deadlines
            .filter((d) => weeks.includes(d.week))
            .map((d) => (
              <div
                key={`${d.project}/${d.week}`}
                className="backlog-deadline"
                style={{ gridRow: weeks.indexOf(d.week) + 2, gridColumn: "1 / -1" }}
                title={`Deadline of ${d.project}, end of ${weekLabel(d.week)}`}
              >
                <span className="backlog-deadline-label">⚑ {d.project}</span>
              </div>
            ))}
        </div>
      </div>
    </div>
  );

  return (
    <div className="backlog">
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
          className="backlog-capacity"
          title="How many cards the teams on screen close in a week. Click to try a number of your own; it stays in this browser."
          onClick={() => {
            const team = teams[0] ?? "";
            const raw = window.prompt(
              `Cards a week for ${team || "the no-team group"} (empty = the derived number)`,
              String(overrides[team] ?? board.sprintStates[team]?.capacity?.week ?? ""),
            );
            if (raw === null) {
              return;
            }
            setOverrides((cur) => {
              const next = { ...cur };
              const n = Number(raw);
              if (raw.trim() === "" || !Number.isFinite(n) || n <= 0) {
                delete next[team];
              } else {
                next[team] = Math.round(n);
              }
              return next;
            });
          }}
        >
          {weekLimit > 0 ? `${weekLimit} / week` : "no limit"}
        </button>
        <span className="backlog-load">{strip.length} untriaged</span>
      </div>
      <SortableBoard<BacklogMeta>
        groups={groups}
        onDrop={handleDrop}
        renderCard={renderCard}
        renderOverlay={(card) => (
          <BacklogCard card={card} avatars={avatars} names={names} onOpen={() => {}} />
        )}
        renderGroup={renderGroup}
        renderLayout={renderLayout}
      />
    </div>
  );
}

function cellKey(week: string, who: string): string {
  return `cell::${week}::${who}`;
}

interface BacklogCardProps {
  card: CardModel;
  avatars: Avatars;
  names: Names;
  onOpen: (card: CardModel) => void;
  /** Stretch the card to the week the pointer lets go over; absent on a card
   *  that stands in no week (the strip), which has nothing to stretch. */
  onStretch?: (week: string) => void;
}

// A compact card: the title, what it belongs to, how far it is. The person is
// the column it stands in, so the avatar is drawn only in the strip, where
// there are no columns yet.
function BacklogCard({ card, avatars, names, onOpen, onStretch }: BacklogCardProps) {
  const done = isDone(card);
  const progress = done ? 100 : (card.progress ?? 0);
  const who = card.assignees[0];
  const span = weeksCovered(card).length;
  // The grip: pointer down on the card's bottom edge, and the week let go
  // over is the new reach. The cells carry their own week (data-week), so the
  // gesture asks the board under the pointer rather than measuring rows.
  const [reaching, setReaching] = useState<string | null>(null);
  const weekUnder = (x: number, y: number): string | undefined =>
    (document.elementFromPoint(x, y)?.closest(".backlog-cell") as HTMLElement | null)?.dataset
      .week;
  const beginStretch = (e: React.PointerEvent) => {
    if (!onStretch) {
      return;
    }
    e.preventDefault();
    e.stopPropagation();
    const move = (ev: PointerEvent) => setReaching(weekUnder(ev.clientX, ev.clientY) ?? null);
    const up = (ev: PointerEvent) => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      setReaching(null);
      const week = weekUnder(ev.clientX, ev.clientY);
      if (week) {
        onStretch(week);
      }
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };
  const reach = reaching ?? reachOf(card);
  const reachSpan = reaching
    ? weeksCovered({ week: card.week, day: addDays(reaching, 4) }).length
    : span;
  return (
    <div
      className={`backlog-card${done ? " backlog-card-done" : ""}${card.overdue ? " backlog-card-late" : ""}${card.stage ? ` backlog-card-${card.stage}` : ""}${card.plan ? ` backlog-card-plan-${card.plan}` : ""}${reaching ? " backlog-card-reaching" : ""}`}
      onClick={() => onOpen(card)}
      title={reach && reachSpan > 1 ? `${card.title} — ${reachSpan} weeks` : card.title}
    >
      <div className="backlog-card-row">
        <span className="backlog-card-title">{card.title}</span>
        {!card.week && who && (
          <Avatar
            login={who}
            avatars={avatars}
            names={names}
            className="avatar-img backlog-card-avatar"
          />
        )}
      </div>
      <div className="backlog-card-row backlog-card-meta">
        {card.epic ? (
          <span
            className="backlog-card-chip"
            title={card.project ? `${card.project} · ${card.epic}` : card.epic}
          >
            {card.epic}
          </span>
        ) : card.project ? (
          <span className="backlog-card-chip">{card.project}</span>
        ) : card.task ? (
          <span className="backlog-card-chip">process</span>
        ) : null}
        {card.reviewOf && <span className="backlog-card-chip">review</span>}
        {reachSpan > 1 && <span className="backlog-card-span">{reachSpan} wk</span>}
        <span className="backlog-card-bar" aria-label={`${progress}%`}>
          <i style={{ width: `${progress}%` }} />
        </span>
      </div>
      {onStretch && (
        <span
          className="backlog-card-grip"
          title="Drag down to stretch the card over more weeks"
          onPointerDown={beginStretch}
          onClick={(e) => e.stopPropagation()}
        />
      )}
    </div>
  );
}
