package board

// Which repository HOLDS a card is answered in ONE namespace —
// the primary named explicitly — because the store stamps every entry with
// its domain's name, the primary's included, while an unstamped entry means
// the primary too; comparing a raw stamp against an unstamped one is how a
// named primary and "" once became two repositories.

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

// PrimaryDomain is the primary repository's name in the board's ONE
// namespace — what an unstamped entry resolves to. It is the repository
// every reader of the board can read (a board is 403 without it), which is
// why a planning card lives there and a hierarchy link may point up to it
// from any repository (H6).
func PrimaryDomain(b Board) string {
	return b.inPrimary("")
}

// FileDomain is the repository whose files hold this card, read in the
// board's ONE namespace: the stamp the store put on it — gitstore stamps
// every card with its domain's name, the primary's included — with an
// unstamped card read as the primary.
//
// It answers where the card IS, which is the question a rule about the
// references written IN its file has to ask. HomeDomain answers where a
// card WOULD live by the placement rule, which is what a CHANGE is judged
// against.
func FileDomain(b Board, c Card) string {
	return b.inPrimary(c.Domain)
}

// HomeDomain is the repository that HOLDS a card, in the same namespace:
// the placement rule's answer (linked cards first, G14), with "nothing
// places this card" read as the primary — which is where such a card's
// file goes.
func HomeDomain(b Board, c Card) string {
	// Answered by the rule, because the question is always where the card
	// will BE after a change, not where its file happens to sit now.
	return b.inPrimary(DomainOf(c, Resolver(b, b.Primary)))
}
