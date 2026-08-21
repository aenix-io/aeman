package server

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
)

// staleControl rides a read-only request's context down to the board store:
// its presence marks the request as one that may accept a stale board snapshot
// (the store then revalidates in the background), and served records that the
// store actually used one, which the middleware surfaces as a response header
// so the client can hold its progress indicator until the Sync watch frame.
type staleControl struct {
	served atomic.Bool
}

type staleCtxKey struct{}

// withStaleAllowed marks the context as accepting a stale board snapshot.
func withStaleAllowed(ctx context.Context) (context.Context, *staleControl) {
	sc := &staleControl{}
	return context.WithValue(ctx, staleCtxKey{}, sc), sc
}

// staleOK is withStaleAllowed for callers that do not care to know whether the
// snapshot they got was stale — the roster mutations, which read the board
// only to check a name and to carry its ids into the write.
func staleOK(ctx context.Context) context.Context {
	c, _ := withStaleAllowed(ctx)
	return c
}

// staleControlFrom returns the request's stale marker, or nil when the request
// must be served current data (every mutation path).
func staleControlFrom(ctx context.Context) *staleControl {
	sc, _ := ctx.Value(staleCtxKey{}).(*staleControl)
	return sc
}

// staleMiddleware lets read-only API requests be answered from a stale board
// snapshot and reports that via the X-Aeman-Stale header. Mutations never see
// stale state: their internal reads must be current (a carry-over picks cards
// by the live sprint pointer, a move resolves its anchor from the live order),
// so only GETs opt in. The watch endpoint hijacks the connection and opts in
// on its own instead of going through the wrapped writer.
func staleMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet ||
			!strings.HasPrefix(r.URL.Path, "/api/v1") ||
			r.URL.Path == "/api/v1/watch" {
			next.ServeHTTP(w, r)
			return
		}
		ctx, sc := withStaleAllowed(r.Context())
		next.ServeHTTP(&staleHeaderWriter{ResponseWriter: w, sc: sc}, r.WithContext(ctx))
	})
}

// staleHeaderWriter stamps X-Aeman-Stale onto the response just before the
// first byte goes out — by then the handler has run LoadBoard and the store
// has recorded whether it served a stale snapshot.
type staleHeaderWriter struct {
	http.ResponseWriter
	sc    *staleControl
	wrote bool
}

func (w *staleHeaderWriter) WriteHeader(code int) {
	if !w.wrote {
		w.wrote = true
		if w.sc.served.Load() {
			w.Header().Set("X-Aeman-Stale", "true")
		}
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *staleHeaderWriter) Write(p []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}
