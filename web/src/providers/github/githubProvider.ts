import { graphql } from "../../api/client";
import { todayIso } from "../../date";
import {
  STAGES,
  STAGE_ORDER,
  optionIdForStage,
  stageFromName,
} from "../../stages";
import { ZONES, ZONE_ORDER, optionIdForZone, zoneFromColor } from "../../zones";
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
  SprintState,
  StageKey,
} from "../types";
import {
  ADD_ASSIGNEES,
  ADD_COMMENT,
  UPDATE_COMMENT,
  DELETE_COMMENT,
  ADD_DRAFT,
  CLEAR_FIELD,
  CREATE_FIELD,
  CREATE_SELECT_FIELD,
  DELETE_ITEM,
  GET_DRAFT_BODY,
  MOVE_ITEM,
  CARDS_BY_ID_QUERY,
  ORG_PROJECT_QUERY,
  ORG_PROJECTS_QUERY,
  REMOVE_ASSIGNEES,
  SET_DATE,
  SET_NUMBER,
  SET_SINGLE_SELECT,
  SET_TEXT,
  UPDATE_DRAFT_ASSIGNEES,
  UPDATE_DRAFT_BODY,
  UPDATE_DRAFT_TITLE,
  UPDATE_ISSUE_BODY,
  UPDATE_ISSUE_TITLE,
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
  /** Creator of a draft issue; issues/PRs expose the same person as `author`. */
  creator?: { login: string } | null;
  author?: { login: string } | null;
  repository?: { nameWithOwner: string };
  assignees?: { nodes: { login: string }[] };
  comments?: { nodes: RawComment[] };
}

interface RawItem {
  id: string;
  type: string;
  createdAt?: string;
  content?: RawContent | null;
  fieldValues: { nodes: RawFieldValue[] };
}

interface RawProject {
  id: string;
  number: number;
  title: string;
  url: string;
  fields: { nodes: RawField[] };
  items: {
    nodes: RawItem[];
    pageInfo: { hasNextPage: boolean; endCursor: string | null };
  };
}

interface ProjectResult {
  organization?: { projectV2?: RawProject | null } | null;
  user?: { projectV2?: RawProject | null } | null;
}

interface ProjectsListResult {
  organization?: { projectsV2: { nodes: BoardSummary[] } } | null;
  user?: { projectsV2: { nodes: BoardSummary[] } } | null;
}

// SPRINT_STATE_TITLE marks the hidden per-team draft card that stores the team's
// sprint pointer (current/previous start dates). It never shows on a board view.
const SPRINT_STATE_TITLE = "aeman:sprint-state";

// LOG_MARKER separates a draft card's description from its appended action log.
const LOG_MARKER = "<!-- aeman:log -->";
const NOTE_RE = /^[-*]?\s*\[([^\]]+)\]\s?(.*)$/;

function parseNoteLines(text: string, itemId: string): Note[] {
  const notes: Note[] = [];
  text.split("\n").forEach((line, i) => {
    const match = NOTE_RE.exec(line.trim());
    if (match) {
      notes.push({ id: `${itemId}:${i}`, body: match[2], createdAt: match[1], source: "draft" });
    }
  });
  return notes;
}

/** parseDraftBody splits a draft body into a description and its action log. */
function parseDraftBody(
  body: string | undefined,
  itemId: string,
): { description: string; notes: Note[] } {
  if (!body) {
    return { description: "", notes: [] };
  }
  const idx = body.indexOf(LOG_MARKER);
  if (idx >= 0) {
    return {
      description: body.slice(0, idx).trim(),
      notes: parseNoteLines(body.slice(idx + LOG_MARKER.length), itemId),
    };
  }
  // Legacy bodies without a marker: treat note-shaped lines as the log.
  const descLines: string[] = [];
  const notes: Note[] = [];
  body.split("\n").forEach((line, i) => {
    const match = NOTE_RE.exec(line.trim());
    if (match) {
      notes.push({ id: `${itemId}:${i}`, body: match[2], createdAt: match[1], source: "draft" });
    } else {
      descLines.push(line);
    }
  });
  return { description: descLines.join("\n").trim(), notes };
}

