package design

// The behaviour matrix's Test column names the tests that pin each rule.
// A reference that no longer resolves is worse than no name: it reads as
// coverage that is not there, and this branch shipped one after a rule was
// reversed and its test renamed. Cheap to check, so checked.

import (
	"fmt"
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
	named := matrixReferences(string(matrix))
	if len(named) == 0 {
		t.Fatal("the matrix names no tests at all — has the format changed?")
	}
	have, err := collectMatrixTests(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range named {
		if problem := matrixReferenceProblem(ref, have); problem != "" {
			t.Error(problem)
		}
	}
}

type testRef struct {
	Group string
	Name  string
}

// The directory basename is the matrix's package label, except that the
// command package is filed under cmd rather than the executable name aeman.
func matrixGroup(dir string) string {
	if filepath.ToSlash(dir) == "cmd/aeman" {
		return "cmd"
	}
	return filepath.Base(dir)
}

// These are abbreviations used alongside the full package names in the matrix.
func matrixGroupAlias(group string) string {
	switch group {
	case "service":
		return "boardservice"
	case "mcp":
		return "mcpserver"
	case "fake":
		return "boardservicetest"
	default:
		return group
	}
}

func matrixReferences(matrix string) []testRef {
	code := regexp.MustCompile("`([^`]+)`")
	testName := regexp.MustCompile(`^Test[A-Za-z0-9_]+$`)
	groupName := regexp.MustCompile(`^[a-z][a-z0-9_/]*$`)
	var refs []testRef
	for _, line := range strings.Split(matrix, "\n") {
		// Reset at cell boundaries: the Lives in column and rule prose do
		// not label the tests. Ungrouped legacy citations stay ungrouped.
		for _, cell := range strings.Split(line, "|") {
			group, end := "", 0
			for _, m := range code.FindAllStringSubmatchIndex(cell, -1) {
				prefix := cell[end:m[0]]
				name := cell[m[2]:m[3]]
				end = m[1]
				// Only test references (including wildcard families) can
				// introduce a group; unrelated code spans are annotations.
				if !testName.MatchString(strings.TrimSuffix(name, "*")) {
					continue
				}
				prefix = prefix[strings.LastIndexAny(prefix, ",;")+1:]
				fields := strings.Fields(prefix)
				if len(fields) > 0 && (fields[0] == "✅" || fields[0] == "🆕") {
					fields = fields[1:]
				}
				// A label starts a group; commas and semicolons without
				// one continue it. Old file citations like service_test
				// are not package labels. Keep unknown labels so typos fail.
				if len(fields) == 1 && groupName.MatchString(fields[0]) && !strings.HasSuffix(fields[0], "_test") {
					group = matrixGroupAlias(fields[0])
				}
				if testName.MatchString(name) {
					refs = append(refs, testRef{Group: group, Name: name})
				}
			}
		}
	}
	return refs
}

func collectMatrixTests(root string) (map[testRef]bool, error) {
	// The tree's own _test.go files, and only those: a walk rather than a
	// grep, so the check does not depend on an external binary and cannot
	// be satisfied by a stray match in node_modules or a build directory.
	decl := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
	have := map[testRef]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
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
		dir, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		for _, m := range decl.FindAllStringSubmatch(string(src), -1) {
			have[testRef{Group: matrixGroup(dir), Name: m[1]}] = true
		}
		return nil
	})
	return have, err
}

func matrixReferenceProblem(ref testRef, have map[testRef]bool) string {
	if have[ref] {
		return ""
	}
	var groups []string
	for candidate := range have {
		if candidate.Name == ref.Name {
			// Older citations name no package. Preserve their existence
			// check without guessing a group from the test's location.
			if ref.Group == "" {
				return ""
			}
			groups = append(groups, candidate.Group)
		}
	}
	if len(groups) > 0 {
		sort.Strings(groups)
		return fmt.Sprintf("behavior matrix cites %s under %s, but the test exists under %s", ref.Name, ref.Group, strings.Join(groups, ", "))
	}
	return fmt.Sprintf("behavior matrix cites %s, but no such test exists", strings.TrimSpace(ref.Group+" "+ref.Name))
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
	known := map[string]bool{"M1": true, "M2": true, "M3": true, "P9": true, "V1": true}
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
