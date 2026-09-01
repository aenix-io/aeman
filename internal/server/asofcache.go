package server

import (
	"sync"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
)

// Reading a past day is a walk of the commit chain plus a full parse of every
// domain's tree — the one read the in-memory cache was built to avoid. And a
// day is read THREE times per flip of the date arrow: the cards, the sprint
// pointers and the roster all carry the same selector. So the answer is kept
// while nothing has changed, and the on-demand deepen behind it is not
// repeated per request.

// asOfKept is how many past days are held at once: a person flipping through
// history walks a few days back and forth, not a month.
const asOfKept = 8

// deepenRetry is how long a day that could not be reached waits before the
// history is pulled for it again. Without it a day beyond what the REMOTE
// holds — or one whose fetch fails — deepens on every single request, for as
// long as somebody keeps that date on screen.
const deepenRetry = 10 * time.Minute

// asOfCache holds the boards of past days, keyed by the moment AND by what
// every domain's branch pointed at when it was read: a commit landing (or a
// fetch bringing one in) makes a new key, so nothing stale is ever served.
type asOfCache struct {
	mu    sync.Mutex
	kept  map[string]board.Board
	order []string
	// deepened is when the history was last pulled for a day it did not
	// reach, by that day's boundary.
	deepened map[string]time.Time
}

func newAsOfCache() *asOfCache {
	return &asOfCache{kept: map[string]board.Board{}, deepened: map[string]time.Time{}}
}

// get returns a kept board for the key, if there is one.
func (c *asOfCache) get(key string) (board.Board, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	bd, ok := c.kept[key]
	return bd, ok
}

// put keeps a board, dropping the oldest once the shelf is full.
func (c *asOfCache) put(key string, bd board.Board) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, seen := c.kept[key]; !seen {
		c.order = append(c.order, key)
		for len(c.order) > asOfKept {
			delete(c.kept, c.order[0])
			c.order = c.order[1:]
		}
	}
	c.kept[key] = bd
}

// shouldDeepen answers whether to pull history for a day the clone does not
// reach — once per day, then not again for deepenRetry. The three reads of
// one date flip arrive together, and a day the remote simply does not have
// would otherwise fetch on every request forever.
func (c *asOfCache) shouldDeepen(day string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if last, tried := c.deepened[day]; tried && now.Sub(last) < deepenRetry {
		return false
	}
	c.deepened[day] = now
	return true
}

// reached forgets a day's deepen attempt once the history covers it, so a
// later day is not held back by an earlier failure.
func (c *asOfCache) reached(day string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.deepened, day)
}
