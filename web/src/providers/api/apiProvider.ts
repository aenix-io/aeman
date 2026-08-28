// The thin intent client of aeman's Kubernetes-style API under /api/v1: reads
// compose the LIST responses into the frontend Board; writes state intent and
// return the resulting resources (mapped to the internal Card model), which the
// caller applies over its optimistic state. All board rules run server-side.

import { clientId } from "../../api/client";
import { resolveCardId } from "../../api/pending";
import type { CardLink } from "../../links";
import {
  resourceToCard,
  resourceToNote,
  semanticZone,
  sprintStatesFrom,
  type BoardResource,
  type CardListResource,
  type CardResource,
  type NoteListResource,
  type SprintListResource,
} from "../../api/resources";
import type {
  Board,
  Card,
  CardEvent,
  CardLog,
  CardPatch,
  CarryReport,
  NewCardInput,
  Note,
  ProcessInfo,
  Provider,
  TaskInput,
  ZoneKey,
} from "../types";

/** boardMetadata maps a board resource's metadata onto board state — used by
 *  loadBoard and by the Board watch frame alike, so a roster change arriving
 *  over the watch lands exactly as a fresh load would. */
export function boardMetadata(
  info: BoardResource,
): Pick<
  Board,
  "title" | "url" | "teams" | "projects" | "deadlines" | "epics" | "members" | "domains"
> {
  return {
    title: info.metadata.title ?? "",
    url: info.metadata.url ?? "",
    teams: info.metadata.teams ?? [],
    projects: info.metadata.projects ?? [],
    deadlines: (info.metadata.deadlines ?? []).map((d) => ({
      week: d.week,
      project: d.project ?? "",
    })),
    epics: (info.metadata.epics ?? []).map((e) => ({
      name: e.name,
      project: e.project ?? "",
    })),
    members: (info.metadata.members ?? []).map((m) => ({
      login: m.login,
      avatarUrl: m.avatarUrl || undefined,
    })),
    // The repositories the board spans, primary first. An older server names
    // none; the UI then shows nothing of domains at all.
    domains: (info.metadata.domains ?? []).map((d) => ({
      name: d.name,
      writable: d.writable ?? false,
      members: d.members ?? [],
    })),
  };
}

/** processesFrom normalises the process structure off the wire. */
export function processesFrom(items: ProcessInfo[] | null | undefined): ProcessInfo[] {
  return (items ?? []).map((p) => ({
    ...p,
    project: p.project ?? "",
    tasks: (p.tasks ?? []).map((t) => ({ ...t, history: t.history ?? [] })),
  }));
}

// api issues a request against /api/v1. The server serves exactly one board,
// so nothing addresses it. Sets a JSON content type when there is a body, and
// on a non-2xx response surfaces the server's {error} message (falling back to
// statusText).
async function api<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  const url = `/api/v1${path}`;
  // X-Aeman-Client keys watch echo suppression: the server skips this tab's
  // own watch connection when broadcasting the changes it makes here.
  const init: RequestInit = { method, headers: { "X-Aeman-Client": clientId } };
  if (body !== undefined) {
    init.headers = {
      "Content-Type": "application/json",
      "X-Aeman-Client": clientId,
    };
    init.body = JSON.stringify(body);
  }
  const res = await fetch(url, init);
  if (!res.ok) {
    let msg = res.statusText;
    try {
      const data = (await res.json()) as { error?: string };
      if (data.error) {
        msg = data.error;
      }
    } catch {
      // No JSON error body; keep the status-text fallback.
    }
    throw new Error(msg);
  }
  return (await res.json()) as T;
}

// inDomain is the optional `domain` of a declare request: the repository a new
// team/project/process is filed in. Omitted, the server picks the primary.
function inDomain(domain?: string): { domain?: string } {
  return domain ? { domain } : {};
}

// cardFrom runs a request that answers with a Card resource and maps it.
async function cardFrom(
  method: string,
  path: string,
  body?: unknown,
): Promise<Card> {
  return resourceToCard(await api<CardResource>(method, path, body));
}

// notesFrom runs a request that answers with a NoteList and maps it.
async function notesFrom(
  method: string,
  path: string,
  body?: unknown,
): Promise<Note[]> {
  const list = await api<NoteListResource>(method, path, body);
  return (list.items ?? []).map(resourceToNote);
}

