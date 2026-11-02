package gitstore

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
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
	Board    BoardFile
	Cards    []board.Card
	Teams    []Team
	Projects []Project
	// Unknown lists paths that are not part of the layout; Broken lists
	// paths that are, but could not be read. Neither fails the load.
	Unknown []string
	Broken  []BrokenFile
	// Aliases are roster names declared in more than one domain: the
	// oldest declaration is the entry, these are the others (their cards
	// still count). Ghosts are cards present in two domains mid-move: the
	// copy that is NOT current. Both are for health, not for the board.
	Aliases []Alias
	Ghosts  []Ghost
	// Users are the primary's users/<login>.yaml files: each person's roster
	// entry. Only the primary's count.
	Users []User
}

// User is one users/<login>.yaml: a person and what the roster records about
// them.
type User struct {
	Login string
	// Capacity is the points a week somebody set for this person; 0 = none.
	Capacity int
	Created  string
}

// Alias is a roster entry that lost the duplicate-name resolution.
type Alias struct {
	Kind   string // team | project
	Name   string
	Domain string
	ID     string
	Winner string // the id of the entry that won
}

// Ghost is the stale copy of a card caught between two domains: Domain is
// where the stale file sits, Current where the served copy is. Current is
// empty when neither copy says it moved — a duplicate, not a torn move, and
// not maintenance's to resolve.
type Ghost struct {
	ID      string
	Domain  string
	Current string
}

// Team is teams/<id>.yaml with its id.
type Team struct {
	ID     string
	Domain string
	TeamFile
}

// Project is a project with its columns and deadline lines.
type Project struct {
	ID     string
	Domain string
	ProjectFile
	Scopes    []Scope
	Deadlines []Deadline
}

// Scope is one column.
type Scope struct {
	ID     string
	Domain string
	ScopeFile
}

// Deadline is one deadline line.
type Deadline struct {
	ID     string
	Domain string
	DeadlineFile
}

// BrokenFile is a layout path whose content could not be read.
type BrokenFile struct {
	Path string
	Err  error
}

// Load reads the snapshot at the branch tip.
// clone copies a snapshot's own slices — every place a reader writes into
// one. LoadAll stamps each entry with its domain's NAME (stamp), so two
// readers sharing one snapshot would stamp over each other; the strings
// themselves are shared, which is what makes this cheap next to decoding
// the tree again.
func (s Snapshot) clone() Snapshot {
	out := s
	out.Cards = append([]board.Card(nil), s.Cards...)
	out.Teams = append([]Team(nil), s.Teams...)
	out.Projects = append([]Project(nil), s.Projects...)
	for i := range out.Projects {
		out.Projects[i].Scopes = append([]Scope(nil), s.Projects[i].Scopes...)
		out.Projects[i].Deadlines = append([]Deadline(nil), s.Projects[i].Deadlines...)
	}
	return out
}

func Load(r *Repo) (Snapshot, error) {
	head := r.Head()
	if head.IsZero() {
		return Snapshot{}, ErrEmptyRepository
	}
	// Memoized by the tip. Reading the tree means decoding every card file
	// on the board, and every setter asks for a snapshot to decide where
	// its card belongs — so one request that wrote fifty cards read the
	// whole board a hundred and fifty times. The commit is immutable: the
	// same hash is the same board, and a write moves the hash.
	if s, ok := r.snapshotAt(head); ok {
		return s, nil
	}
	s, err := LoadAt(r, head)
	if err != nil {
		return s, err
	}
	r.rememberSnapshot(head, s)
	return s, nil
}

