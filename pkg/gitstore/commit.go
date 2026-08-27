package gitstore

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage"
)

// One action, one commit. An action is what an HTTP request or MCP call did
// — Carry Over over twelve cards, a rename, a slider's final value — and the
// commit it becomes touches every file it changed, names the actor as author,
// the server as committer, and carries the machine-readable trailers the
// activity log is read from. Everything goes through the object store —
// blob, the trees along each path, commit — never through a worktree.

// Identity is a name/email pair for author or committer.
type Identity struct {
	Name  string
	Email string
}

// Options configures a Repo.
type Options struct {
	// Committer is the server's identity; it is also the author of
	// unattributed actions (the sweep, an import).
	Committer Identity
	// AuthorEmail renders an actor's login as an email; nil gives
	// <login>@aeman.
	AuthorEmail func(login string) string
	// Branch is the ref commits land on; empty means refs/heads/main.
	Branch plumbing.ReferenceName
}

// Repo is one domain's repository: a go-git object store and the branch the
// board lives on. Writes are serialised — one writer per repository.
type Repo struct {
	s      storage.Storer
	opts   Options
	branch plumbing.ReferenceName
	mu     sync.Mutex
}

// Action is what one commit records.
type Action struct {
	// Name is the action kind — today's event kinds plus the actions that
	// never had one (carry-over, rename-epic, import, sweep, …).
	Name string
	// ID ties the commits of one action together across domains; empty
	// for single-domain actions that need no correlation.
	ID string
	// Actor is the login; empty means the server acted on its own.
	Actor string
	// Summary is the human first line of the message.
	Summary string
	// At is the action time — the commit date. Zero means now.
	At time.Time
	// Cards are the ids the action touched.
	Cards []string
	// Changes carry payloads that are not field diffs.
	Changes []Change
	// Trailers are extra "Aeman-Key: value" lines a caller adds — the
	// migration marks its reconcile commit this way. Keys are written
	// verbatim and must start with "Aeman-".
	Trailers map[string]string
	// AllowEmpty makes a commit even when no file changed: an annotation —
	// the migration records an event whose payload is not a field. Live
	// actions never set it; a no-op stays a no-op.
	AllowEmpty bool
}

// Change is one Aeman-Change trailer: a change on a card whose from/to
// cannot be read from the file's diff.
type Change struct {
	Card string
	Kind string
	From string
	To   string
}

// FileWrite is one file's new content; nil Data deletes the file.
type FileWrite struct {
	Path string
	Data []byte
}

// Init creates the repository in an empty storer.
func Init(s storage.Storer, opts Options) (*Repo, error) {
	if _, err := git.Init(s, nil); err != nil {
		return nil, fmt.Errorf("gitstore: init: %w", err)
	}
	r := wrap(s, opts)
	if err := s.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, r.branch)); err != nil {
		return nil, err
	}
	return r, nil
}

// Open wraps an existing repository.
func Open(s storage.Storer, opts Options) *Repo {
	return wrap(s, opts)
}

func wrap(s storage.Storer, opts Options) *Repo {
	if opts.Branch == "" {
		opts.Branch = plumbing.NewBranchReferenceName("main")
	}
	if opts.AuthorEmail == nil {
		opts.AuthorEmail = func(login string) string { return login + "@aeman" }
	}
	return &Repo{s: s, opts: opts, branch: opts.Branch}
}

// Storer exposes the underlying object store (for sync and tests).
func (r *Repo) Storer() storage.Storer { return r.s }

// Branch is the ref the board lives on.
func (r *Repo) Branch() plumbing.ReferenceName { return r.branch }

// Head is the branch tip, or the zero hash when the branch is unborn.
func (r *Repo) Head() plumbing.Hash {
	ref, err := r.s.Reference(r.branch)
	if err != nil {
		return plumbing.ZeroHash
	}
	return ref.Hash()
}

// CommitObject reads a commit.
func (r *Repo) CommitObject(h plumbing.Hash) (*object.Commit, error) {
	return object.GetCommit(r.s, h)
}

