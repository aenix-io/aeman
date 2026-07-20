package ghprojects

import (
	"context"
	"fmt"
	"strings"

	"github.com/aenix-io/aeman/pkg/board"
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

// nodesByIDResult decodes the nodes(ids:) query: a positional list where a
// missing/deleted id (or a non-ProjectV2Item node) comes back null.
type nodesByIDResult struct {
	Nodes []*rawItem `json:"nodes"`
}

// LoadCards fetches specific project items by node id and maps them to domain
// cards, resolving roles from b's fields. Deleted ids are simply absent from the
// result. It is the fast partial read the live-update path uses instead of
// reloading the whole board.
func (c *Client) LoadCards(ctx context.Context, b board.Board, ids []string) ([]board.Card, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var data nodesByIDResult
	if err := c.graphql(ctx, cardsByIDQuery, map[string]any{"ids": ids}, &data); err != nil {
		return nil, err
	}
	roles := domainRoles(b.Fields)
	cards := make([]board.Card, 0, len(data.Nodes))
	for _, item := range data.Nodes {
		if item == nil || item.ID == "" {
			continue // a deleted id, or a non-ProjectV2Item node
		}
		cards = append(cards, mapDomainItem(item, roles))
	}
	return cards, nil
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
	b.Title = raw.Title
	b.URL = raw.URL
	b.Owner = owner
	return b
}

// mapDomainItem maps one raw item onto a domain board.Card, reading the typed
// roles aeman orients by plus the frontend-facing content fields (url, number,
// repository, state, author, description and notes). It mirrors
// githubProvider.mapItem.
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
		card.URL = content.URL
		card.Number = content.Number
		card.State = content.State
		if content.Repository != nil {
			card.Repository = content.Repository.NameWithOwner
		}
		if isDraft {
			if content.Creator != nil {
				card.Author = content.Creator.Login
			}
		} else if content.Author != nil {
			card.Author = content.Author.Login
		}
		if content.Assignees != nil {
			for _, a := range content.Assignees.Nodes {
				card.Assignees = append(card.Assignees, a.Login)
			}
		}
		if isDraft {
			card.Description, card.Notes = domainParseDraftBody(content.Body, item.ID)
			card.Notes, card.Events = board.PartitionEvents(card.Notes)
		} else {
			card.Description = content.Body
			card.Notes = domainCommentNotes(content)
			card.Notes, card.Events, card.EventLogID = domainSplitLogComments(content, card.Notes)
		}
	}
	for i := range item.FieldValues.Nodes {
		applyDomainRole(&card, &item.FieldValues.Nodes[i], roles)
	}
	return card
}

// domainLogMarker separates a draft card's description from its appended action
// log. It mirrors LOG_MARKER in web/src/providers/github/githubProvider.ts.
const domainLogMarker = "<!-- aeman:log -->"

// domainParseDraftBody splits a draft body into a description and its action log,
// mirroring parseDraftBody in the frontend githubProvider: with a log marker the
// description is the text before it; a legacy body without a marker keeps its
// note-shaped lines as the log and the rest as the description.
func domainParseDraftBody(body, itemID string) (string, []board.Note) {
	if body == "" {
		return "", nil
	}
	if idx := strings.Index(body, domainLogMarker); idx >= 0 {
		notes := domainParseNoteLines(body[idx+len(domainLogMarker):], itemID)
		// Machine event lines stranded ABOVE the marker (left there by an old
		// log migration, or baked in by a description write before writes were
		// sanitized) belong to the log, not the description. Only exact event
		// lines move — a prose line that merely looks dated (a checklist item,
		// say) stays where the person wrote it.
		var descLines []string
		for i, line := range strings.Split(body[:idx], "\n") {
			m := draftNoteRe.FindStringSubmatch(strings.TrimSpace(line))
			if m != nil && strings.HasPrefix(m[2], ":: ") {
				notes = append(notes, board.Note{
					ID:        fmt.Sprintf("%s:h%d", itemID, i),
					Body:      m[2],
					CreatedAt: m[1],
					Source:    "draft",
				})
				continue
			}
			descLines = append(descLines, line)
		}
		return strings.TrimSpace(strings.Join(descLines, "\n")), notes
	}
	// Legacy bodies without a marker: treat note-shaped lines as the log.
	var descLines []string
	var notes []board.Note
	for i, line := range strings.Split(body, "\n") {
		if m := draftNoteRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			author, body := board.SplitNoteAuthor(m[2])
			notes = append(notes, board.Note{
				ID:        fmt.Sprintf("%s:%d", itemID, i),
				Body:      body,
				CreatedAt: m[1],
				Author:    author,
				Source:    "draft",
			})
		} else {
			descLines = append(descLines, line)
		}
	}
	return strings.TrimSpace(strings.Join(descLines, "\n")), notes
}

// domainParseNoteLines extracts "- [timestamp] text" draft-log entries from
// text. A note may span multiple lines: everything after its header line and
// before the next header belongs to its body (agents routinely file whole
// review checklists as one note), so continuation lines are accumulated
// instead of dropped. Note ids stay anchored to the header line's index.
func domainParseNoteLines(text, itemID string) []board.Note {
	var notes []board.Note
	for i, line := range strings.Split(text, "\n") {
		if m := draftNoteRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			author, body := board.SplitNoteAuthor(m[2])
			notes = append(notes, board.Note{
				ID:        fmt.Sprintf("%s:%d", itemID, i),
				Body:      body,
				CreatedAt: m[1],
				Author:    author,
				Source:    "draft",
			})
			continue
		}
		if len(notes) == 0 {
			continue
		}
		notes[len(notes)-1].Body += "\n" + line
	}
	for i := range notes {
		notes[i].Body = strings.TrimSpace(notes[i].Body)
	}
	return notes
}

