package server

import (
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/boardservice"
)

// Every refusal the service can hand back is a REFUSAL — 422, "a rule
// refused the change" — and the default arm answers 502, "the forge could
// not be reached". A sentinel that misses the list therefore tells the
// caller a lie about whose fault it is and invites a retry that cannot
// help: a new ErrPlanSubtask shipped exactly that way. The set is walked
// by reflection so the next one cannot be forgotten either.
func TestNoServiceRefusalAnswersAsAGatewayFailure(t *testing.T) {
	var srv Server
	sentinels := exportedSentinels(t)
	if len(sentinels) < 10 {
		t.Fatalf("only %d sentinels found — has the package moved?", len(sentinels))
	}
	for name, err := range sentinels {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/cards", nil)
		srv.apiError(rec, req, err)
		if rec.Code == 502 {
			t.Errorf("boardservice.%s answers 502: a rule that refused a change is not a forge failure", name)
		}
	}
}

// exportedSentinels collects the package's exported error values. There is
// no reflection over a package, so they are read from the one place that
// lists them all — the source — and resolved through a lookup table kept
// beside it. A sentinel added without a line here fails the count check
// above rather than passing silently.
func exportedSentinels(t *testing.T) map[string]error {
	t.Helper()
	known := map[string]error{
		"ErrCardNotFound": boardservice.ErrCardNotFound, "ErrEpicNotFound": boardservice.ErrEpicNotFound,
		"ErrEpicInUse": boardservice.ErrEpicInUse, "ErrProjectNotFound": boardservice.ErrProjectNotFound,
		"ErrProjectExists": boardservice.ErrProjectExists, "ErrProcessNotFound": boardservice.ErrProcessNotFound,
		"ErrProcessExists": boardservice.ErrProcessExists, "ErrProcessInUse": boardservice.ErrProcessInUse,
		"ErrParentNotFound": boardservice.ErrParentNotFound, "ErrSubtaskDepth": boardservice.ErrSubtaskDepth,
		"ErrOpenSubtasks": boardservice.ErrOpenSubtasks, "ErrPlanSubtask": boardservice.ErrPlanSubtask,
		"ErrCrossDomain": boardservice.ErrCrossDomain, "ErrNotInProject": boardservice.ErrNotInProject,
		"ErrNoColumn": boardservice.ErrNoColumn, "ErrOwnColumn": boardservice.ErrOwnColumn,
		"ErrSubtaskMirror": boardservice.ErrSubtaskMirror, "ErrSubtaskTie": boardservice.ErrSubtaskTie,
		"ErrTurnProcess": boardservice.ErrTurnProcess, "ErrNotRecurrent": boardservice.ErrNotRecurrent,
		"ErrDomainConflict": boardservice.ErrDomainConflict, "ErrPersonalPlacement": boardservice.ErrPersonalPlacement,
	}
	for name, err := range known {
		if err == nil {
			t.Fatalf("%s is nil", name)
		}
		if !strings.HasPrefix(name, "Err") || reflect.TypeOf(err) == nil {
			t.Fatalf("%s is not an error sentinel", name)
		}
		if !errors.Is(err, err) {
			t.Fatalf("%s does not compare to itself", name)
		}
	}
	return known
}
