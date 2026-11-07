// Domain model shared by the UI components and the API provider. The flat Card
// shape is the internal component model; the provider maps the server's
// Kubernetes-style resources onto it at the client boundary (api/resources.ts),
// so components never see wire shapes or semantic zone names.

import type { DomainInfo } from "../domains";
import type { CardLink } from "../links";
import type { Member } from "../users";

/** ZoneKey is the colour zone a card belongs to, in the Ford sense. */
export type ZoneKey = "gray" | "green" | "yellow" | "red";

/** SizeKey is what somebody said a card weighs: S, M, L or XL. The points
 *  (1/2/4/8) are derived in size.ts, never stored. */
export type SizeKey = "S" | "M" | "L" | "XL";

/** StageKey is an explicit per-card status that recolours the progress bar. */
export type StageKey = "locked" | "review" | "recurrent" | "refuse" | "done";

/** CardType is a PLANNING card's level; a task has none. Mirrors the
 *  board.Type* constants. */
export type CardType = "goal" | "initiative" | "epic";

/** LineageRef is one ancestor of a card (status.lineage). */
export interface LineageRef {
  uid: string;
  type?: CardType;
  title: string;
  /** The ancestor's end date, when it has one. The Backlog board shows the
   *  nearest planning card's as the due date of the task under it. */
  end?: string;
}

/** ReviewedRef is the card a review card reviews (status.reviewed). */
export interface ReviewedRef {
  uid: string;
  title: string;
  /** Whose the reviewed card is: the review's counterpart. */
  assignees?: string[];
}

/** HierarchyStatus is a planning card's derived standing (status.hierarchy):
 *  never stored, read off the tasks under it on every listing. */
export interface HierarchyStatus {
  status: "no-items" | "todo" | "in-progress" | "done";
  done: number;
  total: number;
}

/** Note is a dated work note attached to a card (a line of its log). */
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

