package apiserver

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// What a listing marks as a RECORD is what the day's board took from the
// past — by card, not by team name. The two names differ exactly where it
// matters: a card that moved between teams since, and every card of a team
// renamed since, carry the EVENING's team in the merged board while the teams
// the day is over for are named by TODAY's. Marked by name, such a card came
// through live — draggable, editable, its detail pane open for writing — and
// every write door refused it. G60 names that refusal as the thing not to do.
func TestARecordIsMarkedByCardNotByTeamName(t *testing.T) {
	const asOf = "2026-08-24T23:59:59+02:00"
	b := board.Board{
		SprintStates: map[string]board.SprintState{"portal": {Current: "2026-08-24"}},
		Cards: []board.Card{
			// Taken from that evening; its team then is not what it is now.
			{ItemID: "moved", Team: "backoffice", Progress: 20, Rank: "a",
				SprintStart: "2026-08-24", StartDate: "2026-08-24", Day: "2026-08-24"},
			// Live, and of a team the day IS over for by name.
			{ItemID: "live", Team: "portal", Progress: 90, Rank: "b",
				SprintStart: "2026-08-24", StartDate: "2026-08-24", Day: "2026-08-24"},
		},
	}
	list := ListCards(b, Selector{View: "team", Team: "portal,backoffice", Day: "2026-08-24"})
	MarkRecords(&list, map[string]bool{"moved": true}, asOf)

	got := map[string]string{}
	for _, c := range list.Items {
		got[c.Metadata.UID] = c.Status.AsOf
	}
	if got["moved"] != asOf {
		t.Fatalf("the card taken from that evening carries asOf=%q; the client freezes on this and the server refuses writes to it", got["moved"])
	}
	if got["live"] != "" {
		t.Fatalf("a live card must carry no moment, got %q", got["live"])
	}
	if list.AsOf != asOf {
		t.Fatalf("the listing says which moment it is of: %q", list.AsOf)
	}
}
