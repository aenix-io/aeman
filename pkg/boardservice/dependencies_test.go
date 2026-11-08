package boardservice

import (
	"errors"
	"reflect"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

func depSeed() *fakeBackend {
	return newFake([]board.Card{
		{ItemID: "epic", Title: "Billing", Type: board.TypeEpic},
		{ItemID: "task", Title: "Invoices", Parent: "epic", Team: "a", Parked: true},
		{ItemID: "db", Title: "DB", Team: "a", Parked: true},
		{ItemID: "auth", Title: "Auth", Team: "a", Parked: true, DependsOn: []string{"db"}},
		{ItemID: "api", Title: "API", Team: "a", Parked: true, DependsOn: []string{"gone"}},
		{ItemID: "rev", Title: "review: DB", ReviewOf: "db", Team: "a"},
	}, map[string]board.SprintState{"a": {Current: board.TodayIso()}})
}

// N1, N6, N7 — the list is written whole: new ids are checked, each once, in
// order; a dangling id leaves it on this write; an unchanged list writes
// nothing; the change is logged.
func TestADependencyListIsWrittenWhole(t *testing.T) {
	f := depSeed()
	svc := New(f)
	if err := svc.SetDependsOn(ctx, "acme", "api", []string{"auth", "db", "auth", "gone"}); err != nil {
		t.Fatal(err)
	}
	if got := f.get("api").DependsOn; !reflect.DeepEqual(got, []string{"auth", "db"}) {
		t.Fatalf("dependsOn = %v, want auth, db (the dangling id dropped, each once)", got)
	}
	if !f.saw("SetDependsOn api auth,db") {
		t.Fatalf("the write: %v", f.log)
	}
	// The event names what the board holds: the dangling id is no card.
	if !f.saw("AppendEvent api " + board.EventDependsOn + " ->auth,db") {
		t.Fatalf("no depends-on event: %v", f.log)
	}
	n := f.count("SetDependsOn")
	if err := svc.SetDependsOn(ctx, "acme", "api", []string{"auth", "db"}); err != nil || f.count("SetDependsOn") != n {
		t.Fatalf("an unchanged list wrote: %v %v", err, f.log)
	}
	if err := svc.SetDependsOn(ctx, "acme", "api", nil); err != nil || len(f.get("api").DependsOn) != 0 {
		t.Fatalf("clearing: %v %v", err, f.get("api").DependsOn)
	}
}

// N1 — a bad target is refused before anything is written.
func TestABadDependencyTargetIsRefused(t *testing.T) {
	for name, tc := range map[string]struct{ card, target string }{
		"unknown":        {"task", "nowhere"},
		"itself":         {"task", "task"},
		"a review":       {"task", "rev"},
		"from review":    {"rev", "db"},
		"its ancestor":   {"task", "epic"},
		"its descendant": {"epic", "task"},
	} {
		f := depSeed()
		if err := New(f).SetDependsOn(ctx, "acme", tc.card, []string{tc.target}); !errors.Is(err, ErrDependencyTarget) {
			t.Errorf("%s: %v, want ErrDependencyTarget", name, err)
		}
		if f.count("SetDependsOn") != 0 {
			t.Errorf("%s: written before the refusal", name)
		}
	}
}

// N2 — a cycle is refused, direct and transitive.
func TestADependencyCycleIsRefused(t *testing.T) {
	f := depSeed()
	svc := New(f)
	if err := svc.SetDependsOn(ctx, "acme", "db", []string{"auth"}); !errors.Is(err, ErrDependencyCycle) {
		t.Fatalf("direct: %v", err)
	}
	if err := svc.SetDependsOn(ctx, "acme", "task", []string{"auth"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetDependsOn(ctx, "acme", "db", []string{"task"}); !errors.Is(err, ErrDependencyCycle) {
		t.Fatalf("transitive: %v", err)
	}
	// Two new ids that only close a cycle together.
	if err := svc.SetDependsOn(ctx, "acme", "db", []string{"api"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetDependsOn(ctx, "acme", "api", []string{"db"}); !errors.Is(err, ErrDependencyCycle) {
		t.Fatalf("api → db → api: %v", err)
	}
}
