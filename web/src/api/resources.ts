// Wire shapes of the Kubernetes-style /api/v1 resources and the mapping onto
// the flat internal Card model the components render. This is the single place
// where the two vocabularies meet: zones are semantic on the wire
// (urgent/unplanned/planned/niceToHave) and colour keys (red/yellow/gray/green)
// inside the app; dates are nested on the wire and flat on the Card.
//
// The wire half is not written here: it is api/schema.d.ts, generated from
// api/openapi.yaml (`npm run generate`), so a field the server renames or
// drops reaches the mappers below as a compile error rather than as undefined
// at runtime.

import type {
  Card,
  CardType,
  Note,
  SprintState,
  StageKey,
  ZoneKey,
} from "../providers/types";
import { sizeFromWire } from "../size";
import type { components } from "./schema";

// --- Zone vocabulary ---------------------------------------------------------

// WireZone is the band vocabulary as the document states it, "" being "no
// band". Taking it from a request schema rather than restating it here is what
// makes a renamed band a compile error in this table instead of a refusal
// nobody sees until a card is dropped.
type WireZone = NonNullable<components["schemas"]["CardPatch"]["zone"]>;

const ZONE_TO_SEMANTIC: Record<ZoneKey, Exclude<WireZone, "">> = {
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
export function semanticZone(zone: ZoneKey | "" | undefined): WireZone {
  return zone ? ZONE_TO_SEMANTIC[zone] : "";
}

/** zoneFromSemantic maps an API zone name onto a ZoneKey (unknown/empty →
 * undefined, i.e. the no-zone default). */
export function zoneFromSemantic(name: string | undefined): ZoneKey | undefined {
  return name ? SEMANTIC_TO_ZONE[name] : undefined;
}

// --- Wire types ----------------------------------------------------------------

type Schemas = components["schemas"];

export type CardResource = Schemas["Card"];
export type SprintResource = Schemas["Sprint"];
export type NoteResource = Schemas["Note"];
export type BoardResource = Schemas["Board"];
export type NoteListResource = Schemas["NoteList"];

/** WatchFrame is one event on a board's watch WebSocket
 *  (/api/v1/views/{view}/watch), discriminated by `kind`. */
export type WatchFrame = Schemas["WatchFrame"];

// --- Resource → internal model --------------------------------------------------

// Every stage the server can send. A key missing here does not fall back —
// it becomes NO stage, and the card renders as ordinary work: `refuse`
// arrived, was dropped on the way in, and its black bar came out green.
const STAGE_KEYS: Record<string, StageKey> = {
  locked: "locked",
  review: "review",
  recurrent: "recurrent",
  refuse: "refuse",
  done: "done",
};

/** resourceToCard flattens a Card resource onto the internal Card model.
 * Notes are not part of the resource — they load from the notes subresource
 * and are preserved separately by the board's upsert. */
export function resourceToCard(res: CardResource): Card {
  const m = res.metadata;
  const spec = res.spec;
  const dates = spec.dates ?? {};
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
    owner: spec.owner || undefined,
    customers: spec.customers?.length ? spec.customers : undefined,
    effectiveTeam: res.status?.effectiveTeam || undefined,
    effectiveCustomers: res.status?.effectiveCustomers?.length ? res.status.effectiveCustomers : undefined,
    reviewOf: spec.reviewOf || undefined,
    parent: spec.parent || undefined,
    type: spec.type || undefined,
    lineage: res.status?.lineage?.map((l) => ({
      uid: l.uid,
      type: (l.type as CardType | undefined) || undefined,
      title: l.title,
      // The ancestor's end: the Backlog board's due date for a task under it.
      ...(l.end ? { end: l.end } : {}),
    })),
    dueDate: res.status?.dueDate || undefined,
    reviewed: res.status?.reviewed
      ? {
          uid: res.status.reviewed.uid,
          title: res.status.reviewed.title,
          ...(res.status.reviewed.assignees?.length ? { assignees: res.status.reviewed.assignees } : {}),
        }
      : undefined,
    hierarchy: res.status?.hierarchy
      ? { status: res.status.hierarchy.status, done: res.status.hierarchy.done, total: res.status.hierarchy.total }
      : undefined,
    reviewRound: res.status?.reviewRound,
    asOf: res.status?.asOf,
    doneAt: res.status?.doneAt || undefined,
    recurrence: spec.recurrence || undefined,
    day: dates.end || undefined,
    startDate: dates.start || undefined,
    sprintStart: dates.sprint || undefined,
    parked: spec.parked || undefined,
    todo: spec.todo || undefined,
    size: sizeFromWire(spec.size),
    // A summary listing omits the body: description stays undefined ("not
    // loaded") and the boards fetch it on selection. A full resource with a
    // genuinely empty body also arrives undefined (the field is omitempty) —
    // the lazy fetch then answers once with "" and settles it.
    description: spec.description,
    linkRefs: res.status?.links?.map((l) => ({
      url: l.url,
      kind: l.kind,
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
    capacity: res.spec.capacity,
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
