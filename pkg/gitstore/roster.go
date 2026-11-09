package gitstore

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/aenix-io/aeman/pkg/board"
)

// The roster — teams, customers, users and the board file — is
// configuration that used to hide in aeman:*-state cards. Here each is a
// small YAML file at a path that carries identity only.

// SchemaVersion is the layout version this server writes and understands.
// Version 2 retired the Process feature: the processes/ directory and the
// process, task and accumulate card keys (docs/design/hierarchy.md).
// Version 3 is the team backlog: the weekly plan and the Triage strip are
// parked and a task's week is gone (docs/design/backlog.md). Version 4 is
// the ordering model: a task's rank is its team-queue place and an epic's the
// board-wide epic order (docs/design/ordering.md), reached by
// MigrateOrdering over every repository at once.
const SchemaVersion = 4

// BoardPath is the board's own file, primary domain only.
const BoardPath = "board.yaml"

// ErrSchemaNewer is returned when a repository was written by a newer
// server: refusing is safer than misreading it.
var ErrSchemaNewer = errors.New("gitstore: repository schema is newer than this server")

// TeamPath is where a team lives; the no-team group is the team "_".
func TeamPath(id string) string { return "teams/" + id + ".yaml" }

// PathKind classifies a path in the layout.
type PathKind int

// The kinds of path the layout has.
const (
	PathUnknown PathKind = iota
	PathBoard
	PathCard
	PathTeam
	// PathProjectBoard is anything under projects/: the retired Project
	// board's roster — projects, their columns and deadline lines (H15). A
	// repository may still hold it; every reader skips it and no writer
	// touches it.
	PathProjectBoard
	PathUser
	PathCustomer
)

// CustomerPath is customers/<id>.yaml — one entry of the board's customer
// roster, in the primary repository (docs/design/ownership.md).
func CustomerPath(id string) string { return "customers/" + id + ".yaml" }

// CustomerFile is customers/<id>.yaml: the customer's name and its place in
// the roster. Cards name it by id, so a rename is this file alone.
type CustomerFile struct {
	Name    string
	Rank    string
	Created string
	Extra   []ExtraField
}

// EncodeCustomer renders a customer file.
func EncodeCustomer(f CustomerFile) ([]byte, error) {
	var w yamlWriter
	w.str("name", f.Name)
	w.str("rank", f.Rank)
	w.str("created", f.Created)
	return w.finish(f.Extra)
}

// DecodeCustomer parses a customer file; unknown keys are kept.
func DecodeCustomer(data []byte) (CustomerFile, error) {
	var f CustomerFile
	extra, err := each(data, func(key string, val *yaml.Node) bool {
		return setNamed(&f.Name, &f.Rank, &f.Created, key, val)
	})
	f.Extra = extra
	return f, err
}

// UserPath is users/<login>.yaml — a person's roster entry (their capacity),
// read from the primary only.
func UserPath(login string) string { return "users/" + login + ".yaml" }

// UserFile is users/<login>.yaml.
type UserFile struct {
	// Capacity is the points a week a lead set for this person; 0 = none
	// set, and the board does not invent one (board.CapacityOfPerson).
	Capacity int
	Created  string
	Extra    []ExtraField
}

// ParsePath is the layout's inverse: what a path is and the ids in it. A
// remote commit's diff goes through it so the cache reloads exactly the
// objects that changed. A path the layout no longer has — the processes/
// tree of a schema-1 repository, read on a day before the migration — is
// PathUnknown, which the loader records and skips.
func ParsePath(p string) (PathKind, []string) {
	parts := strings.Split(p, "/")
	strip := func(s, ext string) (string, bool) {
		if !strings.HasSuffix(s, ext) || len(s) == len(ext) {
			return "", false
		}
		return strings.TrimSuffix(s, ext), true
	}
	switch {
	case len(parts) == 1 && parts[0] == BoardPath:
		return PathBoard, nil
	case len(parts) == 4 && parts[0] == "cards" && len(parts[1]) == 1 && len(parts[2]) == 1:
		if id, ok := strip(parts[3], ".md"); ok {
			return PathCard, []string{id}
		}
	case len(parts) == 2 && parts[0] == "teams":
		if id, ok := strip(parts[1], ".yaml"); ok {
			return PathTeam, []string{id}
		}
	case len(parts) > 1 && parts[0] == "projects":
		return PathProjectBoard, nil
	case len(parts) == 2 && parts[0] == "customers":
		if id, ok := strip(parts[1], ".yaml"); ok {
			return PathCustomer, []string{id}
		}
	case len(parts) == 2 && parts[0] == "users":
		if login, ok := strip(parts[1], ".yaml"); ok {
			return PathUser, []string{login}
		}
	}
	return PathUnknown, nil
}

