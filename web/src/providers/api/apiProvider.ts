// The thin intent client of aeman's Kubernetes-style API under /api/v1: reads
// compose the LIST responses into the frontend Board; writes state intent and
// return the resulting resources (mapped to the internal Card model), which the
// caller applies over its optimistic state. All board rules run server-side.

import createClient from "openapi-fetch";

import { clientId } from "../../api/client";
import { resolveCardId } from "../../api/pending";
import type { CardLink } from "../../links";
import {
  resourceToCard,
  resourceToNote,
  semanticZone,
  sprintStatesFrom,
  type BoardResource,
  type CardResource,
  type NoteListResource,
} from "../../api/resources";
import type { components, paths } from "../../api/schema";
import { splitDayLogs } from "../../daylog";
import { viewOf } from "../../viewquery";
import { createView, type ViewName } from "../../views";
import type { Member } from "../../users";
import type {
  Board,
  Card,
  CardDayLog,
  CardEvent,
  CardListing,
  CardLog,
  CardPatch,
  CarryReport,
  CustomerRef,
  NewCardInput,
  Priority,
  Note,
  Provider,
  ZoneKey,
} from "../types";

/** membersFrom shapes the board's people: everyone assigned anything, with
 *  what they carry (cards), what it weighs (points) and the points a week
 *  they get through. Used by the board load AND by the Load watch frame,
 *  which carries the same shape — those numbers move on every card write, so
 *  they arrive on their own rather than with the whole roster. */
export function membersFrom(
  raw: components["schemas"]["Member"][],
): Member[] {
  return raw.map((m) => ({
    login: m.login,
    avatarUrl: m.avatarUrl || undefined,
    name: m.name || undefined,
    carrying: m.carrying || undefined,
    load: m.load || undefined,
    capacity: m.capacity || undefined,
  }));
}

/** boardMetadata maps a board resource's metadata onto board state — used by
 *  loadBoard and by the Board watch frame alike, so a roster change arriving
 *  over the watch lands exactly as a fresh load would. */
export function boardMetadata(
  info: BoardResource,
): Pick<
  Board,
  | "title"
  | "url"
  | "teams"
  | "members"
  | "domains"
  | "teamDomains"
  | "customers"
> {
  return {
    title: info.metadata.title ?? "",
    url: info.metadata.url ?? "",
    teams: info.metadata.teams,
    members: membersFrom(info.metadata.members),
    // The repositories the board spans, primary first. An older server names
    // none; the UI then shows nothing of domains at all.
    domains: (info.metadata.domains ?? []).map((d) => ({
      name: d.name,
      writable: d.writable,
      members: d.members,
    })),
    // Which repository a team lives in.
    teamDomains: info.metadata.teamDomains ?? undefined,
    // The customer roster (ownership.md): what a card's customer ids name.
    customers: (info.metadata.customers ?? []).map((c) => ({ id: c.id, name: c.name })),
  };
}

/** ApiError is a non-2xx answer: the problem's detail (or the status text)
 *  with the status code, for callers that treat one code specially. */
export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    /** The page that fixes the refusal (installing the board's GitHub App),
     *  for the UI to render as a button rather than a URL in prose. */
    readonly actionUrl?: string,
    /** Which rule refused, by the server's stable name for it. */
    readonly code?: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

// viewedDay is the past day the board is showing, "" while it shows today.
// Set by the App as the day changes; every request then carries it, and the
// server refuses the writes that day is over for.
let viewedDay = "";

/** showingDay tells the provider which day the board is looking at ("" =
 *  today). It rides every request as X-Aeman-As-Of. */
export function showingDay(day: string): void {
  viewedDay = day;
}

// The BOARD the reader is standing on, as the serialised selector its listing
// used (viewquery.queryString). Every board GESTURE is made through it — the ×,
// the owner picker, "finished earlier" — so
// the server can answer "that card is not on this board" instead of acting on
// a card the person cannot see. Set by the App as the view changes.
let standing = "view=all";

/** standingOn tells the provider which board is open, with the scope its own
 *  listing used. */
export function standingOn(selector: string): void {
  standing = selector;
}