// Commit applies the writes on top of the branch tip and records them as
// one commit. It returns the zero hash — and makes no commit — when the
// writes change nothing: the same bytes again, or a delete of a file that
// is not there.
func (r *Repo) Commit(a Action, writes []FileWrite) (plumbing.Hash, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	head := r.Head()
	root, err := r.loadRoot(head)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	changed := false
	for _, w := range writes {
		ch, err := r.apply(root, w)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		changed = changed || ch
	}
	if !changed && !a.AllowEmpty {
		return plumbing.ZeroHash, nil
	}
	treeHash, err := r.writeTree(root)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	if !changed && head.IsZero() {
		return plumbing.ZeroHash, errors.New("gitstore: an empty commit needs a parent")
	}
	when := a.At
	if when.IsZero() {
		when = time.Now()
	}
	author := object.Signature{Name: r.opts.Committer.Name, Email: r.opts.Committer.Email, When: when}
	if a.Actor != "" {
		author = object.Signature{Name: a.Actor, Email: r.opts.AuthorEmail(a.Actor), When: when}
	}
	c := &object.Commit{
		Author:    author,
		Committer: object.Signature{Name: r.opts.Committer.Name, Email: r.opts.Committer.Email, When: when},
		Message:   a.message(),
		TreeHash:  treeHash,
	}
	if !head.IsZero() {
		c.ParentHashes = []plumbing.Hash{head}
	}
	o := r.s.NewEncodedObject()
	if err := c.Encode(o); err != nil {
		return plumbing.ZeroHash, err
	}
	h, err := r.s.SetEncodedObject(o)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	if err := r.s.SetReference(plumbing.NewHashReference(r.branch, h)); err != nil {
		return plumbing.ZeroHash, err
	}
	return h, nil
}

