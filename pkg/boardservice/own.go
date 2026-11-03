package boardservice

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aenix-io/aeman/pkg/board"
)

// OWN is one question asked of a task — whose is it — with one answer: a
// team, and a person on that team, or neither (docs/design/hierarchy.md
// "Owner", H10). It is the Roadmap's gesture first: the loose work listed
// below the tree (H9) has no home yet, and giving it one is what a lead does
// there. Every board that draws a task offers it as well, so the card's
// drawer can ask the same question from wherever the card was opened.
//
// What the answer DOES depends on whether the card is IN WORK — a date or a
// sprint — because a home is somewhere a board draws:
//
//   - a PERSON takes an unplanned card into work the way typing it into
//     their Me board would: on their team, today, in the unplanned band, in
//     the team's sprint, out of its backlog — so it stands on their Team and
//     Me boards at once (K4). A planned card merely changes hands and keeps
//     its plan.
//   - a TEAM alone parks an unplanned card in that team's backlog with
//     nobody on it. A planned card moves team, keeps its plan, and loses its
//     person.
//   - NOBODY takes the team and the person off; a planned card keeps its
//     plan in the no-team group, an unplanned one is parked in the no-team
//     group's backlog (K3) — there is no strip to hand it to.
//
// A subtask follows its parent's team and person (S9) and is not asked; a
// review card's person is its reviewer and is set by the review link;
// neither has an owner of its own. A planning card and a project card carry
// no owner at all (H3, H4).

// ErrOwnerNeedsTeam is a person named without a team: a person is picked
// FROM a team, which is how the picker reads and what the Backlog is keyed
// by, so a person on no team is an answer the board cannot draw.
var ErrOwnerNeedsTeam = errors.New("an owner is a team, or a person on a team: a person needs a team")

// ErrOwnerFollowsLink is a card whose owner is decided by a link: a subtask
// rides its parent's team and person, a review card's person is whoever was
// asked to review. Owning either directly would pull it off the card it
// belongs with.
var ErrOwnerFollowsLink = errors.New("a subtask's owner is its parent's, and a review card's is its reviewer")

// Own sets a task's team and person as one answer. team = "" with person =
// "" clears both; person without team is refused.
func (s *Service) Own(ctx context.Context, boardID string, itemID, team, person string) error {
	team, person = strings.TrimSpace(team), strings.TrimSpace(person)
	if person != "" && team == "" {
		return ErrOwnerNeedsTeam
	}
	b, card, err := s.loadCard(ctx, boardID, itemID)
	if err != nil {
		return err
	}
	if card.Scope != "" {
		return fmt.Errorf("%w: %q stands in a column and has no owner", ErrProjectCard, card.Title)
	}
	if err := notAPlanningCard(card, "owner"); err != nil {
		return err
	}
	if board.IsSubtask(b, card) || card.ReviewOf != "" {
		return fmt.Errorf("%w: %q", ErrOwnerFollowsLink, card.Title)
	}
	switch {
	case person != "":
		return s.ownByPerson(ctx, boardID, b, card, team, person)
	case team != "":
		return s.ownByTeam(ctx, boardID, b, card, team)
	}
	return s.disown(ctx, boardID, b, card)
}

// unplanned reports a card no day board draws by its own plan: no day and no
// sprint. The backlog is a separate fact — a parked card is unplanned too,
// and comes out of its backlog when a person takes it into work.
func unplanned(c board.Card) bool { return !board.InWork(c) }

