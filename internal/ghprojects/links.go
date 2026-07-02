package ghprojects

import (
	"context"
	"fmt"
	"strings"

	"github.com/aenix-org/aeman/internal/board"
)

// ResolveIssueRef fills a GitHub issue/PR link with its live title and state.
// It satisfies boardservice.LinkResolver.
func (c *Client) ResolveIssueRef(ctx context.Context, link board.Link) (board.Link, error) {
	if !link.IsGitHubRef() {
		return link, fmt.Errorf("not a github issue/pull link: %s", link.URL)
	}
	var out struct {
		Repository struct {
			IssueOrPullRequest struct {
				Typename string `json:"__typename"`
				Title    string `json:"title"`
				State    string `json:"state"`
				IsDraft  bool   `json:"isDraft"`
			} `json:"issueOrPullRequest"`
		} `json:"repository"`
	}
	err := c.graphql(ctx, issueRefQuery, map[string]any{
		"owner": link.Owner, "repo": link.Repo, "number": link.Number,
	}, &out)
	if err != nil {
		return link, err
	}
	ref := out.Repository.IssueOrPullRequest
	if ref.Typename == "" {
		return link, fmt.Errorf("issue or PR %s/%s#%d not found", link.Owner, link.Repo, link.Number)
	}
	if ref.Typename == "PullRequest" {
		link.Kind = "pull"
	} else {
		link.Kind = "issue"
	}
	link.Title = ref.Title
	link.State = strings.ToLower(ref.State)
	if ref.IsDraft && link.State == "open" {
		link.State = "draft"
	}
	return link, nil
}
