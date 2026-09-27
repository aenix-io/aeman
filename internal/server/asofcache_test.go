package server

import (
	"context"
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

func mutateHistoricalSnapshot(b *board.Board) {
	b.Cards[0].Title = "changed"
	b.Cards[0].Assignees[0] = "bob"
	b.Cards[0].Notes[0].Body = "rewritten"
	b.Cards[0].Mirrors[0].Project = "other"
	b.Tasks[0].Title = "changed task"
	b.SprintStates["alpha"] = board.SprintState{ItemID: "other"}
	b.People["ann"] = board.Person{Capacity: 99}
	b.TeamOrder[0] = "other"
	b.ProjectStates["proj"] = "other"
	b.Domains["s1"] = "other"
}

func assertHistoricalSnapshotUnchanged(t *testing.T, b board.Board) {
	t.Helper()
	if b.Cards[0].Title != "c1" || b.Cards[0].Assignees[0] != "ann" ||
		b.Cards[0].Notes[0].Body != "first" || b.Cards[0].Mirrors[0].Project != "p" {
		t.Fatalf("card containers changed through another snapshot: %+v", b.Cards[0])
	}
	if b.Tasks[0].Title != "t1" || b.SprintStates["alpha"].ItemID != "s1" ||
		b.People["ann"].Capacity != 20 || b.TeamOrder[0] != "alpha" ||
		b.ProjectStates["proj"] != "ps1" || b.Domains["s1"] != "primary" {
		t.Fatalf("board containers changed through another snapshot: %+v", b)
	}
}

func TestHistoricalCacheOwnsWhatItKeepsAndEachResult(t *testing.T) {
	c := newAsOfCache()
	source := fullBoard()
	c.put("day", source)

	mutateHistoricalSnapshot(&source)
	first, ok := c.get("day")
	if !ok {
		t.Fatal("the initial put was not retained")
	}
	assertHistoricalSnapshotUnchanged(t, first)

	mutateHistoricalSnapshot(&first)
	second, ok := c.get("day")
	if !ok {
		t.Fatal("the kept snapshot disappeared")
	}
	assertHistoricalSnapshotUnchanged(t, second)

	replacement := fullBoard()
	replacement.Title = "replacement"
	c.put("day", replacement)
	got, ok := c.get("day")
	if !ok || got.Title != "replacement" {
		t.Fatalf("replacement = %+v, %v", got, ok)
	}
	if _, ok := c.get("missing"); ok {
		t.Fatal("a missing key returned a snapshot")
	}
	c.put("empty", board.Board{})
	if empty, ok := c.get("empty"); !ok || empty.Cards != nil || empty.People != nil {
		t.Fatalf("empty snapshot = %+v, %v", empty, ok)
	}
}

func TestHistoricalFlightPublishesIndependentSnapshots(t *testing.T) {
	at := time.Date(2026, 8, 20, 23, 59, 59, 0, time.UTC)
	g := &gitSync{asOf: newAsOfCache()}
	be := &storeBackend{git: g}
	key := "at\x00" + g.asOfKey(at)
	flight, mine := g.asOf.begin(key)
	if !mine {
		t.Fatal("the initial flight was not ours")
	}

	follower := make(chan board.Board, 1)
	go func() {
		bd, _, _ := be.loadPast(context.Background(), "at", at,
			func(context.Context) (board.Board, bool, error) {
				panic("an in-flight follower must not run the load")
			})
		follower <- bd
	}()
	deadline := time.Now().Add(time.Second)
	for {
		g.asOf.mu.Lock()
		waiters := flight.waiters
		g.asOf.mu.Unlock()
		if waiters == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the follower did not join the in-flight historical read")
		}
	}

	producer := fullBoard()
	g.asOf.finish(key, flight, producer, true, nil)
	got := <-follower
	mutateHistoricalSnapshot(&producer)
	assertHistoricalSnapshotUnchanged(t, got)
	mutateHistoricalSnapshot(&got)
	assertHistoricalSnapshotUnchanged(t, flight.bd)
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
