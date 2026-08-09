package ghprojects

import (
	"context"
	"fmt"
)

// The access probe fetches nothing but the project's node id — the cheapest
// possible question GitHub can answer about a board (~0.5s), against the ~2s
// per 100-card page a full load pays a dozen times over. It exists for the
// board store: proving that a signed-in user's token can read an
// already-cached board must not cost that user a full multi-page reload.
const orgProbeQuery = `query($owner: String!, $number: Int!) {
  organization(login: $owner) { projectV2(number: $number) { id } }
}`

const userProbeQuery = `query($owner: String!, $number: Int!) {
  user(login: $owner) { projectV2(number: $number) { id } }
}`

// CheckBoardAccess reports whether the client's token can read the project:
// nil when GitHub resolves it, ErrBoardNotFound when the project does not
// exist or the token cannot see it (GitHub does not distinguish the two), and
// the upstream error otherwise (rate limit, network, auth), mirroring
// loadProject's org-then-user resolution.
func (c *Client) CheckBoardAccess(ctx context.Context, owner string, number int) error {
	vars := map[string]any{"owner": owner, "number": number}

	var orgData projectResult
	orgErr := c.graphql(ctx, orgProbeQuery, vars, &orgData)
	if orgData.Organization != nil {
		if orgData.Organization.ProjectV2 != nil {
			return nil
		}
		return fmt.Errorf("%w: project #%d for %q", ErrBoardNotFound, number, owner)
	}

	var userData projectResult
	userErr := c.graphql(ctx, userProbeQuery, vars, &userData)
	if userData.User != nil {
		if userData.User.ProjectV2 != nil {
			return nil
		}
		return fmt.Errorf("%w: project #%d for %q", ErrBoardNotFound, number, owner)
	}

	if isNotFoundErr(orgErr) || isNotFoundErr(userErr) {
		return fmt.Errorf("%w: project #%d for %q", ErrBoardNotFound, number, owner)
	}
	if userErr != nil {
		return userErr
	}
	if orgErr != nil {
		return orgErr
	}
	return fmt.Errorf("%w: project #%d for %q", ErrBoardNotFound, number, owner)
}
