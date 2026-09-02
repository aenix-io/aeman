// Package gitstore is aeman's storage backend: a board is a set of files in
// a git repository, every change is a commit. This file is the layout — where
// a card lives and what its file says. See docs/design/git-backend.md.
package gitstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/aenix-io/aeman/pkg/board"
)

// ErrBadID is returned for an id that cannot be placed in the layout.
var ErrBadID = errors.New("gitstore: id too short to shard")

// CardPath is where a card lives: cards/<a>/<b>/<id>.md, with a and b the
// id's LAST two characters, one per level. Every commit rewrites the tree of
// each directory on the changed file's path, so the leaves stay tiny; the
// tail rather than the head because a ULID's head is a timestamp. The path
// carries identity only — no state ever moves a file.
func CardPath(id string) (string, error) {
	if len(id) < 2 {
		return "", fmt.Errorf("%w: %q", ErrBadID, id)
	}
	tail := strings.ToLower(id[len(id)-2:])
	return "cards/" + tail[:1] + "/" + tail[1:] + "/" + id + ".md", nil
}

// CardFile is one card's file: the card, plus front-matter keys this server
// does not know and carries along unchanged — a newer server must not lose
// what an older one wrote, nor the other way round.
type CardFile struct {
	Card  board.Card
	Extra []ExtraField
}

// ExtraField is an unknown front-matter key, kept in first-seen order.
type ExtraField struct {
	Key   string
	Value *yaml.Node
}

const (
	fence        = "---\n"
	notesHeading = "## Notes"
)

// EncodeCard renders a card file: front-matter, the description, the notes.
// Empty fields are omitted — the file says what is, not what is not — and
// derived states (In Progress, done-by-100%) are never written because they
// are never in the card to begin with.
func EncodeCard(f CardFile) ([]byte, error) {
	c := f.Card
	var b bytes.Buffer
	b.WriteString(fence)
	w := func(key, val string) {
		if val == "" {
			return
		}
		b.WriteString(key + ": " + scalar(val) + "\n")
	}
	wi := func(key string, val int) {
		if val != 0 {
			b.WriteString(key + ": " + strconv.Itoa(val) + "\n")
		}
	}
	w("title", c.Title)
	if len(c.Assignees) > 0 {
		items := make([]string, len(c.Assignees))
		for i, a := range c.Assignees {
			items[i] = scalar(a)
		}
		b.WriteString("assignees: [" + strings.Join(items, ", ") + "]\n")
	}
	w("author", c.Author)
	w("team", c.Team)
	w("zone", string(c.Zone))
	w("stage", string(c.Stage))
	wi("progress", c.Progress)
	wi("doneFrom", c.DoneFrom)
	w("doneAt", c.DoneAt)
	w("leftAt", c.LeftAt)
	w("start", c.StartDate)
	w("day", c.Day)
	w("sprint", c.SprintStart)
	w("plan", string(c.Plan))
	w("week", c.Week)
	w("lane", string(c.Lane))
	w("project", c.Project)
	w("epic", c.Epic)
	if len(c.Mirrors) > 0 {
		// Structured YAML, not a joined string: project and epic names are
		// user text and no separator survives them (#124's lesson).
		seq := &yaml.Node{Kind: yaml.SequenceNode}
		for _, m := range c.Mirrors {
			seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle, Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "project"}, {Kind: yaml.ScalarNode, Value: m.Project},
				{Kind: yaml.ScalarNode, Value: "epic"}, {Kind: yaml.ScalarNode, Value: m.Epic},
			}})
		}
		out, err := yaml.Marshal(&yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "mirrors"}, seq,
		}})
		if err != nil {
			return nil, fmt.Errorf("gitstore: encode mirrors: %w", err)
		}
		b.Write(out)
	}
	w("parent", c.Parent)
	w("reviewOf", c.ReviewOf)
	wi("reviewRound", c.ReviewRound)
	w("recurrence", c.Recurrence)
	w("process", c.Process)
	w("task", c.Task)
	if c.Accumulate {
		b.WriteString("accumulate: true\n")
	}
	w("link", c.Link)
	w("github", c.GitHubID)
	w("movedFrom", c.MovedFrom)
	w("movedAt", c.MovedAt)
	w("rank", c.Rank)
	w("created", c.CreatedAt)
	for _, x := range f.Extra {
		out, err := yaml.Marshal(&yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: x.Key}, x.Value,
		}})
		if err != nil {
			return nil, fmt.Errorf("gitstore: encode %s: %w", x.Key, err)
		}
		b.Write(out)
	}
	b.WriteString(fence)

	if c.Description != "" {
		b.WriteString("\n" + c.Description + "\n")
	}
	// The notes heading is written whenever there are notes, and also
	// when the description happens to contain one of its own — the reader
	// takes the LAST heading as ours, so ours must be there to be last.
	if len(c.Notes) > 0 || strings.Contains(c.Description, notesHeading) {
		b.WriteString("\n" + notesHeading + "\n")
		if len(c.Notes) > 0 {
			b.WriteString("\n")
			for _, n := range c.Notes {
				b.WriteString(noteLine(n))
			}
		}
	}
	return b.Bytes(), nil
}

