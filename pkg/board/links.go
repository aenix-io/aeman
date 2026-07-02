package board

import (
	"net/url"
	"regexp"
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

var urlPattern = regexp.MustCompile(`https?://[^\s<>"'\)\]]+`)

// ExtractLinks finds every URL in a free-form description, classifies GitHub
// issue/PR links, dedupes, and orders the result the way the UI lists it:
// GitHub references first (in order of appearance), plain links after.
func ExtractLinks(description string) []Link {
	var refs, plain []Link
	seen := map[string]bool{}
	for _, raw := range urlPattern.FindAllString(description, -1) {
		u := strings.TrimRight(raw, ".,;:!?")
		if seen[u] {
			continue
		}
		seen[u] = true
		link := classifyLink(u)
		if link.IsGitHubRef() {
			refs = append(refs, link)
		} else {
			plain = append(plain, link)
		}
	}
	return append(refs, plain...)
}

// ParseGitHubRef parses a GitHub issue/PR URL. ok is false for anything else.
func ParseGitHubRef(raw string) (link Link, ok bool) {
	link = classifyLink(raw)
	return link, link.IsGitHubRef()
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
