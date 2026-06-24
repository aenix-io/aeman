// Domain model and the extensible Provider interface. A provider maps an
// external system (GitHub Projects v2 today, GitLab/Redmine/... later) onto
// these shared types so the UI never depends on a specific backend.

export type ProviderId = "github";

/** ZoneKey is the colour zone a card belongs to, in the Ford sense. */
export type ZoneKey = "gray" | "green" | "yellow" | "red";

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

/** Card is a single project item (issue, PR or draft). */
export interface Card {
  itemId: string;
  title: string;
  isDraft: boolean;
  url?: string;
  number?: number;
  repository?: string;
  state?: string;
  assignees: string[];
  zoneOptionId?: string;
  zone?: ZoneKey;
  /** Readiness, 0..100. */
  progress?: number;
  /** ISO date (yyyy-mm-dd) the card is planned for (Ford day view). */
  day?: string;
  sprintTitle?: string;
  status?: string;
}

export interface Board {
  id: string;
  number: number;
  title: string;
  url: string;
  owner: string;
  fields: ProjectField[];
  cards: Card[];
}

/** FieldRoles resolves well-known fields by their (case-insensitive) name. */
export interface FieldRoles {
  zone?: ProjectField;
  progress?: ProjectField;
  day?: ProjectField;
  sprint?: ProjectField;
  status?: ProjectField;
}

export interface Provider {
  id: ProviderId;
  label: string;
  listBoards(owner: string): Promise<BoardSummary[]>;
  loadBoard(owner: string, number: number): Promise<Board>;
  setZone(board: Board, card: Card, optionId: string | null): Promise<void>;
  setProgress(board: Board, card: Card, progress: number): Promise<void>;
  setDay(board: Board, card: Card, day: string | null): Promise<void>;
}
