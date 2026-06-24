import { graphql } from "../../api/client";
import { zoneFromColor } from "../../zones";
import { fieldRoles } from "../fields";
import type {
  Board,
  BoardSummary,
  Card,
  FieldRoles,
  ProjectField,
  Provider,
} from "../types";
import {
  CLEAR_FIELD,
  ORG_PROJECT_QUERY,
  ORG_PROJECTS_QUERY,
  SET_DATE,
  SET_NUMBER,
  SET_SINGLE_SELECT,
  USER_PROJECT_QUERY,
  USER_PROJECTS_QUERY,
} from "./queries";

// Raw shapes returned by the GraphQL API (only the parts we read).
interface RawField {
  __typename: string;
  id?: string;
  name?: string;
  dataType?: string;
  options?: { id: string; name: string; color: string }[];
}

interface RawFieldValue {
  __typename: string;
  optionId?: string;
  name?: string;
  number?: number;
  date?: string;
  title?: string;
  text?: string;
  field?: { id?: string; name?: string };
}

interface RawContent {
  __typename: string;
  number?: number;
  title?: string;
  url?: string;
  state?: string;
  repository?: { nameWithOwner: string };
  assignees?: { nodes: { login: string }[] };
}

interface RawItem {
  id: string;
  type: string;
  content?: RawContent | null;
  fieldValues: { nodes: RawFieldValue[] };
}

interface RawProject {
  id: string;
  number: number;
  title: string;
  url: string;
  fields: { nodes: RawField[] };
  items: { nodes: RawItem[] };
}

interface ProjectResult {
  organization?: { projectV2?: RawProject | null } | null;
  user?: { projectV2?: RawProject | null } | null;
}

interface ProjectsListResult {
  organization?: { projectsV2: { nodes: BoardSummary[] } } | null;
  user?: { projectsV2: { nodes: BoardSummary[] } } | null;
}

function mapItem(item: RawItem, roles: FieldRoles): Card {
  const content = item.content ?? undefined;
  const isDraft =
    item.type === "DRAFT_ISSUE" || content?.__typename === "DraftIssue";
  const card: Card = {
    itemId: item.id,
    title: content?.title ?? "(untitled)",
    isDraft,
    url: content?.url,
    number: content?.number,
    repository: content?.repository?.nameWithOwner,
    state: content?.state,
    assignees: content?.assignees?.nodes.map((n) => n.login) ?? [],
  };

  for (const value of item.fieldValues.nodes) {
    const fieldID = value.field?.id;
    if (!fieldID) {
      continue;
    }
    if (roles.zone && fieldID === roles.zone.id && value.optionId) {
      card.zoneOptionId = value.optionId;
      const option = roles.zone.options?.find((o) => o.id === value.optionId);
      card.zone = zoneFromColor(option?.color);
    } else if (
      roles.progress &&
      fieldID === roles.progress.id &&
      typeof value.number === "number"
    ) {
      card.progress = value.number;
    } else if (roles.day && fieldID === roles.day.id && value.date) {
      card.day = value.date;
    } else if (roles.sprint && fieldID === roles.sprint.id && value.title) {
      card.sprintTitle = value.title;
    } else if (roles.status && fieldID === roles.status.id && value.name) {
      card.status = value.name;
    }
  }
  return card;
}

function mapProject(owner: string, raw: RawProject): Board {
  const fields: ProjectField[] = raw.fields.nodes
    .filter((f): f is RawField & { id: string; name: string } =>
      Boolean(f.id && f.name),
    )
    .map((f) => ({
      id: f.id,
      name: f.name,
      dataType: f.dataType ?? "",
      options: f.options,
    }));

  const board: Board = {
    id: raw.id,
    number: raw.number,
    title: raw.title,
    url: raw.url,
    owner,
    fields,
    cards: [],
  };
  const roles = fieldRoles(board);
  board.cards = raw.items.nodes.map((item) => mapItem(item, roles));
  return board;
}

async function loadProject(owner: string, number: number): Promise<RawProject> {
  try {
    const data = await graphql<ProjectResult>(ORG_PROJECT_QUERY, { owner, number });
    if (data.organization?.projectV2) {
      return data.organization.projectV2;
    }
  } catch {
    // Owner is likely a user, not an org; fall through to the user query.
  }
  const data = await graphql<ProjectResult>(USER_PROJECT_QUERY, { owner, number });
  if (data.user?.projectV2) {
    return data.user.projectV2;
  }
  throw new Error(`Project #${number} not found for "${owner}"`);
}

function requireRole(
  board: Board,
  role: keyof FieldRoles,
  label: string,
): ProjectField {
  const field = fieldRoles(board)[role];
  if (!field) {
    throw new Error(`Project has no "${label}" field`);
  }
  return field;
}

export const githubProvider: Provider = {
  id: "github",
  label: "GitHub Projects v2",

  async listBoards(owner: string): Promise<BoardSummary[]> {
    try {
      const data = await graphql<ProjectsListResult>(ORG_PROJECTS_QUERY, { owner });
      if (data.organization?.projectsV2) {
        return data.organization.projectsV2.nodes;
      }
    } catch {
      // Fall through to the user query.
    }
    const data = await graphql<ProjectsListResult>(USER_PROJECTS_QUERY, { owner });
    return data.user?.projectsV2.nodes ?? [];
  },

  async loadBoard(owner: string, number: number): Promise<Board> {
    const raw = await loadProject(owner, number);
    return mapProject(owner, raw);
  },

  async setZone(board: Board, card: Card, optionId: string | null): Promise<void> {
    const field = requireRole(board, "zone", "Zone");
    if (optionId === null) {
      await graphql(CLEAR_FIELD, { project: board.id, item: card.itemId, field: field.id });
      return;
    }
    await graphql(SET_SINGLE_SELECT, {
      project: board.id,
      item: card.itemId,
      field: field.id,
      option: optionId,
    });
  },

  async setProgress(board: Board, card: Card, progress: number): Promise<void> {
    const field = requireRole(board, "progress", "Progress");
    await graphql(SET_NUMBER, {
      project: board.id,
      item: card.itemId,
      field: field.id,
      value: progress,
    });
  },

  async setDay(board: Board, card: Card, day: string | null): Promise<void> {
    const field = requireRole(board, "day", "Day");
    if (day === null) {
      await graphql(CLEAR_FIELD, { project: board.id, item: card.itemId, field: field.id });
      return;
    }
    await graphql(SET_DATE, {
      project: board.id,
      item: card.itemId,
      field: field.id,
      value: day,
    });
  },
};
