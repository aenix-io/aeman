package gitstore

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
)

// users/<login>.yaml in the primary is a person's roster entry. It is a
// roster file like the others: YAML, unknown keys kept — a `personal:` line
// an older server wrote there included, which nothing reads any more and
// nothing throws away.
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
	data, err := EncodeUser(UserFile{Capacity: 40, Created: "2026-08-28T10:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "created: 2026-08-28T10:00:00Z") {
		t.Fatalf("user file:\n%s", data)
	}
	f, err := DecodeUser(append([]byte("personal: https://github.com/kvaps/aeman-personal.git\n"), data...))
	if err != nil {
		t.Fatal(err)
	}
	if f.Capacity != 40 || f.Created != "2026-08-28T10:00:00Z" || len(f.Extra) != 1 {
		t.Fatalf("decoded = %+v", f)
	}
	again, _ := EncodeUser(f)
	if !strings.Contains(string(again), "personal: https://github.com/kvaps/aeman-personal.git") {
		t.Fatalf("unknown key lost on rewrite:\n%s", again)
	}
}

// Only the primary's users count: a users file in another domain is not an
// entry this board honours (and not an unknown path either — it is the
// layout, just not read from there).
func TestLoadAllUsersFromThePrimaryOnly(t *testing.T) {
	shared := repoWith(t, map[string]string{
		BoardPath:         "schema: 1\ntitle: b\n",
		TeamPath("_"):     "rank: a\ncreated: 2026-06-01T08:00:00Z\n",
		UserPath("kvaps"): "capacity: 40\ncreated: 2026-08-28T10:00:00Z\n",
	})
	closed := repoWith(t, map[string]string{
		UserPath("bob"): "capacity: 20\n",
	})
	s, err := LoadAll([]Domain{{Name: "shared", Repo: shared}, {Name: "closed", Repo: closed}})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Users) != 1 || s.Users[0].Login != "kvaps" || s.Users[0].Capacity != 40 {
		t.Fatalf("users = %+v, want kvaps from the primary only", s.Users)
	}
	if len(s.Unknown) != 0 {
		t.Fatalf("a users file is part of the layout, got unknown %v", s.Unknown)
	}
}

// leftAt — the board day the older × left a card behind on — is a card field
// like doneAt: written to the file, read back, absent when empty. Nothing
// sets it any more; the cards that carry it still have to survive a rewrite.
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

// The last load's users are kept for the server, like the merge's issues.
func TestMultiBackendUsersFromTheLastLoad(t *testing.T) {
	shared := repoWith(t, map[string]string{
		BoardPath:         "schema: 1\ntitle: b\n",
		UserPath("kvaps"): "capacity: 40\n",
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
