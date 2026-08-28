package server

import (
	"context"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
)

// A team rename re-keys the cached roster at once — the sprint pointer moves
// to the new name, the team order and every card follow — so the very next
// read says the new name without waiting for a reload; and the write then
// lands in the repository as the team file's new name. The live board showed
// the old name for a whole sync cycle because the team's stub is not a card
// the cache could reach.
func TestRenameTeamReKeysTheCachedRoster(t *testing.T) {
	remote := gitRemote(t)
	seedGitRemote(t, remote)
	be, repo := gitStore(t, remote)
	ctx := withAction(board.WithActor(context.Background(), "kvaps"), "01JB4KA0M2P4R6T8V0X2Z4B6T1", "rename")
	svc := boardservice.New(be)
	if err := svc.RenameTeam(ctx, "board", "portal", "platform"); err != nil {
		t.Fatal(err)
	}
	// Straight from the cache, before the queue has written anything.
	bd, err := be.LoadBoard(ctx, "board")
	if err != nil {
		t.Fatal(err)
	}
	if _, old := bd.SprintStates["portal"]; old {
		t.Fatalf("the cache still keys the pointer by the old name: %v", bd.SprintStates)
	}
	st, ok := bd.SprintStates["platform"]
	if !ok || st.Current != "2026-08-24" || st.ItemID != "01JB4TEAM" {
		t.Fatalf("cached pointer under the new name = %+v (ok=%v)", st, ok)
	}
	if len(bd.TeamOrder) != 2 || bd.TeamOrder[1] != "platform" {
		t.Fatalf("team order = %v, want the renamed team in portal's place", bd.TeamOrder)
	}
	for _, c := range bd.Cards {
		if c.Team != "platform" {
			t.Fatalf("card %s still on team %q", c.Title, c.Team)
		}
	}
	// And the repository agrees once the queue has run.
	waitQueue(t, be)
	data, err := repo.ReadFile("teams/01JB4TEAM.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if s := string(data); !strings.Contains(s, "name: platform") || !strings.Contains(s, "current: 2026-08-24") {
		t.Fatalf("team file after the rename:\n%s", data)
	}
	fresh, _ := gitStore(t, remote)
	if err := be.syncNow(ctx, "board"); err != nil {
		t.Fatal(err)
	}
	if err := fresh.syncNow(ctx, "board"); err != nil {
		t.Fatal(err)
	}
	seen, _ := fresh.LoadBoard(ctx, "board")
	if _, ok := seen.SprintStates["platform"]; !ok {
		t.Fatalf("another replica does not see the renamed team: %v", seen.SprintStates)
	}
}
