package board

import (
	"os"
	"regexp"
	"testing"
)

// The frontend keeps its own copy of the stage list (web/src/stages.ts), and
// it is load-bearing: the reader that builds a card from the wire DROPS a
// stage the copy does not name (STAGE_KEYS in web/src/api/resources.ts), so a
// stage added here and not there reaches the browser as no stage at all — the
// card then reads as ordinary work in progress, green bar and no clamp, which
// is exactly how the refuse stage first shipped.
//
// The TS test beside that reader cannot catch it: it iterates its OWN list, so
// a stage TypeScript has never heard of is invisible to it. The assertion has
// to be made from this side, where the list is authoritative.
func TestTheFrontendKnowsEveryStageTheDomainHas(t *testing.T) {
	src, err := os.ReadFile("../../web/src/stages.ts")
	if err != nil {
		t.Skipf("no frontend beside this package: %v", err)
	}
	order := regexp.MustCompile(`(?s)export const STAGE_ORDER: StageKey\[\] = \[(.*?)\]`).FindSubmatch(src)
	if order == nil {
		t.Fatal("STAGE_ORDER not found in web/src/stages.ts — the mirror moved, and this test with it")
	}
	named := map[string]bool{}
	for _, q := range regexp.MustCompile(`"([a-z]+)"`).FindAllStringSubmatch(string(order[1]), -1) {
		named[q[1]] = true
	}
	for _, stage := range StageOrder {
		if !named[string(stage)] {
			t.Fatalf("stage %q is in board.StageOrder and not in web/src/stages.ts: "+
				"a card wearing it reaches the browser as no stage at all", stage)
		}
	}
	// And nothing the other way: a name only the frontend has draws a stage
	// this board can never produce.
	for name := range named {
		// "done" is the one the frontend offers and the domain derives rather
		// than stores, so it is named there and absent from what is written.
		if name == "done" {
			continue
		}
		if StageFromName(name) == StageNone {
			t.Fatalf("web/src/stages.ts names %q, which this domain does not have", name)
		}
	}
}
