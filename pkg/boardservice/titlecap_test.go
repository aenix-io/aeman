package boardservice

import (
	"errors"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A title is a one-line headline that rides into card files, commit trailers
// and every watch frame, so both doors refuse an over-long one — the request
// body cap stops the gigabyte title, this keeps a merely huge one off the
// board (security finding: no title length cap).
func TestTitleLengthIsCappedAtBothDoors(t *testing.T) {
	long := strings.Repeat("x", MaxTitleLen+1)
	f := newFake([]board.Card{{ItemID: "c1", Team: "alpha", Title: "ok"}},
		map[string]board.SprintState{"alpha": {Current: board.TodayIso()}})
	svc := f2svc(f)
	if _, err := svc.CreateCard(WithActor(ctx, "kvaps"), "acme", CreateCardArgs{Title: long, Team: "alpha"}); !errors.Is(err, ErrTitleTooLong) {
		t.Fatalf("create with a huge title: err = %v, want ErrTitleTooLong", err)
	}
	if err := svc.Rename(ctx, "acme", "c1", long); !errors.Is(err, ErrTitleTooLong) {
		t.Fatalf("rename to a huge title: err = %v, want ErrTitleTooLong", err)
	}
	if err := svc.Rename(ctx, "acme", "c1", strings.Repeat("y", MaxTitleLen)); err != nil {
		t.Fatalf("a title at the cap was refused: %v", err)
	}
}
