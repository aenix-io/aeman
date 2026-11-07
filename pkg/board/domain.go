package board

import "context"

// A domain is the repository a thing lives in. A board is an ordered list of
// domains; the first is the primary and is what "" names. Nothing is chosen
// per card: a card's domain follows one rule, linked cards first — a linked
// card has no domain of its own — then the team.

type domainCtxKey struct{}

// WithDomain carries the caller's choice of domain for a team being
// declared — asked only when more than one is writable. Cards never
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
	// TeamDomain is the domain a team was declared in.
	TeamDomain(name string) (string, bool)
}

// DomainOf applies the rule, in order:
//
//  1. a review card lives where the card it reviews lives — the team rule
//     alone would leak a review of a closed card into the shared repository;
//  2. a subtask — a task whose parent is a TASK — lives where its parent
//     lives;
//  3. any other card lives where its team lives.
//
// A hierarchy link does not place the child (H6): a task under an epic
// lives where its own team lives, and the resolver's
// PlanningResolver half is what tells the two parents apart. A resolver
// without it reads every parent as a task — which every parent was before
// the field existed.
//
// An unknown reference falls through to the next rule; nothing deciding
// means the primary domain, "".
func DomainOf(c Card, r DomainResolver) string {
	// A planning card — a goal, an initiative, an epic — is placed by
	// nothing (H3): its team says who carries the plan item out (O1). With
	// nothing for rules 1-2 to read it is the primary's, and
	// that is said here rather than left to fall through, so its team — or a
	// stray value a direct writer put on one — cannot move the whole tree's
	// root into a closed repository.
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
