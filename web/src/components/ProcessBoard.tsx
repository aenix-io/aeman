import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import type {
  Board,
  ProcessTemplate,
  Provider,
  TemplateInput,
} from "../providers/types";
import { teamColor } from "../avatar";
import { avatarUrlFor, displayName, type GhUser } from "../users";
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
  /** GitHub name + avatar per login, as the Me and Team boards have them. */
  users: Record<string, GhUser>;
  onError: (message: string) => void;
}

/** The cycles a template may run on, in the order a person would list them. */
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
 *  into the templates it iterates on, and each template shows how its last
 *  iterations went. Processes belong to projects and the chips are the same
 *  ones the Project tab has; the selection is shared. */
export function ProcessBoard({
  board,
  provider,
  filter,
  onSetFilter,
  onManageProjects,
  users,
  onError,
}: ProcessBoardProps) {
  // The processes are part of the board, loaded with it and refreshed by the
  // Board watch frame — the server's cache is the one source, and a write
  // here shows up the same way a teammate's does: through the watch, with no
  // reload of anything.
  const processes = board.processes;
  const [open, setOpen] = useState<Set<string>>(() => new Set());
  // The template being created (keyed by process) or edited (by uid), the
  // process being named, and the one being renamed.
  const [adding, setAdding] = useState<string | null>(null);
  const [editing, setEditing] = useState<string | null>(null);
  // The project a new process is being named for (null = not naming one).
  const [addingProcess, setAddingProcess] = useState<string | null>(null);
  const [renaming, setRenaming] = useState<string | null>(null);
  // The "which project?" menu behind + add a process, for when the answer is
  // not already one chip.
  const [addMenu, setAddMenu] = useState(false);
  const addAnchor = useRef<HTMLElement | null>(null);

  const shown = useMemo(
    () => processes.filter((p) => !filter || filter.includes(p.project)),
    [processes, filter],
  );
  const targetProject = filter?.length === 1 ? filter[0] : null;
  const looseProcesses = processes.some((p) => !p.project);
  const fail = (err: unknown) => onError(errText(err));

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
    setOpen((cur) => new Set(cur).add(n));
    void provider.addProcess(board, n, project).catch(fail);
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
    void provider.setProcessProject(board, name, to).catch(fail);
  };
  const deleteProcess = (name: string) => {
    if (!window.confirm(`Delete the process “${name}”?`)) {
      return;
    }
    void provider.deleteProcess(board, name).catch(fail);
  };
  const renameProcess = (from: string, to: string) => {
    setRenaming(null);
    const n = to.trim();
    if (!n || n === from) {
      return;
    }
    void provider.renameProcess(board, from, n).catch(fail);
  };

  const saveTemplate = (process: string, uid: string | null, input: TemplateInput) => {
    setAdding(null);
    setEditing(null);
    const call = uid
      ? provider.updateTemplate(board, uid, input)
      : provider.addTemplate(board, process, input).then(() => undefined);
    void call.catch(fail);
  };
  const deleteTemplate = (t: ProcessTemplate) => {
    if (!window.confirm(`Delete the template “${t.title}”? Its past iterations stay.`)) {
      return;
    }
    void provider.deleteTemplate(board, t.uid).catch(fail);
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
      </div>

      <div className="process-list">
        {shown.length === 0 && !addingProcess && (
          <div className="project-empty">
            <p>
              A process is recurring work the team keeps doing — publishing
              articles, collecting payment — and wants to see itself doing.
            </p>
            <button type="button" className="btn btn-primary" onClick={beginAdd}>
              + Add the first process{targetProject ? ` of ${targetProject}` : ""}
            </button>
          </div>
        )}
        {shown.map((p) => (
          <section key={p.name} className="process-item">
            <header
              className="process-head"
              onClick={() => toggle(p.name)}
              onDoubleClick={(e) => {
                e.stopPropagation();
                setRenaming(p.name);
              }}
              title="Click to expand · double-click to rename"
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
                {p.templates.length} {p.templates.length === 1 ? "template" : "templates"}
              </span>
              <Health templates={p.templates} />
              <button
                type="button"
                className="card-action card-action-delete process-del"
                title={p.templates.length ? "Delete its templates first" : "Delete the process"}
                disabled={p.templates.length > 0}
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
                {p.templates.map((t) =>
                  editing === t.uid ? (
                    <TemplateForm
                      key={t.uid}
                      board={board}
                      initial={t}
                      onSave={(input) => saveTemplate(p.name, t.uid, input)}
                      onCancel={() => setEditing(null)}
                    />
                  ) : (
                    <TemplateRow
                      key={t.uid}
                      template={t}
                      teams={board.teams}
                      members={board.members}
                      users={users}
                      onSetRecurrence={(recurrence) => saveTemplate(p.name, t.uid, { recurrence })}
                      onSetTeam={(team) => saveTemplate(p.name, t.uid, { team })}
                      onSetAssignee={(assignee) => saveTemplate(p.name, t.uid, { assignee })}
                      onSetAccumulate={(accumulate) => saveTemplate(p.name, t.uid, { accumulate })}
                      onEdit={() => setEditing(t.uid)}
                      onDelete={() => deleteTemplate(t)}
                    />
                  ),
                )}
                {adding === p.name ? (
                  <TemplateForm
                    board={board}
                    onSave={(input) => saveTemplate(p.name, null, input)}
                    onCancel={() => setAdding(null)}
                  />
                ) : (
                  <button
                    type="button"
                    className="add-card process-add"
                    onClick={() => setAdding(p.name)}
                  >
                    + add a template
                  </button>
                )}
              </div>
            )}
          </section>
        ))}
        {/* A new process, named in place — the way a column is on Project.
            It needs a single project in view: that is what it belongs to. */}
        {addingProcess !== null ? (
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
            onBlur={(e) => addProcess(e.target.value, addingProcess)}
          />
        ) : (
          shown.length > 0 && (
            <button type="button" className="add-card process-new" onClick={beginAdd}>
              + add a process
            </button>
          )
        )}
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

