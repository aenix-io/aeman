import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import type {
  Board,
  ProcessInfo,
  ProcessTask,
  Provider,
  TaskInput,
} from "../providers/types";
import { teamColor } from "../avatar";
import { displayName, type Avatars, type Names } from "../users";
import { Avatar } from "./Avatar";
import { declareDomain, offerableTeams } from "../domains";
import { nameConflict } from "../names";
import { DomainSelect, blurredIntoDomainSelect } from "./DomainSelect";
import { Dropdown } from "./Dropdown";
import { ProjectPicker } from "./ProjectPicker";
import { TeamChips } from "./TeamChips";

interface ProcessBoardProps {
  board: Board;
  provider: Provider;
  /** The project chips' selection, shared with the Project tab. */
  filter: string[] | null;
  onSetFilter: (keys: string[] | null) => void;
  /** Opens the PROJECT manager — the chips' "manage" is about projects here
   *  exactly as it is on the Project tab. */
  onManageProjects: () => void;
  /** Avatars by login (the board roster), as the Me and Team boards have them. */
  avatars: Avatars;
  /** Display names by login (the board roster); a login without one is shown
   *  as is. */
  names: Names;
  onError: (message: string) => void;
}

/** The cycles a task may run on, in the order a person would list them. */
const CYCLES: { key: string; label: string }[] = [
  { key: "week", label: "every week" },
  { key: "2weeks", label: "every two weeks" },
  { key: "month", label: "every month" },
  { key: "quarter", label: "every quarter" },
];

function cycleLabel(key: string): string {
  return CYCLES.find((c) => c.key === key)?.label ?? key;
}

/** The Process tab: recurring work the team keeps doing — each process expands
 *  into the tasks it iterates on, and each task shows how its last
 *  iterations went. Processes belong to projects and the chips are the same
 *  ones the Project tab has; the selection is shared. */
