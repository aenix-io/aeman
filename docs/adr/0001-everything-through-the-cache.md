# ADR 0001 — Everything goes through the cache, with a Kubernetes-shaped API

**Status:** accepted · 2026-08-22

## Decision

Every entity aeman exposes — cards, teams, projects, epic columns,
deadlines, processes, templates — is served from the server's in-memory
board cache and mutated through it. A write is applied to the cache and
acknowledged at once; the backing store (GitHub Projects v2) is written
behind, through a per-board FIFO with DeltaFIFO-style coalescing. Every
change is broadcast to every open client over a `watch` stream the way
Kubernetes streams resource events, and clients apply the event to their
state. Nobody reloads.

We do not design for a slow backend. A request's latency is the cache's,
not GitHub's; GitHub is eventually consistent with the cache, never the
other way round.

## Why

aeman is a shared board. The whole point is that when one person moves
a card, everyone with the board open sees it move — and sees it now, not
after a reload, and not after a multi-second round trip to GitHub.
GitHub's API is the store of record, but it is too slow (a 400-card
board loads in ~40 s) and too rate-limited to sit on the request path.

The first time a new entity was wired straight to the store instead of
through the cache, it looked fine in isolation and was wrong in use:
adding a column blocked on a full board reload (47 s), and the other
open tabs never learned about it. Both were fixed by routing the entity
through the cache like everything else. That experience is this ADR.

## Consequences

- A new entity is not done until it lives in the cache (`boardEntry`),
  is mutated through the write queue, and is carried by a `watch` frame
  that clients apply. Adding it to the store alone is not adding it.
- Hidden state cards (`aeman:*-state`) are how non-card entities persist
  in a store that only has cards; the cache splits them out on load and
  maintains the rosters on every mutation.
- Clients never call `reload()` after a write they made. The frame that
  confirms the write repaints the view — theirs and everyone else's.
- Roster reads may accept a stale snapshot (minutes old) when they only
  need to check a name; the background revalidation catches up.
- The cost: the cache is the one place correctness lives, and a mutation
  that forgets to update it (a template's body silently dropped because
  it was "not a card row") is a bug that tests must cover.
