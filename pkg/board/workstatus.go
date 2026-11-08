package board

// The work statuses the Roadmap's status filter reads (O14). Mirrored by
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
