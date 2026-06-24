import { graphql } from "../../api/client";
import { optionIdForZone, zoneFromColor } from "../../zones";
import { fieldRoles } from "../fields";
import type {
  Board,
  BoardSummary,
  Card,
  FieldRoles,
  NewCardInput,
  Note,
  ProjectField,
  Provider,
} from "../types";
import {
  ADD_ASSIGNEES,
  ADD_COMMENT,
  ADD_DRAFT,
  CLEAR_FIELD,
  DELETE_ITEM,
  GET_DRAFT_BODY,
  ORG_PROJECT_QUERY,
  ORG_PROJECTS_QUERY,
  REMOVE_ASSIGNEES,
  SET_DATE,
  SET_NUMBER,
  SET_SINGLE_SELECT,
  UPDATE_DRAFT_ASSIGNEES,
  UPDATE_DRAFT_BODY,
  USER_ID_QUERY,
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

interface RawComment {
  id: string;
  body: string;
  createdAt: string;
  author?: { login: string } | null;
}

interface RawContent {
  __typename: string;
  id?: string;
  number?: number;
  title?: string;
  url?: string;
  state?: string;
  body?: string;
  repository?: { nameWithOwner: string };
  assignees?: { nodes: { login: string }[] };
  comments?: { nodes: RawComment[] };
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

const DRAFT_NOTE_RE = /^[-*]?\s*\[([^\]]+)\]\s?(.*)$/;

function parseNotes(content: RawContent | undefined, itemId: string): Note[] {
  if (!content) {
    return [];
  }
  if (content.comments?.nodes?.length) {
    return content.comments.nodes.map((c) => ({
      id: c.id,
      body: c.body,
      createdAt: c.createdAt,
      author: c.author?.login,
      source: "comment" as const,
    }));
  }
  if (content.body) {
    const notes: Note[] = [];
    content.body.split("\n").forEach((line, i) => {
      const match = DRAFT_NOTE_RE.exec(line.trim());
      if (match) {
        notes.push({ id: `${itemId}:${i}`, body: match[2], createdAt: match[1], source: "draft" });
      }
    });
    return notes;
  }
  return [];
}

function mapItem(item: RawItem, roles: FieldRoles): Card {
  const content = item.content ?? undefined;
  const isDraft =
    item.type === "DRAFT_ISSUE" || content?.__typename === "DraftIssue";
  const card: Card = {
    itemId: item.id,
    contentId: content?.id,
    title: content?.title ?? "(untitled)",
    isDraft,
    url: content?.url,
    number: content?.number,
    repository: content?.repository?.nameWithOwner,
    state: content?.state,
    assignees: content?.assignees?.nodes.map((n) => n.login) ?? [],
    notes: parseNotes(content, item.id),
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

const userIdCache = new Map<string, string>();

async function resolveUserId(login: string): Promise<string> {
  const cached = userIdCache.get(login);
  if (cached) {
    return cached;
  }
  const data = await graphql<{ user?: { id: string } | null }>(USER_ID_QUERY, { login });
  const id = data.user?.id;
  if (!id) {
    throw new Error(`GitHub user "${login}" not found`);
  }
  userIdCache.set(login, id);
  return id;
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

  async setAssignee(_board: Board, card: Card, login: string | null): Promise<void> {
    if (!card.contentId) {
      throw new Error("Card has no underlying issue to assign");
    }
    const newId = login ? await resolveUserId(login) : null;
    if (card.isDraft) {
      await graphql(UPDATE_DRAFT_ASSIGNEES, {
        draft: card.contentId,
        assignees: newId ? [newId] : [],
      });
      return;
    }
    if (card.assignees.length > 0) {
      const ids = await Promise.all(card.assignees.map(resolveUserId));
      await graphql(REMOVE_ASSIGNEES, { assignable: card.contentId, assignees: ids });
    }
    if (newId) {
      await graphql(ADD_ASSIGNEES, { assignable: card.contentId, assignees: [newId] });
    }
  },

  async createCard(board: Board, input: NewCardInput): Promise<Card> {
    const assigneeIds = input.assigneeLogin
      ? [await resolveUserId(input.assigneeLogin)]
      : [];
    const created = await graphql<{
      addProjectV2DraftIssue: {
        projectItem: { id: string; content?: { id?: string } | null };
      };
    }>(ADD_DRAFT, { project: board.id, title: input.title, assignees: assigneeIds });
    const item = created.addProjectV2DraftIssue.projectItem;
    const roles = fieldRoles(board);

    let zoneOptionId: string | undefined;
    if (input.zone && roles.zone) {
      zoneOptionId = optionIdForZone(roles.zone, input.zone);
      if (zoneOptionId) {
        await graphql(SET_SINGLE_SELECT, {
          project: board.id,
          item: item.id,
          field: roles.zone.id,
          option: zoneOptionId,
        });
      }
    }
    if (input.day && roles.day) {
      await graphql(SET_DATE, {
        project: board.id,
        item: item.id,
        field: roles.day.id,
        value: input.day,
      });
    }

    return {
      itemId: item.id,
      contentId: item.content?.id,
      title: input.title,
      isDraft: true,
      assignees: input.assigneeLogin ? [input.assigneeLogin] : [],
      zone: zoneOptionId ? input.zone : undefined,
      zoneOptionId,
      day: input.day ?? undefined,
      notes: [],
    };
  },

  async deleteCard(board: Board, card: Card): Promise<void> {
    await graphql(DELETE_ITEM, { project: board.id, item: card.itemId });
  },

  async addNote(_board: Board, card: Card, text: string): Promise<void> {
    if (!card.contentId) {
      throw new Error("Card has no underlying issue to note on");
    }
    if (card.isDraft) {
      const data = await graphql<{ node?: { body?: string } | null }>(GET_DRAFT_BODY, {
        id: card.contentId,
      });
      const body = data.node?.body ?? "";
      const line = `- [${new Date().toISOString()}] ${text}`;
      await graphql(UPDATE_DRAFT_BODY, {
        draft: card.contentId,
        body: body ? `${body}\n${line}` : line,
      });
      return;
    }
    await graphql(ADD_COMMENT, { subject: card.contentId, body: text });
  },
};
