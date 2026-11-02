package gitstore

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing/object"
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
// the marker and rewrote nothing; version 2 retired the Process feature
// (docs/design/hierarchy.md, H8).
var upgrades = map[int]upgrade{
	2: retireProcesses,
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

// retiredCardKeys are the front-matter keys the Process feature wrote on a
// card and schema 2 takes off it: the link to the task a turn was copied
// from, the process a card was tied to, and the pile-up flag. A turn so
// stripped is ORPHANED — a card of its week and its zone, and nothing more.
var retiredCardKeys = map[string]bool{"process": true, "task": true, "accumulate": true}

// retiredProcessesDir is the directory the feature kept its processes and
// tasks in; the whole tree goes.
const retiredProcessesDir = "processes/"

// retireProcesses is schema 2: delete the processes/ tree and strip the
// retired keys from every card that carries one. A card without them is not
// rewritten, so the commit touches exactly what the feature touched.
func retireProcesses(tree *object.Tree) ([]FileWrite, error) {
	var writes []FileWrite
	err := tree.Files().ForEach(func(f *object.File) error {
		if strings.HasPrefix(f.Name, retiredProcessesDir) {
			writes = append(writes, FileWrite{Path: f.Name})
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
		cf, err := DecodeCard(ids[0], data)
		if err != nil {
			// A card the loader already reports as broken is not this
			// step's to mend; it is left as it is, as every reader leaves it.
			return nil //nolint:nilerr // see above
		}
		kept := cf.Extra[:0:0]
		stripped := false
		for _, x := range cf.Extra {
			if retiredCardKeys[x.Key] {
				stripped = true
				continue
			}
			kept = append(kept, x)
		}
		if !stripped {
			return nil
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
