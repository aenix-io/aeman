package board

// ZoneOf is the band a card is drawn in — its own zone, or PLANNED when it is
// in the working area and nobody said.
//
// The working area is drawn as four bands and nothing else, so a card standing
// on a day has to be in one of them. Every client already decides that the
// same way (`card.zone ?? "gray"` in MeBoard and TeamBoard), which means the
// answer existed but only inside the clients: the API said nothing, and each
// reader — a script, an agent, a second client — had to reinvent the fallback
// or read the card as zone-less work in the middle of somebody's day.
//
// Outside the working area the empty zone is kept, because there it is DRAWN:
// the backlog drawer gives a zone-less card a neutral line and the Triage grid
// no zone class at all. A card in the strip or on a shelf genuinely has no
// zone yet — the zones are statements about a DAY (critical means today,
// unplanned means it turned up today, "if time left" is a day's spare
// capacity), and a card on no day has not been the subject of one.
func ZoneOf(c Card) ZoneKey {
	if c.Zone != ZoneNone {
		return c.Zone
	}
	if c.SprintStart != "" || c.StartDate != "" || c.Day != "" {
		return ZoneGray
	}
	return ZoneNone
}
