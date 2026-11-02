package apiserver

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A project card and a planning card carry no band and no size on the wire,
// whatever the file says: the board strips both on read, so the resource
// never shows a band the Project board or the Roadmap would not draw. A
// task's own stay.
func TestAProjectCardAndAPlanningCardCarryNoZoneOrSize(t *testing.T) {
	t.Parallel()
	b := board.NewBoard([]board.Card{
		{ItemID: "plan", Title: "a plan", Scope: "Infra", Project: "P", Zone: board.ZoneRed, Size: board.SizeM},
		{ItemID: "epic", Title: "an epic", Type: board.TypeEpic, Zone: board.ZoneYellow, Size: board.SizeXL},
		{ItemID: "task", Title: "a task", Team: "t", Zone: board.ZoneGreen, Size: board.SizeS},
	})
	find := func(id string) board.Card {
		c, _ := board.Find(b, id)
		return c
	}
	for _, id := range []string{"plan", "epic"} {
		res := CardResource(b, find(id))
		if res.Spec.Zone != "" || res.Spec.Size != "" {
			t.Errorf("%s on the wire: zone %q size %q, want neither", id, res.Spec.Zone, res.Spec.Size)
		}
	}
	if res := CardResource(b, find("task")); res.Spec.Zone == "" || res.Spec.Size != "S" {
		t.Errorf("a task keeps its band and size on the wire: %+v", res.Spec)
	}
}
