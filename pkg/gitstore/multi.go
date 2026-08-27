package gitstore

import (
	"errors"
	"sort"

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

// LoadAll reads every domain at its branch tip and merges the snapshots.
func LoadAll(domains []Domain) (Snapshot, error) {
	if len(domains) == 0 {
		return Snapshot{}, ErrNoDomains
	}
	var parts []Snapshot
	for i, d := range domains {
		s, err := Load(d.Repo)
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

// stamp marks every entry of a snapshot with its domain.
func stamp(s *Snapshot, domain string) {
	for i := range s.Cards {
		s.Cards[i].Domain = domain
	}
	for i := range s.Teams {
		s.Teams[i].Domain = domain
	}
	for i := range s.Projects {
		s.Projects[i].Domain = domain
		for j := range s.Projects[i].Epics {
			s.Projects[i].Epics[j].Domain = domain
		}
		for j := range s.Projects[i].Deadlines {
			s.Projects[i].Deadlines[j].Domain = domain
		}
	}
	for i := range s.Processes {
		s.Processes[i].Domain = domain
		for j := range s.Processes[i].Tasks {
			s.Processes[i].Tasks[j].Domain = domain
			s.Processes[i].Tasks[j].Card.Domain = domain
		}
	}
}

// merge folds the parts into one snapshot.
func merge(parts []Snapshot) Snapshot {
	out := Snapshot{Board: parts[0].Board}
	for _, p := range parts {
		out.Unknown = append(out.Unknown, p.Unknown...)
		out.Broken = append(out.Broken, p.Broken...)
	}
	out.Cards, out.Ghosts = mergeCards(parts)
	out.Teams, out.Aliases = mergeTeams(parts, out.Aliases)
	out.Projects, out.Aliases = mergeProjects(parts, out.Aliases)
	out.Processes, out.Aliases = mergeProcesses(parts, out.Aliases)
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

// mergeProjects resolves duplicate names to the oldest and folds an alias
// project's columns and deadline lines into the winner's.
func mergeProjects(parts []Snapshot, aliases []Alias) ([]Project, []Alias) {
	byName := map[string]*Project{}
	for _, p := range parts {
		for _, pr := range p.Projects {
			pr := pr
			prev, seen := byName[pr.Name]
			if !seen {
				byName[pr.Name] = &pr
				continue
			}
			winner, loser := prev, &pr
			if older(pr.Created, pr.ID, prev.Created, prev.ID) {
				winner, loser = &pr, prev
			}
			winner.Epics = append(winner.Epics, loser.Epics...)
			winner.Deadlines = append(winner.Deadlines, loser.Deadlines...)
			aliases = append(aliases, Alias{Kind: "project", Name: pr.Name, Domain: loser.Domain, ID: loser.ID, Winner: winner.ID})
			byName[pr.Name] = winner
		}
	}
	projects := make([]Project, 0, len(byName))
	for _, pr := range byName {
		sort.Slice(pr.Epics, func(i, j int) bool { return byRank(pr.Epics[i].Rank, pr.Epics[i].ID, pr.Epics[j].Rank, pr.Epics[j].ID) })
		sort.Slice(pr.Deadlines, func(i, j int) bool {
			return byRank(pr.Deadlines[i].Week, pr.Deadlines[i].ID, pr.Deadlines[j].Week, pr.Deadlines[j].ID)
		})
		projects = append(projects, *pr)
	}
	sort.Slice(projects, func(i, j int) bool { return byRank(projects[i].Rank, projects[i].ID, projects[j].Rank, projects[j].ID) })
	return projects, aliases
}

func mergeProcesses(parts []Snapshot, aliases []Alias) ([]Process, []Alias) {
	byName := map[string]*Process{}
	for _, p := range parts {
		for _, pc := range p.Processes {
			pc := pc
			prev, seen := byName[pc.Name]
			if !seen {
				byName[pc.Name] = &pc
				continue
			}
			winner, loser := prev, &pc
			if older(pc.Created, pc.ID, prev.Created, prev.ID) {
				winner, loser = &pc, prev
			}
			winner.Tasks = append(winner.Tasks, loser.Tasks...)
			aliases = append(aliases, Alias{Kind: "process", Name: pc.Name, Domain: loser.Domain, ID: loser.ID, Winner: winner.ID})
			byName[pc.Name] = winner
		}
	}
	processes := make([]Process, 0, len(byName))
	for _, pc := range byName {
		sort.Slice(pc.Tasks, func(i, j int) bool {
			return byRank(pc.Tasks[i].Card.Rank, pc.Tasks[i].ID, pc.Tasks[j].Card.Rank, pc.Tasks[j].ID)
		})
		processes = append(processes, *pc)
	}
	sort.Slice(processes, func(i, j int) bool {
		return byRank(processes[i].Rank, processes[i].ID, processes[j].Rank, processes[j].ID)
	})
	return processes, aliases
}