// noteLine renders one note: id, [timestamp], author, em dash, text;
// continuation lines indented two spaces.
func noteLine(n board.Note) string {
	head := "- " + n.ID + " [" + n.CreatedAt + "] "
	if n.Author != "" {
		head += n.Author + " "
	}
	return head + "— " + strings.ReplaceAll(n.Body, "\n", "\n  ") + "\n"
}

// scalar renders a string as a YAML scalar: plain when it can be read back
// unchanged, else JSON-quoted (a JSON string is a valid YAML double-quoted
// scalar). Values that YAML would read as another type (dates, numbers,
// booleans) stay plain — the reader takes the node's text, not its type.
func scalar(s string) string {
	if plainSafe(s) {
		return s
	}
	q, _ := json.Marshal(s)
	return string(q)
}

func plainSafe(s string) bool {
	if s == "" || s != strings.TrimSpace(s) {
		return false
	}
	if strings.ContainsAny(s, "\n\r\t") || strings.Contains(s, ": ") || strings.Contains(s, " #") || strings.HasSuffix(s, ":") {
		return false
	}
	switch s[0] {
	case '[', ']', '{', '}', '&', '*', '!', '|', '>', '\'', '"', '%', '@', '`', '#', ',', '?', '-':
		return false
	}
	return true
}

// DecodeCard parses a card file. id is the card's id — it lives in the path,
// not the file — and is stamped onto the card.
func DecodeCard(id string, data []byte) (CardFile, error) {
	f := CardFile{}
	f.Card.ItemID = id
	if !bytes.HasPrefix(data, []byte(fence)) {
		return f, errors.New("gitstore: card file has no front-matter")
	}
	rest := data[len(fence):]
	end := bytes.Index(rest, []byte("\n"+fence))
	var front, body []byte
	switch {
	case bytes.HasPrefix(rest, []byte(fence)):
		front, body = nil, rest[len(fence):]
	case end >= 0:
		front, body = rest[:end+1], rest[end+1+len(fence):]
	default:
		return f, errors.New("gitstore: card file front-matter is not closed")
	}
	if err := decodeFront(&f, front); err != nil {
		return f, err
	}
	// A hand-written mirror equal to the home pair, or written twice, is
	// the state the x bug lives in: the slot drawn twice, the x
	// unmirroring instead of removing. Dropped in a post-pass — post-pass
	// because a hand-written file guarantees no key order, so the home may
	// be read after the mirrors.
	// A subtask carries at most the ONE column of its own (G57, which the
	// Project board draws); a SECOND placement it may not have, because
	// its file follows its parent and every mirror would be stranded the
	// moment the parent changes repository.
	if f.Card.Parent != "" {
		f.Card.Mirrors = nil
	}
	if len(f.Card.Mirrors) > 0 {
		seen := map[board.Placement]bool{}
		kept := f.Card.Mirrors[:0]
		for _, m := range f.Card.Mirrors {
			if (m.Project == f.Card.Project && m.Epic == f.Card.Epic) || seen[m] {
				continue
			}
			seen[m] = true
			kept = append(kept, m)
		}
		f.Card.Mirrors = kept
		if len(f.Card.Mirrors) == 0 {
			f.Card.Mirrors = nil
		}
	}
	desc, notes := splitBody(string(body))
	f.Card.Description = desc
	f.Card.Notes = notes
	return f, nil
}

