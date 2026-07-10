package ghprojects

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aenix-org/aeman/pkg/board"
)

// domainFieldCache memoises fields the domain setters lazily create, keyed by
// "<projectID>\x00<role>". A project's fields persist on GitHub, so the cache is
// only needed within a single board operation (where two setters may both need a
// field the board did not have yet) and is safe for a per-request Client.
type domainFieldCache struct {
	mu sync.Mutex
	m  map[string]board.ProjectField
}

func (d *domainFieldCache) load(key string) (board.ProjectField, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	f, ok := d.m[key]
	return f, ok
}

func (d *domainFieldCache) store(key string, f board.ProjectField) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.m == nil {
		d.m = map[string]board.ProjectField{}
	}
	d.m[key] = f
}

// domainSelectOption is one option of a single-select field aeman may create.
type domainSelectOption struct {
	name, color, description string
}

// domainFieldSpec describes how to create a missing role field. It mirrors
// FIELD_SPECS in web/src/providers/github/githubProvider.ts.
type domainFieldSpec struct {
	name     string
	dataType string // for non-select fields: NUMBER | DATE | TEXT
	options  []domainSelectOption
}

// domainFieldSpecs lists the fields aeman lazily creates on a board that lacks
// them. Roles without a spec (e.g. an absent custom field) cannot be created.
var domainFieldSpecs = map[string]domainFieldSpec{
	"zone": {name: "Zone", options: []domainSelectOption{
		{"Critical today", "RED", "Must be resolved before the end of the day"},
		{"Unplanned", "YELLOW", "Popped up unplanned during the day"},
		{"Planned", "GRAY", "Regular, planned work"},
		{"If time left", "GREEN", "Start only when every other zone is clear"},
	}},
	"stage": {name: "Stage", options: []domainSelectOption{
		{"Locked", "RED", "Locked"},
		{"Review", "YELLOW", "Review"},
		{"Recurrent", "BLUE", "Recurrent"},
		{"Done", "GREEN", "Done"},
	}},
	"progress":    {name: "Progress", dataType: "NUMBER"},
	"day":         {name: "Day", dataType: "DATE"},
	"start":       {name: "Start", dataType: "DATE"},
	"sprintStart": {name: "Sprint Start", dataType: "DATE"},
	"plan": {name: "Plan", options: []domainSelectOption{
		{"Wed", "BLUE", "By Wednesday"},
		{"Fri", "PURPLE", "By Friday"},
	}},
	"week":        {name: "Week", dataType: "DATE"},
	"team":        {name: "Team", dataType: "TEXT"},
	"reviewOf":    {name: "Review Of", dataType: "TEXT"},
	"parent":      {name: "Parent", dataType: "TEXT"},
	"reviewRound": {name: "Review Round", dataType: "NUMBER"},
}

// zoneGHColors maps each Ford zone onto the GitHub single-select option colours
// that represent it. It mirrors web/src/zones.ts ghColors.
var zoneGHColors = map[board.ZoneKey][]string{
	board.ZoneGray:   {"GRAY"},
	board.ZoneGreen:  {"GREEN"},
	board.ZoneYellow: {"YELLOW", "ORANGE"},
	board.ZoneRed:    {"RED", "PINK"},
}

// createdFieldRaw is the createProjectV2Field payload aeman reads.
type createdFieldRaw struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	DataType string `json:"dataType"`
	Options  []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Color string `json:"color"`
	} `json:"options"`
}

func (f createdFieldRaw) toDomain() board.ProjectField {
	field := board.ProjectField{ID: f.ID, Name: f.Name, DataType: f.DataType}
	for _, o := range f.Options {
		field.Options = append(field.Options, board.SingleSelectOption{ID: o.ID, Name: o.Name, Color: o.Color})
	}
	return field
}

