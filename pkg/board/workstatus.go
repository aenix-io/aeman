package board

// The work statuses the Roadmap's status filter reads (O14). Mirrored by
// workStatus in web/src/roadmap.ts.
const (
	WorkStatusInProgress = "in-progress"
	WorkStatusBacklog    = "backlog"
	WorkStatusDone       = "done"
)

// WorkStatus is a task's work status: done when complete; backlog when
// parked; in progress when in work — a date or a sprint, whatever its stage
// says (review, locked and refused are in somebody's hands). A task nobody
// has placed yet is waiting like the backlog and reads as backlog.
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
