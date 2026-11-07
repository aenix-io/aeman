package gitstore

import (
	"errors"
	"sort"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
)

// A board is an ordered list of domains, each a repository. The primary is
// first: its board.yaml names the board, and it is what a card without a
// domain of its own belongs to. LoadAll merges the domains a reader can see
// into one snapshot — rosters are fragments merged by rank, duplicate names
// resolve to the oldest declaration, and a card caught between two domains
// shows once.

// Domain is one repository of a board.
type Domain struct {
	Name string
	Repo *Repo
}

// ErrNoDomains is returned when a board has nothing to read.
var ErrNoDomains = errors.New("gitstore: a board needs at least its primary domain")

// ErrNameTaken refuses a roster name — a team — that
// some domain of the board already declares. Names are one namespace across
// the board: a card refers to its team by name, and the domain
// rule resolves them by name, so two of a kind would leave a card's home
// undecidable. The store checks every domain, readable by the caller or not;
// the read-side alias merge is left for collisions made behind its back.
var ErrNameTaken = errors.New("gitstore: name already declared on the board")

// LoadAll reads every domain at its branch tip and merges the snapshots.
func LoadAll(domains []Domain) (Snapshot, error) {
	return loadAllOverlaid(domains, nil)
}

// loadAllOverlaid is LoadAll with an action's staged writes laid over each
// domain's tip (loadOverlaid); staged answers a repository's staged files,
// nil for none. Nil staged is LoadAll.
func loadAllOverlaid(domains []Domain, staged func(*Repo) map[string][]byte) (Snapshot, error) {
	if len(domains) == 0 {
		return Snapshot{}, ErrNoDomains
	}
	var parts []Snapshot
	for i, d := range domains {
		var s Snapshot
		var err error
		if staged != nil {
			s, err = loadOverlaid(d.Repo, staged(d.Repo))
		} else {
			s, err = Load(d.Repo)
		}
		if err != nil {
			if i == 0 || !errors.Is(err, ErrEmptyRepository) {
				return Snapshot{}, err
			}
			continue // an unborn secondary contributes nothing yet
		}
		stamp(&s, d.Name)
		if i > 0 {
			s.Board = BoardFile{} // only the primary names the board
		}
		parts = append(parts, s)
	}
	return merge(parts), nil
}

// LoadAllAsOf is LoadAll at a past moment: every domain at the tree it had
// when that moment passed. ok is false unless EVERY domain can answer — a
// board missing one repository's half of that day is not that day's board,
// it is a smaller board that never existed.
func LoadAllAsOf(domains []Domain, at time.Time) (Snapshot, bool, error) {
	if len(domains) == 0 {
		return Snapshot{}, false, ErrNoDomains
	}
	var parts []Snapshot
	for i, d := range domains {
		s, ok, err := LoadAsOf(d.Repo, at)
		if err != nil {
			if i == 0 || !errors.Is(err, ErrEmptyRepository) {
				return Snapshot{}, false, err
			}
			continue // an unborn secondary contributes nothing yet
		}
		if !ok {
			return Snapshot{}, false, nil
		}
		stamp(&s, d.Name)
		if i > 0 {
			s.Board = BoardFile{} // only the primary names the board
		}
		parts = append(parts, s)
	}
	return merge(parts), true, nil
}

// LoadAllAsOfDay is LoadAllAsOf for a DAY: every domain at the tree it had
// when the day ended, plus the cards each of them removed during it (see
// LoadAsOfDay). ok follows LoadAllAsOf — every domain must be able to
// answer, or the day is not that board's day.
func LoadAllAsOfDay(domains []Domain, from, to time.Time) (Snapshot, bool, error) {
	if len(domains) == 0 {
		return Snapshot{}, false, ErrNoDomains
	}
	var parts []Snapshot
	for i, d := range domains {
		s, ok, err := LoadAsOfDay(d.Repo, from, to)
		if err != nil {
			if i == 0 || !errors.Is(err, ErrEmptyRepository) {
				return Snapshot{}, false, err
			}
			continue
		}
		if !ok {
			return Snapshot{}, false, nil
		}
		stamp(&s, d.Name)
		if i > 0 {
			s.Board = BoardFile{}
		}
		parts = append(parts, s)
	}
	return merge(parts), true, nil
}

