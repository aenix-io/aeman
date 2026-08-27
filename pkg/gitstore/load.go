package gitstore

import (
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/aenix-io/aeman/pkg/board"
)

// ErrEmptyRepository is returned when the branch has no commits: a board
// that was never initialised, which `serve` reports as "run aeman init"
// rather than serving an empty board.
var ErrEmptyRepository = errors.New("gitstore: repository has no commits")

// Snapshot is everything in one domain's tree at one commit.
type Snapshot struct {
	Board     BoardFile
	Cards     []board.Card
	Teams     []Team
	Projects  []Project
	Processes []Process
	// Unknown lists paths that are not part of the layout; Broken lists
	// paths that are, but could not be read. Neither fails the load.
	Unknown []string
	Broken  []BrokenFile
}

// Team is teams/<id>.yaml with its id.
type Team struct {
	ID string
	TeamFile
}

// Project is a project with its columns and deadline lines.
type Project struct {
	ID string
	ProjectFile
	Epics     []Epic
	Deadlines []Deadline
}

// Epic is one column.
type Epic struct {
	ID string
	EpicFile
}

// Deadline is one deadline line.
type Deadline struct {
	ID string
	DeadlineFile
}

// Process is a process with its tasks.
type Process struct {
	ID string
	ProcessFile
	Tasks []Task
}

// Task is one process task — a card file.
type Task struct {
	ID string
	CardFile
}

// BrokenFile is a layout path whose content could not be read.
type BrokenFile struct {
	Path string
	Err  error
}

// Load reads the snapshot at the branch tip.
func Load(r *Repo) (Snapshot, error) {
	head := r.Head()
	if head.IsZero() {
		return Snapshot{}, ErrEmptyRepository
	}
	return LoadAt(r, head)
}

// LoadAt reads the snapshot at a commit — the tip, or a past one for the
// day-state replay.
func LoadAt(r *Repo, h plumbing.Hash) (Snapshot, error) {
	c, err := object.GetCommit(r.s, h)
	if err != nil {
		return Snapshot{}, err
	}
	tree, err := c.Tree()
	if err != nil {
		return Snapshot{}, err
	}
	l := &loader{s: Snapshot{}, projects: map[string]*Project{}, processes: map[string]*Process{}}
	err = tree.Files().ForEach(func(f *object.File) error {
		data, err := readAll(f)
		if err != nil {
			// Recorded as broken; the rest of the board still loads.
			l.s.Broken = append(l.s.Broken, BrokenFile{Path: f.Name, Err: err})
			return nil //nolint:nilerr // see above
		}
		return l.file(f.Name, data)
	})
	if err != nil {
		return Snapshot{}, err
	}
	l.finish()
	return l.s, nil
}

func readAll(f *object.File) ([]byte, error) {
	rd, err := f.Reader()
	if err != nil {
		return nil, err
	}
	defer rd.Close()
	return io.ReadAll(rd)
}

type loader struct {
	s         Snapshot
	projects  map[string]*Project
	processes map[string]*Process
}

// file places one file; a schema the server does not know stops the load,
// anything else broken is recorded and skipped.
func (l *loader) file(p string, data []byte) error {
	kind, ids := ParsePath(p)
	var err error
	switch kind {
	case PathBoard:
		var b BoardFile
		if b, err = DecodeBoard(data); err != nil {
			if errors.Is(err, ErrSchemaNewer) {
				return err
			}
			break
		}
		l.s.Board = b
	case PathCard:
		var f CardFile
		if f, err = DecodeCard(ids[0], data); err == nil {
			l.s.Cards = append(l.s.Cards, f.Card)
		}
	case PathTeam:
		var f TeamFile
		if f, err = DecodeTeam(data); err == nil {
			l.s.Teams = append(l.s.Teams, Team{ID: ids[0], TeamFile: f})
		}
	case PathProject:
		var f ProjectFile
		if f, err = DecodeProject(data); err == nil {
			l.project(ids[0]).ProjectFile = f
		}
	case PathEpic:
		var f EpicFile
		if f, err = DecodeEpic(data); err == nil {
			pr := l.project(ids[0])
			pr.Epics = append(pr.Epics, Epic{ID: ids[1], EpicFile: f})
		}
	case PathDeadline:
		var f DeadlineFile
		if f, err = DecodeDeadline(data); err == nil {
			pr := l.project(ids[0])
			pr.Deadlines = append(pr.Deadlines, Deadline{ID: ids[1], DeadlineFile: f})
		}
	case PathProcess:
		var f ProcessFile
		if f, err = DecodeProcess(data); err == nil {
			l.process(ids[0]).ProcessFile = f
		}
	case PathTask:
		var f CardFile
		if f, err = DecodeCard(ids[1], data); err == nil {
			pc := l.process(ids[0])
			pc.Tasks = append(pc.Tasks, Task{ID: ids[1], CardFile: f})
		}
	default:
		l.s.Unknown = append(l.s.Unknown, p)
	}
	if err != nil {
		l.s.Broken = append(l.s.Broken, BrokenFile{Path: p, Err: fmt.Errorf("%s: %w", p, err)})
	}
	return nil
}

func (l *loader) project(id string) *Project {
	if p, ok := l.projects[id]; ok {
		return p
	}
	p := &Project{ID: id}
	l.projects[id] = p
	return p
}

func (l *loader) process(id string) *Process {
	if p, ok := l.processes[id]; ok {
		return p
	}
	p := &Process{ID: id}
	l.processes[id] = p
	return p
}

// finish sorts every list by rank, ties by id — one deterministic order
// wherever the snapshot is read.
func (l *loader) finish() {
	sort.Slice(l.s.Cards, func(i, j int) bool {
		return byRank(l.s.Cards[i].Rank, l.s.Cards[i].ItemID, l.s.Cards[j].Rank, l.s.Cards[j].ItemID)
	})
	sort.Slice(l.s.Teams, func(i, j int) bool {
		return byRank(l.s.Teams[i].Rank, l.s.Teams[i].ID, l.s.Teams[j].Rank, l.s.Teams[j].ID)
	})
	sort.Slice(l.s.Unknown, func(i, j int) bool { return l.s.Unknown[i] < l.s.Unknown[j] })
	for _, p := range l.projects {
		sort.Slice(p.Epics, func(i, j int) bool { return byRank(p.Epics[i].Rank, p.Epics[i].ID, p.Epics[j].Rank, p.Epics[j].ID) })
		sort.Slice(p.Deadlines, func(i, j int) bool {
			return byRank(p.Deadlines[i].Week, p.Deadlines[i].ID, p.Deadlines[j].Week, p.Deadlines[j].ID)
		})
		l.s.Projects = append(l.s.Projects, *p)
	}
	sort.Slice(l.s.Projects, func(i, j int) bool {
		return byRank(l.s.Projects[i].Rank, l.s.Projects[i].ID, l.s.Projects[j].Rank, l.s.Projects[j].ID)
	})
	for _, p := range l.processes {
		sort.Slice(p.Tasks, func(i, j int) bool {
			return byRank(p.Tasks[i].Card.Rank, p.Tasks[i].ID, p.Tasks[j].Card.Rank, p.Tasks[j].ID)
		})
		l.s.Processes = append(l.s.Processes, *p)
	}
	sort.Slice(l.s.Processes, func(i, j int) bool {
		return byRank(l.s.Processes[i].Rank, l.s.Processes[i].ID, l.s.Processes[j].Rank, l.s.Processes[j].ID)
	})
}

func byRank(r1, id1, r2, id2 string) bool {
	if r1 != r2 {
		return r1 < r2
	}
	return id1 < id2
}
