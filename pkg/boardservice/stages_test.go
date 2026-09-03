package boardservice

import (
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// REFUSE is a first-person act: "I am not doing this". Only the person the
// card is on may say it, and the server is what says so — the Me board is
// where the stage is offered, but an agent reaches the same door. A lead
// marking somebody else's card refused would be putting words in their
// mouth; the lead's answer is the × or a stage that puts the card back to
// work.
func TestOnlyItsOwnerRefusesACard(t *testing.T) {
	today := board.TodayIso()
	newBoard := func() *fakeBackend {
		return newFake([]board.Card{
			{ItemID: "mine", Team: "alpha", Assignees: []string{"kvaps"},
				SprintStart: today, StartDate: today, Progress: 30},
			{ItemID: "theirs", Team: "alpha", Assignees: []string{"lllamnyp"},
				SprintStart: today, StartDate: today, Progress: 30},
			{ItemID: "nobodys", Team: "alpha", Assignees: []string{},
				SprintStart: today, StartDate: today},
		}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	}

	f := newBoard()
	kvaps := WithActor(ctx, "kvaps")
	if err := f2svc(f).SetStage(kvaps, "acme", "mine", board.StageRefuse); err != nil {
		t.Fatalf("a person refuses their own card: %v", err)
	}
	if got := f.get("mine"); got.Stage != board.StageRefuse {
		t.Fatalf("stage = %q, want refused", got.Stage)
	}

	// Somebody else's card is not theirs to answer for.
	f = newBoard()
	if err := f2svc(f).SetStage(kvaps, "acme", "theirs", board.StageRefuse); !errors.Is(err, ErrNotYoursToRefuse) {
		t.Fatalf("refusing another person's card = %v, want ErrNotYoursToRefuse", err)
	}
	if got := f.get("theirs"); got.Stage != board.StageNone {
		t.Fatalf("the refusal must fire before the write: %+v", got)
	}

	// Nor is a card on nobody: refusing is done by the person carrying the
	// work, and an unassigned card is carried by no one.
	f = newBoard()
	if err := f2svc(f).SetStage(kvaps, "acme", "nobodys", board.StageRefuse); !errors.Is(err, ErrNotYoursToRefuse) {
		t.Fatalf("refusing an unassigned card = %v, want ErrNotYoursToRefuse", err)
	}

	// An anonymous caller is nobody in particular and may not refuse at all.
	f = newBoard()
	if err := f2svc(f).SetStage(ctx, "acme", "mine", board.StageRefuse); !errors.Is(err, ErrNotYoursToRefuse) {
		t.Fatalf("an anonymous refusal = %v, want ErrNotYoursToRefuse", err)
	}
}

// Taking a refusal BACK is the lead's answer to it: they put the card in
// progress, lock it, or take it off the board. Only setting the stage is
// guarded, never clearing it — a rule that trapped the card in the stage
// would leave the lead nothing to do but delete it.
func TestAnyoneCanAnswerARefusal(t *testing.T) {
	today := board.TodayIso()
	f := newFake([]board.Card{
		{ItemID: "c1", Team: "alpha", Assignees: []string{"lllamnyp"},
			Stage: board.StageRefuse, SprintStart: today, StartDate: today, Progress: 30},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	svc := f2svc(f)
	lead := WithActor(ctx, "kvaps")
	if err := svc.SetStage(lead, "acme", "c1", board.StageLocked); err != nil {
		t.Fatalf("the lead answers a refusal: %v", err)
	}
	if got := f.get("c1"); got.Stage != board.StageLocked {
		t.Fatalf("stage = %q, want locked", got.Stage)
	}
}
