package server

import (
	"sync"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// Flipping to a past day fires three requests at once — the cards, the roster
// and the sprint pointers — and each of them missed a cache the others had not
// filled yet, so one cold day was read three times over, in parallel, for one
// answer. The first read is the one the others wait on.
func TestOneColdDayIsReadOnceForEveryoneAskingAtOnce(t *testing.T) {
	c := newAsOfCache()
	const key = "day\x00a-day"
	answer := board.Board{Board: "acme"}

	var reads int
	var mu sync.Mutex
	// The read is under way until the test lets it finish, which is what a
	// cold day is: slow enough for the others to arrive while it runs.
	release := make(chan struct{})
	var arrived, done sync.WaitGroup
	got := make([]board.Board, 8)

	for i := range got {
		arrived.Add(1)
		done.Add(1)
		go func() {
			defer done.Done()
			fl, mine := c.begin(key)
			arrived.Done()
			if !mine {
				<-fl.done
				got[i] = fl.bd
				return
			}
			mu.Lock()
			reads++
			mu.Unlock()
			<-release
			c.finish(key, fl, answer, true, nil)
			got[i] = answer
		}()
	}
	arrived.Wait() // everyone has asked
	close(release) // now the one read finishes
	done.Wait()

	if reads != 1 {
		t.Fatalf("%d reads for one day asked for by eight callers at once", reads)
	}
	for i, bd := range got {
		if bd.Board != "acme" {
			t.Fatalf("caller %d got %+v, want the answer the flight carried", i, bd)
		}
	}
	// And nothing is left in flight afterwards.
	if _, mine := c.begin(key); !mine {
		t.Fatal("a finished flight is still held")
	}
}
