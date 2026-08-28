package gitstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
)

// users/<login>.yaml in the primary links a person to their personal
// repository. It is a roster file like the others: YAML, unknown keys kept.
func TestUserFileRoundTripAndPath(t *testing.T) {
	if UserPath("kvaps") != "users/kvaps.yaml" {
		t.Fatalf("UserPath = %q", UserPath("kvaps"))
	}
	if kind, ids := ParsePath("users/kvaps.yaml"); kind != PathUser || len(ids) != 1 || ids[0] != "kvaps" {
		t.Fatalf("ParsePath(users/kvaps.yaml) = %v %v", kind, ids)
	}
	if kind, _ := ParsePath("users/kvaps"); kind != PathUnknown {
		t.Fatalf("a users entry without .yaml is not a user file: %v", kind)
	}
	data, err := EncodeUser(UserFile{Personal: "https://github.com/kvaps/aeman-personal.git", Created: "2026-08-28T10:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "personal: https://github.com/kvaps/aeman-personal.git") {
		t.Fatalf("user file:\n%s", data)
	}
	f, err := DecodeUser(append(data, []byte("note: kept\n")...))
	if err != nil {
		t.Fatal(err)
	}
	if f.Personal != "https://github.com/kvaps/aeman-personal.git" || f.Created != "2026-08-28T10:00:00Z" || len(f.Extra) != 1 {
		t.Fatalf("decoded = %+v", f)
	}
	again, _ := EncodeUser(f)
	if !strings.Contains(string(again), "note: kept") {
		t.Fatalf("unknown key lost on rewrite:\n%s", again)
	}
}

// Only the primary's users count: a users file in another domain is not a
// link this board honours (and not an unknown path either — it is the
// layout, just not read from there).
func TestLoadAllUsersFromThePrimaryOnly(t *testing.T) {
	shared := repoWith(t, map[string]string{
		BoardPath:         "schema: 1\ntitle: b\n",
		TeamPath("_"):     "rank: a\ncreated: 2026-06-01T08:00:00Z\n",
		UserPath("kvaps"): "personal: https://github.com/kvaps/p.git\ncreated: 2026-08-28T10:00:00Z\n",
	})
	closed := repoWith(t, map[string]string{
		UserPath("bob"): "personal: https://github.com/bob/p.git\n",
	})
	s, err := LoadAll([]Domain{{Name: "shared", Repo: shared}, {Name: "closed", Repo: closed}})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Users) != 1 || s.Users[0].Login != "kvaps" || s.Users[0].Personal != "https://github.com/kvaps/p.git" {
		t.Fatalf("users = %+v, want kvaps from the primary only", s.Users)
	}
	if len(s.Unknown) != 0 {
		t.Fatalf("a users file is part of the layout, got unknown %v", s.Unknown)
	}
}