// message renders the summary and the trailer block.
func (a Action) message() string {
	var b strings.Builder
	b.WriteString(a.Summary)
	b.WriteString("\n\nAeman-Action: " + a.Name + "\n")
	if a.ID != "" {
		b.WriteString("Aeman-Action-Id: " + a.ID + "\n")
	}
	if a.Actor != "" {
		b.WriteString("Aeman-Actor: " + a.Actor + "\n")
	}
	if len(a.Cards) > 0 {
		b.WriteString("Aeman-Cards: " + strings.Join(a.Cards, " ") + "\n")
	}
	for _, ch := range a.Changes {
		b.WriteString("Aeman-Change: " + ch.Card + " " + ch.Kind + " " + dash(ch.From) + " " + dash(ch.To) + "\n")
	}
	keys := make([]string, 0, len(a.Trailers))
	for k := range a.Trailers {
		keys = append(keys, k)
	}
	sort.Strings(keys) // a stable order keeps identical actions byte-identical
	for _, k := range keys {
		b.WriteString(k + ": " + a.Trailers[k] + "\n")
	}
	return b.String()
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// Trailers is the machine-readable part of a commit message.
type Trailers struct {
	Action   string
	ActionID string
	Actor    string
	Cards    []string
	Changes  []Change
}

// ParseTrailers reads the Aeman-* trailer block: the run of trailer lines
// at the end of the message. Anything before it is prose and is ignored.
func ParseTrailers(msg string) Trailers {
	lines := strings.Split(strings.TrimRight(msg, "\n"), "\n")
	var tr Trailers
	start := len(lines)
	for start > 0 && strings.HasPrefix(lines[start-1], "Aeman-") && strings.Contains(lines[start-1], ": ") {
		start--
	}
	for _, line := range lines[start:] {
		key, val, _ := strings.Cut(line, ": ")
		switch key {
		case "Aeman-Action":
			tr.Action = val
		case "Aeman-Action-Id":
			tr.ActionID = val
		case "Aeman-Actor":
			tr.Actor = val
		case "Aeman-Cards":
			tr.Cards = strings.Fields(val)
		case "Aeman-Change":
			f := strings.Fields(val)
			if len(f) == 4 {
				tr.Changes = append(tr.Changes, Change{Card: f[0], Kind: f[1], From: undash(f[2]), To: undash(f[3])})
			}
		}
	}
	return tr
}

func undash(s string) string {
	if s == "-" {
		return ""
	}
	return s
}

// ---- tree editing -------------------------------------------------------------

// dir is a directory being edited: its entries, with subdirectories loaded
// only along the paths that change. dirty marks a directory whose tree
// object must be rewritten.
type dir struct {
	entries map[string]*entry
	dirty   bool
	loaded  bool
	hash    plumbing.Hash // the tree object this dir came from, if any
}

type entry struct {
	mode filemode.FileMode
	hash plumbing.Hash
	sub  *dir // non-nil once a subdirectory has been loaded for editing
}

func (r *Repo) loadRoot(head plumbing.Hash) (*dir, error) {
	if head.IsZero() {
		return &dir{entries: map[string]*entry{}, loaded: true}, nil
	}
	c, err := object.GetCommit(r.s, head)
	if err != nil {
		return nil, err
	}
	d := &dir{hash: c.TreeHash}
	return d, r.load(d)
}

// load reads a directory's entries from its tree object.
func (r *Repo) load(d *dir) error {
	if d.loaded {
		return nil
	}
	d.entries = map[string]*entry{}
	d.loaded = true
	if d.hash.IsZero() {
		return nil
	}
	t, err := object.GetTree(r.s, d.hash)
	if err != nil {
		return err
	}
	for _, e := range t.Entries {
		d.entries[e.Name] = &entry{mode: e.Mode, hash: e.Hash}
	}
	return nil
}

// apply performs one write on the tree model and reports whether anything
// actually changed.
func (r *Repo) apply(root *dir, w FileWrite) (bool, error) {
	clean := path.Clean(w.Path)
	if clean == "." || clean == "/" || strings.HasPrefix(clean, "../") {
		return false, fmt.Errorf("gitstore: bad path %q", w.Path)
	}
	parts := strings.Split(clean, "/")
	d := root
	chain := []*dir{root}
	for _, name := range parts[:len(parts)-1] {
		e := d.entries[name]
		if e == nil {
			if w.Data == nil {
				return false, nil // deleting under a directory that does not exist
			}
			e = &entry{mode: filemode.Dir, sub: &dir{entries: map[string]*entry{}, loaded: true}}
			d.entries[name] = e
		}
		if e.mode != filemode.Dir {
			return false, fmt.Errorf("gitstore: %q is a file in the way of %q", name, w.Path)
		}
		if e.sub == nil {
			e.sub = &dir{hash: e.hash}
			if err := r.load(e.sub); err != nil {
				return false, err
			}
		}
		d = e.sub
		chain = append(chain, d)
	}
	name := parts[len(parts)-1]
	cur := d.entries[name]
	if w.Data == nil {
		if cur == nil {
			return false, nil
		}
		delete(d.entries, name)
	} else {
		h, err := r.putBlob(w.Data)
		if err != nil {
			return false, err
		}
		if cur != nil && cur.mode == filemode.Regular && cur.hash == h {
			return false, nil
		}
		d.entries[name] = &entry{mode: filemode.Regular, hash: h}
	}
	for _, x := range chain {
		x.dirty = true
	}
	return true, nil
}

func (r *Repo) putBlob(data []byte) (plumbing.Hash, error) {
	o := r.s.NewEncodedObject()
	o.SetType(plumbing.BlobObject)
	o.SetSize(int64(len(data)))
	w, err := o.Writer()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	if _, err := w.Write(data); err != nil {
		return plumbing.ZeroHash, err
	}
	if err := w.Close(); err != nil {
		return plumbing.ZeroHash, err
	}
	return r.s.SetEncodedObject(o)
}

// writeTree writes every dirty directory bottom-up and returns the root
// hash. A directory left empty is pruned from its parent.
func (r *Repo) writeTree(d *dir) (plumbing.Hash, error) {
	if !d.dirty {
		return d.hash, nil
	}
	t := &object.Tree{}
	for name, e := range d.entries {
		if e.sub != nil && e.sub.dirty {
			h, err := r.writeTree(e.sub)
			if err != nil {
				return plumbing.ZeroHash, err
			}
			if h.IsZero() {
				delete(d.entries, name) // pruned: nothing left underneath
				continue
			}
			e.hash = h
		}
		t.Entries = append(t.Entries, object.TreeEntry{Name: name, Mode: e.mode, Hash: e.hash})
	}
	if len(t.Entries) == 0 {
		return plumbing.ZeroHash, nil
	}
	sortTreeEntries(t.Entries)
	o := r.s.NewEncodedObject()
	if err := t.Encode(o); err != nil {
		return plumbing.ZeroHash, err
	}
	h, err := r.s.SetEncodedObject(o)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	d.hash = h
	d.dirty = false
	return h, nil
}

// sortTreeEntries puts entries in git's canonical order: byte order of the
// name, with a directory compared as if its name ended in "/".
func sortTreeEntries(es []object.TreeEntry) {
	key := func(e object.TreeEntry) string {
		if e.Mode == filemode.Dir {
			return e.Name + "/"
		}
		return e.Name
	}
	sort.Slice(es, func(i, j int) bool { return key(es[i]) < key(es[j]) })
}

// ErrNotFound is returned by readers for a missing path.
var ErrNotFound = errors.New("gitstore: not found")

// ReadFile returns a file's bytes at the branch tip.
func (r *Repo) ReadFile(p string) ([]byte, error) {
	head := r.Head()
	if head.IsZero() {
		return nil, ErrNotFound
	}
	c, err := object.GetCommit(r.s, head)
	if err != nil {
		return nil, err
	}
	t, err := c.Tree()
	if err != nil {
		return nil, err
	}
	f, err := t.File(p)
	if err != nil {
		if errors.Is(err, object.ErrFileNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rd, err := f.Reader()
	if err != nil {
		return nil, err
	}
	defer rd.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(rd); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
