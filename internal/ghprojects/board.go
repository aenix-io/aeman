package ghprojects

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Raw GraphQL shapes (only the parts aeman reads).

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
	Typename   string `json:"__typename"`
	ID         string `json:"id"`
	Number     int    `json:"number"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	State      string `json:"state"`
	Body       string `json:"body"`
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
		Nodes []rawItem `json:"nodes"`
	} `json:"items"`
}

type projectResult struct {
	Organization *struct {
		ProjectV2 *rawProject `json:"projectV2"`
	} `json:"organization"`
	User *struct {
		ProjectV2 *rawProject `json:"projectV2"`
	} `json:"user"`
}

type projectsListResult struct {
	Organization *struct {
		ProjectsV2 struct {
			Nodes []BoardSummary `json:"nodes"`
		} `json:"projectsV2"`
	} `json:"organization"`
	User *struct {
		ProjectsV2 struct {
			Nodes []BoardSummary `json:"nodes"`
		} `json:"projectsV2"`
	} `json:"user"`
}

// ListBoards returns the projects owned by owner (organization or user).
func (c *Client) ListBoards(ctx context.Context, owner string) ([]BoardSummary, error) {
	var orgData projectsListResult
	if err := c.graphql(ctx, orgProjectsQuery, map[string]any{"owner": owner}, &orgData); err == nil {
		if orgData.Organization != nil {
			return orgData.Organization.ProjectsV2.Nodes, nil
		}
	}
	var userData projectsListResult
	if err := c.graphql(ctx, userProjectsQuery, map[string]any{"owner": owner}, &userData); err != nil {
		return nil, err
	}
	if userData.User != nil {
		return userData.User.ProjectsV2.Nodes, nil
	}
	return nil, nil
}

// LoadBoard loads a project board fully, mapping fields and cards.
func (c *Client) LoadBoard(ctx context.Context, owner string, number int) (*Board, error) {
	raw, err := c.loadProject(ctx, owner, number)
	if err != nil {
		return nil, err
	}
	return mapProject(owner, raw), nil
}

// loadProject fetches the raw project, trying the org query first then user.
func (c *Client) loadProject(ctx context.Context, owner string, number int) (*rawProject, error) {
	vars := map[string]any{"owner": owner, "number": number}
	var orgData projectResult
	if err := c.graphql(ctx, orgProjectQuery, vars, &orgData); err == nil {
		if orgData.Organization != nil && orgData.Organization.ProjectV2 != nil {
			return orgData.Organization.ProjectV2, nil
		}
	}
	var userData projectResult
	if err := c.graphql(ctx, userProjectQuery, vars, &userData); err != nil {
		return nil, err
	}
	if userData.User != nil && userData.User.ProjectV2 != nil {
		return userData.User.ProjectV2, nil
	}
	return nil, fmt.Errorf("%w: project #%d for %q", ErrBoardNotFound, number, owner)
}

func mapProject(owner string, raw *rawProject) *Board {
	board := &Board{
		ID:     raw.ID,
		Number: raw.Number,
		Title:  raw.Title,
		URL:    raw.URL,
		Owner:  owner,
	}
	for _, f := range raw.Fields.Nodes {
		if f.ID == "" || f.Name == "" {
			continue
		}
		field := ProjectField{ID: f.ID, Name: f.Name, DataType: f.DataType}
		for _, o := range f.Options {
			field.Options = append(field.Options, SingleSelectOption{ID: o.ID, Name: o.Name, Color: o.Color})
		}
		board.Fields = append(board.Fields, field)
	}
	roles := board.roles()
	for i := range raw.Items.Nodes {
		board.Cards = append(board.Cards, mapItem(&raw.Items.Nodes[i], roles))
	}
	return board
}

func mapItem(item *rawItem, roles FieldRoles) Card {
	content := item.Content
	isDraft := item.Type == "DRAFT_ISSUE" || (content != nil && content.Typename == "DraftIssue")
	card := Card{
		ItemID:    item.ID,
		Title:     "(untitled)",
		IsDraft:   isDraft,
		CreatedAt: item.CreatedAt,
		Assignees: []string{},
		Notes:     []Note{},
	}
	if content != nil {
		card.ContentID = content.ID
		if content.Title != "" {
			card.Title = content.Title
		}
		card.URL = content.URL
		card.Number = content.Number
		card.State = content.State
		if content.Repository != nil {
			card.Repository = content.Repository.NameWithOwner
		}
		if content.Assignees != nil {
			for _, a := range content.Assignees.Nodes {
				card.Assignees = append(card.Assignees, a.Login)
			}
		}
		card.Notes = parseNotes(content, item.ID)
	}
	applyFieldValues(&card, item.FieldValues.Nodes, roles)
	return card
}

// applyFieldValues maps each item field value onto the card's typed roles and
// the generic Fields map.
func applyFieldValues(card *Card, values []rawFieldValue, roles FieldRoles) {
	for i := range values {
		v := &values[i]
		if v.Field == nil || v.Field.ID == "" {
			continue
		}
		if s := stringifyValue(v); s != "" {
			if card.Fields == nil {
				card.Fields = map[string]string{}
			}
			card.Fields[v.Field.Name] = s
		}
		applyRole(card, v, roles)
	}
}

// applyRole records a field value under the matching well-known role.
func applyRole(card *Card, v *rawFieldValue, roles FieldRoles) {
	id := v.Field.ID
	switch {
	case roles.Zone != nil && id == roles.Zone.ID && v.OptionID != "":
		card.ZoneOptionID = v.OptionID
		for _, o := range roles.Zone.Options {
			if o.ID == v.OptionID {
				card.Zone = zoneFromColor(o.Color)
			}
		}
	case roles.Progress != nil && id == roles.Progress.ID && v.Number != nil:
		card.Progress = v.Number
	case roles.Day != nil && id == roles.Day.ID && v.Date != "":
		card.Day = v.Date
	case roles.Sprint != nil && id == roles.Sprint.ID && v.Title != "":
		card.SprintTitle = v.Title
	case roles.Status != nil && id == roles.Status.ID && v.Name != "":
		card.Status = v.Name
	case roles.Team != nil && id == roles.Team.ID:
		card.Team = stringifyValue(v)
	}
}

// stringifyValue renders a field value as a display string.
func stringifyValue(v *rawFieldValue) string {
	switch {
	case v.Name != "":
		return v.Name
	case v.Text != "":
		return v.Text
	case v.Date != "":
		return v.Date
	case v.Title != "":
		return v.Title
	case v.Number != nil:
		return strconv.FormatFloat(*v.Number, 'f', -1, 64)
	default:
		return ""
	}
}

// draftNoteRe matches a "[timestamp] text" line stored in a draft issue body.
var draftNoteRe = regexp.MustCompile(`^[-*]?\s*\[([^\]]+)\]\s?(.*)$`)

// parseNotes extracts notes from issue comments, or from a draft body when the
// card has no comment thread.
func parseNotes(content *rawContent, itemID string) []Note {
	if content == nil {
		return []Note{}
	}
	if content.Comments != nil && len(content.Comments.Nodes) > 0 {
		notes := make([]Note, 0, len(content.Comments.Nodes))
		for _, cm := range content.Comments.Nodes {
			n := Note{ID: cm.ID, Body: cm.Body, CreatedAt: cm.CreatedAt, Source: "comment"}
			if cm.Author != nil {
				n.Author = cm.Author.Login
			}
			notes = append(notes, n)
		}
		return notes
	}
	if content.Body != "" {
		var notes []Note
		for i, line := range strings.Split(content.Body, "\n") {
			m := draftNoteRe.FindStringSubmatch(strings.TrimSpace(line))
			if m != nil {
				notes = append(notes, Note{
					ID:        fmt.Sprintf("%s:%d", itemID, i),
					Body:      m[2],
					CreatedAt: m[1],
					Source:    "draft",
				})
			}
		}
		if notes != nil {
			return notes
		}
	}
	return []Note{}
}
