package gitstore

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/aenix-io/aeman/pkg/board"
)

// The schema is a contract between server and repository (G18). A
// repository written by a newer server is refused — DecodeBoard returns
// ErrSchemaNewer — because this server would write states it does not
// understand. One written by an older server is brought to the current
// schema here, in one commit at start-up, so every replica agrees on the
// layout before it serves anything.

// upgrade is one version's rewrite of the tree: the files to write (or
// delete, with nil Data) beside the moved marker to bring a repository that
// stands at version-1 to it. Each is idempotent on its own — a tree that
// already has the shape writes nothing — so a run that moves several
// versions at once is the sum of its steps, and a step re-run finds
// nothing to do.
type upgrade func(tree *object.Tree) ([]FileWrite, error)

// upgrades holds the rewrite each version asks for. Version 1 introduced
// the marker and rewrote nothing; version 2 is the hierarchy work's bump
// (docs/design/hierarchy.md, H8): the Process feature retired, the column
// renamed from epic to scope, card mirrors retired.
var upgrades = map[int]upgrade{
	2: schema2,
}

// MigrateSchema brings board.yaml to SchemaVersion when it is older (or
// missing), in one "schema" commit that also carries every version's
// rewrite of the tree, and reports whether it did. A current repository is
// left alone; a newer one is refused.
func MigrateSchema(r *Repo) (bool, error) {
	var f BoardFile
	data, err := r.ReadFile(BoardPath)
	switch {
	case errors.Is(err, ErrNotFound):
		// A secondary domain seeded by hand: it gets a board file at the
		// current schema and nothing else.
	case err != nil:
		return false, err
	default:
		if f, err = DecodeBoard(data); err != nil {
			return false, err
		}
	}
	if f.Schema >= SchemaVersion {
		return false, nil
	}
	from := f.Schema
	f.Schema = SchemaVersion
	out, err := EncodeBoard(f)
	if err != nil {
		return false, err
	}
	writes := []FileWrite{{Path: BoardPath, Data: out}}
	// The rewrites ride the same commit as the marker: a replica that reads
	// the new marker reads the new shape, never one without the other.
	if head := r.Head(); !head.IsZero() {
		c, err := object.GetCommit(r.s, head)
		if err != nil {
			return false, err
		}
		tree, err := c.Tree()
		if err != nil {
			return false, err
		}
		for v := from + 1; v <= SchemaVersion; v++ {
			step, ok := upgrades[v]
			if !ok {
				continue
			}
			more, err := step(tree)
			if err != nil {
				return false, fmt.Errorf("gitstore: schema %d: %w", v, err)
			}
			writes = append(writes, more...)
		}
	}
	_, err = r.Commit(Action{Name: "schema", Summary: fmt.Sprintf("schema: %d → %d", from, SchemaVersion), At: time.Now()},
		writes)
	if err != nil {
		return false, err
	}
	return true, nil
}

// Schema 2 is one bump with several steps, specified in order in
// docs/design/hierarchy.md ("Migration"):
//
//  1. the processes/ tree goes (the Process feature retired);
//  2. `process`, `task` and `accumulate` come off every card — a filed turn
//     is ORPHANED, a card of its week and its zone and nothing more;
//  3. the column stubs move from projects/<id>/epics/ to projects/<id>/scopes/,
//     content untouched, and the card key `epic` is written as `scope`;
//  4. `mirrors` comes off every card (card mirrors retired);
//  5. a card in a column is a PROJECT CARD and stands there alone (H4):
//     `team`, `assignees` and `parent` come off it, and a card grouped UNDER
//     one is ungrouped — a project card has no subtasks either.
//
// The steps that rewrite a card COMPOSE: one pass decides per file whether
// any of them applies and writes the file once with all of them applied —
// independent passes would each produce a FileWrite for the same path and
// the last would win, undoing the others. A card none of the steps touch is
// not rewritten, so the commit touches exactly what the retired features,
// the rename and the project-card rule touched. Step 5 needs to know which
// cards stand in a column before it reads the rest, so the tree is walked
// twice: once for the column cards' ids, once for the writes.

// retiredCardKeys are the front-matter keys schema 2 takes off a card: the
// Process feature's (steps 2) and the mirrors list (step 4). The codec no
// longer knows them, so on read they sit in Extra, which is where this pass
// finds them.
var retiredCardKeys = map[string]bool{"process": true, "task": true, "accumulate": true, "mirrors": true}

// renamedCardKeys are the keys the codec reads under a NEW name: a file that
// still spells the old one is rewritten so the current spelling is on disk
// (step 3). The alias in setKnown is what makes the rewrite a plain
// decode-and-encode.
var renamedCardKeys = map[string]bool{"epic": true}

// retiredProcessesDir is the directory the Process feature kept its
// processes and tasks in; the whole tree goes (step 1).
const retiredProcessesDir = "processes/"

// oldColumnDir is the directory segment the column stubs used to live under
// (step 3): projects/<id>/epics/<id>.yaml → projects/<id>/scopes/<id>.yaml.
const oldColumnDir = "/epics/"