// domainCommentNotes maps an issue/PR's comment thread onto domain notes,
// mirroring commentsToNotes in the frontend githubProvider.
func domainCommentNotes(content *rawContent) []board.Note {
	if content.Comments == nil {
		return nil
	}
	notes := make([]board.Note, 0, len(content.Comments.Nodes))
	for _, cm := range content.Comments.Nodes {
		n := board.Note{ID: cm.ID, Body: cm.Body, CreatedAt: cm.CreatedAt, Source: "comment"}
		if cm.Author != nil {
			n.Author = cm.Author.Login
		}
		notes = append(notes, n)
	}
	return notes
}

// domainSplitLogComments extracts events from an issue/PR card's dedicated log
// comments — comments whose body starts with the log marker hold event lines,
// not conversation — returning the remaining notes, the parsed events and the
// first log comment's id (the one AppendEvent keeps appending to).
func domainSplitLogComments(content *rawContent, notes []board.Note) ([]board.Note, []board.Event, string) {
	if content.Comments == nil {
		return notes, nil, ""
	}
	isLog := map[string]bool{}
	logID := ""
	var events []board.Event
	for _, cm := range content.Comments.Nodes {
		if !strings.HasPrefix(strings.TrimSpace(cm.Body), domainLogMarker) {
			continue
		}
		isLog[cm.ID] = true
		if logID == "" {
			logID = cm.ID
		}
		rest := strings.TrimSpace(cm.Body)[len(domainLogMarker):]
		_, evs := board.PartitionEvents(domainParseNoteLines(rest, cm.ID))
		events = append(events, evs...)
	}
	if len(isLog) == 0 {
		return notes, nil, ""
	}
	keep := notes[:0]
	for _, n := range notes {
		if !isLog[n.ID] {
			keep = append(keep, n)
		}
	}
	return keep, events, logID
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
		card.ZoneOptionID = v.OptionID
		for _, o := range roles.Zone.Options {
			if o.ID == v.OptionID {
				card.Zone = board.ZoneKey(zoneFromColor(o.Color))
			}
		}
	case roles.Stage != nil && id == roles.Stage.ID && v.Name != "":
		card.Stage = board.StageFromName(v.Name)
	case roles.Progress != nil && id == roles.Progress.ID && v.Number != nil:
		card.Progress = int(*v.Number)
	case roles.Day != nil && id == roles.Day.ID && v.Date != "":
		card.Day = v.Date
	case roles.Start != nil && id == roles.Start.ID && v.Date != "":
		card.StartDate = v.Date
	case roles.SprintStart != nil && id == roles.SprintStart.ID && v.Date != "":
		card.SprintStart = v.Date
	case roles.Sprint != nil && id == roles.Sprint.ID && (v.Title != "" || v.Name != ""):
		if v.Title != "" {
			card.SprintTitle = v.Title
		} else {
			card.SprintTitle = v.Name
		}
	case roles.Status != nil && id == roles.Status.ID && v.Name != "":
		card.Status = v.Name
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
	case roles.Parent != nil && id == roles.Parent.ID && v.Text != "":
		card.Parent = v.Text
	case roles.ReviewRound != nil && id == roles.ReviewRound.ID && v.Number != nil:
		card.ReviewRound = int(*v.Number)
	case roles.Recurrence != nil && id == roles.Recurrence.ID && v.Text != "":
		card.Recurrence = v.Text
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
	Sprint      *board.ProjectField
	Status      *board.ProjectField
	Plan        *board.ProjectField
	Week        *board.ProjectField
	Stage       *board.ProjectField
	Team        *board.ProjectField
	ReviewOf    *board.ProjectField
	Parent      *board.ProjectField
	ReviewRound *board.ProjectField
	Recurrence  *board.ProjectField
}

// domainRoleAliases maps each role to the field names (case-insensitive) that
// fill it. It mirrors ALIASES in web/src/providers/fields.ts (Stage is its own
// role here, distinct from the default Status field).
var domainRoleAliases = map[string][]string{
	"zone":        {"zone", "priority zone"},
	"progress":    {"progress", "readiness", "% done", "percent"},
	"day":         {"day", "date", "due date", "due", "finish", "finish date"},
	"start":       {"start", "start date"},
	"sprintStart": {"sprint start", "sprintstart"},
	"sprint":      {"sprint", "iteration"},
	"status":      {"status"},
	"plan":        {"plan"},
	"week":        {"week"},
	"stage":       {"stage"},
	"team":        {"team"},
	"reviewOf":    {"review of", "reviewof"},
	"parent":      {"parent"},
	"reviewRound": {"review round", "reviewround"},
	"recurrence":  {"recurrence", "recur"},
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
		case r.Sprint == nil && domainMatchesAlias("sprint", name):
			r.Sprint = f
		case r.Status == nil && domainMatchesAlias("status", name):
			r.Status = f
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
		case r.Parent == nil && domainMatchesAlias("parent", name):
			r.Parent = f
		case r.ReviewRound == nil && domainMatchesAlias("reviewRound", name):
			r.ReviewRound = f
		case r.Recurrence == nil && domainMatchesAlias("recurrence", name):
			r.Recurrence = f
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
	case "sprint":
		return r.Sprint
	case "status":
		return r.Status
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
	case "parent":
		return r.Parent
	case "reviewRound":
		return r.ReviewRound
	case "recurrence":
		return r.Recurrence
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
