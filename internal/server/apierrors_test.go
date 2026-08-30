package server

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/boardservice"
)

// Every refusal the service can hand back is a REFUSAL — 422, "a rule
// refused the change" — while the default arm answers 502, "the forge
// could not be reached". A sentinel that misses apiError's list therefore
// tells the caller a lie about whose fault it is and invites a retry that
// cannot help: ErrPlanSubtask shipped exactly that way.
//
// The list of sentinels is read from the SOURCE, not from a table kept
// here: a table has to be updated by the same person who forgot the other
// one, which is no check at all. Every exported Err… in the package must
// appear in apiError, by name.
func TestEverySentinelIsAnsweredByApiError(t *testing.T) {
	names := exportedSentinelNames(t, "../../pkg/boardservice")
	if len(names) < 15 {
		t.Fatalf("only %d sentinels found — has the package moved?", len(names))
	}
	answered := readsSentinels(t, "api.go")
	var missing []string
	for _, n := range names {
		if !answered[n] {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("apiError does not name these, so they answer 502 — a rule that refused a change is not a forge failure: %s",
			strings.Join(missing, ", "))
	}
	// And the mapping is real, not just a mention: one sentinel end to end.
	if code := statusFor(t, boardservice.ErrPlanSubtask); code != 422 {
		t.Fatalf("ErrPlanSubtask answers %d, want 422", code)
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

// readsSentinels collects the boardservice.Err… names a file mentions.
func readsSentinels(t *testing.T, path string) map[string]bool {
	t.Helper()
	src, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	text := string(src)
	for i := 0; ; {
		j := strings.Index(text[i:], "boardservice.Err")
		if j < 0 {
			break
		}
		start := i + j + len("boardservice.")
		end := start
		for end < len(text) && (text[end] == '_' ||
			(text[end] >= 'a' && text[end] <= 'z') ||
			(text[end] >= 'A' && text[end] <= 'Z') ||
			(text[end] >= '0' && text[end] <= '9')) {
			end++
		}
		out[text[start:end]] = true
		i = end
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