// columnCardKeys are the keys that put a card in a column, under either
// spelling (step 5 runs on a tree that may still say `epic`).
var columnCardKeys = map[string]bool{"scope": true, "epic": true}

// projectCardCannotCarryKeys are the keys step 5 takes off a card in a
// column: a project card has no team, no owner, no parent, no band and no
// size (H4).
var projectCardCannotCarryKeys = map[string]bool{"team": true, "assignees": true, "parent": true, "zone": true, "size": true, "sprint": true, "week": true}

// cardsInColumns lists the ids of the cards that stand in a column, read off
// their front-matter keys alone — what step 5 needs to recognise a card
// grouped under one of them.
func cardsInColumns(tree *object.Tree) (map[string]bool, error) {
	in := map[string]bool{}
	err := tree.Files().ForEach(func(f *object.File) error {
		kind, ids := ParsePath(f.Name)
		if kind != PathCard {
			return nil
		}
		data, err := readAll(f)
		if err != nil {
			return fmt.Errorf("%s: %w", f.Name, err)
		}
		keys, err := frontKeys(data)
		if err != nil {
			return nil //nolint:nilerr // a broken card is left as every reader leaves it
		}
		for _, k := range keys {
			if columnCardKeys[k] {
				in[ids[0]] = true
				break
			}
		}
		return nil
	})
	return in, err
}

// schema2 walks the tree and emits every write the bump asks for.
func schema2(tree *object.Tree) ([]FileWrite, error) {
	inColumn, err := cardsInColumns(tree)
	if err != nil {
		return nil, err
	}
	var writes []FileWrite
	err = tree.Files().ForEach(func(f *object.File) error {
		switch {
		case strings.HasPrefix(f.Name, retiredProcessesDir):
			writes = append(writes, FileWrite{Path: f.Name})
			return nil
		case strings.HasPrefix(f.Name, "projects/") && strings.Contains(f.Name, oldColumnDir):
			// The stub moves: same bytes under the new path, the old path
			// deleted — in one commit, so no tree ever has both or neither.
			data, err := readAll(f)
			if err != nil {
				return fmt.Errorf("%s: %w", f.Name, err)
			}
			writes = append(writes,
				FileWrite{Path: strings.Replace(f.Name, oldColumnDir, "/scopes/", 1), Data: data},
				FileWrite{Path: f.Name})
			return nil
		}
		kind, ids := ParsePath(f.Name)
		if kind != PathCard {
			return nil
		}
		data, err := readAll(f)
		if err != nil {
			return fmt.Errorf("%s: %w", f.Name, err)
		}
		keys, err := frontKeys(data)
		if err != nil {
			// A card the loader already reports as broken is not this
			// step's to mend; it is left as it is, as every reader leaves it.
			return nil //nolint:nilerr // see above
		}
		// Which steps this file MIGHT need, read off its keys: the retired
		// and renamed keys (steps 2–4), a team, an owner or a parent on a
		// card in a column (step 5), and a parent on any card — which is
		// step 5 too when the parent stands in a column, known only once the
		// card is read.
		touched, hasParent := false, false
		for _, k := range keys {
			switch {
			case retiredCardKeys[k] || renamedCardKeys[k]:
				touched = true
			case inColumn[ids[0]] && projectCardCannotCarryKeys[k]:
				touched = true
			case k == "parent":
				hasParent = true
			}
		}
		if !touched && !hasParent {
			return nil
		}
		cf, err := DecodeCard(ids[0], data)
		if err != nil {
			return nil //nolint:nilerr // see above
		}
		switch {
		case cf.Card.Scope != "":
			cf.Card.Team, cf.Card.Assignees, cf.Card.Parent = "", nil, ""
			cf.Card.Zone, cf.Card.Size = "", ""
			// And no sprint, no week: the runtime door that files a card into
			// a column clears both, and a sprint on a card no day board draws
			// is one a carry-over would move for nothing. A card whose week
			// was its only date takes that week as its dates first — the
			// Project board draws a card by its dates, and without them it
			// would stand on no board at all.
			if start, day, ok := board.WeekAsDates(cf.Card); ok {
				cf.Card.StartDate, cf.Card.Day = start, day
			}
			cf.Card.SprintStart, cf.Card.Week = "", ""
		case inColumn[cf.Card.Parent]:
			cf.Card.Parent = ""
			touched = true
		}
		if !touched {
			return nil // a parent that stands in no column: nothing to do here
		}
		kept := cf.Extra[:0:0]
		for _, x := range cf.Extra {
			if !retiredCardKeys[x.Key] {
				kept = append(kept, x)
			}
		}
		cf.Extra = kept
		out, err := EncodeCard(cf)
		if err != nil {
			return fmt.Errorf("%s: %w", f.Name, err)
		}
		writes = append(writes, FileWrite{Path: f.Name, Data: out})
		return nil
	})
	return writes, err
}