/** Card is a single board item. */
export interface Card {
  itemId: string;
  title: string;
  assignees: string[];
  /** Login of the card's creator. */
  author?: string;
  /** ISO timestamp the card was created (its age on the board). */
  createdAt?: string;
  /** The repository the card lives in (one of the board's domains). Absent on
   *  an older server, which means the primary. */
  domain?: string;
  zone?: ZoneKey;
  /** What somebody said the card weighs — S, M, L or XL — set on a daily
   *  sync or by a sizing tool. The points a board sums are derived from it
   *  (size.ts), never stored. Absent = unsized. */
  size?: SizeKey;
  /** Readiness, 0..100. */
  progress?: number;
  /** Explicit status (locked/review/done) driving the progress-bar colour. */
  stage?: StageKey;
  /** Team label the card belongs to (a free-text field, used for filtering).
   *  On a goal, an initiative or an epic: the team that carries it out. */
  team?: string;
  /** The one person who answers for a goal, an initiative or an epic
   *  (ownership.md, O1); never inherited, on no day board. */
  owner?: string;
  /** Ids of the board's customers this card itself names (O5). */
  customers?: string[];
  /** The server's effective team (O3) and customers (O4): computed, never
   *  written. ownership.ts works them out the same way from held cards. */
  effectiveTeam?: string;
  effectiveCustomers?: string[];
  /** On a review card, the itemId of the original card it reviews (review →
   * original; find the reverse by scanning for `reviewOf === original.itemId`). */
  reviewOf?: string;
  /** The itemId of the card this one sits under — the ONE link of the
   *  hierarchy. What it means is the parent's type: under a task this card is
   *  a SUBTASK (subtasks.ts isSubtask); under a goal, an initiative or an
   *  epic it is filed in the hierarchy and nothing else about it changes. */
  parent?: string;
  /** The card's level. Absent for a task; a PLANNING card — drawn on the
   *  Roadmap and on no day board — says which (docs/design/hierarchy.md). */
  type?: CardType;
  /** Every ancestor, root first: the breadcrumb. Absent on a root. The last
   *  entry is the parent itself, and its type is what tells a subtask from a
   *  task under an epic without a lookup. */
  lineage?: LineageRef[];
  /** On a REVIEW card, the card it reviews (status.reviewed): its uid, title
   *  and people, so the drawer and the backlog row can name it, open it and
   *  draw whose it is. */
  reviewed?: ReviewedRef;
  /** A task's due date, the server's (status.dueDate): its nearest
   *  planning card's end. */
  dueDate?: string;
  /** A planning card's derived standing, from the tasks under it. Absent on a
   *  task. */
  hierarchy?: HierarchyStatus;
  /** On a review card, its review-round counter (>=2 shown; round 1 implicit). */
  reviewRound?: number;
  /** A recurrent card's reseed cycle: "" = every sprint, "week" | "month". */
  recurrence?: string;
  /** ISO date (yyyy-mm-dd) the card is planned to finish/be due on. */
  day?: string;
  /** ISO date (yyyy-mm-dd) the card starts on (set at creation). */
  startDate?: string;
  /** ISO date (yyyy-mm-dd) the sprint this card belongs to was started.
   * This is what the boards orient by (day/startDate are just metadata). */
  sprintStart?: string;
  /** PARKED in its team's backlog rather than in work (K1): a task is one or
   *  the other — in work when it has a date or a sprint, parked otherwise —
   *  and a parked card is on no day board. Every team has a backlog and
   *  nothing declares it (docs/design/backlog.md). */
  parked?: boolean;
  /** The column this card is filed under: scope + project TOGETHER, since scope
   *  names repeat across projects. Its start date's week is the row. */
  scope?: string;
  project?: string;
  /** A card scheduled ahead — a slot — still open past the day it was owed
   *  by. Derived by the server from its dates. */
  overdue?: boolean;
  /** The moment this card is FROM, when it is a record rather than the live
   *  card: its team's sprint has moved on past the day being looked at. The
   *  board shows it as it was and refuses to change it (G60). */
  asOf?: string;
  /** The board day (yyyy-mm-dd) the card reached done; cleared when it
   *  reopens. */
  doneAt?: string;
  /** Free-form card details (the body minus the appended action log).
   *  Undefined until loaded: listings are the board-row shape without the
   *  body, and the boards fetch it when a card is selected or opened. */
  description?: string;
  /** References extracted from the description server-side (unresolved: no
   *  titles). Summary listings carry these INSTEAD of the description, so a
   *  row shows its links icon without the body. */
  linkRefs?: CardLink[];
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
  size?: SizeKey;
  day?: string | null;
  start?: string | null;
  /** Start the card in its team's backlog: no dates, no sprint (K3). */
  parked?: boolean | null;
  scope?: string | null;
  project?: string | null;
  assigneeLogin?: string | null;
  team?: string | null;
  /** On a review card, the itemId of the original card it reviews. */
  reviewOf?: string | null;
  /** The card to sit under on create: a task to be a subtask of, or a
   *  planning card to be filed under (the level rule, H2). */
  parent?: string | null;
  /** A PLANNING card's level — a goal, an initiative or an epic. Set, the
   *  create is the Roadmap's (views.ts createView) whatever board is open;
   *  absent, a task. Fixed for the card's life. */
  type?: CardType | null;
  /** A TASK made under a planning card (the add in an epic's pane): the
   *  create is the Roadmap's, whatever board is open — the task is parked in
   *  the no-team backlog until taken into work (K3). Client-side only; never
   *  sent. */
  underPlanning?: boolean | null;
  /** A planning card's owner, and the customer ids a card is born for
   *  (ownership.md). */
  owner?: string | null;
  customers?: string[] | null;
  /** Where a backlog card is born (the insert "+"): right after the card
   *  `after` names, or right before the card `before` names — one or
   *  neither. The create places it in the same request. */
  after?: string | null;
  before?: string | null;
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
  /** What a week of the team's plan is weighed against: the POINTS a week it
   *  gets through, a number somebody SET and never derived — absent when
   *  nobody has said. Mirrors board.PointsAWeekOf. */
  capacity?: {
    points?: number;
  };
}

/** DeadlineRef is one deadline line: the week it sits on and whose it is. */
export interface DeadlineRef {
  week: string;
  project: string;
}

/** ScopeRef is one Project-board column: its name and the project that owns
 *  it. The pair travels together — a column is meaningless without knowing
 *  which project's grid it belongs in. */
