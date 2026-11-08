package gitstore

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/aenix-io/aeman/pkg/board"
)

// SCHEMA 4 is the ordering model (docs/design/ordering.md, Q14). Unlike the
// earlier bumps it needs the MERGED board — a backlog mixes the cards of
// every repository — so it is not a per-repository step: MigrateSchema
// brings each repository to 3, and MigrateOrdering, run once every
// repository is open, re-keys the board and moves every marker to 4, one
// commit per repository, marker and rank lines together.
//
// Resumable: the plan is computed from the PRE-bump trees — for a repository
// a previous, interrupted run already moved to 4, the parent of its schema
// commit — so a resumed run plans exactly what the first one did and writes
// only the repositories still at 3.

// lastPerRepoSchema is the newest schema a single repository can be brought
// to on its own; SchemaVersion is reached by MigrateOrdering.
const lastPerRepoSchema = 3

// schema4Summary names the bump's commits.
const schema4Summary = "schema: 3 → 4 (ordering)"

// OrderingReport is what the schema-4 migration did — or, on a dry run,
// would do.
type OrderingReport struct {
	// Migrated is whether any repository was (or would be) bumped.
	Migrated bool
	// Files is the card files re-keyed, by repository.
	Files map[string]int
	// Cells are the Team cells whose in-work order changes (Appendix D):
	// the one order one key per card cannot always keep.
	Cells []board.CellChange
}

// Total is the files re-keyed in every repository.
func (r OrderingReport) Total() int {
	n := 0
	for _, f := range r.Files {
		n += f
	}
	return n
}

// String is the report as `aeman ordering-report` prints it.
func (r OrderingReport) String() string {
	var b strings.Builder
	if !r.Migrated {
		b.WriteString("every repository is at schema 4: nothing to migrate\n")
		return b.String()
	}
	fmt.Fprintf(&b, "files re-keyed: %d\n", r.Total())
	for name, n := range r.Files {
		fmt.Fprintf(&b, "  %s: %d\n", name, n)
	}
	fmt.Fprintf(&b, "Team cells whose in-work order changes: %d\n", len(r.Cells))
	for _, c := range r.Cells {
		team, person := c.Team, c.Person
		if team == "" {
			team = "(no team)"
		}
		if person == "" {
			person = "(unassigned)"
		}
		fmt.Fprintf(&b, "  %s / %s / %s\n    before: %s\n    after:  %s\n", team, person, c.Zone,
			strings.Join(c.Before, " "), strings.Join(c.After, " "))
	}
	return b.String()
}

// MigrateOrdering brings every repository of a board to schema 4. dryRun
// computes the report and writes nothing. A board already at 4 everywhere is
// left alone (Migrated false).
func MigrateOrdering(domains []Domain, dryRun bool) (OrderingReport, error) {
	report := OrderingReport{Files: map[string]int{}}
	if len(domains) == 0 {
		return report, ErrNoDomains
	}
	pre := make([]plumbing.Hash, len(domains))
	todo := make([]bool, len(domains))
	pending := false
	for i, d := range domains {
		head := d.Repo.Head()
		if head.IsZero() {
			continue
		}
		schema, err := schemaAt(d.Repo, head)
		if err != nil {
			return report, fmt.Errorf("%s: %w", d.Name, err)
		}
		switch {
		case schema < lastPerRepoSchema:
			return report, fmt.Errorf("%s: schema %d: bring it to %d first", d.Name, schema, lastPerRepoSchema)
		case schema == lastPerRepoSchema:
			pre[i], todo[i], pending = head, true, true
		default:
			pre[i] = preBump(d.Repo, head)
		}
	}
	if !pending {
		return report, nil
	}
	report.Migrated = true
	var parts []Snapshot
	for i, d := range domains {
		if pre[i].IsZero() {
			continue
		}
		s, err := LoadAt(d.Repo, pre[i])
		if err != nil {
			return report, fmt.Errorf("%s: %w", d.Name, err)
		}
		stamp(&s, d.Name)
		if i > 0 {
			s.Board = BoardFile{}
		}
		parts = append(parts, s)
	}
	b := boardFromSnapshotIn(domains[0].Name, merge(parts))
	changes, cells, err := board.Schema4Plan(b)
	if err != nil {
		return report, err
	}
	report.Cells = cells
	domainOf := map[string]string{}
	for _, c := range b.Cards {
		domainOf[c.ItemID] = c.Domain
	}
	byDomain := map[string][]board.RankChange{}
	for _, ch := range changes {
		byDomain[domainOf[ch.ID]] = append(byDomain[domainOf[ch.ID]], ch)
	}
	for i, d := range domains {
		if !todo[i] {
			continue
		}
		report.Files[d.Name] = len(byDomain[d.Name])
		if dryRun {
			continue
		}
		if err := commitSchema4(d, byDomain[d.Name]); err != nil {
			return report, fmt.Errorf("%s: %w", d.Name, err)
		}
	}
	return report, nil
}

// commitSchema4 writes one repository's half: the marker and the rank line
// of every card the plan re-keys there, in one commit.
func commitSchema4(d Domain, changes []board.RankChange) error {
	data, err := d.Repo.ReadFile(BoardPath)
	var f BoardFile
	switch {
	case errors.Is(err, ErrNotFound):
	case err != nil:
		return err
	default:
		if f, err = DecodeBoard(data); err != nil {
			return err
		}
	}
	f.Schema = SchemaVersion
	marker, err := EncodeBoard(f)
	if err != nil {
		return err
	}
	writes := []FileWrite{{Path: BoardPath, Data: marker}}
	ids := make([]string, 0, len(changes))
	for _, ch := range changes {
		p, err := CardPath(ch.ID)
		if err != nil {
			return err
		}
		data, err := d.Repo.ReadFile(p)
		if err != nil {
			continue // gone since the plan's tree: nothing to re-key
		}
		cf, err := DecodeCard(ch.ID, data)
		if err != nil || cf.ProjectCard {
			continue
		}
		cf.Card.Rank = ch.After
		out, err := EncodeCard(cf)
		if err != nil {
			return err
		}
		writes = append(writes, FileWrite{Path: p, Data: out})
		ids = append(ids, ch.ID)
	}
	_, err = d.Repo.Commit(Action{Name: "schema", Summary: schema4Summary, At: time.Now(), Cards: ids}, writes)
	return err
}

// schemaAt is the board file's schema at a commit; a repository with no
// board file (a secondary seeded by hand) reads as the per-repository
// schema, which MigrateSchema writes into it.
func schemaAt(r *Repo, h plumbing.Hash) (int, error) {
	c, err := object.GetCommit(r.s, h)
	if err != nil {
		return 0, err
	}
	data, err := blobAt(c, BoardPath)
	if err != nil {
		return 0, err
	}
	if data == nil {
		return lastPerRepoSchema, nil
	}
	f, err := DecodeBoard(data)
	if err != nil {
		return 0, err
	}
	return f.Schema, nil
}

// preBump is the commit a repository's schema-4 bump was made on: the
// parent of its schema commit, or — when the history no longer reaches it —
// the head (its keys are the migrated ones, which plan to nothing new).
func preBump(r *Repo, head plumbing.Hash) plumbing.Hash {
	found := head
	_ = r.Walk(head, func(c *object.Commit) (bool, error) {
		if strings.HasPrefix(c.Message, schema4Summary) && c.NumParents() > 0 {
			found = c.ParentHashes[0]
			return false, nil
		}
		return true, nil
	})
	return found
}
