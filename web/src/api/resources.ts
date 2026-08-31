// Wire shapes of the Kubernetes-style /api/v1 resources and the mapping onto
// the flat internal Card model the components render. This is the single place
// where the two vocabularies meet: zones are semantic on the wire
// (urgent/unplanned/planned/niceToHave) and colour keys (red/yellow/gray/green)
// inside the app; dates are nested on the wire and flat on the Card.

import type {
  Card,
  Note,
  SprintState,
  StageKey,
  ZoneKey,
} from "../providers/types";

// --- Zone vocabulary ---------------------------------------------------------

const ZONE_TO_SEMANTIC: Record<ZoneKey, string> = {
  red: "urgent",
  yellow: "unplanned",
  gray: "planned",
  green: "niceToHave",
};

const SEMANTIC_TO_ZONE: Record<string, ZoneKey> = {
  urgent: "red",
  unplanned: "yellow",
  planned: "gray",
  niceToHave: "green",
};

/** semanticZone maps a ZoneKey onto its API name ("" and undefined stay ""). */
export function semanticZone(zone: ZoneKey | "" | undefined): string {
  return zone ? ZONE_TO_SEMANTIC[zone] : "";
}

/** zoneFromSemantic maps an API zone name onto a ZoneKey (unknown/empty →
 * undefined, i.e. the no-zone default). */
export function zoneFromSemantic(name: string | undefined): ZoneKey | undefined {
  return name ? SEMANTIC_TO_ZONE[name] : undefined;
}

// --- Wire types ----------------------------------------------------------------

export interface CardResource {
  kind: string;
  metadata: {
    uid: string;
    author?: string;
    createdAt?: string;
  };
  spec: {
    title: string;
    description?: string;
    team?: string;
    zone?: string;
    assignees?: string[];
    progress?: number;
    stage?: string;
    recurrence?: string;
    dates?: { start?: string; end?: string; sprint?: string };
    plan?: { band?: string; week?: string };
    epic?: string;
    process?: string;
    task?: string;
    project?: string;
    mirrors?: { project: string; epic: string }[];
    reviewOf?: string;
    parent?: string;
  };
  status?: {
    complete?: boolean;
    inProgress?: boolean;
    overdue?: boolean;
    reviewedBy?: string;
    reviewRound?: number;
    /** The repository the card lives in; absent on an older server. */
    domain?: string;
    /** The board day the card reached done (yyyy-mm-dd); cleared on reopen. */
    doneAt?: string;
    leftAt?: string;
    links?: {
      kind: string;
      url: string;
      owner?: string;
      repo?: string;
      number?: number;
    }[];
  };
}

export interface SprintResource {
  kind: string;
  metadata: { team: string };
  spec: { current?: string; previous?: string };
}

export interface NoteResource {
  kind: string;
  metadata: {
    id: string;
    cardUid: string;
    author?: string;
    createdAt?: string;
    source: string;
  };
  spec: { text: string };
}

export interface OrderingResource {
  kind: string;
  spec: { uids: string[] };
}

export interface BoardResource {
  kind: string;
  metadata: {
    title?: string;
    url?: string;
    teams?: string[];
    projects?: string[];
    deadlines?: { week: string; project?: string }[];
    processes?: { name: string; project?: string }[];
    epics?: { name: string; project?: string; domain?: string }[];
    /** The roster; `name` is the display name, absent on a GitHub board. */
    members?: { login: string; avatarUrl?: string; name?: string }[];
    /** The repositories the board spans, primary first. */
    domains?: {
      name: string;
      writable?: boolean;
      members?: string[];
      personal?: boolean;
    }[];
    /** The repository a team or a project was declared in, for the entries
     *  outside the primary (which is never named). A board of one repository
     *  sends neither. */
    teamDomains?: Record<string, string>;
    projectDomains?: Record<string, string>;
    processDomains?: Record<string, string>;
    /** The visitor's personal board, when they linked one. `problem` says
     *  why the repository is not attached (the server cannot reach it) and
     *  `actionUrl` is the page that fixes it — installing the board's
     *  GitHub App on the repository. */
    personal?: { domain: string; url: string; problem?: string; actionUrl?: string };
  };
}

