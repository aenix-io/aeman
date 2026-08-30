package design

// The behaviour matrix's third column names the test that pins each rule.
// A name that no longer resolves is worse than no name: it reads as
// coverage that is not there, and this branch shipped one after a rule was
// reversed and its test renamed. Cheap to check, so checked.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestEveryTestTheMatrixNamesExists(t *testing.T) {
	matrix, err := os.ReadFile("behavior-matrix.md")
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	// Every `TestSomething` in backticks, and every Go test function in the
	// tree. The web cases the matrix cites are prose, not identifiers, so
	// only the Go shape is checked here.
	named := regexp.MustCompile("`(Test[A-Za-z0-9_]+)`").FindAllStringSubmatch(string(matrix), -1)
	if len(named) == 0 {
		t.Fatal("the matrix names no tests at all — has the format changed?")
	}
	out, err := exec.Command("grep", "-rhoE", `^func (Test[A-Za-z0-9_]+)`, root).Output()
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if name := strings.TrimPrefix(line, "func "); name != "" {
			have[name] = true
		}
	}
	var missing []string
	for _, m := range named {
		if !have[m[1]] {
			missing = append(missing, m[1])
		}
	}
	if len(missing) > 0 {
		t.Fatalf("the matrix names tests that do not exist: %s", strings.Join(missing, ", "))
	}
}