/** buildDraftBody serialises a description and action log back into a body. */
function buildDraftBody(description: string, notes: Note[]): string {
  const desc = description.trim();
  if (notes.length === 0) {
    return desc;
  }
  const log = notes.map((n) => `- [${n.createdAt}] ${n.body}`).join("\n");
  return `${desc ? `${desc}\n\n` : ""}${LOG_MARKER}\n${log}`;
}

function commentsToNotes(content: RawContent): Note[] {
  return (content.comments?.nodes ?? []).map((c) => ({
    id: c.id,
    body: c.body,
    createdAt: c.createdAt,
    author: c.author?.login,
    source: "comment" as const,
  }));
}

function mapItem(item: RawItem, roles: FieldRoles): Card {
  const content = item.content ?? undefined;
  const isDraft =
    item.type === "DRAFT_ISSUE" || content?.__typename === "DraftIssue";

  let description = "";
  let notes: Note[] = [];
  if (content) {
    if (isDraft) {
      const parsed = parseDraftBody(content.body, item.id);
      description = parsed.description;
      notes = parsed.notes;
    } else {
      description = content.body ?? "";
      notes = commentsToNotes(content);
    }
  }

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
    author: content?.creator?.login ?? content?.author?.login,
    createdAt: item.createdAt,
    description,
    notes,
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
    } else if (roles.stage && fieldID === roles.stage.id && value.name) {
      card.stage = stageFromName(value.name);
    } else if (
      roles.progress &&
      fieldID === roles.progress.id &&
      typeof value.number === "number"
    ) {
      card.progress = value.number;
    } else if (roles.day && fieldID === roles.day.id && value.date) {
      card.day = value.date;
    } else if (roles.start && fieldID === roles.start.id && value.date) {
      card.startDate = value.date;
    } else if (
      roles.sprintStart &&
      fieldID === roles.sprintStart.id &&
      value.date
    ) {
      card.sprintStart = value.date;
    } else if (roles.plan && fieldID === roles.plan.id && value.name) {
      card.plan = value.name.toLowerCase() === "fri" ? "fri" : "wed";
    } else if (roles.week && fieldID === roles.week.id && value.date) {
      card.week = value.date;
    } else if (roles.sprint && fieldID === roles.sprint.id && value.title) {
      card.sprintTitle = value.title;
    } else if (roles.status && fieldID === roles.status.id && value.name) {
      card.status = value.name;
    } else if (roles.team && fieldID === roles.team.id && value.text) {
      card.team = value.text;
    } else if (roles.reviewOf && fieldID === roles.reviewOf.id && value.text) {
      card.reviewOf = value.text;
    }
  }
  return card;
}

/** parseSprintState reads a sprint-state card into its team key and pointer:
 * Team text = the team ("" = no-team group), Sprint Start = current sprint,
 * Start = previous sprint. */
function parseSprintState(
  item: RawItem,
  roles: FieldRoles,
): { team: string; state: SprintState } {
  let team = "";
  let current: string | null = null;
  let previous: string | null = null;
  for (const value of item.fieldValues.nodes) {
    const fieldID = value.field?.id;
    if (!fieldID) {
      continue;
    }
    if (roles.team && fieldID === roles.team.id && value.text) {
      team = value.text;
    } else if (
      roles.sprintStart &&
      fieldID === roles.sprintStart.id &&
      value.date
    ) {
      current = value.date;
    } else if (roles.start && fieldID === roles.start.id && value.date) {
      previous = value.date;
    }
  }
  return { team, state: { current, previous, itemId: item.id } };
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
    sprintStates: {},
  };
  const roles = fieldRoles(board);
  // Split the hidden sprint-state cards out of the normal cards: they carry the
  // per-team sprint pointers and must never render on a board view.
  const cards: Card[] = [];
  const sprintStates: Record<string, SprintState> = {};
  for (const item of raw.items.nodes) {
    if (item.content?.title === SPRINT_STATE_TITLE) {
      const { team, state } = parseSprintState(item, roles);
      sprintStates[team] = state;
    } else {
      cards.push(mapItem(item, roles));
    }
  }
  board.cards = cards;
  board.sprintStates = sprintStates;
  return board;
}

