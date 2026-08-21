import { useMemo, useRef, useState } from "react";
import type {
  Board,
  Card as CardModel,
  EpicRef,
  Provider,
} from "../providers/types";
import { addDays, mondayOf, todayIso } from "../date";
import { teamColor, teamInitial } from "../avatar";
import { Dropdown } from "./Dropdown";
import { STAGES } from "../stages";
import { TeamChips } from "./TeamChips";
import { TeamsModal } from "./TeamsModal";

interface ProjectBoardProps {
  board: Board;
  provider: Provider;
  patchCard: (
    itemId: string,
    patch: Partial<CardModel> | ((c: CardModel) => Partial<CardModel>),
  ) => void;
  addCard: (card: CardModel) => void;
  replaceCard: (itemId: string, card: CardModel) => void;
  removeCard: (itemId: string) => void;
  reload: () => void;
  onError: (message: string) => void;
  onOpen: (card: CardModel) => void;
}

/** weeksBetween counts whole weeks from Monday a to Monday b (0 = same week). */
function weeksBetween(a: string, b: string): number {
  const [ay, am, ad] = a.split("-").map(Number);
  const [by, bm, bd] = b.split("-").map(Number);
  return Math.round(
    (Date.UTC(by, bm - 1, bd) - Date.UTC(ay, am - 1, ad)) / (7 * 86400000),
  );
}

