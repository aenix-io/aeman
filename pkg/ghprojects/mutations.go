package ghprojects

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// userIDCache memoises login -> node id lookups for the lifetime of a client.
type userIDCache struct {
	mu sync.Mutex
	m  map[string]string
}

// resolveUserID returns the node id of a GitHub login, caching the result.
func (c *Client) resolveUserID(ctx context.Context, login string) (string, error) {
	c.userCache.mu.Lock()
	if c.userCache.m == nil {
		c.userCache.m = map[string]string{}
	}
	if id, ok := c.userCache.m[login]; ok {
		c.userCache.mu.Unlock()
		return id, nil
	}
	c.userCache.mu.Unlock()

	var data struct {
		User *struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := c.graphql(ctx, userIDQuery, map[string]any{"login": login}, &data); err != nil {
		return "", err
	}
	if data.User == nil || data.User.ID == "" {
		return "", fmt.Errorf("github user %q not found", login)
	}
	c.userCache.mu.Lock()
	c.userCache.m[login] = data.User.ID
	c.userCache.mu.Unlock()
	return data.User.ID, nil
}

// CreateProjectCard creates a draft-issue card on the board and applies the
// optional fields, returning the loaded rich card. The domain-typed create used
// by the board service is the CreateCard method in setters.go.
func (c *Client) CreateProjectCard(ctx context.Context, board *Board, in CreateCardInput) (*Card, error) {
	if strings.TrimSpace(in.Title) == "" {
		return nil, fmt.Errorf("title is required")
	}

	// Sprint-aware date assignment, mirroring the Team board's create rule. It
	// engages when the caller sets startNewSprint, or when the card has a team
	// (so a teamless create with no flag keeps the legacy behaviour). Start date
	// always equals the sprint start; day defaults to today. Fields the board
	// lacks are skipped, so this never fails a create on a leaner board.
	if in.StartNewSprint != nil || in.Team != "" {
		today := time.Now().Format("2006-01-02")
		sprintStart := board.sprintStartForNew(in.Team, in.StartNewSprint, today)
		roles := board.roles()
		if roles.SprintStart != nil {
			in.SprintStart = sprintStart
		}
		if roles.Start != nil {
			in.Start = sprintStart
		}
		if in.Day == "" && roles.Day != nil {
			in.Day = today
		}
	}
	var assigneeIDs []string
	if in.Assignee != "" {
		id, err := c.resolveUserID(ctx, in.Assignee)
		if err != nil {
			return nil, err
		}
		assigneeIDs = []string{id}
	}

	var created struct {
		AddProjectV2DraftIssue struct {
			ProjectItem struct {
				ID      string `json:"id"`
				Content *struct {
					ID string `json:"id"`
				} `json:"content"`
			} `json:"projectItem"`
		} `json:"addProjectV2DraftIssue"`
	}
	vars := map[string]any{
		"project": board.ID, "title": in.Title, "body": in.Body, "assignees": assigneeIDs,
	}
	if err := c.graphql(ctx, addDraftMutation, vars, &created); err != nil {
		return nil, err
	}
	itemID := created.AddProjectV2DraftIssue.ProjectItem.ID

	update := UpdateCardInput{Fields: in.Fields}
	if in.Zone != "" {
		z := in.Zone
		update.Zone = &z
	}
	if in.Day != "" {
		update.Day = &in.Day
	}
	if in.Start != "" {
		update.Start = &in.Start
	}
	if in.SprintStart != "" {
		update.SprintStart = &in.SprintStart
	}
	if in.Status != "" {
		update.Status = &in.Status
	}
	if in.Team != "" {
		update.Team = &in.Team
	}
	update.Progress = in.Progress
	if err := c.applyUpdate(ctx, board, itemID, false, "", nil, update); err != nil {
		return nil, err
	}

	reloaded, err := c.LoadProjectBoard(ctx, board.Owner, board.Number)
	if err == nil {
		if card := reloaded.cardByItemID(itemID); card != nil {
			return card, nil
		}
	}
	return &Card{ItemID: itemID, Title: in.Title, IsDraft: true, Assignees: []string{}, Notes: []Note{}}, nil
}

// UpdateCard applies a partial update to the card identified by itemID.
func (c *Client) UpdateCard(ctx context.Context, board *Board, itemID string, in UpdateCardInput) error {
	card := board.cardByItemID(itemID)
	if card == nil {
		return fmt.Errorf("%w: %s", ErrCardNotFound, itemID)
	}
	return c.applyUpdate(ctx, board, itemID, card.IsDraft, card.ContentID, card.Assignees, in)
}

// applyUpdate performs each requested field mutation. isDraft, contentID and
// assignees describe the target card (empty/nil for a freshly created draft).
func (c *Client) applyUpdate(
	ctx context.Context, board *Board, itemID string,
	isDraft bool, contentID string, assignees []string, in UpdateCardInput,
) error {
	if in.Title != nil {
		if err := c.setTitle(ctx, isDraft, contentID, *in.Title); err != nil {
			return err
		}
	}
	if in.Zone != nil {
		if err := c.setZone(ctx, board, itemID, *in.Zone); err != nil {
			return err
		}
	}
	if in.Progress != nil {
		if err := c.setNumberRole(ctx, board, itemID, board.roles().Progress, "progress", *in.Progress); err != nil {
			return err
		}
	}
	if in.Day != nil {
		if err := c.setDateRole(ctx, board, itemID, board.roles().Day, "day", *in.Day); err != nil {
			return err
		}
	}
	if in.Start != nil {
		if err := c.setDateRole(ctx, board, itemID, board.roles().Start, "start", *in.Start); err != nil {
			return err
		}
	}
	if in.SprintStart != nil {
		if err := c.setDateRole(ctx, board, itemID, board.roles().SprintStart, "sprintStart", *in.SprintStart); err != nil {
			return err
		}
	}
	if in.Status != nil {
		if err := c.setSingleSelectRole(ctx, board, itemID, board.roles().Status, "status", *in.Status); err != nil {
			return err
		}
	}
	if in.Team != nil {
		if err := c.setRoleByValue(ctx, board, itemID, board.roles().Team, "team", *in.Team); err != nil {
			return err
		}
	}
	if in.Assignee != nil {
		if err := c.setAssignee(ctx, isDraft, contentID, assignees, *in.Assignee); err != nil {
			return err
		}
	}
	return c.setGenericFields(ctx, board, itemID, in.Fields)
}

// setGenericFields sets arbitrary board fields by name.
func (c *Client) setGenericFields(ctx context.Context, board *Board, itemID string, fields map[string]string) error {
	for name, value := range fields {
		field := board.fieldByName(name)
		if field == nil {
			return fmt.Errorf("%w: %q", ErrFieldNotFound, name)
		}
		if err := c.setFieldValue(ctx, board.ID, itemID, field, value); err != nil {
			return err
		}
	}
	return nil
}

// setTitle updates the card title (draft issue or real issue).
func (c *Client) setTitle(ctx context.Context, isDraft bool, contentID, title string) error {
	if contentID == "" {
		return ErrNoContent
	}
	if isDraft {
		return c.graphql(ctx, updateDraftTitleMutation, map[string]any{"draft": contentID, "title": title}, nil)
	}
	return c.graphql(ctx, updateIssueTitleMutation, map[string]any{"id": contentID, "title": title}, nil)
}

// setZone sets (or clears, when zone is empty) the board's zone field.
func (c *Client) setZone(ctx context.Context, board *Board, itemID string, zone ZoneKey) error {
	field := board.roles().Zone
	if field == nil {
		return fmt.Errorf("%w: zone", ErrFieldNotFound)
	}
	if zone == "" {
		return c.clearField(ctx, board.ID, itemID, field.ID)
	}
	option := optionForZone(field, zone)
	if option == "" {
		return fmt.Errorf("zone %q has no matching option on field %q", zone, field.Name)
	}
	return c.setSingleSelect(ctx, board.ID, itemID, field.ID, option)
}

// setNumberRole sets a numeric role field.
func (c *Client) setNumberRole(ctx context.Context, board *Board, itemID string, field *ProjectField, role string, v float64) error {
	if field == nil {
		return fmt.Errorf("%w: %s", ErrFieldNotFound, role)
	}
	return c.graphql(ctx, setNumberMutation,
		map[string]any{"project": board.ID, "item": itemID, "field": field.ID, "value": v}, nil)
}

// setDateRole sets (or clears, when value is empty) a date role field.
func (c *Client) setDateRole(ctx context.Context, board *Board, itemID string, field *ProjectField, role, value string) error {
	if field == nil {
		return fmt.Errorf("%w: %s", ErrFieldNotFound, role)
	}
	if value == "" {
		return c.clearField(ctx, board.ID, itemID, field.ID)
	}
	return c.graphql(ctx, setDateMutation,
		map[string]any{"project": board.ID, "item": itemID, "field": field.ID, "value": value}, nil)
}

// setSingleSelectRole sets (or clears) a single-select role field by option name.
func (c *Client) setSingleSelectRole(ctx context.Context, board *Board, itemID string, field *ProjectField, role, name string) error {
	if field == nil {
		return fmt.Errorf("%w: %s", ErrFieldNotFound, role)
	}
	if name == "" {
		return c.clearField(ctx, board.ID, itemID, field.ID)
	}
	option := optionByName(field, name)
	if option == "" {
		return fmt.Errorf("%s has no option %q", role, name)
	}
	return c.setSingleSelect(ctx, board.ID, itemID, field.ID, option)
}

// setRoleByValue sets a role whose field may be single-select, text, date or
// number, dispatching on its data type.
func (c *Client) setRoleByValue(ctx context.Context, board *Board, itemID string, field *ProjectField, role, value string) error {
	if field == nil {
		return fmt.Errorf("%w: %s", ErrFieldNotFound, role)
	}
	return c.setFieldValue(ctx, board.ID, itemID, field, value)
}

// setFieldValue sets any field by inspecting its data type. An empty value
// clears the field.
func (c *Client) setFieldValue(ctx context.Context, projectID, itemID string, field *ProjectField, value string) error {
	if value == "" {
		return c.clearField(ctx, projectID, itemID, field.ID)
	}
	switch strings.ToUpper(field.DataType) {
	case "SINGLE_SELECT":
		option := optionByName(field, value)
		if option == "" {
			return fmt.Errorf("field %q has no option %q", field.Name, value)
		}
		return c.setSingleSelect(ctx, projectID, itemID, field.ID, option)
	case "DATE":
		return c.graphql(ctx, setDateMutation,
			map[string]any{"project": projectID, "item": itemID, "field": field.ID, "value": value}, nil)
	case "NUMBER":
		n, err := parseFloat(value)
		if err != nil {
			return fmt.Errorf("field %q expects a number: %w", field.Name, err)
		}
		return c.graphql(ctx, setNumberMutation,
			map[string]any{"project": projectID, "item": itemID, "field": field.ID, "value": n}, nil)
	case "TEXT", "TITLE":
		return c.graphql(ctx, setTextMutation,
			map[string]any{"project": projectID, "item": itemID, "field": field.ID, "value": value}, nil)
	default:
		return fmt.Errorf("field %q has unsupported data type %q", field.Name, field.DataType)
	}
}

func (c *Client) setSingleSelect(ctx context.Context, projectID, itemID, fieldID, option string) error {
	return c.graphql(ctx, setSingleSelectMutation,
		map[string]any{"project": projectID, "item": itemID, "field": fieldID, "option": option}, nil)
}

func (c *Client) clearField(ctx context.Context, projectID, itemID, fieldID string) error {
	return c.graphql(ctx, clearFieldMutation,
		map[string]any{"project": projectID, "item": itemID, "field": fieldID}, nil)
}

// setAssignee replaces the card's assignee with login (empty clears it).
func (c *Client) setAssignee(ctx context.Context, isDraft bool, contentID string, current []string, login string) error {
	if contentID == "" {
		return ErrNoContent
	}
	var newID string
	if login != "" {
		id, err := c.resolveUserID(ctx, login)
		if err != nil {
			return err
		}
		newID = id
	}
	if isDraft {
		ids := []string{}
		if newID != "" {
			ids = []string{newID}
		}
		return c.graphql(ctx, updateDraftAssigneesMutation,
			map[string]any{"draft": contentID, "assignees": ids}, nil)
	}
	if len(current) > 0 {
		ids := make([]string, 0, len(current))
		for _, login := range current {
			id, err := c.resolveUserID(ctx, login)
			if err != nil {
				return err
			}
			ids = append(ids, id)
		}
		if err := c.graphql(ctx, removeAssigneesMutation,
			map[string]any{"assignable": contentID, "assignees": ids}, nil); err != nil {
			return err
		}
	}
	if newID != "" {
		return c.graphql(ctx, addAssigneesMutation,
			map[string]any{"assignable": contentID, "assignees": []string{newID}}, nil)
	}
	return nil
}

// MoveProjectCard reorders itemID to sit after afterID; a nil afterID moves it
// to the top of the board. The domain-typed move is the MoveCard method in
// setters.go.
func (c *Client) MoveProjectCard(ctx context.Context, board *Board, itemID string, afterID *string) error {
	if board.cardByItemID(itemID) == nil {
		return fmt.Errorf("%w: %s", ErrCardNotFound, itemID)
	}
	vars := map[string]any{"project": board.ID, "item": itemID, "after": nil}
	if afterID != nil && *afterID != "" {
		vars["after"] = *afterID
	}
	return c.graphql(ctx, moveItemMutation, vars, nil)
}

// DeleteProjectCard removes the item from the board. The domain-typed delete is
// the DeleteCard method in setters.go.
func (c *Client) DeleteProjectCard(ctx context.Context, board *Board, itemID string) error {
	return c.graphql(ctx, deleteItemMutation, map[string]any{"project": board.ID, "item": itemID}, nil)
}

// AddProjectNote appends a note to the card: an issue comment, or a dated line
// in the draft body when the card is a draft. The domain-typed note is the
// AddNote method in setters.go.
func (c *Client) AddProjectNote(ctx context.Context, board *Board, itemID, text string) error {
	card := board.cardByItemID(itemID)
	if card == nil {
		return fmt.Errorf("%w: %s", ErrCardNotFound, itemID)
	}
	if card.ContentID == "" {
		return ErrNoContent
	}
	if card.IsDraft {
		var data struct {
			Node *struct {
				Body string `json:"body"`
			} `json:"node"`
		}
		if err := c.graphql(ctx, getDraftBodyQuery, map[string]any{"id": card.ContentID}, &data); err != nil {
			return err
		}
		body := ""
		if data.Node != nil {
			body = data.Node.Body
		}
		line := fmt.Sprintf("- [%s] %s", time.Now().UTC().Format(time.RFC3339), text)
		if body != "" {
			line = body + "\n" + line
		}
		return c.graphql(ctx, updateDraftBodyMutation, map[string]any{"draft": card.ContentID, "body": line}, nil)
	}
	return c.graphql(ctx, addCommentMutation, map[string]any{"subject": card.ContentID, "body": text}, nil)
}
