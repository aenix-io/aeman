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
  items(first: 100) {
    nodes {
      id
      type
      content {
        __typename
        ... on DraftIssue { title }
        ... on Issue {
          number title url state
          repository { nameWithOwner }
          assignees(first: 10) { nodes { login } }
        }
        ... on PullRequest {
          number title url state
          repository { nameWithOwner }
          assignees(first: 10) { nodes { login } }
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

export const ORG_PROJECT_QUERY = `query($owner: String!, $number: Int!) {
  organization(login: $owner) { projectV2(number: $number) { ${PROJECT_BODY} } }
}`;

export const USER_PROJECT_QUERY = `query($owner: String!, $number: Int!) {
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

export const CLEAR_FIELD = `mutation($project: ID!, $item: ID!, $field: ID!) {
  clearProjectV2ItemFieldValue(input: {
    projectId: $project, itemId: $item, fieldId: $field
  }) { projectV2Item { id } }
}`;
