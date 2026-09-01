package board

import "testing"

// A day inside a RUNNING sprint is still being worked on, whatever the
// calendar says: a team whose sprint opened yesterday lays that sprint out on
// its own day, and the lead works it from there. Only what lies before the
// running sprint is over and can be shown as a record.
func TestTheRunningSprintStartsWhereHistoryEnds(t *testing.T) {
	b := Board{SprintStates: map[string]SprintState{
		"portal":     {Current: "2026-09-01", Previous: "2026-08-31"},
		"backoffice": {Current: "2026-08-31", Previous: "2026-08-28"},
		"sales":      {Current: "2026-07-06"},
		"":           {Current: "2026-06-30"},
	}}

	// One team: its own sprint is the boundary.
	if got := RunningSprintStart(b, []string{"portal"}); got != "2026-09-01" {
		t.Fatalf("portal = %q", got)
	}
	// Several: the EARLIEST, because a day still inside anyone's running
	// sprint must not be frozen — freezing it would take the board away from
	// the team that is still working it.
	if got := RunningSprintStart(b, []string{"portal", "backoffice"}); got != "2026-08-31" {
		t.Fatalf("portal+backoffice = %q", got)
	}
	// A team whose sprint has stood since July drags the boundary with it:
	// that sprint IS still running, however old, and its days are its
	// working surface. Pressing carry-over is what moves it.
	if got := RunningSprintStart(b, []string{"portal", "sales"}); got != "2026-07-06" {
		t.Fatalf("portal+sales = %q", got)
	}
	// The no-team group counts like a team.
	if got := RunningSprintStart(b, []string{"portal", ""}); got != "2026-06-30" {
		t.Fatalf("portal+no-team = %q", got)
	}
	// A team with no sprint at all has no running one and does not decide.
	if got := RunningSprintStart(b, []string{"portal", "nobody"}); got != "2026-09-01" {
		t.Fatalf("portal+unknown = %q", got)
	}
	// Nothing named: no boundary, and the caller falls back to today.
	if got := RunningSprintStart(b, nil); got != "" {
		t.Fatalf("no teams = %q", got)
	}
}
