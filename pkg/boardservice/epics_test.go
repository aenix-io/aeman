package boardservice

import (
	"context"
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

func epicBoard() *fakeBackend {
	return newFake([]board.Card{
		{ItemID: "e1", Title: board.EpicStateTitle, Epic: "Infra"},
		{ItemID: "c1", Team: "alpha", Title: "vGPU setup", Epic: "Infra", Week: "2026-08-17",
			StartDate: "2026-08-17", Day: "2026-08-28"},
	}, map[string]board.SprintState{"alpha": {Current: board.TodayIso(), ItemID: "s1"}})
}

// An epic is declared by its hidden state card — the team-roster mechanism —
// and duplicates are refused case-insensitively.
func TestAddEpic(t *testing.T) {
	fake := epicBoard()
	svc := New(fake)
	if err := svc.AddEpic(context.Background(), "acme", 1, "Console"); err != nil {
		t.Fatal(err)
	}
	last := fake.creates[len(fake.creates)-1]
	if last.Title != board.EpicStateTitle || last.Epic != "Console" {
		t.Fatalf("epic-state create = %+v", last)
	}
	if err := svc.AddEpic(context.Background(), "acme", 1, "infra"); !errors.Is(err, ErrEpicExists) {
		t.Fatalf("a case-insensitive duplicate must be refused, got %v", err)
	}
	if err := svc.AddEpic(context.Background(), "acme", 1, "  "); err == nil {
		t.Fatal("an empty name must be refused")
	}
}

// Deleting an epic with cards is protected; an empty one removes its state card.
func TestDeleteEpic(t *testing.T) {
	fake := epicBoard()
	svc := New(fake)
	if err := svc.DeleteEpic(context.Background(), "acme", 1, "Infra"); !errors.Is(err, ErrEpicInUse) {
		t.Fatalf("an epic with cards must be protected, got %v", err)
	}
	if err := svc.SetEpic(context.Background(), "acme", 1, "c1", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteEpic(context.Background(), "acme", 1, "Infra"); err != nil {
		t.Fatal(err)
	}
	if fake.count("DeleteCard e1") == 0 {
		t.Fatalf("the state card must be deleted; log=%v", fake.log)
	}
}

// Filing a card under an epic validates the column exists — a typo must not
// mint a phantom column.
func TestSetEpicValidates(t *testing.T) {
	fake := epicBoard()
	svc := New(fake)
	if err := svc.SetEpic(context.Background(), "acme", 1, "c1", "Nope"); err == nil {
		t.Fatal("an unknown epic must be refused")
	}
	if err := svc.SetEpic(context.Background(), "acme", 1, "c1", "Infra"); err != nil {
		t.Fatalf("a no-op re-file must pass: %v", err)
	}
}

// An epic card is created on the Plan board: filed under its column, anchored
// to its week, spanning its dates, and joining NO sprint — so it stays off
// the day boards until a team takes it up.
func TestCreateCardUnderEpic(t *testing.T) {
	fake := epicBoard()
	svc := New(fake)
	card, err := svc.CreateCard(context.Background(), "acme", 1, CreateCardArgs{
		Title: "KMS encryption", Epic: "Infra",
		Start: "2026-09-14", Day: "2026-09-25",
	})
	if err != nil {
		t.Fatal(err)
	}
	if card.Epic != "Infra" || card.Week != "2026-09-14" {
		t.Fatalf("epic/week = %q/%q", card.Epic, card.Week)
	}
	if card.SprintStart != "" {
		t.Fatalf("an epic card must join no sprint, got %q", card.SprintStart)
	}
	if card.StartDate != "2026-09-14" || card.Day != "2026-09-25" {
		t.Fatalf("span = %q..%q", card.StartDate, card.Day)
	}
	if _, err := svc.CreateCard(context.Background(), "acme", 1, CreateCardArgs{
		Title: "typo", Epic: "Nope",
	}); err == nil {
		t.Fatal("an unknown epic must be refused on create")
	}

	// And the day boards do not smear its multi-week span.
	b, err := svc.Board(context.Background(), "acme", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := board.TeamGrid(b, "", "2026-09-15"); len(got) != 0 {
		t.Fatalf("a sprint-less epic card must stay off the day grid, got %+v", got)
	}
}

// The board parses epic-state cards into the ordered column list.
func TestBoardParsesEpics(t *testing.T) {
	b := board.NewBoard(nil, []board.Card{
		{ItemID: "e2", Title: board.EpicStateTitle, Epic: "Console"},
		{ItemID: "e1", Title: board.EpicStateTitle, Epic: "Infra"},
		{ItemID: "c1", Title: "работа", Epic: "Infra"},
	})
	if len(b.Epics) != 2 || b.Epics[0] != "Console" || b.Epics[1] != "Infra" {
		t.Fatalf("epics = %v (board order = state-card positions)", b.Epics)
	}
	if b.EpicStates["Infra"] != "e1" {
		t.Fatalf("state map = %v", b.EpicStates)
	}
	if len(b.Cards) != 1 {
		t.Fatalf("state cards must be split out of Cards, got %d", len(b.Cards))
	}
}

// Handing an epic card to a team files it into the team's WEEKLY plan (band +
// week carry it there); it must NOT join today's sprint — a multi-week slot
// in the current sprint would smear across the team's whole day grid.
func TestSetTeamKeepsEpicCardPlanLevel(t *testing.T) {
	today := board.TodayIso()
	fake := newFake([]board.Card{
		{ItemID: "e1", Title: board.EpicStateTitle, Epic: "Infra"},
		{ItemID: "slot", Title: "vGPU rollout", Epic: "Infra", Week: board.MondayOf(today),
			StartDate: board.MondayOf(today), Day: board.AddDays(board.MondayOf(today), 18)},
		{ItemID: "day", Title: "ordinary", Team: "", StartDate: today, Day: today, SprintStart: today},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	svc := New(fake)

	if err := svc.SetTeam(context.Background(), "acme", 1, "slot", "alpha", ""); err != nil {
		t.Fatal(err)
	}
	got := fake.get("slot")
	if got.Team != "alpha" {
		t.Fatalf("team = %q", got.Team)
	}
	if got.SprintStart != "" {
		t.Fatalf("an epic card must stay out of the sprint on team assignment, got %q", got.SprintStart)
	}

	// An ordinary card still joins the team's sprint — the old behaviour.
	if err := svc.SetTeam(context.Background(), "acme", 1, "day", "alpha", ""); err != nil {
		t.Fatal(err)
	}
	if got := fake.get("day"); got.SprintStart != today {
		t.Fatalf("an ordinary card joins the team sprint, got %q", got.SprintStart)
	}

	// An epic card ALREADY in work follows the normal rule (it has a sprint).
	if err := svc.SetSprintStart(context.Background(), "acme", 1, "slot", today); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetTeam(context.Background(), "acme", 1, "slot", "alpha", ""); err != nil {
		t.Fatal(err)
	}
	if got := fake.get("slot"); got.SprintStart != today {
		t.Fatalf("an in-work epic card keeps sprint semantics, got %q", got.SprintStart)
	}
}

// Filing under a column that does not exist is a rejected input, not an
// upstream failure: the API must answer 422 (and say which name), so a typo
// reads as a typo instead of "the server is broken".
func TestUnknownEpicIsTyped(t *testing.T) {
	fake := epicBoard()
	svc := New(fake)
	err := svc.SetEpic(context.Background(), "acme", 1, "c1", "Ghost")
	if !errors.Is(err, ErrEpicNotFound) {
		t.Fatalf("SetEpic error = %v, want ErrEpicNotFound", err)
	}
	_, err = svc.CreateCard(context.Background(), "acme", 1, CreateCardArgs{Title: "x", Epic: "Ghost"})
	if !errors.Is(err, ErrEpicNotFound) {
		t.Fatalf("CreateCard error = %v, want ErrEpicNotFound", err)
	}
}
