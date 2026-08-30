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
func TestNoTwoRulesShareAnID(t *testing.T) {
	matrix, err := os.ReadFile("behavior-matrix.md")
	if err != nil {
		t.Fatal(err)
	}
	// Collisions older than this test, in blocks nothing cites by id from
	// the code. They are recorded rather than fixed — renumbering a row
	// rewrites every citation of it, and these have none — but the list
	// does not grow: a NEW collision fails here, and one that is cleaned
	// up fails here too, so the list cannot go stale either way.
	known := map[string]bool{"M1": true, "M2": true, "M3": true, "P9": true, "V1": true, "V2": true}
	seen := map[string]int{}
	for _, line := range strings.Split(string(matrix), "\n") {
		m := regexp.MustCompile(`^\| ([A-Z][0-9]+) \|`).FindStringSubmatch(line)
		if m == nil {
			continue
		}
		seen[m[1]]++
	}
	var dup, fixed []string
	for id, n := range seen {
		switch {
		case n > 1 && !known[id]:
			dup = append(dup, id)
		case n == 1 && known[id]:
			fixed = append(fixed, id)
		}
	}
	if len(dup) > 0 {
		sort.Strings(dup)
		t.Fatalf("these ids name more than one rule, so every citation of them is ambiguous: %s",
			strings.Join(dup, ", "))
	}
	if len(fixed) > 0 {
		sort.Strings(fixed)
		t.Fatalf("these ids are no longer duplicated — take them out of the known list: %s",
			strings.Join(fixed, ", "))
	}
}
