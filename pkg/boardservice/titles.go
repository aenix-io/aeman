package boardservice

import (
	"context"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
)

// titleRetryEvery is how long the sweep waits before asking the forge about a
// reference it could not resolve. A dead link — a deleted issue, a private
// repository, a forge that is down — must not become a request every fifteen
// seconds for the life of the process, and a link that failed once because
// the forge was busy deserves another go without anybody's help.
const titleRetryEvery = 30 * time.Minute

// titlesPerSweep bounds one pass. A board that has just been migrated could
// hold hundreds of unresolved references, and housekeeping must not turn into
// a burst of forge requests; what is left waits for the next tick.
const titlesPerSweep = 20

// unresolvedTitle reports the reference a card is still WAITING on: the first
// GitHub reference in its body, when the card's title is exactly the fallback
// that reference makes ("Issue: owner/repo#N").
//
// The exact match is the whole guard. A person's own words never look like a
// fallback, so nothing anybody wrote can be overwritten by a sweep — the same
// rule the create's own resolve applies before it renames, and for the same
// reason: their words win over ours.
func unresolvedTitle(c board.Card) (board.Link, bool) {
	if c.Title == "" || c.Description == "" {
		return board.Link{}, false
	}
	for _, l := range board.ExtractLinks(c.Description) {
		if l.IsGitHubRef() && l.FallbackTitle() == c.Title {
			return l, true
		}
	}
	return board.Link{}, false
}

// ResolveOpenTitles finishes what a create-by-URL started: it renames the
// cards still wearing the readable fallback to the title of the issue or pull
// request they name.
//
// The create resolves in the background already (resolveTitleAsync), but that
// goroutine gets ONE try in a process that may be restarted, deployed over or
// unable to write, and it says nothing when it fails: five cards on the
// production board wore "Issue: owner/repo#N" for a week — three of them
// somebody's open work — and no gesture in the UI would ever have finished
// them. This is the second try, and it runs on the board's own tick, so the
// job outlives whatever lost it.
//
// It is HOUSEKEEPING: a reference it cannot resolve is left as it is (the
// fallback is a usable card, which is why it exists) and reported by the
// caller's log, never as a failed tick.
func (s *Service) ResolveOpenTitles(ctx context.Context, boardID string) (done, waiting int, err error) {
	resolver, ok := s.backend.(LinkResolver)
	if !ok {
		return 0, 0, nil
	}
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return 0, 0, err
	}
	left := titlesPerSweep
	for _, c := range b.Cards {
		if left == 0 {
			break
		}
		link, unfinished := unresolvedTitle(c)
		if !unfinished {
			continue
		}
		waiting++
		if !s.tryTitleNow(c.ItemID) {
			continue
		}
		left--
		resolved, rerr := resolver.ResolveIssueRef(ctx, link)
		if rerr != nil || resolved.Title == "" || resolved.Title == link.FallbackTitle() {
			continue
		}
		// The board was read before the round trip: re-read the card, so a
		// rename that landed meanwhile (a person's own words, or the create's
		// own resolve arriving late) is not undone by this one.
		fresh, ferr := s.backend.LoadBoard(ctx, boardID)
		if ferr != nil {
			return done, waiting, ferr
		}
		current, found := findCard(fresh, c.ItemID)
		if !found || current.Title != c.Title {
			continue
		}
		if rnerr := s.backend.RenameCard(ctx, fresh, current, resolved.Title); rnerr != nil {
			return done, waiting, rnerr
		}
		done++
		waiting--
	}
	return done, waiting, nil
}

// tryTitleNow says whether this card's reference may be asked about now, and
// records the attempt. The ledger holds only the cards that are still
// unresolved, so it is as small as the problem.
func (s *Service) tryTitleNow(itemID string) bool {
	s.titleMu.Lock()
	defer s.titleMu.Unlock()
	now := s.clock()
	if last, seen := s.titleTried[itemID]; seen && now.Sub(last) < titleRetryEvery {
		return false
	}
	if s.titleTried == nil {
		s.titleTried = map[string]time.Time{}
	}
	s.titleTried[itemID] = now
	return true
}

// clock is time.Now unless a test replaced it.
func (s *Service) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}
