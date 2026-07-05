package boardservice

import (
	"context"
	"time"

	"github.com/aenix-org/aeman/pkg/board"
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

// logEvent records one activity event on a card, best-effort: the event log
// must never fail or slow down the mutation it describes, so errors are
// swallowed. No-op changes (from == to) are not recorded.
func (s *Service) logEvent(ctx context.Context, b board.Board, card board.Card, kind, from, to string) {
	if from == to && kind != board.EventCreated {
		return
	}
	_ = s.backend.AppendEvent(ctx, b, card, board.Event{
		Kind:  kind,
		Actor: ActorFrom(ctx),
		From:  from,
		To:    to,
		At:    time.Now().UTC().Format(time.RFC3339),
	})
}
