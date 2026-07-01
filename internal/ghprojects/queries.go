package ghprojects

// GraphQL documents for GitHub Projects v2, ported from
// web/src/providers/github/queries.ts. Projects can be owned by an organization
// or a user, so most reads come in org/user pairs sharing the same body.

// itemNode is one project item's selection body, shared by the full board query
// and the nodes(ids:) fetch that reloads just the cards a mutation changed.
const itemNode = `
  id
  type
  createdAt
  content {
    __typename
    ... on DraftIssue {
      id title body
      creator { login }
      assignees(first: 10) { nodes { login } }
    }
    ... on Issue {
      id number title url state
      author { login }
      repository { nameWithOwner }
      assignees(first: 10) { nodes { login } }
      comments(last: 20) { nodes { id body createdAt author { login } } }
    }
    ... on PullRequest {
      id number title url state
      author { login }
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
`

const projectBody = `
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
    nodes { ` + itemNode + ` }
  }
`

// cardsByIDQuery fetches specific project items by node id (a deleted id comes
// back null). It backs the fast single-card reload the live-update path uses
// instead of paging the whole board.
const cardsByIDQuery = `query($ids: [ID!]!) {
  nodes(ids: $ids) {
    __typename
    ... on ProjectV2Item { ` + itemNode + ` }
  }
}`

const orgProjectQuery = `query($owner: String!, $number: Int!, $after: String) {
  organization(login: $owner) { projectV2(number: $number) { ` + projectBody + ` } }
}`

const userProjectQuery = `query($owner: String!, $number: Int!, $after: String) {
  user(login: $owner) { projectV2(number: $number) { ` + projectBody + ` } }
}`

const projectsListBody = `
  projectsV2(first: 50, orderBy: { field: NUMBER, direction: DESC }) {
    nodes { id number title url shortDescription }
  }
`

const orgProjectsQuery = `query($owner: String!) {
  organization(login: $owner) { ` + projectsListBody + ` }
}`

const userProjectsQuery = `query($owner: String!) {
  user(login: $owner) { ` + projectsListBody + ` }
}`

const userIDQuery = `query($login: String!) {
  user(login: $login) { id }
}`

const getDraftBodyQuery = `query($id: ID!) {
  node(id: $id) { ... on DraftIssue { body } }
}`

const setSingleSelectMutation = `mutation($project: ID!, $item: ID!, $field: ID!, $option: String!) {
  updateProjectV2ItemFieldValue(input: {
    projectId: $project, itemId: $item, fieldId: $field,
    value: { singleSelectOptionId: $option }
  }) { projectV2Item { id } }
}`

const setNumberMutation = `mutation($project: ID!, $item: ID!, $field: ID!, $value: Float!) {
  updateProjectV2ItemFieldValue(input: {
    projectId: $project, itemId: $item, fieldId: $field,
    value: { number: $value }
  }) { projectV2Item { id } }
}`

const setDateMutation = `mutation($project: ID!, $item: ID!, $field: ID!, $value: Date!) {
  updateProjectV2ItemFieldValue(input: {
    projectId: $project, itemId: $item, fieldId: $field,
    value: { date: $value }
  }) { projectV2Item { id } }
}`

const setTextMutation = `mutation($project: ID!, $item: ID!, $field: ID!, $value: String!) {
  updateProjectV2ItemFieldValue(input: {
    projectId: $project, itemId: $item, fieldId: $field,
    value: { text: $value }
  }) { projectV2Item { id } }
}`

const clearFieldMutation = `mutation($project: ID!, $item: ID!, $field: ID!) {
  clearProjectV2ItemFieldValue(input: {
    projectId: $project, itemId: $item, fieldId: $field
  }) { projectV2Item { id } }
}`

const addDraftMutation = `mutation($project: ID!, $title: String!, $assignees: [ID!]) {
  addProjectV2DraftIssue(input: { projectId: $project, title: $title, assigneeIds: $assignees }) {
    projectItem { id content { ... on DraftIssue { id } } }
  }
}`

const createFieldMutation = `mutation($project: ID!, $name: String!, $dataType: ProjectV2CustomFieldType!) {
  createProjectV2Field(input: { projectId: $project, name: $name, dataType: $dataType }) {
    projectV2Field { ... on ProjectV2FieldCommon { id name dataType } }
  }
}`

const createSelectFieldMutation = `mutation($project: ID!, $name: String!, $options: [ProjectV2SingleSelectFieldOptionInput!]!) {
  createProjectV2Field(input: { projectId: $project, name: $name, dataType: SINGLE_SELECT, singleSelectOptions: $options }) {
    projectV2Field { ... on ProjectV2SingleSelectField { id name dataType options { id name color } } }
  }
}`

const deleteItemMutation = `mutation($project: ID!, $item: ID!) {
  deleteProjectV2Item(input: { projectId: $project, itemId: $item }) { deletedItemId }
}`

const moveItemMutation = `mutation($project: ID!, $item: ID!, $after: ID) {
  updateProjectV2ItemPosition(input: { projectId: $project, itemId: $item, afterId: $after }) {
    items(first: 1) { nodes { id } }
  }
}`

const updateDraftTitleMutation = `mutation($draft: ID!, $title: String!) {
  updateProjectV2DraftIssue(input: { draftIssueId: $draft, title: $title }) {
    draftIssue { id }
  }
}`

const updateDraftAssigneesMutation = `mutation($draft: ID!, $assignees: [ID!]) {
  updateProjectV2DraftIssue(input: { draftIssueId: $draft, assigneeIds: $assignees }) {
    draftIssue { id }
  }
}`

const updateDraftBodyMutation = `mutation($draft: ID!, $body: String!) {
  updateProjectV2DraftIssue(input: { draftIssueId: $draft, body: $body }) {
    draftIssue { id }
  }
}`

const updateIssueTitleMutation = `mutation($id: ID!, $title: String!) {
  updateIssue(input: { id: $id, title: $title }) { issue { id } }
}`

const updateIssueBodyMutation = `mutation($id: ID!, $body: String!) {
  updateIssue(input: { id: $id, body: $body }) { issue { id } }
}`

const updateCommentMutation = `mutation($id: ID!, $body: String!) {
  updateIssueComment(input: { id: $id, body: $body }) { clientMutationId }
}`

const deleteCommentMutation = `mutation($id: ID!) {
  deleteIssueComment(input: { id: $id }) { clientMutationId }
}`

const addCommentMutation = `mutation($subject: ID!, $body: String!) {
  addComment(input: { subjectId: $subject, body: $body }) {
    commentEdge { node { id createdAt body author { login } } }
  }
}`

const addAssigneesMutation = `mutation($assignable: ID!, $assignees: [ID!]!) {
  addAssigneesToAssignable(input: { assignableId: $assignable, assigneeIds: $assignees }) {
    clientMutationId
  }
}`

const removeAssigneesMutation = `mutation($assignable: ID!, $assignees: [ID!]!) {
  removeAssigneesFromAssignable(input: { assignableId: $assignable, assigneeIds: $assignees }) {
    clientMutationId
  }
}`
