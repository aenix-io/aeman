package board

// A mirror is the same card standing in a second Project-board column: one
// file, one log, one set of dates — shown in both places. The card's own
// (project, epic) pair stays its home and keeps deciding where it lives
// (DomainOf reads the primary, never a mirror); mirrors only add columns
// that show it. They exist so that work shared by two projects is one card
// on one person, not a duplicate per project drifting apart.

// Placement names a Project-board column: the (project, epic) pair. A
// column is the pair — epic names repeat across projects.
type Placement struct {
	Project string `json:"project"`
	Epic    string `json:"epic"`
}

// Mirrored reports whether the pair is one of the card's MIRROR placements
// — not its home. InEpic answers "does the card stand in this column";
// promotion, unmirroring and duplicate checks need the narrower question.
func Mirrored(c Card, project, epic string) bool {
	for _, m := range c.Mirrors {
		if m.Project == project && m.Epic == epic {
			return true
		}
	}
	return false
}

// ProcessDomain is the repository a process was declared in, read in the
// board's ONE namespace (inPrimary): an unstamped entry answers with the
// PRIMARY's name, which is "" only on a board that does not name its own.
// A process the roster does not declare answers "" and decides nothing.
func ProcessDomain(b Board, name string) string {
	for _, p := range b.Processes {
		if p.Name == name {
			return b.inPrimary(b.Domains[p.ItemID])
		}
	}
	return ""
}

// ColumnDomain is the repository a COLUMN belongs to — the domain of the
// epic stub that declares it, not of its project. The two agree wherever a
// project owns the column (a project and its columns live together), but
// the NO-PROJECT bucket is a real column with no project to ask: reading
// the project there answers "no such repository" for every card in it.
// Reports false only for a column the roster does not declare at all.
func ColumnDomain(b Board, project, epic string) (string, bool) {
	col, ok := FindEpic(b, project, epic)
	if !ok {
		return "", false
	}
	return b.inPrimary(b.Domains[col.ItemID]), true
}

// inPrimary reads an entry's stamp in ONE namespace: an unstamped entry
// belongs to the primary, which the store names explicitly. Every "is this
// the same repository" question must go through it, or a stamped primary
// ("board") is compared against an unstamped one ("") and answers no.
func (b Board) inPrimary(domain string) string {
	if domain == "" {
		return b.Primary
	}
	return domain
}

// HomeDomain is the repository that HOLDS a card, in the same namespace:
// the placement rule's answer (linked cards first, G14), with "nothing
// places this card" read as the primary — which is where such a card's
// file goes.
func HomeDomain(b Board, c Card) string {
	// A PERSONAL card is placed by nothing — no team, no project, no link
	// — so the placement rule says "" for it and only its file knows whose
	// board it is. Every other card is answered by the rule, because the
	// question is always where the card will BE after a change, not where
	// its file happens to sit now.
	if IsPersonalDomain(c.Domain) {
		return c.Domain
	}
	return b.inPrimary(DomainOf(c, Resolver(b, b.Primary)))
}