export function ProcessBoard({
  board,
  provider,
  filter,
  onSetFilter,
  onManageProjects,
  avatars,
  names,
  onError,
}: ProcessBoardProps) {
  // The processes are part of the board, loaded with it and refreshed by the
  // Board watch frame — the server's cache is the one source, and a write
  // here shows up the same way a teammate's does: through the watch, with no
  // reload of anything.
  const processes = board.processes;
  const [open, setOpen] = useState<Set<string>>(() => new Set());
  // The task being created (keyed by process) or edited (by uid), the
  // process being named, and the one being renamed.
  const [adding, setAdding] = useState<string | null>(null);
  const [editing, setEditing] = useState<string | null>(null);
  // The project a new process is being named for (null = not naming one).
  const [addingProcess, setAddingProcess] = useState<string | null>(null);
  // The repository a new process is declared in ("" = the selector's default);
  // only a multi-domain board offers the choice.
  const [newDomain, setNewDomain] = useState("");
  const [renaming, setRenaming] = useState<string | null>(null);
  // The "which project?" menu behind + add a process, for when the answer is
  // not already one chip.
  const [addMenu, setAddMenu] = useState(false);
  const addAnchor = useRef<HTMLElement | null>(null);

  // Dragging, the way the Project board drags its columns: a press arms it,
  // movement past a slop makes it a drag, and the list previews the order
  // live — the preview IS what a drop will persist.
  const DRAG_SLOP = 5;
  // A process being dragged among processes: the previewed name order.
  const procDrag = useRef<{ name: string; y: number; moved: boolean } | null>(null);
  const [procOrder, setProcOrderState] = useState<string[] | null>(null);
  // Mirrored in refs: the drag's window listeners are the closures of the
  // PRESS render, and reading state through them would read the past.
  const procOrderRef = useRef<string[] | null>(null);
  const setProcOrder = (v: string[] | null) => {
    procOrderRef.current = v;
    setProcOrderState(v);
  };
  // A task being dragged: where it currently previews (process + index).
  const taskDrag = useRef<{
    uid: string;
    fromProcess: string;
    y: number;
    moved: boolean;
  } | null>(null);
  const [taskPreview, setTaskPreviewState] = useState<{
    uid: string;
    process: string;
    index: number;
  } | null>(null);
  const taskPreviewRef = useRef<typeof taskPreview>(null);
  const setTaskPreview = (v: typeof taskPreview) => {
    taskPreviewRef.current = v;
    setTaskPreviewState(v);
  };
  const shownRef = useRef<ProcessInfo[]>([]);
  // Tasks whose create is still in flight, shown at once under the process
  // they belong to. A task create is two synchronous writes upstream (the
  // task, then the turn it owes this week) and takes seconds; waiting for
  // them with nothing on screen reads as a click that did nothing.
  const [pendingTasks, setPendingTasks] = useState<
    { process: string; task: ProcessTask }[]
  >([]);

  const shown = useMemo(() => {
    let list = processes.filter((p) => !filter || filter.includes(p.project));
    if (procOrder) {
      const by = new Map(list.map((p) => [p.name, p]));
      const ordered = procOrder.map((n) => by.get(n)).filter((p): p is ProcessInfo => !!p);
      const rest = list.filter((p) => !procOrder.includes(p.name));
      list = [...ordered, ...rest];
    }
    if (taskPreview) {
      // The dragged task shows at its previewed place — lifted out of its
      // process and inserted where the pointer says, mid-list included.
      let dragged: ProcessTask | undefined;
      for (const p of list) {
        dragged = dragged ?? p.tasks.find((t) => t.uid === taskPreview.uid);
      }
      if (dragged) {
        list = list.map((p) => {
          const without = p.tasks.filter((t) => t.uid !== taskPreview.uid);
          if (p.name !== taskPreview.process) {
            return without.length === p.tasks.length ? p : { ...p, tasks: without };
          }
          const at = Math.max(0, Math.min(taskPreview.index, without.length));
          return { ...p, tasks: [...without.slice(0, at), dragged!, ...without.slice(at)] };
        });
      }
    }
    shownRef.current = list;
    return list;
  }, [processes, filter, procOrder, taskPreview]);
  const targetProject = filter?.length === 1 ? filter[0] : null;
  const looseProcesses = processes.some((p) => !p.project);
  const fail = (err: unknown) => onError(errText(err));

  const allOpen = shown.length > 0 && shown.every((p) => open.has(p.name));
  const toggleAll = () =>
    setOpen(allOpen ? new Set() : new Set(shown.map((p) => p.name)));

  const toggle = (name: string) =>
    setOpen((cur) => {
      const next = new Set(cur);
      if (next.has(name)) {
        next.delete(name);
      } else {
        next.add(name);
      }
      return next;
    });

  // Every write below returns as soon as the server's cache has it; the
  // Board frame that follows repaints this tab (and every other open one).
  const addProcess = (name: string, project: string) => {
    setAddingProcess(null);
    const n = name.trim();
    if (!n) {
      return;
    }
    const conflict = nameConflict("process", board.processes.map((p) => p.name), n);
    if (conflict) {
      fail(new Error(conflict));
      return;
    }
    setOpen((cur) => new Set(cur).add(n));
    void provider
      .addProcess(n, project, declareDomain(board.domains, newDomain))
      .catch(fail);
  };

  // Where a new process would go: the one chip in view answers it; otherwise
  // the button asks, the way the week menu asks which project a deadline is
  // for. The no-project bucket is offered in both cases — a process without a
  // project exists and has to be creatable.
  const menuProjects = filter ?? [...board.projects, ""];
  const beginAdd = (ev: React.MouseEvent) => {
    if (targetProject !== null) {
      setAddingProcess(targetProject);
      return;
    }
    addAnchor.current = ev.currentTarget as HTMLElement;
    setAddMenu(true);
  };

  const setProcessProject = (name: string, to: string) => {
    void provider.setProcessProject(name, to).catch(fail);
  };

  const setPaused = (name: string, paused: boolean) => {
    void provider.setProcessPaused(name, paused).catch(fail);
  };

  // ---- dragging processes ------------------------------------------------
  const beginProcDrag = (p: ProcessInfo, e: React.PointerEvent) => {
    if ((e.target as HTMLElement).closest("button, input, .process-cell")) {
      return;
    }
    e.preventDefault();
    procDrag.current = { name: p.name, y: e.clientY, moved: false };
    const onMove = (ev: PointerEvent) => moveProcDrag(ev);
    const onUp = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      window.removeEventListener("pointercancel", onCancel);
      endProcDrag();
    };
    const onCancel = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      window.removeEventListener("pointercancel", onCancel);
      procDrag.current = null;
      setProcOrder(null);
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    window.addEventListener("pointercancel", onCancel);
  };
  const moveProcDrag = (e: PointerEvent) => {
    const d = procDrag.current;
    if (!d) {
      return;
    }
    if (!d.moved && Math.abs(e.clientY - d.y) < DRAG_SLOP) {
      return;
    }
    d.moved = true;
    // Which process the pointer is over decides the previewed position. The
    // list is read through a ref: this closure was made at press time.
    const heads = [...document.querySelectorAll(".process-item")];
    const names = (procOrderRef.current ?? shownRef.current.map((p) => p.name));
    let to = names.indexOf(d.name);
    heads.forEach((h, i) => {
      const r = h.getBoundingClientRect();
      if (e.clientY >= r.top && e.clientY <= r.bottom && i < names.length) {
        to = i;
      }
    });
    const from = names.indexOf(d.name);
    if (to !== from && to >= 0) {
      const next = [...names];
      next.splice(from, 1);
      next.splice(to, 0, d.name);
      setProcOrder(next);
    }
  };
  const endProcDrag = () => {
    const d = procDrag.current;
    procDrag.current = null;
    if (!d?.moved) {
      return;
    }
    const order = procOrderRef.current;
    setProcOrder(null);
    if (!order) {
      return;
    }
    // The full roster in board order, with the shown block reordered in
    // place: the server applies exactly what the preview showed.
    const rest = processes.map((p) => p.name).filter((n) => !order.includes(n));
    void provider.reorderProcesses([...order, ...rest]).catch(fail);
  };

  // ---- dragging tasks ----------------------------------------------------
  const beginTaskDrag = (p: ProcessInfo, task: ProcessTask, e: React.PointerEvent) => {
    if ((e.target as HTMLElement).closest("button, input, .process-cell, label")) {
      return;
    }
    // Listeners on the WINDOW, not pointer capture on the row: the preview
    // remounts the dragged row under its target process, and a removed
    // element takes its capture — and the rest of the gesture — with it.
    e.preventDefault();
    taskDrag.current = { uid: task.uid, fromProcess: p.name, y: e.clientY, moved: false };
    const onMove = (ev: PointerEvent) => moveTaskDrag(ev);
    const onUp = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      window.removeEventListener("pointercancel", onCancel);
      endTaskDrag();
    };
    const onCancel = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      window.removeEventListener("pointercancel", onCancel);
      taskDrag.current = null;
      setTaskPreview(null);
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    window.addEventListener("pointercancel", onCancel);
  };
  const moveTaskDrag = (e: PointerEvent) => {
    const d = taskDrag.current;
    if (!d) {
      return;
    }
    if (!d.moved && Math.abs(e.clientY - d.y) < DRAG_SLOP) {
      return;
    }
    d.moved = true;
    // The open process body under the pointer is the target; the row index
    // within it comes from the row midpoints. A collapsed process cannot
    // receive (there is nowhere to show the preview), same as Me/Team where
    // a drop needs a visible list.
    const items = [...document.querySelectorAll(".process-item")];
    const list = shownRef.current;
    for (let i = 0; i < items.length && i < list.length; i++) {
      const body = items[i].querySelector(".process-body");
      if (!body) {
        continue;
      }
      const br = body.getBoundingClientRect();
      if (e.clientY < br.top || e.clientY > br.bottom) {
        continue;
      }
      const rows = [...body.querySelectorAll(".process-task")];
      let index = rows.length;
      for (let j = 0; j < rows.length; j++) {
        const rr = rows[j].getBoundingClientRect();
        if (e.clientY < rr.top + rr.height / 2) {
          index = j;
          break;
        }
      }
      const cur = taskPreviewRef.current;
      if (!cur || cur.process !== list[i].name || cur.index !== index) {
        setTaskPreview({ uid: d.uid, process: list[i].name, index });
      }
      return;
    }
  };
  const endTaskDrag = () => {
    const d = taskDrag.current;
    taskDrag.current = null;
    if (!d?.moved) {
      return;
    }
    const preview = taskPreviewRef.current;
    setTaskPreview(null);
    if (!preview) {
      return;
    }
    // The target process's final uid order — the dragged task included at its
    // previewed index — is the whole instruction; a cross-process move is the
    // same call, the server adopts the stranger.
    const target = shownRef.current.find((p) => p.name === preview.process);
    if (!target) {
      return;
    }
    void provider
      .reorderProcessTasks(preview.process, target.tasks.map((t) => t.uid))
      .catch(fail);
  };

  // Ticking off a turn from here is the same act as on the Project board: the
  // stage carries it, and reopening puts the card back in progress rather
  // than to zero — the work that was done was still done.
  const setTurnDone = (iteration: { uid: string; state: string }, done: boolean) => {
    // Reopening moves the progress, not the stage: a turn stays recurrent for
    // as long as it exists — its task owns the repeat — and "in progress"
    // would strip the marker the service now refuses to let go.
    const call = done
      ? provider.patchCard(iteration.uid, { stage: "done" })
      : provider.patchCard(iteration.uid, { progress: 90 });
    void call.catch(fail);
  };
  const deleteProcess = (name: string) => {
    if (!window.confirm(`Delete the process “${name}”?`)) {
      return;
    }
    void provider.deleteProcess(name).catch(fail);
  };
  const renameProcess = (from: string, to: string) => {
    setRenaming(null);
    const n = to.trim();
    if (!n || n === from) {
      return;
    }
    const conflict = nameConflict("process", board.processes.map((p) => p.name), n, from);
    if (conflict) {
      fail(new Error(conflict));
      return;
    }
    void provider.renameProcess(from, n).catch(fail);
  };

  const saveTask = (process: string, uid: string | null, input: TaskInput) => {
    setAdding(null);
    setEditing(null);
    if (uid) {
      void provider.updateTask(uid, input).catch(fail);
      return;
    }
    const optimistic: ProcessTask = {
      uid: `tmp-${process}-${input.title ?? ""}`,
      title: input.title ?? "",
      description: input.description,
      recurrence: input.recurrence ?? "week",
      start: input.start,
      team: input.team,
      assignee: input.assignee,
      accumulate: input.accumulate,
      history: [],
    };
    setPendingTasks((cur) => [...cur, { process, task: optimistic }]);
    const forget = () =>
      setPendingTasks((cur) => cur.filter((x) => x.task.uid !== optimistic.uid));
    void provider
      .addTask(process, input)
      .then(forget)
      .catch((err: unknown) => {
        forget();
        fail(err);
      });
  };
  const deleteTask = (t: ProcessTask) => {
    if (!window.confirm(`Delete the task “${t.title}”? Its past iterations stay.`)) {
      return;
    }
    void provider.deleteTask(t.uid).catch(fail);
  };

  return (
    <div className="process">
      <div className="board-toolbar">
        <TeamChips
          label="Project"
          entity="project"
          teams={board.projects}
          selectedKeys={filter}
          onSelect={onSetFilter}
          onAdd={() => undefined}
          onRemove={() => undefined}
          canManage={false}
          onManage={onManageProjects}
          noneChip={looseProcesses ? "No project" : undefined}
        />
        {shown.length > 0 && (
          <button
            type="button"
            className="add-card process-toggle-all"
            title={allOpen ? "Collapse every process" : "Expand every process"}
            onClick={toggleAll}
          >
            {allOpen ? "collapse all" : "expand all"}
          </button>
        )}
      </div>

      <div className="process-list">
        {/* The empty state stays on screen while the first process is being
            named — the field is part of it, as on the Project tab. Hiding it
            the moment naming began sent the field to the list's own add row
            at the bottom, below nothing. */}
        {shown.length === 0 && (
          <div className="project-empty">
            <p>
              A process is recurring work the team keeps doing — publishing
              articles, collecting payment — and wants to see itself doing.
            </p>
            {addingProcess !== null ? (
              <div className="process-new-row">
                <DomainSelect domains={board.domains} value={newDomain} onChange={setNewDomain} />
                <input
                  type="text"
                  className="add-card-input project-empty-input"
                  autoFocus
                  placeholder={
                    addingProcess ? `Process in ${addingProcess}…` : "Process with no project…"
                  }
                  onKeyDown={(ev) => {
                    if (ev.key === "Enter") {
                      addProcess((ev.target as HTMLInputElement).value, addingProcess);
                    } else if (ev.key === "Escape") {
                      setAddingProcess(null);
                    }
                  }}
                  onBlur={(ev) => {
                    if (!blurredIntoDomainSelect(ev)) {
                      addProcess(ev.target.value, addingProcess);
                    }
                  }}
                />
              </div>
            ) : (
              <button type="button" className="btn btn-primary" onClick={beginAdd}>
                + Add the first process{targetProject ? ` of ${targetProject}` : ""}
              </button>
            )}
          </div>
        )}
        {shown.map((p) => (
          <section key={p.name} className={`process-item${p.paused ? " process-item-paused" : ""}`}>
            {/* No double-click here: opening and closing a process is two
                clicks in a row, and the browser reads that as one — the
                process kept sliding into rename. The pencil renames. */}
            <header
              className={`process-head${procDrag.current?.name === p.name && procOrder ? " process-head-dragging" : ""}`}
              onClick={() => toggle(p.name)}
              onPointerDown={(e) => beginProcDrag(p, e)}
              title="Click to expand · drag to reorder"
            >
              <span className="process-caret">{open.has(p.name) ? "▾" : "▸"}</span>
              {renaming === p.name ? (
                <input
                  type="text"
                  className="add-card-input process-name-input"
                  autoFocus
                  defaultValue={p.name}
                  onClick={(e) => e.stopPropagation()}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      renameProcess(p.name, (e.target as HTMLInputElement).value);
                    } else if (e.key === "Escape") {
                      setRenaming(null);
                    }
                  }}
                  onBlur={(e) => renameProcess(p.name, e.target.value)}
                />
              ) : (
                <span className="process-name">{p.name}</span>
              )}
              <ProjectPicker
                current={p.project}
                projects={board.projects}
                entity="process"
                onPick={(to) => setProcessProject(p.name, to)}
              />
              <span className="process-count">
                {p.tasks.length} {p.tasks.length === 1 ? "task" : "tasks"}
              </span>
              {p.paused && <span className="process-paused-tag">paused</span>}
              <button
                type="button"
                className="card-action process-rename"
                title="Rename the process"
                onClick={(e) => {
                  e.stopPropagation();
                  setRenaming(p.name);
                }}
              >
                ✎
              </button>
              <Health tasks={p.tasks} />
              {/* Work that stops for a month is not work that was deleted: a
                  paused process files nothing and keeps everything. */}
              <button
                type="button"
                className="card-action process-pause"
                title={
                  p.paused
                    ? "Paused — files no cards. Click to resume; the current week gets what it is owed."
                    : "Pause: file no cards until resumed. Tasks and history are kept."
                }
                onClick={(e) => {
                  e.stopPropagation();
                  setPaused(p.name, !p.paused);
                }}
              >
                {p.paused ? (
                  <svg viewBox="0 0 12 12" aria-hidden="true">
                    <path d="M3 1.6 L10 6 L3 10.4 Z" />
                  </svg>
                ) : (
                  <svg viewBox="0 0 12 12" aria-hidden="true">
                    <rect x="2.5" y="1.6" width="2.6" height="8.8" rx="0.8" />
                    <rect x="6.9" y="1.6" width="2.6" height="8.8" rx="0.8" />
                  </svg>
                )}
              </button>
              <button
                type="button"
                className="card-action card-action-delete process-del"
                title={p.tasks.length ? "Delete its tasks first" : "Delete the process"}
                disabled={p.tasks.length > 0}
                onClick={(e) => {
                  e.stopPropagation();
                  deleteProcess(p.name);
                }}
              >
                ×
              </button>
            </header>

            {open.has(p.name) && (
              <div className="process-body">
                {[
                  ...p.tasks,
                  // …and the ones still being written upstream.
                  ...pendingTasks
                    .filter(
                      (x) =>
                        x.process === p.name &&
                        !p.tasks.some((t) => t.title === x.task.title),
                    )
                    .map((x) => x.task),
                ].map((t) =>
                  editing === t.uid ? (
                    <TaskForm
                      key={t.uid}
                      board={board}
                      project={p.project ?? ""}
                      initial={t}
                      onSave={(input) => saveTask(p.name, t.uid, input)}
                      onCancel={() => setEditing(null)}
                    />
                  ) : (
                    <TaskRow
                      key={t.uid}
                      pending={t.uid.startsWith("tmp-")}
                      dragging={taskPreview?.uid === t.uid}
                      onDragStart={(e) => beginTaskDrag(p, t, e)}
                      task={t}
                      teams={board.teams}
                      members={board.members.map((m) => m.login)}
                      avatars={avatars}
                      names={names}
                      onSetRecurrence={(recurrence) => saveTask(p.name, t.uid, { recurrence })}
                      onSetStart={(start) => saveTask(p.name, t.uid, { start })}
                      onSetTeam={(team) => saveTask(p.name, t.uid, { team })}
                      onSetAssignee={(assignee) => saveTask(p.name, t.uid, { assignee })}
                      onSetAccumulate={(accumulate) => saveTask(p.name, t.uid, { accumulate })}
                      onSetDone={setTurnDone}
                      onEdit={() => setEditing(t.uid)}
                      onDelete={() => deleteTask(t)}
                    />
                  ),
                )}
                {adding === p.name ? (
                  <TaskForm
                    board={board}
                    project={p.project ?? ""}
                    onSave={(input) => saveTask(p.name, null, input)}
                    onCancel={() => setAdding(null)}
                  />
                ) : (
                  <button
                    type="button"
                    className="add-card process-add"
                    title="Add a task — what an iteration is copied from"
                    onClick={() => setAdding(p.name)}
                  >
                    + add
                  </button>
                )}
              </div>
            )}
          </section>
        ))}
        {/* A new process, named in place — the way a column is on Project.
            It needs a single project in view: that is what it belongs to. */}
        {shown.length > 0 &&
          (addingProcess !== null ? (
            <div className="process-new-row">
              <DomainSelect domains={board.domains} value={newDomain} onChange={setNewDomain} />
              <input
                type="text"
                className="add-card-input process-new"
                autoFocus
                placeholder={
                  addingProcess ? `New process in ${addingProcess}…` : "New process in no project…"
                }
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    addProcess((e.target as HTMLInputElement).value, addingProcess);
                  } else if (e.key === "Escape") {
                    setAddingProcess(null);
                  }
                }}
                onBlur={(e) => {
                  if (!blurredIntoDomainSelect(e)) {
                    addProcess(e.target.value, addingProcess);
                  }
                }}
              />
            </div>
          ) : (
            <button type="button" className="add-card process-new" onClick={beginAdd}>
              + add a process
            </button>
          ))}
      </div>

      <Dropdown
        open={addMenu}
        anchorRef={addAnchor}
        onClose={() => setAddMenu(false)}
        className="card-stage-menu"
      >
        {menuProjects.map((pr) => (
          <button
            key={pr || "\u0000none"}
            type="button"
            className="card-stage-item"
            onClick={() => {
              setAddMenu(false);
              setAddingProcess(pr);
            }}
          >
            <span
              className="card-stage-dot"
              style={{ background: pr ? teamColor(pr) : "var(--line)" }}
            />
            {pr ? `New process in ${pr}` : "New process in no project"}
          </button>
        ))}
      </Dropdown>
    </div>
  );
}

