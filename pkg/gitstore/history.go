package gitstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/aenix-io/aeman/pkg/board"
)

// History is read from commits: a card's log is the commits that touched
// it, and what each changed is read from Aeman-Change trailers first and
// the front-matter diff second. The walker follows first parents and stops
// AT a shallow boundary — go-git's own Log ignores the shallow list and
// fails on the missing parent, which is why this exists.

// Walk visits commits from `from` back along first parents. fn returns
// false to stop. A shallow boundary is visited and not crossed; a root
// ends the walk.
func (r *Repo) Walk(from plumbing.Hash, fn func(*object.Commit) (bool, error)) error {
	shallow, err := r.shallows()
	if err != nil {
		return err
	}
	for h := from; !h.IsZero(); {
		c, err := object.GetCommit(r.s, h)
		if err != nil {
			return err
		}
		more, err := fn(c)
		if err != nil || !more {
			return err
		}
		if shallow[h] || c.NumParents() == 0 {
			return nil
		}
		h = c.ParentHashes[0]
	}
	return nil
}

func (r *Repo) shallows() (map[plumbing.Hash]bool, error) {
	hs, err := r.s.Shallow()
	if err != nil {
		return nil, err
	}
	m := make(map[plumbing.Hash]bool, len(hs))
	for _, h := range hs {
		m[h] = true
	}
	return m, nil
}

// LogEntry is one commit as seen from one card.
type LogEntry struct {
	Hash     plumbing.Hash
	At       time.Time
	Actor    string
	Action   string
	ActionID string
	Summary  string
	// Changes: the card's Aeman-Change trailers first, then what the
	// front-matter diff says, then "description"/"note" for body edits. A
	// commit that brings the file into being is one "created" change, one
	// that removes it one "deleted".
	Changes []Change
	// MovedFrom names the domain the card came from when this commit is the
	// destination half of a move (G22): the log continues there.
	MovedFrom string
}

// Log is a card's history within the loaded horizon.
type Log struct {
	Entries []LogEntry
	// TruncatedBefore is the boundary's time when the history is cut by a
	// shallow clone; zero when the whole history is present.
	TruncatedBefore time.Time
}

// CardLog lists the commits that touched the card, newest first. limit
// caps the list (0 = all); it never hides that the history is truncated.
func (r *Repo) CardLog(id string, limit int) (Log, error) {
	p, err := CardPath(id)
	if err != nil {
		return Log{}, err
	}
	var log Log
	if log.TruncatedBefore, err = r.horizon(); err != nil {
		return Log{}, err
	}
	shallow, err := r.shallows()
	if err != nil {
		return Log{}, err
	}
	err = r.Walk(r.Head(), func(c *object.Commit) (bool, error) {
		entry, touched, err := r.entryFor(c, id, p, shallow[c.Hash])
		if err != nil {
			return false, err
		}
		if touched {
			log.Entries = append(log.Entries, entry)
		}
		return limit <= 0 || len(log.Entries) < limit, nil
	})
	return log, err
}

// horizon is the earliest time still in the clone: the oldest shallow
// boundary's author time, or zero when nothing is cut.
func (r *Repo) horizon() (time.Time, error) {
	hs, err := r.s.Shallow()
	if err != nil {
		return time.Time{}, err
	}
	var t time.Time
	for _, h := range hs {
		c, err := object.GetCommit(r.s, h)
		if err != nil {
			continue
		}
		if t.IsZero() || c.Author.When.Before(t) {
			t = c.Author.When
		}
	}
	return t, nil
}

