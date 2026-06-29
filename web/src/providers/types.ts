// Domain model and the extensible Provider interface. A provider maps an
// external system (GitHub Projects v2 today, GitLab/Redmine/... later) onto
// these shared types so the UI never depends on a specific backend.

export type ProviderId = "github";

/** ZoneKey is the colour zone a card belongs to, in the Ford sense. */
export type ZoneKey = "gray" | "green" | "yellow" | "red";

/** StageKey is an explicit per-card status that recolours the progress bar. */
export type StageKey = "locked" | "review" | "done";

export interface SingleSelectOption {
  id: string;
  name: string;
  color: string;
}

export interface ProjectField {
  id: string;
  name: string;
  dataType: string;
  options?: SingleSelectOption[];
}

export interface BoardSummary {
  id: string;
  number: number;
  title: string;
  url: string;
  shortDescription?: string;
}

/** Note is a dated work note attached to a card (an issue comment, or a line
 * stored in a draft issue's body when the card has no comment thread). */
export interface Note {
  id: string;
  body: string;
  createdAt: string;
  author?: string;
  source: "comment" | "draft";
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
  state?: string;
  assignees: string[];
  /** ISO timestamp the item was added to the project (its age on the board). */
  createdAt?: string;
  zoneOptionId?: string;
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
  sprintTitle?: string;
  status?: string;
  /** Free-form card details (the body minus the appended action log). */
  description?: string;
  notes?: Note[];
}

/** NewCardInput describes a card to create on a board. */
export interface NewCardInput {
  title: string;
  zone?: ZoneKey;
  day?: string | null;
  start?: string | null;
  sprintStart?: string | null;
  plan?: "wed" | "fri" | null;
  week?: string | null;
  assigneeLogin?: string | null;
  team?: string | null;
  /** On a review card, the itemId of the original card it reviews. */
  reviewOf?: string | null;
}

/** SprintState is a team's explicit sprint pointer, stored on a hidden
 * "sprint-state" card: its current and previous sprint start dates. */
export interface SprintState {
  current: string | null;
  previous: string | null;
  itemId: string;
}

export interface Board {
  id: string;
  number: number;
  title: string;
  url: string;
  owner: string;
  fields: ProjectField[];
  cards: Card[];
  /** Per-team sprint pointers, keyed by team name ("" = the no-team group). */
  sprintStates: Record<string, SprintState>;
}

/** FieldRoles resolves well-known fields by their (case-insensitive) name. */
export interface FieldRoles {
  zone?: ProjectField;
  progress?: ProjectField;
  day?: ProjectField;
  start?: ProjectField;
  sprintStart?: ProjectField;
  plan?: ProjectField;
  week?: ProjectField;
  sprint?: ProjectField;
  status?: ProjectField;
  stage?: ProjectField;
  team?: ProjectField;
  reviewOf?: ProjectField;
}

export interface Provider {
  id: ProviderId;
  label: string;
  listBoards(owner: string): Promise<BoardSummary[]>;
  loadBoard(owner: string, number: number): Promise<Board>;
  setZone(board: Board, card: Card, optionId: string | null): Promise<void>;
  setProgress(board: Board, card: Card, progress: number): Promise<void>;
  setDay(board: Board, card: Card, day: string | null): Promise<void>;
  setStart(board: Board, card: Card, date: string | null): Promise<void>;
  setSprintStart(board: Board, card: Card, date: string | null): Promise<void>;
  /** Set a team's sprint pointer (current/previous start dates), creating the
   * hidden state card if the team has none yet. team = null is the no-team group. */
  setSprintState(
    board: Board,
    team: string | null,
    current: string | null,
    previous: string | null,
  ): Promise<void>;
  setPlan(board: Board, card: Card, plan: "wed" | "fri" | null): Promise<void>;
  setWeek(board: Board, card: Card, date: string | null): Promise<void>;
  setAssignee(board: Board, card: Card, login: string | null): Promise<void>;
  setStage(board: Board, card: Card, stage: StageKey | null): Promise<void>;
  setTeam(board: Board, card: Card, team: string | null): Promise<void>;
  renameCard(board: Board, card: Card, title: string): Promise<void>;
  setDescription(board: Board, card: Card, description: string): Promise<void>;
  createCard(board: Board, input: NewCardInput): Promise<Card>;
  deleteCard(board: Board, card: Card): Promise<void>;
  /** Reposition card after afterItemId in the project order (null = top). */
  moveCard(board: Board, card: Card, afterItemId: string | null): Promise<void>;
  addNote(board: Board, card: Card, text: string): Promise<void>;
  editNote(board: Board, card: Card, note: Note, text: string): Promise<void>;
  deleteNote(board: Board, card: Card, note: Note): Promise<void>;
}
