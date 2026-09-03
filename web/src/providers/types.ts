// Domain model shared by the UI components and the API provider. The flat Card
// shape is the internal component model; the provider maps the server's
// Kubernetes-style resources onto it at the client boundary (api/resources.ts),
// so components never see wire shapes or semantic zone names.

import type { DomainInfo } from "../domains";
import type { CardLink } from "../links";
import type { PersonalBoard } from "../personal";
import type { Member } from "../users";

/** ZoneKey is the colour zone a card belongs to, in the Ford sense. */
export type ZoneKey = "gray" | "green" | "yellow" | "red";

/** StageKey is an explicit per-card status that recolours the progress bar. */
export type StageKey = "locked" | "review" | "recurrent" | "refuse" | "done";

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
  /** Additional Project-board columns the same card stands in — one file,
   *  one log, one set of dates, shown in every listed project too. Always
   *  the card's own repository (the server admits nothing else). */
  mirrors?: { project: string; epic: string }[];
  /** The repository the card lives in (one of the board's domains). Absent on
   *  an older server, which means the primary. */
  domain?: string;
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
  /** A recurrent card's reseed cycle: "" = every sprint, "week" | "month". */
  recurrence?: string;
  /** ISO date (yyyy-mm-dd) the card is planned to finish/be due on. */
  day?: string;
  /** ISO date (yyyy-mm-dd) the card starts on (set at creation). */
  startDate?: string;
  /** ISO date (yyyy-mm-dd) the sprint this card belongs to was started.
   * This is what the boards orient by (day/startDate are just metadata). */
  sprintStart?: string;
  /** ISO date (yyyy-mm-dd) of the WEEK this card is scheduled for — the row
   *  it stands in on the Triage board. */
  week?: string;
  /** The column this card is filed under: epic + project TOGETHER, since epic
   *  names repeat across projects. Its week is the row. */
  epic?: string;
  project?: string;
  /** On a process turn: the process it belongs to, and the task it came from. */
  process?: string;
  task?: string;
  /** A card scheduled ahead — a slot, a turn, a card given a week — still
   *  open past the day it was owed by. Derived by the server from its dates. */
  overdue?: boolean;
  /** The moment this card is FROM, when it is a record rather than the live
   *  card: its team's sprint has moved on past the day being looked at. The
   *  board shows it as it was and refuses to change it (G60). */
  asOf?: string;
  /** The board day (yyyy-mm-dd) the card reached done; cleared when it
   *  reopens. The personal column shows a done card that day, not the next. */
  doneAt?: string;
  /** The board day a personal card was left behind on by the × — on the
   *  column that day and before, off it from the next; cleared by re-dating. */
  leftAt?: string;
  /** Nobody placed the card in a week and it is not being worked (B3). */
  triage?: boolean;
  /** The Monday of the Triage column the card stands in (B5). */
  triageWeek?: string;
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
  day?: string | null;
  start?: string | null;
  week?: string | null;
  epic?: string | null;
  project?: string | null;
  assigneeLogin?: string | null;
  team?: string | null;
  /** On a review card, the itemId of the original card it reviews. */
  reviewOf?: string | null;
  /** Group the new card as a subtask of this card on create. */
  parent?: string | null;
  /** Force the team's sprint pointer to (re)start on the card's day. */
  startNewSprint?: boolean;
  /** Schedule the card for its day without joining any sprint (a "next
   * sprint" create); the next carry-over to reach its day adopts it. */
  noSprint?: boolean;
  /** Create on the visitor's personal board: their own repository, assigned
   * to them, with no team and no column. */
  personal?: boolean;
}

/** SprintState is a team's explicit sprint pointer: its current and previous
 * sprint start dates. */
export interface SprintState {
  current: string | null;
  previous: string | null;
  /** The team's cards a week and the lanes' shares, for the Triage board;
   *  derived from the last four weeks' done cards when the roster has no
   *  number (docs/design/triage.md). */
  capacity?: { week: number; client: number; internal: number; derived: boolean };
}


