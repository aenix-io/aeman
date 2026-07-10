// Domain model shared by the UI components and the API provider. The flat Card
// shape is the internal component model; the provider maps the server's
// Kubernetes-style resources onto it at the client boundary (api/resources.ts),
// so components never see wire shapes or semantic zone names.

/** ZoneKey is the colour zone a card belongs to, in the Ford sense. */
import type { CardLink } from "../links";

export type ZoneKey = "gray" | "green" | "yellow" | "red";

/** StageKey is an explicit per-card status that recolours the progress bar. */
export type StageKey = "locked" | "review" | "recurrent" | "done";

/** Note is a dated work note attached to a card (an issue comment, or a line
 * stored in a draft issue's body when the card has no comment thread). */
export interface Note {
  id: string;
  body: string;
  createdAt: string;
  author?: string;
  source: "comment" | "draft";
}

/** CardEvent is one recorded action on a card (see board.Event server-side). */
export interface CardEvent {
  id: string;
  kind: string;
  actor?: string;
  from?: string;
  to?: string;
  at: string;
}

/** Card is a single project item (issue, PR or draft). */
export interface Card {
  itemId: string;
  /** Node id of the underlying issue/PR/draft, used for comments and assignees. */
  contentId?: string;
  title: string;
  isDraft: boolean;
  url?: string;
  number?: number;
  repository?: string;
  assignees: string[];
  /** GitHub login of the card's creator (draft-issue creator or issue author). */
  author?: string;
  /** ISO timestamp the item was added to the project (its age on the board). */
  createdAt?: string;
  zone?: ZoneKey;
  /** Readiness, 0..100. */
  progress?: number;
  /** Explicit status (locked/review/done) driving the progress-bar colour. */
  stage?: StageKey;
  /** Team label the card belongs to (a free-text field, used for filtering). */
  team?: string;
  /** On a review card, the itemId of the original card it reviews (review →
   * original; find the reverse by scanning for `reviewOf === original.itemId`). */
  reviewOf?: string;
  /** On a subtask, the itemId of the card it is grouped under. */
  parent?: string;
  /** On a review card, its review-round counter (>=2 shown; round 1 implicit). */
  reviewRound?: number;
  /** ISO date (yyyy-mm-dd) the card is planned to finish/be due on. */
  day?: string;
  /** ISO date (yyyy-mm-dd) the card starts on (set at creation). */
  startDate?: string;
  /** ISO date (yyyy-mm-dd) the sprint this card belongs to was started.
   * This is what the boards orient by (day/startDate are just metadata). */
  sprintStart?: string;
  /** Weekly-plan band ("wed"/"fri"); set = this is a founders' weekly-plan card. */
  plan?: "wed" | "fri";
  /** ISO date (yyyy-mm-dd) of the plan week this card belongs to (weekly cycle). */
  week?: string;
  /** Free-form card details (the body minus the appended action log). */
  description?: string;
  /** Work notes; undefined until loaded from the notes subresource. */
  notes?: Note[];
  /** Recorded activity events; undefined until loaded from the log subresource. */
  events?: CardEvent[];
}

/** NewCardInput describes a card to create on a board. The server fills the
 * defaults (dates, sprint join, first-sprint record) and applies admission. */
export interface NewCardInput {
  title: string;
  zone?: ZoneKey;
  day?: string | null;
  start?: string | null;
  plan?: "wed" | "fri" | null;
  week?: string | null;
  assigneeLogin?: string | null;
  team?: string | null;
  /** On a review card, the itemId of the original card it reviews. */
  reviewOf?: string | null;
  /** Force the team's sprint pointer to (re)start on the card's day. */
  startNewSprint?: boolean;
  /** Schedule the card for its day without joining any sprint (a "next
   * sprint" create); the next carry-over to reach its day adopts it. */
  noSprint?: boolean;
}

/** SprintState is a team's explicit sprint pointer: its current and previous
 * sprint start dates. */
export interface SprintState {
  current: string | null;
  previous: string | null;
}

export interface Board {
  owner: string;
  number: number;
  title: string;
  url: string;
  cards: Card[];
  /** The board's team roster (teams that have a sprint pointer), from GET
   *  /board — the source of truth now that cards load one view at a time. */
  teams: string[];
  /** Every distinct assignee on the board, from GET /board — the people roster
   *  for pickers (assign, review, view-as). */
  members: string[];
  /** Per-team sprint pointers, keyed by team name ("" = the no-team group). */
  sprintStates: Record<string, SprintState>;
}

/** BoardAddr addresses a board on the server (?owner=&project=). */
export type BoardAddr = Pick<Board, "owner" | "number">;

/** CardPatch is a partial spec edit mirroring PATCH /cards/{uid}: only the
 * present fields are sent, and an empty string clears a field. Including
 * dates.start runs the calendar rule server-side (the sprint is recomputed
 * from the start day); dates.end / dates.sprint alone stay granular. */
