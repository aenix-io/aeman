package tokenstoretest_test

import (
	"sync"
	"testing"

	"github.com/aenix-io/aeman/internal/tokenstore/tokenstoretest"
)

// The counters are readable while the store is being asked. The real
// store is shared across every request, so a double that can only be
// counted after everything has stopped cannot stand in for it — and a
// bare field read here is the race the accessors exist to prevent, which
// only shows up under -race once a test looks at a count mid-run.
func TestCountersAreReadableWhileTheStoreIsBusy(t *testing.T) {
	const readers = 8
	f := tokenstoretest.NewFake().Put("github.com", "ghp_x")

	var wg sync.WaitGroup
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.Get("github.com"); err != nil {
				t.Errorf("Get = %v", err)
			}
		}()
	}
	for range 50 {
		_ = f.Gets()
	}
	wg.Wait()

	if got := f.Gets(); got != readers {
		t.Fatalf("Gets = %d, want %d", got, readers)
	}
	if got := f.Sets(); got != 0 {
		t.Fatalf("Sets = %d; Put must not count as a write", got)
	}
}
