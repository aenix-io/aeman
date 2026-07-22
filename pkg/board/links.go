package board

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Link is one URL found in a card's description. GitHub issue and pull-request
// links are recognised and addressable (Owner/Repo/Number); everything else is
// a plain link. Title and State are filled by a LinkResolver, not here.
type Link struct {
	URL string `json:"url"`
	// Kind is "issue", "pull" or "link".
	Kind   string `json:"kind"`
	Owner  string `json:"owner,omitempty"`
	Repo   string `json:"repo,omitempty"`
	Number int    `json:"number,omitempty"`
	// Title is the issue/PR title, when resolved.
	Title string `json:"title,omitempty"`
	// State is the issue/PR state (open/closed/merged), when resolved.
	State string `json:"state,omitempty"`
}

// IsGitHubRef reports whether the link addresses a GitHub issue or PR.
func (l Link) IsGitHubRef() bool { return l.Kind == "issue" || l.Kind == "pull" }

// FallbackTitle is a readable card title for a GitHub ref whose real title
// could not be fetched (e.g. a token without access to a private repo):
// "Issue: owner/repo#123" or "Pull: owner/repo#123".
func (l Link) FallbackTitle() string {
	kind := "Issue"
	if l.Kind == "pull" {
		kind = "Pull"
	}
	return fmt.Sprintf("%s: %s/%s#%d", kind, l.Owner, l.Repo, l.Number)
}

var urlPattern = regexp.MustCompile(`https?://[^\s<>"'\)\]]+`)

// shorthandPattern is the GitHub cross-reference shorthand: owner/repo#123.
// Boundaries are validated separately (shorthandBounded) — RE2 has no
// lookbehind, and "a/b/c#1" or a URL tail must not produce a phantom ref.
var shorthandPattern = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.\-]+#[0-9]+`)

// ExtractLinks finds every link in a free-form description — full URLs and
// GitHub owner/repo#number shorthands — classifies GitHub issue/PR references,
// dedupes (a shorthand and a full URL of the same item are one link), and
// orders the result the way the UI lists it: GitHub references first (in
// order of appearance), plain links after.
func ExtractLinks(description string) []Link {
	type match struct {
		pos  int
		link Link
	}
	var matches []match
	urlSpans := urlPattern.FindAllStringIndex(description, -1)
	for _, sp := range urlSpans {
		raw := strings.TrimRight(description[sp[0]:sp[1]], ".,;:!?")
		matches = append(matches, match{sp[0], classifyLink(raw)})
	}
	inURL := func(i int) bool {
		for _, sp := range urlSpans {
			if i >= sp[0] && i < sp[1] {
				return true
			}
		}
		return false
	}
	for _, sp := range shorthandPattern.FindAllStringIndex(description, -1) {
		if inURL(sp[0]) || !shorthandBounded(description, sp[0], sp[1]) {
			continue
		}
		if link, ok := classifyShorthand(description[sp[0]:sp[1]]); ok {
			matches = append(matches, match{sp[0], link})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].pos < matches[j].pos })
	var refs, plain []Link
	seen := map[string]bool{}
	for _, m := range matches {
		key := m.link.URL
		if m.link.IsGitHubRef() {
			// A shorthand and a full URL of the same item must collapse into
			// one entry, whatever their URL spelling (/issues/ vs /pull/).
			key = fmt.Sprintf("%s/%s#%d", m.link.Owner, m.link.Repo, m.link.Number)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		if m.link.IsGitHubRef() {
			refs = append(refs, m.link)
		} else {
			plain = append(plain, m.link)
		}
	}
	return append(refs, plain...)
}

// shorthandBounded checks a shorthand match stands on its own: not glued to a
// preceding path/word (a/b/c#1, user@repo/x#2) and not running into trailing
// word characters (#1465abc).
func shorthandBounded(s string, start, end int) bool {
	if start > 0 {
		switch prev := s[start-1]; {
		case prev == '/' || prev == '.' || prev == '-' || prev == '_' || prev == '#' || prev == '@':
			return false
		case prev >= 'a' && prev <= 'z', prev >= 'A' && prev <= 'Z', prev >= '0' && prev <= '9':
			return false
		}
	}
	if end < len(s) {
		switch next := s[end]; {
		case next == '_':
			return false
		case next >= 'a' && next <= 'z', next >= 'A' && next <= 'Z', next >= '0' && next <= '9':
			return false
		}
	}
	return true
}

// classifyShorthand parses owner/repo#number into an addressable GitHub ref.
// The kind starts as "issue" — the live resolver corrects it to "pull" when
// the number turns out to be a pull request (GitHub redirects either URL).
func classifyShorthand(raw string) (Link, bool) {
	slash := strings.IndexByte(raw, '/')
	hash := strings.IndexByte(raw, '#')
	if slash <= 0 || hash <= slash+1 {
		return Link{}, false
	}
	n, err := strconv.Atoi(raw[hash+1:])
	if err != nil || n <= 0 {
		return Link{}, false
	}
	owner, repo := raw[:slash], raw[slash+1:hash]
	return Link{
		URL:    fmt.Sprintf("https://github.com/%s/%s/issues/%d", owner, repo, n),
		Kind:   "issue",
		Owner:  owner,
		Repo:   repo,
		Number: n,
	}, true
}

// ParseGitHubRef parses a GitHub issue/PR reference — a full URL or the
// owner/repo#number shorthand. ok is false for anything else.
func ParseGitHubRef(raw string) (link Link, ok bool) {
	link = classifyLink(raw)
	if link.IsGitHubRef() {
		return link, true
	}
	if sp := shorthandPattern.FindStringIndex(raw); sp != nil && sp[0] == 0 && sp[1] == len(raw) {
		return classifyShorthand(raw)
	}
	return link, false
}

// classifyLink recognises github.com/{owner}/{repo}/issues|pull/{n} (an
// optional trailing path or fragment, e.g. a comment anchor, is fine).
func classifyLink(raw string) Link {
	link := Link{URL: raw, Kind: "link"}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return link
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if host != "github.com" {
		return link
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 4 {
		return link
	}
	n, err := strconv.Atoi(parts[3])
	if err != nil || n <= 0 {
		return link
	}
	switch parts[2] {
	case "issues":
		link.Kind = "issue"
	case "pull":
		link.Kind = "pull"
	default:
		return link
	}
	link.Owner, link.Repo, link.Number = parts[0], parts[1], n
	return link
}
