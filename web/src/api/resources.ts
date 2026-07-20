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
    contentId?: string;
    isDraft?: boolean;
    url?: string;
    number?: number;
    repository?: string;
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
    reviewOf?: string;
    parent?: string;
  };
  status?: {
    complete?: boolean;
    inProgress?: boolean;
    reviewedBy?: string;
    reviewRound?: number;
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
  metadata: { title?: string; url?: string; teams?: string[]; members?: string[] };
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
    contentId: m.contentId || undefined,
    title: spec.title,
    isDraft: m.isDraft ?? false,
    url: m.url || undefined,
    number: m.number,
    repository: m.repository || undefined,
    assignees: spec.assignees ?? [],
    author: m.author || undefined,
    createdAt: m.createdAt || undefined,
    zone: zoneFromSemantic(spec.zone),
    progress: spec.progress ?? 0,
    stage: spec.stage ? STAGE_KEYS[spec.stage] : undefined,
    team: spec.team || undefined,
    reviewOf: spec.reviewOf || undefined,
    parent: spec.parent || undefined,
    reviewRound: res.status?.reviewRound,
    recurrence: spec.recurrence || undefined,
    day: dates.end || undefined,
    startDate: dates.start || undefined,
    sprintStart: dates.sprint || undefined,
    plan: band === "wed" || band === "fri" ? band : undefined,
    week: spec.plan?.week || undefined,
    description: spec.description ?? "",
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