/** ProcessTask is what a process iterates on, plus how the last few
 *  iterations went. */
export interface ProcessTask {
  uid: string;
  title: string;
  description?: string;
  recurrence: string;
  start?: string;
  team?: string;
  assignee?: string;
  accumulate?: boolean;
  history: { uid: string; week: string; state: "done" | "open" | "late" }[];
  /** The weeks this task comes due in over the planning horizon and has no
   *  turn of its own yet — what the process is going to file. A board that
   *  plans weeks ahead draws them: a week already spoken for by a process is
   *  not a week the team is free in. A paused process sends none. */
  due?: string[];
  /** Counts over ALL turns, including the ones history leaves out. */
  turns?: number;
  done?: number;
  late?: number;
}

/** ProcessInfo is one process and its tasks. */
export interface ProcessInfo {
  name: string;
  project: string;
  /** A paused process files no iterations; nothing else about it changes. */
  paused?: boolean;
  tasks: ProcessTask[];
}

/** TaskInput is a task on its way in (create: all; patch: some). */
export interface TaskInput {
  title?: string;
  description?: string;
  recurrence?: string;
  start?: string;
  team?: string;
  assignee?: string;
  accumulate?: boolean;
}

/** DeadlineRef is one deadline line: the week it sits on and whose it is. */
export interface DeadlineRef {
  week: string;
  project: string;
}

/** EpicRef is one Project-board column: its name and the project that owns
 *  it. The pair travels together — a column is meaningless without knowing
 *  which project's grid it belongs in. */
export interface EpicRef {
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
   *  project owns epic columns. */
  projects: string[];
  /** The Project board's epic columns, in board order, each naming the project
   *  that owns it. An empty project means the column belongs to none. */
  epics: EpicRef[];
  /** The deadline lines: the week (a Monday) each sits on and the project it
   *  belongs to. A project holds at most one per week. */
  deadlines: DeadlineRef[];
  /** The processes with their tasks and history — the Process tab is
   *  drawn from this, and the Board watch frame refreshes it. */
  processes: ProcessInfo[];
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
  /** The repository each process was declared in — a card is only tied to a
   *  process of its own repository, so the picker narrows itself by this. */
  processDomains?: Record<string, string>;
  /** The visitor's personal board, when they linked one. */
  personal?: PersonalBoard;
  /** Per-team sprint pointers, keyed by team name ("" = the no-team group). */
  sprintStates: Record<string, SprintState>;
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
  assignees?: string[];
  progress?: number;
  /** "" clears the stage; "done" marks the card done (derived server-side). */
  stage?: StageKey | "";
  dates?: { start?: string; end?: string; sprint?: string };
  /** The week the card is scheduled for ("" takes it off the Triage weeks). */
  week?: string;
  /** Re-file under a column ("" clears). Naming only the epic keeps the card
   *  inside its project; crossing projects names both halves. */
  epic?: string;
  /** Tie the card to a process — the recurring shelf's counterpart of a
   *  column ("" clears). The process must already exist. */
  process?: string;
  project?: string;
  reviewOf?: string;
  /** Group under another card as a subtask ("" ungroups back to standalone). */
  parent?: string;
  /** On a review card, its review-round counter (>=2 shown; round 1 implicit). */
  reviewRound?: number;
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
  /** The cards of one view (GET /cards with a selector), so the UI loads only
   *  the active board — Me by default, a team's grid on demand. */
  listCards(query: Record<string, string>): Promise<CardListing>;

