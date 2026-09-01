package server

import (
	"testing"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
)

// The three reads of one date flip — the cards, the sprint pointers and the
// roster — must not pull history three times, and a day the remote itself
// does not hold must not pull it again on every request for as long as that
// date stays on screen. Each read is a commit walk plus a full parse of every
// tree; unpaced, one held-down arrow key is a fetch storm.
func TestHistoryIsPulledOncePerDay(t *testing.T) {
	c := newAsOfCache()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	day := "2026-08-20T23:59:59Z"

	if !c.shouldDeepen(day, now) {
		t.Fatal("the first read of a day the clone does not reach pulls history")
	}
	// The other two reads of the same flip ride the first.
	if c.shouldDeepen(day, now.Add(time.Second)) || c.shouldDeepen(day, now.Add(2*time.Second)) {
		t.Fatal("the same day pulled history again within the same flip")
	}
	// And a day the remote does not hold does not keep pulling.
	if c.shouldDeepen(day, now.Add(deepenRetry-time.Second)) {
		t.Fatal("a day that could not be reached pulled again too soon")
	}
	if !c.shouldDeepen(day, now.Add(deepenRetry+time.Second)) {
		t.Fatal("after the wait it may try again — the remote may have more by then")
	}
	// Another day is another question.
	if !c.shouldDeepen("2026-08-19T23:59:59Z", now) {
		t.Fatal("a different day answers for itself")
	}
	// Once the history covers it, the attempt is forgotten: a later failure
	// must not be held back by an earlier one.
	c.reached(day)
	if !c.shouldDeepen(day, now) {
		t.Fatal("a day the history reached keeps no record of the attempt")
	}
}

// A kept board is served while nothing has changed, and the shelf stays
// small: a person flipping through history walks a few days, not a month.
func TestAKeptDayIsServedAndTheShelfStaysSmall(t *testing.T) {
	c := newAsOfCache()
	c.put("k1", board.Board{Board: "one"})
	if bd, ok := c.get("k1"); !ok || bd.Board != "one" {
		t.Fatalf("kept = %+v, %v", bd, ok)
	}
	if _, ok := c.get("k2"); ok {
		t.Fatal("a key nobody kept must not answer")
	}
	for i := range asOfKept + 3 {
		c.put(string(rune('a'+i)), board.Board{})
	}
	if _, ok := c.get("k1"); ok {
		t.Fatal("the oldest entry must fall off a full shelf")
	}
	c.mu.Lock()
	held := len(c.kept)
	c.mu.Unlock()
	if held > asOfKept {
		t.Fatalf("the shelf holds %d days, at most %d", held, asOfKept)
	}
}
