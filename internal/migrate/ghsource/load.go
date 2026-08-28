package ghsource

import (
	"context"
	"fmt"
	"strings"

	"github.com/aenix-io/aeman/pkg/board"
)

// Export is what the reader hands the migration: the domain board, plus what
// Projects v2 carried per item that the domain card no longer does.
type Export struct {
	Board board.Board
	// Items is keyed by item id and covers every card NewBoard sorted into
	// Board.Cards, Board.Tasks or the roster.
	Items map[string]Item
}

// Item is the GitHub side of one project item.
type Item struct {
	// IsDraft marks a draft-issue card — the kind the boards were made of.
	IsDraft bool
	// URL is the underlying issue or PR (empty on a draft); it becomes the
	// card's link.
	URL string
	// Events is the action log parsed out of the draft body (or the log
	// comment on an issue card), in source order.
	Events []board.Event
}

// Field is a Projects v2 field definition — what the roles are resolved from.
type Field struct {
	ID       string
	Name     string
	DataType string
	Options  []FieldOption
}

// FieldOption is one single-select choice; a zone is read by its colour.
type FieldOption struct {
	ID    string
	Name  string
	Color string
}

// LoadBoard loads a project board and maps it into the domain board.Board the
// migration consumes (with the hidden sprint-state cards split out by
// board.NewBoard), alongside the per-item GitHub data.
func (c *Client) LoadBoard(ctx context.Context, owner string, project int) (Export, error) {
	raw, err := c.loadProject(ctx, owner, project)
	if err != nil {
		return Export{}, err
	}
	return mapDomainBoard(owner, raw), nil
}

// mapDomainBoard maps a raw project onto the domain board.Board, calling
// board.NewBoard so the per-team sprint-state cards are split out of Cards.
func mapDomainBoard(owner string, raw *rawProject) Export {
	fields := make([]Field, 0, len(raw.Fields.Nodes))
	for _, f := range raw.Fields.Nodes {
		if f.ID == "" || f.Name == "" {
			continue
		}
		field := Field{ID: f.ID, Name: f.Name, DataType: f.DataType}
		for _, o := range f.Options {
			field.Options = append(field.Options, FieldOption{ID: o.ID, Name: o.Name, Color: o.Color})
		}
		fields = append(fields, field)
	}
	roles := domainRoles(fields)
	cards := make([]board.Card, 0, len(raw.Items.Nodes))
	items := make(map[string]Item, len(raw.Items.Nodes))
	for i := range raw.Items.Nodes {
		card, item := mapDomainItem(&raw.Items.Nodes[i], roles)
		cards = append(cards, card)
		items[card.ItemID] = item
	}
	b := board.NewBoard(cards)
	b.Board = fmt.Sprintf("%s/%d", owner, raw.Number) // provenance: the Projects v2 board this came from
	b.Title = raw.Title
	b.URL = raw.URL
	return Export{Board: b, Items: items}
}

// mapDomainItem maps one raw item onto a domain board.Card, reading the typed
// roles aeman orients by plus the content fields (author, description and
// notes), and returns the GitHub side apart.
func mapDomainItem(item *rawItem, roles domainFieldRoles) (board.Card, Item) {
	content := item.Content
	isDraft := item.Type == "DRAFT_ISSUE" || (content != nil && content.Typename == "DraftIssue")
	card := board.Card{
		ItemID:    item.ID,
		Title:     "(untitled)",
		CreatedAt: item.CreatedAt,
		Assignees: []string{},
	}
	gh := Item{IsDraft: isDraft}
	if content != nil {
		if content.Title != "" {
			card.Title = content.Title
		}
		gh.URL = content.URL
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
			card.Notes, gh.Events = board.PartitionEvents(card.Notes)
		} else {
			card.Description = content.Body
			card.Notes = domainCommentNotes(content)
			card.Notes, gh.Events, _ = domainSplitLogComments(content, card.Notes)
		}
	}
	for i := range item.FieldValues.Nodes {
		applyDomainRole(&card, &item.FieldValues.Nodes[i], roles)
	}
	return card, gh
}