export interface ScopeRef {
  name: string;
  project: string;
  /** The repository the column was declared in — a NAME, compared against
   *  the board's own primary (domains[0]), never tested for emptiness; an
   *  absent stamp means the primary too. It cannot be computed from the
   *  project: one project NAME may be declared in two repositories with
   *  its columns merged under one entry, and it is the column that decides
   *  whether a card may stand in it. */
  domain?: string;
}

/** Board is the one board the server serves: its identity and structure from
 *  GET /board, plus the cards of the active view. */
export interface Board {
  title: string;
  url: string;
  cards: Card[];
  /** The board's team roster (teams that have a sprint pointer), from GET
   *  /board — the source of truth now that cards load one view at a time. */
  teams: string[];
  /** The Project board's projects, in board order — the top grouping: a
   *  project owns scope columns. */
  projects: string[];
  /** The Project board's scope columns, in board order, each naming the project
   *  that owns it. An empty project means the column belongs to none. */
  scopes: ScopeRef[];
  /** The deadline lines: the week (a Monday) each sits on and the project it
   *  belongs to. A project holds at most one per week. */
  deadlines: DeadlineRef[];
  /** The people roster from GET /board (login + avatar) for pickers (assign,
   *  review, view-as) and for every avatar the boards draw. */
  members: Member[];
  /** The repositories the board spans, primary first; a single entry on a
   *  one-repository board, none from an older server. */
  domains: DomainInfo[];
  /** Which repository each team and project was declared in, for the entries
   *  outside the primary. A card's team and project must live in the same
   *  one, so the pickers narrow themselves by these. */
  teamDomains?: Record<string, string>;
  projectDomains?: Record<string, string>;
  /** Per-team sprint pointers, keyed by team name ("" = the no-team group). */
  sprintStates: Record<string, SprintState>;
  /** The customer roster in board order (ownership.md, O5): cards name
   *  customers by id. Absent from an older server. */
  customers?: CustomerRef[];
}

/** CustomerRef is one entry of the board's customer roster. */
export interface CustomerRef {
  id: string;
  name: string;
}

/** CardPatch is a partial spec edit mirroring PATCH /cards/{uid}: only the
 * present fields are sent, and an empty string clears a field. Including
 * dates.start runs the calendar rule server-side (the sprint is recomputed
 * from the start day); dates.end / dates.sprint alone stay granular. */
export interface CardPatch {
  title?: string;
  description?: string;
  team?: string;
  zone?: ZoneKey | "";
  /** "" takes the size back. */
  size?: SizeKey | "";
  assignees?: string[];
  progress?: number;
  /** "" clears the stage; "done" marks the card done (derived server-side). */
  stage?: StageKey | "";
  dates?: { start?: string; end?: string; sprint?: string };
  /** Put the card in its team's backlog, or take it out. Taking a parked task
   *  out alone — with no date and no sprint to be in work by — is refused
   *  (422 takeIntoWork): giving it a date, a sprint or a person is what takes
   *  it into work (K4). */
  parked?: boolean;
  /** Re-file under a column ("" clears). Naming only the scope keeps the card
   *  inside its project; crossing projects names both halves. */
  scope?: string;
  project?: string;
  reviewOf?: string;
  /** Group under another card as a subtask ("" ungroups back to standalone). */
  parent?: string;
  /** A planning card's owner ("" takes them off; ownership.md). */
  owner?: string;
  /** The customer ids the card itself names (replacing what it named). */
  customers?: string[];
  /** Recurrent card's reseed cycle: "" = every sprint, "week" | "month". */
  recurrence?: string;
}

/** CardLog is a card's activity feed split back into notes and events. When
 * `truncatedBefore` is set, older history exists but is not loaded — the
 * server's clone reaches only so far back. */
export interface CardLog {
  notes: Note[];
  events: CardEvent[];
  truncatedBefore?: string;
}

/** CardDayLog is one card's slice of a day feed (GET /logs): its notes and
 *  events on that day alone. */
export interface CardDayLog {
  notes: Note[];
  events: CardEvent[];
}

/** CarryReport is what a carry-over pass did — or would do on a
 * dry run, which feeds the confirm-dialog counts. */
export interface CarryReport {
  carried: number;
  reseeded: number;
}

/** Provider is the thin intent client of the aeman API: components state
 * intent, keep optimistic state, and reconcile from the returned resources
 * (and the watch stream). All board rules live server-side. */
