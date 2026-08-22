import { useEffect, useMemo, useRef, useState } from "react";
import type {
  Board,
  ProcessTemplate,
  Provider,
  TemplateInput,
} from "../providers/types";
import { teamColor } from "../avatar";
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
  const [addingProcess, setAddingProcess] = useState(false);
  const [renaming, setRenaming] = useState<string | null>(null);

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
  const addProcess = (name: string) => {
    setAddingProcess(false);
    const n = name.trim();
    if (!n || targetProject === null) {
      return;
    }
    setOpen((cur) => new Set(cur).add(n));
    void provider.addProcess(board, n, targetProject).catch(fail);
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
            {targetProject !== null ? (
              <button type="button" className="btn btn-primary" onClick={() => setAddingProcess(true)}>
                + Add the first process{targetProject ? ` of ${targetProject}` : ""}
              </button>
            ) : (
              <p className="project-empty-hint">Pick one project above to add a process to it.</p>
            )}
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
              {!targetProject && p.project && (
                <span
                  className="project-epic-avatar"
                  style={{ background: teamColor(p.project) }}
                  title={p.project}
                >
                  {p.project[0]?.toUpperCase()}
                </span>
              )}
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
        {shown.length > 0 &&
          (addingProcess ? (
            <input
              type="text"
              className="add-card-input process-new"
              autoFocus
              placeholder={targetProject ? `New process in ${targetProject}…` : "New process…"}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  addProcess((e.target as HTMLInputElement).value);
                } else if (e.key === "Escape") {
                  setAddingProcess(false);
                }
              }}
              onBlur={(e) => addProcess(e.target.value)}
            />
          ) : (
            <button
              type="button"
              className="add-card process-new"
              disabled={targetProject === null}
              title={
                targetProject !== null
                  ? "Add a process"
                  : "Pick one project first — a process belongs to a project"
              }
              onClick={() => setAddingProcess(true)}
            >
              + add a process
            </button>
          ))}
        {shown.length === 0 && addingProcess && (
          <input
            type="text"
            className="add-card-input process-new"
            autoFocus
            placeholder={targetProject ? `New process in ${targetProject}…` : "New process…"}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                addProcess((e.target as HTMLInputElement).value);
              } else if (e.key === "Escape") {
                setAddingProcess(false);
              }
            }}
            onBlur={(e) => addProcess(e.target.value)}
          />
        )}
      </div>
    </div>
  );
}

/** Health is the row of dots: one per iteration, how it went. */
function Health({ templates }: { templates: ProcessTemplate[] }) {
  const all = templates.flatMap((t) => t.history);
  if (all.length === 0) {
    return <span className="process-health process-health-empty">no iterations yet</span>;
  }
  const recent = all.slice(-12);
  return (
    <span className="process-health" title={`${all.filter((i) => i.state === "done").length} of ${all.length} iterations done`}>
      {recent.map((i) => (
        <span key={i.uid} className={`process-dot process-dot-${i.state}`} title={`${i.week}: ${i.state}`} />
      ))}
    </span>
  );
}

function TemplateRow({
  template: t,
  onEdit,
  onDelete,
}: {
  template: ProcessTemplate;
  onEdit: () => void;
  onDelete: () => void;
}) {
  return (
    <div className="process-template">
      <div className="process-template-main">
        <span className="process-template-title">{t.title}</span>
        {t.description && (
          <span className="process-template-desc">{t.description}</span>
        )}
      </div>
      <span className="process-template-meta">
        <span>{cycleLabel(t.recurrence)}</span>
        <span>{t.start ? `from ${t.start}` : ""}</span>
        <span className="process-template-who">
          {t.team ? (
            <span className="process-team">
              <span className="team-dot" style={{ background: teamColor(t.team) }} />
              {t.team}
            </span>
          ) : (
            <span className="process-template-none">no team</span>
          )}
          {t.assignee && <span>@{t.assignee}</span>}
          {t.accumulate && (
            <span title="The next iteration spawns even while the previous is still open">
              accumulates
            </span>
          )}
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
