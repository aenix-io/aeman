// GraphQL documents for GitHub Projects v2. Projects can be owned by an
// organization or a user, so most reads come in org/user pairs that share the
// same selection body.

const PROJECT_BODY = `
  id
  number
  title
  url
  fields(first: 50) {
    nodes {
      __typename
      ... on ProjectV2FieldCommon { id name dataType }
      ... on ProjectV2SingleSelectField { id name dataType options { id name color } }
    }
  }
  items(first: 100, after: $after) {
    pageInfo { hasNextPage endCursor }
    nodes {
      id
      type
      createdAt
      content {
        __typename
        ... on DraftIssue {
          id title body
          assignees(first: 10) { nodes { login } }
        }
        ... on Issue {
          id number title body url state
          repository { nameWithOwner }
          assignees(first: 10) { nodes { login } }
          comments(last: 20) { nodes { id body createdAt author { login } } }
        }
        ... on PullRequest {
          id number title body url state
          repository { nameWithOwner }
          assignees(first: 10) { nodes { login } }
          comments(last: 20) { nodes { id body createdAt author { login } } }
        }
      }
      fieldValues(first: 30) {
        nodes {
          __typename
          ... on ProjectV2ItemFieldSingleSelectValue {
            optionId name
            field { ... on ProjectV2FieldCommon { id name } }
          }
          ... on ProjectV2ItemFieldNumberValue {
            number
            field { ... on ProjectV2FieldCommon { id name } }
          }
          ... on ProjectV2ItemFieldDateValue {
            date
            field { ... on ProjectV2FieldCommon { id name } }
          }
          ... on ProjectV2ItemFieldTextValue {
            text
            field { ... on ProjectV2FieldCommon { id name } }
          }
          ... on ProjectV2ItemFieldIterationValue {
            title
            field { ... on ProjectV2FieldCommon { id name } }
          }
        }
      }
    }
  }
`;

export const ORG_PROJECT_QUERY = `query($owner: String!, $number: Int!, $after: String) {
  organization(login: $owner) { projectV2(number: $number) { ${PROJECT_BODY} } }
}`;

export const USER_PROJECT_QUERY = `query($owner: String!, $number: Int!, $after: String) {
  user(login: $owner) { projectV2(number: $number) { ${PROJECT_BODY} } }
}`;

const PROJECTS_LIST_BODY = `
  projectsV2(first: 50, orderBy: { field: NUMBER, direction: DESC }) {
    nodes { id number title url shortDescription }
  }
`;

export const ORG_PROJECTS_QUERY = `query($owner: String!) {
  organization(login: $owner) { ${PROJECTS_LIST_BODY} }
}`;

export const USER_PROJECTS_QUERY = `query($owner: String!) {
  user(login: $owner) { ${PROJECTS_LIST_BODY} }
}`;

export const USER_ID_QUERY = `query($login: String!) {
  user(login: $login) { id }
}`;

export const GET_DRAFT_BODY = `query($id: ID!) {
  node(id: $id) { ... on DraftIssue { body } }
}`;

export const SET_SINGLE_SELECT = `mutation($project: ID!, $item: ID!, $field: ID!, $option: String!) {
  updateProjectV2ItemFieldValue(input: {
    projectId: $project, itemId: $item, fieldId: $field,
    value: { singleSelectOptionId: $option }
  }) { projectV2Item { id } }
}`;

export const SET_NUMBER = `mutation($project: ID!, $item: ID!, $field: ID!, $value: Float!) {
  updateProjectV2ItemFieldValue(input: {
    projectId: $project, itemId: $item, fieldId: $field,
    value: { number: $value }
  }) { projectV2Item { id } }
}`;

export const SET_DATE = `mutation($project: ID!, $item: ID!, $field: ID!, $value: Date!) {
  updateProjectV2ItemFieldValue(input: {
    projectId: $project, itemId: $item, fieldId: $field,
    value: { date: $value }
  }) { projectV2Item { id } }
}`;

