package board

import (
	"slices"
	"testing"
)

// A card on the Backlog board can be stretched over more than one week, the
// way a Project-board slot is: the week it was placed in is where it starts,
// and its end date says where it reaches. It occupies every week between —
// a card stretched over two weeks is two weeks of work, and each of those
// weeks counts it against what the team can do.
func TestACardCoversEveryWeekItIsStretchedOver(t *testing.T) {
	for _, tc := range []struct {
		name string
		card Card
		want []string
	}{
		{
			name: "one week, no reach of its own",
			card: Card{Week: "2026-09-07"},
			want: []string{"2026-09-07"},
		},
		{
			name: "an end inside its own week is still one week",
			card: Card{Week: "2026-09-07", Day: "2026-09-11"},
			want: []string{"2026-09-07"},
		},
		{
			name: "stretched over three",
			card: Card{Week: "2026-09-07", Day: "2026-09-25"},
			want: []string{"2026-09-07", "2026-09-14", "2026-09-21"},
		},
		{
			name: "an end BEFORE the week is nonsense and covers the week alone",
			card: Card{Week: "2026-09-07", Day: "2026-08-31"},
			want: []string{"2026-09-07"},
		},
		{
			name: "no week, no weeks: the card is in the strip",
			card: Card{Day: "2026-09-11"},
			want: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := WeeksCovered(tc.card); !slices.Equal(got, tc.want) {
				t.Fatalf("WeeksCovered = %v, want %v", got, tc.want)
			}
		})
	}
}

// A stretched card is owed by the END of its reach, not by the Friday of the
// week it starts in: stretching it IS saying it takes longer. Without this a
// card stretched over three weeks read as late the moment its first week
// ended.
func TestAStretchedCardIsOwedByTheEndOfItsReach(t *testing.T) {
	const monday, friday, later = "2026-09-07", "2026-09-11", "2026-09-25"
	if got := DueDate(Card{Week: monday}); got != friday {
		t.Fatalf("a card placed in a week alone is owed by its Friday: %q", got)
	}
	if got := DueDate(Card{Week: monday, Day: later}); got != later {
		t.Fatalf("a stretched card is owed by the end of its reach: %q, want %q", got, later)
	}
	// And it is not late until then.
	if Overdue(Card{Week: monday, Day: later}, "2026-09-18") {
		t.Fatal("a card stretched into the week after next is not late in between")
	}
	if !Overdue(Card{Week: monday, Day: later}, "2026-09-28") {
		t.Fatal("past its reach it is")
	}
}