// leftAt — the board day a personal card was left behind on by the × — is a
// card field like doneAt: written to the file, read back, absent when empty.
func TestLeftAtRoundTripsThroughTheCardFile(t *testing.T) {
	data, err := EncodeCard(CardFile{Card: board.Card{Title: "half done", Progress: 40, LeftAt: "2026-08-27"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "leftAt: 2026-08-27") {
		t.Fatalf("card file lacks leftAt:\n%s", data)
	}
	f, err := DecodeCard("01JB4K2E7QZMX3R8V0N5T9WYP1", data)
	if err != nil || f.Card.LeftAt != "2026-08-27" {
		t.Fatalf("decoded leftAt = %q, %v", f.Card.LeftAt, err)
	}
	data, err = EncodeCard(CardFile{Card: board.Card{Title: "back", Progress: 40}})
	if err != nil || strings.Contains(string(data), "leftAt") {
		t.Fatalf("a card not left behind carries no leftAt: %v\n%s", err, data)
	}
}

// doneAt is the board day a write took the card to 100 — what lets a done
// card be shown that day and hidden the next without reading history — and
// it goes when the card drops below 100, like doneFrom.
func TestSetProgressWritesDoneAtAndReopenClearsIt(t *testing.T) {
	be, repo := newBackend(t)
	ctx := ctxAs("kvaps")
	b, _ := be.LoadBoard(ctx, "acme")
	c, err := be.CreateCard(ctx, b, board.CreateInput{Title: "mine", Team: "portal"})
	if err != nil {
		t.Fatal(err)
	}
	if err := be.SetProgress(ctx, b, c, 100); err != nil {
		t.Fatal(err)
	}
	p, _ := CardPath(c.ItemID)
	data, _ := repo.ReadFile(p)
	today := board.LocalDateIso(be.now().UTC().Format(time.RFC3339))
	if !strings.Contains(string(data), "doneAt: "+today) {
		t.Fatalf("done card lacks doneAt %s:\n%s", today, data)
	}
	f, _ := DecodeCard(c.ItemID, data)
	if f.Card.DoneAt != today {
		t.Fatalf("decoded doneAt = %q", f.Card.DoneAt)
	}
	if err := be.SetProgress(ctx, b, f.Card, 40); err != nil {
		t.Fatal(err)
	}
	data, _ = repo.ReadFile(p)
	if strings.Contains(string(data), "doneAt") {
		t.Fatalf("reopened card still carries doneAt:\n%s", data)
	}
}

// A personal domain is a repository one person owns; its cards are pinned to
// it: the home rule (team → project → primary) never moves them out, and a
// card linked to a personal card (a subtask, a review) follows it in.
func TestMultiBackendPersonalDomainPinsItsCards(t *testing.T) {
	mb, shared, _ := twoDomains(t)
	personal := repoWith(t, map[string]string{BoardPath: "schema: 1\ntitle: kvaps\n"})
	ctx := ctxAs("kvaps")
	if err := mb.AddDomain(Domain{Name: "~kvaps", Repo: personal}); err != nil {
		t.Fatal(err)
	}
	b, _ := mb.LoadBoard(ctx, "x")
	mine, err := mb.CreateCard(ctx, b, board.CreateInput{Title: "mine", Personal: true, Domain: "~kvaps"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := CardPath(mine.ItemID)
	if _, err := personal.ReadFile(p); err != nil || mine.Domain != "~kvaps" {
		t.Fatalf("personal card not in the personal repository: %v (domain %q)", err, mine.Domain)
	}
	// Setting a team that lives in shared would re-file an ordinary card;
	// a personal card stays home.
	b, _ = mb.LoadBoard(ctx, "x")
	if err := mb.SetTeam(ctx, b, mine, "portal"); err != nil {
		t.Fatal(err)
	}
	if _, err := personal.ReadFile(p); err != nil {
		t.Fatal("the personal card left its repository on a team change")
	}
	if _, err := shared.ReadFile(p); err == nil {
		t.Fatal("the personal card was copied into shared")
	}
	// A subtask of a personal card is personal by the linked-cards rule.
	sub, err := mb.CreateCard(ctx, b, board.CreateInput{Title: "step", Parent: mine.ItemID})
	if err != nil {
		t.Fatal(err)
	}
	if sub.Domain != "~kvaps" {
		t.Fatalf("subtask of a personal card landed in %q", sub.Domain)
	}
	// Personal needs a domain the board has; nothing is invented.
	if _, err := mb.CreateCard(ctx, b, board.CreateInput{Title: "x", Personal: true, Domain: "~nobody"}); !errors.Is(err, ErrUnknownDomain) {
		t.Fatalf("unknown personal domain: err = %v", err)
	}
	if _, err := mb.CreateCard(ctx, b, board.CreateInput{Title: "x", Personal: true}); !errors.Is(err, ErrUnknownDomain) {
		t.Fatalf("personal without a domain: err = %v", err)
	}
	// The domain can go again; its cards go with it.
	if err := mb.RemoveDomain("~kvaps"); err != nil {
		t.Fatal(err)
	}
	b, _ = mb.LoadBoard(ctx, "x")
	for _, c := range b.Cards {
		if c.ItemID == mine.ItemID {
			t.Fatal("a removed domain's card is still served")
		}
	}
	if err := mb.RemoveDomain("shared"); err == nil {
		t.Fatal("the primary cannot be removed")
	}
}

// The last load's users are kept for the server, like the merge's issues.
func TestMultiBackendUsersFromTheLastLoad(t *testing.T) {
	shared := repoWith(t, map[string]string{
		BoardPath:         "schema: 1\ntitle: b\n",
		UserPath("kvaps"): "personal: https://github.com/kvaps/p.git\n",
	})
	mb := NewMultiBackend([]Domain{{Name: "shared", Repo: shared}}, BackendOptions{})
	if len(mb.Users()) != 0 {
		t.Fatal("no load yet, no users")
	}
	if _, err := mb.LoadBoard(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	users := mb.Users()
	if len(users) != 1 || users[0].Login != "kvaps" {
		t.Fatalf("users = %+v", users)
	}
}
