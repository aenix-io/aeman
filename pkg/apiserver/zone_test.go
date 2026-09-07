package apiserver

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// The API answers the band the board DRAWS (board.ZoneOf): a card standing on
// a day and carrying no zone of its own is planned work, because that is where
// every client already puts it. Before this, such a card came back with no
// zone at all and each reader had to know the clients' fallback to make sense
// of it — a process turn, filed with no zone, arrived in the middle of
// somebody's day as work in no band.
func TestAWorkingCardWithNoZoneIsPlanned(t *testing.T) {
	b := board.Board{Cards: []board.Card{
		{ItemID: "turn", Title: "Получить оплату", Team: "alpha", Task: "t1",
			Stage: board.StageRecurrent, StartDate: "2026-09-07", Day: "2026-09-13"},
		{ItemID: "strip", Title: "somebody's idea", Team: "alpha"},
		{ItemID: "urgent", Title: "the fire", Team: "alpha",
			SprintStart: "2026-09-07", Zone: board.ZoneRed},
	}}
	if got := CardResource(b, b.Cards[0]).Spec.Zone; got != "planned" {
		t.Errorf("a dated turn nobody zoned = %q, want planned", got)
	}
	// The strip is not the working area: there a card with no zone is drawn
	// with none, so the API says none.
	if got := CardResource(b, b.Cards[1]).Spec.Zone; got != "" {
		t.Errorf("a card in the strip = %q, want no zone", got)
	}
	if got := CardResource(b, b.Cards[2]).Spec.Zone; got != "urgent" {
		t.Errorf("what somebody said = %q, want urgent", got)
	}
}

// The zone FILTER reads the same answer, or the listing and the filter
// disagree about the very same card: ?zone=planned would skip the cards the
// API itself reports as planned.
func TestTheZoneFilterMatchesWhatTheCardReports(t *testing.T) {
	b := board.Board{Cards: []board.Card{
		{ItemID: "turn", Title: "Получить оплату", Team: "alpha", Task: "t1",
			StartDate: "2026-09-07", Day: "2026-09-13"},
		{ItemID: "strip", Title: "somebody's idea", Team: "alpha"},
	}}
	planned := "planned"
	got := FilterCards(b, Selector{View: "all", Zone: &planned})
	if len(got) != 1 || got[0].ItemID != "turn" {
		t.Fatalf("zone=planned selected %+v, want the dated turn alone", ids(got))
	}
	// The derived band is PLANNED and nothing else: a filter for another zone
	// must not adopt the cards nobody zoned.
	urgent := "urgent"
	if sel := FilterCards(b, Selector{View: "all", Zone: &urgent}); len(sel) != 0 {
		t.Fatalf("zone=urgent selected %+v, want nothing", ids(sel))
	}
}

func ids(cards []board.Card) []string {
	out := make([]string, 0, len(cards))
	for _, c := range cards {
		out = append(out, c.ItemID)
	}
	return out
}