// ensureDomainField returns the board field filling a role, creating it on the
// project when it does not exist yet. It mirrors githubProvider.ensureField; the
// created field is cached (see domainFieldCache) instead of pushed onto the
// pass-by-value board.
func (c *Client) ensureDomainField(ctx context.Context, b board.Board, role string) (board.ProjectField, error) {
	if f := domainRoles(b.Fields).get(role); f != nil {
		return *f, nil
	}
	key := b.ID + "\x00" + role
	if f, ok := c.fieldCache.load(key); ok {
		return f, nil
	}
	spec, ok := domainFieldSpecs[role]
	if !ok {
		return board.ProjectField{}, fmt.Errorf("%w: %s", ErrFieldNotFound, role)
	}
	// The snapshot's fields may be stale — a cached board loaded before the
	// field was created (by another request or client). Re-check the live
	// project first; creating blindly would fail with "Name has already been
	// taken".
	if fields, err := c.projectFields(ctx, b.ID); err == nil {
		if f := domainRoles(fields).get(role); f != nil {
			c.fieldCache.store(key, *f)
			return *f, nil
		}
	}
	field, err := c.createDomainField(ctx, b.ID, spec)
	if err != nil {
		// A concurrent create may have won the race between our check and the
		// create; resolve to the now-existing field instead of failing.
		if strings.Contains(err.Error(), "already been taken") {
			if fields, ferr := c.projectFields(ctx, b.ID); ferr == nil {
				if f := domainRoles(fields).get(role); f != nil {
					c.fieldCache.store(key, *f)
					return *f, nil
				}
			}
		}
		return board.ProjectField{}, err
	}
	c.fieldCache.store(key, field)
	return field, nil
}

// projectFields fetches the project's current fields, mapped to domain fields.
func (c *Client) projectFields(ctx context.Context, projectID string) ([]board.ProjectField, error) {
	var data struct {
		Node *struct {
			Fields struct {
				Nodes []rawField `json:"nodes"`
			} `json:"fields"`
		} `json:"node"`
	}
	if err := c.graphql(ctx, projectFieldsQuery, map[string]any{"id": projectID}, &data); err != nil {
		return nil, err
	}
	if data.Node == nil {
		return nil, nil
	}
	fields := make([]board.ProjectField, 0, len(data.Node.Fields.Nodes))
	for _, f := range data.Node.Fields.Nodes {
		if f.ID == "" || f.Name == "" {
			continue
		}
		field := board.ProjectField{ID: f.ID, Name: f.Name, DataType: f.DataType}
		for _, o := range f.Options {
			field.Options = append(field.Options, board.SingleSelectOption{ID: o.ID, Name: o.Name, Color: o.Color})
		}
		fields = append(fields, field)
	}
	return fields, nil
}

// createDomainField creates a project field from its spec.
func (c *Client) createDomainField(ctx context.Context, projectID string, spec domainFieldSpec) (board.ProjectField, error) {
	var data struct {
		CreateProjectV2Field struct {
			ProjectV2Field createdFieldRaw `json:"projectV2Field"`
		} `json:"createProjectV2Field"`
	}
	if len(spec.options) > 0 {
		options := make([]map[string]any, len(spec.options))
		for i, o := range spec.options {
			options[i] = map[string]any{"name": o.name, "color": o.color, "description": o.description}
		}
		if err := c.graphql(ctx, createSelectFieldMutation,
			map[string]any{"project": projectID, "name": spec.name, "options": options}, &data); err != nil {
			return board.ProjectField{}, err
		}
		return data.CreateProjectV2Field.ProjectV2Field.toDomain(), nil
	}
	if err := c.graphql(ctx, createFieldMutation,
		map[string]any{"project": projectID, "name": spec.name, "dataType": spec.dataType}, &data); err != nil {
		return board.ProjectField{}, err
	}
	return data.CreateProjectV2Field.ProjectV2Field.toDomain(), nil
}

// domainOptionForZone finds the option id in a single-select field for a zone.
func domainOptionForZone(field board.ProjectField, zone board.ZoneKey) string {
	for _, opt := range field.Options {
		for _, want := range zoneGHColors[zone] {
			if strings.EqualFold(opt.Color, want) {
				return opt.ID
			}
		}
	}
	return ""
}