// entryFor decides whether commit c touched the card and, if so, builds its
// entry. With a parent at hand the file's blobs are compared; a root is
// compared against nothing; at a shallow boundary (no parent to read) the
// trailers decide, and failing those, the file's presence.
func (r *Repo) entryFor(c *object.Commit, id, p string, boundary bool) (LogEntry, bool, error) {
	tr := ParseTrailers(c.Message)
	movedIn, movedOut := tr.Extra["Aeman-Moved-From"], tr.Extra["Aeman-Moved-To"]
	after, err := blobAt(c, p)
	if err != nil {
		return LogEntry{}, false, err
	}
	var before []byte
	switch {
	case boundary || c.NumParents() == 0:
		named := len(tr.Cards) > 0
		switch {
		case boundary && ((named && !contains(tr.Cards, id)) || (!named && after == nil)):
			return LogEntry{}, false, nil
		case !boundary && after == nil && !contains(tr.Cards, id):
			// A root commit (an import) is in a card's log only when the
			// card is in its tree or its trailers name it — a card created
			// later never inherits the import as its first event.
			return LogEntry{}, false, nil
		}
	default:
		parent, err := c.Parent(0)
		if err != nil {
			return LogEntry{}, false, err
		}
		if before, err = blobAt(parent, p); err != nil {
			return LogEntry{}, false, err
		}
		if bytes.Equal(before, after) && !contains(tr.Cards, id) {
			return LogEntry{}, false, nil
		}
	}
	if movedOut != "" && after == nil && contains(tr.Cards, id) {
		// The source half of a move: the card went on, its feed with it.
		return LogEntry{}, false, nil
	}
	e := LogEntry{Hash: c.Hash, At: c.Author.When, Actor: tr.Actor, Action: tr.Action, ActionID: tr.ActionID,
		Summary: firstLineOf(c.Message), MovedFrom: movedIn}
	covered := map[string]bool{}
	for _, ch := range tr.Changes {
		if ch.Card == id {
			e.Changes = append(e.Changes, ch)
			covered[ch.Kind] = true
		}
	}
	switch {
	case boundary, movedIn != "":
		// No parent to diff against, or a file that arrived whole from
		// another domain: the trailers say what changed, nothing else.
	case before == nil && after != nil:
		// The file appearing IS the creation; a trailer saying so too (the
		// service records the event) is the same fact, not a second one.
		if !covered["created"] {
			e.Changes = append(e.Changes, Change{Card: id, Kind: "created"})
		}
	case before != nil && after == nil:
		if !covered["deleted"] {
			e.Changes = append(e.Changes, Change{Card: id, Kind: "deleted"})
		}
	default:
		for _, ch := range diffCard(id, before, after) {
			if !covered[ch.Kind] {
				e.Changes = append(e.Changes, ch)
			}
		}
	}
	return e, true, nil
}

// blobAt returns the file's bytes in the commit's tree, or nil if absent.
func blobAt(c *object.Commit, p string) ([]byte, error) {
	t, err := c.Tree()
	if err != nil {
		return nil, err
	}
	f, err := t.File(p)
	if err != nil {
		if errors.Is(err, object.ErrFileNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return readAll(f)
}

// diffCard compares two versions of a card file field by field. From/To
// are the front-matter scalars as written; a body edit is one "description"
// or "note" change with no payload.
func diffCard(id string, before, after []byte) []Change {
	var a, b CardFile
	if before != nil {
		a, _ = DecodeCard(id, before)
	}
	if after != nil {
		b, _ = DecodeCard(id, after)
	}
	fa, fb := frontFields(before), frontFields(after)
	var out []Change
	seen := map[string]bool{}
	for _, k := range orderedKeys(before, after) {
		if seen[k] || fa[k] == fb[k] {
			continue
		}
		seen[k] = true
		out = append(out, Change{Card: id, Kind: k, From: fa[k], To: fb[k]})
	}
	if a.Card.Description != b.Card.Description {
		out = append(out, Change{Card: id, Kind: "description"})
	}
	if len(a.Card.Notes) != len(b.Card.Notes) || notesText(a.Card.Notes) != notesText(b.Card.Notes) {
		out = append(out, Change{Card: id, Kind: "note"})
	}
	return out
}

// frontFields flattens a card file's front-matter to key → written text.
func frontFields(data []byte) map[string]string {
	out := map[string]string{}
	for _, kv := range frontLines(data) {
		out[kv[0]] = kv[1]
	}
	return out
}

// orderedKeys is the union of both versions' keys, in the order they
// appear (after's order first, then anything only before had).
func orderedKeys(before, after []byte) []string {
	var keys []string
	seen := map[string]bool{}
	for _, data := range [][]byte{after, before} {
		for _, kv := range frontLines(data) {
			if !seen[kv[0]] {
				seen[kv[0]] = true
				keys = append(keys, kv[0])
			}
		}
	}
	return keys
}

// frontLines reads the "key: value" lines of the front-matter block,
// keeping the value's written form (quotes stripped from a JSON-quoted
// scalar so a diff compares meaning, not punctuation).
func frontLines(data []byte) [][2]string {
	if !bytes.HasPrefix(data, []byte(fence)) {
		return nil
	}
	rest := data[len(fence):]
	end := bytes.Index(rest, []byte("\n"+fence))
	if end < 0 {
		return nil
	}
	var out [][2]string
	for _, line := range strings.Split(string(rest[:end+1]), "\n") {
		key, val, ok := strings.Cut(line, ": ")
		if !ok || strings.HasPrefix(line, " ") {
			continue
		}
		out = append(out, [2]string{key, unquote(val)})
	}
	return out
}

func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		var v string
		if err := json.Unmarshal([]byte(s), &v); err == nil {
			return v
		}
	}
	return s
}

func notesText(ns []board.Note) string {
	var b strings.Builder
	for _, n := range ns {
		b.WriteString(n.ID + "\x00" + n.Body + "\x00" + n.Author + "\x00" + n.CreatedAt + "\x01")
	}
	return b.String()
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
