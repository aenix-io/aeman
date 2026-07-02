package board

import (
	"reflect"
	"testing"
)

func TestExtractLinksClassifiesAndOrders(t *testing.T) {
	desc := "See https://example.com/docs first.\n" +
		"Fix: https://github.com/aenix-org/aeman/issues/12, then\n" +
		"review https://github.com/aenix-org/aeman/pull/34#discussion_r1 and\n" +
		"read http://blog.example.com/post."
	got := ExtractLinks(desc)
	want := []Link{
		{URL: "https://github.com/aenix-org/aeman/issues/12", Kind: "issue",
			Owner: "aenix-org", Repo: "aeman", Number: 12},
		{URL: "https://github.com/aenix-org/aeman/pull/34#discussion_r1", Kind: "pull",
			Owner: "aenix-org", Repo: "aeman", Number: 34},
		{URL: "https://example.com/docs", Kind: "link"},
		{URL: "http://blog.example.com/post", Kind: "link"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("links = %+v\nwant %+v", got, want)
	}
}

func TestExtractLinksDedupes(t *testing.T) {
	desc := "https://example.com/a https://example.com/a https://example.com/a."
	if got := ExtractLinks(desc); len(got) != 1 {
		t.Fatalf("links = %+v", got)
	}
}

func TestExtractLinksEmpty(t *testing.T) {
	if got := ExtractLinks("no links here, just text"); len(got) != 0 {
		t.Fatalf("links = %+v", got)
	}
}

func TestParseGitHubRef(t *testing.T) {
	link, ok := ParseGitHubRef("https://github.com/acme/repo/pull/7")
	if !ok || link.Kind != "pull" || link.Owner != "acme" || link.Repo != "repo" || link.Number != 7 {
		t.Fatalf("link = %+v ok = %v", link, ok)
	}
	for _, raw := range []string{
		"https://github.com/acme/repo",
		"https://github.com/acme/repo/commit/abc123",
		"https://gitlab.com/acme/repo/issues/7",
		"not a url",
	} {
		if _, ok := ParseGitHubRef(raw); ok {
			t.Fatalf("%q must not parse as a GitHub ref", raw)
		}
	}
}