/** How many turns the dots show. A process that has run for a year has fifty
 *  of them and the row would become a smear: the recent ones are the ones
 *  that say whether it is alive, and the count carries the rest. */
const SHOWN_ITERATIONS = 10;

/** Health is the row of dots: one per iteration, how it went. Given a way to
 *  set the current turn's state, the row becomes the control for it: the dot
 *  you are looking at is the turn you want to tick off. */
function Health({
  tasks,
  onSetDone,
}: {
  tasks: ProcessTask[];
  onSetDone?: (iteration: { uid: string; state: string }, done: boolean) => void;
}) {
  // Sorted by week: a process's row gathers several tasks, and their
  // turns interleave in time rather than in the order the tasks were
  // added — an unsorted row would read as a history that never happened.
  const all = tasks
    .flatMap((t) => t.history)
    .slice()
    .sort((a, b) => a.week.localeCompare(b.week));
  if (all.length === 0) {
    return <span className="process-health process-health-empty">no iterations yet</span>;
  }
  const recent = all.slice(-SHOWN_ITERATIONS);
  // The server sends the recent turns and counts the rest, so the numbers
  // hold even for a process that has been running for years.
  const total = tasks.reduce((n, t) => n + (t.turns ?? t.history.length), 0);
  const hidden = total - recent.length;
  const done = tasks.reduce((n, t) => n + (t.done ?? 0), 0);
  const late = tasks.reduce((n, t) => n + (t.late ?? 0), 0);
  const current = all[all.length - 1];
  const summary = `${done} of ${total} turns done${late ? `, ${late} overdue` : ""}${
    hidden > 0 ? ` · showing the last ${recent.length}` : ""
  }`;
  const dots = (
    <>
      {hidden > 0 && <span className="process-health-more">+{hidden}</span>}
      {recent.map((i) => (
        <span key={i.uid} className={`process-dot process-dot-${i.state}`} title={`${i.week}: ${i.state}`} />
      ))}
    </>
  );
  if (!onSetDone || !current) {
    return (
      <span className="process-health" title={summary}>
        {dots}
      </span>
    );
  }
  return (
    <CellPicker
      className="process-health process-health-pick"
      title={summary}
      label={dots}
      options={
        current.state === "done"
          ? [{ key: "open", active: false, label: <>Reopen this turn</> }]
          : [{ key: "done", active: false, label: <>Mark this turn done</> }]
      }
      onPick={(key) => onSetDone(current, key === "done")}
    />
  );
}

