package gitstore

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"gopkg.in/yaml.v3"

	"github.com/aenix-io/aeman/pkg/board"
)

// A rejected push is re-applied on the remote's new tip (G10/G11). The
// re-application replays the commits the remote has not seen — they are on
// the branch, so a run that stopped between commit and push loses nothing —
// oldest first, each field by field onto the file as it now is. That is the
// contract the queue's closures gave: writers on different fields of one
// card both land, the same field goes to the replayed write, a create whose
// path already exists is a no-op. Replaying commits rather than closures
// also keeps a move — a create in one repository and a delete in another —
// replayable per repository.

// RebaseResult counts what a replay did.
type RebaseResult struct {
	// Replayed commits were recorded again on the new tip. Dropped ones had
	// nothing left to say there: a create whose path exists, an edit of a
	// card the tip deleted, a change the tip already has.
	Replayed, Dropped int
}

// Rebase moves the branch onto a freshly fetched remote tip and replays the
// local commits the remote has not seen, each keeping its message, trailers,
// author and dates. A tip that shares no history with the branch (a
// rewritten remote) is refused and nothing moves; a tip behind the branch
// changes nothing; a tip ahead of it fast-forwards.
func (r *Repo) Rebase(tip plumbing.Hash) (RebaseResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var res RebaseResult
	head := r.Head()
	if head == tip {
		return res, nil
	}
	if head.IsZero() {
		return res, r.resetLocked(tip)
	}
	ours := map[plumbing.Hash]bool{}
	var chain []*object.Commit // newest first
	if err := r.Walk(head, func(c *object.Commit) (bool, error) {
		ours[c.Hash] = true
		chain = append(chain, c)
		return true, nil
	}); err != nil {
		return res, err
	}
	if ours[tip] {
		return res, nil // the remote is behind us: nothing to replay, the push will bring it up
	}
	base := plumbing.ZeroHash
	if err := r.Walk(tip, func(c *object.Commit) (bool, error) {
		if ours[c.Hash] {
			base = c.Hash
			return false, nil
		}
		return true, nil
	}); err != nil {
		return res, err
	}
	if base.IsZero() {
		return res, fmt.Errorf("gitstore: remote tip %s shares no history with %s", tip, r.branch.Short())
	}
	var replay []*object.Commit
	for _, c := range chain {
		if c.Hash == base {
			break
		}
		replay = append(replay, c)
	}
	if err := r.resetLocked(tip); err != nil {
		return res, err
	}
	for i := len(replay) - 1; i >= 0; i-- {
		c := replay[i]
		writes, err := r.replayWrites(c)
		if err != nil {
			return res, err
		}
		h, err := r.commitLocked(c.Message, c.Author, c.Committer, writes, false)
		if err != nil {
			return res, err
		}
		if h.IsZero() {
			res.Dropped++
		} else {
			res.Replayed++
		}
	}
	return res, nil
}

// UnpushedCommits lists the local commits the remote has not seen, oldest
// first: from the tip back to the last known remote tip (within the loaded
// history).
func (r *Repo) UnpushedCommits() ([]*object.Commit, error) {
	remoteTip := r.RemoteTip()
	var out []*object.Commit
	err := r.Walk(r.Head(), func(c *object.Commit) (bool, error) {
		if c.Hash == remoteTip {
			return false, nil
		}
		out = append(out, c)
		return true, nil
	})
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, err
}

