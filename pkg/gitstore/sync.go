package gitstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/storage"
)

// Sync is how a domain's clone talks to its remote: clone (shallow), fetch
// what others pushed, push what we committed, deepen the history to a time
// horizon, and tidy the object store. Nothing here touches the branch on
// its own except Clone and ResetTo: fetch reports the remote tip, the caller
// re-applies its queue and adopts it.
//
// Fetch and deepen drive one upload-pack session by hand rather than going
// through go-git's Fetch: that one has no time-based depth, applies only
// the server's "shallow" lines and not its "unshallow" lines, and offers
// every local ref as a "have" — including commits the remote has never seen,
// which a strict server refuses. Here the only have is the tip the remote
// is known to hold.

// Remote is where a domain's repository lives: a URL and how to
// authenticate. Nothing forge-specific — HTTPS with a token works the same
// on GitHub, GitLab, Gitea and a bare repository on a server.
type Remote struct {
	URL  string
	Auth transport.AuthMethod
}

const remoteName = "origin"

// ErrNoRemoteTip is returned when the remote has no branch to sync with.
var ErrNoRemoteTip = errors.New("gitstore: remote has no such branch")

// Clone clones the remote's branch into s. depth 1 is the board's current
// state — the cold start; 0 is the whole history.
func Clone(ctx context.Context, s storage.Storer, remote Remote, opts Options, depth int) (*Repo, error) {
	r := wrap(s, opts)
	_, err := git.CloneContext(ctx, s, nil, &git.CloneOptions{
		URL:           remote.URL,
		Auth:          remote.Auth,
		RemoteName:    remoteName,
		ReferenceName: r.branch,
		SingleBranch:  true,
		NoCheckout:    true,
		Depth:         depth,
	})
	if err != nil {
		// An unborn remote, or one with commits on other branches but no
		// board branch: for the board both are "not initialised".
		var noRef git.NoMatchingRefSpecError
		if errors.Is(err, transport.ErrEmptyRemoteRepository) || errors.As(err, &noRef) {
			return nil, fmt.Errorf("%w: %s", ErrEmptyRepository, remote.URL)
		}
		return nil, fmt.Errorf("gitstore: clone %s: %w", remote.URL, err)
	}
	return r, nil
}

// tracking is the remote-tracking ref for the board branch.
func (r *Repo) tracking() plumbing.ReferenceName {
	return plumbing.NewRemoteReferenceName(remoteName, r.branch.Short())
}

// RemoteTip is the last known tip of the remote's branch (zero if never
// fetched or pushed).
func (r *Repo) RemoteTip() plumbing.Hash {
	ref, err := r.s.Reference(r.tracking())
	if err != nil {
		return plumbing.ZeroHash
	}
	return ref.Hash()
}

// Fetch brings the remote's branch into the tracking ref — no depth, so a
// shallow clone gains no new boundaries — and reports the remote tip and
// whether it moved since the last fetch or push. The local branch is not
// touched.
func (r *Repo) Fetch(ctx context.Context, remote Remote) (plumbing.Hash, bool, error) {
	before := r.RemoteTip()
	tip, err := r.uploadPack(ctx, remote, func(req *packp.UploadPackRequest, tip plumbing.Hash) bool {
		if tip == before {
			return false // nothing new
		}
		if !before.IsZero() && r.has(before) {
			req.Haves = []plumbing.Hash{before}
		}
		return true
	})
	if err != nil {
		return plumbing.ZeroHash, false, fmt.Errorf("gitstore: fetch: %w", err)
	}
	if tip != before {
		if err := r.s.SetReference(plumbing.NewHashReference(r.tracking(), tip)); err != nil {
			return plumbing.ZeroHash, false, err
		}
	}
	return tip, tip != before, nil
}

