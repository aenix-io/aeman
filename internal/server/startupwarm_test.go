package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
)

// The warm roster survives a restart: a session-bound load records which
// board is warmed on whose login, and a fresh store reads the roster back —
// that is what startupWarm feeds on so a deploy leaves no cold-cache pit.
func TestWarmRosterSurvivesRestart(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sessions.json.boards")
	store := newBoardStore()
	store.warmFile = file
	fake := boardservicetest.New([]board.Card{{ItemID: "c1", Title: "one"}}, nil)
	be := &storeBackend{
		inner: fake, store: store, multiUser: true,
		warmAlive: func() bool { return true },
	}
	ctx := board.WithActor(context.Background(), "kvaps")
	if _, err := be.LoadBoard(ctx, "acme", 7); err != nil {
		t.Fatal(err)
	}

	restarted := newBoardStore()
	restarted.warmFile = file
	roster := restarted.loadWarmRoster()
	if roster["acme/7"] != "kvaps" {
		t.Fatalf("roster after restart = %v, want acme/7 -> kvaps", roster)
	}
}

// An MCP-style load (no session liveness check) must not be recorded: its
// token has nothing to ever stop the warmer, so it may not power one.
func TestUncheckedTokenNotRecorded(t *testing.T) {
	file := filepath.Join(t.TempDir(), "roster.json")
	store := newBoardStore()
	store.warmFile = file
	fake := boardservicetest.New(nil, nil)
	be := &storeBackend{inner: fake, store: store, multiUser: true} // no warmAlive
	ctx := board.WithActor(context.Background(), "kvaps")
	if _, err := be.LoadBoard(ctx, "acme", 7); err != nil {
		t.Fatal(err)
	}
	if roster := newBoardStoreWithFile(file).loadWarmRoster(); len(roster) != 0 {
		t.Fatalf("an unchecked token was recorded: %v", roster)
	}
}

func newBoardStoreWithFile(file string) *boardStore {
	s := newBoardStore()
	s.warmFile = file
	return s
}

// newestSessionFor picks the login's freshest live session — the way back
// onto a token after a restart.
func TestNewestSessionFor(t *testing.T) {
	a := newAuthManager(OAuthConfig{ClientID: "id", ClientSecret: "s", BaseURL: "http://x"}, nil)
	now := time.Now()
	a.sessions["old"] = oauthSession{token: "t-old", login: "kvaps", created: now.Add(-2 * time.Hour)}
	a.sessions["new"] = oauthSession{token: "t-new", login: "kvaps", created: now.Add(-time.Minute)}
	a.sessions["other"] = oauthSession{token: "t-x", login: "tym83", created: now}
	sid, s, ok := a.newestSessionFor(context.Background(), "kvaps")
	if !ok || sid != "new" || s.token != "t-new" {
		t.Fatalf("got %q %q %v, want the freshest kvaps session", sid, s.token, ok)
	}
	if _, _, ok := a.newestSessionFor(context.Background(), "nobody"); ok {
		t.Fatal("a login with no sessions must not resolve")
	}
}
