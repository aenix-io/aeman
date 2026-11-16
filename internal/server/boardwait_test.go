package server

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	forgepkg "github.com/aenix-io/aeman/internal/forge"
)

// GET /board must not wait on the forge. Asking who reads a repository is
// a listing plus one request per login the listing does not name, and it
// runs on every board load: with a login the answer did not cover — a card
// assigned to somebody new, which is what a morning of planning produces —
// the page waited for all of it. Measured on the real board: five to nine
// seconds, again and again.
func TestTheBoardDoesNotWaitOnTheForgeOnceItIsWarm(t *testing.T) {
	const probeTime = 400 * time.Millisecond
	var calls atomic.Int32
	srv := memberForge(t, &calls, probeTime)
	fa := newForgeAccess(forgepkg.NewGitHubAt(srv.URL), guardedClient(srv),
		[]RepoSpec{{Name: "shared", URL: "https://github.com/acme/shared.git"}}, "srv-token", nil)
	ctx := context.Background()

	// The first board of a fresh process pays once: nobody has ever asked
	// about this repository, and an empty picker would be a worse answer
	// than a slow one.
	first, err := fa.readers(ctx, "shared", []string{"kvaps"})
	if err != nil || len(first) != 1 {
		t.Fatalf("the cold answer: %v, %v", first, err)
	}

	// From then on nobody waits — not for a login nobody has heard of…
	started := time.Now()
	got, err := fa.readers(ctx, "shared", []string{"kvaps", "newcomer"})
	took := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	if took > probeTime/2 {
		t.Fatalf("a warm board waited %v on the forge", took)
	}
	if len(got) != 2 {
		t.Fatalf("and the newcomer is offered while the forge is asked: %v", got)
	}

	// …nor for an answer that has merely aged.
	waitForCalls(t, &calls, 2)
	started = time.Now()
	if _, err := fa.readers(ctx, "shared", []string{"kvaps", "newcomer"}); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(started); took > probeTime/2 {
		t.Fatalf("a settled board waited %v on the forge", took)
	}
}

// A login the forge is being asked about right now has no answer yet, and
// no answer is not "no": a board loaded while the question is in flight
// still offers the newcomer. Only a question that came back without them
// takes them off.
func TestANewcomerIsOfferedWhileTheForgeIsBeingAsked(t *testing.T) {
	const probeTime = 400 * time.Millisecond
	var calls atomic.Int32
	srv := memberForge(t, &calls, probeTime)
	fa := newForgeAccess(forgepkg.NewGitHubAt(srv.URL), guardedClient(srv),
		[]RepoSpec{{Name: "shared", URL: "https://github.com/acme/shared.git"}}, "srv-token", nil)
	ctx := context.Background()
	if _, err := fa.readers(ctx, "shared", []string{"kvaps"}); err != nil {
		t.Fatal(err)
	}

	if _, err := fa.readers(ctx, "shared", []string{"kvaps", "newcomer"}); err != nil {
		t.Fatal(err)
	}
	waitForCalls(t, &calls, 2) // the background question has reached the forge

	got, err := fa.readers(ctx, "shared", []string{"kvaps", "newcomer"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("a board loaded while the forge is asked dropped the newcomer: %v", got)
	}
}

// The answer lands in one step: a board read while the answer is being
// recorded — here, held at handing the names to the directory — sees a
// login the forge does name either unasked or answered, never asked with
// no answer.
func TestANewcomerIsOfferedWhileTheAnswerIsRecorded(t *testing.T) {
	var calls atomic.Int32
	srv := memberForge(t, &calls, 0)
	dir := newPeople(forgepkg.NewGitHubAt(srv.URL), guardedClient(srv), nil)
	fa := newForgeAccess(forgepkg.NewGitHubAt(srv.URL), guardedClient(srv),
		[]RepoSpec{{Name: "shared", URL: "https://github.com/acme/shared.git"}}, "srv-token", dir)
	ctx := context.Background()
	if _, err := fa.readers(ctx, "shared", []string{"kvaps"}); err != nil {
		t.Fatal(err)
	}

	dir.mu.Lock()
	if _, err := fa.readers(ctx, "shared", []string{"kvaps", "lex"}); err != nil {
		dir.mu.Unlock()
		t.Fatal(err)
	}
	waitForCalls(t, &calls, 2)
	// Give the answer time to reach the held directory; a fixed store is
	// green however long this is.
	time.Sleep(200 * time.Millisecond)
	got, err := fa.readers(ctx, "shared", []string{"kvaps", "lex"})
	dir.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("a board read while the answer was recorded dropped lex: %v", got)
	}
}
