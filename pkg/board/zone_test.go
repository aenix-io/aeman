package board

import "testing"

// A card in the WORKING AREA is drawn in one of the four bands, and the four
// are all there is: one nobody zoned is drawn in the PLANNED band by every
// client already (MeBoard and TeamBoard bucket `card.zone ?? "gray"`). So the
// board answers that itself instead of leaving each reader to guess, and a
// reader that never sees the card's own zone stops being possible.
//
// Off the day boards the honest answer is kept, because there a card with no
// zone is DRAWN with no zone: the backlog drawer gives it a neutral line and
// the Triage grid no zone class at all. "No zone" is a real state in the
// strip and on a shelf — it is only in the working area that it is a gap.
func TestZoneOf(t *testing.T) {
	cases := []struct {
		name string
		card Card
		want ZoneKey
	}{
		{"a card in a sprint nobody zoned is planned work",
			Card{SprintStart: "2026-09-07"}, ZoneGray},
		{"so is one standing on its own start day",
			Card{StartDate: "2026-09-07"}, ZoneGray},
		{"and one carrying only the day it is owed by",
			Card{Day: "2026-09-11"}, ZoneGray},
		{"a process turn dated across its week is planned work like any other",
			Card{StartDate: "2026-09-07", Day: "2026-09-13", Task: "01T", Stage: StageRecurrent}, ZoneGray},
		{"what somebody actually said always wins",
			Card{SprintStart: "2026-09-07", Zone: ZoneRed}, ZoneRed},
		{"a card in the strip has no zone, and that is a state of its own",
			Card{}, ZoneNone},
		{"nor has one waiting in a week to come",
			Card{Week: "2026-09-14"}, ZoneNone},
		{"nor one on its team's shelf",
			Card{Parked: true}, ZoneNone},
		{"a strip card somebody zoned keeps what they said",
			Card{Zone: ZoneYellow}, ZoneYellow},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ZoneOf(c.card); got != c.want {
				t.Errorf("ZoneOf() = %q, want %q", got, c.want)
			}
		})
	}
}
