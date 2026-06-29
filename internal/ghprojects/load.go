package ghprojects

import (
	"context"
	"strings"

	"github.com/aenix-org/aeman/internal/board"
)

// LoadBoard loads a project board and maps it into the domain board.Board the
// board service operates on (fields + cards, with the hidden sprint-state cards
// split out by board.NewBoard). It mirrors githubProvider.mapProject. The rich
// ghprojects.Board (notes, URLs, content ids) is loaded by LoadProjectBoard.
func (c *Client) LoadBoard(ctx context.Context, owner string, project int) (board.Board, error) {
	raw, err := c.loadProject(ctx, owner, project)
	if err != nil {
		return board.Board{}, err
	}
	return mapDomainBoard(owner, raw), nil
}

// mapDomainBoard maps a raw project onto the domain board.Board, calling
// board.NewBoard so the per-team sprint-state cards are split out of Cards.
func mapDomainBoard(owner string, raw *rawProject) board.Board {
	fields := make([]board.ProjectField, 0, len(raw.Fields.Nodes))
	for _, f := range raw.Fields.Nodes {
		if f.ID == "" || f.Name == "" {
			continue
		}
		field := board.ProjectField{ID: f.ID, Name: f.Name, DataType: f.DataType}
		for _, o := range f.Options {
			field.Options = append(field.Options, board.SingleSelectOption{ID: o.ID, Name: o.Name, Color: o.Color})
		}
		fields = append(fields, field)
	}
	roles := domainRoles(fields)
	cards := make([]board.Card, 0, len(raw.Items.Nodes))
	for i := range raw.Items.Nodes {
		cards = append(cards, mapDomainItem(&raw.Items.Nodes[i], roles))
	}
	b := board.NewBoard(fields, cards)
	b.ID = raw.ID
	b.Number = raw.Number
	b.Owner = owner
	return b
}

// mapDomainItem maps one raw item onto a domain board.Card, reading the typed
// roles aeman orients by. It mirrors githubProvider.mapItem (minus the
// description/notes the pure card does not carry). The Day field has no place on
// board.Card and is intentionally dropped.
func mapDomainItem(item *rawItem, roles domainFieldRoles) board.Card {
	content := item.Content
	isDraft := item.Type == "DRAFT_ISSUE" || (content != nil && content.Typename == "DraftIssue")
	card := board.Card{
		ItemID:    item.ID,
		Title:     "(untitled)",
		IsDraft:   isDraft,
		CreatedAt: item.CreatedAt,
		Assignees: []string{},
	}
	if content != nil {
		card.ContentID = content.ID
		if content.Title != "" {
			card.Title = content.Title
		}
		if content.Assignees != nil {
			for _, a := range content.Assignees.Nodes {
				card.Assignees = append(card.Assignees, a.Login)
			}
		}
	}
	for i := range item.FieldValues.Nodes {
		applyDomainRole(&card, &item.FieldValues.Nodes[i], roles)
	}
	return card
}

// applyDomainRole records a single raw field value under the matching typed role,
// mirroring the if/else-if chain in githubProvider.mapItem.
func applyDomainRole(card *board.Card, v *rawFieldValue, roles domainFieldRoles) {
	if v.Field == nil || v.Field.ID == "" {
		return
	}
	id := v.Field.ID
	switch {
	case roles.Zone != nil && id == roles.Zone.ID && v.OptionID != "":
		for _, o := range roles.Zone.Options {
			if o.ID == v.OptionID {
				card.Zone = board.ZoneKey(zoneFromColor(o.Color))
			}
		}
	case roles.Stage != nil && id == roles.Stage.ID && v.Name != "":
		card.Stage = board.StageFromName(v.Name)
	case roles.Progress != nil && id == roles.Progress.ID && v.Number != nil:
		card.Progress = int(*v.Number)
	case roles.Start != nil && id == roles.Start.ID && v.Date != "":
		card.StartDate = v.Date
	case roles.SprintStart != nil && id == roles.SprintStart.ID && v.Date != "":
		card.SprintStart = v.Date
	case roles.Plan != nil && id == roles.Plan.ID && v.Name != "":
		if strings.EqualFold(v.Name, "fri") {
			card.Plan = board.PlanFri
		} else {
			card.Plan = board.PlanWed
		}
	case roles.Week != nil && id == roles.Week.ID && v.Date != "":
		card.Week = v.Date
	case roles.Team != nil && id == roles.Team.ID && v.Text != "":
		card.Team = v.Text
	case roles.ReviewOf != nil && id == roles.ReviewOf.ID && v.Text != "":
		card.ReviewOf = v.Text
	}
}

