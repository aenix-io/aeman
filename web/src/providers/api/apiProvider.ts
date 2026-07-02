// A Provider backed by aeman's own REST API under /api/v1, instead of talking to
// GitHub GraphQL directly. It is still GitHub-backed (the server proxies to
// Projects v2), so it keeps the "github" ProviderId. Live board updates arrive
// out-of-band over a WebSocket handled elsewhere; loadCards therefore always
// asks for a full reload rather than patching individual cards.

import { clientId } from "../../api/client";
import { fieldRoles } from "../fields";
import type {
  Board,
  BoardSummary,
  Card,
  NewCardInput,
  Note,
  Provider,
  StageKey,
  ZoneKey,
} from "../types";
import { zoneFromColor } from "../../zones";

// api issues a request against /api/v1 for a given board. It carries the board
// identity as query parameters (?owner=&project=) so the server resolves the
// right board, sets a JSON content type when there is a body, and on a non-2xx
// response surfaces the server's {error} message (falling back to statusText).
async function api<T>(
  board: Pick<Board, "owner" | "number">,
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

// setSprintStartOn posts a card's Sprint Start date. Shared by setSprintStart and
// setSprintStartMany so Carry Over reuses the exact same request per card.
function setSprintStartOn(
  board: Board,
  card: Card,
  date: string | null,
): Promise<Card> {
  return api<Card>(board, "POST", `/cards/${card.itemId}/sprint-start`, {
    sprintStart: date ?? "",
  });
}

export const apiProvider: Provider = {
  id: "github",
  label: "GitHub",

  async listBoards(_owner: string): Promise<BoardSummary[]> {
    return [];
  },

  async loadBoard(owner: string, number: number): Promise<Board> {
    return api<Board>({ owner, number }, "GET", "/snapshot");
  },

  async loadCards(
    _board: Board,
    _ids: string[],
  ): Promise<{ cards: Card[]; needsFullReload: boolean }> {
    // Live updates arrive over a WebSocket handled elsewhere; if a caller ever
    // reaches here, force a full board reload rather than patching cards.
    return { cards: [], needsFullReload: true };
  },

  async setZone(board: Board, card: Card, optionId: string | null): Promise<void> {
    // The API takes a ZoneKey, not an option id: resolve the option's colour on
    // the board's zone field and convert it to a zone (null → clear, i.e. "").
    let zone: ZoneKey | "" = "";
    if (optionId !== null) {
      const option = fieldRoles(board).zone?.options?.find(
        (o) => o.id === optionId,
      );
      zone = zoneFromColor(option?.color) ?? "";
    }
    await api<Card>(board, "POST", `/cards/${card.itemId}/zone`, { zone });
  },

  async setProgress(board: Board, card: Card, progress: number): Promise<void> {
    await api<Card>(board, "POST", `/cards/${card.itemId}/progress`, { progress });
  },

  async setDay(board: Board, card: Card, day: string | null): Promise<void> {
    await api<Card>(board, "POST", `/cards/${card.itemId}/day`, { day: day ?? "" });
  },

  async setStart(board: Board, card: Card, date: string | null): Promise<void> {
    await api<Card>(board, "POST", `/cards/${card.itemId}/start`, {
      start: date ?? "",
    });
  },

  async setSprintStart(board: Board, card: Card, date: string | null): Promise<void> {
    await setSprintStartOn(board, card, date);
  },

  async setSprintStartMany(
    board: Board,
    cards: Card[],
    date: string,
  ): Promise<void> {
    await Promise.all(cards.map((c) => setSprintStartOn(board, c, date)));
  },

  async setSprintState(
    board: Board,
    team: string | null,
    current: string | null,
    previous: string | null,
  ): Promise<void> {
    await api(board, "POST", "/sprint-state", {
      team: team ?? "",
      current: current ?? "",
      previous: previous ?? "",
    });
  },

  async setPlan(board: Board, card: Card, plan: "wed" | "fri" | null): Promise<void> {
    await api<Card>(board, "POST", `/cards/${card.itemId}/plan`, {
      plan: plan ?? "",
    });
  },

  async setWeek(board: Board, card: Card, date: string | null): Promise<void> {
    await api<Card>(board, "POST", `/cards/${card.itemId}/week`, {
      week: date ?? "",
    });
  },

  async setAssignee(board: Board, card: Card, login: string | null): Promise<void> {
    await api<Card>(board, "POST", `/cards/${card.itemId}/assignee`, {
      login: login ?? "",
    });
  },

  async setStage(board: Board, card: Card, stage: StageKey | null): Promise<void> {
    await api<Card>(board, "POST", `/cards/${card.itemId}/stage`, {
      stage: stage ?? "",
    });
  },

  async setTeam(board: Board, card: Card, team: string | null): Promise<void> {
    await api<Card>(board, "POST", `/cards/${card.itemId}/team`, {
      team: team ?? "",
    });
  },

  async renameCard(board: Board, card: Card, title: string): Promise<void> {
    await api<Card>(board, "POST", `/cards/${card.itemId}/rename`, { title });
  },

  async setDescription(
    board: Board,
    card: Card,
    description: string,
  ): Promise<void> {
    await api<Card>(board, "POST", `/cards/${card.itemId}/description`, {
      description,
    });
  },

  async createCard(board: Board, input: NewCardInput): Promise<Card> {
    return api<Card>(board, "POST", "/cards", {
      title: input.title,
      zone: input.zone ?? "",
      day: input.day ?? "",
      start: input.start ?? "",
      sprintStart: input.sprintStart ?? "",
      plan: input.plan ?? "",
      week: input.week ?? "",
      team: input.team ?? "",
      assignee: input.assigneeLogin ?? "",
      reviewOf: input.reviewOf ?? "",
    });
  },

  async deleteCard(board: Board, card: Card): Promise<void> {
    await api(board, "DELETE", `/cards/${card.itemId}`);
  },

  async moveCard(board: Board, card: Card, afterItemId: string | null): Promise<void> {
    await api<Card>(board, "POST", `/cards/${card.itemId}/move`, {
      afterId: afterItemId ?? "",
    });
  },

  async addNote(board: Board, card: Card, text: string): Promise<void> {
    await api(board, "POST", `/cards/${card.itemId}/note`, { text });
  },

  async editNote(
    board: Board,
    card: Card,
    note: Note,
    text: string,
  ): Promise<void> {
    await api<Card>(
      board,
      "PATCH",
      `/cards/${card.itemId}/notes/${encodeURIComponent(note.id)}`,
      { text },
    );
  },

  async deleteNote(board: Board, card: Card, note: Note): Promise<void> {
    await api<Card>(
      board,
      "DELETE",
      `/cards/${card.itemId}/notes/${encodeURIComponent(note.id)}`,
    );
  },

  async carryOver(board: Board, team: string | null): Promise<void> {
    // One server-side call: the backend advances the sprint pointer and carries
    // the unfinished cards concurrently, emitting watch events per card.
    await api(board, "POST", "/carry-over", { team: team ?? "" });
  },

  async carryWeek(
    board: Board,
    team: string | null,
    week: string,
  ): Promise<void> {
    // One server-side call: the backend moves the unfinished plan cards and
    // reseeds finished recurrent ones as fresh copies in the target week.
    await api(board, "POST", "/carry-week", { team: team ?? "", week });
  },
};
