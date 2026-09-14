package gitstore

import "testing"

// A commit trailer is the activity log, so a value a caller controls must not
// forge one. A date field carrying newlines and Aeman- lines used to inject a
// done event on another card, attributed to someone else; every interpolated
// value is flattened to one line at the sink now (security finding: trailer
// injection).
func TestTrailerValuesCannotForgeTheLog(t *testing.T) {
	inject := "2026-01-01\nAeman-Cards: VICTIMUID\nAeman-Change: VICTIMUID done - -\nAeman-Actor: lead"
	a := Action{
		Name: "update", ID: "01REALID", Actor: "attacker",
		Summary: "set dates on «my card»",
		Cards:   []string{"MYUID"},
		Changes: []Change{{Card: "MYUID", Kind: "dates", From: "", To: inject}},
	}
	tr := ParseTrailers(a.message())
	if tr.Actor != "attacker" {
		t.Fatalf("actor forged to %q, want the real caller", tr.Actor)
	}
	for _, c := range tr.Cards {
		if c == "VICTIMUID" {
			t.Fatalf("a card was injected into Aeman-Cards: %v", tr.Cards)
		}
	}
	for _, ch := range tr.Changes {
		if ch.Card == "VICTIMUID" {
			t.Fatalf("a change was forged onto another card: %+v", ch)
		}
	}
	// A newline in a caller-supplied summary cannot open the trailer block early.
	early := Action{Name: "update", Summary: "hi\nAeman-Actor: lead", Actor: "real"}
	if ParseTrailers(early.message()).Actor != "real" {
		t.Fatal("a summary newline forged the actor")
	}
	// A legitimate change — the values the writer actually emits are single
	// tokens — still round-trips unchanged.
	ok := Action{
		Name: "update", Actor: "kvaps", Summary: "set dates",
		Cards:   []string{"MYUID"},
		Changes: []Change{{Card: "MYUID", Kind: "dates", From: "", To: "2026-01-01..2026-01-02"}},
	}
	tr = ParseTrailers(ok.message())
	if len(tr.Changes) != 1 || tr.Changes[0].Card != "MYUID" || tr.Changes[0].To != "2026-01-01..2026-01-02" {
		t.Fatalf("a legitimate change did not round-trip: %+v", tr.Changes)
	}
}