/** CardListing is a listing and what it says about itself: `asOf` is the
 *  moment a SNAPSHOT reflects — a past day answered as it stood. The server
 *  decides (a day inside the running sprint is still live, whatever the
 *  calendar says), so the client reads the answer rather than guessing. */
export interface CardListing {
  cards: Card[];
  asOf?: string;
}

export interface Provider {
  /** Board identity + per-team sprint pointers (no cards — the active view is
   *  loaded lazily via listCards). */
  /** Board identity, roster and per-team sprint pointers. `query` carries the
   *  snapshot selectors when a PAST day is being shown (`day`, `snapshot`), so
   *  the pointers and the roster are of the same moment as that day's cards —
   *  the view rules compare a card's sprint against them. */
  loadBoard(query?: Record<string, string>): Promise<Board>;
  /** The cards of one view (GET /views/{view}/cards), so the UI loads only
   *  the active board — Me by default, a team's grid on demand. */
  listCards(query: Record<string, string>): Promise<CardListing>;

  /** Fetch one card in full — listings are the light board-row shape, and the
   *  description is loaded here when a card is selected or opened. */
  getCard(uid: string): Promise<Card>;
  createCard(input: NewCardInput): Promise<Card>;
  patchCard(uid: string, patch: CardPatch): Promise<Card>;
  /** Hard delete; the server cascades to the linked review card. */
  deleteCard(uid: string): Promise<void>;
  /** The ×, carrying which of its two meanings was chosen: "unassign" empties
   *  the working area and leaves the card in its week or its column,
   *  "off-board" takes it away with the subtasks that were pieces of it.
   *  Omitted, the gesture decides for itself as it always did. */
  /** `deletes` says the caller knows the remove deletes the card (a plan
   *  item's ×): the board then turns away stale frames for it
   *  (tombstones.ts). The server is not told. */
  removeCard(uid: string, intent?: "unassign" | "off-board", opts?: { deletes?: boolean }): Promise<void>;
  /** Reposition card after afterId in the project order (null = top). */
  moveCard(uid: string, afterId: string | null): Promise<void>;
  /** Reorder to sit right before another card: the server resolves the true
   *  global anchor, so callers rendering a filtered slice don't need to know
   *  the full board order. */
  moveCardBefore(uid: string, beforeId: string): Promise<void>;
  /** Push the scheduled day N days ahead of max(today, current start). */
  deferCard(uid: string, days: number): Promise<Card>;
  /** Move to the implicit In Progress status (no stage, progress in [10,90]). */
  setInProgress(uid: string): Promise<Card>;
  /** Undo a done mark: the stage clears and the progress returns to what the
   *  card had when done was set (its log records the jump). */
  reopen(uid: string): Promise<Card>;
  /** Create the linked review card (or reassign an existing one). */
  sendToReview(
    uid: string,
    reviewer: string,
    day?: string,
    /** Zone for the review card; omitted = the original's zone. */
    zone?: ZoneKey,
  ): Promise<Card>;
  /** Delete the linked review card; returns the original. */
  removeReviewer(uid: string): Promise<Card>;
  /** Send finished work back to the sprint it was done in, its dates and the
   *  day it counts as done along with it. Refused (422) on work still going,
   *  and where there is no earlier sprint to send it to. */
  finishedEarlier(uid: string): Promise<Card>;
  /** Set how many points a week a TEAM gets through. A number of its own,
   *  not a sum of the team's people; 0 takes it back. Kept in the team file,
   *  the API and the MCP; no board reads it until a backlog shows load
   *  again (docs/design/backlog.md). The caller reloads. */
  setTeamCapacity(team: string, points: number): Promise<void>;
  /** Whose is this: a team and a person on it, a team alone (person = ""),
   *  or nobody (both ""). A person takes an UNPLANNED card into work the way
   *  the Me board would — today, the unplanned band, the team's sprint, out
   *  of its backlog (K4); a team alone parks it in that team's backlog, and
   *  a parked card handed to another team stays parked (K5); nobody parks it
   *  in the no-team backlog (K3); a planned card only changes hands (docs/design/hierarchy.md "Owner", H10). A person needs a
   *  team (422 ownerNeedsTeam); a subtask and a review card are refused
   *  (ownerFollowsLink); so are planning and project cards. */
  ownCard(uid: string, team: string, person: string): Promise<Card>;
  /** The Backlog's "I take this into work" (K11): the card becomes the
   *  CALLER's — the server's identity of the request — on its own team,
   *  today, out of the backlog; a review nobody has taken makes the caller
   *  its reviewer. A task already in work is 422 alreadyInWork. */
  takeCard(uid: string): Promise<Card>;
  /** Advance a team's sprint to today and carry its unfinished cards forward.
   * dryRun reports the would-be counts without writing. team = null is the
   * no-team group. */
  carryOver(
    team: string | null,
    dryRun?: boolean,
  ): Promise<CarryReport>;
  /** Apply a shared team order (moves the hidden sprint-state cards). */
  reorderTeams(teams: string[]): Promise<void>;