export interface CardPatch {
  title?: string;
  description?: string;
  team?: string;
  zone?: ZoneKey | "";
  assignees?: string[];
  progress?: number;
  /** "" clears the stage; "done" marks the card done (derived server-side). */
  stage?: StageKey | "";
  dates?: { start?: string; end?: string; sprint?: string };
  plan?: { band?: "wed" | "fri" | ""; week?: string };
  reviewOf?: string;
  /** Group under another card as a subtask ("" ungroups back to standalone). */
  parent?: string;
  /** On a review card, its review-round counter (>=2 shown; round 1 implicit). */
  reviewRound?: number;
}

/** CarryReport is what a carry-over / carry-week pass did — or would do on a
 * dry run, which feeds the confirm-dialog counts. */
export interface CarryReport {
  carried: number;
  reseeded: number;
}

/** Provider is the thin intent client of the aeman API: components state
 * intent, keep optimistic state, and reconcile from the returned resources
 * (and the watch stream). All board rules live server-side. */
export interface Provider {
  /** Board identity + per-team sprint pointers (no cards — the active view is
   *  loaded lazily via listCards). */
  loadBoard(owner: string, number: number): Promise<Board>;
  /** The cards of one view (GET /cards with a selector), so the UI loads only
   *  the active board — Me by default, a team's grid on demand. */
  listCards(board: BoardAddr, query: Record<string, string>): Promise<Card[]>;
  createCard(board: BoardAddr, input: NewCardInput): Promise<Card>;
  patchCard(board: BoardAddr, uid: string, patch: CardPatch): Promise<Card>;
  /** Hard delete; the server cascades to the linked review card. */
  deleteCard(board: BoardAddr, uid: string): Promise<void>;
  /** The smart ×: the server demotes / releases / deletes by the board rules. */
  removeCard(board: BoardAddr, uid: string, from: "grid" | "plan"): Promise<void>;
  /** Reposition card after afterId in the project order (null = top). */
  moveCard(board: BoardAddr, uid: string, afterId: string | null): Promise<void>;
  /** Reorder to sit right before another card: the server resolves the true
   *  global anchor, so callers rendering a filtered slice (a weekly band)
   *  don't need to know the full board order. */
  moveCardBefore(board: BoardAddr, uid: string, beforeId: string): Promise<void>;
  /** Push the scheduled day N days ahead of max(today, current start). */
  deferCard(board: BoardAddr, uid: string, days: number): Promise<Card>;
  /** Move to the implicit In Progress status (no stage, progress in [10,90]). */
  setInProgress(board: BoardAddr, uid: string): Promise<Card>;
  /** Create the linked review card (or reassign an existing one). */
  sendToReview(
    board: BoardAddr,
    uid: string,
    reviewer: string,
    day?: string,
    /** Zone for the review card; omitted = the original's zone. */
    zone?: ZoneKey,
  ): Promise<Card>;
  /** Delete the linked review card; returns the original. */
  removeReviewer(board: BoardAddr, uid: string): Promise<Card>;
  /** Take a plan card into work: assign + zone + join the sprint. */
  takeIntoPlan(
    board: BoardAddr,
    uid: string,
    engineer: string,
    zone: ZoneKey | undefined,
    day?: string,
  ): Promise<Card>;
  /** Release a card from the weekly plan (the plan-band × semantics). */
  releaseFromPlan(board: BoardAddr, uid: string): Promise<Card>;
  /** Advance a team's sprint to today and carry its unfinished cards forward.
   * dryRun reports the would-be counts without writing. team = null is the
   * no-team group. */
  carryOver(
    board: BoardAddr,
    team: string | null,
    dryRun?: boolean,
  ): Promise<CarryReport>;
  /** Pull a team's unfinished plan cards from earlier weeks into `week`;
   * finished recurrent ones reseed as fresh copies. dryRun = counts only. */
  carryWeek(
    board: BoardAddr,
    team: string | null,
    week: string,
    dryRun?: boolean,
  ): Promise<CarryReport>;
  /** Set a team's sprint pointer directly (current/previous start dates). */
  setSprintState(
    board: BoardAddr,
    team: string | null,
    current: string | null,
    previous: string | null,
  ): Promise<void>;
  /** URLs from the card's description: GitHub issue/PR refs (resolved with
   *  titles when possible) first, plain links after. */
  listLinks(board: BoardAddr, uid: string): Promise<CardLink[]>;
  /** The card's unified activity feed, split back into notes and events. */
  listLog(
    board: BoardAddr,
    uid: string,
  ): Promise<{ notes: Note[]; events: CardEvent[] }>;
  /** Share the caller's live card selection ("" clears) with other boards. */
  setPresence(board: BoardAddr, login: string, card: string | null): Promise<void>;
  listNotes(board: BoardAddr, uid: string): Promise<Note[]>;
  addNote(board: BoardAddr, uid: string, text: string): Promise<Note[]>;
  editNote(
    board: BoardAddr,
    uid: string,
    noteId: string,
    text: string,
  ): Promise<Note[]>;
  deleteNote(board: BoardAddr, uid: string, noteId: string): Promise<Note[]>;
}
