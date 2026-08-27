package ghsource

// GraphQL documents for reading a GitHub Projects v2 board. Projects can be
// owned by an organization or a user, so the read comes as an org/user pair
// sharing the same body.

// itemNode is one project item's selection body.
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

const orgProjectQuery = `query($owner: String!, $number: Int!, $after: String) {
  organization(login: $owner) { projectV2(number: $number) { ` + projectBody + ` } }
}`

const userProjectQuery = `query($owner: String!, $number: Int!, $after: String) {
  user(login: $owner) { projectV2(number: $number) { ` + projectBody + ` } }
}`