function TaskRow({
  task: t,
  pending,
  dragging,
  onDragStart,
  teams,
  members,
  avatars,
  names,
  onSetRecurrence,
  onSetStart,
  onSetTeam,
  onSetAssignee,
  onSetAccumulate,
  onSetDone,
  onEdit,
  onDelete,
}: {
  task: ProcessTask;
  /** Its create is still in flight: shown, but not yet something to act on. */
  pending?: boolean;
  dragging?: boolean;
  onDragStart?: (e: React.PointerEvent) => void;
  teams: string[];
  members: string[];
  avatars: Avatars;
  names: Names;
  onSetRecurrence: (recurrence: string) => void;
  onSetStart: (start: string) => void;
  onSetTeam: (team: string) => void;
  onSetAssignee: (assignee: string) => void;
  onSetAccumulate: (accumulate: boolean) => void;
  onSetDone: (iteration: { uid: string; state: string }, done: boolean) => void;
  onEdit: () => void;
  onDelete: () => void;
}) {
  return (
    <div
      className={`process-task${pending ? " process-task-pending" : ""}${dragging ? " process-task-dragging" : ""}`}
      onPointerDown={onDragStart}
    >
      {/* The title opens the task on a double-click, the way a card does:
          the pencil is for finding it, this is for using it. */}
      <div
        className="process-task-main"
        title="Double-click to edit"
        onDoubleClick={onEdit}
      >
        <span className="process-task-title">{t.title}</span>
        {t.description && (
          <span className="process-task-desc">{t.description}</span>
        )}
      </div>
      <span className="process-task-meta">
        {/* One control for one subject: how often the work comes round, and
            what a turn does when the last one is unfinished. The stack marks
            a task that accumulates; a plain cycle says nothing, because
            not accumulating is what every process does by default. */}
        <CellPicker
          className="process-cell-cycle"
          title="How often this repeats"
          label={
            <>
              {cycleLabel(t.recurrence)}
              {t.accumulate && (
                <span className="process-accum" title="Missed turns pile up">
                  <StackIcon />
                </span>
              )}
            </>
          }
          options={CYCLES.map((c) => ({
            key: c.key,
            active: c.key === t.recurrence,
            label: <>{c.label}</>,
          }))}
          onPick={onSetRecurrence}
          extra={
            <button
              type="button"
              className={`card-stage-item process-accum-item${t.accumulate ? " card-stage-item-active" : ""}`}
              title={
                t.accumulate
                  ? "The next card is filed even while this one is open, so missed turns pile up"
                  : "An open card holds the next one back and goes overdue"
              }
              onClick={(e) => {
                e.stopPropagation();
                onSetAccumulate(!t.accumulate);
              }}
            >
              <StackIcon />
              Pile up missed turns
            </button>
          }
        />
        {/* The anchor the cycle is counted from. A date typed into this
            field moves every future due date with it; the turn already
            running is left alone. */}
        <label className="process-cell process-cell-from" title="The day the cycle is counted from — click to change">
          <span>{t.start ? `from ${t.start}` : "no start"}</span>
          <input
            type="date"
            className="process-from-input"
            value={t.start ?? ""}
            onClick={(e) => {
              e.stopPropagation();
              // Clicking a date input only FOCUSES it; the picker opens from
              // the calendar icon, which is invisible here (the input is an
              // opacity-0 overlay over the text). Ask for the picker
              // outright, or the click does nothing a person can see.
              try {
                (e.target as HTMLInputElement).showPicker();
              } catch {
                // Some browsers gate showPicker (or lack it); focus still
                // lets the keyboard edit the date.
              }
            }}
            onChange={(e) => {
              if (e.target.value && e.target.value !== t.start) {
                onSetStart(e.target.value);
              }
            }}
          />
        </label>
        <span className="process-task-who">
          {/* Team and owner are set where they are read — the cell IS the
              control, as a card's badges are. The form behind ✎ is for the
              title, the body and the cycle. */}
          <CellPicker
            className="process-cell-team"
            title="Which team the iterations land in"
            label={
              t.team ? (
                <>
                  <span className="team-dot" style={{ background: teamColor(t.team) }} />
                  {t.team}
                </>
              ) : (
                <span className="process-task-none">no team</span>
              )
            }
            options={[
              ...teams
                .filter((x) => x !== "")
                .map((x) => ({
                  key: x,
                  active: x === t.team,
                  label: (
                    <>
                      <span className="card-stage-dot" style={{ background: teamColor(x) }} />
                      {x}
                    </>
                  ),
                })),
              {
                key: "",
                active: !t.team,
                label: (
                  <>
                    <span className="card-stage-dot" style={{ background: "var(--line)" }} />
                    No team
                  </>
                ),
              },
            ]}
            onPick={onSetTeam}
          />
          <CellPicker
            className="process-cell-owner"
            title={
              t.assignee
                ? `Assigned to ${displayName(t.assignee, names)} — click to change`
                : "Nobody owns the iterations — click to assign"
            }
            label={
              t.assignee ? (
                <>
                  <Avatar
                    login={t.assignee}
                    avatars={avatars}
                    names={names}
                    className="avatar-img process-owner-avatar"
                  />
                  {/* The name alone in the row — it is in the tooltip and in
                      the menu too, and it is the half that gets cut. */}
                  {displayName(t.assignee, names)}
                </>
              ) : (
                <span className="process-task-none">nobody</span>
              )
            }
            options={[
              ...members.map((m) => ({
                key: m,
                active: m === t.assignee,
                label: (
                  <>
                    <Avatar login={m} avatars={avatars} names={names} />
                    {displayName(m, names)}
                  </>
                ),
              })),
              { key: "", active: !t.assignee, label: <>Nobody</> },
            ]}
            onPick={onSetAssignee}
          />
        </span>
      </span>
      <Health tasks={[t]} onSetDone={onSetDone} />
      <span className="process-task-actions">
        <button type="button" className="card-action" title="Edit the task" onClick={onEdit}>
          ✎
        </button>
        <button type="button" className="card-action card-action-delete" title="Delete the task" onClick={onDelete}>
          ×
        </button>
      </span>
    </div>
  );
}

