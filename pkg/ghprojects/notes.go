package ghprojects

import (
	"context"
	"fmt"
	"strings"

	"github.com/aenix-org/aeman/pkg/board"
)

// draftBody fetches a draft issue's current markdown body.
func (c *Client) draftBody(ctx context.Context, contentID string) (string, error) {
	var data struct {
		Node *struct {
			Body string `json:"body"`
		} `json:"node"`
	}
	if err := c.graphql(ctx, getDraftBodyQuery, map[string]any{"id": contentID}, &data); err != nil {
		return "", err
	}
	if data.Node == nil {
		return "", nil
	}
	return data.Node.Body, nil
}

// domainBuildDraftBody renders a draft body from a description and the note log,
// mirroring buildDraftBody in the frontend githubProvider: the description, a
// blank line, the log marker, then one "- [timestamp] text" line per note.
func domainBuildDraftBody(description string, notes []board.Note) string {
	lines := make([]string, 0, len(notes))
	for _, n := range notes {
		lines = append(lines, fmt.Sprintf("- [%s] %s", n.CreatedAt, n.Body))
	}
	head := ""
	if description != "" {
		head = description + "\n\n"
	}
	return head + domainLogMarker + "\n" + strings.Join(lines, "\n")
}

// SetDescription replaces a card's free-form description. On a draft the body is
// rebuilt around the preserved note log; on an issue/PR the body is the
// description. It mirrors githubProvider.setDescription.
func (c *Client) SetDescription(ctx context.Context, _ board.Board, card board.Card, description string) error {
	if card.ContentID == "" {
		return ErrNoContent
	}
	if card.IsDraft {
		body, err := c.draftBody(ctx, card.ContentID)
		if err != nil {
			return err
		}
		_, notes := domainParseDraftBody(body, card.ItemID)
		return c.graphql(ctx, updateDraftBodyMutation, map[string]any{
			"draft": card.ContentID,
			"body":  domainBuildDraftBody(description, notes),
		}, nil)
	}
	return c.graphql(ctx, updateIssueBodyMutation, map[string]any{"id": card.ContentID, "body": description}, nil)
}

// EditNote rewrites one work note: an issue comment is updated in place, a
// draft-log line is replaced in the rebuilt body. It mirrors
// githubProvider.editNote.
func (c *Client) EditNote(ctx context.Context, _ board.Board, card board.Card, note board.Note, text string) error {
	if note.Source == "comment" {
		return c.graphql(ctx, updateCommentMutation, map[string]any{"id": note.ID, "body": text}, nil)
	}
	if card.ContentID == "" {
		return ErrNoContent
	}
	body, err := c.draftBody(ctx, card.ContentID)
	if err != nil {
		return err
	}
	description, notes := domainParseDraftBody(body, card.ItemID)
	for i := range notes {
		if notes[i].ID == note.ID {
			notes[i].Body = text
		}
	}
	return c.graphql(ctx, updateDraftBodyMutation, map[string]any{
		"draft": card.ContentID,
		"body":  domainBuildDraftBody(description, notes),
	}, nil)
}

// DeleteNote removes one work note: an issue comment is deleted, a draft-log
// line is dropped from the rebuilt body. It mirrors githubProvider.deleteNote.
func (c *Client) DeleteNote(ctx context.Context, _ board.Board, card board.Card, note board.Note) error {
	if note.Source == "comment" {
		return c.graphql(ctx, deleteCommentMutation, map[string]any{"id": note.ID}, nil)
	}
	if card.ContentID == "" {
		return ErrNoContent
	}
	body, err := c.draftBody(ctx, card.ContentID)
	if err != nil {
		return err
	}
	description, notes := domainParseDraftBody(body, card.ItemID)
	remaining := notes[:0]
	for _, n := range notes {
		if n.ID != note.ID {
			remaining = append(remaining, n)
		}
	}
	return c.graphql(ctx, updateDraftBodyMutation, map[string]any{
		"draft": card.ContentID,
		"body":  domainBuildDraftBody(description, remaining),
	}, nil)
}
