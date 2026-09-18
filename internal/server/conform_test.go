package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/aenix-io/aeman/api"
	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
)

// The spec is the contract, so the exchanges these tests make are held
// against it: conforms wraps a server's handler, finds the operation the
// request matches and validates what went out and what came back. A path the
// document does not describe is a failure in itself — the description would
// be a partial one, and a client generated from it would be missing exactly
// the door nobody wrote down.

var (
	specOnce   sync.Once
	specDoc    *openapi3.T
	specRouter routers.Router
	specErr    error
)

// loadedSpec parses and validates api.Spec once for the whole package.
func loadedSpec(t *testing.T) (*openapi3.T, routers.Router) {
	t.Helper()
	specOnce.Do(func() {
		loader := openapi3.NewLoader()
		doc, err := loader.LoadFromData(api.Spec)
		if err != nil {
			specErr = err
			return
		}
		if err := doc.Validate(context.Background()); err != nil {
			specErr = err
			return
		}
		specDoc, specErr = doc, nil
		specRouter, specErr = gorillamux.NewRouter(doc)
	})
	if specErr != nil {
		t.Fatalf("api/openapi.yaml: %v", specErr)
	}
	return specDoc, specRouter
}

// conforms wraps a handler so every /api/v1 exchange it serves is checked
// against the spec. Only a recorded response is checked: a watch connection
// hijacks the writer, and the spec describes no frames of it.
func conforms(t *testing.T, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec, recorded := w.(*httptest.ResponseRecorder)
		if !recorded || !describedPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		var sent []byte
		if r.Body != nil {
			sent, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(sent))
		}
		next.ServeHTTP(w, r)
		checkExchange(t, r, sent, rec)
	})
}

// describedPath reports whether the spec speaks for a path. The watch is an
// upgrade rather than a response, and the document cannot be validated
// against itself.
func describedPath(path string) bool {
	if !strings.HasPrefix(path, "/api/v1/") {
		return false
	}
	return !strings.HasSuffix(path, "/watch") && path != "/api/v1/openapi.json"
}

func checkExchange(t *testing.T, r *http.Request, sent []byte, rec *httptest.ResponseRecorder) {
	t.Helper()
	_, router := loadedSpec(t)
	// A copy, because the served request has had its body read and its
	// context stamped by the middleware chain.
	probe := httptest.NewRequest(r.Method, r.URL.String(), bytes.NewReader(sent))
	for name, values := range r.Header {
		probe.Header[name] = values
	}
	if len(sent) > 0 && probe.Header.Get("Content-Type") == "" {
		probe.Header.Set("Content-Type", "application/json")
	}
	route, params, err := router.FindRoute(probe)
	if err != nil {
		// A path the document does not describe is only a failure when a
		// handler answered it. The catch-all saying "no such route" is the
		// mux agreeing with the spec that there is no such door.
		if !catchAll(rec) {
			t.Errorf("%s %s: no operation in the spec, yet a handler answered %d — %v",
				r.Method, r.URL, rec.Code, err)
		}
		return
	}
	in := &openapi3filter.RequestValidationInput{
		Request:    probe,
		PathParams: params,
		Route:      route,
		Options:    &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
	}
	// Only what the server ACCEPTED: a refused request is often deliberately
	// malformed, and the spec is not the thing that refused it.
	if rec.Code < 400 {
		if err := openapi3filter.ValidateRequest(context.Background(), in); err != nil {
			t.Errorf("%s %s: the request does not conform: %v — %s", r.Method, r.URL, err, sent)
		}
	}
	if err := openapi3filter.ValidateResponse(context.Background(), &openapi3filter.ResponseValidationInput{
		RequestValidationInput: in,
		Status:                 rec.Code,
		Header:                 rec.Header(),
		Body:                   io.NopCloser(bytes.NewReader(rec.Body.Bytes())),
		Options:                &openapi3filter.Options{IncludeResponseStatus: true},
	}); err != nil {
		t.Errorf("%s %s: the %d does not conform: %v — %s", r.Method, r.URL, rec.Code, err, rec.Body.String())
	}
}

// A problem's code is a closed set: a client branches on it, so a code the
// server can emit and the document does not name is a case nobody can
// handle, and one the document names and the server cannot is a case
// written for nothing.
func TestProblemCodesMatchTheSpec(t *testing.T) {
	doc, _ := loadedSpec(t)
	schema := doc.Components.Schemas["Problem"]
	if schema == nil {
		t.Fatal("the spec has no Problem schema")
	}
	code := schema.Value.Properties["code"]
	if code == nil || len(code.Value.Enum) == 0 {
		t.Fatal("Problem.code is not an enum: the codes are not closed")
	}
	described := map[string]bool{}
	for _, v := range code.Value.Enum {
		described[v.(string)] = true
	}
	emitted := map[string]bool{}
	for _, row := range sentinelRows(t, "problem.go") {
		emitted[row.code] = true
	}
	for _, c := range writtenProblemCodes(t) {
		emitted[c] = true
	}
	for _, c := range sortedKeys(emitted) {
		if !described[c] {
			t.Errorf("the server can answer code %q and the spec does not name it", c)
		}
	}
	for _, c := range sortedKeys(described) {
		if !emitted[c] {
			t.Errorf("the spec names code %q and nothing in the package writes it", c)
		}
	}
}

