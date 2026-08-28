package ghsource

import (
	"context"
	"fmt"
	"regexp"
)

// Raw GraphQL shapes (only the parts the loader reads).

type rawField struct {
	Typename string `json:"__typename"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	DataType string `json:"dataType"`
	Options  []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Color string `json:"color"`
	} `json:"options"`
}

type rawComment struct {
	ID        string `json:"id"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
	Author    *struct {
		Login string `json:"login"`
	} `json:"author"`
}

type rawContent struct {
	Typename string `json:"__typename"`
	ID       string `json:"id"`
	Number   int    `json:"number"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	State    string `json:"state"`
	Body     string `json:"body"`
	// Creator is a draft issue's author; Author is an issue/PR's author. They feed
	// the card's Author (draft → creator, issue/PR → author).
	Creator *struct {
		Login string `json:"login"`
	} `json:"creator"`
	Author *struct {
		Login string `json:"login"`
	} `json:"author"`
	Repository *struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	Assignees *struct {
		Nodes []struct {
			Login string `json:"login"`
		} `json:"nodes"`
	} `json:"assignees"`
	Comments *struct {
		Nodes []rawComment `json:"nodes"`
	} `json:"comments"`
}

type rawFieldValue struct {
	Typename string   `json:"__typename"`
	OptionID string   `json:"optionId"`
	Name     string   `json:"name"`
	Number   *float64 `json:"number"`
	Date     string   `json:"date"`
	Title    string   `json:"title"`
	Text     string   `json:"text"`
	Field    *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"field"`
}

type rawItem struct {
	ID          string      `json:"id"`
	Type        string      `json:"type"`
	CreatedAt   string      `json:"createdAt"`
	Content     *rawContent `json:"content"`
	FieldValues struct {
		Nodes []rawFieldValue `json:"nodes"`
	} `json:"fieldValues"`
}

type rawProject struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Fields struct {
		Nodes []rawField `json:"nodes"`
	} `json:"fields"`
	Items struct {
		PageInfo rawPageInfo `json:"pageInfo"`
		Nodes    []rawItem   `json:"nodes"`
	} `json:"items"`
}

// rawPageInfo is the items() connection cursor used to page a large board.
type rawPageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

type projectResult struct {
	Organization *struct {
		ProjectV2 *rawProject `json:"projectV2"`
	} `json:"organization"`
	User *struct {
		ProjectV2 *rawProject `json:"projectV2"`
	} `json:"user"`
}

// loadProject fetches the raw project, trying the org query first then user.
//
// GitHub answers a missing project on an existing org with a non-null
// organization, a null projectV2 and a NOT_FOUND error, so the decoded data
// (not the error alone) tells us whether the owner resolved. A confirmed
// organization with no such project maps straight to ErrBoardNotFound rather
// than falling through to the user query (which would error on an org login).
func (c *Client) loadProject(ctx context.Context, owner string, number int) (*rawProject, error) {
	vars := map[string]any{"owner": owner, "number": number, "after": nil}

	var orgData projectResult
	orgErr := c.graphql(ctx, orgProjectQuery, vars, &orgData)
	if orgData.Organization != nil {
		if orgData.Organization.ProjectV2 != nil {
			return c.pageProject(ctx, true, owner, number, orgData.Organization.ProjectV2)
		}
		return nil, fmt.Errorf("%w: project #%d for %q", ErrBoardNotFound, number, owner)
	}

	// Owner is not an organization (or the org itself was not found); try the
	// user query.
	var userData projectResult
	userErr := c.graphql(ctx, userProjectQuery, vars, &userData)
	if userData.User != nil {
		if userData.User.ProjectV2 != nil {
			return c.pageProject(ctx, false, owner, number, userData.User.ProjectV2)
		}
		return nil, fmt.Errorf("%w: project #%d for %q", ErrBoardNotFound, number, owner)
	}

	// Neither an org nor a user project resolved. Treat pure NOT_FOUND results
	// as a clean not-found, but surface any other upstream error (rate limit,
	// network, auth) so it is not masked as a missing board.
	if isNotFoundErr(orgErr) || isNotFoundErr(userErr) {
		return nil, fmt.Errorf("%w: project #%d for %q", ErrBoardNotFound, number, owner)
	}
	if userErr != nil {
		return nil, userErr
	}
	if orgErr != nil {
		return nil, orgErr
	}
	return nil, fmt.Errorf("%w: project #%d for %q", ErrBoardNotFound, number, owner)
}

// pageProject collects every remaining items() page for an already-resolved
// project, appending them onto the first page's nodes. A board can hold more
// than the 100-item GraphQL page size, and without paging the newest cards
// would silently fall off the migration.
func (c *Client) pageProject(ctx context.Context, isOrg bool, owner string, number int, first *rawProject) (*rawProject, error) {
	query := userProjectQuery
	if isOrg {
		query = orgProjectQuery
	}
	for first.Items.PageInfo.HasNextPage && first.Items.PageInfo.EndCursor != "" {
		vars := map[string]any{"owner": owner, "number": number, "after": first.Items.PageInfo.EndCursor}
		var data projectResult
		if err := c.graphql(ctx, query, vars, &data); err != nil {
			return nil, err
		}
		var next *rawProject
		switch {
		case isOrg && data.Organization != nil:
			next = data.Organization.ProjectV2
		case !isOrg && data.User != nil:
			next = data.User.ProjectV2
		}
		if next == nil {
			break
		}
		first.Items.Nodes = append(first.Items.Nodes, next.Items.Nodes...)
		first.Items.PageInfo = next.Items.PageInfo
	}
	return first, nil
}

// draftNoteRe matches a "[timestamp] text" line stored in a draft issue body.
var draftNoteRe = regexp.MustCompile(`^[-*]?\s*\[([^\]]+)\]\s?(.*)$`)