  /** Fetch one card in full — listings are the light board-row shape, and the
   *  description is loaded here when a card is selected or opened. */
  getCard(uid: string): Promise<Card>;
  createCard(input: NewCardInput): Promise<Card>;
  patchCard(uid: string, patch: CardPatch): Promise<Card>;
  /** Hard delete; the server cascades to the linked review card. */
  deleteCard(uid: string): Promise<void>;
  /** The smart ×: the server hands the card back to a home it still has, or
   *  deletes it when the working area was the last one. */
  removeCard(uid: string, from: "grid"): Promise<void>;
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
  /** Place a card in a week of the Triage board — which is what triaging it
   *  means (docs/design/triage.md). */
  placeCard(uid: string, week: string): Promise<Card>;
  /** Take a card out of every week — back to the triage strip. */
  untriageCard(uid: string): Promise<Card>;
  /** Advance a team's sprint to today and carry its unfinished cards forward.
   * dryRun reports the would-be counts without writing. team = null is the
   * no-team group. */
  carryOver(
    team: string | null,
    dryRun?: boolean,
  ): Promise<CarryReport>;
  /** Apply a shared team order (moves the hidden sprint-state cards). */
  reorderTeams(teams: string[]): Promise<void>;

  /** Declare a new epic column inside a project (which is required). */
  addEpic(name: string, project: string): Promise<void>;
  /** Delete an EMPTY epic column (422 while cards still sit under it). */
  deleteEpic(name: string, project: string): Promise<void>;
  /** Rename a column in place; its cards follow. */
  renameEpic(
    project: string,
    epic: string,
    to: string,
  ): Promise<void>;
  /** Apply one project's column order (moves the hidden epic-state cards). */
  reorderEpics(
    project: string,
    epics: string[],
  ): Promise<void>;
  /** Move a column between projects ("" detaches it from every project). */
  setEpicProject(
    from: string,
    epic: string,
    project: string,
  ): Promise<void>;
  /** Declare a project — the Project board's top grouping. `domain` picks the
   *  repository it is declared in (default: the primary). */
  addProject(name: string, domain?: string): Promise<void>;
  /** Delete an EMPTY project (422 while it still owns epic columns). */
  deleteProject(name: string): Promise<void>;
  /** Apply the shared project order (moves the hidden project-state cards). */
  reorderProjects(names: string[]): Promise<void>;
  /** Rename a project in place; its columns and their cards follow. */
  renameProject(from: string, to: string): Promise<void>;
  /** Rename a declared team where it lives; its cards and process tasks
   *  follow. A name another team has is refused by the server. */
  renameTeam(from: string, to: string): Promise<void>;
  /** The Process tab: every process with its tasks and their history. */
  listProcesses(project?: string): Promise<ProcessInfo[]>;
  /** Declare a process in a project; `domain` picks its repository. */
  addProcess(name: string, project: string, domain?: string): Promise<void>;
  deleteProcess(name: string): Promise<void>;
  renameProcess(from: string, to: string): Promise<void>;
  /** Move a process to another project ("" = the no-project bucket). */
  setProcessProject(process: string, project: string): Promise<void>;
  /** Stop a process filing iterations, or start it again. */
  setProcessPaused(process: string, paused: boolean): Promise<void>;
  /** Apply a shared process order. */
  reorderProcesses(processes: string[]): Promise<void>;
  /** Apply one process's task order; a uid from another process is adopted. */
  reorderProcessTasks(process: string, uids: string[]): Promise<void>;
  addTask(process: string, input: TaskInput): Promise<string>;
  updateTask(uid: string, patch: TaskInput): Promise<void>;
  deleteTask(uid: string): Promise<void>;
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
  /** Show the card in a second Project-board column — the same card, one
   *  file and one log, standing in both projects. */
  mirrorCard(uid: string, project: string, epic: string): Promise<void>;
  /** Take one mirror column away; the home and everything else stay. */
  unmirrorCard(uid: string, project: string, epic: string): Promise<void>;
  /** The Project board's ×: remove the card from one column (mirror goes /
   *  home hands over / last column keeps only a worked card, as an orphan
   *  of the working area). */
  removeFromProject(uid: string, project: string, epic: string): Promise<void>;
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
  /** The visitor's personal board, or null when none is linked. */
  getPersonal(): Promise<PersonalBoard | null>;
  /** Link a repository of the visitor's own as their personal board (they
   *  need push access to it; the server clones it with their credential). */
  linkPersonal(url: string): Promise<PersonalBoard>;
  /** Drop the link; the repository itself is left untouched. */
  unlinkPersonal(): Promise<void>;
}
