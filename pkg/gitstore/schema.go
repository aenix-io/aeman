package gitstore

import (
	"errors"
	"fmt"
	"time"
)

// The schema is a contract between server and repository (G18). A
// repository written by a newer server is refused — DecodeBoard returns
// ErrSchemaNewer — because this server would write states it does not
// understand. One written by an older server is brought to the current
// schema here, in one commit at start-up, so every replica agrees on the
// layout before it serves anything.

// MigrateSchema brings board.yaml to SchemaVersion when it is older (or
// missing), in one "schema" commit, and reports whether it did. A current
// repository is left alone; a newer one is refused.
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
	// Per-version upgrades of other files go here as the schema grows;
	// version 1 has nothing to rewrite but the marker.
	out, err := EncodeBoard(f)
	if err != nil {
		return false, err
	}
	_, err = r.Commit(Action{Name: "schema", Summary: fmt.Sprintf("schema: %d → %d", from, SchemaVersion), At: time.Now()},
		[]FileWrite{{Path: BoardPath, Data: out}})
	if err != nil {
		return false, err
	}
	return true, nil
}
