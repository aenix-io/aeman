# Embedding aeman as a library

The packages under `pkg/` are aeman's public library surface. They exist for one reason: any tool that drives the same board — an alternative MCP server, a CLI, a bot — should run **the same board contract** (the date model of [dates.md](dates.md), the stage/progress rules, the review-card link, the carry semantics, the domain rule of [design/git-backend.md](design/git-backend.md)) instead of re-implementing them and drifting.

A typical embedder is a **local MCP server**: it clones the board's repository itself, commits with the same trailers the server writes, and pushes with its own credential — nothing goes through a shared aeman deployment, and the board behaves exactly as it does for everyone else.

## Packages

| Package | What it is |
| --- | --- |
| `pkg/board` | The pure domain: card/board model, date and visibility rules, stage/progress/zone semantics, rank keys, the domain (repository) rule, the visibility projection, link extraction. No I/O. |
| `pkg/boardservice` | The rules engine over a `Backend` interface: create/patch admission, defer, the smart remove, the review cycle, carry-over/carry-week, the activity log. This is the contract. |
| `pkg/gitstore` | The git storage: the repository layout and file formats, one commit per action with trailers, shallow clone / deepen / push / rebase, the card log read from commits. `*gitstore.Backend` (one repository) and `*gitstore.MultiBackend` (a board of several domains) satisfy `boardservice.Backend`. |
| `pkg/apiserver` | The resource layer: Card/Sprint/Note/Ordering shapes (`{kind, metadata, spec, status}`), semantic zones, view selectors. |
| `pkg/mcpserver` | The full MCP tool set over a backend. |

`internal/` (the HTTP server, its cache and coalescing write queue, the sync and push workers, the embedded UI, the migration) stays private. An embedder therefore gets **one commit per write and an explicit push** — the batching the server does between requests is its own.

## The whole MCP server, locally

The shortest path — everything aeman's own `aeman mcp` command does, inside your binary:

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing/cache"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/filesystem"

	"github.com/aenix-io/aeman/pkg/gitstore"
	"github.com/aenix-io/aeman/pkg/mcpserver"
)

func main() {
	ctx := context.Background()
	// The push credential never leaves this process.
	remote := gitstore.Remote{
		URL:  "https://github.com/acme/aeman-db.git",
		Auth: &githttp.BasicAuth{Username: "x-access-token", Password: os.Getenv("AEMAN_GIT_TOKEN")},
	}
	storer := filesystem.NewStorage(osfs.New("./aeman-db"), cache.NewObjectLRUDefault())
	opts := gitstore.Options{Committer: gitstore.Identity{Name: "my-mcp", Email: "mcp@example.com"}}
	repo, err := gitstore.Clone(ctx, storer, remote, opts, 1) // depth 1: the current state
	if err != nil {
		log.Fatal(err)
	}
	srv := mcpserver.New(mcpserver.Config{
		Board:   "aeman-db", // the board's name: its primary repository
		Lock:    true,
		Version: "my-mcp-0.1",
		Backend: gitstore.NewBackend(repo, gitstore.BackendOptions{}),
	})
	if err := mcpserver.Serve(ctx, srv); err != nil {
		log.Fatal(err)
	}
	// Every tool call became a commit; push what accumulated before exiting.
	if err := repo.Push(ctx, remote); err != nil {
		log.Fatal(err)
	}
}
```

That serves the complete tool set (`list_cards`, `update_card`, `carry_over`, …) over stdio with all board rules applied. Add your own tools onto the returned `*mcp.Server` if you need extras. To reopen an existing clone instead of cloning, `gitstore.Open(storer, opts)` returns the repository (its `Head()` is zero when the directory is empty); `repo.Fetch` + `repo.Rebase` bring it up to date the way the server does on its sync tick.

A board of several repositories (domains) is `gitstore.NewMultiBackend([]gitstore.Domain{{Name: "aeman-db", Repo: primary}, {Name: "closed", Repo: closed}}, opts)` — the primary first; cards land in the domain the inheritance rule picks.

## Just the service (your own tools, our contract)

If you keep your own tool surface, build it on the service instead of editing files yourself — every rule then stays in lockstep with aeman:

```go
repo, _ := gitstore.Clone(ctx, storer, remote, opts, 1)
svc := boardservice.New(gitstore.NewBackend(repo, gitstore.BackendOptions{}))

ctx = board.WithActor(ctx, "octocat") // commits are authored by the actor