// domainOptionForStage finds the option id in the Stage field for a stage.
func domainOptionForStage(field board.ProjectField, stage board.StageKey) string {
	for _, opt := range field.Options {
		if board.StageFromName(opt.Name) == stage {
			return opt.ID
		}
	}
	return ""
}

// domainOptionByName finds the option id whose name matches (case-insensitively).
func domainOptionByName(field board.ProjectField, name string) string {
	for _, opt := range field.Options {
		if strings.EqualFold(strings.TrimSpace(opt.Name), strings.TrimSpace(name)) {
			return opt.ID
		}
	}
	return ""
}

// SetZone sets (or clears, when zone is empty) the board's zone field.
func (c *Client) SetZone(ctx context.Context, b board.Board, card board.Card, zone board.ZoneKey) error {
	field, err := c.ensureDomainField(ctx, b, "zone")
	if err != nil {
		return err
	}
	if zone == "" {
		return c.clearField(ctx, b.ID, card.ItemID, field.ID)
	}
	option := domainOptionForZone(field, zone)
	if option == "" {
		return fmt.Errorf("zone %q has no matching option on field %q", zone, field.Name)
	}
	return c.setSingleSelect(ctx, b.ID, card.ItemID, field.ID, option)
}

// SetStage sets (or clears, when stage is empty) the board's Stage field. The
// progress side of the stage transition is owned by the board service.
func (c *Client) SetStage(ctx context.Context, b board.Board, card board.Card, stage board.StageKey) error {
	field, err := c.ensureDomainField(ctx, b, "stage")
	if err != nil {
		return err
	}
	if stage == "" {
		return c.clearField(ctx, b.ID, card.ItemID, field.ID)
	}
	option := domainOptionForStage(field, stage)
	if option == "" {
		// A Stage field provisioned before this stage existed (e.g. Recurrent)
		// lacks its option: add it to the field in place, then retry.
		field, err = c.ensureStageOption(ctx, b, field, stage)
		if err != nil {
			return err
		}
		option = domainOptionForStage(field, stage)
	}
	if option == "" {
		return fmt.Errorf("stage %q has no matching option on field %q", stage, field.Name)
	}
	return c.setSingleSelect(ctx, b.ID, card.ItemID, field.ID, option)
}

// ensureStageOption adds a missing stage option to an existing Stage field.
// The GitHub API replaces the whole option list on update and RECREATES every
// option with a new id — even for unchanged names — which wipes the stage
// values stored on cards. So the current values are snapshotted from the board
// first and re-applied against the new option ids right after the update.
func (c *Client) ensureStageOption(ctx context.Context, b board.Board, field board.ProjectField, stage board.StageKey) (board.ProjectField, error) {
	var spec *domainSelectOption
	for i := range domainFieldSpecs["stage"].options {
		o := &domainFieldSpecs["stage"].options[i]
		if board.StageFromName(o.name) == stage {
			spec = o
			break
		}
	}
	if spec == nil {
		return field, fmt.Errorf("stage %q has no option spec", stage)
	}
	// Fetch the live options with their descriptions — the update input requires
	// a description per option, and wiping existing ones would lose data.
	var cur struct {
		Node *struct {
			Options []struct {
				Name        string `json:"name"`
				Color       string `json:"color"`
				Description string `json:"description"`
			} `json:"options"`
		} `json:"node"`
	}
	if err := c.graphql(ctx, selectFieldOptionsQuery, map[string]any{"id": field.ID}, &cur); err != nil {
		return field, err
	}
	options := []map[string]any{}
	if cur.Node != nil {
		for _, o := range cur.Node.Options {
			options = append(options, map[string]any{"name": o.Name, "color": o.Color, "description": o.Description})
		}
	}
	options = append(options, map[string]any{"name": spec.name, "color": spec.color, "description": spec.description})
	// Snapshot every card's current stage before the update wipes the values.
	staged := map[string]board.StageKey{}
	for _, card := range b.Cards {
		if card.Stage != board.StageNone {
			staged[card.ItemID] = card.Stage
		}
	}
	var data struct {
		UpdateProjectV2Field struct {
			ProjectV2Field createdFieldRaw `json:"projectV2Field"`
		} `json:"updateProjectV2Field"`
	}
	if err := c.graphql(ctx, updateSelectFieldOptionsMutation,
		map[string]any{"field": field.ID, "options": options}, &data); err != nil {
		return field, err
	}
	updated := data.UpdateProjectV2Field.ProjectV2Field.toDomain()
	c.fieldCache.store(b.ID+"\x00stage", updated)
	// Re-apply the wiped values against the new option ids.
	var restoreErr error
	restored := 0
	for itemID, s := range staged {
		opt := domainOptionForStage(updated, s)
		if opt == "" {
			continue
		}
		if err := c.setSingleSelect(ctx, b.ID, itemID, updated.ID, opt); err != nil {
			if restoreErr == nil {
				restoreErr = err
			}
			continue
		}
		restored++
	}
	if restoreErr != nil {
		return updated, fmt.Errorf("stage option %q added, but only %d of %d stage values were restored: %w",
			spec.name, restored, len(staged), restoreErr)
	}
	return updated, nil
}

