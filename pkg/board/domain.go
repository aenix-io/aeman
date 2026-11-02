package board

import "context"

// A domain is the repository a thing lives in. A board is an ordered list of
// domains; the first is the primary and is what "" names. Nothing is chosen
// per card: a card's domain follows one rule, linked cards first — a linked
// card has no domain of its own — then the project, then the team.

type domainCtxKey struct{}

// WithDomain carries the caller's choice of domain for a team or project
// being declared — asked only when more than one is writable. Cards never
// take one; a backend ignores the choice for them.
func WithDomain(ctx context.Context, domain string) context.Context {
	if domain == "" {
		return ctx
	}
	return context.WithValue(ctx, domainCtxKey{}, domain)
}

// DomainFrom is the caller's choice of domain, or "" for the default.
func DomainFrom(ctx context.Context) string {
	d, _ := ctx.Value(domainCtxKey{}).(string)
	return d
}

// DomainResolver answers where the things a card refers to live. A false ok
// means the reference is unknown and does not decide.
type DomainResolver interface {
	// CardDomain is the domain of another card by id.
	CardDomain(id string) (string, bool)
	// ProjectDomain is the domain a project was declared in.
	ProjectDomain(name string) (string, bool)
	// TeamDomain is the domain a team was declared in.
	TeamDomain(name string) (string, bool)
}

// DomainOf applies the rule, in order:
//
//  1. a review card lives where the card it reviews lives — its team is the
//     original's and its project is empty, so the team rule would leak a
//     review of a closed card into the shared repository;
//  2. a subtask — a task whose parent is a TASK — lives where its parent
//     lives, whatever column it carries;
//  3. an unlinked card filed under a project lives where the project lives;
//  4. any other card lives where its team lives.
//
// A hierarchy link does not place the child (H6): a task under an epic
// lives where its own project or team lives, and the resolver's
// PlanningResolver half is what tells the two parents apart. A resolver
// without it reads every parent as a task — which every parent was before
// the field existed.
//
// An unknown reference falls through to the next rule; nothing deciding
// means the primary domain, "".
func DomainOf(c Card, r DomainResolver) string {
	// A planning card — a goal, an initiative, an epic — has no team and no
	// project (H3): the people and the projects are on the tasks under it.
	// With nothing for rules 1-3 to read it is the primary's, and that is
	// said here rather than left to fall through, so a stray value a direct
	// writer put on one cannot move the whole tree's root into a closed
	// repository.
	if IsPlanning(c) {
		return ""
	}
	for _, ref := range []string{c.ReviewOf, c.Parent} {
		if ref == "" {
			continue
		}
		if ref == c.Parent {
			if pr, ok := r.(PlanningResolver); ok && pr.CardIsPlanning(ref) {
				continue
			}
		}
		if d, ok := r.CardDomain(ref); ok {
			return d
		}
	}
	if c.Project != "" {
		if d, ok := r.ProjectDomain(c.Project); ok {
			return d
		}
	}
	if c.Team != "" {
		if d, ok := r.TeamDomain(c.Team); ok {
			return d
		}
	}
	return ""
}

// TeamDomain is the repository a team was declared in, read in the board's
// ONE namespace (inPrimary): an unstamped entry answers with the PRIMARY's
// name, which is "" only on a board that does not name its own. A team the
// roster does not declare answers "" and decides nothing.
func TeamDomain(b Board, team string) string {
	if team == "" {
		return ""
	}
	st, ok := b.SprintStates[team]
	if !ok {
		return ""
	}
	return b.inPrimary(b.Domains[st.ItemID])
}

// ProjectDomain is the repository a project was declared in, in the same
// namespace: the PRIMARY's name for an unstamped entry, "" for a project
// the roster does not declare — and "" for the empty project NAME, which
// is not a project at all and decides nothing.
func ProjectDomain(b Board, project string) string {
	if project == "" {
		return ""
	}
	id, ok := b.ProjectStates[project]
	if !ok {
		return ""
	}
	return b.inPrimary(b.Domains[id])
}

// RosterConflict reports a team and a project that live in different
// repositories, and names both. It is the pair DomainOf cannot honour: the
// project decides where the card lives, so the team's people would
// not see a card carrying their name while the project's people would.
// Neither answer is right, so the pair is refused where it is made rather
// than resolved silently. A name the roster does not declare yet decides
// nothing — it will be declared in the card's own domain. (The project
// decides where the card lives: rule 3.)
func RosterConflict(b Board, team, project string) (teamDomain, projectDomain string, conflict bool) {
	if team == "" || project == "" {
		return TeamDomain(b, team), ProjectDomain(b, project), false
	}
	if _, ok := b.SprintStates[team]; !ok {
		return "", ProjectDomain(b, project), false
	}
	if _, ok := b.ProjectStates[project]; !ok {
		return TeamDomain(b, team), "", false
	}
	td, pd := TeamDomain(b, team), ProjectDomain(b, project)
	return td, pd, td != pd
}
