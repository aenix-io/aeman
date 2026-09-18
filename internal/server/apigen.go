package server

import (
	"net/http"

	"github.com/aenix-io/aeman/internal/server/apiv1"
)

// surface is the /api/v1 surface as the document describes it: one method per
// operation, taking the request already bound and answering with a response
// object or an error. The embedded interface is nil on purpose — an operation
// that has not been written yet panics here instead of answering wrongly —
// and it goes with the last one.
type surface struct {
	apiv1.StrictServerInterface
	s *Server
}

// registerStrict registers the generated routes on the mux, one pattern per
// operation, and gives the three error hooks the generated server calls — a
// body it cannot read, a parameter it cannot bind, an error a method
// returned — the problem shape every other answer under /api/v1 has.
func (s *Server) registerStrict(mux *http.ServeMux) {
	strict := apiv1.NewStrictHandlerWithOptions(surface{s: s}, nil,
		apiv1.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  bodyRefused,
			ResponseErrorHandlerFunc: s.apiError,
		})
	apiv1.HandlerWithOptions(hybrid{ServerInterface: strict, s: s}, apiv1.StdHTTPServerOptions{
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
	writeProblem(w, problem(http.StatusBadRequest, "invalidBody", "invalid JSON body: "+err.Error()))
}

// parameterRefused answers a path or query value the generated binder could
// not take. A repeated query key is one: `?team=a&team=b` is a binder error
// where reading the query by hand silently took the first value.
func parameterRefused(w http.ResponseWriter, _ *http.Request, err error) {
	writeProblem(w, problem(http.StatusBadRequest, "invalidParameter", err.Error()))
}

// notAuthenticated is the answer to a request that brings no usable
// credential, as the error a strict method returns.
func notAuthenticated(err error) error {
	return problem(http.StatusUnauthorized, "notAuthenticated", "not authenticated: "+err.Error())
}