// standingScope is the address of one of that board's presses on a card: the
// board is a path segment, the scope its own listing used stays a query, and
// together they let the server answer "that card is not on this board" rather
// than act on a card the person cannot see. `standing` is the serialised
// selector (viewquery.queryString), so its values arrive percent-encoded and
// are decoded back here for the client to encode again.
function standingScope(uid: string): {
  path: { view: string; uid: string };
  query: Record<string, string>;
} {
  const query: Record<string, string> = {};
  for (const part of standing.split("&")) {
    const eq = part.indexOf("=");
    if (eq < 0 || part.startsWith("view=")) {
      continue;
    }
    query[part.slice(0, eq)] = decodeURIComponent(part.slice(eq + 1));
  }
  return { path: { view: viewOf(standing), uid }, query };
}

// The server serves exactly one board, so nothing addresses it. The base is
// absolute because a request is built as `new Request(url)`, which outside a
// browser has no document to resolve a relative path against; `fetch` is
// looked up per call rather than captured, because this client is built when
// the module loads and a test installs its own afterwards.
const client = createClient<paths>({
  baseUrl: `${globalThis.location?.origin ?? "http://localhost"}/api/v1`,
  fetch: (request) => globalThis.fetch(request),
});

client.use({
  onRequest({ request }) {
    // X-Aeman-Client keys watch echo suppression: the server skips this tab's
    // own watch connection when broadcasting the changes it makes here.
    request.headers.set("X-Aeman-Client", clientId);
    // X-Aeman-As-Of says which day the person is LOOKING AT when that is a
    // past one: the server then refuses a write to a card the day is over
    // for, so a UI path that forgot fails loudly instead of writing today's
    // board from a picture of a day that ended.
    if (viewedDay) {
      request.headers.set("X-Aeman-As-Of", viewedDay);
    }
    return request;
  },
  async onResponse({ response }) {
    // A refusal is raised here rather than returned as `{error}` for the
    // caller to unwrap: a method that answers void has nothing to unwrap, and
    // the unwrap somebody forgets is the one that swallows the 401 the
    // sign-out guard (session.ts) is waiting for.
    if (!response.ok) {
      throw await problemFrom(response);
    }
  },
});

// problemFrom reads a refusal: the RFC 9457 problem body the API answers with,
// or the status alone when the answer is not one (a proxy's own 502 page).
async function problemFrom(res: Response): Promise<ApiError> {
  let detail = "";
  let actionUrl: string | undefined;
  let code: string | undefined;
  try {
    const problem = (await res.json()) as Partial<components["schemas"]["Problem"]>;
    detail = problem.detail ?? "";
    actionUrl = problem.actionUrl || undefined;
    code = problem.code || undefined;
  } catch {
    // Not a problem body; the status is all there is left to say.
  }
  return new ApiError(
    detail || res.statusText || `HTTP ${res.status}`,
    res.status,
    actionUrl,
    code,
  );
}

// answered unwraps a call that has a resource to give. The middleware has
// already raised a refusal, so an absent body here is a 2xx that broke the
// document's own word.
async function answered<T>(call: Promise<{ data?: T }>): Promise<T> {
  const { data } = await call;
  if (data === undefined) {
    throw new Error("the API answered with no body");
  }
  return data;
}

// inDomain is the optional `domain` of a declare request: the repository a new
// team is filed in. Omitted, the server picks the primary.
function inDomain(domain?: string): { domain?: string } {
  return domain ? { domain } : {};
}

// cardFrom maps the Card a call answers with onto the internal model.
async function cardFrom(call: Promise<{ data?: CardResource }>): Promise<Card> {
  return resourceToCard(await answered(call));
}

// notesFrom maps the whole thread a note call answers with.
async function notesFrom(
  call: Promise<{ data?: NoteListResource }>,
): Promise<Note[]> {
  return (await answered(call)).items.map(resourceToNote);
}

