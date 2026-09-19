package server

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
)

// Every refusal the service can hand back is a REFUSAL — 422, "a rule
// refused the change" — while what the sentinels table does not name answers
// 502, "the forge could not be reached". A sentinel that misses the table
// therefore tells the caller a lie about whose fault it is and invites a
// retry that cannot help: ErrSubtaskWeek shipped exactly that way.
//
// The list of sentinels is read from the SOURCE, not from a table kept
// here: a table has to be updated by the same person who forgot the other
// one, which is no check at all. Every exported Err… in the package must
// have a row, by name — and the row's code is the name itself (ErrCardNotFound
// is cardNotFound), so a row cannot pair a sentinel with another one's code.
func TestEverySentinelIsAnsweredByApiError(t *testing.T) {
	names := exportedSentinelNames(t, "../../pkg/boardservice")
	if len(names) < 15 {
		t.Fatalf("only %d sentinels found — has the package moved?", len(names))
	}
	rows := sentinelRows(t, "problem.go")
	answered := map[string]bool{}
	owner := map[string]string{}
	for _, r := range rows {
		if r.pkg == "boardservice" {
			answered[r.name] = true
		}
		if want := strings.ToLower(r.name[3:4]) + r.name[4:]; r.code != want {
			t.Errorf("%s.%s is coded %q, want %q — the code is the sentinel's name", r.pkg, r.name, r.code, want)
		}
		if prev, taken := owner[r.code]; taken {
			t.Errorf("code %q names both %s and %s.%s", r.code, prev, r.pkg, r.name)
		}
		owner[r.code] = r.pkg + "." + r.name
	}
	var missing []string
	for _, n := range names {
		if !answered[n] {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("the sentinels table does not name these, so they answer 502 — a rule that refused a change is not a forge failure: %s",
			strings.Join(missing, ", "))
	}
	// Named, not necessarily 422: a not-found sentinel answers 404 and a
	// forbidden one 403. What no refusal may be is a GATEWAY failure,
	// which is what falling off the table means.
	// And the mapping is real, not just a mention: one sentinel end to end.
	if code := statusFor(t, boardservice.ErrSubtaskWeek); code != 422 {
		t.Fatalf("ErrSubtaskWeek answers %d, want 422", code)
	}
}

// exportedSentinelNames lists the package's exported error values —
// `var ErrX = errors.New(...)` — by parsing its files. Walked and parsed
// one by one rather than through ParseDir, which is deprecated for not
// honouring build tags.
func exportedSentinelNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range file.Decls {
			gen, ok := d.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, id := range vs.Names {
					if strings.HasPrefix(id.Name, "Err") && ast.IsExported(id.Name) {
						out = append(out, id.Name)
					}
				}
			}
		}
	}
	return out
}

type sentinelRow struct{ pkg, name, code string }

// sentinelRows reads the `sentinels` table out of the source: each row's
// pkg.ErrName and the code written beside it.
func sentinelRows(t *testing.T, path string) []sentinelRow {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []sentinelRow
	ast.Inspect(file, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "sentinels" || len(vs.Values) != 1 {
			return true
		}
		table, ok := vs.Values[0].(*ast.CompositeLit)
		if !ok {
			t.Fatalf("%s: sentinels is not a literal", path)
		}
		for _, elt := range table.Elts {
			row, ok := elt.(*ast.CompositeLit)
			if !ok || len(row.Elts) != 3 {
				t.Fatalf("%s: a sentinels row is not {err, status, code}", path)
			}
			sel, isSel := row.Elts[0].(*ast.SelectorExpr)
			lit, isLit := row.Elts[2].(*ast.BasicLit)
			if !isSel || !isLit {
				t.Fatalf("%s: a sentinels row is not {pkg.ErrName, status, \"code\"}", path)
			}
			pkg, isIdent := sel.X.(*ast.Ident)
			if !isIdent {
				t.Fatalf("%s: a sentinels row names its error as something other than pkg.ErrName", path)
			}
			code, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, sentinelRow{pkg: pkg.Name, name: sel.Sel.Name, code: code})
		}
		return false
	})
	if len(out) == 0 {
		t.Fatalf("%s has no sentinels table", path)
	}
	return out
}

func statusFor(t *testing.T, err error) int {
	t.Helper()
	var srv Server
	rec := httptest.NewRecorder()
	srv.apiError(rec, httptest.NewRequest("GET", "/api/v1/cards", nil), err)
	return rec.Code
}

// A query key sent twice is refused rather than half-read. Every selector is
// ONE value — `team` unions a comma-separated set inside that one value — and
// reading the query by hand took the first of a repeated key and dropped the
// rest, so `?team=a&team=b` quietly listed team a's cards. The generated
// binder refuses it, which is the honest answer: the caller asked for
// something the surface does not offer. It is the GENERATED doors that refuse.
// The watch is registered by hand, goes nowhere near the binder, and still
// takes the first value — one query string, two answers, worth knowing before
// reading the two doors as one rule.
func TestARepeatedQueryKeyIsRefused(t *testing.T) {
	fake := boardservicetest.New([]board.Card{{ItemID: "c1", Team: "a"}}, nil)
	srv := apiServer(t, Options{}, fake)

	rec := do(t, srv, http.MethodGet, cardsPath("team=a,b"), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("a comma-separated set is one value: %d — %s", rec.Code, rec.Body.String())
	}
	rec = do(t, srv, http.MethodGet, cardsPath("team=a&team=b"), "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a repeated key = %d, want 400 — %s", rec.Code, rec.Body.String())
	}
	if code := decodeProblem(t, rec).Code; code != "invalidParameter" {
		t.Errorf("code = %q, want invalidParameter", code)
	}
}

// The body is read before the board is looked at, and that order is now the
// generated server's rather than each handler's. It shows where a request is
// wrong twice over: a malformed body sent to a board that does not draw the
// gesture — or to no board at all — is answered as the malformed body (400)
// where the gate used to answer first (404). Both answers are true of the
// request; which one arrives is what this pins.
func TestTheBodyIsReadBeforeTheBoardIsChecked(t *testing.T) {
	fake := boardservicetest.New([]board.Card{{ItemID: "c1", Team: "t"}}, nil)
	srv := apiServer(t, Options{}, fake)
	for _, c := range []struct {
		name, target, body string
		want               int
		code               string
	}{
		{"a gesture the board does not draw", "/api/v1/views/me/cards/c1/actions/place",
			`{"week":"2026-09-14"}`, http.StatusNotFound, "gestureNotOffered"},
		{"the same gesture with a body that does not parse", "/api/v1/views/me/cards/c1/actions/place",
			`{"week":`, http.StatusBadRequest, "invalidBody"},
		{"a create into a board nobody has", "/api/v1/views/nope/cards",
			`{"title":"x"}`, http.StatusNotFound, "noSuchView"},
		{"the same create with a body that does not parse", "/api/v1/views/nope/cards",
			`{"title":`, http.StatusBadRequest, "invalidBody"},
	} {
		rec := do(t, srv, http.MethodPost, c.target, c.body)
		if rec.Code != c.want {
			t.Errorf("%s: %d, want %d — %s", c.name, rec.Code, c.want, rec.Body.String())
			continue
		}
		if got := decodeProblem(t, rec).Code; got != c.code {
			t.Errorf("%s: code %q, want %q", c.name, got, c.code)
		}
	}
}