async function loadProject(owner: string, number: number): Promise<RawProject> {
  // Resolve org-vs-user once, then page through every item. A board can hold more
  // than the 100-item GraphQL page size, and without paging the newest cards fall
  // off the load and "disappear" after a refresh.
  let first: RawProject | undefined;
  let isOrg = true;
  try {
    const data = await graphql<ProjectResult>(ORG_PROJECT_QUERY, {
      owner,
      number,
      after: null,
    });
    first = data.organization?.projectV2 ?? undefined;
  } catch {
    // Owner is likely a user, not an org; fall through to the user query.
  }
  if (!first) {
    isOrg = false;
    const data = await graphql<ProjectResult>(USER_PROJECT_QUERY, {
      owner,
      number,
      after: null,
    });
    first = data.user?.projectV2 ?? undefined;
  }
  if (!first) {
    throw new Error(`Project #${number} not found for "${owner}"`);
  }

  const nodes = [...first.items.nodes];
  let pageInfo = first.items.pageInfo;
  const query = isOrg ? ORG_PROJECT_QUERY : USER_PROJECT_QUERY;
  while (pageInfo?.hasNextPage && pageInfo.endCursor) {
    const data = await graphql<ProjectResult>(query, {
      owner,
      number,
      after: pageInfo.endCursor,
    });
    const page = isOrg ? data.organization?.projectV2 : data.user?.projectV2;
    if (!page) {
      break;
    }
    nodes.push(...page.items.nodes);
    pageInfo = page.items.pageInfo;
  }
  return { ...first, items: { ...first.items, nodes } };
}

// Specs for lazily-created project fields. A missing field is created the first
// time a card/team change needs it, so aeman works on any fresh board.
interface FieldSpec {
  name: string;
  dataType?: "NUMBER" | "DATE" | "TEXT";
  options?: { name: string; color: string; description: string }[];
}

const STAGE_GH_COLOR: Record<StageKey, string> = {
  locked: "RED",
  review: "YELLOW",
  recurrent: "BLUE",
  done: "GREEN",
};

const FIELD_SPECS: Partial<Record<keyof FieldRoles, FieldSpec>> = {
  zone: {
    name: "Zone",
    options: ZONE_ORDER.map((z) => ({
      name: ZONES[z].title,
      color: ZONES[z].ghColors[0],
      description: ZONES[z].description,
    })),
  },
  stage: {
    name: "Stage",
    options: STAGE_ORDER.map((s) => ({
      name: STAGES[s].label,
      color: STAGE_GH_COLOR[s],
      description: STAGES[s].label,
    })),
  },
  progress: { name: "Progress", dataType: "NUMBER" },
  day: { name: "Day", dataType: "DATE" },
  start: { name: "Start", dataType: "DATE" },
  sprintStart: { name: "Sprint Start", dataType: "DATE" },
  plan: {
    name: "Plan",
    options: [
      { name: "Wed", color: "BLUE", description: "By Wednesday" },
      { name: "Fri", color: "PURPLE", description: "By Friday" },
    ],
  },
  week: { name: "Week", dataType: "DATE" },
  team: { name: "Team", dataType: "TEXT" },
  reviewOf: { name: "Review Of", dataType: "TEXT" },
};

interface CreatedField {
  id: string;
  name: string;
  dataType?: string;
  options?: { id: string; name: string; color: string }[];
}

/** ensureField returns the board field for a role, creating it on the project
 * if it does not exist yet (and recording it on the in-memory board). */