// SprintPointer is a team's current and previous sprint start.
type SprintPointer struct {
	Current  string
	Previous string
}

// TeamFile is teams/<id>.yaml.
type TeamFile struct {
	Name     string
	Rank     string
	Created  string
	Sprint   SprintPointer
	Capacity board.Capacity
	// CapacityExtra is everything in the `capacity:` mapping this server does
	// not know: the old cards-a-week fields on a file written before they
	// were removed, and whatever another writer put there. Extra keeps the
	// unknown keys of the file's TOP level, and a known key whose value is a
	// mapping needs the same promise one level down — without it, any write
	// to a team file quietly ate the whole block.
	CapacityExtra []ExtraField
	Extra         []ExtraField
}

// BoardFile is board.yaml.
type BoardFile struct {
	Schema int
	Title  string
	Extra  []ExtraField
}

// ---- encoding -------------------------------------------------------------------

// yamlWriter writes "key: value" lines, omitting empties, then the unknown
// keys it was handed.
type yamlWriter struct{ b bytes.Buffer }

func (w *yamlWriter) str(key, val string) {
	if val != "" {
		w.b.WriteString(key + ": " + scalar(val) + "\n")
	}
}

// finish appends the unknown keys and returns the file.
// nested writes unknown keys of a nested mapping, indented under the key that
// holds them. The file is written by hand rather than marshalled whole, so
// this is what keeping a promise one level down costs.
func (w *yamlWriter) nested(xs []ExtraField) error {
	for _, x := range xs {
		out, err := yaml.Marshal(&yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: x.Key}, x.Value,
		}})
		if err != nil {
			return fmt.Errorf("gitstore: encode capacity.%s: %w", x.Key, err)
		}
		for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
			w.b.WriteString("  " + line + "\n")
		}
	}
	return nil
}

func (w *yamlWriter) finish(xs []ExtraField) ([]byte, error) {
	for _, x := range xs {
		out, err := yaml.Marshal(&yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: x.Key}, x.Value,
		}})
		if err != nil {
			return nil, fmt.Errorf("gitstore: encode %s: %w", x.Key, err)
		}
		w.b.Write(out)
	}
	return w.b.Bytes(), nil
}

// EncodeTeam renders a team file.
func EncodeTeam(f TeamFile) ([]byte, error) {
	var w yamlWriter
	w.str("name", f.Name)
	w.str("rank", f.Rank)
	w.str("created", f.Created)
	if f.Sprint != (SprintPointer{}) {
		w.b.WriteString("sprint:\n")
		if f.Sprint.Current != "" {
			w.b.WriteString("  current: " + scalar(f.Sprint.Current) + "\n")
		}
		if f.Sprint.Previous != "" {
			w.b.WriteString("  previous: " + scalar(f.Sprint.Previous) + "\n")
		}
	}
	if f.Capacity != (board.Capacity{}) || len(f.CapacityExtra) > 0 {
		w.b.WriteString("capacity:\n")
		if f.Capacity.Points != 0 {
			w.b.WriteString("  points: " + strconv.Itoa(f.Capacity.Points) + "\n")
		}
		if err := w.nested(f.CapacityExtra); err != nil {
			return nil, err
		}
	}
	return w.finish(f.Extra)
}

// EncodeUser renders a user file.
func EncodeUser(f UserFile) ([]byte, error) {
	var w yamlWriter
	if f.Capacity != 0 {
		w.b.WriteString("capacity: " + strconv.Itoa(f.Capacity) + "\n")
	}
	w.str("created", f.Created)
	return w.finish(f.Extra)
}

