# Embedding aeman as a library

The packages under `pkg/` are aeman's public library surface. They exist for one reason: any tool that drives the same GitHub Projects board — an alternative MCP server, a CLI, a bot — should run **the same board contract** (the date model of [dates.md](dates.md), the stage/progress rules, the review-card link, the carry semantics) instead of re-implementing them and drifting.

A typical embedder is a **local, privacy-preserving MCP server**: the GitHub token stays on the user's machine and requests go to GitHub directly — nothing is sent through a shared aeman deployment — while the board behaves exactly as it does for everyone else.

## Packages

| Package | What it is |
| --- | --- |
| `pkg/board` | The pure domain: card/board model, date and visibility rules, stage/progress/zone semantics, link extraction. No I/O. |
| `pkg/boardservice` | The rules engine over a `Backend` interface: create/patch admission, defer, the smart remove, the review cycle, carry-over/carry-week. This is the contract. |
| `pkg/ghprojects` | The GitHub Projects v2 backend (GraphQL, direct). `*ghprojects.Client` satisfies `boardservice.Backend`. |
| `pkg/apiserver` | The resource layer: Card/Sprint/Note/Ordering shapes (`{kind, metadata, spec, status}`), semantic zones, view selectors. |
| `pkg/mcpserver` | The full MCP tool set over a board service. |

`internal/` (the HTTP server, the watch hub, the embedded UI) stays private.

## The whole MCP server, locally

The shortest path — everything aeman's own `aeman mcp` command does, inside your binary:

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/aenix-org/aeman/pkg/mcpserver"
)

func main() {
	srv := mcpserver.New(mcpserver.Config{
		Owner:   "aenix-org",
		Project: 37,
		Version: "my-mcp-0.1",
		// The token never leaves this process: the backend talks to
		// api.github.com directly.
		ResolveToken: func(ctx context.Context) (string, error) {
			return os.Getenv("GITHUB_TOKEN"), nil
		},
	})
	if err := mcpserver.Serve(context.Background(), srv); err != nil {
		log.Fatal(err)
	}
}
```

That serves the complete tool set (`list_cards`, `update_card`, `carry_over`, …) over stdio with all board rules applied. Add your own tools onto the returned `*mcp.Server` if you need extras.

## Just the service (your own tools, our contract)

If you keep your own tool surface, build it on the service instead of raw GitHub calls — every rule then stays in lockstep with aeman:

```go
client := ghprojects.New(os.Getenv("GITHUB_TOKEN"))
svc := boardservice.New(client)

// Everything the board can do, with admission applied:
cards, _ := svc.Board(ctx, "aenix-org", 37)      // load
_ = svc.Defer(ctx, "aenix-org", 37, uid, 1)      // the +1d rule, incl. same-day relocation
_ = svc.Remove(ctx, "aenix-org", 37, uid, "grid") // demote / release / delete by board rules
rep, _ := svc.CarryOver(ctx, "aenix-org", 37, "team", false)
```

For read-side shapes (semantic zones, derived status, view selectors) use `pkg/apiserver`: `apiserver.CardResource`, `apiserver.FilterCards`, `apiserver.ListCards`.

## Versioning

The module is pre-1.0: minor versions may move things around. Pin a tag (`go get github.com/aenix-org/aeman@vX.Y.Z`) and read the [behavior matrix](design/behavior-matrix.md) — it enumerates every rule the contract carries, with the test that pins it.
