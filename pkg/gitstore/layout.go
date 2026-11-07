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
	// Week is the `week` a file written before schema 3 still carries. No
	// card has one (K7): it is read for the schema-3 bump and the importer,
	// which place a task by it, and never written back.
	Week string
	// ProjectCard reports a file the board no longer reads: a card in a
	// Project-board column, a `scope` (or the pre-schema-2 `epic`) in its
	// front matter. Project cards were removed with the Project board (H15);
	// their files stay in the repository untouched, and every reader skips
	// them — so nothing ever writes or deletes one either.
	ProjectCard bool
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
	// The level, right after the title: what the card IS comes before
	// everything it carries (docs/design/hierarchy.md). A task writes none.
	w("type", c.Type)
	if len(c.Assignees) > 0 {
		items := make([]string, len(c.Assignees))
		for i, a := range c.Assignees {
			items[i] = scalar(a)
		}
		b.WriteString("assignees: [" + strings.Join(items, ", ") + "]\n")
	}
	w("author", c.Author)
	w("team", c.Team)
	// A planning card's owner (one login) and any card's customers (roster
	// ids): docs/design/ownership.md.
	w("owner", c.Owner)
	if len(c.Customers) > 0 {
		items := make([]string, len(c.Customers))
		for i, id := range c.Customers {
			items[i] = scalar(id)
		}
		b.WriteString("customers: [" + strings.Join(items, ", ") + "]\n")
	}
	w("zone", string(c.Zone))
	w("size", string(c.Size))
	w("priority", string(c.Priority))
	w("stage", string(c.Stage))
	wi("progress", c.Progress)
	wi("doneFrom", c.DoneFrom)
	w("doneAt", c.DoneAt)
	w("leftAt", c.LeftAt)
	w("start", c.StartDate)
	w("day", c.Day)
	w("sprint", c.SprintStart)
	// No `week`: a task has none (K7). The reader still takes the key, so a
	// file written before schema 3 is understood (and parked by the bump),
	// and the next write of the card drops it.
	if c.Parked {
		b.WriteString("parked: true\n")
	}
	w("parent", c.Parent)
	w("reviewOf", c.ReviewOf)
	wi("reviewRound", c.ReviewRound)
	w("recurrence", c.Recurrence)
	w("link", c.Link)
	w("github", c.GitHubID)
	w("movedFrom", c.MovedFrom)
	w("movedAt", c.MovedAt)
	w("rank", c.Rank)
	w("created", c.CreatedAt)
	for _, x := range f.Extra {
		// An invalid priority rides as an unknown key only until a valid one
		// is set: written beside it, the file would carry the key twice — a
		// duplicate YAML key no later parse survives.
		if x.Key == "priority" && c.Priority != "" {
			continue
		}
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
	front, body, err := splitFront(data)
	if err != nil {
		return f, err
	}
	if err := decodeFront(&f, front); err != nil {
		return f, err
	}
	desc, notes := splitBody(string(body))
	f.Card.Description = desc
	f.Card.Notes = notes
	return f, nil
}

// splitFront cuts a card file at its fences: the front-matter (without the
// fences) and the body after the closing one. The opening fence has been
// checked by the caller.
func splitFront(data []byte) (front, body []byte, err error) {
	rest := data[len(fence):]
	end := bytes.Index(rest, []byte("\n"+fence))
	switch {
	case bytes.HasPrefix(rest, []byte(fence)):
		return nil, rest[len(fence):], nil
	case end >= 0:
		return rest[:end+1], rest[end+1+len(fence):], nil
	default:
		return nil, nil, errors.New("gitstore: card file front-matter is not closed")
	}
}

