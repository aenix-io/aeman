package apiserver

import (
	"net/url"
	"testing"
)

// THE STRATEGY BOARD WAS RENAMED FROM ROADMAP on 2026-11-08, and the old
// name is no board any more: a query or a path segment naming it is refused
// as any unknown view is.
func TestTheOldRoadmapNameIsNoView(t *testing.T) {
	t.Parallel()
	if _, err := ParseSelector(url.Values{"view": {"roadmap"}}); err == nil {
		t.Fatal("view=roadmap was accepted")
	}
	if _, err := ParseViewSelector("roadmap", url.Values{}); err == nil {
		t.Fatal("the roadmap segment was accepted")
	}
	if sel, err := ParseViewSelector("strategy", url.Values{}); err != nil || sel.View != "strategy" {
		t.Fatalf("the strategy segment parsed as %+v, %v", sel, err)
	}
}