// DeepenSince fetches, into a shallow clone, every commit of the remote's
// branch newer than since, and repairs the shallow list: (old + shallows −
// unshallows). One round-trip, exact to the day.
func (r *Repo) DeepenSince(ctx context.Context, remote Remote, since time.Time) error {
	_, err := r.uploadPack(ctx, remote, func(req *packp.UploadPackRequest, _ plumbing.Hash) bool {
		req.Depth = packp.DepthSince(since)
		if h := r.RemoteTip(); !h.IsZero() && r.has(h) {
			req.Haves = append(req.Haves, h)
		}
		return true
	})
	if err != nil {
		return fmt.Errorf("gitstore: deepen: %w", err)
	}
	return nil
}

// uploadPack runs one upload-pack session for the board branch: advertise,
// let build shape the request (haves, depth) — build returns false to skip
// the transfer — store the pack, apply shallow and unshallow lines. It
// returns the remote's tip.
func (r *Repo) uploadPack(ctx context.Context, remote Remote, build func(*packp.UploadPackRequest, plumbing.Hash) bool) (tip plumbing.Hash, err error) {
	ep, err := transport.NewEndpoint(remote.URL)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	cl, err := client.NewClient(ep)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	sess, err := cl.NewUploadPackSession(ep, remote.Auth)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	defer closeKeep(sess, &err)
	ar, err := sess.AdvertisedReferencesContext(ctx)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	refs, err := ar.AllReferences()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	ref, ok := refs[r.branch]
	if !ok {
		return plumbing.ZeroHash, ErrNoRemoteTip
	}
	tip = ref.Hash()
	req := packp.NewUploadPackRequestFromCapabilities(ar.Capabilities)
	req.Wants = []plumbing.Hash{tip}
	if !build(req, tip) {
		return tip, nil
	}
	if !req.Depth.IsZero() && !ar.Capabilities.Supports(capability.DeepenSince) {
		return plumbing.ZeroHash, errors.New("remote does not support deepen-since")
	}
	for _, cp := range []capability.Capability{capability.Shallow, capability.DeepenSince, capability.OFSDelta, capability.Sideband64k, capability.NoProgress} {
		if ar.Capabilities.Supports(cp) {
			if err := req.Capabilities.Set(cp); err != nil {
				return plumbing.ZeroHash, err
			}
		}
	}
	shallows, err := r.s.Shallow()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	if len(shallows) > 0 {
		if !ar.Capabilities.Supports(capability.Shallow) {
			return plumbing.ZeroHash, errors.New("remote does not support shallow clients")
		}
		req.Shallows = shallows
	}
	resp, err := sess.UploadPack(ctx, req)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	defer closeKeep(resp, &err)
	var body io.Reader = resp
	if req.Capabilities.Supports(capability.Sideband64k) {
		body = sideband.NewDemuxer(sideband.Sideband64k, resp)
	}
	if err := packfile.UpdateObjectStorage(r.s, body); err != nil {
		return plumbing.ZeroHash, err
	}
	if len(resp.Shallows) > 0 || len(resp.Unshallows) > 0 {
		if err := r.applyShallows(resp.Shallows, resp.Unshallows); err != nil {
			return plumbing.ZeroHash, err
		}
	}
	return tip, nil
}

// has reports whether the object is in the local store.
func (r *Repo) has(h plumbing.Hash) bool {
	_, err := r.s.EncodedObject(plumbing.AnyObject, h)
	return err == nil
}

// Push sends the local branch to the remote. Any error may be a rejected
// non-fast-forward — a shallow clone reports it as "object not found" — so
// the caller does not classify: it fetches, and if the remote moved it
// re-applies and retries. On success the tracking ref is the pushed tip.
func (r *Repo) Push(ctx context.Context, remote Remote) error {
	return r.push(ctx, remote, false)
}

// PushForce writes the local branch over the remote's whatever it holds —
// a migration re-run with --force replaces the earlier import and anything
// written since. Nothing on the request path uses it: a rejected push there
// is re-applied on the remote's tip (Rebase), never forced.
func (r *Repo) PushForce(ctx context.Context, remote Remote) error {
	return r.push(ctx, remote, true)
}