// replayWrites turns one commit's changes into writes against the current
// tip: for every path it touched, the field-level merge of what it changed
// onto the file as it now is.
func (r *Repo) replayWrites(c *object.Commit) ([]FileWrite, error) {
	var parentTree *object.Tree
	if len(c.ParentHashes) > 0 {
		pc, err := object.GetCommit(r.s, c.ParentHashes[0])
		if err != nil {
			return nil, err
		}
		if parentTree, err = pc.Tree(); err != nil {
			return nil, err
		}
	}
	ourTree, err := c.Tree()
	if err != nil {
		return nil, err
	}
	changes, err := object.DiffTree(parentTree, ourTree)
	if err != nil {
		return nil, err
	}
	headTree, err := r.treeOf(r.Head())
	if err != nil {
		return nil, err
	}
	var writes []FileWrite
	for _, ch := range changes {
		p := ch.To.Name
		if p == "" {
			p = ch.From.Name
		}
		base, err := blobOf(parentTree, p)
		if err != nil {
			return nil, err
		}
		ours, err := blobOf(ourTree, p)
		if err != nil {
			return nil, err
		}
		theirs, err := blobOf(headTree, p)
		if err != nil {
			return nil, err
		}
		if merged, ok := mergeFile(p, base, ours, theirs); ok {
			writes = append(writes, FileWrite{Path: p, Data: merged})
		}
	}
	return writes, nil
}

// blobOf is a file's content in a tree; nil when the tree is nil or the
// file is not there.
func blobOf(t *object.Tree, p string) ([]byte, error) {
	if t == nil {
		return nil, nil
	}
	f, err := t.File(p)
	if err != nil {
		if errors.Is(err, object.ErrFileNotFound) {
			return nil, nil
		}
		return nil, err
	}
	rd, err := f.Reader()
	if err != nil {
		return nil, err
	}
	defer rd.Close()
	return io.ReadAll(rd)
}

// mergeFile decides what a replayed change to one file becomes on the
// current tip. ok is false when there is nothing to write: the change is
// already there, or the tip has made it moot.
//
//	ours deleted            → delete (no-op if already gone)
//	we created, tip has it  → no-op: a create is idempotent by path (G11)
//	we created, tip has not → our file
//	tip deleted, we edited  → no-op: the delete stands
//	both edited             → field-level: our changed fields onto theirs
func mergeFile(p string, base, ours, theirs []byte) ([]byte, bool) {
	switch {
	case ours == nil:
		return nil, theirs != nil
	case theirs == nil:
		return ours, base == nil
	case base == nil:
		return nil, false
	}
	merged, same, mergeable := mergeByKind(p, base, ours, theirs)
	if !mergeable {
		// Not a file this package can read field by field: our version, whole.
		return ours, !bytes.Equal(ours, theirs)
	}
	return merged, !same
}

// mergeByKind applies our changed fields onto theirs for a file of the
// layout and re-encodes canonically; same reports that the result is what
// the tip already holds (compared canonically, so formatting alone never
// makes a commit). mergeable is false for a file that is not of the layout
// or does not parse.
func mergeByKind(p string, base, ours, theirs []byte) (merged []byte, same, mergeable bool) {
	kind, ids := ParsePath(p)
	switch kind {
	case PathCard, PathTask:
		id := ids[len(ids)-1]
		return mergeTyped(base, ours, theirs, func(d []byte) (CardFile, error) { return DecodeCard(id, d) }, EncodeCard)
	case PathTeam:
		return mergeTyped(base, ours, theirs, DecodeTeam, EncodeTeam)
	case PathProject:
		return mergeTyped(base, ours, theirs, DecodeProject, EncodeProject)
	case PathEpic:
		return mergeTyped(base, ours, theirs, DecodeEpic, EncodeEpic)
	case PathDeadline:
		return mergeTyped(base, ours, theirs, DecodeDeadline, EncodeDeadline)
	case PathProcess:
		return mergeTyped(base, ours, theirs, DecodeProcess, EncodeProcess)
	case PathBoard:
		return mergeTyped(base, ours, theirs, DecodeBoard, EncodeBoard)
	default:
		return nil, false, false
	}
}

// mergeTyped decodes the three versions, sets our changed fields on theirs
// and encodes the result; same compares it with theirs re-encoded.
func mergeTyped[T any](base, ours, theirs []byte, dec func([]byte) (T, error), enc func(T) ([]byte, error)) (merged []byte, same, mergeable bool) {
	b, err1 := dec(base)
	o, err2 := dec(ours)
	t, err3 := dec(theirs)
	if err1 != nil || err2 != nil || err3 != nil {
		return nil, false, false
	}
	canonical, err := enc(t)
	if err != nil {
		return nil, false, false
	}
	m := t
	mergeStruct(&m, b, o)
	if merged, err = enc(m); err != nil {
		return nil, false, false
	}
	return merged, bytes.Equal(merged, canonical), true
}