export const SET_TEXT = `mutation($project: ID!, $item: ID!, $field: ID!, $value: String!) {
  updateProjectV2ItemFieldValue(input: {
    projectId: $project, itemId: $item, fieldId: $field,
    value: { text: $value }
  }) { projectV2Item { id } }
}`;

export const CLEAR_FIELD = `mutation($project: ID!, $item: ID!, $field: ID!) {
  clearProjectV2ItemFieldValue(input: {
    projectId: $project, itemId: $item, fieldId: $field
  }) { projectV2Item { id } }
}`;

export const ADD_DRAFT = `mutation($project: ID!, $title: String!, $assignees: [ID!]) {
  addProjectV2DraftIssue(input: { projectId: $project, title: $title, assigneeIds: $assignees }) {
    projectItem { id content { ... on DraftIssue { id } } }
  }
}`;

export const CREATE_FIELD = `mutation($project: ID!, $name: String!, $dataType: ProjectV2CustomFieldType!) {
  createProjectV2Field(input: { projectId: $project, name: $name, dataType: $dataType }) {
    projectV2Field { ... on ProjectV2FieldCommon { id name dataType } }
  }
}`;

export const CREATE_SELECT_FIELD = `mutation($project: ID!, $name: String!, $options: [ProjectV2SingleSelectFieldOptionInput!]!) {
  createProjectV2Field(input: { projectId: $project, name: $name, dataType: SINGLE_SELECT, singleSelectOptions: $options }) {
    projectV2Field { ... on ProjectV2SingleSelectField { id name dataType options { id name color } } }
  }
}`;

export const DELETE_ITEM = `mutation($project: ID!, $item: ID!) {
  deleteProjectV2Item(input: { projectId: $project, itemId: $item }) { deletedItemId }
}`;

export const MOVE_ITEM = `mutation($project: ID!, $item: ID!, $after: ID) {
  updateProjectV2ItemPosition(input: { projectId: $project, itemId: $item, afterId: $after }) {
    clientMutationId
  }
}`;

export const UPDATE_DRAFT_ASSIGNEES = `mutation($draft: ID!, $assignees: [ID!]) {
  updateProjectV2DraftIssue(input: { draftIssueId: $draft, assigneeIds: $assignees }) {
    draftIssue { id }
  }
}`;

export const UPDATE_DRAFT_BODY = `mutation($draft: ID!, $body: String!) {
  updateProjectV2DraftIssue(input: { draftIssueId: $draft, body: $body }) {
    draftIssue { id }
  }
}`;

export const UPDATE_DRAFT_TITLE = `mutation($draft: ID!, $title: String!) {
  updateProjectV2DraftIssue(input: { draftIssueId: $draft, title: $title }) {
    draftIssue { id }
  }
}`;

export const UPDATE_ISSUE_TITLE = `mutation($id: ID!, $title: String!) {
  updateIssue(input: { id: $id, title: $title }) { issue { id } }
}`;

export const UPDATE_ISSUE_BODY = `mutation($id: ID!, $body: String!) {
  updateIssue(input: { id: $id, body: $body }) { issue { id } }
}`;

export const ADD_COMMENT = `mutation($subject: ID!, $body: String!) {
  addComment(input: { subjectId: $subject, body: $body }) {
    commentEdge { node { id createdAt body author { login } } }
  }
}`;

export const UPDATE_COMMENT = `mutation($id: ID!, $body: String!) {
  updateIssueComment(input: { id: $id, body: $body }) { clientMutationId }
}`;

export const DELETE_COMMENT = `mutation($id: ID!) {
  deleteIssueComment(input: { id: $id }) { clientMutationId }
}`;

export const ADD_ASSIGNEES = `mutation($assignable: ID!, $assignees: [ID!]!) {
  addAssigneesToAssignable(input: { assignableId: $assignable, assigneeIds: $assignees }) {
    clientMutationId
  }
}`;

export const REMOVE_ASSIGNEES = `mutation($assignable: ID!, $assignees: [ID!]!) {
  removeAssigneesFromAssignable(input: { assignableId: $assignable, assigneeIds: $assignees }) {
    clientMutationId
  }
}`;