func (r *Repo) push(ctx context.Context, remote Remote, force bool) error {
	head := r.Head()
	if head.IsZero() {
		return errors.New("gitstore: nothing to push")
	}
	spec := r.branch.String() + ":" + r.branch.String()
	if force {
		spec = "+" + spec
	}
	rem := git.NewRemote(r.s, &config.RemoteConfig{Name: remoteName, URLs: []string{remote.URL}})
	err := rem.PushContext(ctx, &git.PushOptions{
		Auth:     remote.Auth,
		RefSpecs: []config.RefSpec{config.RefSpec(spec)},
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("gitstore: push: %w", err)
	}
	return r.s.SetReference(plumbing.NewHashReference(r.tracking(), head))
}

// ResetTo moves the local branch to a commit — onto a freshly fetched
// remote tip before the queue is re-applied on top of it.
func (r *Repo) ResetTo(h plumbing.Hash) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.resetLocked(h)
}

func (r *Repo) resetLocked(h plumbing.Hash) error {
	return r.s.SetReference(plumbing.NewHashReference(r.branch, h))
}

// Unpushed counts the local commits the remote has not seen: from the tip
// back to the last known remote tip (within the loaded history).
func (r *Repo) Unpushed() (int, error) {
	cs, err := r.UnpushedCommits()
	return len(cs), err
}

// ChangedPaths lists the paths whose content differs between two commits'
// trees, sorted. A remote commit's diff goes through ParsePath from here.
func (r *Repo) ChangedPaths(from, to plumbing.Hash) ([]string, error) {
	fromTree, err := r.treeOf(from)
	if err != nil {
		return nil, err
	}
	toTree, err := r.treeOf(to)
	if err != nil {
		return nil, err
	}
	changes, err := object.DiffTree(fromTree, toTree)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, ch := range changes {
		name := ch.To.Name
		if name == "" {
			name = ch.From.Name
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// treeOf returns a commit's tree, or nil for the zero hash.
func (r *Repo) treeOf(h plumbing.Hash) (*object.Tree, error) {
	if h.IsZero() {
		return nil, nil //nolint:nilnil // nil tree is "nothing", the diff handles it
	}
	c, err := object.GetCommit(r.s, h)
	if err != nil {
		return nil, err
	}
	return c.Tree()
}

// applyShallows sets the shallow list to old + added − removed.
func (r *Repo) applyShallows(added, removed []plumbing.Hash) error {
	old, err := r.s.Shallow()
	if err != nil {
		return err
	}
	drop := map[plumbing.Hash]bool{}
	for _, h := range removed {
		drop[h] = true
	}
	seen := map[plumbing.Hash]bool{}
	next := []plumbing.Hash{}
	for _, h := range append(old, added...) {
		if drop[h] || seen[h] {
			continue
		}
		seen[h] = true
		next = append(next, h)
	}
	return r.s.SetShallow(next)
}

func closeKeep(c io.Closer, err *error) {
	if cerr := c.Close(); cerr != nil && *err == nil {
		*err = cerr
	}
}

// Maintain repacks loose objects into one pack and prunes them — go-git
// never packs on its own, and a commit per action leaves ~4 loose objects
// each. A store that cannot pack (the in-memory one) is left alone.
func (r *Repo) Maintain() error {
	if _, ok := r.s.(storer.PackedObjectStorer); !ok {
		return nil
	}
	repo, err := git.Open(r.s, nil)
	if err != nil {
		return err
	}
	if err := repo.RepackObjects(&git.RepackConfig{}); err != nil {
		return fmt.Errorf("gitstore: repack: %w", err)
	}
	err = repo.Prune(git.PruneOptions{OnlyObjectsOlderThan: time.Now(), Handler: repo.DeleteObject})
	if err != nil && !errors.Is(err, git.ErrLooseObjectsNotSupported) {
		return fmt.Errorf("gitstore: prune: %w", err)
	}
	return nil
}