// domainLogMarker separates a draft card's description from its appended action
// log.
const domainLogMarker = "<!-- aeman:log -->"

// domainParseDraftBody splits a draft body into a description and its action
// log: with a log marker the description is the text before it; a legacy body
// without a marker keeps its note-shaped lines as the log and the rest as the
// description.
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

// domainCommentNotes maps an issue/PR's comment thread onto domain notes.
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
// first log comment's id.
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

// applyDomainRole records a single raw field value under the matching typed
// role.
func applyDomainRole(card *board.Card, v *rawFieldValue, roles domainFieldRoles) {
	if v.Field == nil || v.Field.ID == "" {
		return
	}
	id := v.Field.ID
	switch {
	case roles.Zone != nil && id == roles.Zone.ID && v.OptionID != "":
		for _, o := range roles.Zone.Options {
			if o.ID == v.OptionID {
				card.Zone = zoneFromColor(o.Color)
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
	case roles.Plan != nil && id == roles.Plan.ID && v.Name != "":
		if strings.EqualFold(v.Name, "fri") {
			card.Plan = board.PlanFri
		} else {
			card.Plan = board.PlanWed
		}
	case roles.Week != nil && id == roles.Week.ID && v.Date != "":
		card.Week = v.Date
	case roles.Epic != nil && id == roles.Epic.ID && v.Text != "":
		card.Epic = v.Text
	case roles.Project != nil && id == roles.Project.ID && v.Text != "":
		card.Project = v.Text
	case roles.Process != nil && id == roles.Process.ID && v.Text != "":
		card.Process = v.Text
	case roles.Task != nil && id == roles.Task.ID && v.Text != "":
		card.Task = v.Text
	case roles.Accumulate != nil && id == roles.Accumulate.ID && v.Text != "":
		card.Accumulate = v.Text == "yes"
	case roles.Paused != nil && id == roles.Paused.ID && v.Text != "":
		card.Paused = v.Text == "yes"
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
// domain reads.
type domainFieldRoles struct {
	Zone        *Field
	Progress    *Field
	Day         *Field
	Start       *Field
	SprintStart *Field
	Sprint      *Field
	Status      *Field
	Plan        *Field
	Week        *Field
	Epic        *Field
	Project     *Field
	Process     *Field
	Task        *Field
	Accumulate  *Field
	Paused      *Field
	Stage       *Field
	Team        *Field
	ReviewOf    *Field
	Parent      *Field
	ReviewRound *Field
	Recurrence  *Field
}

// domainRoleAliases maps each role to the field names (case-insensitive) that
// fill it. Stage is its own role, distinct from the default Status field.
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
	"epic":        {"epic"},
	"project":     {"project"},
	"process":     {"process"},
	"task":        {"task"},
	"accumulate":  {"accumulate"},
	"paused":      {"paused"},
	"stage":       {"stage"},
	"team":        {"team"},
	"reviewOf":    {"review of", "reviewof"},
	"parent":      {"parent"},
	"reviewRound": {"review round", "reviewround"},
	"recurrence":  {"recurrence", "recur"},
}

// domainRoles maps the board's fields onto the typed roles by name.
func domainRoles(fields []Field) domainFieldRoles {
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
		case r.Epic == nil && domainMatchesAlias("epic", name):
			r.Epic = f
		case r.Project == nil && domainMatchesAlias("project", name):
			r.Project = f
		case r.Process == nil && domainMatchesAlias("process", name):
			r.Process = f
		case r.Task == nil && domainMatchesAlias("task", name):
			r.Task = f
		case r.Accumulate == nil && domainMatchesAlias("accumulate", name):
			r.Accumulate = f
		case r.Paused == nil && domainMatchesAlias("paused", name):
			r.Paused = f
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

// domainMatchesAlias reports whether the lower-cased field name fills the role.
func domainMatchesAlias(role, lowerName string) bool {
	for _, alias := range domainRoleAliases[role] {
		if alias == lowerName {
			return true
		}
	}
	return false
}