// stamp marks every entry of a snapshot with its domain.
func stamp(s *Snapshot, domain string) {
	for i := range s.Cards {
		s.Cards[i].Domain = domain
	}
	for i := range s.Teams {
		s.Teams[i].Domain = domain
	}
}

// merge folds the parts into one snapshot.
func merge(parts []Snapshot) Snapshot {
	// People and customers are declared in the primary.
	out := Snapshot{Board: parts[0].Board, Users: parts[0].Users, Customers: parts[0].Customers}
	for _, p := range parts {
		out.Unknown = append(out.Unknown, p.Unknown...)
		out.Broken = append(out.Broken, p.Broken...)
	}
	out.Cards, out.Ghosts = mergeCards(parts)
	out.Teams, out.Aliases = mergeTeams(parts, out.Aliases)
	return out
}

// mergeCards keeps one copy per id. Two copies are a move in flight: the
// one whose movedFrom names the other's domain is current.
func mergeCards(parts []Snapshot) ([]board.Card, []Ghost) {
	byID := map[string]board.Card{}
	var ghosts []Ghost
	for _, p := range parts {
		for _, c := range p.Cards {
			prev, seen := byID[c.ItemID]
			if !seen {
				byID[c.ItemID] = c
				continue
			}
			switch {
			case c.MovedFrom == prev.Domain:
				ghosts = append(ghosts, Ghost{ID: prev.ItemID, Domain: prev.Domain, Current: c.Domain})
				byID[c.ItemID] = c
			case prev.MovedFrom == c.Domain:
				ghosts = append(ghosts, Ghost{ID: c.ItemID, Domain: c.Domain, Current: prev.Domain})
			default:
				// Neither says it moved: keep the first (the primary side
				// wins), record the other as a ghost so health can say so.
				ghosts = append(ghosts, Ghost{ID: c.ItemID, Domain: c.Domain})
			}
		}
	}
	cards := make([]board.Card, 0, len(byID))
	for _, c := range byID {
		cards = append(cards, c)
	}
	sort.Slice(cards, func(i, j int) bool { return byRank(cards[i].Rank, cards[i].ItemID, cards[j].Rank, cards[j].ItemID) })
	return cards, ghosts
}

// older reports whether a was declared before b (ties by id).
func older(aCreated, aID, bCreated, bID string) bool {
	if aCreated != bCreated {
		return aCreated < bCreated
	}
	return aID < bID
}

func mergeTeams(parts []Snapshot, aliases []Alias) ([]Team, []Alias) {
	byName := map[string]Team{}
	for i, p := range parts {
		for _, t := range p.Teams {
			if t.Name == "" && i > 0 {
				// The no-team group is the primary's; `aeman init` writes a
				// `_` file into every repository and those are not aliases.
				continue
			}
			prev, seen := byName[t.Name]
			if !seen {
				byName[t.Name] = t
				continue
			}
			if older(t.Created, t.ID, prev.Created, prev.ID) {
				aliases = append(aliases, Alias{Kind: "team", Name: t.Name, Domain: prev.Domain, ID: prev.ID, Winner: t.ID})
				byName[t.Name] = t
			} else {
				aliases = append(aliases, Alias{Kind: "team", Name: t.Name, Domain: t.Domain, ID: t.ID, Winner: prev.ID})
			}
		}
	}
	teams := make([]Team, 0, len(byName))
	for _, t := range byName {
		teams = append(teams, t)
	}
	sort.Slice(teams, func(i, j int) bool { return byRank(teams[i].Rank, teams[i].ID, teams[j].Rank, teams[j].ID) })
	return teams, aliases
}