// frontKeys lists the top-level keys of a card file's front-matter, in file
// order, without reading the card: what a migration asks before deciding
// whether a file needs rewriting at all.
func frontKeys(data []byte) ([]string, error) {
	if !bytes.HasPrefix(data, []byte(fence)) {
		return nil, errors.New("gitstore: card file has no front-matter")
	}
	front, _, err := splitFront(data)
	if err != nil || len(bytes.TrimSpace(front)) == 0 {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(front, &doc); err != nil {
		return nil, fmt.Errorf("gitstore: front-matter: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("gitstore: front-matter is not a mapping")
	}
	m := doc.Content[0]
	keys := make([]string, 0, len(m.Content)/2)
	for i := 0; i+1 < len(m.Content); i += 2 {
		keys = append(keys, m.Content[i].Value)
	}
	return keys, nil
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
		if !setKnown(f, key, val) {
			// An unknown key is kept verbatim so a newer server does not lose
			// it — but its value node is marshalled ALONE on re-encode, so an
			// alias (`*t`) whose anchor (`&t`) sits on another key would be
			// written dangling and every later parse of the card would fail:
			// the card drops into Broken for good, on every replica, the next
			// time anyone edits it. Resolve aliases to their target and strip
			// anchors so the kept fragment stands on its own. The board's own
			// writer never emits either.
			f.Extra = append(f.Extra, ExtraField{Key: key, Value: plainNode(val)})
		}
	}
	return nil
}

// plainNode returns a copy of n with every alias resolved to its target and
// every anchor removed, so the node can be marshalled as a self-contained
// fragment. It follows the alias chain and recurses into mappings and
// sequences; a cyclic anchor (which yaml.v3 already refuses to unmarshal) is
// guarded against all the same.
func plainNode(n *yaml.Node) *yaml.Node {
	return resolveNode(n, map[*yaml.Node]bool{})
}

func resolveNode(n *yaml.Node, seen map[*yaml.Node]bool) *yaml.Node {
	if n == nil || seen[n] {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
	}
	seen[n] = true
	defer delete(seen, n)
	if n.Kind == yaml.AliasNode {
		return resolveNode(n.Alias, seen)
	}
	out := *n
	out.Anchor = ""
	if len(n.Content) > 0 {
		out.Content = make([]*yaml.Node, len(n.Content))
		for i, c := range n.Content {
			out.Content[i] = resolveNode(c, seen)
		}
	}
	return &out
}

// setKnown stores one front-matter key on the card; false means the key is
// not one of ours. The keys of a retired feature — `process`, `task`,
// `accumulate` and `mirrors` (schema 1) — are deliberately NOT ours any
// more: on a file written before the migration they ride through as Extra,
// so a record of that day still shows the card as it stood, and the
// migration strips them once rather than every reader forgetting them.
func setKnown(f *CardFile, key string, val *yaml.Node) bool {
	c := &f.Card
	num := func() int { n, _ := strconv.Atoi(val.Value); return n }
	strs := map[string]*string{
		"title": &c.Title, "type": &c.Type, "author": &c.Author, "team": &c.Team, "owner": &c.Owner, "start": &c.StartDate,
		"day": &c.Day, "sprint": &c.SprintStart, "week": &f.Week,
		"parent": &c.Parent, "reviewOf": &c.ReviewOf, "recurrence": &c.Recurrence,
		"link": &c.Link, "github": &c.GitHubID,
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
	case "customers":
		for _, it := range val.Content {
			c.Customers = append(c.Customers, it.Value)
		}
	case "zone":
		c.Zone = board.ZoneKey(val.Value)
	case "size":
		c.Size = board.SizeKey(val.Value)
	case "stage":
		c.Stage = board.StageKey(val.Value)
	case "scope", "epic":
		// A card in a Project-board column — `epic` is what the key was
		// called before schema 2 (nothing to do with the planning level of
		// the same name, which is a card TYPE). Project cards are gone (H15):
		// the file is marked for every reader to skip, never rewritten.
		if val.Value != "" {
			f.ProjectCard = true
		}
	case "project":
		// A project name on a card in no column filed it nowhere; with the
		// Project board gone it means nothing, and is read and dropped like
		// `plan` below rather than ridden through on every edit.
	case "plan":
		// The weekly plan is gone. The key is READ and dropped rather than
		// left unknown: an unknown key rides through in Extra and would be
		// written back on every edit, so a board would keep re-committing a
		// band nothing has meant since v0.20.
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
	case "parked":
		c.Parked = val.Value == "true"
	case "priority":
		// A value the board has no such thing as is an UNKNOWN key: kept
		// as it is, and no priority — it neither shadows what the card
		// inherits nor reaches a status.
		if !board.ValidPriority(val.Value) {
			return false
		}
		c.Priority = board.PriorityKey(val.Value)
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