// ownByPerson puts the card on a team and a person. Unplanned, it is filed
// the way the Me board files a create — today, the unplanned band, the
// team's sprint — through the same doors (SetTeam joins the sprint as the
// Team board's team menu does, SetDates and SetZone are the pane's own), so
// the two cannot read differently.
func (s *Service) ownByPerson(ctx context.Context, boardID string, b board.Board, card board.Card, team, person string) error {
	if unplanned(card) {
		// SetDates takes it out of its backlog on the way (takenIntoWork).
		today := board.TodayIso()
		if err := s.SetTeam(ctx, boardID, card.ItemID, team, today); err != nil {
			return err
		}
		if err := s.SetDates(ctx, boardID, card.ItemID, today, today); err != nil {
			return err
		}
		if err := s.SetZone(ctx, boardID, card.ItemID, board.ZoneYellow); err != nil {
			return err
		}
		return s.SetAssignee(ctx, boardID, card.ItemID, person)
	}
	if card.Team != team {
		if err := s.changeHands(ctx, boardID, b, card, team); err != nil {
			return err
		}
	}
	return s.SetAssignee(ctx, boardID, card.ItemID, person)
}

// changeHands moves a PLANNED card to a team and keeps its plan as it is —
// its sprint included. A card in a sprint joins the new team's current one,
// as the Team board's team menu does; a card in NO sprint stays in none: a
// card dated for a day still to come has no sprint by design (no sprint
// covers that day yet), and SetTeam would give it today's, which makes a
// plan for next week this sprint's unfinished work and carries it over.
func (s *Service) changeHands(ctx context.Context, boardID string, b board.Board, card board.Card, team string) error {
	if card.SprintStart == "" {
		return s.moveTeamOffTheDay(ctx, b, card, team)
	}
	return s.SetTeam(ctx, boardID, card.ItemID, team, "")
}

// ownByTeam puts the card on a team with nobody on it. Unplanned, it goes
// into that team's backlog — a card given a team and no plan is work the
// team has accepted and not taken into work, which is what the backlog
// holds — and joins no sprint, since a backlog is on no day. A parked card
// handed to another team stays parked (K5).
func (s *Service) ownByTeam(ctx context.Context, boardID string, b board.Board, card board.Card, team string) error {
	if len(card.Assignees) > 0 {
		if err := s.SetAssignee(ctx, boardID, card.ItemID, ""); err != nil {
			return err
		}
	}
	if !unplanned(card) {
		if card.Team == team {
			return nil
		}
		return s.changeHands(ctx, boardID, b, card, team)
	}
	if err := s.moveTeamOffTheDay(ctx, b, card, team); err != nil {
		return err
	}
	if card.Parked {
		return nil
	}
	return s.SetBacklog(ctx, boardID, card.ItemID, true)
}

// disown takes the team and the person off. A planned card keeps its plan
// in the no-team group; an unplanned one is parked in the no-team group's
// backlog (K3) — on no team, in no sprint, and somewhere a board draws.
func (s *Service) disown(ctx context.Context, boardID string, b board.Board, card board.Card) error {
	if len(card.Assignees) > 0 {
		if err := s.SetAssignee(ctx, boardID, card.ItemID, ""); err != nil {
			return err
		}
	}
	if !unplanned(card) {
		if card.Team == "" {
			return nil
		}
		return s.SetTeam(ctx, boardID, card.ItemID, "", "")
	}
	if err := s.moveTeamOffTheDay(ctx, b, card, ""); err != nil {
		return err
	}
	if card.Parked {
		return nil
	}
	return s.SetBacklog(ctx, boardID, card.ItemID, true)
}

// moveTeamOffTheDay moves an UNPLANNED card (and its subtasks) to a team
// without joining that team's sprint: SetTeam would, because on the Team
// board a card handed to a team is in hand, but a card with no plan is not —
// a sprint on it would draw it on the team's grid as work in progress the
// moment nobody meant it to be.
func (s *Service) moveTeamOffTheDay(ctx context.Context, b board.Board, card board.Card, team string) error {
	if err := guardRoster(b, team, card.Project); err != nil {
		return err
	}
	if err := s.declareTeam(ctx, b, team); err != nil {
		return err
	}
	if err := s.setTeamOne(ctx, b, card, team, ""); err != nil {
		return err
	}
	for _, c := range board.Children(b, card.ItemID) {
		if err := s.setTeamOne(ctx, b, c, team, ""); err != nil {
			return err
		}
	}
	return nil
}
