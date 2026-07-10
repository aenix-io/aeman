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
  BoardAddr,
  Card,
  CardEvent,
  CardPatch,
  CarryReport,
  NewCardInput,
  Note,
  Provider,
  ZoneKey,
} from "../types";

// api issues a request against /api/v1 for a given board. It carries the board
// identity as query parameters (?owner=&project=) so the server resolves the
// right board, sets a JSON content type when there is a body, and on a non-2xx
// response surfaces the server's {error} message (falling back to statusText).
async function api<T>(
  board: BoardAddr,
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  const sep = path.includes("?") ? "&" : "?";
  const url = `/api/v1${path}${sep}owner=${encodeURIComponent(
    board.owner,
  )}&project=${board.number}`;
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

// cardFrom runs a request that answers with a Card resource and maps it.
async function cardFrom(
  board: BoardAddr,
  method: string,
  path: string,
  body?: unknown,
): Promise<Card> {
  return resourceToCard(await api<CardResource>(board, method, path, body));
}

// notesFrom runs a request that answers with a NoteList and maps it.
async function notesFrom(
  board: BoardAddr,
  method: string,
  path: string,
  body?: unknown,
): Promise<Note[]> {
  const list = await api<NoteListResource>(board, method, path, body);
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
  if (patch.reviewOf !== undefined) {
    body.reviewOf = patch.reviewOf;
  }
  if (patch.parent !== undefined) {
    body.parent = patch.parent;
  }
  return body;
}

export const apiProvider: Provider = {
  async loadBoard(owner: string, number: number): Promise<Board> {
    const addr: BoardAddr = { owner, number };
    const [info, sprints] = await Promise.all([
      api<BoardResource>(addr, "GET", "/board"),
      api<SprintListResource>(addr, "GET", "/sprints"),
    ]);
    return {
      owner,
      number,
      title: info.metadata.title ?? "",
      url: info.metadata.url ?? "",
      // Cards are loaded per view via listCards; the initial set arrives right
      // after this from the App's first view fetch.
      cards: [],
      teams: info.metadata.teams ?? [],
      members: info.metadata.members ?? [],
      sprintStates: sprintStatesFrom(sprints.items ?? []),
    };
  },

  async listCards(
    board: BoardAddr,
    query: Record<string, string>,
  ): Promise<Card[]> {
    const qs = Object.keys(query)
      .map((k) => `${encodeURIComponent(k)}=${encodeURIComponent(query[k])}`)
      .join("&");
    // LIST responses are served in board order; the Ordering watch events keep
    // the local copy sorted between re-lists.
    const cards = await api<CardListResource>(board, "GET", `/cards?${qs}`);
    return (cards.items ?? []).map(resourceToCard);
  },

  async createCard(board: BoardAddr, input: NewCardInput): Promise<Card> {
    const body: Record<string, unknown> = {
      title: input.title,
      team: input.team ?? "",
      zone: semanticZone(input.zone),
      assignees: input.assigneeLogin ? [input.assigneeLogin] : [],
      reviewOf: input.reviewOf ?? "",
    };
    // Plan cards carry no dates (they live in the weekly bands); day cards pass
    // their start/end and the server joins (or records) the sprint itself.
    if (input.start || input.day) {
      body.dates = { start: input.start ?? "", end: input.day ?? "" };
    }
    if (input.plan) {
      body.plan = { band: input.plan, week: input.week ?? "" };
    }
    if (input.startNewSprint !== undefined) {
      body.startNewSprint = input.startNewSprint;
    }
    if (input.noSprint) {
      body.noSprint = true;
    }
    return cardFrom(board, "POST", "/cards", body);
  },

  async patchCard(
    board: BoardAddr,
    uid: string,
    patch: CardPatch,
  ): Promise<Card> {
    uid = await resolveCardId(uid);
    return cardFrom(board, "PATCH", `/cards/${uid}`, patchBody(patch));
  },

  async deleteCard(board: BoardAddr, uid: string): Promise<void> {
    uid = await resolveCardId(uid);
    await api(board, "DELETE", `/cards/${uid}`);
  },

  async removeCard(
    board: BoardAddr,
    uid: string,
    from: "grid" | "plan",
  ): Promise<void> {
    uid = await resolveCardId(uid);
    await api(board, "POST", `/cards/${uid}/actions/remove`, { from });
  },

  async moveCard(
    board: BoardAddr,
    uid: string,
    afterId: string | null,
  ): Promise<void> {
    uid = await resolveCardId(uid);
    const after = afterId ? await resolveCardId(afterId) : "";
    await api(board, "POST", `/cards/${uid}/actions/move`, { after });
  },

  async moveCardBefore(
    board: BoardAddr,
    uid: string,
    beforeId: string,
  ): Promise<void> {
    uid = await resolveCardId(uid);
    const before = await resolveCardId(beforeId);
    await api(board, "POST", `/cards/${uid}/actions/move`, { before });
  },

  async deferCard(board: BoardAddr, uid: string, days: number): Promise<Card> {
    uid = await resolveCardId(uid);
    return cardFrom(board, "POST", `/cards/${uid}/actions/defer`, { days });
  },

  async setInProgress(board: BoardAddr, uid: string): Promise<Card> {
    uid = await resolveCardId(uid);
    return cardFrom(board, "POST", `/cards/${uid}/actions/in-progress`, {});
  },

  async sendToReview(
    board: BoardAddr,
    uid: string,
    reviewer: string,
    day?: string,
    zone?: ZoneKey,
  ): Promise<Card> {
    uid = await resolveCardId(uid);
    return cardFrom(board, "POST", `/cards/${uid}/actions/send-to-review`, {
      reviewer,
      day: day ?? "",
      zone: zone ? semanticZone(zone) : "",
    });
  },

  async removeReviewer(board: BoardAddr, uid: string): Promise<Card> {
    uid = await resolveCardId(uid);
    return cardFrom(board, "POST", `/cards/${uid}/actions/remove-reviewer`, {});
  },

  async takeIntoPlan(
    board: BoardAddr,
    uid: string,
    engineer: string,
    zone,
    day?: string,
  ): Promise<Card> {
    uid = await resolveCardId(uid);
    return cardFrom(board, "POST", `/cards/${uid}/actions/take-into-plan`, {
      engineer,
      zone: semanticZone(zone),
      day: day ?? "",
    });
  },

  async releaseFromPlan(board: BoardAddr, uid: string): Promise<Card> {
    uid = await resolveCardId(uid);
    return cardFrom(
      board,
      "POST",
      `/cards/${uid}/actions/release-from-plan`,
      {},
    );
  },

  async carryOver(
    board: BoardAddr,
    team: string | null,
    dryRun = false,
  ): Promise<CarryReport> {
    return api<CarryReport>(board, "POST", "/sprints/actions/carry-over", {
      team: team ?? "",
      dryRun,
    });
  },

  async carryWeek(
    board: BoardAddr,
    team: string | null,
    week: string,
    dryRun = false,
  ): Promise<CarryReport> {
    return api<CarryReport>(board, "POST", "/sprints/actions/carry-week", {
      team: team ?? "",
      week,
      dryRun,
    });
  },

  async setSprintState(
    board: BoardAddr,
    team: string | null,
    current: string | null,
    previous: string | null,
  ): Promise<void> {
    await api(board, "PATCH", "/sprints", {
      team: team ?? "",
      current: current ?? "",
      previous: previous ?? "",
    });
  },

  async listLog(
    board: BoardAddr,
    uid: string,
  ): Promise<{ notes: Note[]; events: CardEvent[] }> {
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
    }>(board, "GET", `/cards/${uid}/log`);
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
    return { notes, events };
  },

  async listLinks(board: BoardAddr, uid: string): Promise<CardLink[]> {
    uid = await resolveCardId(uid);
    const list = await api<{ kind: string; items: CardLink[] | null }>(
      board,
      "GET",
      `/cards/${uid}/links`,
    );
    return list.items ?? [];
  },

  async setPresence(
    board: BoardAddr,
    login: string,
    card: string | null,
  ): Promise<void> {
    const uid = card && !card.startsWith("tmp-") ? card : "";
    await api(board, "POST", "/presence", { login, card: uid });
  },

  async listNotes(board: BoardAddr, uid: string): Promise<Note[]> {
    uid = await resolveCardId(uid);
    return notesFrom(board, "GET", `/cards/${uid}/notes`);
  },

  async addNote(board: BoardAddr, uid: string, text: string): Promise<Note[]> {
    uid = await resolveCardId(uid);
    return notesFrom(board, "POST", `/cards/${uid}/notes`, { text });
  },

  async editNote(
    board: BoardAddr,
    uid: string,
    noteId: string,
    text: string,
  ): Promise<Note[]> {
    uid = await resolveCardId(uid);
    return notesFrom(
      board,
      "PATCH",
      `/cards/${uid}/notes/${encodeURIComponent(noteId)}`,
      { text },
    );
  },

  async deleteNote(
    board: BoardAddr,
    uid: string,
    noteId: string,
  ): Promise<Note[]> {
    uid = await resolveCardId(uid);
    return notesFrom(
      board,
      "DELETE",
      `/cards/${uid}/notes/${encodeURIComponent(noteId)}`,
    );
  },
};
