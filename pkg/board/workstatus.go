package board

// The work statuses the Strategy board's status filter reads (O14). Mirrored by
// workStatus in web/src/workstatus.ts; both read testdata/work_status.json.
const (
	WorkStatusInProgress = "in-progress"
	WorkStatusBacklog    = "backlog"
	WorkStatusDone       = "done"
)

// WorkStatus is a task's work status, and it follows PLACEMENT: done when
// complete (Complete — so locked or on review at 100 is not, and recurrent
// at 100 is); else backlog when parked, whatever its dates or its stage say;
// else in progress when in work — a date (an end date alone counts) or a
// sprint, whatever its stage (review, locked and refused are in somebody's
// hands). A task nobody has placed yet is waiting like the backlog and reads
// as backlog, on review or not. A card's stage badge may still show its
// stage; the filter reads where the task stands.
func WorkStatus(c Card) string {
	switch {
	case Complete(c.Stage, c.Progress):
		return WorkStatusDone
	case InBacklog(c):
		return WorkStatusBacklog
	case InWork(c):
		return WorkStatusInProgress
	default:
		return WorkStatusBacklog
	}
}

// ValidWorkStatus reports whether a name is one of the work statuses.
func ValidWorkStatus(s string) bool {
	switch s {
	case WorkStatusInProgress, WorkStatusBacklog, WorkStatusDone:
		return true
	}
	return false
}

// The values the Strategy board's status filter takes beyond the work statuses
// (O14): a card a dependency holds (the derived lock, N3 — status.locked,
// never the manual stage) and a planning card with nothing under it (its
// roll-up is HierarchyNoItems). Mirrored by web/src/workstatus.ts.
const (
	StatusFilterLocked  = "locked"
	StatusFilterNoItems = HierarchyNoItems
)

// StatusFilters are the status filter's values in the order a toolbar
// offers them. Every one but done is what "Hide done" asks for.
var StatusFilters = []string{
	WorkStatusInProgress, WorkStatusBacklog, StatusFilterLocked, StatusFilterNoItems, WorkStatusDone,
}

// ValidStatusFilter reports whether a name is one of the status filter's
// values: a work status, locked or no-items.
func ValidStatusFilter(s string) bool {
	return ValidWorkStatus(s) || s == StatusFilterLocked || s == StatusFilterNoItems
}

// TaskStatuses are the status filter values a task answers (O14): its work
// status, and locked while a dependency holds it (locked — Index.Locked, own
// or inherited) and it is not done. Done wins: a finished task that still
// waits is done, so "Hide done" — every value but done — hides exactly the
// finished work.
func TaskStatuses(c Card, locked bool) []string {
	ws := WorkStatus(c)
	if locked && ws != WorkStatusDone {
		return []string{ws, StatusFilterLocked}
	}
	return []string{ws}
}

// PlanStatuses are the status filter values a planning card answers BY
// ITSELF, from its roll-up (Index.HierarchyStatus) and its lock: no-items
// when nothing is under it, locked while a dependency holds it and its
// roll-up is not done. The other values it answers by a task under it.
func PlanStatuses(rollUp string, locked bool) []string {
	var out []string
	if locked && rollUp != HierarchyDone {
		out = append(out, StatusFilterLocked)
	}
	if rollUp == HierarchyNoItems {
		out = append(out, StatusFilterNoItems)
	}
	return out
}
