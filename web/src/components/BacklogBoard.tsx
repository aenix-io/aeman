// The Backlog board: the weekly plan seen several weeks ahead, with a limit
// on each week (docs/design/backlog.md). Columns are weeks, rows are teams,
// each column has three lanes by the source of the work, and a strip above
// the columns holds what nobody placed yet. A card is placed by dragging it
// into a lane of a week; the server decides what that does to it (Place).
import { useCallback, useEffect, useMemo, useState, type CSSProperties, type ReactNode, type Ref } from "react";
import type { Board, Card as CardModel, Lane, Provider } from "../providers/types";
import { addDays, mondayOf, todayIso } from "../date";
import { BACKLOG_WEEKS } from "../viewquery";
import type { Avatars, Names } from "../users";
import { Avatar } from "./Avatar";
import { TeamChips } from "./TeamChips";
import { SortableBoard, type BoardGroup, type DropResult } from "./SortableBoard";

type BacklogMeta =
  | { kind: "triage"; team: string }
  | { kind: "cell"; team: string; week: string; lane: Lane | "" };

// The lanes are drawn as the day boards' zone areas, and two of them borrow a
// zone's colour because they mean the same thing there: inbound is what a day
// board calls unplanned, the plan is planned. Internal has no counterpart —
// green would read as "when there is time", the very fate its floor exists to
// prevent — so it carries a blue of its own.
const LANES: { key: Lane; spine: string; accent: string; background: string }[] = [
  { key: "client", spine: "CLIENT", accent: "var(--zone-yellow-accent)", background: "var(--zone-yellow-bg)" },
  { key: "plan", spine: "PLAN", accent: "var(--zone-gray-accent)", background: "var(--zone-gray-bg)" },
  { key: "internal", spine: "INTERNAL", accent: "var(--lane-internal-accent)", background: "var(--lane-internal-bg)" },
];

// The default lane shares, in percent of the week, for a team that set none
// — the same numbers the server fills in.
const DEFAULT_CLIENT_SHARE = 30;
const DEFAULT_INTERNAL_SHARE = 10;

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

// laneDerives mirrors board.LaneDerives: a lane on such a card is refused,
// so a drop into a lane sends the week alone.
function laneDerives(c: CardModel): boolean {
  return !!c.epic || !!c.task || !!c.parent || !!c.reviewOf;
}

