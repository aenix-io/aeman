package ghprojects

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aenix-org/aeman/pkg/board"
)

// eventLogCap bounds a card's stored event log: the oldest events beyond it
// are dropped on append, so the body/comment never grows unbounded (work notes
// are never dropped).
const eventLogCap = 200

// getContentCommentsQuery fetches an issue/PR's latest comments (id + body) so
// AppendEvent can find the card's dedicated log comment without relying on a
// possibly-stale loaded board.
const getContentCommentsQuery = `query($id: ID!) {
  node(id: $id) {
    ... on Issue { comments(last: 100) { nodes { id body } } }
    ... on PullRequest { comments(last: 100) { nodes { id body } } }
  }
}`

// AppendEvent records one activity event on a card, best-effort storage-wise:
// a draft card gets a machine line appended to its body log (next to the work
// notes), an issue/PR card gets it appended to a dedicated log comment
// (created on first use, so issue subscribers see one comment, not a stream).
func (c *Client) AppendEvent(ctx context.Context, _ board.Board, card board.Card, e board.Event) error {
	if card.ContentID == "" {
		return ErrNoContent
	}
	if e.At == "" {
		e.At = time.Now().UTC().Format(time.RFC3339)
	}
	line := fmt.Sprintf("- [%s] %s", e.At, board.FormatEventBody(e))
	if card.IsDraft {
		body, err := c.draftBody(ctx, card.ContentID)
		if err != nil {
			return err
		}
		next := line
		if body != "" {
			next = body + "\n" + line
		}
		next = capDraftEventLog(next, card.ItemID)
		return c.graphql(ctx, updateDraftBodyMutation, map[string]any{
			"draft": card.ContentID, "body": next,
		}, nil)
	}
	return c.appendIssueEvent(ctx, card, line)
}

// appendIssueEvent appends an event line to an issue/PR card's dedicated log
// comment, creating the comment when the card has none yet.
func (c *Client) appendIssueEvent(ctx context.Context, card board.Card, line string) error {
	var data struct {
		Node *struct {
			Comments *struct {
				Nodes []struct {
					ID   string `json:"id"`
					Body string `json:"body"`
				} `json:"nodes"`
			} `json:"comments"`
		} `json:"node"`
	}
	if err := c.graphql(ctx, getContentCommentsQuery, map[string]any{"id": card.ContentID}, &data); err != nil {
		return err
	}
	logID, logBody := "", ""
	if data.Node != nil && data.Node.Comments != nil {
		for _, cm := range data.Node.Comments.Nodes {
			if strings.HasPrefix(strings.TrimSpace(cm.Body), domainLogMarker) {
				logID, logBody = cm.ID, cm.Body
				break
			}
		}
	}
	if logID == "" {
		return c.graphql(ctx, addCommentMutation, map[string]any{
			"subject": card.ContentID, "body": domainLogMarker + "\n" + line,
		}, nil)
	}
	next := capCommentEventLog(logBody+"\n"+line, logID)
	return c.graphql(ctx, updateCommentMutation, map[string]any{"id": logID, "body": next}, nil)
}

// capDraftEventLog drops the oldest events beyond eventLogCap from a draft
// body, keeping the description and every work note; the log section is
// rebuilt chronologically. Bodies within the cap pass through untouched.
func capDraftEventLog(body, itemID string) string {
	desc, parsed := domainParseDraftBody(body, itemID)
	notes, events := board.PartitionEvents(parsed)
	if len(events) <= eventLogCap {
		return body
	}
	events = events[len(events)-eventLogCap:]
	return domainBuildDraftBodyMixed(desc, notes, events)
}

// capCommentEventLog drops the oldest events beyond eventLogCap from a log
// comment's body.
func capCommentEventLog(body, commentID string) string {
	rest := strings.TrimSpace(body)[len(domainLogMarker):]
	notes, events := board.PartitionEvents(domainParseNoteLines(rest, commentID))
	if len(events) <= eventLogCap {
		return body
	}
	events = events[len(events)-eventLogCap:]
	lines := logLinesMixed(notes, events)
	return domainLogMarker + "\n" + strings.Join(lines, "\n")
}

// domainBuildDraftBodyMixed renders a draft body from a description plus its
// notes AND events, merged chronologically (both are dated log lines).
func domainBuildDraftBodyMixed(description string, notes []board.Note, events []board.Event) string {
	head := ""
	if description != "" {
		head = description + "\n\n"
	}
	return head + domainLogMarker + "\n" + strings.Join(logLinesMixed(notes, events), "\n")
}

// logLinesMixed renders notes and events as "- [ts] body" lines sorted by
// timestamp (RFC3339 sorts lexicographically).
func logLinesMixed(notes []board.Note, events []board.Event) []string {
	type entry struct{ at, body string }
	entries := make([]entry, 0, len(notes)+len(events))
	for _, n := range notes {
		entries = append(entries, entry{n.CreatedAt, board.RenderNoteBody(n.Author, n.Body)})
	}
	for _, e := range events {
		entries = append(entries, entry{e.At, board.FormatEventBody(e)})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].at < entries[j].at })
	lines := make([]string, 0, len(entries))
	for _, en := range entries {
		lines = append(lines, fmt.Sprintf("- [%s] %s", en.at, en.body))
	}
	return lines
}