// mergeStruct sets on dst every exported field where ours differs from base
// — field-level, last write wins per field. Nested structs (a card file's
// card, a team's sprint pointer) merge field by field too; notes merge by
// id and unknown front-matter keys by key, so both sides' additions survive.
func mergeStruct(dst, base, ours any) {
	dv := reflect.ValueOf(dst).Elem()
	bv := reflect.ValueOf(base)
	ov := reflect.ValueOf(ours)
	for i := 0; i < dv.NumField(); i++ {
		if !dv.Type().Field(i).IsExported() {
			continue
		}
		switch f := dv.Field(i).Addr().Interface().(type) {
		case *[]board.Note:
			*f = mergeNotes(bv.Field(i).Interface().([]board.Note), ov.Field(i).Interface().([]board.Note), *f)
		case *[]ExtraField:
			*f = mergeExtra(bv.Field(i).Interface().([]ExtraField), ov.Field(i).Interface().([]ExtraField), *f)
		default:
			if dv.Field(i).Kind() == reflect.Struct && hasExportedField(dv.Field(i).Type()) {
				mergeStruct(dv.Field(i).Addr().Interface(), bv.Field(i).Interface(), ov.Field(i).Interface())
				continue
			}
			if !reflect.DeepEqual(bv.Field(i).Interface(), ov.Field(i).Interface()) {
				dv.Field(i).Set(ov.Field(i))
			}
		}
	}
}

func hasExportedField(t reflect.Type) bool {
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).IsExported() {
			return true
		}
	}
	return false
}

// mergeNotes applies our note changes onto theirs: notes we added join,
// notes we removed leave, notes we edited take our text; the rest is theirs.
// The result is in id order — ids are ULIDs, so that is time order.
func mergeNotes(base, ours, theirs []board.Note) []board.Note {
	baseBy := map[string]board.Note{}
	for _, n := range base {
		baseBy[n.ID] = n
	}
	oursBy := map[string]board.Note{}
	for _, n := range ours {
		oursBy[n.ID] = n
	}
	out := make([]board.Note, 0, len(theirs)+len(ours))
	seen := map[string]bool{}
	for _, n := range theirs {
		if b, inBase := baseBy[n.ID]; inBase {
			o, kept := oursBy[n.ID]
			if !kept {
				continue
			}
			if o != b {
				n = o
			}
		}
		out = append(out, n)
		seen[n.ID] = true
	}
	for _, n := range ours {
		if _, inBase := baseBy[n.ID]; !inBase && !seen[n.ID] {
			out = append(out, n)
			seen[n.ID] = true
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if len(out) == 0 {
		return nil
	}
	return out
}

// mergeExtra is mergeNotes for unknown front-matter keys.
func mergeExtra(base, ours, theirs []ExtraField) []ExtraField {
	baseBy := map[string]*yaml.Node{}
	for _, f := range base {
		baseBy[f.Key] = f.Value
	}
	oursBy := map[string]*yaml.Node{}
	for _, f := range ours {
		oursBy[f.Key] = f.Value
	}
	out := make([]ExtraField, 0, len(theirs)+len(ours))
	seen := map[string]bool{}
	for _, f := range theirs {
		if b, inBase := baseBy[f.Key]; inBase {
			o, kept := oursBy[f.Key]
			if !kept {
				continue
			}
			if !nodeEqual(o, b) {
				f.Value = o
			}
		}
		out = append(out, f)
		seen[f.Key] = true
	}
	for _, f := range ours {
		if _, inBase := baseBy[f.Key]; !inBase && !seen[f.Key] {
			out = append(out, f)
			seen[f.Key] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func nodeEqual(a, b *yaml.Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	x, err1 := yaml.Marshal(a)
	y, err2 := yaml.Marshal(b)
	return err1 == nil && err2 == nil && bytes.Equal(x, y)
}
