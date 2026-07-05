package boardservice

import (
	"context"
	"time"

	"github.com/aenix-org/aeman/pkg/board"
)

// actorKey carries the acting user's GitHub login on the context.
type actorKey struct{}

// WithActor returns a context carrying the acting user's GitHub login. The
// HTTP server stamps it from the request's session, the MCP servers from
// their resolved login; service mutations read it when recording events.
func WithActor(ctx context.Context, login string) context.Context {
	if login == "" {
		return ctx
	}
	return context.WithValue(ctx, actorKey{}, login)
}

// ActorFrom returns the acting user's login stamped by WithActor ("" when
// none — the event is still recorded, just unattributed).
func ActorFrom(ctx context.Context) string {
	login, _ := ctx.Value(actorKey{}).(string)
	return login
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