// SetProgress sets the readiness percentage.
func (c *Client) SetProgress(ctx context.Context, b board.Board, card board.Card, progress int) error {
	field, err := c.ensureDomainField(ctx, b, "progress")
	if err != nil {
		return err
	}
	return c.graphql(ctx, setNumberMutation,
		map[string]any{"project": b.ID, "item": card.ItemID, "field": field.ID, "value": float64(progress)}, nil)
}

// SetReviewRound records a review card's review-round counter.
func (c *Client) SetReviewRound(ctx context.Context, b board.Board, card board.Card, round int) error {
	field, err := c.ensureDomainField(ctx, b, "reviewRound")
	if err != nil {
		return err
	}
	return c.graphql(ctx, setNumberMutation,
		map[string]any{"project": b.ID, "item": card.ItemID, "field": field.ID, "value": float64(round)}, nil)
}

// SetPlan sets (or clears) the weekly-plan band.
func (c *Client) SetPlan(ctx context.Context, b board.Board, card board.Card, plan board.PlanBand) error {
	field, err := c.ensureDomainField(ctx, b, "plan")
	if err != nil {
		return err
	}
	if plan == "" {
		return c.clearField(ctx, b.ID, card.ItemID, field.ID)
	}
	option := domainOptionByName(field, string(plan))
	if option == "" {
		return fmt.Errorf("plan field %q has no %q option", field.Name, plan)
	}
	return c.setSingleSelect(ctx, b.ID, card.ItemID, field.ID, option)
}

// SetDay sets (or clears) the card's Day field.
func (c *Client) SetDay(ctx context.Context, b board.Board, card board.Card, day string) error {
	return c.setDomainDate(ctx, b, card, "day", day)
}

// SetStart sets (or clears) the card's Start field.
func (c *Client) SetStart(ctx context.Context, b board.Board, card board.Card, date string) error {
	return c.setDomainDate(ctx, b, card, "start", date)
}

// SetSprintStart sets (or clears) the card's Sprint Start field.
func (c *Client) SetSprintStart(ctx context.Context, b board.Board, card board.Card, date string) error {
	return c.setDomainDate(ctx, b, card, "sprintStart", date)
}

// SetWeek sets (or clears) the card's Week field.
func (c *Client) SetWeek(ctx context.Context, b board.Board, card board.Card, week string) error {
	return c.setDomainDate(ctx, b, card, "week", week)
}

// setDomainDate sets (or clears, when value is empty) a date role field.
func (c *Client) setDomainDate(ctx context.Context, b board.Board, card board.Card, role, value string) error {
	field, err := c.ensureDomainField(ctx, b, role)
	if err != nil {
		return err
	}
	if value == "" {
		return c.clearField(ctx, b.ID, card.ItemID, field.ID)
	}
	return c.graphql(ctx, setDateMutation,
		map[string]any{"project": b.ID, "item": card.ItemID, "field": field.ID, "value": value}, nil)
}

// SetTeam sets (or clears) the card's team label.
func (c *Client) SetTeam(ctx context.Context, b board.Board, card board.Card, team string) error {
	return c.setDomainText(ctx, b, card, "team", team)
}

