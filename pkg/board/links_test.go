package board

import (
	"reflect"
	"testing"
)

func TestExtractLinksClassifiesAndOrders(t *testing.T) {
	desc := "See https://example.com/docs first.\n" +
		"Fix: https://github.com/acme/webapp/issues/12, then\n" +
		"review https://github.com/acme/webapp/pull/34#discussion_r1 and\n" +
		"read http://blog.example.com/post."
	got := ExtractLinks(desc)
	want := []Link{
		{URL: "https://github.com/acme/webapp/issues/12", Kind: "issue",
			Owner: "acme", Repo: "webapp", Number: 12},
		{URL: "https://github.com/acme/webapp/pull/34#discussion_r1", Kind: "pull",
			Owner: "acme", Repo: "webapp", Number: 34},
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

func TestFallbackTitle(t *testing.T) {
	cases := map[string]string{
		"https://github.com/acme/webapp/issues/1060": "Issue: acme/webapp#1060",
		"https://github.com/acme/repo/pull/34":       "Pull: acme/repo#34",
	}
	for url, want := range cases {
		ref, ok := ParseGitHubRef(url)
		if !ok {
			t.Fatalf("%s did not parse as a ref", url)
		}
		if got := ref.FallbackTitle(); got != want {
			t.Errorf("FallbackTitle(%s) = %q, want %q", url, got, want)
		}
	}
}

// The GitHub owner/repo#number shorthand is an addressable reference: it
// resolves to the item's canonical URL, dedupes against a full URL of the
// same item, and never fires glued to a path or inside a URL.
func TestExtractLinksShorthandRefs(t *testing.T) {
	desc := "Blocked on cncf/foundation#1465 and cncf/foundation#1466,\n" +
		"see also https://github.com/acme/webapp/pull/7 and acme/webapp#7.\n" +
		"Not refs: src/a/b#1, https://example.com/x/y#12, v1/v2#12abc."
	got := ExtractLinks(desc)
	want := []Link{
		{URL: "https://github.com/cncf/foundation/issues/1465", Kind: "issue",
			Owner: "cncf", Repo: "foundation", Number: 1465},
		{URL: "https://github.com/cncf/foundation/issues/1466", Kind: "issue",
			Owner: "cncf", Repo: "foundation", Number: 1466},
		{URL: "https://github.com/acme/webapp/pull/7", Kind: "pull",
			Owner: "acme", Repo: "webapp", Number: 7},
		{URL: "https://example.com/x/y#12", Kind: "link"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("links = %+v\nwant %+v", got, want)
	}
}

// ParseGitHubRef accepts the shorthand too — a card created with the title
// "cncf/foundation#1465" resolves like one created with the full URL.
func TestParseGitHubRefShorthand(t *testing.T) {
	link, ok := ParseGitHubRef("cncf/foundation#1465")
	if !ok || link.Owner != "cncf" || link.Repo != "foundation" || link.Number != 1465 {
		t.Fatalf("shorthand not parsed: ok=%v link=%+v", ok, link)
	}
	if link.URL != "https://github.com/cncf/foundation/issues/1465" {
		t.Fatalf("URL = %q", link.URL)
	}
	if _, ok := ParseGitHubRef("cncf/foundation#1465 trailing"); ok {
		t.Fatal("a shorthand with trailing text is not a bare ref")
	}
	if _, ok := ParseGitHubRef("just text"); ok {
		t.Fatal("plain text must not parse")
	}
}