// patchBody translates a CardPatch onto the wire shape: only present fields go
// out ("" clears), zones travel under their semantic names.
function patchBody(patch: CardPatch): components["schemas"]["CardPatch"] {
  const body: components["schemas"]["CardPatch"] = {};
  if (patch.title !== undefined) {
    body.title = patch.title;
  }
  if (patch.description !== undefined) {
    body.description = patch.description;
  }
  if (patch.team !== undefined) {
    body.team = patch.team;
  }
  if (patch.owner !== undefined) {
    body.owner = patch.owner;
  }
  if (patch.customers !== undefined) {
    body.customers = patch.customers;
  }
  if (patch.dependsOn !== undefined) {
    body.dependsOn = patch.dependsOn;
  }
  if (patch.zone !== undefined) {
    body.zone = semanticZone(patch.zone);
  }
  if (patch.size !== undefined) {
    body.size = patch.size;
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
    const dates: components["schemas"]["CardDatesPatch"] = {};
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
  if (patch.parked !== undefined) {
    body.parked = patch.parked;
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
  async loadBoard(query: Record<string, string> = {}): Promise<Board> {
    const [info, sprints] = await Promise.all([
      answered(client.GET("/board", { params: { query } })),
      answered(client.GET("/sprints", { params: { query } })),
    ]);
    return {
      // Cards are loaded per view via listCards; the initial set arrives right
      // after this from the App's first view fetch.
      cards: [],
      ...boardMetadata(info),
      sprintStates: sprintStatesFrom(sprints.items),
    };
  },

  async listCards(
    query: Record<string, string>,
  ): Promise<CardListing> {
    // Listings are board rows (the server-side default): card bodies live
    // behind getCard, and status.links stands in for the row's links icon.
    // They come back in board order; the Ordering watch events keep the local
    // copy sorted between re-lists.
    //
    // The board the selector names is the path segment; the rest narrows it.
    const { view, ...narrowing } = query;
    const list = await answered(
      client.GET("/views/{view}/cards", {
        params: { path: { view: view || "all" }, query: narrowing },
      }),
    );
    return {
      cards: list.items.map(resourceToCard),
      // Set when the server answered with a past day's board rather than
      // today's: the client freezes on that, and says which day it shows.
      asOf: list.asOf,
      boardOrder: list.boardOrder,
    };
  },

  async getCard(uid: string): Promise<Card> {
    return cardFrom(
      client.GET("/cards/{uid}", { params: { path: { uid: await resolveCardId(uid) } } }),
    );
  },

  async createCard(input: NewCardInput): Promise<Card> {
    // The board the card was typed into says what it means — the server fills
    // in the rest and refuses the fields that board does not own (views.ts).
    const view = createView(input, viewOf(standing) as ViewName);
    const body: components["schemas"]["CreateCardRequest"] = {
      title: input.title,
      team: input.team ?? "",
      zone: semanticZone(input.zone),
      size: input.size ?? "",
      assignees: input.assigneeLogin ? [input.assigneeLogin] : [],
      reviewOf: input.reviewOf ?? "",
      // A parent may still be an optimistic tmp id: wait for the real uid.
      parent: input.parent ? await resolveCardId(input.parent) : "",
    };
    if (input.type) {
      body.type = input.type; // a planning card: the Roadmap's create
    }
    // A day card passes its start/end and the server joins (or records) the
    // sprint itself.
    if (input.start || input.day) {
      body.dates = { start: input.start ?? "", end: input.day ?? "" };
    }
    // Born IN the backlog: no dates, no sprint, parked on its team, last
    // (K3). A Backlog column's create says so by being made THERE
    // (views.createView), so nothing in the body has to.
    if (input.startNewSprint !== undefined) {
      body.startNewSprint = input.startNewSprint;
    }
    if (input.noSprint) {
      body.noSprint = true;
    }
    if (input.owner) {
      body.owner = input.owner;
    }
    if (input.customers?.length) {
      body.customers = input.customers;
    }
    // Born where it was typed (the backlog's insert "+"): the place rides
    // the create, one request.
    if (input.after) {
      body.after = await resolveCardId(input.after);
    } else if (input.before) {
      body.before = await resolveCardId(input.before);
    }
    return cardFrom(
      client.POST("/views/{view}/cards", { params: { path: { view } }, body }),
    );
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
    return cardFrom(
      client.PATCH("/cards/{uid}", { params: { path: { uid } }, body: patchBody(patch) }),
    );
  },

  async deleteCard(uid: string): Promise<void> {
    uid = await resolveCardId(uid);
    await client.DELETE("/cards/{uid}", { params: { path: { uid } } });
  },

  async removeCard(uid: string, intent?: "unassign" | "off-board"): Promise<void> {
    uid = await resolveCardId(uid);
    await client.POST("/views/{view}/cards/{uid}/actions/remove", {
      params: standingScope(uid),
      body: intent ? { intent } : {},
    });
  },

  async moveCard(
    uid: string,
    afterId: string | null,
  ): Promise<void> {
    uid = await resolveCardId(uid);
    const after = afterId ? await resolveCardId(afterId) : "";
    await client.POST("/cards/{uid}/actions/move", {
      params: { path: { uid } },
      body: { after },
    });
  },

  async moveCardBefore(
    uid: string,
    beforeId: string,
  ): Promise<void> {
    uid = await resolveCardId(uid);
    const before = await resolveCardId(beforeId);
    await client.POST("/cards/{uid}/actions/move", {
      params: { path: { uid } },
      body: { before },
    });
  },

  async deferCard(uid: string, days: number): Promise<Card> {
    uid = await resolveCardId(uid);
    return cardFrom(
      client.POST("/cards/{uid}/actions/defer", {
        params: { path: { uid } },
        body: { days },
      }),
    );
  },

  async setInProgress(uid: string): Promise<Card> {
    uid = await resolveCardId(uid);
    return cardFrom(
      client.POST("/cards/{uid}/actions/in-progress", { params: { path: { uid } } }),
    );
  },

  async reopen(uid: string): Promise<Card> {
    uid = await resolveCardId(uid);
    return cardFrom(
      client.POST("/cards/{uid}/actions/reopen", { params: { path: { uid } } }),
    );
  },

  async sendToReview(
    uid: string,
    reviewer: string,
    day?: string,
    zone?: ZoneKey,
  ): Promise<Card> {
    uid = await resolveCardId(uid);
    return cardFrom(
      client.POST("/cards/{uid}/actions/send-to-review", {
        params: { path: { uid } },
        body: { reviewer, day: day ?? "", zone: semanticZone(zone) },
      }),
    );
  },

  async removeReviewer(uid: string): Promise<Card> {
    uid = await resolveCardId(uid);
    return cardFrom(
      client.POST("/cards/{uid}/actions/remove-reviewer", { params: { path: { uid } } }),
    );
  },


  async finishedEarlier(uid: string): Promise<Card> {
    uid = await resolveCardId(uid);
    return cardFrom(
      client.POST("/views/{view}/cards/{uid}/actions/finished-earlier", {
        params: standingScope(uid),
      }),
    );
  },


  async setTeamCapacity(team: string, points: number): Promise<void> {
    await client.POST("/teams/actions/capacity", { body: { team, points } });
  },

  async takeCard(uid: string): Promise<Card> {
    uid = await resolveCardId(uid);
    return cardFrom(
      client.POST("/views/{view}/cards/{uid}/actions/take", {
        params: standingScope(uid),
      }),
    );
  },

  async setPriority(uid: string, priority: Priority | ""): Promise<Card> {
    uid = await resolveCardId(uid);
    return cardFrom(client.POST("/cards/{uid}/actions/priority", { params: { path: { uid } }, body: { priority } }));
  },

  async previewUnassign(uid: string): Promise<Card[]> {
    uid = await resolveCardId(uid);
    const res = await answered(client.POST("/cards/{uid}/actions/unassign-work", { params: { path: { uid } }, body: { dryRun: true } }));
    return (res.items ?? []).map(resourceToCard);
  },

  async unassignWork(uid: string, uids?: string[]): Promise<Card[]> {
    uid = await resolveCardId(uid);
    const body = uids ? { uids: await Promise.all(uids.map(resolveCardId)) } : undefined;
    const res = await answered(client.POST("/cards/{uid}/actions/unassign-work", { params: { path: { uid } }, body }));
    return (res.items ?? []).map(resourceToCard);
  },

  async ownCard(uid: string, team: string, person: string): Promise<Card> {
    uid = await resolveCardId(uid);
    return cardFrom(
      client.POST("/views/{view}/cards/{uid}/actions/own", {
        params: standingScope(uid),
        body: { team, person },
      }),
    );
  },


  async carryOver(
    team: string | null,
    dryRun = false,
  ): Promise<CarryReport> {
    return answered(
      client.POST("/sprints/actions/carry-over", {
        body: { team: team ?? "", dryRun },
      }),
    );
  },

  async reorderTeams(teams: string[]): Promise<void> {
    await client.POST("/sprints/actions/reorder-teams", { body: { teams } });
  },

  async deleteTeam(team: string): Promise<void> {
    await client.POST("/sprints/actions/delete-team", { body: { team } });
  },

  async addCustomer(name: string): Promise<CustomerRef> {
    const data = await answered(client.POST("/customers", { body: { name } }));
    return { id: data.id, name: data.name };
  },

  async renameCustomer(id: string, to: string): Promise<void> {
    await client.POST("/customers/actions/rename", { body: { customer: id, to } });
  },

  async reorderCustomers(ids: string[]): Promise<void> {
    await client.POST("/customers/actions/reorder", { body: { customers: ids } });
  },

  async deleteCustomer(id: string): Promise<void> {
    await client.POST("/customers/actions/delete", { body: { customer: id } });
  },

  async renameTeam(from: string, to: string): Promise<void> {
    await client.POST("/teams/actions/rename", { body: { team: from, to } });
  },


  async setSprintState(
    team: string | null,
    current: string | null,
    previous: string | null,
    domain?: string,
  ): Promise<void> {
    await client.PATCH("/sprints", {
      body: {
        team: team ?? "",
        current: current ?? "",
        previous: previous ?? "",
        ...inDomain(domain),
      },
    });
  },

  async listLog(uid: string): Promise<CardLog> {
    uid = await resolveCardId(uid);
    const list = await answered(
      client.GET("/cards/{uid}/log", { params: { path: { uid } } }),
    );
    const notes: Note[] = [];
    const events: CardEvent[] = [];
    for (const it of list.items) {
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
          // A note the server answered with is a made comment; "draft" is the
          // optimistic tmp- note that has not come back yet. Matches the
          // day-feed splitter (daylog.ts).
          source: "comment",
        });
      }
    }
    return { notes, events, truncatedBefore: list.truncatedBefore || undefined };
  },

  async listDayLogs(uids: string[], day: string): Promise<Record<string, CardDayLog>> {
    // The server bounds one request at 200 cards; a larger set is asked in
    // slices (a day board rarely needs more than one).
    const slice = 200;
    const batches: string[][] = [];
    for (let i = 0; i < uids.length; i += slice) {
      batches.push(uids.slice(i, i + slice));
    }
    const answers = await Promise.all(
      batches.map((batch) =>
        answered(
          client.GET("/logs", { params: { query: { day, uids: batch.join(",") } } }),
        ),
      ),
    );
    const out: Record<string, CardDayLog> = {};
    for (const a of answers) {
      Object.assign(out, splitDayLogs(a.cards));
    }
    return out;
  },

  async listLinks(uid: string): Promise<CardLink[]> {
    uid = await resolveCardId(uid);
    const list = await answered(
      client.GET("/cards/{uid}/links", { params: { path: { uid } } }),
    );
    return list.items;
  },

  async setPresence(
    login: string,
    card: string | null,
  ): Promise<void> {
    const uid = card && !card.startsWith("tmp-") ? card : "";
    await client.POST("/presence", { body: { login, card: uid } });
  },

  async listNotes(uid: string): Promise<Note[]> {
    uid = await resolveCardId(uid);
    return notesFrom(client.GET("/cards/{uid}/notes", { params: { path: { uid } } }));
  },

  async addNote(uid: string, text: string): Promise<Note[]> {
    uid = await resolveCardId(uid);
    return notesFrom(
      client.POST("/cards/{uid}/notes", { params: { path: { uid } }, body: { text } }),
    );
  },

  async editNote(
    uid: string,
    noteId: string,
    text: string,
  ): Promise<Note[]> {
    uid = await resolveCardId(uid);
    return notesFrom(
      client.PATCH("/cards/{uid}/notes/{noteId}", {
        params: { path: { uid, noteId } },
        body: { text },
      }),
    );
  },

  async deleteNote(
    uid: string,
    noteId: string,
  ): Promise<Note[]> {
    uid = await resolveCardId(uid);
    return notesFrom(
      client.DELETE("/cards/{uid}/notes/{noteId}", {
        params: { path: { uid, noteId } },
      }),
    );
  },
};
