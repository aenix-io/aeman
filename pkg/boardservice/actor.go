package boardservice

import (
	"context"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
)

// WithActor returns a context carrying the acting user's GitHub login. The
// HTTP server stamps it from the request's session, the MCP servers from
// their resolved login; service mutations read it when recording events, and
// backends when attributing notes. (Defined in pkg/board so backends can read
// it without importing boardservice.)
func WithActor(ctx context.Context, login string) context.Context {
	return board.WithActor(ctx, login)
}

// ActorFrom returns the acting user's login stamped by WithActor ("" when
// none — the entry is still recorded, just unattributed).
func ActorFrom(ctx context.Context) string {
	return board.ActorFrom(ctx)
}

// eventRetryBackoff is the per-attempt backoff base for event writes; a
// package variable so tests can shrink it.
var eventRetryBackoff = 400 * time.Millisecond

// logEvent records one activity event on a card, best-effort: the event log
// must never fail the mutation it describes, so the final error is swallowed —
// but transient GitHub hiccups (secondary rate limits on carry bursts, 5xx)
// are retried with backoff first, mirroring setSprintStartRetry. A retry after
// a timed-out-but-applied write can rarely duplicate an event line; events are
// informational, so that beats silently losing them. No-op changes (from ==
// to) are not recorded.
func (s *Service) logEvent(ctx context.Context, b board.Board, card board.Card, kind, from, to string) {
	if from == to && kind != board.EventCreated {
		return
	}
	e := board.Event{
		Kind:  kind,
		Actor: ActorFrom(ctx),
		From:  from,
		To:    to,
		At:    time.Now().UTC().Format(time.RFC3339),
	}
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(attempt) * eventRetryBackoff):
			}
		}
		if err := s.backend.AppendEvent(ctx, b, card, e); err == nil {
			return
		}
	}
}