// SetReviewOf sets (or clears) the review-of link on a review card.
func (c *Client) SetReviewOf(ctx context.Context, b board.Board, card board.Card, reviewOf string) error {
	return c.setDomainText(ctx, b, card, "reviewOf", reviewOf)
}

// setDomainText sets (or clears, when value is empty) a text role field.
func (c *Client) setDomainText(ctx context.Context, b board.Board, card board.Card, role, value string) error {
	field, err := c.ensureDomainField(ctx, b, role)
	if err != nil {
		return err
	}
	if value == "" {
		return c.clearField(ctx, b.ID, card.ItemID, field.ID)
	}
	return c.graphql(ctx, setTextMutation,
		map[string]any{"project": b.ID, "item": card.ItemID, "field": field.ID, "value": value}, nil)
}

// SetParent sets (or clears) the parent link making a card a subtask.
func (c *Client) SetParent(ctx context.Context, b board.Board, card board.Card, parent string) error {
	return c.setDomainText(ctx, b, card, "parent", parent)
}

// SetAssignee replaces the card's assignee with login (empty clears it).
func (c *Client) SetAssignee(ctx context.Context, _ board.Board, card board.Card, login string) error {
	return c.setAssignee(ctx, card.IsDraft, card.ContentID, card.Assignees, login)
}

// RenameCard updates the card's title.
func (c *Client) RenameCard(ctx context.Context, _ board.Board, card board.Card, title string) error {
	return c.setTitle(ctx, card.IsDraft, card.ContentID, title)
}

// MoveCard reorders card to sit after afterID; an empty afterID moves it to the
// top of the board.
func (c *Client) MoveCard(ctx context.Context, b board.Board, card board.Card, afterID string) error {
	vars := map[string]any{"project": b.ID, "item": card.ItemID, "after": nil}
	if afterID != "" {
		vars["after"] = afterID
	}
	return c.graphql(ctx, moveItemMutation, vars, nil)
}

// DeleteCard removes the card from the board.
func (c *Client) DeleteCard(ctx context.Context, b board.Board, card board.Card) error {
	return c.graphql(ctx, deleteItemMutation, map[string]any{"project": b.ID, "item": card.ItemID}, nil)
}

// AddNote appends a note to the card: an issue comment, or a dated line in the
// draft body when the card is a draft. It mirrors the rich AddProjectNote.
func (c *Client) AddNote(ctx context.Context, _ board.Board, card board.Card, text string) error {
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
		line := fmt.Sprintf("- [%s] %s", time.Now().UTC().Format(time.RFC3339),
			board.RenderNoteBody(board.ActorFrom(ctx), text))
		if body != "" {
			line = body + "\n" + line
		}
		return c.graphql(ctx, updateDraftBodyMutation, map[string]any{"draft": card.ContentID, "body": line}, nil)
	}
	return c.graphql(ctx, addCommentMutation, map[string]any{"subject": card.ContentID, "body": text}, nil)
}