/** CellPicker turns a cell of the row into the control that sets it: the value
 *  is the trigger, the menu is the choices. */
function CellPicker({
  className,
  title,
  label,
  options,
  onPick,
  extra,
}: {
  className: string;
  title: string;
  label: ReactNode;
  options: { key: string; label: ReactNode; active: boolean }[];
  onPick: (key: string) => void;
  /** Rendered under the options, for a second question about the same
   *  subject — the cycle's menu carries its accumulate toggle there. */
  extra?: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const anchor = useRef<HTMLElement | null>(null);
  return (
    <>
      <button
        type="button"
        className={`process-cell ${className}`}
        title={title}
        ref={(el) => {
          anchor.current = el;
        }}
        onClick={(e) => {
          e.stopPropagation();
          setOpen((v) => !v);
        }}
      >
        {label}
      </button>
      <Dropdown
        open={open}
        anchorRef={anchor}
        onClose={() => setOpen(false)}
        className="card-stage-menu"
      >
        {options.map((o) => (
          <button
            key={o.key || "\u0000none"}
            type="button"
            className={`card-stage-item${o.active ? " card-stage-item-active" : ""}`}
            onClick={(e) => {
              e.stopPropagation();
              setOpen(false);
              onPick(o.key);
            }}
          >
            {o.label}
          </button>
        ))}
        {extra}
      </Dropdown>
    </>
  );
}

/** StackIcon: turns piling up on one another. */
function StackIcon() {
  return (
    <svg className="process-stack" viewBox="0 0 12 12" aria-hidden="true">
      <rect x="1" y="7.5" width="10" height="2.2" rx="1.1" />
      <rect x="2" y="4.4" width="8" height="2.2" rx="1.1" />
      <rect x="3" y="1.3" width="6" height="2.2" rx="1.1" />
    </svg>
  );
}

function TaskForm({
  board,
  project,
  initial,
  onSave,
  onCancel,
}: {
  board: Board;
  /** The project the process files its iterations under: it decides which
   *  repository they land in, so only that repository's teams may own
   *  them — a task pairing the two would spawn cards the server refuses. */
  project: string;
  initial?: ProcessTask;
  onSave: (input: TaskInput) => void;
  onCancel: () => void;
}) {
  const [title, setTitle] = useState(initial?.title ?? "");
  const [description, setDescription] = useState(initial?.description ?? "");
  const [recurrence, setRecurrence] = useState(initial?.recurrence ?? "week");
  const [start, setStart] = useState(initial?.start ?? new Date().toISOString().slice(0, 10));
  const [team, setTeam] = useState(initial?.team ?? "");
  const [assignee, setAssignee] = useState(initial?.assignee ?? "");
  const [accumulate, setAccumulate] = useState(initial?.accumulate ?? false);
  const titleRef = useRef<HTMLInputElement | null>(null);
  useEffect(() => titleRef.current?.focus(), []);

  const submit = () => {
    if (!title.trim()) {
      return;
    }
    onSave({ title: title.trim(), description, recurrence, start, team, assignee, accumulate });
  };

  return (
    <form
      className="process-form"
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
    >
      <input
        ref={titleRef}
        type="text"
        className="add-card-input"
        placeholder="What every iteration is called…"
        value={title}
        onChange={(e) => setTitle(e.target.value)}
        onKeyDown={(e) => e.key === "Escape" && onCancel()}
      />
      <textarea
        className="modal-textarea process-form-desc"
        placeholder="What every iteration says (optional)"
        value={description}
        onChange={(e) => setDescription(e.target.value)}
      />
      <div className="process-form-row">
        <label>
          <span>Cycle</span>
          <select value={recurrence} onChange={(e) => setRecurrence(e.target.value)}>
            {CYCLES.map((c) => (
              <option key={c.key} value={c.key}>
                {c.label}
              </option>
            ))}
          </select>
        </label>
        <label>
          <span>From</span>
          <input type="date" value={start} onChange={(e) => setStart(e.target.value)} />
        </label>
        <label>
          <span>Team</span>
          <select value={team} onChange={(e) => setTeam(e.target.value)}>
            <option value="">— none —</option>
            {offerableTeams(board.teams, board.teamDomains, board.projectDomains, project, team)
              .filter((t) => t !== "")
              .map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
          </select>
        </label>
        <label>
          <span>Owner</span>
          <select value={assignee} onChange={(e) => setAssignee(e.target.value)}>
            <option value="">— nobody —</option>
            {board.members.map((m) => (
              <option key={m.login} value={m.login}>
                {m.login}
              </option>
            ))}
          </select>
        </label>
        <label className="process-form-check" title="Spawn the next iteration even while the previous one is still open — unpaid months pile up as separate cards">
          <input type="checkbox" checked={accumulate} onChange={(e) => setAccumulate(e.target.checked)} />
          <span>accumulate</span>
        </label>
      </div>
      <div className="process-form-actions">
        <button type="button" className="btn" onClick={onCancel}>
          Cancel
        </button>
        <button type="submit" className="btn btn-primary" disabled={!title.trim()}>
          {initial ? "Save" : "Add"}
        </button>
      </div>
    </form>
  );
}

function errText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