/** Health is the row of dots: one per iteration, how it went. */
function Health({ templates }: { templates: ProcessTemplate[] }) {
  // Sorted by week: a process's row gathers several templates, and their
  // turns interleave in time rather than in the order the templates were
  // added — an unsorted row would read as a history that never happened.
  const all = templates
    .flatMap((t) => t.history)
    .slice()
    .sort((a, b) => a.week.localeCompare(b.week));
  if (all.length === 0) {
    return <span className="process-health process-health-empty">no iterations yet</span>;
  }
  const recent = all.slice(-SHOWN_ITERATIONS);
  const hidden = all.length - recent.length;
  const done = all.filter((i) => i.state === "done").length;
  const late = all.filter((i) => i.state === "late").length;
  return (
    <span
      className="process-health"
      title={`${done} of ${all.length} turns done${late ? `, ${late} overdue` : ""}${
        hidden ? ` · showing the last ${recent.length}` : ""
      }`}
    >
      {hidden > 0 && <span className="process-health-more">+{hidden}</span>}
      {recent.map((i) => (
        <span key={i.uid} className={`process-dot process-dot-${i.state}`} title={`${i.week}: ${i.state}`} />
      ))}
    </span>
  );
}

function TemplateRow({
  template: t,
  teams,
  members,
  users,
  onSetRecurrence,
  onSetTeam,
  onSetAssignee,
  onSetAccumulate,
  onEdit,
  onDelete,
}: {
  template: ProcessTemplate;
  teams: string[];
  members: string[];
  users: Record<string, GhUser>;
  onSetRecurrence: (recurrence: string) => void;
  onSetTeam: (team: string) => void;
  onSetAssignee: (assignee: string) => void;
  onSetAccumulate: (accumulate: boolean) => void;
  onEdit: () => void;
  onDelete: () => void;
}) {
  return (
    <div className="process-template">
      {/* The title opens the template on a double-click, the way a card does:
          the pencil is for finding it, this is for using it. */}
      <div
        className="process-template-main"
        title="Double-click to edit"
        onDoubleClick={onEdit}
      >
        <span className="process-template-title">{t.title}</span>
        {t.description && (
          <span className="process-template-desc">{t.description}</span>
        )}
      </div>
      <span className="process-template-meta">
        {/* One control for one subject: how often the work comes round, and
            what a turn does when the last one is unfinished. The stack marks
            a template that accumulates; a plain cycle says nothing, because
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
        <span>{t.start ? `from ${t.start}` : ""}</span>
        <span className="process-template-who">
          {/* Team and owner are set where they are read — the cell IS the
              control, as a card's badges are. The form behind ✎ is for the
              title, the body and the cycle. */}
          <CellPicker
            className="process-cell-team"
            title="Which team's weekly plan the iterations land in"
            label={
              t.team ? (
                <>
                  <span className="team-dot" style={{ background: teamColor(t.team) }} />
                  {t.team}
                </>
              ) : (
                <span className="process-template-none">no team</span>
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
                ? `Assigned to ${displayName(t.assignee, users[t.assignee])} — click to change`
                : "Nobody owns the iterations — click to assign"
            }
            label={
              t.assignee ? (
                <>
                  <img
                    className="avatar-img process-owner-avatar"
                    src={avatarUrlFor(t.assignee, users[t.assignee])}
                    alt={t.assignee}
                  />
                  {/* The name alone in the row — the login is in the tooltip
                      and in the menu, and it is the half that gets cut. */}
                  {users[t.assignee]?.name ?? t.assignee}
                </>
              ) : (
                <span className="process-template-none">nobody</span>
              )
            }
            options={[
              ...members.map((m) => ({
                key: m,
                active: m === t.assignee,
                label: (
                  <>
                    <img className="avatar-img" src={avatarUrlFor(m, users[m])} alt={m} />
                    {displayName(m, users[m])}
                  </>
                ),
              })),
              { key: "", active: !t.assignee, label: <>Nobody</> },
            ]}
            onPick={onSetAssignee}
          />
        </span>
      </span>
      <Health templates={[t]} />
      <span className="process-template-actions">
        <button type="button" className="card-action" title="Edit the template" onClick={onEdit}>
          ✎
        </button>
        <button type="button" className="card-action card-action-delete" title="Delete the template" onClick={onDelete}>
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

function TemplateForm({
  board,
  initial,
  onSave,
  onCancel,
}: {
  board: Board;
  initial?: ProcessTemplate;
  onSave: (input: TemplateInput) => void;
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
            {board.teams.filter((t) => t !== "").map((t) => (
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
              <option key={m} value={m}>
                {m}
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