function weekLabel(monday: string): string {
  const d = (iso: string) => {
    const [, m, day] = iso.split("-");
    return `${Number(day)}.${m}`;
  };
  return `${d(monday)} – ${d(addDays(monday, 4))}`;
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
  const from = mondayOf(today);
  const weeks = useMemo(
    () => Array.from({ length: BACKLOG_WEEKS }, (_, i) => addDays(from, 7 * i)),
    [from],
  );
  const teams = teamFilter ?? roster;

  const [overrides, setOverrides] = useState<Record<string, number>>(readCapacityOverrides);
  useEffect(() => {
    try {
      localStorage.setItem(LS_CAPACITY, JSON.stringify(overrides));
    } catch {
      // A browser without storage keeps the override for the session.
    }
  }, [overrides]);

  const capacityOf = useCallback(
    (team: string) => {
      const st = board.sprintStates[team]?.capacity;
      const week = overrides[team] ?? st?.week ?? 0;
      return {
        week,
        client: st?.client || DEFAULT_CLIENT_SHARE,
        internal: st?.internal || DEFAULT_INTERNAL_SHARE,
        derived: overrides[team] === undefined && !!st?.derived,
        local: overrides[team] !== undefined,
      };
    },
    [board.sprintStates, overrides],
  );

  // Every card of the shown teams that stands somewhere on this board: in
  // the strip (triage) or in a column (backlogWeek). Subtasks ride their
  // parent and personal cards are on no team board.
  const placed = useMemo(() => {
    const strip = new Map<string, CardModel[]>();
    const cells = new Map<string, CardModel[]>();
    for (const c of board.cards) {
      if (c.parent || (c.domain ?? "").startsWith("~")) {
        continue;
      }
      const team = c.team ?? "";
      if (!teams.includes(team)) {
        continue;
      }
      if (c.triage) {
        strip.set(team, [...(strip.get(team) ?? []), c]);
        continue;
      }
      const w = c.backlogWeek;
      if (!w) {
        continue;
      }
      const col = w < from ? from : w; // a debt stands in the current column
      if (col > weeks[weeks.length - 1]) {
        continue;
      }
      const key = cellKey(team, col, c.lane ?? "");
      cells.set(key, [...(cells.get(key) ?? []), c]);
    }
    return { strip, cells };
  }, [board.cards, teams, from, weeks]);

  const groups = useMemo<BoardGroup<BacklogMeta>[]>(() => {
    const out: BoardGroup<BacklogMeta>[] = [];
    for (const team of teams) {
      out.push({ key: `triage::${team}`, meta: { kind: "triage", team }, cards: placed.strip.get(team) ?? [] });
      for (const week of weeks) {
        for (const lane of LANES.map((l) => l.key)) {
          out.push({
            key: cellKey(team, week, lane),
            meta: { kind: "cell", team, week, lane },
            cards: placed.cells.get(cellKey(team, week, lane)) ?? [],
          });
        }
      }
    }
    return out;
  }, [teams, weeks, placed]);

  const handleDrop = useCallback(
    ({ card, fromMeta, toMeta }: DropResult<BacklogMeta>) => {
      if (toMeta.kind === "cell" && toMeta.lane === "") {
        return; // the lane-less shelf is not a place
      }
      if (toMeta.kind === "triage") {
        if (fromMeta.kind === "triage") {
          return;
        }
        const before = { week: card.week, plan: card.plan, backlogWeek: card.backlogWeek, triage: card.triage };
        patchCard(card.itemId, { week: undefined, plan: undefined, backlogWeek: undefined, triage: true });
        provider
          .untriageCard(card.itemId)
          .then(addCard)
          .catch((err: Error) => {
            patchCard(card.itemId, before);
            onError(err.message);
          });
        return;
      }
      const same =
        fromMeta.kind === "cell" && fromMeta.week === toMeta.week && fromMeta.lane === toMeta.lane;
      if (same) {
        return; // ordering inside a cell is not kept yet
      }
      const lane: Lane | undefined = laneDerives(card) || toMeta.lane === "" ? undefined : toMeta.lane;
      const before = {
        week: card.week,
        plan: card.plan,
        lane: card.lane,
        backlogWeek: card.backlogWeek,
        triage: card.triage,
        startDate: card.startDate,
        day: card.day,
        sprintStart: card.sprintStart,
      };
      patchCard(card.itemId, (c) => ({
        week: toMeta.week,
        plan: c.plan ?? "fri",
        lane: lane ?? c.lane,
        backlogWeek: toMeta.week,
        triage: false,
        ...(toMeta.week > from && !c.epic
          ? { startDate: undefined, day: undefined, sprintStart: undefined }
          : {}),
      }));
      provider
        .placeCard(card.itemId, toMeta.week, lane)
        .then(addCard)
        .catch((err: Error) => {
          patchCard(card.itemId, before);
          onError(err.message);
        });
    },
    [provider, patchCard, addCard, onError, from],
  );

  const renderCard = (card: CardModel): ReactNode => (
    <BacklogCard card={card} avatars={avatars} names={names} onOpen={onOpen} />
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
          className={`backlog-strip-cards${isOver ? " backlog-dragover" : ""}`}
        >
          {body}
          {group.cards.length === 0 && <span className="backlog-empty">nothing to triage</span>}
        </div>
      );
    }
    const { team, week, lane } = group.meta;
    if (lane === "") {
      return null; // no area of its own — see the group build
    }
    const cap = capacityOf(team);
    const def = LANES.find((l) => l.key === lane) ?? LANES[0];
    const n = group.cards.length;
    const share = lane === "client" ? cap.client : lane === "internal" ? cap.internal : 0;
    const limit = cap.week > 0 && share > 0 ? Math.round((cap.week * share) / 100) : 0;
    let tone = "";
    if (cap.week > 0 && lane === "client" && n > limit) {
      tone = " backlog-lane-over";
    }
    if (cap.week > 0 && lane === "internal" && week === from && n < limit) {
      tone = " backlog-lane-starved";
    }
    return (
      <section
        key={group.key}
        ref={dropRef as Ref<HTMLElement>}
        className={`zone-area backlog-lane${tone}${isOver ? " zone-area-dragover" : ""}`}
        style={
          {
            background: def.background,
            borderLeftColor: def.accent,
            "--zone-accent": def.accent,
          } as CSSProperties
        }
      >
        <span className="zone-spine">{def.spine}</span>
        <div className="zone-cards">
          {(n > 0 || limit > 0) && (
            <div className="backlog-lane-head">
              <span className="backlog-count">
                {n}
                {limit > 0 && (
                  <span className="backlog-limit">
                    {lane === "internal" ? " ≥ " : " ≤ "}
                    {limit}
                  </span>
                )}
              </span>
            </div>
          )}
          {body}
        </div>
      </section>
    );
  };

  const renderLayout = (nodes: Map<string, ReactNode>): ReactNode => (
    <div className="backlog-rows">
      {teams.map((team) => {
        const cap = capacityOf(team);
        const strip = placed.strip.get(team) ?? [];
        const openPlaced = board.cards.filter(
          (c) =>
            (c.team ?? "") === team && !c.parent && !!c.backlogWeek && !c.triage && !isDone(c),
        ).length;
        const deadlines = board.deadlines.filter((d) => d.week >= from && d.week <= weeks[weeks.length - 1]);
        return (
          <section key={team} className="backlog-row">
            <header className="backlog-row-head">
              <h3 className="backlog-team">{team || "No team"}</h3>
              <button
                type="button"
                className="backlog-capacity"
                title="Capacity: cards a week. Click to set a local number to try a limit with."
                onClick={() => {
                  const raw = window.prompt(
                    `Cards a week for ${team || "the no-team group"} (empty = the roster's / derived)`,
                    cap.week ? String(cap.week) : "",
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
                {cap.week > 0 ? `${cap.week} / week` : "no limit"}
                {cap.derived && <span className="backlog-note"> derived</span>}
                {cap.local && <span className="backlog-note"> local</span>}
              </button>
              <span className="backlog-load">
                {cap.week > 0 ? `booked ${(openPlaced / cap.week).toFixed(1)} wk` : `${openPlaced} placed`}
                {" · "}
                {strip.length} untriaged
              </span>
            </header>
            <div className="backlog-strip">
              <div className="backlog-strip-head">
                needs triage
                <span className="backlog-count">{strip.length}</span>
              </div>
              {nodes.get(`triage::${team}`)}
            </div>
            <div className="backlog-weeks">
              {weeks.map((week) => {
                const laned = LANES.reduce(
                  (n, l) => n + (placed.cells.get(cellKey(team, week, l.key))?.length ?? 0),
                  0,
                );
                // Counted, not drawn: a card of this week that nobody gave a
                // lane still spends the week's capacity.
                const noLane = placed.cells.get(cellKey(team, week, ""))?.length ?? 0;
                const total = laned + noLane;
                const over = cap.week > 0 && total > cap.week;
                const lines = deadlines.filter((d) => d.week === week);
                const breached = lines.filter((d) =>
                  board.cards.some(
                    (c) =>
                      (c.team ?? "") === team &&
                      c.project === d.project &&
                      !!c.backlogWeek &&
                      c.backlogWeek > d.week &&
                      !isDone(c),
                  ),
                );
                return (
                  <div key={week} className="backlog-slot">
                    <div
                      className={`backlog-col${week === from ? " backlog-col-now" : ""}${over ? " backlog-col-over" : ""}`}
                    >
                      <div className="backlog-col-head">
                        <span className="backlog-col-week">
                          {week === from ? "this week" : weekLabel(week)}
                        </span>
                        <span className="backlog-count">
                          {total}
                          {noLane > 0 && (
                            <span
                              className="backlog-nolane"
                              title={`${noLane} of them carry no lane and are not shown here — they are counted all the same`}
                            >
                              {" "}
                              ({noLane} no lane)
                            </span>
                          )}
                          {cap.week > 0 && <span className="backlog-limit"> / {cap.week}</span>}
                        </span>
                      </div>
                      {LANES.map((l) => nodes.get(cellKey(team, week, l.key)))}
                    </div>
                    {/* The gap after the column carries the deadlines that
                        fall in its week: the line stands between the weeks,
                        where a card on the far side of it is past it. It is
                        as tall as the week it closes — the slot's height —
                        and never longer. */}
                    <div className={`backlog-gap${lines.length ? " backlog-gap-deadline" : ""}`}>
                      {lines.map((d) => (
                        <div
                          key={d.project}
                          className={`backlog-deadline${breached.includes(d) ? " backlog-deadline-breached" : ""}`}
                          title={`Deadline of ${d.project} — end of the week of ${weekLabel(d.week)}`}
                        >
                          <span className="backlog-deadline-label">⚑ {d.project}</span>
                        </div>
                      ))}
                    </div>
                  </div>
                );
              })}
            </div>
          </section>
        );
      })}
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
        scrollClassName="backlog-scroll"
      />
    </div>
  );
}

function cellKey(team: string, week: string, lane: Lane | ""): string {
  return `cell::${team}::${week}::${lane}`;
}

interface BacklogCardProps {
  card: CardModel;
  avatars: Avatars;
  names: Names;
  onOpen: (card: CardModel) => void;
}

// A compact card: the title, who has it, what it belongs to, how far it is.
// Everything else — the body, the notes, the dates — is one click away in
// the detail pane.
function BacklogCard({ card, avatars, names, onOpen }: BacklogCardProps) {
  const done = isDone(card);
  const progress = done ? 100 : (card.progress ?? 0);
  const who = card.assignees[0];
  return (
    <div
      className={`backlog-card${done ? " backlog-card-done" : ""}${card.overdue ? " backlog-card-late" : ""}${card.stage ? ` backlog-card-${card.stage}` : ""}`}
      onClick={() => onOpen(card)}
      title={card.title}
    >
      <div className="backlog-card-row">
        <span className="backlog-card-title">{card.title}</span>
        {who && (
          <Avatar login={who} avatars={avatars} names={names} className="avatar-img backlog-card-avatar" />
        )}
      </div>
      <div className="backlog-card-row backlog-card-meta">
        {card.epic ? (
          <span className="backlog-card-chip" title={card.project ? `${card.project} · ${card.epic}` : card.epic}>
            {card.epic}
          </span>
        ) : card.project ? (
          <span className="backlog-card-chip">{card.project}</span>
        ) : card.task ? (
          <span className="backlog-card-chip">process</span>
        ) : null}
        {card.reviewOf && <span className="backlog-card-chip">review</span>}
        <span className="backlog-card-bar" aria-label={`${progress}%`}>
          <i style={{ width: `${progress}%` }} />
        </span>
      </div>
    </div>
  );
}