/** isoWeekNo is the ISO-8601 week number of a Monday. */
function isoWeekNo(monday: string): number {
  const [y, m, d] = monday.split("-").map(Number);
  const date = new Date(Date.UTC(y, m - 1, d));
  const jan4 = new Date(Date.UTC(date.getUTCFullYear(), 0, 4));
  const week1Mon = new Date(jan4);
  week1Mon.setUTCDate(jan4.getUTCDate() - ((jan4.getUTCDay() + 6) % 7));
  const diff = Math.round((date.getTime() - week1Mon.getTime()) / 86400000);
  if (diff < 0) {
    // The Monday belongs to the previous year's last ISO week.
    const prevJan4 = new Date(Date.UTC(date.getUTCFullYear() - 1, 0, 4));
    const prevW1 = new Date(prevJan4);
    prevW1.setUTCDate(prevJan4.getUTCDate() - ((prevJan4.getUTCDay() + 6) % 7));
    return Math.floor((date.getTime() - prevW1.getTime()) / (7 * 86400000)) + 1;
  }
  return Math.floor(diff / 7) + 1;
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/** One grid row's height in px, and how many weeks each "more" press adds.
 *  ROW_PX must match .project-cell in styles.css — it is what keeps the view
 *  still when rows are prepended. */
const ROW_PX = 28;
const WEEK_STEP = 8;

function weekLabel(monday: string): string {
  const [, m, d] = monday.split("-").map(Number);
  return `${String(d).padStart(2, "0")} ${MONTHS[m - 1]}`;
}

/** colKey is a column's identity: the (project, epic) pair. Epic names repeat
 *  across projects, so a name alone is not a key. */
function colKey(project: string, epic: string): string {
  return `${project}\u0000${epic}`;
}

/** LS_FILTER remembers which project you were looking at. */
const LS_FILTER = "aeman.projectFilter";

function readFilter(): string[] | null {
  try {
    const raw = localStorage.getItem(LS_FILTER);
    const v: unknown = raw ? JSON.parse(raw) : null;
    return Array.isArray(v) && v.every((x) => typeof x === "string") ? v : null;
  } catch {
    return null;
  }
}

/** The Project board: weeks as rows and one project's epics as columns, cards
 *  as slots that may span several weeks (dates start..end). Dragging down an
 *  empty column stretch selects a slot and creates a card in it; assigning a
 *  card to a team (the badge menu) also hands it to that team's weekly plan —
 *  which is how work planned here reaches the people who do it. */
export function ProjectBoard({
  board,
  provider,
  patchCard,
  addCard,
  replaceCard,
  removeCard,
  reload,
  onError,
  onOpen,
}: ProjectBoardProps) {
  const today = todayIso();
  const thisMonday = mondayOf(today);

  // Which project(s) the chips select; null is every project. "" is the chip
  // for columns that belong to no project.
  const [filter, setFilter] = useState<string[] | null>(readFilter);
  const selectFilter = (keys: string[] | null) => {
    setFilter(keys);
    localStorage.setItem(LS_FILTER, JSON.stringify(keys));
  };

  // The columns in view: the selected projects' epics, in board order. Every
  // other derived list follows from this one, so a column and its cards can
  // never disagree about being visible. A column is the (project, name) PAIR —
  // "Docs" exists in every project and they are different columns.
  const epics = useMemo(
    () => board.epics.filter((e) => !filter || filter.includes(e.project)),
    [board.epics, filter],
  );

  // Columns belonging to no project are reachable through a "No project" chip
  // — but only while such a column exists, so the chip never sits there as a
  // permanently empty option.
  const looseEpics = useMemo(
    () => board.epics.some((e) => !e.project),
    [board.epics],
  );

  // Cards on the board: filed under one of the visible columns. Teams do not
  // filter here — a project spans teams, and the badge is where a card is
  // handed to one.
  const cards = useMemo(() => {
    const shown = new Set(epics.map((e) => colKey(e.project, e.name)));
    return board.cards.filter(
      (c) => c.epic && !c.parent && shown.has(colKey(c.project ?? "", c.epic)),
    );
  }, [board.cards, epics]);

  // The week window: two weeks of history before today (or the earliest
  // card), through the latest card plus a quarter of runway to plan into.
  // How far the window reaches beyond the default, in weeks, grown by the
  // buttons at either end — planning is not confined to a fixed horizon.
  const [padBack, setPadBack] = useState(0);
  const [padFwd, setPadFwd] = useState(0);

  const weeks = useMemo(() => {
    let first = addDays(thisMonday, -14 - 7 * padBack);
    let last = addDays(thisMonday, 7 * (8 + padFwd));
    for (const c of cards) {
      const anchor = c.week ? mondayOf(c.week) : null;
      if (anchor && anchor < first) {
        first = anchor;
      }
      const end = c.day ? mondayOf(c.day) : anchor;
      if (end && addDays(end, 7 * 2) > last) {
        last = addDays(end, 7 * 2);
      }
    }
    const out: string[] = [];
    for (let w = first; w <= last; w = addDays(w, 7)) {
      out.push(w);
    }
    return out;
  }, [cards, thisMonday, padBack, padFwd]);

  // Prepending rows would slide the grid under the reader, so the scroll
  // position is moved by exactly the height added.
  const showEarlier = () => {
    const scroller = scrollRef.current;
    setPadBack(padBack + WEEK_STEP);
    if (scroller) {
      requestAnimationFrame(() => {
        scroller.scrollTop += WEEK_STEP * ROW_PX;
      });
    }
  };

  // ---- drag-to-create (and resize): a pressed column stretch becomes a slot.
  const [drag, setDrag] = useState<{
    epic: EpicRef;
    from: number;
    to: number;
    resize?: CardModel;
  } | null>(null);
  const [draft, setDraft] = useState<{
    epic: EpicRef;
    from: number;
    to: number;
  } | null>(null);
  const gridRef = useRef<HTMLDivElement | null>(null);
  // The scrolling ancestor — .project-board, not the grid inside it.
  const scrollRef = useRef<HTMLDivElement | null>(null);

  // A slot being dragged to another week / epic. It follows the pointer as a
  // preview and only writes on release.
  const [move, setMove] = useState<{
    card: CardModel;
    span: number;
    row: number;
    epic: EpicRef;
  } | null>(null);

  const epicAt = (clientX: number): EpicRef | null => {
    const grid = gridRef.current;
    if (!grid) {
      return null;
    }
    const heads = grid.querySelectorAll(".project-epic-head:not(.project-epic-add)");
    for (let i = 0; i < heads.length; i++) {
      const r = heads[i].getBoundingClientRect();
      if (clientX >= r.left && clientX < r.right) {
        return epics[i] ?? null;
      }
    }
    return null;
  };

  const rowAt = (clientY: number): number => {
    const grid = gridRef.current;
    if (!grid) {
      return 0;
    }
    const rows = grid.querySelectorAll(".project-week");
    for (let i = 0; i < rows.length; i++) {
      const r = rows[i].getBoundingClientRect();
      if (clientY < r.bottom) {
        return i;
      }
    }
    return rows.length - 1;
  };

  const beginDrag = (epic: EpicRef, week: number, e: React.PointerEvent, resize?: CardModel) => {
    e.preventDefault();
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    setDrag({ epic, from: week, to: week, resize });
  };

  const moveDrag = (e: React.PointerEvent) => {
    if (!drag) {
      return;
    }
    const row = rowAt(e.clientY);
    if (row !== drag.to) {
      setDrag({ ...drag, to: Math.max(drag.from, row) });
    }
  };

  const endDrag = () => {
    if (!drag) {
      return;
    }
    const { epic, from, to, resize } = drag;
    setDrag(null);
    if (resize) {
      // Stretch (or shrink) an existing slot: its anchor week stays, the end
      // lands on the Friday of the released week.
      const end = addDays(weeks[Math.max(from, to)], 4);
      if (end === resize.day) {
        return;
      }
      const prev = { day: resize.day };
      patchCard(resize.itemId, { day: end });
      void provider
        .patchCard(board, resize.itemId, { dates: { end } })
        .then(addCard)
        .catch((err: unknown) => {
          patchCard(resize.itemId, prev);
          onError(errText(err));
        });
      return;
    }
    setDraft({ epic, from, to });
  };

  const beginMove = (card: CardModel, row: number, span: number, e: React.PointerEvent) => {
    // Only the slot's BODY drags. A press that started on a control inside it
    // (the team badge, delete, the resize grip) must reach that control: the
    // press bubbles up here, and capturing it — plus preventDefault — used to
    // swallow the click entirely, so the badge could never be pressed.
    if ((e.target as HTMLElement).closest("button, input, .project-slot-resize")) {
      return;
    }
    e.preventDefault();
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    setMove({
      card,
      span,
      row,
      epic: { name: card.epic ?? "", project: card.project ?? "" },
    });
  };

  const moveMove = (e: React.PointerEvent) => {
    if (!move) {
      return;
    }
    const row = Math.min(rowAt(e.clientY), weeks.length - 1);
    const epic = epicAt(e.clientX) ?? move.epic;
    if (row !== move.row || colKey(epic.project, epic.name) !== colKey(move.epic.project, move.epic.name)) {
      setMove({ ...move, row, epic });
    }
  };

  const endMove = () => {
    if (!move) {
      return;
    }
    const { card, span, row, epic } = move;
    setMove(null);
    const week = weeks[row];
    const end = addDays(weeks[Math.min(row + span - 1, weeks.length - 1)], 4);
    if (
      week === card.week &&
      colKey(epic.project, epic.name) === colKey(card.project ?? "", card.epic ?? "")
    ) {
      return;
    }
    const prev = {
      week: card.week,
      epic: card.epic,
      project: card.project,
      startDate: card.startDate,
      day: card.day,
    };
    patchCard(card.itemId, {
      week,
      epic: epic.name,
      project: epic.project,
      startDate: week,
      day: end,
    });
    void provider
      .patchCard(board, card.itemId, {
        epic: epic.name,
        project: epic.project,
        plan: { week },
        dates: { start: week, end },
      })
      .then(addCard)
      .catch((err: unknown) => {
        patchCard(card.itemId, prev);
        onError(errText(err));
      });
  };

  const cancelDraft = () => setDraft(null);

  const createSlot = (title: string) => {
    if (!draft || !title.trim()) {
      setDraft(null);
      return;
    }
    const { epic, from, to } = draft;
    setDraft(null);
    const week = weeks[from];
    const start = week;
    const end = addDays(weeks[to], 4); // the Friday of the last week
    const tempId = `tmp-${new Date().toISOString()}`;
    addCard({
      itemId: tempId,
      title: title.trim(),
      isDraft: true,
      assignees: [],
      epic: epic.name,
      project: epic.project,
      week,
      startDate: start,
      day: end,
      description: "",
      progress: 0,
    });
    void provider
      .createCard(board, {
        title: title.trim(),
        epic: epic.name,
        project: epic.project,
        week,
        start,
        day: end,
      })
      .then((c) => replaceCard(tempId, c))
      .catch((err: unknown) => {
        removeCard(tempId);
        onError(errText(err));
      });
  };

  // ---- columns ----------------------------------------------------------
  const [addingEpic, setAddingEpic] = useState(false);
  // The column being renamed in its header, and whether the project manager
  // dialog is open.
  const [renaming, setRenaming] = useState<string | null>(null);
  const [managing, setManaging] = useState(false);
  // The week whose menu is open, and the deadline being dragged to another
  // week (its line follows the pointer and only writes on release).
  const [weekMenu, setWeekMenu] = useState<string | null>(null);
  const weekAnchor = useRef<HTMLElement | null>(null);
  const [dragLine, setDragLine] = useState<{
    project: string;
    from: string;
    row: number;
  } | null>(null);
  const [teamMenu, setTeamMenu] = useState<string | null>(null);
  // Only one team menu is open at a time, so a single anchor ref serves
  // whichever badge was last pressed.
  const teamAnchor = useRef<HTMLButtonElement | null>(null);

  // A column belongs to exactly one project, so adding one is only offered
  // when a single project is in view — otherwise there is no answer to
  // "which project does this column go in".
  const targetProject = filter?.length === 1 && filter[0] ? filter[0] : null;

  const addEpic = (name: string) => {
    setAddingEpic(false);
    if (!name.trim() || !targetProject) {
      return;
    }
    void provider
      .addEpic(board, name.trim(), targetProject)
      .then(reload)
      .catch((err: unknown) => onError(errText(err)));
  };

  const addProject = (name: string) => {
    if (!name.trim()) {
      return;
    }
    void provider
      .addProject(board, name.trim())
      .then(() => {
        selectFilter([name.trim()]);
        reload();
      })
      .catch((err: unknown) => onError(errText(err)));
  };

  const deleteProject = (name: string) => {
    if (!window.confirm(`Delete the project “${name}”?`)) {
      return;
    }
    void provider
      .deleteProject(board, name)
      .then(() => {
        if (filter?.includes(name)) {
          selectFilter(null);
        }
        reload();
      })
      .catch((err: unknown) => onError(errText(err)));
  };

  const deleteEpic = (col: EpicRef) => {
    if (!window.confirm(`Delete the epic “${col.name}”?`)) {
      return;
    }
    void provider
      .deleteEpic(board, col.name, col.project)
      .then(reload)
      .catch((err: unknown) => onError(errText(err)));
  };

  const renameEpic = (col: EpicRef, to: string) => {
    setRenaming(null);
    if (!to.trim() || to.trim() === col.name) {
      return;
    }
    void provider
      .renameEpic(board, col.project, col.name, to.trim())
      .then(reload)
      .catch((err: unknown) => onError(errText(err)));
  };

  const renameProject = (from: string, to: string) => {
    void provider
      .renameProject(board, from, to)
      .then(() => {
        if (filter?.includes(from)) {
          selectFilter(filter.map((p) => (p === from ? to : p)));
        }
        reload();
      })
      .catch((err: unknown) => onError(errText(err)));
  };

  const reorderProjects = (ordered: string[]) => {
    void provider
      .reorderProjects(board, ordered)
      .then(reload)
      .catch((err: unknown) => onError(errText(err)));
  };

  // Assigning a team hands the card to that team's weekly plan: the band is
  // what places it there, so an unbanded card gets the week-end band.
  const assignTeam = (card: CardModel, team: string | null) => {
    setTeamMenu(null);
    const prev = { team: card.team, plan: card.plan };
    patchCard(card.itemId, { team: team ?? undefined, plan: card.plan ?? (team ? "fri" : undefined) });
    void provider
      .patchCard(board, card.itemId, {
        team: team ?? "",
        ...(team && !card.plan ? { plan: { band: "fri", week: card.week ?? "" } } : {}),
      })
      .then(addCard)
      .catch((err: unknown) => {
        patchCard(card.itemId, prev);
        onError(errText(err));
      });
  };

  // A deadline belongs to the project whose plan it is part of, so the week's
  // menu offers one entry per project in view — with several on screen there
  // is no single "the" deadline to set.
  const setDeadline = (week: string, projectName: string, on: boolean) => {
    setWeekMenu(null);
    const call = on
      ? provider.addDeadline(board, week, projectName)
      : provider.deleteDeadline(board, week, projectName);
    void call.then(reload).catch((err: unknown) => onError(errText(err)));
  };

  const beginLineDrag = (
    project: string,
    week: string,
    row: number,
    e: React.PointerEvent,
  ) => {
    e.preventDefault();
    e.stopPropagation();
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    setDragLine({ project, from: week, row });
  };

  const moveLineDrag = (e: React.PointerEvent) => {
    if (!dragLine) {
      return;
    }
    const row = Math.min(rowAt(e.clientY), weeks.length - 1);
    if (row !== dragLine.row) {
      setDragLine({ ...dragLine, row });
    }
  };

  const endLineDrag = () => {
    if (!dragLine) {
      return;
    }
    const { project: dlProject, from, row } = dragLine;
    setDragLine(null);
    const to = weeks[row];
    if (!to || to === from) {
      return;
    }
    void provider
      .moveDeadline(board, dlProject, from, to)
      .then(reload)
      .catch((err: unknown) => onError(errText(err)));
  };

  const deleteCard = (card: CardModel) => {
    if (!window.confirm(`Delete "${card.title}"?`)) {
      return;
    }
    removeCard(card.itemId);
    void provider.deleteCard(board, card.itemId).catch((err: unknown) => {
      onError(errText(err));
      reload();
    });
  };

  // While a slot is being stretched, its span follows the pointer — the drag
  // has to be visible on the thing being dragged, not only on the cursor.
  const previewSpan = (card: CardModel, row: number, span: number): number => {
    if (!drag?.resize || drag.resize.itemId !== card.itemId) {
      return span;
    }
    return Math.max(1, Math.min(drag.to - row + 1, weeks.length - row));
  };

  // Cards per column with their row spans and, when they overlap in time, the
  // lane each one sits in: cards sharing weeks split the column's width
  // between them instead of covering each other up.
  const slots = useMemo(() => {
    type Slot = {
      card: CardModel;
      row: number;
      span: number;
      lane: number;
      lanes: number;
    };
    const byCol = new Map<string, Slot[]>();
    if (weeks.length === 0) {
      return byCol;
    }
    for (const c of cards) {
      if (!c.epic || !c.week) {
        continue;
      }
      const anchor = mondayOf(c.week);
      const row = weeksBetween(weeks[0], anchor);
      if (row < 0 || row >= weeks.length) {
        continue;
      }
      const endMon = c.day && c.day > anchor ? mondayOf(c.day) : anchor;
      const span = Math.max(1, Math.min(weeksBetween(anchor, endMon) + 1, weeks.length - row));
      const k = colKey(c.project ?? "", c.epic);
      const list = byCol.get(k) ?? [];
      list.push({ card: c, row, span, lane: 0, lanes: 1 });
      byCol.set(k, list);
    }
    // Interval partitioning per column: earliest first, each card taking the
    // first lane already free by the week it starts.
    for (const list of byCol.values()) {
      list.sort((a, b) => a.row - b.row || b.span - a.span);
      const laneEnd: number[] = [];
      for (const s of list) {
        let lane = laneEnd.findIndex((end) => end <= s.row);
        if (lane === -1) {
          lane = laneEnd.length;
        }
        laneEnd[lane] = s.row + s.span;
        s.lane = lane;
      }
      for (const s of list) {
        s.lanes = laneEnd.length;
      }
    }
    return byCol;
  }, [cards, weeks]);

  const todayRow = weeks.indexOf(thisMonday);

  // The projects the week menu can act on: those in view, or the whole roster
  // when nothing is filtered. The "no project" chip is not among them — a
  // deadline is part of a plan, so it is always somebody's.
  const menuProjects = (filter ?? board.projects).filter((p) => p !== "");

  // Where a column sits among the visible ones (-1 while it is filtered out).
  const colIndex = (e: EpicRef) =>
    epics.findIndex((x) => colKey(x.project, x.name) === colKey(e.project, e.name));

  // Everything below renders through ONE return: the empty board differs only
  // in its body. An early return here used to leave the shared furniture — the
  // chip row, the manage dialog — out of the empty state, where they are
  // exactly what a person needs to get started.
  const empty = epics.length === 0 && !addingEpic;

  return (
    <div className="project">
      {/* The same toolbar row the other boards wear — the chips are a shared
          control and must not look like a stray line of text here. */}
      <div className="board-toolbar">
        <TeamChips
          label="Project"
          entity="project"
          teams={board.projects}
          selectedKeys={filter}
          onSelect={selectFilter}
          onAdd={addProject}
          onRemove={deleteProject}
          canManage={false}
          onManage={() => setManaging(true)}
          noneChip={looseEpics ? "No project" : undefined}
        />
      </div>
      {empty && (
        <div className="project-empty">
          <p>
            The Project board maps a project&rsquo;s epics (columns) across
            weeks (rows).
          </p>
          {board.projects.length === 0 ? (
            <p className="project-empty-hint">
              Start with a project — “manage” above adds one.
            </p>
          ) : targetProject ? (
            <button
              type="button"
              className="btn btn-primary"
              onClick={() => setAddingEpic(true)}
            >
              + Add the first epic of {targetProject}
            </button>
          ) : (
            <p className="project-empty-hint">
              Pick one project above to add its first epic.
            </p>
          )}
        </div>
      )}
      {!empty && (
      <div className="project-board" ref={scrollRef}>
      <button
        type="button"
        className="project-more"
        onClick={showEarlier}
        title={`Show ${WEEK_STEP} more weeks before`}
      >
        ↑ earlier weeks
      </button>
      <div
        className="project-grid"
        ref={gridRef}
        style={{
          gridTemplateColumns: `66px repeat(${epics.length}, minmax(140px, 1fr)) 34px`,
          gridTemplateRows: `26px repeat(${weeks.length}, 28px)`,
        }}
      >
        {/* header row */}
        <div className="project-corner" />
        {epics.map((e) => {
          const k = colKey(e.project, e.name);
          if (renaming === k) {
            return (
              <div key={k} className="project-epic-head">
                <input
                  type="text"
                  className="project-epic-input"
                  autoFocus
                  defaultValue={e.name}
                  onKeyDown={(ev) => {
                    if (ev.key === "Enter") {
                      renameEpic(e, (ev.target as HTMLInputElement).value);
                    } else if (ev.key === "Escape") {
                      setRenaming(null);
                    }
                  }}
                  onBlur={(ev) => renameEpic(e, ev.target.value)}
                />
              </div>
            );
          }
          return (
            <div
              key={k}
              className="project-epic-head"
              title={`${e.name} — ${e.project || "no project"} · double-click to rename`}
              onDoubleClick={() => setRenaming(k)}
            >
              <span className="project-epic-name">{e.name}</span>
              {/* With several projects on screen the column header alone is
                  ambiguous, so it carries its project — as the same round
                  badge a team wears on a card, not as a second line of text
                  competing with the column's own name. Inside one project the
                  badge would repeat on every column and is left off. */}
              {!targetProject && e.project && (
                <span
                  className="project-epic-avatar"
                  style={{ background: teamColor(e.project) }}
                  title={e.project}
                >
                  {teamInitial(e.project)}
                </span>
              )}
              <button
                type="button"
                className="card-action project-epic-del"
                title="Delete the epic (must be empty)"
                onClick={() => deleteEpic(e)}
              >
                ×
              </button>
            </div>
          );
        })}
        <div className="project-epic-head project-epic-add">
          {addingEpic ? (
            <input
              type="text"
              className="project-epic-input"
              autoFocus
              placeholder={`Epic in ${targetProject ?? ""}…`}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  addEpic((e.target as HTMLInputElement).value);
                } else if (e.key === "Escape") {
                  setAddingEpic(false);
                }
              }}
              onBlur={(e) => addEpic(e.target.value)}
            />
          ) : (
            <button
              type="button"
              className="card-action"
              title={
                targetProject
                  ? `Add an epic to ${targetProject}`
                  : "Pick one project first — a column belongs to a project"
              }
              disabled={!targetProject}
              onClick={() => setAddingEpic(true)}
            >
              +
            </button>
          )}
        </div>

        {/* week label column + row stripes */}
        {weeks.map((w, i) => (
          <div
            key={w}
            className={`project-week${i === todayRow ? " project-week-today" : ""}`}
            style={{ gridRow: i + 2, gridColumn: 1 }}
            title={`ISO week ${isoWeekNo(w)} · click for the deadline`}
            onClick={(ev) => {
              weekAnchor.current = ev.currentTarget;
              setWeekMenu(weekMenu === w ? null : w);
            }}
          >
            <span className="project-week-date">{weekLabel(w)}</span>
            <span className="project-week-no">{isoWeekNo(w)}</span>
          </div>
        ))}

        {/* cells: one per epic × week, the drag surface */}
        {epics.map((e, col) =>
          weeks.map((w, row) => (
            <div
              key={`${colKey(e.project, e.name)}/${w}`}
              className={`project-cell${row === todayRow ? " project-cell-today" : ""}${
                drag &&
                !drag.resize &&
                colKey(drag.epic.project, drag.epic.name) === colKey(e.project, e.name) &&
                row >= drag.from &&
                row <= drag.to
                  ? " project-cell-drag"
                  : ""
              }`}
              style={{ gridRow: row + 2, gridColumn: col + 2 }}
              onPointerDown={(ev) => beginDrag(e, row, ev)}
              onPointerMove={moveDrag}
              onPointerUp={endDrag}
              onPointerCancel={() => setDrag(null)}
            />
          )),
        )}

        {/* deadlines: one line per week, dragged by the dot on its left */}
        {board.deadlines
          .filter((d) => !filter || filter.includes(d.project))
          .map((d) => {
            const dragging =
              dragLine?.from === d.week && dragLine.project === d.project;
            const at = dragging ? dragLine.row : weeks.indexOf(d.week);
            if (at < 0 || at >= weeks.length) {
              return null;
            }
            // Inside one project the line is simply the deadline — plain red.
            // With several projects on screen there are several plans at once,
            // so each line takes its project's colour and steps back, or the
            // grid turns into a stack of red bars nobody can attribute.
            const colour = targetProject
              ? undefined
              : d.project
                ? teamColor(d.project)
                : undefined;
            return (
              <div
                key={`${d.project}\u0000${d.week}`}
                className={`project-deadline${dragging ? " project-deadline-dragging" : ""}${
                  targetProject ? "" : " project-deadline-muted"
                }`}
                // The full width of the row, dates included: the line is the
                // week's edge, and its handle belongs at the very start of it.
                // (Which is why the line outranks the sticky week column — see
                // the z-index scale in the stylesheet.)
                style={{
                  gridRow: at + 2,
                  gridColumn: "1 / -1",
                  ...(colour ? { borderTopColor: colour } : {}),
                }}
              >
                <span
                  className="project-deadline-dot"
                  style={colour ? { background: colour } : undefined}
                  title={`${d.project || "no project"} deadline — drag to another week · ${weekLabel(weeks[at])}`}
                  onPointerDown={(ev) => beginLineDrag(d.project, d.week, at, ev)}
                  onPointerMove={moveLineDrag}
                  onPointerUp={endLineDrag}
                  onPointerCancel={() => setDragLine(null)}
                />
              </div>
            );
          })}

        {/* the create draft */}
        {draft && (
          <div
            className="project-slot project-slot-draft"
            style={{
              gridColumn: colIndex(draft.epic) + 2,
              gridRow: `${draft.from + 2} / span ${draft.to - draft.from + 1}`,
            }}
          >
            <input
              type="text"
              className="project-slot-input"
              autoFocus
              placeholder="New card…"
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  createSlot((e.target as HTMLInputElement).value);
                } else if (e.key === "Escape") {
                  cancelDraft();
                }
              }}
              onBlur={(e) => createSlot(e.target.value)}
            />
          </div>
        )}

        {/* the slots */}
        {epics.map((e, col) =>
          (slots.get(colKey(e.project, e.name)) ?? []).map(({ card, row, span, lane, lanes }) => (
            <div
              key={card.itemId}
              className={`project-slot${card.stage === "done" || (!card.stage && (card.progress ?? 0) >= 100) ? " project-slot-done" : ""}${
                move?.card.itemId === card.itemId ? " project-slot-moving" : ""
              }`}
              style={{
                // While dragged, the slot itself sits where it would land —
                // the preview IS the card, so there is nothing to guess.
                gridColumn:
                  (move?.card.itemId === card.itemId ? colIndex(move.epic) : col) + 2,
                gridRow: `${(move?.card.itemId === card.itemId ? move.row : row) + 2} / span ${previewSpan(card, row, span)}`,
                borderLeftColor: card.team ? teamColor(card.team) : undefined,
                // Cards sharing weeks in one column split its width instead
                // of hiding each other. The width holds while the card is
                // being dragged too: widening it on press made the first click
                // of a double-click visibly inflate the card.
                ...(lanes > 1
                  ? {
                      width: `calc(${100 / lanes}% - 2px)`,
                      marginLeft: `${(100 / lanes) * lane}%`,
                    }
                  : {}),
              }}
              onPointerDown={(ev) => beginMove(card, row, previewSpan(card, row, span), ev)}
              onPointerMove={moveMove}
              onPointerUp={endMove}
              onPointerCancel={() => setMove(null)}
              onDoubleClick={() => onOpen(card)}
              title={card.title}
            >
              <span className="project-slot-title">{card.title}</span>
              <span className="project-slot-actions">
                <button
                  type="button"
                  className="card-action card-action-delete"
                  title="Delete"
                  onClick={(ev) => {
                    ev.stopPropagation();
                    deleteCard(card);
                  }}
                >
                  ×
                </button>
              </span>
              {/* The team badge is the card's owner, not an action: it stays
                  visible once assigned. Unassigned, it is a hover affordance.
                  Delete sits to its LEFT so the badge keeps the slot's edge —
                  it is the thing you point at, and it must not shift when the
                  hover-only actions appear. */}
              <button
                type="button"
                className={`project-slot-team${card.team ? "" : " project-slot-team-empty"}`}
                style={
                  card.team
                    ? { background: teamColor(card.team), color: "#fff" }
                    : undefined
                }
                title={card.team ? `Team: ${card.team} — click to change` : "Assign to a team"}
                onClick={(ev) => {
                  ev.stopPropagation();
                  teamAnchor.current = ev.currentTarget;
                  setTeamMenu(teamMenu === card.itemId ? null : card.itemId);
                }}
              >
                {card.team ? teamInitial(card.team) : "+"}
              </button>
              {card.stage && card.stage !== "done" && (
                <span
                  className="project-slot-stage"
                  style={{ background: STAGES[card.stage].color }}
                  title={STAGES[card.stage].label}
                />
              )}
              <Dropdown
                open={teamMenu === card.itemId}
                anchorRef={teamAnchor}
                onClose={() => setTeamMenu(null)}
                className="card-stage-menu"
              >
                {board.teams.map((t) => (
                  <button
                    key={t}
                    type="button"
                    className={`card-stage-item${card.team === t ? " card-stage-item-active" : ""}`}
                    onClick={() => assignTeam(card, t)}
                  >
                    <span
                      className="card-stage-dot"
                      style={{ background: teamColor(t) }}
                    />
                    {t}
                  </button>
                ))}
                <button
                  type="button"
                  className={`card-stage-item${card.team ? "" : " card-stage-item-active"}`}
                  onClick={() => assignTeam(card, null)}
                >
                  <span className="card-stage-dot card-stage-dot-none" />
                  No team
                </button>
              </Dropdown>
              <div
                className="project-slot-resize"
                title="Drag to stretch over more weeks"
                onPointerDown={(ev) => {
                  ev.stopPropagation();
                  beginDrag(e, row, ev, card);
                }}
                onPointerCancel={() => setDrag(null)}
                onPointerMove={moveDrag}
                onPointerUp={endDrag}
              />
            </div>
          )),
        )}
        </div>
        <button
          type="button"
          className="project-more"
          onClick={() => setPadFwd(padFwd + WEEK_STEP)}
          title={`Show ${WEEK_STEP} more weeks after`}
        >
          ↓ later weeks
        </button>
      </div>
      )}
      <Dropdown
        open={weekMenu !== null}
        anchorRef={weekAnchor}
        onClose={() => setWeekMenu(null)}
        className="card-stage-menu"
      >
        {menuProjects.length === 0 && (
          <span className="card-stage-item project-menu-hint">
            Add a project first — a deadline belongs to a plan
          </span>
        )}
        {menuProjects.map((p) => {
          const has = board.deadlines.some(
            (d) => d.week === weekMenu && d.project === p,
          );
          return (
            <button
              key={p}
              type="button"
              className="card-stage-item"
              onClick={() => setDeadline(weekMenu ?? "", p, !has)}
            >
              <span
                className="card-stage-dot"
                style={{ background: teamColor(p) }}
              />
              {has ? `Remove ${p}'s deadline` : `Set a deadline for ${p}`}
            </button>
          );
        })}
      </Dropdown>
      {managing && (
        <TeamsModal
          teams={board.projects}
          title="Manage projects"
          entity="project"
          onAdd={addProject}
          onRename={renameProject}
          onRemove={deleteProject}
          onReorder={reorderProjects}
          onClose={() => setManaging(false)}
        />
      )}
    </div>
  );
}

function errText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
