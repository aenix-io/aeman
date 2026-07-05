package board

import (
	"fmt"
	"strings"
)

// Event is one recorded action on a card: who changed what, when. Events live
// in the card itself — a draft card keeps them as machine lines in its body
// log next to the work notes, an issue/PR card in a dedicated log comment — so
// the history is stored with the card and dies with it. Writes that bypass
// aeman (edits in the GitHub Projects UI) leave no events: Projects v2 exposes
// no field history, so aeman records changes at its own write path.
type Event struct {
	// ID anchors the event like a note id (itemID:line or comment-derived).
	ID string `json:"id"`
	// Kind is one of the Event* constants below.
	Kind string `json:"kind"`
	// Actor is the GitHub login that made the change ("" when unknown).
	Actor string `json:"actor,omitempty"`
	// From and To carry the change's old and new value, kind-specific.
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
	// At is the RFC3339 timestamp the change was recorded.
	At string `json:"at"`
}

// Event kinds written by the board service.
const (
	EventCreated         = "created"
	EventStage           = "stage"
	EventProgress        = "progress"
	EventAssignee        = "assignee"
	EventTeam            = "team"
	EventZone            = "zone"
	EventReviewSent      = "review-sent"
	EventReviewPassed    = "review-passed"
	EventReviewerRemoved = "reviewer-removed"
	EventPlanTaken       = "plan-taken"
	EventPlanReleased    = "plan-released"
	EventDates           = "dates"
	EventSprint          = "sprint"
	EventWeek            = "week"
	EventPlanBand        = "plan-band"
	EventReviewRound     = "review-round"
)

// DateRange renders a start..end pair for a dates event value ("" parts kept
// readable: "..2026-07-04", "2026-07-01..", "" when both empty).
func DateRange(start, end string) string {
	if start == "" && end == "" {
		return ""
	}
	return start + ".." + end
}

// eventPrefix marks a log-line body as a machine event rather than a work
// note. Parsers treat any log line whose body starts with it as an Event.
const eventPrefix = ":: "

// FormatEventBody renders an event as the body of a log line (the part after
// the "- [timestamp] " header): ":: kind | actor | from | to". Pipes inside
// values are replaced — they are the field separators.
func FormatEventBody(e Event) string {
	clean := func(s string) string {
		return strings.TrimSpace(strings.ReplaceAll(s, "|", "/"))
	}
	return strings.TrimRight(
		fmt.Sprintf("%s%s | %s | %s | %s",
			eventPrefix, clean(e.Kind), clean(e.Actor), clean(e.From), clean(e.To)),
		" |",
	)
}

// ParseEventBody parses a log-line body produced by FormatEventBody. ok is
// false when the body is a plain work note, not an event line.
func ParseEventBody(body string) (Event, bool) {
	if !strings.HasPrefix(body, eventPrefix) {
		return Event{}, false
	}
	parts := strings.Split(body[len(eventPrefix):], "|")
	get := func(i int) string {
		if i < len(parts) {
			return strings.TrimSpace(parts[i])
		}
		return ""
	}
	e := Event{Kind: get(0), Actor: get(1), From: get(2), To: get(3)}
	if e.Kind == "" {
		return Event{}, false
	}
	return e, true
}

// PartitionEvents splits a parsed log into plain work notes and events: any
// note whose body is an event line becomes an Event carrying the note's id and
// timestamp. The relative order inside each slice is preserved.
func PartitionEvents(notes []Note) ([]Note, []Event) {
	var keep []Note
	var events []Event
	for _, n := range notes {
		if e, ok := ParseEventBody(n.Body); ok {
			e.ID, e.At = n.ID, n.CreatedAt
			events = append(events, e)
			continue
		}
		keep = append(keep, n)
	}
	return keep, events
}