// domainFieldRoles resolves the board's fields onto the well-known roles the
// board service reads and writes. It mirrors FieldRoles in
// web/src/providers/types.ts (the subset aeman acts on).
type domainFieldRoles struct {
	Zone        *board.ProjectField
	Progress    *board.ProjectField
	Day         *board.ProjectField
	Start       *board.ProjectField
	SprintStart *board.ProjectField
	Plan        *board.ProjectField
	Week        *board.ProjectField
	Stage       *board.ProjectField
	Team        *board.ProjectField
	ReviewOf    *board.ProjectField
}

// domainRoleAliases maps each role to the field names (case-insensitive) that
// fill it. It mirrors ALIASES in web/src/providers/fields.ts (Stage is its own
// role here, distinct from the default Status field).
var domainRoleAliases = map[string][]string{
	"zone":        {"zone", "priority zone", "зона"},
	"progress":    {"progress", "readiness", "% done", "percent", "готовность"},
	"day":         {"day", "date", "due date", "due", "finish", "finish date", "день", "дата"},
	"start":       {"start", "start date", "начало", "старт"},
	"sprintStart": {"sprint start", "sprintstart", "спринт старт"},
	"plan":        {"plan", "план"},
	"week":        {"week", "неделя"},
	"stage":       {"stage", "состояние"},
	"team":        {"team", "команда"},
	"reviewOf":    {"review of", "reviewof"},
}

// domainRoles maps the board's fields onto the typed roles by name.
func domainRoles(fields []board.ProjectField) domainFieldRoles {
	var r domainFieldRoles
	for i := range fields {
		f := &fields[i]
		name := strings.ToLower(strings.TrimSpace(f.Name))
		switch {
		case r.Zone == nil && domainMatchesAlias("zone", name):
			r.Zone = f
		case r.Progress == nil && domainMatchesAlias("progress", name):
			r.Progress = f
		case r.Day == nil && domainMatchesAlias("day", name):
			r.Day = f
		case r.Start == nil && domainMatchesAlias("start", name):
			r.Start = f
		case r.SprintStart == nil && domainMatchesAlias("sprintStart", name):
			r.SprintStart = f
		case r.Plan == nil && domainMatchesAlias("plan", name):
			r.Plan = f
		case r.Week == nil && domainMatchesAlias("week", name):
			r.Week = f
		case r.Stage == nil && domainMatchesAlias("stage", name):
			r.Stage = f
		case r.Team == nil && domainMatchesAlias("team", name):
			r.Team = f
		case r.ReviewOf == nil && domainMatchesAlias("reviewOf", name):
			r.ReviewOf = f
		}
	}
	return r
}

// get returns the field resolved for a role name, or nil.
func (r domainFieldRoles) get(role string) *board.ProjectField {
	switch role {
	case "zone":
		return r.Zone
	case "progress":
		return r.Progress
	case "day":
		return r.Day
	case "start":
		return r.Start
	case "sprintStart":
		return r.SprintStart
	case "plan":
		return r.Plan
	case "week":
		return r.Week
	case "stage":
		return r.Stage
	case "team":
		return r.Team
	case "reviewOf":
		return r.ReviewOf
	default:
		return nil
	}
}

// domainMatchesAlias reports whether the lower-cased field name fills the role.
func domainMatchesAlias(role, lowerName string) bool {
	for _, alias := range domainRoleAliases[role] {
		if alias == lowerName {
			return true
		}
	}
	return false
}