async function ensureField(
  board: Board,
  role: keyof FieldRoles,
  label: string,
): Promise<ProjectField> {
  const existing = fieldRoles(board)[role];
  if (existing) {
    return existing;
  }
  const spec = FIELD_SPECS[role];
  if (!spec) {
    throw new Error(`Project has no "${label}" field`);
  }
  let created: CreatedField;
  if (spec.options) {
    const data = await graphql<{ createProjectV2Field: { projectV2Field: CreatedField } }>(
      CREATE_SELECT_FIELD,
      { project: board.id, name: spec.name, options: spec.options },
    );
    created = data.createProjectV2Field.projectV2Field;
  } else {
    const data = await graphql<{ createProjectV2Field: { projectV2Field: CreatedField } }>(
      CREATE_FIELD,
      { project: board.id, name: spec.name, dataType: spec.dataType },
    );
    created = data.createProjectV2Field.projectV2Field;
  }
  const field: ProjectField = {
    id: created.id,
    name: created.name,
    dataType: created.dataType ?? "",
    options: created.options,
  };
  board.fields.push(field);
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

  async loadCards(
    board: Board,
    ids: string[],
  ): Promise<{ cards: Card[]; needsFullReload: boolean }> {
    if (ids.length === 0) {
      return { cards: [], needsFullReload: false };
    }
    const roles = fieldRoles(board);
    const data = await graphql<{ nodes: (RawItem | null)[] }>(
      CARDS_BY_ID_QUERY,
      { ids },
    );
    const cards: Card[] = [];
    for (const node of data.nodes) {
      if (!node) {
        continue; // the item was deleted
      }
      if (node.content?.title === SPRINT_STATE_TITLE) {
        // A sprint pointer moved (Carry Over): the board's sprint framing may
        // have shifted, so reconcile with a full reload instead of patching.
        return { cards: [], needsFullReload: true };
      }
      cards.push(mapItem(node, roles));
    }
    return { cards, needsFullReload: false };
  },

  async setZone(board: Board, card: Card, optionId: string | null): Promise<void> {
    const field = await ensureField(board, "zone", "Zone");
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
    const field = await ensureField(board, "progress", "Progress");
    await graphql(SET_NUMBER, {
      project: board.id,
      item: card.itemId,
      field: field.id,
      value: progress,
    });
  },

  async setDay(board: Board, card: Card, day: string | null): Promise<void> {
    const field = await ensureField(board, "day", "Day");
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

  async setStart(board: Board, card: Card, date: string | null): Promise<void> {
    const field = await ensureField(board, "start", "Start");
    if (date === null) {
      await graphql(CLEAR_FIELD, { project: board.id, item: card.itemId, field: field.id });
      return;
    }
    await graphql(SET_DATE, {
      project: board.id,
      item: card.itemId,
      field: field.id,
      value: date,
    });
  },

  async setSprintStart(board: Board, card: Card, date: string | null): Promise<void> {
    const field = await ensureField(board, "sprintStart", "Sprint Start");
    if (date === null) {
      await graphql(CLEAR_FIELD, { project: board.id, item: card.itemId, field: field.id });
      return;
    }
    await graphql(SET_DATE, {
      project: board.id,
      item: card.itemId,
      field: field.id,
      value: date,
    });
  },

  async setSprintStartMany(
    board: Board,
    cards: Card[],
    date: string,
  ): Promise<void> {
    if (cards.length === 0) {
      return;
    }
    const field = await ensureField(board, "sprintStart", "Sprint Start");
    // One request per card is far too slow for a whole sprint, so batch: each
    // request runs up to CHUNK aliased mutations, and the chunks go in parallel.
    const CHUNK = 25;
    const chunks: Card[][] = [];
    for (let i = 0; i < cards.length; i += CHUNK) {
      chunks.push(cards.slice(i, i + CHUNK));
    }
    await Promise.all(
      chunks.map((chunk) => {
        const varDefs = chunk.map((_, i) => `$item${i}: ID!`).join(", ");
        const body = chunk
          .map(
            (_, i) =>
              `m${i}: updateProjectV2ItemFieldValue(input: { projectId: $project, itemId: $item${i}, fieldId: $field, value: { date: $value } }) { projectV2Item { id } }`,
          )
          .join("\n");
        const query = `mutation($project: ID!, $field: ID!, $value: Date!, ${varDefs}) {\n${body}\n}`;
        const variables: Record<string, unknown> = {
          project: board.id,
          field: field.id,
          value: date,
        };
        chunk.forEach((c, i) => {
          variables[`item${i}`] = c.itemId;
        });
        return graphql(query, variables);
      }),
    );
  },

  async setSprintState(
    board: Board,
    team: string | null,
    current: string | null,
    previous: string | null,
  ): Promise<void> {
    // Reuse the team's existing hidden state card, or create one (a draft titled
    // SPRINT_STATE_TITLE, no assignees) tagged with the team name.
    let itemId = board.sprintStates[team ?? ""]?.itemId;
    if (!itemId) {
      const created = await graphql<{
        addProjectV2DraftIssue: { projectItem: { id: string } };
      }>(ADD_DRAFT, {
        project: board.id,
        title: SPRINT_STATE_TITLE,
        assignees: [],
      });
      itemId = created.addProjectV2DraftIssue.projectItem.id;
      if (team !== null) {
        const teamField = await ensureField(board, "team", "Team");
        await graphql(SET_TEXT, {
          project: board.id,
          item: itemId,
          field: teamField.id,
          value: team,
        });
      }
    }
    // Sprint Start = the current sprint; Start = the previous sprint.
    const sprintField = await ensureField(board, "sprintStart", "Sprint Start");
    if (current === null) {
      await graphql(CLEAR_FIELD, {
        project: board.id,
        item: itemId,
        field: sprintField.id,
      });
    } else {
      await graphql(SET_DATE, {
        project: board.id,
        item: itemId,
        field: sprintField.id,
        value: current,
      });
    }
    const startField = await ensureField(board, "start", "Start");
    if (previous === null) {
      await graphql(CLEAR_FIELD, {
        project: board.id,
        item: itemId,
        field: startField.id,
      });
    } else {
      await graphql(SET_DATE, {
        project: board.id,
        item: itemId,
        field: startField.id,
        value: previous,
      });
    }
  },

  async carryOver(board: Board, team: string | null): Promise<void> {
    // Client-side reference implementation: advance the pointer, then batch the
    // unfinished cards' Sprint Start writes. It mirrors boardservice.CarryOver.
    const key = team ?? "";
    const today = todayIso();
    const old = board.sprintStates[key]?.current ?? null;
    if (old === today) {
      return;
    }
    // Only the closing (current) sprint's unfinished cards carry, mirroring
    // boardservice.CarryOver (recurrent at 100% counts as complete).
    const carry = board.cards.filter(
      (c) =>
        (c.team ?? "") === key &&
        old !== null &&
        c.sprintStart === old &&
        c.stage !== "done" &&
        !((!c.stage || c.stage === "recurrent") && (c.progress ?? 0) >= 100) &&
        !c.itemId.startsWith("tmp-"),
    );
    await githubProvider.setSprintState(board, team, today, old);
    await githubProvider.setSprintStartMany(board, carry, today);
    // Finished recurrent cards of the closing sprint stay behind and seed the
    // new sprint with fresh copies, mirroring boardservice.CarryOver.
    const reseed = board.cards.filter(
      (c) =>
        (c.team ?? "") === key &&
        old !== null &&
        c.sprintStart === old &&
        c.stage === "recurrent" &&
        (c.progress ?? 0) >= 100 &&
        !c.itemId.startsWith("tmp-"),
    );
    for (const c of reseed) {
      const created = await githubProvider.createCard(board, {
        title: c.title,
        zone: c.zone,
        day: today,
        start: today,
        sprintStart: today,
        assigneeLogin: c.assignees[0] ?? null,
        team: team,
      });
      await githubProvider.setStage(board, created, "recurrent");
      if (c.description) {
        await githubProvider.setDescription(board, created, c.description);
      }
    }
  },

  async carryWeek(
    board: Board,
    team: string | null,
    week: string,
  ): Promise<void> {
    // Client-side reference implementation, mirroring boardservice.CarryWeek:
    // move the unfinished plan cards, reseed finished recurrent ones (skipping
    // titles already planned in the target week).
    const key = team ?? "";
    const inTarget = new Set(
      board.cards
        .filter((c) => c.plan && c.week === week && (c.team ?? "") === key)
        .map((c) => c.title),
    );
    for (const c of board.cards) {
      if (!c.plan || !c.week || c.week >= week || (c.team ?? "") !== key) {
        continue;
      }
      if (c.itemId.startsWith("tmp-")) {
        continue;
      }
      if (c.stage === "recurrent" && (c.progress ?? 0) >= 100) {
        if (inTarget.has(c.title)) {
          continue;
        }
        const created = await githubProvider.createCard(board, {
          title: c.title,
          zone: c.zone,
          plan: c.plan,
          week,
          team: team,
        });
        await githubProvider.setStage(board, created, "recurrent");
        if (c.description) {
          await githubProvider.setDescription(board, created, c.description);
        }
        inTarget.add(c.title);
        continue;
      }
      if (
        c.stage === "done" ||
        ((!c.stage || c.stage === "recurrent") && (c.progress ?? 0) >= 100)
      ) {
        continue;
      }
      await githubProvider.setWeek(board, c, week);
    }
  },

  async setPlan(board: Board, card: Card, plan: "wed" | "fri" | null): Promise<void> {
    const field = await ensureField(board, "plan", "Plan");
    if (plan === null) {
      await graphql(CLEAR_FIELD, { project: board.id, item: card.itemId, field: field.id });
      return;
    }
    const optionId = field.options?.find((o) => o.name.toLowerCase() === plan)?.id;
    if (!optionId) {
      throw new Error(`Plan field has no "${plan}" option`);
    }
    await graphql(SET_SINGLE_SELECT, {
      project: board.id,
      item: card.itemId,
      field: field.id,
      option: optionId,
    });
  },

  async setWeek(board: Board, card: Card, date: string | null): Promise<void> {
    const field = await ensureField(board, "week", "Week");
    if (date === null) {
      await graphql(CLEAR_FIELD, { project: board.id, item: card.itemId, field: field.id });
      return;
    }
    await graphql(SET_DATE, {
      project: board.id,
      item: card.itemId,
      field: field.id,
      value: date,
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

  async setStage(board: Board, card: Card, stage: StageKey | null): Promise<void> {
    const field = await ensureField(board, "stage", "Stage");
    if (stage === null) {
      await graphql(CLEAR_FIELD, { project: board.id, item: card.itemId, field: field.id });
      return;
    }
    const optionId = optionIdForStage(field, stage);
    if (!optionId) {
      throw new Error(`Project Stage field has no "${stage}" option`);
    }
    await graphql(SET_SINGLE_SELECT, {
      project: board.id,
      item: card.itemId,
      field: field.id,
      option: optionId,
    });
    if (stage === "done") {
      const progress = await ensureField(board, "progress", "Progress");
      await graphql(SET_NUMBER, {
        project: board.id,
        item: card.itemId,
        field: progress.id,
        value: 100,
      });
    }
  },

  async setTeam(board: Board, card: Card, team: string | null): Promise<void> {
    const field = await ensureField(board, "team", "Team");
    if (!team) {
      await graphql(CLEAR_FIELD, { project: board.id, item: card.itemId, field: field.id });
      return;
    }
    await graphql(SET_TEXT, {
      project: board.id,
      item: card.itemId,
      field: field.id,
      value: team,
    });
  },

  async renameCard(_board: Board, card: Card, title: string): Promise<void> {
    if (!card.contentId) {
      throw new Error("Card has no underlying content to rename");
    }
    if (card.isDraft) {
      await graphql(UPDATE_DRAFT_TITLE, { draft: card.contentId, title });
      return;
    }
    await graphql(UPDATE_ISSUE_TITLE, { id: card.contentId, title });
  },

  async setDescription(_board: Board, card: Card, description: string): Promise<void> {
    if (!card.contentId) {
      throw new Error("Card has no underlying content");
    }
    if (card.isDraft) {
      const data = await graphql<{ node?: { body?: string } | null }>(GET_DRAFT_BODY, {
        id: card.contentId,
      });
      const { notes } = parseDraftBody(data.node?.body, card.itemId);
      await graphql(UPDATE_DRAFT_BODY, {
        draft: card.contentId,
        body: buildDraftBody(description, notes),
      });
      return;
    }
    await graphql(UPDATE_ISSUE_BODY, { id: card.contentId, body: description });
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
    let zoneOptionId: string | undefined;
    if (input.zone) {
      const zoneField = await ensureField(board, "zone", "Zone");
      zoneOptionId = optionIdForZone(zoneField, input.zone);
      if (zoneOptionId) {
        await graphql(SET_SINGLE_SELECT, {
          project: board.id,
          item: item.id,
          field: zoneField.id,
          option: zoneOptionId,
        });
      }
    }
    if (input.day) {
      const field = await ensureField(board, "day", "Day");
      await graphql(SET_DATE, {
        project: board.id,
        item: item.id,
        field: field.id,
        value: input.day,
      });
    }
    if (input.start) {
      const field = await ensureField(board, "start", "Start");
      await graphql(SET_DATE, {
        project: board.id,
        item: item.id,
        field: field.id,
        value: input.start,
      });
    }
    if (input.sprintStart) {
      const field = await ensureField(board, "sprintStart", "Sprint Start");
      await graphql(SET_DATE, {
        project: board.id,
        item: item.id,
        field: field.id,
        value: input.sprintStart,
      });
    }
    if (input.team) {
      const field = await ensureField(board, "team", "Team");
      await graphql(SET_TEXT, {
        project: board.id,
        item: item.id,
        field: field.id,
        value: input.team,
      });
    }
    if (input.reviewOf) {
      const field = await ensureField(board, "reviewOf", "Review Of");
      await graphql(SET_TEXT, {
        project: board.id,
        item: item.id,
        field: field.id,
        value: input.reviewOf,
      });
    }
    if (input.plan) {
      const planField = await ensureField(board, "plan", "Plan");
      const optionId = planField.options?.find(
        (o) => o.name.toLowerCase() === input.plan,
      )?.id;
      if (optionId) {
        await graphql(SET_SINGLE_SELECT, {
          project: board.id,
          item: item.id,
          field: planField.id,
          option: optionId,
        });
      }
    }
    if (input.week) {
      const weekField = await ensureField(board, "week", "Week");
      await graphql(SET_DATE, {
        project: board.id,
        item: item.id,
        field: weekField.id,
        value: input.week,
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
      startDate: input.start ?? undefined,
      sprintStart: input.sprintStart ?? undefined,
      plan: input.plan ?? undefined,
      week: input.week ?? undefined,
      team: input.team ?? undefined,
      reviewOf: input.reviewOf ?? undefined,
      // The item was just created; without this the age badge (and its date
      // editor) would not render until the next full board reload.
      createdAt: new Date().toISOString(),
      description: "",
      notes: [],
    };
  },

  async deleteCard(board: Board, card: Card): Promise<void> {
    await graphql(DELETE_ITEM, { project: board.id, item: card.itemId });
  },

  async moveCard(board: Board, card: Card, afterItemId: string | null): Promise<void> {
    await graphql(MOVE_ITEM, {
      project: board.id,
      item: card.itemId,
      after: afterItemId,
    });
  },

  async addNote(_board: Board, card: Card, text: string): Promise<void> {
    if (!card.contentId) {
      throw new Error("Card has no underlying issue to note on");
    }
    if (card.isDraft) {
      const data = await graphql<{ node?: { body?: string } | null }>(GET_DRAFT_BODY, {
        id: card.contentId,
      });
      const { description, notes } = parseDraftBody(data.node?.body, card.itemId);
      notes.push({ id: "tmp", body: text, createdAt: new Date().toISOString(), source: "draft" });
      await graphql(UPDATE_DRAFT_BODY, {
        draft: card.contentId,
        body: buildDraftBody(description, notes),
      });
      return;
    }
    await graphql(ADD_COMMENT, { subject: card.contentId, body: text });
  },

  async editNote(_board: Board, card: Card, note: Note, text: string): Promise<void> {
    if (note.source === "comment") {
      await graphql(UPDATE_COMMENT, { id: note.id, body: text });
      return;
    }
    if (!card.contentId) {
      throw new Error("Card has no draft body to edit the note in");
    }
    const data = await graphql<{ node?: { body?: string } | null }>(GET_DRAFT_BODY, {
      id: card.contentId,
    });
    const { description, notes } = parseDraftBody(data.node?.body, card.itemId);
    const updated = notes.map((n) => (n.id === note.id ? { ...n, body: text } : n));
    await graphql(UPDATE_DRAFT_BODY, {
      draft: card.contentId,
      body: buildDraftBody(description, updated),
    });
  },

  async deleteNote(_board: Board, card: Card, note: Note): Promise<void> {
    if (note.source === "comment") {
      await graphql(DELETE_COMMENT, { id: note.id });
      return;
    }
    if (!card.contentId) {
      throw new Error("Card has no draft body to delete the note from");
    }
    const data = await graphql<{ node?: { body?: string } | null }>(GET_DRAFT_BODY, {
      id: card.contentId,
    });
    const { description, notes } = parseDraftBody(data.node?.body, card.itemId);
    const remaining = notes.filter((n) => n.id !== note.id);
    await graphql(UPDATE_DRAFT_BODY, {
      draft: card.contentId,
      body: buildDraftBody(description, remaining),
    });
  },
};
