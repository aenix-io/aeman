package boardservice

import (
	"context"

	"github.com/aenix-io/aeman/pkg/board"
)

// followPlacement moves into its new team's current sprint every card IN
// WORK whose placed team a change moves (O13): a plan item taking another
// team, a task filed under another plan item or taken out from under one, a
// plan item deleted with its children handed up. None of those writes the
// task, yet the task now stands under another team — and left in its old
// team's sprint it would sit in a sprint its new team never opens, which no
// carry-over of either team reaches. It does what a direct team change does
// (SetTeam): the new team's current sprint, else today for a team with no
// sprint yet. A card with no sprint (parked, or planned for a day to come),
// and a plan item have nothing to move. `edit` applies the
// change to a copy of the board; the caller writes it.
func (s *Service) followPlacement(ctx context.Context, b board.Board, edit func(*board.Board)) error {
	after := b
	after.Cards = append([]board.Card(nil), b.Cards...)
	edit(&after)
	was, now := board.PlacedTeams(b), board.PlacedTeams(after)
	for _, c := range after.Cards {
		if board.IsPlanning(c) || board.InBacklog(c) || c.SprintStart == "" {
			continue
		}
		if was[c.ItemID] == now[c.ItemID] {
			continue
		}
		target := board.CurrentSprint(after, now[c.ItemID])
		if target == "" {
			target = board.TodayIso()
		}
		if target == c.SprintStart {
			continue
		}
		if err := s.backend.SetSprintStart(ctx, b, c, target); err != nil {
			return err
		}
		s.logEvent(ctx, b, c, board.EventSprint, c.SprintStart, target)
	}
	return nil
}

// editCard is an edit of one card for followPlacement.
func editCard(id string, fn func(*board.Card)) func(*board.Board) {
	return func(b *board.Board) {
		for i := range b.Cards {
			if b.Cards[i].ItemID == id {
				fn(&b.Cards[i])
			}
		}
	}
}