// CreateCard creates a draft-issue card and applies the optional fields,
// returning an optimistic domain card. It mirrors githubProvider.createCard.
func (c *Client) CreateCard(ctx context.Context, b board.Board, in board.CreateInput) (board.Card, error) {
	var assigneeIDs []string
	if in.Assignee != "" {
		id, err := c.resolveUserID(ctx, in.Assignee)
		if err != nil {
			return board.Card{}, err
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
	if err := c.graphql(ctx, addDraftMutation,
		map[string]any{"project": b.ID, "title": in.Title, "assignees": assigneeIDs}, &created); err != nil {
		return board.Card{}, err
	}
	item := created.AddProjectV2DraftIssue.ProjectItem
	contentID := ""
	if item.Content != nil {
		contentID = item.Content.ID
	}

	if in.Zone != "" {
		field, err := c.ensureDomainField(ctx, b, "zone")
		if err != nil {
			return board.Card{}, err
		}
		if option := domainOptionForZone(field, in.Zone); option != "" {
			if err := c.setSingleSelect(ctx, b.ID, item.ID, field.ID, option); err != nil {
				return board.Card{}, err
			}
		}
	}
	for _, df := range []struct{ role, value string }{
		{"day", in.Day}, {"start", in.Start}, {"sprintStart", in.SprintStart}, {"week", in.Week},
	} {
		if df.value == "" {
			continue
		}
		field, err := c.ensureDomainField(ctx, b, df.role)
		if err != nil {
			return board.Card{}, err
		}
		if err := c.graphql(ctx, setDateMutation,
			map[string]any{"project": b.ID, "item": item.ID, "field": field.ID, "value": df.value}, nil); err != nil {
			return board.Card{}, err
		}
	}
	for _, tf := range []struct{ role, value string }{
		{"team", in.Team}, {"reviewOf", in.ReviewOf},
	} {
		if tf.value == "" {
			continue
		}
		field, err := c.ensureDomainField(ctx, b, tf.role)
		if err != nil {
			return board.Card{}, err
		}
		if err := c.graphql(ctx, setTextMutation,
			map[string]any{"project": b.ID, "item": item.ID, "field": field.ID, "value": tf.value}, nil); err != nil {
			return board.Card{}, err
		}
	}
	if in.Plan != "" {
		field, err := c.ensureDomainField(ctx, b, "plan")
		if err != nil {
			return board.Card{}, err
		}
		if option := domainOptionByName(field, string(in.Plan)); option != "" {
			if err := c.setSingleSelect(ctx, b.ID, item.ID, field.ID, option); err != nil {
				return board.Card{}, err
			}
		}
	}

	assignees := []string{}
	if in.Assignee != "" {
		assignees = []string{in.Assignee}
	}
	return board.Card{
		ItemID:      item.ID,
		ContentID:   contentID,
		Title:       in.Title,
		IsDraft:     true,
		Assignees:   assignees,
		Zone:        in.Zone,
		StartDate:   in.Start,
		SprintStart: in.SprintStart,
		Plan:        in.Plan,
		Week:        in.Week,
		Team:        in.Team,
		ReviewOf:    in.ReviewOf,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// SetSprintState creates or updates a team's hidden sprint-state card: Sprint
// Start holds the current sprint, Start holds the previous one. team = "" is the
// no-team group (its state card carries no team field). It mirrors
// githubProvider.setSprintState.
func (c *Client) SetSprintState(ctx context.Context, b board.Board, team, current, previous string) error {
	itemID := b.SprintStates[team].ItemID
	if itemID == "" {
		var created struct {
			AddProjectV2DraftIssue struct {
				ProjectItem struct {
					ID string `json:"id"`
				} `json:"projectItem"`
			} `json:"addProjectV2DraftIssue"`
		}
		if err := c.graphql(ctx, addDraftMutation,
			map[string]any{"project": b.ID, "title": board.SprintStateTitle, "assignees": []string{}}, &created); err != nil {
			return err
		}
		itemID = created.AddProjectV2DraftIssue.ProjectItem.ID
		if team != "" {
			teamField, err := c.ensureDomainField(ctx, b, "team")
			if err != nil {
				return err
			}
			if err := c.graphql(ctx, setTextMutation,
				map[string]any{"project": b.ID, "item": itemID, "field": teamField.ID, "value": team}, nil); err != nil {
				return err
			}
		}
	}
	if err := c.setSprintStateDate(ctx, b, itemID, "sprintStart", current); err != nil {
		return err
	}
	return c.setSprintStateDate(ctx, b, itemID, "start", previous)
}

// setSprintStateDate sets (or clears, when value is empty) a date field on the
// sprint-state card identified by itemID.
func (c *Client) setSprintStateDate(ctx context.Context, b board.Board, itemID, role, value string) error {
	field, err := c.ensureDomainField(ctx, b, role)
	if err != nil {
		return err
	}
	if value == "" {
		return c.clearField(ctx, b.ID, itemID, field.ID)
	}
	return c.graphql(ctx, setDateMutation,
		map[string]any{"project": b.ID, "item": itemID, "field": field.ID, "value": value}, nil)
}