// patchBody translates a CardPatch onto the wire shape: only present fields go
// out ("" clears), zones travel under their semantic names.
function patchBody(patch: CardPatch): Record<string, unknown> {
  const body: Record<string, unknown> = {};
  if (patch.title !== undefined) {
    body.title = patch.title;
  }
  if (patch.description !== undefined) {
    body.description = patch.description;
  }
  if (patch.team !== undefined) {
    body.team = patch.team;
  }
  if (patch.zone !== undefined) {
    body.zone = semanticZone(patch.zone);
  }
  if (patch.assignees !== undefined) {
    body.assignees = patch.assignees;
  }
  if (patch.progress !== undefined) {
    body.progress = patch.progress;
  }
  if (patch.recurrence !== undefined) {
    body.recurrence = patch.recurrence;
  }
  if (patch.stage !== undefined) {
    body.stage = patch.stage;
  }
  if (patch.dates) {
    const dates: Record<string, string> = {};
    if (patch.dates.start !== undefined) {
      dates.start = patch.dates.start;
    }
    if (patch.dates.end !== undefined) {
      dates.end = patch.dates.end;
    }
    if (patch.dates.sprint !== undefined) {
      dates.sprint = patch.dates.sprint;
    }
    body.dates = dates;
  }
  if (patch.plan) {
    const plan: Record<string, string> = {};
    if (patch.plan.band !== undefined) {
      plan.band = patch.plan.band;
    }
    if (patch.plan.week !== undefined) {
      plan.week = patch.plan.week;
    }
    body.plan = plan;
  }
  if (patch.epic !== undefined) {
    body.epic = patch.epic;
  }
  if (patch.project !== undefined) {
    body.project = patch.project;
  }
  if (patch.reviewOf !== undefined) {
    body.reviewOf = patch.reviewOf;
  }
  if (patch.parent !== undefined) {
    body.parent = patch.parent;
  }
  return body;
}