// DecodeUser parses a user file; unknown keys are kept.
func DecodeUser(data []byte) (UserFile, error) {
	var f UserFile
	extra, err := each(data, func(key string, val *yaml.Node) bool {
		switch key {
		case "capacity":
			f.Capacity, _ = strconv.Atoi(val.Value)
		case "created":
			f.Created = val.Value
		default:
			return false
		}
		return true
	})
	f.Extra = extra
	return f, err
}

// EncodeBoard renders board.yaml. The schema is always written, even at
// zero, because its absence means "older than versioning".
func EncodeBoard(f BoardFile) ([]byte, error) {
	var w yamlWriter
	w.b.WriteString("schema: " + strconv.Itoa(f.Schema) + "\n")
	w.str("title", f.Title)
	return w.finish(f.Extra)
}

// ---- decoding -------------------------------------------------------------------

// mapping parses a YAML document into its top-level key/value pairs, in
// file order. An empty document is an empty mapping.
func mapping(data []byte) ([]*yaml.Node, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("gitstore: yaml: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("gitstore: file is not a mapping")
	}
	return doc.Content[0].Content, nil
}

// each calls fn for every pair; fn returns false for a key it does not
// know, which is kept as an extra.
func each(data []byte, fn func(key string, val *yaml.Node) bool) ([]ExtraField, error) {
	pairs, err := mapping(data)
	if err != nil {
		return nil, err
	}
	var extra []ExtraField
	for i := 0; i+1 < len(pairs); i += 2 {
		if !fn(pairs[i].Value, pairs[i+1]) {
			// Resolved and anchor-stripped so a kept unknown key holding an
			// alias is not rewritten dangling — see plainNode (the card codec
			// carried the same landmine).
			extra = append(extra, ExtraField{Key: pairs[i].Value, Value: plainNode(pairs[i+1])})
		}
	}
	return extra, nil
}

// DecodeTeam parses a team file.
func DecodeTeam(data []byte) (TeamFile, error) {
	var f TeamFile
	extra, err := each(data, func(key string, val *yaml.Node) bool {
		switch key {
		case "name":
			f.Name = val.Value
		case "rank":
			f.Rank = val.Value
		case "created":
			f.Created = val.Value
		case "sprint":
			for i := 0; i+1 < len(val.Content); i += 2 {
				switch val.Content[i].Value {
				case "current":
					f.Sprint.Current = val.Content[i+1].Value
				case "previous":
					f.Sprint.Previous = val.Content[i+1].Value
				}
			}
		case "capacity":
			for i := 0; i+1 < len(val.Content); i += 2 {
				if val.Content[i].Value == "points" {
					f.Capacity.Points, _ = strconv.Atoi(val.Content[i+1].Value)
					continue
				}
				f.CapacityExtra = append(f.CapacityExtra,
					ExtraField{Key: val.Content[i].Value, Value: plainNode(val.Content[i+1])})
			}
		default:
			return false
		}
		return true
	})
	f.Extra = extra
	return f, err
}

func setNamed(name, rank, created *string, key string, val *yaml.Node) bool {
	switch key {
	case "name":
		*name = val.Value
	case "rank":
		*rank = val.Value
	case "created":
		*created = val.Value
	default:
		return false
	}
	return true
}

// DecodeBoard parses board.yaml. A schema newer than SchemaVersion is
// refused; an older or missing one comes back as is, for migration.
func DecodeBoard(data []byte) (BoardFile, error) {
	var f BoardFile
	extra, err := each(data, func(key string, val *yaml.Node) bool {
		switch key {
		case "schema":
			f.Schema, _ = strconv.Atoi(val.Value)
		case "title":
			f.Title = val.Value
		default:
			return false
		}
		return true
	})
	if err != nil {
		return f, err
	}
	f.Extra = extra
	if f.Schema > SchemaVersion {
		return f, fmt.Errorf("%w: schema %d, this server knows %d", ErrSchemaNewer, f.Schema, SchemaVersion)
	}
	return f, nil
}
