package design

// The behaviour matrix's third column names the test that pins each rule.
// A name that no longer resolves is worse than no name: it reads as
// coverage that is not there, and this branch shipped one after a rule was
// reversed and its test renamed. Cheap to check, so checked.

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
	// Every `TestSomething` in backticks. The web cases the matrix cites are
	// prose, not identifiers, so only the Go shape is checked here.
	named := regexp.MustCompile("`(Test[A-Za-z0-9_]+)`").FindAllStringSubmatch(string(matrix), -1)
	if len(named) == 0 {
		t.Fatal("the matrix names no tests at all — has the format changed?")
	}
	// The tree's own _test.go files, and only those: a walk rather than a
	// grep, so the check does not depend on an external binary and cannot
	// be satisfied by a stray match in node_modules or a build directory.
	decl := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
	have := map[string]bool{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			// .claude holds this repository's worktrees — full copies on
			// other branches. A name that exists only there would satisfy
			// a check whose whole point is catching a name that exists
			// nowhere in THIS tree.
			case ".git", ".claude", "node_modules", "dist", "vendor":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range decl.FindAllStringSubmatch(string(src), -1) {
			have[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
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

// A rule's ID is how every other document cites it, so two rows sharing
// one make a citation ambiguous — and this branch spent a commit moving a
// row off an id that was already taken. New rows must not add to that.
func TestNoTwoRulesShareAnIDInTheGitBlock(t *testing.T) {
	matrix, err := os.ReadFile("behavior-matrix.md")
	if err != nil {
		t.Fatal(err)
	}
	// The G-block is this line's own; the older prefixes carry collisions
	// that predate it and are not this test's to relitigate.
	seen := map[string]int{}
	for _, line := range strings.Split(string(matrix), "\n") {
		m := regexp.MustCompile(`^\| (G[0-9]+) \|`).FindStringSubmatch(line)
		if m == nil {
			continue
		}
		seen[m[1]]++
	}
	var dup []string
	for id, n := range seen {
		if n > 1 {
			dup = append(dup, id)
		}
	}
	if len(dup) > 0 {
		sort.Strings(dup)
		t.Fatalf("these ids name more than one rule: %s", strings.Join(dup, ", "))
	}
}