export const apiProvider: Provider = {
  async loadBoard(): Promise<Board> {
    const [info, sprints] = await Promise.all([
      api<BoardResource>("GET", "/board"),
      api<SprintListResource>("GET", "/sprints"),
    ]);
    return {
      // Cards are loaded per view via listCards; the initial set arrives right
      // after this from the App's first view fetch.
      cards: [],
      ...boardMetadata(info),
      processes: [],
      sprintStates: sprintStatesFrom(sprints.items ?? []),
    };
  },

  async listCards(
    query: Record<string, string>,
  ): Promise<Card[]> {
    // Listings are board rows (the server-side default): card bodies live
    // behind getCard, and status.links stands in for the row's links icon.
    const qs = Object.keys(query)
      .map((k) => `${encodeURIComponent(k)}=${encodeURIComponent(query[k])}`)
      .join("&");
    // LIST responses are served in board order; the Ordering watch events keep
    // the local copy sorted between re-lists.
    const cards = await api<CardListResource>("GET", `/cards?${qs}`);
    return (cards.items ?? []).map(resourceToCard);
  },

  async getCard(uid: string): Promise<Card> {
    return cardFrom("GET", `/cards/${await resolveCardId(uid)}`);
  },

  async createCard(input: NewCardInput): Promise<Card> {
    const body: Record<string, unknown> = {
      title: input.title,
      team: input.team ?? "",
      zone: semanticZone(input.zone),
      assignees: input.assigneeLogin ? [input.assigneeLogin] : [],
      reviewOf: input.reviewOf ?? "",
      // A parent may still be an optimistic tmp id: wait for the real uid.
      parent: input.parent ? await resolveCardId(input.parent) : "",
    };
    // Plan cards carry no dates (they live in the weekly bands); day cards pass
    // their start/end and the server joins (or records) the sprint itself.
    if (input.start || input.day) {
      body.dates = { start: input.start ?? "", end: input.day ?? "" };
    }
    if (input.plan) {
      body.plan = { band: input.plan, week: input.week ?? "" };
    }
    if (input.epic) {
      body.epic = input.epic;
      body.project = input.project ?? "";
      // No plan.week: a slot's row is the week of its start date.
    }
    if (input.startNewSprint !== undefined) {
      body.startNewSprint = input.startNewSprint;
    }
    if (input.noSprint) {
      body.noSprint = true;
    }
    return cardFrom("POST", "/cards", body);
  },

  async patchCard(
    uid: string,
    patch: CardPatch,
  ): Promise<Card> {
    uid = await resolveCardId(uid);
    if (patch.parent) {
      // Grouping under a just-created card: wait for its real uid.
      patch = { ...patch, parent: await resolveCardId(patch.parent) };
    }
    return cardFrom("PATCH", `/cards/${uid}`, patchBody(patch));
  },

  async deleteCard(uid: string): Promise<void> {
    uid = await resolveCardId(uid);
    await api("DELETE", `/cards/${uid}`);
  },

  async removeCard(
    uid: string,
    from: "grid" | "plan",
  ): Promise<void> {
    uid = await resolveCardId(uid);
    await api("POST", `/cards/${uid}/actions/remove`, { from });
  },

  async moveCard(
    uid: string,
    afterId: string | null,
  ): Promise<void> {
    uid = await resolveCardId(uid);
    const after = afterId ? await resolveCardId(afterId) : "";
    await api("POST", `/cards/${uid}/actions/move`, { after });
  },

  async moveCardBefore(
    uid: string,
    beforeId: string,
  ): Promise<void> {
    uid = await resolveCardId(uid);
    const before = await resolveCardId(beforeId);
    await api("POST", `/cards/${uid}/actions/move`, { before });
  },

  async deferCard(uid: string, days: number): Promise<Card> {
    uid = await resolveCardId(uid);
    return cardFrom("POST", `/cards/${uid}/actions/defer`, { days });
  },

  async setInProgress(uid: string): Promise<Card> {
    uid = await resolveCardId(uid);
    return cardFrom("POST", `/cards/${uid}/actions/in-progress`, {});
  },

  async reopen(uid: string): Promise<Card> {
    uid = await resolveCardId(uid);
    return cardFrom("POST", `/cards/${uid}/actions/reopen`, {});
  },

  async sendToReview(
    uid: string,
    reviewer: string,
    day?: string,
    zone?: ZoneKey,
  ): Promise<Card> {
    uid = await resolveCardId(uid);
    return cardFrom("POST", `/cards/${uid}/actions/send-to-review`, {
      reviewer,
      day: day ?? "",
      zone: zone ? semanticZone(zone) : "",
    });
  },

  async removeReviewer(uid: string): Promise<Card> {
    uid = await resolveCardId(uid);
    return cardFrom("POST", `/cards/${uid}/actions/remove-reviewer`, {});
  },

  async takeIntoPlan(
    uid: string,
    engineer: string,
    zone,
    day?: string,
  ): Promise<Card> {
    uid = await resolveCardId(uid);
    return cardFrom("POST", `/cards/${uid}/actions/take-into-plan`, {
      engineer,
      zone: semanticZone(zone),
      day: day ?? "",
    });
  },

  async releaseFromPlan(uid: string): Promise<Card> {
    uid = await resolveCardId(uid);
    return cardFrom("POST",
      `/cards/${uid}/actions/release-from-plan`,
      {},
    );
  },

  async carryOver(
    team: string | null,
    dryRun = false,
  ): Promise<CarryReport> {
    return api<CarryReport>("POST", "/sprints/actions/carry-over", {
      team: team ?? "",
      dryRun,
    });
  },

  async reorderTeams(teams: string[]): Promise<void> {
    await api("POST", "/sprints/actions/reorder-teams", { teams });
  },

  async deleteTeam(team: string): Promise<void> {
    await api("POST", "/sprints/actions/delete-team", { team });
  },

  async addEpic(
    name: string,
    project: string,
  ): Promise<void> {
    await api("POST", "/epics", { name, project });
  },

  async deleteEpic(
    name: string,
    project: string,
  ): Promise<void> {
    await api("POST", "/epics/actions/delete-epic", {
      epic: name,
      project,
    });
  },

  async renameEpic(
    project: string,
    epic: string,
    to: string,
  ): Promise<void> {
    await api("POST", "/epics/actions/rename", { project, epic, to });
  },

  async reorderEpics(
    project: string,
    epics: string[],
  ): Promise<void> {
    await api("POST", "/epics/actions/reorder-epics", { project, epics });
  },

  async setEpicProject(
    from: string,
    epic: string,
    project: string,
  ): Promise<void> {
    await api("POST", "/epics/actions/set-project", {
      epic,
      from,
      project,
    });
  },

  async addProject(name: string, domain?: string): Promise<void> {
    await api("POST", "/projects", { name, ...inDomain(domain) });
  },

  async deleteProject(name: string): Promise<void> {
    await api("POST", "/projects/actions/delete-project", {
      project: name,
    });
  },

  async reorderProjects(names: string[]): Promise<void> {
    await api("POST", "/projects/actions/reorder-projects", {
      projects: names,
    });
  },

  async renameProject(
    from: string,
    to: string,
  ): Promise<void> {
    await api("POST", "/projects/actions/rename", { project: from, to });
  },

  async listProcesses(project?: string): Promise<ProcessInfo[]> {
    const q = project ? `?project=${encodeURIComponent(project)}` : "";
    const res = await api<{ items: ProcessInfo[] | null }>("GET", `/processes${q}`);
    return processesFrom(res.items);
  },

  async addProcess(name: string, project: string, domain?: string): Promise<void> {
    await api("POST", "/processes", { name, project, ...inDomain(domain) });
  },

  async deleteProcess(name: string): Promise<void> {
    await api("POST", "/processes/actions/delete-process", { process: name });
  },

  async renameProcess(from: string, to: string): Promise<void> {
    await api("POST", "/processes/actions/rename", { process: from, to });
  },

  async setProcessProject(
    process: string,
    project: string,
  ): Promise<void> {
    await api("POST", "/processes/actions/set-project", { process, project });
  },

  async setProcessPaused(
    process: string,
    paused: boolean,
  ): Promise<void> {
    await api("POST", "/processes/actions/set-paused", { process, paused });
  },

  async reorderProcesses(processes: string[]): Promise<void> {
    await api("POST", "/processes/actions/reorder", { processes });
  },

  async reorderProcessTasks(
    process: string,
    uids: string[],
  ): Promise<void> {
    await api("POST", "/processes/tasks/actions/reorder", { process, uids });
  },

  async addTask(
    process: string,
    input: TaskInput,
  ): Promise<string> {
    const res = await api<{ uid: string }>("POST", "/processes/tasks", {
      process,
      ...input,
    });
    return res.uid;
  },

  async updateTask(
    uid: string,
    patch: TaskInput,
  ): Promise<void> {
    await api("PATCH", `/processes/tasks/${uid}`, patch);
  },

  async deleteTask(uid: string): Promise<void> {
    await api("DELETE", `/processes/tasks/${uid}`);
  },

  async addDeadline(
    week: string,
    project: string,
  ): Promise<void> {
    await api("POST", "/deadlines", { week, project });
  },

  async deleteDeadline(
    week: string,
    project: string,
  ): Promise<void> {
    await api("POST", "/deadlines/actions/delete", { week, project });
  },

  async moveDeadline(
    project: string,
    from: string,
    to: string,
  ): Promise<void> {
    await api("POST", "/deadlines/actions/move", { project, from, to });
  },

  async setSprintState(
    team: string | null,
    current: string | null,
    previous: string | null,
    domain?: string,
  ): Promise<void> {
    await api("PATCH", "/sprints", {
      team: team ?? "",
      current: current ?? "",
      previous: previous ?? "",
      ...inDomain(domain),
    });
  },

  async listLog(uid: string): Promise<CardLog> {
    uid = await resolveCardId(uid);
    const list = await api<{
      items:
        | {
            type: string;
            id: string;
            at?: string;
            actor?: string;
            kind?: string;
            from?: string;
            to?: string;
            text?: string;
          }[]
        | null;
      /** Set when older history exists beyond what the server has loaded. */
      truncatedBefore?: string;
    }>("GET", `/cards/${uid}/log`);
    const notes: Note[] = [];
    const events: CardEvent[] = [];
    for (const it of list.items ?? []) {
      if (it.type === "event") {
        events.push({
          id: it.id,
          kind: it.kind ?? "",
          actor: it.actor,
          from: it.from,
          to: it.to,
          at: it.at ?? "",
        });
      } else {
        notes.push({
          id: it.id,
          body: it.text ?? "",
          createdAt: it.at ?? "",
          author: it.actor,
          source: "draft",
        });
      }
    }
    return { notes, events, truncatedBefore: list.truncatedBefore || undefined };
  },

  async listLinks(uid: string): Promise<CardLink[]> {
    uid = await resolveCardId(uid);
    const list = await api<{ kind: string; items: CardLink[] | null }>("GET",
      `/cards/${uid}/links`,
    );
    return list.items ?? [];
  },

  async setPresence(
    login: string,
    card: string | null,
  ): Promise<void> {
    const uid = card && !card.startsWith("tmp-") ? card : "";
    await api("POST", "/presence", { login, card: uid });
  },

  async listNotes(uid: string): Promise<Note[]> {
    uid = await resolveCardId(uid);
    return notesFrom("GET", `/cards/${uid}/notes`);
  },

  async addNote(uid: string, text: string): Promise<Note[]> {
    uid = await resolveCardId(uid);
    return notesFrom("POST", `/cards/${uid}/notes`, { text });
  },

  async editNote(
    uid: string,
    noteId: string,
    text: string,
  ): Promise<Note[]> {
    uid = await resolveCardId(uid);
    return notesFrom("PATCH",
      `/cards/${uid}/notes/${encodeURIComponent(noteId)}`,
      { text },
    );
  },

  async deleteNote(
    uid: string,
    noteId: string,
  ): Promise<Note[]> {
    uid = await resolveCardId(uid);
    return notesFrom("DELETE",
      `/cards/${uid}/notes/${encodeURIComponent(noteId)}`,
    );
  },
};
