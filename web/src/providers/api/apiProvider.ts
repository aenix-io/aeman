// A Provider backed by aeman's own REST API under /api/v1, instead of talking to
// GitHub GraphQL directly. It is still GitHub-backed (the server proxies to
// Projects v2), so it keeps the "github" ProviderId. Live board updates arrive
// out-of-band over a WebSocket handled elsewhere; loadCards therefore always
// asks for a full reload rather than patching individual cards.

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
  const init: RequestInit = { method };
  if (body !== undefined) {
    init.headers = { "Content-Type": "application/json" };
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
    _board: Board,
    _card: Card,
    _description: string,
  ): Promise<void> {
    // TODO: no REST endpoint for setting a card's description yet.
    throw new Error("setDescription is not supported by the API provider yet");
  },

  async createCard(board: Board, input: NewCardInput): Promise<Card> {
    // The create endpoint only accepts these keys, and its decoder rejects
    // unknown fields, so the other NewCardInput fields are intentionally omitted.
    return api<Card>(board, "POST", "/cards", {
      title: input.title,
      zone: input.zone ?? "",
      day: input.day ?? "",
      team: input.team ?? "",
      assignee: input.assigneeLogin ?? "",
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
    _board: Board,
    _card: Card,
    _note: Note,
    _text: string,
  ): Promise<void> {
    // TODO: no REST endpoint for editing a note yet.
    throw new Error("editNote is not supported by the API provider yet");
  },

  async deleteNote(_board: Board, _card: Card, _note: Note): Promise<void> {
    // TODO: no REST endpoint for deleting a note yet.
    throw new Error("deleteNote is not supported by the API provider yet");
  },
};
