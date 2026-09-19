package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/aenix-io/aeman/internal/server/apiv1"
)

// surface is the /api/v1 surface as the document describes it: one method per
// operation, taking the request already bound and answering with a response
// object or an error.
type surface struct {
	s *Server
}

// An operation the generated interface names and this type does not implement
// is a compile error: the document cannot promise a door the server lacks.
var _ apiv1.StrictServerInterface = surface{}

// registerStrict registers the generated routes on the mux, one pattern per
// operation, and gives the three error hooks the generated server calls — a
// body it cannot read, a parameter it cannot bind, an error a method
// returned — the problem shape every other answer under /api/v1 has.
func (s *Server) registerStrict(mux *http.ServeMux) {
	strict := apiv1.NewStrictHandlerWithOptions(surface{s: s},
		[]apiv1.StrictMiddlewareFunc{carryQuery},
		apiv1.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  bodyRefused,
			ResponseErrorHandlerFunc: s.apiError,
		})
	apiv1.HandlerWithOptions(strict, apiv1.StdHTTPServerOptions{
		BaseURL:          "/api/v1",
		BaseRouter:       mux,
		ErrorHandlerFunc: parameterRefused,
	})
}

// bodyRefused answers a request body the generated server could not read. The
// cap is found through the error chain rather than compared, because the
// strict template wraps the decoder's error in one of its own.
func bodyRefused(w http.ResponseWriter, _ *http.Request, err error) {
	if tooBig(w, err) {
		return
	}
	// The sentence a person reads is the decoder's own; the generated server
	// wraps it in one saying it could not decode the body, which is what the
	// code already says.
	if inner := errors.Unwrap(err); inner != nil {
		err = inner
	}
	writeProblem(w, problem(http.StatusBadRequest, "invalidBody", "invalid JSON body: "+err.Error()))
}

// parameterRefused answers a path or query value the generated binder could
// not take. A repeated query key is one: `?team=a&team=b` is a binder error
// where reading the query by hand silently took the first value.
func parameterRefused(w http.ResponseWriter, _ *http.Request, err error) {
	writeProblem(w, problem(http.StatusBadRequest, "invalidParameter", err.Error()))
}

// queryCtxKey carries the request's raw query to a strict method.
type queryCtxKey struct{}

// carryQuery puts the raw query on the context. A strict method is handed the
// typed parameters and not the request, and two readers want the query itself:
// selectorOf, which is the tree's only query-to-Selector parse and is shared
// with the hand-registered watch, and boardOfRequest, which falls back to the
// query's own `view` where the route has no board segment. The parameters
// stay declared in the document, for clients; this side ignores the typed
// copy.
func carryQuery(f apiv1.StrictHandlerFunc, _ string) apiv1.StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
		return f(withQuery(ctx, r.URL.Query()), w, r, request)
	}
}

// withQuery is carryQuery's own step, for the watch: it is registered by hand
// and reads the selectors through the same door a strict method does.
func withQuery(ctx context.Context, q url.Values) context.Context {
	return context.WithValue(ctx, queryCtxKey{}, q)
}

// queryFrom is the request's raw query, as carryQuery left it.
func queryFrom(ctx context.Context) url.Values {
	q, _ := ctx.Value(queryCtxKey{}).(url.Values)
	return q
}

// notAuthenticated is the answer to a request that brings no usable
// credential, as the error a strict method returns.
func notAuthenticated(err error) error {
	return problem(http.StatusUnauthorized, "notAuthenticated", "not authenticated: "+err.Error())
}