  /** Declare a new scope column inside a project (which is required). */
  addScope(name: string, project: string): Promise<void>;
  /** Delete an EMPTY scope column (422 while cards still sit under it). */
  deleteScope(name: string, project: string): Promise<void>;
  /** Rename a column in place; its cards follow. */
  renameScope(
    project: string,
    scope: string,
    to: string,
  ): Promise<void>;
  /** Apply one project's column order (moves the hidden scope-state cards). */
  reorderScopes(
    project: string,
    scopes: string[],
  ): Promise<void>;
  /** Move a column between projects ("" detaches it from every project). */
  setScopeProject(
    from: string,
    scope: string,
    project: string,
  ): Promise<void>;
  /** The customer roster (ownership.md, O5): add one (its id comes back),
   *  rename it (cards hold the id), set the order, delete it (it comes off
   *  every card that names it). */
  addCustomer(name: string): Promise<CustomerRef>;
  renameCustomer(id: string, to: string): Promise<void>;
  reorderCustomers(ids: string[]): Promise<void>;
  deleteCustomer(id: string): Promise<void>;
  /** Declare a project — the Project board's top grouping. `domain` picks the
   *  repository it is declared in (default: the primary). */
  addProject(name: string, domain?: string): Promise<void>;
  /** Delete an EMPTY project (422 while it still owns scope columns). */
  deleteProject(name: string): Promise<void>;
  /** Apply the shared project order (moves the hidden project-state cards). */
  reorderProjects(names: string[]): Promise<void>;
  /** Rename a project in place; its columns and their cards follow. */
  renameProject(from: string, to: string): Promise<void>;
  /** Rename a declared team where it lives; its cards follow. A name another
   *  team has is refused by the server. */
  renameTeam(from: string, to: string): Promise<void>;
  /** Mark a week with a project's deadline (one per project and week). */
  addDeadline(week: string, project: string): Promise<void>;
  /** Clear a project's deadline on a week. */
  deleteDeadline(
    week: string,
    project: string,
  ): Promise<void>;
  /** Drag a project's deadline to another week; two of its own become one. */
  moveDeadline(
    project: string,
    from: string,
    to: string,
  ): Promise<void>;
  /** Delete a team's sprint pointer; rejects while cards still use the team. */
  deleteTeam(team: string): Promise<void>;
  /** Set a team's sprint pointer directly (current/previous start dates). With
   *  both empty this declares the team; `domain` picks the repository its
   *  roster entry is written to (default: the primary). */
  setSprintState(
    team: string | null,
    current: string | null,
    previous: string | null,
    domain?: string,
  ): Promise<void>;
  /** URLs from the card's description: GitHub issue/PR refs (resolved with
   *  titles when possible) first, plain links after. */
  listLinks(uid: string): Promise<CardLink[]>;
  /** The card's unified activity feed, split back into notes and events. */
  listLog(uid: string): Promise<CardLog>;
  /** One day of activity for many cards in one request — the day feed's
   *  question. A quiet card answers with empty lists; a card the visitor
   *  cannot see is absent from the answer. */
  listDayLogs(uids: string[], day: string): Promise<Record<string, CardDayLog>>;
  /** Share the caller's live card selection ("" clears) with other boards. */
  setPresence(login: string, card: string | null): Promise<void>;
  listNotes(uid: string): Promise<Note[]>;
  addNote(uid: string, text: string): Promise<Note[]>;
  editNote(
    uid: string,
    noteId: string,
    text: string,
  ): Promise<Note[]>;
  deleteNote(uid: string, noteId: string): Promise<Note[]>;
}
