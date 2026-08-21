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
