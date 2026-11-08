package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/apiserver"
	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
)

// N7 over HTTP (ADR 0002): PATCH spec.dependsOn writes the whole list; the
// card reads back locked with its blocker; a cycle and a bad target are
// refused by name.
func TestDependenciesOverHTTP(t *testing.T) {
	today := board.TodayIso()
	fake := boardservicetest.New([]board.Card{
		{ItemID: "db", Title: "DB", Team: "alpha", Parked: true},
		{ItemID: "api", Title: "API", Team: "alpha", Parked: true},
		{ItemID: "rev", Title: "review: DB", ReviewOf: "db", Team: "alpha"},
	}, map[string]board.SprintState{"alpha": {Current: today, ItemID: "s1"}})
	srv := apiServer(t, Options{}, fake)
	srv.apiTokens = func(*http.Request) (string, string, error) { return "tok", "bob", nil }

	rec := do(t, srv, http.MethodPatch, "/api/v1/cards/api", `{"dependsOn":["db"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch = %d %s", rec.Code, rec.Body.String())
	}
	var c apiserver.Card
	_ = json.Unmarshal(rec.Body.Bytes(), &c)
	if len(c.Spec.DependsOn) != 1 || !c.Status.Locked || len(c.Status.BlockedBy) != 1 || c.Status.BlockedBy[0].UID != "db" {
		t.Fatalf("the card = %+v / %+v", c.Spec.DependsOn, c.Status)
	}
	for body, code := range map[string]string{
		`{"dependsOn":["api"]}`:  "dependencyCycle",
		`{"dependsOn":["rev"]}`:  "dependencyTarget",
		`{"dependsOn":["nope"]}`: "dependencyTarget",
	} {
		if rec := do(t, srv, http.MethodPatch, "/api/v1/cards/db", body); rec.Code != http.StatusUnprocessableEntity ||
			!strings.Contains(rec.Body.String(), code) {
			t.Fatalf("%s = %d %s, want 422 %s", body, rec.Code, rec.Body.String(), code)
		}
	}
	if rec := do(t, srv, http.MethodPatch, "/api/v1/cards/api", `{"dependsOn":[]}`); rec.Code != http.StatusOK || len(fake.Card("api").DependsOn) != 0 {
		t.Fatalf("clear = %d %v", rec.Code, fake.Card("api").DependsOn)
	}
}

// N4 — a visitor's list is the readable part: writing it keeps a dependency
// on a card they cannot read (their board does not hold it, so their list
// cannot name it), and a write they may not make is refused.
func TestAVisitorsDependencyWriteKeepsWhatTheyCannotSee(t *testing.T) {
	fake := boardservicetest.New([]board.Card{
		{ItemID: "api", Title: "API", Team: "alpha", Parked: true, DependsOn: []string{"secret", "db"}, Domain: "shared"},
		{ItemID: "db", Title: "DB", Team: "alpha", Parked: true, Domain: "shared"},
		{ItemID: "auth", Title: "Auth", Team: "alpha", Parked: true, Domain: "shared"},
		{ItemID: "secret", Title: "Secret", Team: "alpha", Parked: true, Domain: "closed"},
	}, nil)
	v := &visibleBackend{Backend: fake, primary: "shared", domains: []string{"shared", "closed"}}
	ctx := withRights(t.Context(), rightsOn([]string{"shared"}, []string{"shared"}))
	whole, _ := fake.LoadBoard(ctx, "acme")
	api, _ := board.Find(whole, "api")
	if err := v.SetDependsOn(ctx, whole, api, []string{"auth"}); err != nil {
		t.Fatal(err)
	}
	if got := fake.Card("api").DependsOn; len(got) != 2 || got[0] != "secret" || got[1] != "auth" {
		t.Fatalf("dependsOn = %v, want secret kept where it stood and auth (the visitor's)", got)
	}
	readOnly := withRights(t.Context(), rightsOn([]string{"shared"}, nil))
	if err := v.SetDependsOn(readOnly, whole, api, nil); err == nil {
		t.Fatal("a visitor who may not write the card wrote its dependencies")
	}
}

// N1, N2 — a visitor's new dependency is judged on the WHOLE board: their
// board breaks a lineage at a card they cannot read and stops a cycle's walk
// there. Refused alike, and the refusal names nothing they cannot read.
func TestAVisitorsDependencyIsJudgedOnTheWholeBoard(t *testing.T) {
	fake := boardservicetest.New([]board.Card{
		// X sits under the hidden epic E, under the goal G: X → G is a wait
		// on its own ancestor.
		{ItemID: "G", Title: "Goal", Type: board.TypeGoal, Domain: "shared"},
		{ItemID: "E", Title: "Hidden epic", Type: board.TypeEpic, Parent: "G", Domain: "closed"},
		{ItemID: "X", Title: "X", Parent: "E", Team: "alpha", Parked: true, Domain: "shared"},
		// B waits for the hidden H, which waits for A: A → B closes a cycle.
		{ItemID: "A", Title: "A", Team: "alpha", Parked: true, Domain: "shared"},
		{ItemID: "B", Title: "B", Team: "alpha", Parked: true, DependsOn: []string{"H"}, Domain: "shared"},
		{ItemID: "H", Title: "Hidden task", Team: "alpha", Parked: true, DependsOn: []string{"A"}, Domain: "closed"},
	}, nil)
	v := &visibleBackend{Backend: fake, primary: "shared", domains: []string{"shared", "closed"}}
	svc := boardservice.New(v)
	ctx := withRights(t.Context(), rightsOn([]string{"shared"}, []string{"shared"}))
	for _, tc := range []struct {
		card, target string
		want         error
	}{
		{"X", "G", boardservice.ErrDependencyTarget},
		{"A", "B", boardservice.ErrDependencyCycle},
	} {
		err := svc.SetDependsOn(ctx, "acme", tc.card, []string{tc.target})
		if !errors.Is(err, tc.want) {
			t.Errorf("%s → %s = %v, want %v", tc.card, tc.target, err, tc.want)
		}
		if err != nil && (strings.Contains(err.Error(), "Hidden") || strings.Contains(err.Error(), "\"E\"") || strings.Contains(err.Error(), "\"H\"")) {
			t.Errorf("the refusal names a card the visitor cannot read: %v", err)
		}
		if got := fake.Card(tc.card).DependsOn; len(got) != 0 {
			t.Errorf("%s was written anyway: %v", tc.card, got)
		}
	}
}

// N6 — the visitor's list is compared with the readable part of the card's
// as a SET: an unchanged set — reordered, or with hidden dependencies the
// visitor cannot name — writes nothing and records nothing.
func TestAVisitorsUnchangedDependencySetWritesNothing(t *testing.T) {
	fake := boardservicetest.New([]board.Card{
		{ItemID: "api", Title: "API", Team: "alpha", Parked: true, DependsOn: []string{"db", "secret", "auth"}, Domain: "shared"},
		{ItemID: "db", Title: "DB", Team: "alpha", Parked: true, Domain: "shared"},
		{ItemID: "auth", Title: "Auth", Team: "alpha", Parked: true, Domain: "shared"},
		{ItemID: "secret", Title: "Secret", Team: "alpha", Parked: true, Domain: "closed"},
	}, nil)
	v := &visibleBackend{Backend: fake, primary: "shared", domains: []string{"shared", "closed"}}
	svc := boardservice.New(v)
	ctx := withRights(t.Context(), rightsOn([]string{"shared"}, []string{"shared"}))
	for _, ids := range [][]string{{"db", "auth"}, {"auth", "db"}} {
		if err := svc.SetDependsOn(ctx, "acme", "api", ids); err != nil {
			t.Fatal(err)
		}
	}
	if n := fake.Count("SetDependsOn"); n != 0 {
		t.Fatalf("an unchanged set wrote %d times", n)
	}
	if n := fake.Count("AppendEvent api depends-on"); n != 0 {
		t.Fatalf("an unchanged set recorded %d events", n)
	}
	// A real change keeps the hidden one where it stood and names only what
	// the visitor changed.
	if err := svc.SetDependsOn(ctx, "acme", "api", []string{"auth"}); err != nil {
		t.Fatal(err)
	}
	// db, secret, auth → the visitor's auth takes db's place; secret stays
	// second, where it stood.
	if got := fake.Card("api").DependsOn; strings.Join(got, ",") != "auth,secret" {
		t.Fatalf("dependsOn = %v, want auth and secret kept in its place", got)
	}
	ev := fake.Events("api")
	if len(ev) != 1 || ev[0].From != "db,auth" || ev[0].To != "auth" {
		t.Fatalf("the event = %+v, want db,auth → auth (nothing hidden named)", ev)
	}
}

// N2 — the check that matters runs where writes are put in order: the
// store judges a list against its cache under the write lock, so two
// requests that each saw the board before the other's write cannot close a
// cycle between them.
func TestTwoDependencyWritesCannotCloseACycleBetweenThem(t *testing.T) {
	store := newBoardStore()
	inner := boardservicetest.New(nil, nil)
	be := &storeBackend{inner: inner, store: store}
	bd := board.NewBoard([]board.Card{
		{ItemID: "a", Title: "A", Team: "alpha", Parked: true},
		{ItemID: "b", Title: "B", Team: "alpha", Parked: true},
	})
	bd.Board = "acme"
	seedEntry(store, "acme", bd)
	a, _ := board.Find(bd, "a")
	b, _ := board.Find(bd, "b") // read before a's write, as a concurrent request would
	if err := be.SetDependsOn(t.Context(), bd, a, []string{"b"}); err != nil {
		t.Fatal(err)
	}
	if err := be.SetDependsOn(t.Context(), bd, b, []string{"a"}); !errors.Is(err, boardservice.ErrDependencyCycle) {
		t.Fatalf("b → a after a → b = %v, want a cycle", err)
	}
}

// A visitor's list is merged with the hidden dependencies the card has NOW,
// under the write lock — not with a snapshot read before it: a hidden
// dependency another request added in between stays on the card.
func TestAVisitorsDependencyMergeReadsTheCardUnderTheLock(t *testing.T) {
	store := newBoardStore()
	inner := boardservicetest.New(nil, nil)
	be := &storeBackend{inner: inner, store: store}
	bd := board.NewBoard([]board.Card{
		{ItemID: "api", Title: "API", Team: "alpha", Parked: true, DependsOn: []string{"secret", "db"}},
		{ItemID: "db", Title: "DB", Team: "alpha", Parked: true},
		{ItemID: "auth", Title: "Auth", Team: "alpha", Parked: true},
		{ItemID: "secret", Title: "Secret", Team: "alpha", Parked: true, Domain: "closed"},
	})
	bd.Board = "acme"
	e := seedEntry(store, "acme", bd)
	// The visitor's copy was read before `secret` was added.
	stale := board.Card{ItemID: "api", Title: "API", Team: "alpha", Parked: true, DependsOn: []string{"db"}}
	canRead := func(c board.Card) bool { return c.Domain != "closed" }
	if err := be.setDependsOnKeeping(t.Context(), bd, stale, []string{"auth"}, canRead); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	got, _ := board.Find(e.board, "api")
	e.mu.Unlock()
	if len(got.DependsOn) != 2 || got.DependsOn[0] != "secret" || got.DependsOn[1] != "auth" {
		t.Fatalf("dependsOn = %v, want the hidden secret kept where it stands now, and auth", got.DependsOn)
	}
}

// Which of the card's dependencies the visitor cannot see is judged on the
// board under the lock too, not on the snapshot the request read: a hidden
// target that reached the cache after that read is still hidden, and so
// still kept, though the visitor's snapshot never held it.
func TestAVisitorsHiddenDependencyIsJudgedUnderTheLock(t *testing.T) {
	store := newBoardStore()
	inner := boardservicetest.New(nil, nil)
	be := &storeBackend{inner: inner, store: store}
	bd := board.NewBoard([]board.Card{
		{ItemID: "api", Title: "API", Team: "alpha", Parked: true, DependsOn: []string{"late"}},
		{ItemID: "auth", Title: "Auth", Team: "alpha", Parked: true},
		{ItemID: "late", Title: "Late", Team: "alpha", Parked: true, Domain: "closed"},
	})
	bd.Board = "acme"
	e := seedEntry(store, "acme", bd)
	stale := board.Card{ItemID: "api", Title: "API", Team: "alpha", Parked: true}
	canRead := func(c board.Card) bool { return c.Domain != "closed" }
	if err := be.setDependsOnKeeping(t.Context(), bd, stale, []string{"auth"}, canRead); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	got, _ := board.Find(e.board, "api")
	e.mu.Unlock()
	if !slices.Equal(got.DependsOn, []string{"late", "auth"}) {
		t.Fatalf("dependsOn = %v, want the hidden late kept and auth", got.DependsOn)
	}
}