// Everything the board can do, with admission applied; the board is named
// by its primary repository:
b, _ := svc.Board(ctx, "aeman-db")           // load
_ = svc.Defer(ctx, "aeman-db", uid, 1)       // the +1d rule, incl. same-day relocation
_ = svc.Remove(ctx, "aeman-db", uid, "grid") // release / delete by board rules
rep, _ := svc.CarryOver(ctx, "aeman-db", "team", false)
_ = repo.Push(ctx, remote)                   // one push for what accumulated
```

Each service call is its own commit. To make several calls one action — one commit per touched repository, sharing an `Aeman-Action-Id` — open a scope: `ctx, done := gitstore.WithScope(ctx, gitstore.Action{Name: "carry-over", ID: gitstore.NewID(time.Now())})`, make the calls, then `done()` commits. Inside a scope a write is STAGED: the files land when `done()` commits, and a plain `LoadBoard` in between still reads the repository as it was. Within one call the service never re-reads what it has just written — a create groups the card it holds, the × carries the card through a pull-out, a task's re-routing reads the task the edit just made — so a single action is safe whatever it touches. What a scope cannot do is carry a thing from one call to the NEXT: every entry point starts by loading the board, and a bare store answers that load with the repository as it was, so a card or a task created earlier in the scope is not there yet. Create it, close the scope, then patch it; and read what you wrote from the value the call returned rather than from a fresh load.

For read-side shapes (semantic zones, derived status, view selectors) use `pkg/apiserver`: `apiserver.CardResource`, `apiserver.FilterCards`, `apiserver.ListCards`.

## Writing files without the service

A tool that edits the repository directly (a plugin driving `git` itself) must reproduce the contract by hand: the layout and file formats, the rank keys, the domain rule, the move protocol and the commit trailers are specified in [design/git-backend.md](design/git-backend.md) and summarised for that purpose in [design/plugin-impact.md](design/plugin-impact.md). The server picks such commits up on its fetch tick and reads their changes into the card log like its own.

## Contract changes in this line

`pkg/board` lost `MirrorAllowed` and gained `ColumnDomain` and `Followers`. The removal is the point of the change, not collateral: `MirrorAllowed` compared two PROJECTS' repositories, and a project name may be declared in two repositories with its columns merged under one entry, so it answered for the wrong thing — and answered "no" for every column of no project. `ColumnDomain(b, project, epic)` asks the column itself, which is what the server asks wherever a placement is judged. `Followers(b, id)` lists the cards whose file moves with one — its subtasks and its review card, transitively — for a caller that needs to know what a re-file drags along.

Added in the same line: `board.Board.Primary` (the repository a board's own entries belong to), `board.NewBoardIn` (the assembly, told that name — its own rules ask "the same repository?" while it runs), `board.HomeDomain` (which repository holds a card, in that one namespace), `board.FileDomain` (where a card's FILE is, in the same namespace — the question a rule about the references written IN the file asks), `board.ColumnDomain`, `board.Followers`, `board.InProject` (a card stands in a project through its home pair OR any mirror — what the `?view=project&project=` listing answers by), `boardservice.ErrPlanSubtask`, `apiserver.EpicRef.Domain` and `boardservicetest.Backend.InRepository`.

**Reading a past day is opt-in for a backend.** `boardservice.AsOfReader` — one method, `LoadBoardAsOf(ctx, boardID, at) (board.Board, bool, error)` — is what `Service.BoardAsOf` looks for; storage that keeps history implements it (both gitstore backends do, by reading the tree at the last commit before that moment), and storage that holds only the present simply does not. `ok=false` means the moment is behind what the storage still holds, which the service reports as `ErrHistoryTruncated`; a backend that does not implement the interface at all yields `ErrNoHistory`. Neither is an error your writes have to handle: a board with no history just has no past days (G60).

**The one silent break**: `board.TeamDomain`, `board.ProjectDomain` and `board.ProcessDomain` kept their signatures and changed their answers. They used to return the raw stamp, so an entry of the primary answered `""`; they now answer in the board's one namespace, which means **the primary's NAME** whenever `Primary` is set (`""` only on a board that names none). `ColumnDomain` does the same. Code that tested those answers for emptiness to mean "the primary" reads every entry as foreign the moment the board names its primary — compare against `b.Primary`, or against another reader's answer, never against `""`. "Nothing declares this" is still `""`, and still decides nothing: `TeamDomain` of a team the roster does not carry, and `ColumnDomain`'s `false`.

**If you build a `board.Board` by hand**, either leave every domain blank or set `Primary` to the name your entries are stamped with. A board that stamps its entries while leaving `Primary` empty puts two namespaces in one struct — a card that nothing places resolves to `""` while its column resolves to `"your-repo"` — and every domain rule then answers "another repository". The store stamps CARDS too, the primary's included, so a hand-built board that stamps only its roster leaves `FileDomain` answering for the wrong repository (G59).

## Testing against the board service

`pkg/boardservice/boardservicetest` is a backend fake to run the service against in your own tests. Since the release that carries this note (v0.29.0 or later) it splits the hidden state cards the way a real board does: a sprint-state card among the seeded cards sets its team's sprint, the repository the team was declared in and its place in board order, and is no longer returned as a card row. A test that counted rows, or that expected a team's repository to come from `SprintStates` alone, should be read again — the change is what lets the domain rules (G14, G46, G57) be exercised through the fake at all.

## Versioning

The module is pre-1.0: minor versions may move things around. Pin a tag (`go get github.com/aenix-io/aeman@vX.Y.Z`) and read the [behavior matrix](design/behavior-matrix.md) — it enumerates every rule the contract carries, with the test that pins it.
