package gitstore

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// The roster — teams, projects, epics, deadlines, processes and the board
// file — is configuration that used to hide in aeman:*-state cards. Here
// each is a small YAML file at a path that carries identity only.

// SchemaVersion is the layout version this server writes and understands.
const SchemaVersion = 1

// BoardPath is the board's own file, primary domain only.
const BoardPath = "board.yaml"

// ErrSchemaNewer is returned when a repository was written by a newer
// server: refusing is safer than misreading it.
var ErrSchemaNewer = errors.New("gitstore: repository schema is newer than this server")

// TeamPath is where a team lives; the no-team group is the team "_".
func TeamPath(id string) string { return "teams/" + id + ".yaml" }

// ProjectPath is a project's own file.
func ProjectPath(id string) string { return "projects/" + id + "/project.yaml" }

// EpicPath is one column of a project.
func EpicPath(projectID, id string) string {
	return "projects/" + projectID + "/epics/" + id + ".yaml"
}

// DeadlinePath is one deadline line of a project.
func DeadlinePath(projectID, id string) string {
	return "projects/" + projectID + "/deadlines/" + id + ".yaml"
}

// ProcessPath is a process's own file.
func ProcessPath(id string) string { return "processes/" + id + "/process.yaml" }

// TaskPath is one task of a process — a card file, minus placement.
func TaskPath(processID, id string) string {
	return "processes/" + processID + "/tasks/" + id + ".md"
}

// PathKind classifies a path in the layout.
type PathKind int

// The kinds of path the layout has.
const (
	PathUnknown PathKind = iota
	PathBoard
	PathCard
	PathTeam
	PathProject
	PathEpic
	PathDeadline
	PathProcess
	PathTask
)

// ParsePath is the layout's inverse: what a path is and the ids in it. A
// remote commit's diff goes through it so the cache reloads exactly the
// objects that changed.
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
	case len(parts) == 3 && parts[0] == "projects" && parts[2] == "project.yaml":
		return PathProject, []string{parts[1]}
	case len(parts) == 4 && parts[0] == "projects" && parts[2] == "epics":
		if id, ok := strip(parts[3], ".yaml"); ok {
			return PathEpic, []string{parts[1], id}
		}
	case len(parts) == 4 && parts[0] == "projects" && parts[2] == "deadlines":
		if id, ok := strip(parts[3], ".yaml"); ok {
			return PathDeadline, []string{parts[1], id}
		}
	case len(parts) == 3 && parts[0] == "processes" && parts[2] == "process.yaml":
		return PathProcess, []string{parts[1]}
	case len(parts) == 4 && parts[0] == "processes" && parts[2] == "tasks":
		if id, ok := strip(parts[3], ".md"); ok {
			return PathTask, []string{parts[1], id}
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
	Name    string
	Rank    string
	Created string
	Sprint  SprintPointer
	Extra   []ExtraField
}

// ProjectFile is projects/<id>/project.yaml.
type ProjectFile struct {
	Name    string
	Rank    string
	Created string
	Extra   []ExtraField
}

// EpicFile is projects/<pid>/epics/<id>.yaml.
type EpicFile struct {
	Name    string
	Rank    string
	Created string
	Extra   []ExtraField
}

// DeadlineFile is projects/<pid>/deadlines/<id>.yaml.
type DeadlineFile struct {
	Week    string
	Created string
	Extra   []ExtraField
}

// ProcessFile is processes/<id>/process.yaml.
type ProcessFile struct {
	Name    string
	Project string
	Paused  bool
	Rank    string
	Created string
	Extra   []ExtraField
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

func (w *yamlWriter) flag(key string, val bool) {
	if val {
		w.b.WriteString(key + ": true\n")
	}
}

// finish appends the unknown keys and returns the file.
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
	return w.finish(f.Extra)
}

// EncodeProject renders a project file.
func EncodeProject(f ProjectFile) ([]byte, error) {
	var w yamlWriter
	w.str("name", f.Name)
	w.str("rank", f.Rank)
	w.str("created", f.Created)
	return w.finish(f.Extra)
}

// EncodeEpic renders an epic file.
func EncodeEpic(f EpicFile) ([]byte, error) {
	var w yamlWriter
	w.str("name", f.Name)
	w.str("rank", f.Rank)
	w.str("created", f.Created)
	return w.finish(f.Extra)
}

// EncodeDeadline renders a deadline file.
func EncodeDeadline(f DeadlineFile) ([]byte, error) {
	var w yamlWriter
	w.str("week", f.Week)
	w.str("created", f.Created)
	return w.finish(f.Extra)
}

// EncodeProcess renders a process file.
func EncodeProcess(f ProcessFile) ([]byte, error) {
	var w yamlWriter
	w.str("name", f.Name)
	w.str("project", f.Project)
	w.flag("paused", f.Paused)
	w.str("rank", f.Rank)
	w.str("created", f.Created)
	return w.finish(f.Extra)
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
			extra = append(extra, ExtraField{Key: pairs[i].Value, Value: pairs[i+1]})
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
		default:
			return false
		}
		return true
	})
	f.Extra = extra
	return f, err
}

// DecodeProject parses a project file.
func DecodeProject(data []byte) (ProjectFile, error) {
	var f ProjectFile
	extra, err := each(data, func(key string, val *yaml.Node) bool {
		return setNamed(&f.Name, &f.Rank, &f.Created, key, val)
	})
	f.Extra = extra
	return f, err
}

// DecodeEpic parses an epic file.
func DecodeEpic(data []byte) (EpicFile, error) {
	var f EpicFile
	extra, err := each(data, func(key string, val *yaml.Node) bool {
		return setNamed(&f.Name, &f.Rank, &f.Created, key, val)
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

// DecodeDeadline parses a deadline file.
func DecodeDeadline(data []byte) (DeadlineFile, error) {
	var f DeadlineFile
	extra, err := each(data, func(key string, val *yaml.Node) bool {
		switch key {
		case "week":
			f.Week = val.Value
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

// DecodeProcess parses a process file.
func DecodeProcess(data []byte) (ProcessFile, error) {
	var f ProcessFile
	extra, err := each(data, func(key string, val *yaml.Node) bool {
		switch key {
		case "name":
			f.Name = val.Value
		case "project":
			f.Project = val.Value
		case "paused":
			f.Paused = val.Value == "true"
		case "rank":
			f.Rank = val.Value
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
