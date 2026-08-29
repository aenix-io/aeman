package board

import (
	"context"
	"fmt"
	"regexp"
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
	EventEpic            = "epic"
	EventMirror          = "mirror"
	EventProcess         = "process"
	EventZone            = "zone"
	EventReviewSent      = "review-sent"
	EventReviewPassed    = "review-passed"
	EventReviewerRemoved = "reviewer-removed"
	EventPlanTaken       = "plan-taken"
	EventPlanAdded       = "plan-added"
	EventPlanReleased    = "plan-released"
	EventDates           = "dates"
	EventSprint          = "sprint"
	EventWeek            = "week"
	EventPlanBand        = "plan-band"
	EventReviewRound     = "review-round"
	EventParent          = "parent"
	EventSubtask         = "subtask"
	EventRecurrence      = "recurrence"
	// EventLeft: a personal card left behind on a past day's board by the ×
	// (to = that day), or brought back by re-dating it (to = "").
	EventLeft = "left"
)

// DateRange renders a start..end pair for a dates event value ("" parts kept
// readable: "..2026-07-04", "2026-07-01..", "" when both empty).
func DateRange(start, end string) string {
	if start == "" && end == "" {
		return ""
	}
	return start + ".." + end
}

// actorKey carries the acting user's GitHub login on the context, so backends
// can attribute the notes and events they write.
type actorKey struct{}

// WithActor returns a context carrying the acting user's GitHub login.
func WithActor(ctx context.Context, login string) context.Context {
	if login == "" {
		return ctx
	}
	return context.WithValue(ctx, actorKey{}, login)
}

// ActorFrom returns the acting user's login stamped by WithActor ("" when none).
func ActorFrom(ctx context.Context) string {
	login, _ := ctx.Value(actorKey{}).(string)
	return login
}

// unattributedKey marks a context whose changes belong to no single client:
// work the server does on its own behalf (a background title resolve), which
// every watcher — the originating tab included — must be told about.
type unattributedKey struct{}

// Unattributed marks ctx as carrying server-side work with no originating
// client, so watch frames reach everyone instead of being echo-suppressed
// against whoever happened to trigger it.
func Unattributed(ctx context.Context) context.Context {
	return context.WithValue(ctx, unattributedKey{}, true)
}

// IsUnattributed reports whether ctx was marked by Unattributed.
func IsUnattributed(ctx context.Context) bool {
	on, _ := ctx.Value(unattributedKey{}).(bool)
	return on
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

// noteAuthorRe extracts an "@login: " author prefix from a draft note body —
// the attribution AddNote writes for the acting user.
var noteAuthorRe = regexp.MustCompile(`^@([A-Za-z0-9][A-Za-z0-9-]*): ?`)

// eventLineRe matches a whole event log line ("- [ts] :: kind | ...."): the
// dated-line header followed by the machine event prefix. Nothing a person
// writes as prose - the ":: " marker is aeman's own.
var eventLineRe = regexp.MustCompile(`^[-*]?\s*\[[^\]]+\]\s*` + regexp.QuoteMeta(eventPrefix))

// StripEventLines drops machine event-log lines from a description text.
// Descriptions occasionally get them baked in - an agent copying a card's
// visible text back, a description synced from a card whose body was damaged
// by an old log migration - and they are never legitimate prose, so every
// description write runs through this.
func StripEventLines(text string) string {
	if !strings.Contains(text, eventPrefix) {
		return text
	}
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		if eventLineRe.MatchString(strings.TrimSpace(line)) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// RenderNoteBody prepends the author attribution to a note body for storage
// in the draft log ("@login: text"); an empty author stores the bare text.
func RenderNoteBody(author, text string) string {
	if author == "" {
		return text
	}
	return "@" + author + ": " + text
}

// SplitNoteAuthor extracts the "@login: " attribution from a stored note body,
// returning the author ("" for legacy unattributed notes) and the bare text.
func SplitNoteAuthor(body string) (author, text string) {
	if m := noteAuthorRe.FindStringSubmatch(body); m != nil {
		return m[1], body[len(m[0]):]
	}
	return "", body
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