// writtenProblemCodes reads every code passed to problem() in the package's
// own source — the refusals a handler or a gate writes itself, beside the
// sentinels table's.
func writtenProblemCodes(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 3 {
				return true
			}
			fn, ok := call.Fun.(*ast.Ident)
			if !ok || fn.Name != "problem" {
				return true
			}
			lit, ok := call.Args[1].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			code, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, code)
			return true
		})
	}
	if len(out) == 0 {
		t.Fatal("no problem() call was found — has the helper been renamed?")
	}
	return out
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// An operation nothing serves is a promise the document cannot keep. Every
// path the spec describes is asked of the real mux: whatever a handler
// answers proves the door is there, while the mux's own "not found" — plain
// text from no handler at all — proves it is not.
func TestEverySpecOperationHasItsRoute(t *testing.T) {
	doc, _ := loadedSpec(t)
	fake := boardservicetest.New([]board.Card{{ItemID: "x", Team: "test"}},
		map[string]board.SprintState{"test": {Current: "2026-08-24", ItemID: "st1"}})
	srv := apiServer(t, Options{}, fake)
	for path, item := range doc.Paths.Map() {
		target := "/api/v1" + regexpPathParams.ReplaceAllString(path, "x")
		for method, op := range item.Operations() {
			if op.OperationID == "watchView" {
				continue
			}
			rec := do(t, srv, method, target, "")
			if catchAll(rec) {
				t.Errorf("%s %s (%s): nothing serves it — the catch-all answered",
					method, target, op.OperationID)
			}
		}
	}
}

// The other direction: a door nobody wrote down is a door an agent cannot
// find, which is what happened to PATCH /people/{login} while the catalog was
// kept by hand. The wiring is read from the source, so a route added without
// its operation fails here rather than at the first client that needs it.
func TestEveryWiredRouteIsDescribed(t *testing.T) {
	doc, _ := loadedSpec(t)
	src, err := os.ReadFile("api.go")
	if err != nil {
		t.Fatal(err)
	}
	wired := regexp.MustCompile(`mux\.HandleFunc\("([A-Z]+) (/api/v1[^"]*)"`)
	found := wired.FindAllStringSubmatch(string(src), -1)
	if len(found) < 50 {
		t.Fatalf("only %d routes found — has registerAPI moved?", len(found))
	}
	for _, m := range found {
		method, path := m[1], strings.TrimPrefix(m[2], "/api/v1")
		// The index is the one route the document does not describe: under
		// a server of /api/v1 it would be the path "/", which a generator
		// turns into a subtree pattern swallowing every unknown path.
		if path == "" {
			continue
		}
		item := doc.Paths.Value(path)
		if item == nil || item.GetOperation(method) == nil {
			t.Errorf("%s %s is wired and the spec does not describe it", method, m[2])
		}
	}
}

// The document is answered like the index beside it: a client generating
// itself from it has nothing to be authorized for yet, so neither answer
// resolves a token or builds a board service.
func TestOpenAPIDocumentIsServed(t *testing.T) {
	srv := apiServer(t, Options{}, boardservicetest.New(nil, nil))
	srv.newService = func(*http.Request) (*boardservice.Service, error) {
		t.Fatal("the document must not build a board service")
		return nil, nil
	}
	srv.apiTokens = func(*http.Request) (string, string, error) {
		return "", "", errors.New("no credential on this machine")
	}
	rec := do(t, srv, http.MethodGet, "/api/v1/openapi.json", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want JSON", ct)
	}
	doc, err := openapi3.NewLoader().LoadFromData(rec.Body.Bytes())
	if err != nil {
		t.Fatalf("what is served is not an OpenAPI document: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("the served document does not validate: %v", err)
	}
	if doc.Paths.Value("/cards/{uid}") == nil {
		t.Error("the served document describes no card: it is not this board's")
	}
}

// regexpPathParams matches a spec path's {placeholders}.
var regexpPathParams = regexp.MustCompile(`\{[^}]+\}`)

// catchAll reports whether the response is the one the catch-all writes for a
// path no route serves. It is the single answer under /api/v1/ that proves no
// handler ran: everything else — a refusal included — came from one, and a
// refusal proves the route as well as a 200 does.
func catchAll(rec *httptest.ResponseRecorder) bool {
	if rec.Code != http.StatusNotFound {
		return false
	}
	var p Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		return false
	}
	return p.Code == "unknownRoute"
}