// decodeFront reads the known keys into the card and keeps the rest.
func decodeFront(f *CardFile, front []byte) error {
	if len(bytes.TrimSpace(front)) == 0 {
		return nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(front, &doc); err != nil {
		return fmt.Errorf("gitstore: front-matter: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return errors.New("gitstore: front-matter is not a mapping")
	}
	m := doc.Content[0]
	for i := 0; i+1 < len(m.Content); i += 2 {
		key, val := m.Content[i].Value, m.Content[i+1]
		if !setKnown(&f.Card, key, val) {
			f.Extra = append(f.Extra, ExtraField{Key: key, Value: val})
		}
	}
	return nil
}

// setKnown stores one front-matter key on the card; false means the key is
// not one of ours.
func setKnown(c *board.Card, key string, val *yaml.Node) bool {
	num := func() int { n, _ := strconv.Atoi(val.Value); return n }
	strs := map[string]*string{
		"title": &c.Title, "author": &c.Author, "team": &c.Team, "start": &c.StartDate,
		"day": &c.Day, "sprint": &c.SprintStart, "week": &c.Week, "project": &c.Project,
		"epic": &c.Epic, "parent": &c.Parent, "reviewOf": &c.ReviewOf, "recurrence": &c.Recurrence,
		"process": &c.Process, "task": &c.Task, "link": &c.Link, "github": &c.GitHubID,
		"movedFrom": &c.MovedFrom, "movedAt": &c.MovedAt, "rank": &c.Rank, "created": &c.CreatedAt,
	}
	if p, ok := strs[key]; ok {
		*p = val.Value
		return true
	}
	switch key {
	case "assignees":
		for _, it := range val.Content {
			c.Assignees = append(c.Assignees, it.Value)
		}
	case "mirrors":
		for _, it := range val.Content {
			var m board.Placement
			for i := 0; i+1 < len(it.Content); i += 2 {
				switch it.Content[i].Value {
				case "project":
					m.Project = it.Content[i+1].Value
				case "epic":
					m.Epic = it.Content[i+1].Value
				}
			}
			// A hand-written scalar (`mirrors: [foo]`) decodes to an empty
			// pair. A column is named by its EPIC, though: the no-project
			// bucket is a lawful mirror home (G15), so only the epic half
			// is required — dropping every project-less entry erased the
			// very placements the service had just accepted.
			if m.Epic == "" {
				continue
			}
			c.Mirrors = append(c.Mirrors, m)
		}
	case "zone":
		c.Zone = board.ZoneKey(val.Value)
	case "stage":
		c.Stage = board.StageKey(val.Value)
	case "plan":
		c.Plan = board.PlanBand(val.Value)
	case "lane":
		c.Lane = board.Lane(val.Value)
	case "progress":
		c.Progress = num()
	case "doneFrom":
		c.DoneFrom = num()
	case "doneAt":
		c.DoneAt = val.Value
	case "leftAt":
		c.LeftAt = val.Value
	case "reviewRound":
		c.ReviewRound = num()
	case "accumulate":
		c.Accumulate = val.Value == "true"
	default:
		return false
	}
	return true
}

var noteHeadRe = regexp.MustCompile(`^- (\S+) \[([^\]]+)\] (?:(\S+) )?— ?(.*)$`)

// splitBody separates the description from the notes: the LAST "## Notes"
// heading is ours (a description may contain one of its own), everything
// after it is note items.
func splitBody(body string) (string, []board.Note) {
	marker := "\n" + notesHeading + "\n"
	idx := strings.LastIndex(body, marker)
	if idx < 0 {
		return strings.Trim(body, "\n"), nil
	}
	desc := strings.Trim(body[:idx], "\n")
	var notes []board.Note
	for _, line := range strings.Split(body[idx+len(marker):], "\n") {
		if m := noteHeadRe.FindStringSubmatch(line); m != nil {
			notes = append(notes, board.Note{ID: m[1], CreatedAt: m[2], Author: m[3], Body: m[4], Source: "draft"})
			continue
		}
		if len(notes) > 0 && strings.HasPrefix(line, "  ") {
			notes[len(notes)-1].Body += "\n" + line[2:]
		}
	}
	for i := range notes {
		notes[i].Body = strings.TrimRight(notes[i].Body, "\n")
	}
	return desc, notes
}