export interface CardListResource {
  kind: string;
  items: CardResource[] | null;
  weekly?: { progress: number };
}

export interface SprintListResource {
  kind: string;
  items: SprintResource[] | null;
}

export interface NoteListResource {
  kind: string;
  items: NoteResource[] | null;
}

/** PresenceResource is one user's live Me-view selection, broadcast over the
 * watch ("" card = cleared). Ephemeral: never part of the board data. */
export interface PresenceResource {
  login?: string;
  card?: string;
}

/** WatchFrame is one event on the /api/v1/watch WebSocket. */
export interface WatchFrame {
  type?: "ADDED" | "MODIFIED" | "DELETED";
  kind?: string;
  object?: unknown;
}

// --- Resource → internal model --------------------------------------------------

const STAGE_KEYS: Record<string, StageKey> = {
  locked: "locked",
  review: "review",
  recurrent: "recurrent",
  done: "done",
};

/** resourceToCard flattens a Card resource onto the internal Card model.
 * Notes are not part of the resource — they load from the notes subresource
 * and are preserved separately by the board's upsert. */
export function resourceToCard(res: CardResource): Card {
  const m = res.metadata;
  const spec = res.spec;
  const dates = spec.dates ?? {};
  const band = spec.plan?.band;
  return {
    itemId: m.uid,
    title: spec.title,
    assignees: spec.assignees ?? [],
    author: m.author || undefined,
    createdAt: m.createdAt || undefined,
    domain: res.status?.domain || undefined,
    zone: zoneFromSemantic(spec.zone),
    progress: spec.progress ?? 0,
    stage: spec.stage ? STAGE_KEYS[spec.stage] : undefined,
    team: spec.team || undefined,
    reviewOf: spec.reviewOf || undefined,
    parent: spec.parent || undefined,
    reviewRound: res.status?.reviewRound,
    overdue: res.status?.overdue ?? false,
    doneAt: res.status?.doneAt || undefined,
    leftAt: res.status?.leftAt || undefined,
    recurrence: spec.recurrence || undefined,
    day: dates.end || undefined,
    startDate: dates.start || undefined,
    sprintStart: dates.sprint || undefined,
    plan: band === "wed" || band === "fri" ? band : undefined,
    week: spec.plan?.week || undefined,
    epic: spec.epic || undefined,
    project: spec.project || undefined,
    mirrors: spec.mirrors?.length ? spec.mirrors : undefined,
    process: spec.process || undefined,
    task: spec.task || undefined,
    // A summary listing omits the body: description stays undefined ("not
    // loaded") and the boards fetch it on selection. A full resource with a
    // genuinely empty body also arrives undefined (the field is omitempty) —
    // the lazy fetch then answers once with "" and settles it.
    description: spec.description,
    linkRefs: res.status?.links?.map((l) => ({
      url: l.url,
      kind: l.kind === "issue" || l.kind === "pull" ? l.kind : "link",
      owner: l.owner,
      repo: l.repo,
      number: l.number,
    })),
  };
}

/** resourceToNote flattens a Note resource onto the internal Note model. */
export function resourceToNote(res: NoteResource): Note {
  return {
    id: res.metadata.id,
    body: res.spec.text,
    createdAt: res.metadata.createdAt ?? "",
    author: res.metadata.author || undefined,
    source: res.metadata.source === "draft" ? "draft" : "comment",
  };
}

/** sprintStateFrom maps one Sprint resource onto a SprintState. */
export function sprintStateFrom(res: SprintResource): SprintState {
  return {
    current: res.spec.current || null,
    previous: res.spec.previous || null,
  };
}

/** sprintStatesFrom maps a SprintList onto the per-team pointer record. */
export function sprintStatesFrom(
  items: SprintResource[],
): Record<string, SprintState> {
  const out: Record<string, SprintState> = {};
  for (const s of items) {
    out[s.metadata.team ?? ""] = sprintStateFrom(s);
  }
  return out;
}