// loadOverlaid is Load with an action's staged writes laid over the tip: a
// path the action has written reads as written (nil: gone), every other path
// as committed. It is what a board read answers INSIDE a scope, so the doors
// an action goes through one after another each see the board as the action
// has left it. Read from the committed tree instead, a later door decides on
// a card the action no longer has — Own's SetDates read a team-less card and
// gave it the no-team group's sprint over the team's that SetTeam had just
// staged. The server's cache never had the problem (it applies every write
// at once); the store on its own did. Not memoized: the staged set is the
// action's, not the tip's.
func loadOverlaid(r *Repo, staged map[string][]byte) (Snapshot, error) {
	if len(staged) == 0 {
		return Load(r)
	}
	l := &loader{s: Snapshot{}, projects: map[string]*Project{}}
	if head := r.Head(); !head.IsZero() {
		c, err := object.GetCommit(r.s, head)
		if err != nil {
			return Snapshot{}, err
		}
		tree, err := c.Tree()
		if err != nil {
			return Snapshot{}, err
		}
		err = tree.Files().ForEach(func(f *object.File) error {
			if _, written := staged[f.Name]; written {
				return nil // the action's version, below, or gone
			}
			data, err := readAll(f)
			if err != nil {
				l.s.Broken = append(l.s.Broken, BrokenFile{Path: f.Name, Err: err})
				return nil //nolint:nilerr // recorded as broken, as Load does
			}
			return l.file(f.Name, data)
		})
		if err != nil {
			return Snapshot{}, err
		}
	}
	for _, p := range slices.Sorted(maps.Keys(staged)) {
		if data := staged[p]; data != nil {
			if err := l.file(p, data); err != nil {
				return Snapshot{}, err
			}
		}
	}
	l.finish()
	return l.s, nil
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
	l := &loader{s: Snapshot{}, projects: map[string]*Project{}}
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

// maxBlobBytes caps a card or roster file. These are small YAML/Markdown
// documents (a description is capped at 16k runes upstream), so the ceiling is
// generous; it is what stops one oversized committed blob from being pulled
// whole into memory by every replica's load, as-of read and rebase. A file over
// it is refused rather than read, and the caller records it as broken.
const maxBlobBytes = 4 << 20

// ErrBlobTooLarge is a repository file larger than maxBlobBytes.
var ErrBlobTooLarge = fmt.Errorf("gitstore: file is larger than %d bytes", maxBlobBytes)

func readAll(f *object.File) ([]byte, error) {
	if f.Size > maxBlobBytes {
		return nil, fmt.Errorf("%w (%d)", ErrBlobTooLarge, f.Size)
	}
	rd, err := f.Reader()
	if err != nil {
		return nil, err
	}
	defer rd.Close()
	// The size was checked above, but a lying tree entry would still stream
	// past it; the cap here is the real bound.
	return io.ReadAll(io.LimitReader(rd, maxBlobBytes+1))
}

type loader struct {
	s        Snapshot
	projects map[string]*Project
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
	case PathScope:
		var f ScopeFile
		if f, err = DecodeScope(data); err == nil {
			pr := l.project(ids[0])
			pr.Scopes = append(pr.Scopes, Scope{ID: ids[1], ScopeFile: f})
		}
	case PathDeadline:
		var f DeadlineFile
		if f, err = DecodeDeadline(data); err == nil {
			pr := l.project(ids[0])
			pr.Deadlines = append(pr.Deadlines, Deadline{ID: ids[1], DeadlineFile: f})
		}
	case PathUser:
		var f UserFile
		if f, err = DecodeUser(data); err == nil {
			l.s.Users = append(l.s.Users, User{Login: ids[0], Capacity: f.Capacity, Created: f.Created})
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
		sort.Slice(p.Scopes, func(i, j int) bool { return byRank(p.Scopes[i].Rank, p.Scopes[i].ID, p.Scopes[j].Rank, p.Scopes[j].ID) })
		sort.Slice(p.Deadlines, func(i, j int) bool {
			return byRank(p.Deadlines[i].Week, p.Deadlines[i].ID, p.Deadlines[j].Week, p.Deadlines[j].ID)
		})
		l.s.Projects = append(l.s.Projects, *p)
	}
	sort.Slice(l.s.Projects, func(i, j int) bool {
		return byRank(l.s.Projects[i].Rank, l.s.Projects[i].ID, l.s.Projects[j].Rank, l.s.Projects[j].ID)
	})
}

func byRank(r1, id1, r2, id2 string) bool {
	if r1 != r2 {
		return r1 < r2
	}
	return id1 < id2
}
