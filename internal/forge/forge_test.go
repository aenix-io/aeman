package forge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// The forge is named by the primary repository's host unless the operator
// says otherwise: github.com is GitHub, a host that says "gitlab" is that
// GitLab instance, anything else defaults to GitHub (the code base's first
// forge) unless a GitLab base URL is given. An explicit kind always wins.
func TestDetectNamesTheForgeByHostUnlessToldOtherwise(t *testing.T) {
	cases := []struct {
		url, explicit, gitlabBase string
		want                      Kind
		base                      string
	}{
		{"https://github.com/acme/aeman-db.git", "", "", GitHub, ""},
		{"git@github.com:acme/aeman-db.git", "", "", GitHub, ""},
		{"https://gitlab.com/kvaps/aeman-db.git", "", "", GitLab, "https://gitlab.com"},
		{"https://gitlab.example.org/team/sub/board.git", "", "", GitLab, "https://gitlab.example.org"},
		{"https://git.example.org/team/board.git", "", "", GitHub, ""},
		{"https://git.example.org/team/board.git", "", "https://git.example.org", GitLab, "https://git.example.org"},
		{"https://git.example.org/team/board.git", "gitlab", "", GitLab, "https://git.example.org"},
		{"https://gitlab.com/kvaps/aeman-db.git", "github", "", GitHub, ""},
	}
	for _, tc := range cases {
		f, err := Detect(tc.url, Kind(tc.explicit), tc.gitlabBase)
		if err != nil {
			t.Fatalf("Detect(%q, %q, %q): %v", tc.url, tc.explicit, tc.gitlabBase, err)
		}
		if f.Kind() != tc.want {
			t.Errorf("Detect(%q, %q, %q) = %s, want %s", tc.url, tc.explicit, tc.gitlabBase, f.Kind(), tc.want)
		}
		if gl, ok := f.(*gitlab); ok && gl.base != tc.base {
			t.Errorf("Detect(%q): gitlab base = %q, want %q", tc.url, gl.base, tc.base)
		}
	}
	if _, err := Detect("https://gitlab.com/x/y.git", "gitea", ""); err == nil {
		t.Fatal("an unknown forge kind must be refused, not guessed")
	}
}

// A repository URL names the repository the way the forge's API wants it:
// GitHub takes owner/repo — the last two segments; GitLab takes the whole
// path (subgroups included), URL-encoded as one segment.
func TestRepoRefIsTheAPIsNameForTheRepository(t *testing.T) {
	gh := NewGitHub()
	for in, want := range map[string]string{
		"https://github.com/acme/aeman-db.git": "acme/aeman-db",
		"https://github.com/acme/aeman-db/":    "acme/aeman-db",
		"git@github.com:acme/aeman-db.git":     "acme/aeman-db",
		"acme/aeman-db":                        "acme/aeman-db",
	} {
		if got, err := gh.RepoRef(in); err != nil || got != want {
			t.Errorf("github RepoRef(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := gh.RepoRef("https://github.com/acme"); err == nil {
		t.Error("github: a URL without owner/repo must be refused")
	}
	gl := NewGitLab("https://gitlab.com")
	for in, want := range map[string]string{
		"https://gitlab.com/kvaps/aeman-db.git":          "kvaps%2Faeman-db",
		"https://gitlab.com/group/subgroup/board/":       "group%2Fsubgroup%2Fboard",
		"git@gitlab.com:group/subgroup/deeper/board.git": "group%2Fsubgroup%2Fdeeper%2Fboard",
		"group/board": "group%2Fboard",
		"https://gitlab.example.org/team/board.git":       "team%2Fboard",
		"https://gitlab.com/kvaps/aeman-personal-db.git/": "kvaps%2Faeman-personal-db",
	} {
		if got, err := gl.RepoRef(in); err != nil || got != want {
			t.Errorf("gitlab RepoRef(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := gl.RepoRef("https://gitlab.com/onlygroup"); err == nil {
		t.Error("gitlab: a URL without a project path must be refused")
	}
}

// Git over HTTPS takes the token as a basic-auth password under the
// username the forge expects: GitHub's x-access-token, GitLab's oauth2 (the
// only username GitLab accepts for an OAuth token).
func TestGitAuthUsesTheForgesUsername(t *testing.T) {
	if a := NewGitHub().GitAuth("tok"); a.Username != "x-access-token" || a.Password != "tok" {
		t.Fatalf("github GitAuth = %+v", a)
	}
	if a := NewGitLab("https://gitlab.com").GitAuth("tok"); a.Username != "oauth2" || a.Password != "tok" {
		t.Fatalf("gitlab GitAuth = %+v", a)
	}
}

// The OAuth token forms: GitHub's exchange as it always was; GitLab's names
// the grant type and repeats redirect_uri on refresh, which GitLab requires.
func TestTokenFormsFollowEachForgesDialect(t *testing.T) {
	gh := NewGitHub()
	ex := gh.ExchangeForm("id", "secret", "code", "https://a/auth/callback")
	if ex.Get("client_id") != "id" || ex.Get("client_secret") != "secret" || ex.Get("code") != "code" || ex.Get("redirect_uri") != "https://a/auth/callback" {
		t.Fatalf("github exchange form = %v", ex)
	}
	rf := gh.RefreshForm("id", "secret", "r1", "https://a/auth/callback")
	if rf.Get("grant_type") != "refresh_token" || rf.Get("refresh_token") != "r1" || rf.Has("redirect_uri") {
		t.Fatalf("github refresh form = %v", rf)
	}
	gl := NewGitLab("https://gitlab.com")
	ex = gl.ExchangeForm("id", "secret", "code", "https://a/auth/callback")
	if ex.Get("grant_type") != "authorization_code" || ex.Get("redirect_uri") != "https://a/auth/callback" || ex.Get("code") != "code" {
		t.Fatalf("gitlab exchange form = %v", ex)
	}
	rf = gl.RefreshForm("id", "secret", "r1", "https://a/auth/callback")
	if rf.Get("grant_type") != "refresh_token" || rf.Get("refresh_token") != "r1" || rf.Get("redirect_uri") != "https://a/auth/callback" {
		t.Fatalf("gitlab refresh form = %v", rf)
	}
	if gl.AuthorizeURL() != "https://gitlab.com/oauth/authorize" || gl.TokenURL() != "https://gitlab.com/oauth/token" {
		t.Fatalf("gitlab oauth endpoints = %s %s", gl.AuthorizeURL(), gl.TokenURL())
	}
	if !strings.Contains(gl.DefaultScopes(), "read_user") || !strings.Contains(gl.DefaultScopes(), "write_repository") {
		t.Fatalf("gitlab scopes = %q", gl.DefaultScopes())
	}
}

// fakeGitLab is enough of a GitLab REST API for the tests: one project with
// per-visitor access, its members, and a user directory.
func fakeGitLab(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/user", func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer alice-token":
			_ = json.NewEncoder(w).Encode(map[string]any{"username": "alice", "name": "Alice Liddell", "avatar_url": "https://gitlab.example/uploads/alice.png"})
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	})
	mux.HandleFunc("/api/v4/projects/kvaps%2Faeman-db", func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer alice-token": // owner
			_ = json.NewEncoder(w).Encode(map[string]any{"visibility": "private", "permissions": map[string]any{
				"project_access": map[string]any{"access_level": 50}, "group_access": nil}})
		case "Bearer bob-token": // reporter through the group
			_ = json.NewEncoder(w).Encode(map[string]any{"visibility": "private", "permissions": map[string]any{
				"project_access": nil, "group_access": map[string]any{"access_level": 20}}})
		case "Bearer guest-token":
			_ = json.NewEncoder(w).Encode(map[string]any{"visibility": "private", "permissions": map[string]any{
				"project_access": map[string]any{"access_level": 10}, "group_access": nil}})
		case "Bearer stranger-token":
			w.WriteHeader(http.StatusNotFound)
		case "Bearer expired-token":
			w.WriteHeader(http.StatusUnauthorized)
		default:
			w.WriteHeader(http.StatusForbidden)
		}
	})
	mux.HandleFunc("/api/v4/projects/kvaps%2Fpublic-notes", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"visibility": "public", "permissions": map[string]any{
			"project_access": nil, "group_access": nil}})
	})
	mux.HandleFunc("/api/v4/projects/kvaps%2Faeman-db/members/all", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer server-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Query().Get("page") {
		case "", "1":
			w.Header().Set("X-Next-Page", "2")
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"username": "alice", "name": "Alice Liddell", "avatar_url": "https://gitlab.example/uploads/alice.png", "access_level": 50},
				{"username": "guest", "name": "Just Looking", "avatar_url": "", "access_level": 10},
			})
		case "2":
			w.Header().Set("X-Next-Page", "")
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"username": "bob", "name": "Bob", "avatar_url": "https://gitlab.example/uploads/bob.png", "access_level": 20},
			})
		}
	})
	mux.HandleFunc("/api/v4/users", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("username") {
		case "carol":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"username": "carol", "name": "Carol", "avatar_url": "https://gitlab.example/uploads/carol.png"}})
		default:
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestGitLabUserIsTheTokensOwnerWithNameAndAvatar(t *testing.T) {
	srv := fakeGitLab(t)
	gl := NewGitLab(srv.URL)
	u, err := gl.User(context.Background(), srv.Client(), "alice-token")
	if err != nil || u.Login != "alice" || u.Name != "Alice Liddell" || u.AvatarURL != "https://gitlab.example/uploads/alice.png" {
		t.Fatalf("User = %+v, %v", u, err)
	}
	if _, err := gl.User(context.Background(), srv.Client(), "expired-token"); !errors.Is(err, ErrBadToken) {
		t.Fatalf("a rejected token = %v, want ErrBadToken", err)
	}
}

// Access on GitLab is the numeric access level: Reporter (20) reads the
// repository, Developer (30) pushes, a Guest (10) sees the project but not
// its code, a public project reads for anyone. A project the visitor cannot
// see is invisible (404/403), a rejected token is ErrBadToken.
func TestGitLabAccessReadsTheAccessLevel(t *testing.T) {
	srv := fakeGitLab(t)
	gl := NewGitLab(srv.URL)
	ctx := context.Background()
	repo := "https://gitlab.com/kvaps/aeman-db.git"
	for name, tc := range map[string]struct {
		token       string
		read, write bool
	}{
		"owner":    {"alice-token", true, true},
		"reporter": {"bob-token", true, false},
		"guest":    {"guest-token", false, false},
		"stranger": {"stranger-token", false, false},
		"no token": {"", false, false},
	} {
		read, write, err := gl.Access(ctx, srv.Client(), tc.token, repo)
		if err != nil || read != tc.read || write != tc.write {
			t.Errorf("%s: Access = %v %v %v; want %v %v", name, read, write, err, tc.read, tc.write)
		}
	}
	if _, _, err := gl.Access(ctx, srv.Client(), "expired-token", repo); !errors.Is(err, ErrBadToken) {
		t.Fatalf("expired token: %v, want ErrBadToken", err)
	}
	if read, write, err := gl.Access(ctx, srv.Client(), "alice-token", "https://gitlab.com/kvaps/public-notes.git"); err != nil || !read || write {
		t.Fatalf("public project: %v %v %v; want read only", read, write, err)
	}
}

// Readers on GitLab is one call for the whole member list (inherited members
// included, every page), Reporter and up; it hands back what the list says
// about each — name and avatar — so the server learns people without a
// lookup each.
func TestGitLabReadersComeFromTheMemberListWithNamesAndAvatars(t *testing.T) {
	srv := fakeGitLab(t)
	gl := NewGitLab(srv.URL)
	got, err := gl.Readers(context.Background(), srv.Client(), "server-token", "https://gitlab.com/kvaps/aeman-db.git",
		[]string{"alice", "bob", "guest", "nobody"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["alice"].Name != "Alice Liddell" || got["bob"].AvatarURL != "https://gitlab.example/uploads/bob.png" {
		t.Fatalf("readers = %+v; want alice and bob with their names and avatars", got)
	}
	if _, err := gl.Readers(context.Background(), srv.Client(), "", "https://gitlab.com/kvaps/aeman-db.git", []string{"alice"}); err == nil {
		t.Fatal("without a server token there is nobody to ask the forge as")
	}
}

// Lookup finds a person by login: GitLab asks the user directory (an avatar
// is an upload, not a URL one can build); GitHub builds the avatar URL from
// the login without a call. An unknown login is ErrNotFound.
func TestLookupFindsAPersonByLogin(t *testing.T) {
	srv := fakeGitLab(t)
	gl := NewGitLab(srv.URL)
	p, err := gl.Lookup(context.Background(), srv.Client(), "", "carol")
	if err != nil || p.Login != "carol" || p.Name != "Carol" || p.AvatarURL != "https://gitlab.example/uploads/carol.png" {
		t.Fatalf("gitlab Lookup(carol) = %+v, %v", p, err)
	}
	if _, err := gl.Lookup(context.Background(), srv.Client(), "", "nobody"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("gitlab Lookup(nobody) = %v, want ErrNotFound", err)
	}
	p, err = NewGitHub().Lookup(context.Background(), nil, "", "octocat")
	if err != nil || p.Login != "octocat" || p.AvatarURL != "https://avatars.githubusercontent.com/octocat?size=48" || p.Name != "" {
		t.Fatalf("github Lookup(octocat) = %+v, %v", p, err)
	}
}

// A forge that is rate-limiting says 403 — the same code it uses for a
// repository the visitor may not see. Told apart by what the answer carries
// (GitHub: a spent rate-limit budget or a Retry-After; GitLab: the same, or
// 429), being throttled is an ErrRateLimited, never "you have no access":
// the difference decides whether the last answer stands or the visitor is
// locked out of a board they own.
func TestBeingThrottledIsNotTheSameAsHavingNoAccess(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		headers map[string]string
		limited bool
	}{
		{"primary rate limit", http.StatusForbidden, map[string]string{"X-RateLimit-Remaining": "0"}, true},
		{"secondary rate limit", http.StatusForbidden, map[string]string{"Retry-After": "60"}, true},
		{"too many requests", http.StatusTooManyRequests, nil, true},
		{"genuinely forbidden", http.StatusForbidden, map[string]string{"X-RateLimit-Remaining": "4823"}, false},
		{"forbidden, nothing said", http.StatusForbidden, nil, false},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			for k, v := range tc.headers {
				w.Header().Set(k, v)
			}
			w.WriteHeader(tc.status)
		}))
		for _, f := range []Forge{NewGitHubAt(srv.URL), NewGitLab(srv.URL)} {
			repo := "https://host/acme/repo.git"
			read, write, err := f.Access(context.Background(), srv.Client(), "tok", repo)
			switch {
			case tc.limited && !errors.Is(err, ErrRateLimited):
				t.Errorf("%s/%s: Access err = %v, want ErrRateLimited", f.Kind(), tc.name, err)
			case !tc.limited && err != nil:
				t.Errorf("%s/%s: Access err = %v, want none", f.Kind(), tc.name, err)
			case !tc.limited && (read || write):
				t.Errorf("%s/%s: Access = %v %v, want no access", f.Kind(), tc.name, read, write)
			}
			// Readers is asked with the server's credential and must not
			// swallow the throttling either: an empty roster would quietly
			// empty every picker on the board.
			if _, err := f.Readers(context.Background(), srv.Client(), "srv", repo, []string{"alice"}); tc.limited && !errors.Is(err, ErrRateLimited) {
				t.Errorf("%s/%s: Readers err = %v, want ErrRateLimited", f.Kind(), tc.name, err)
			}
		}
		srv.Close()
	}
}

// A token minted before the server asked for the scope it now needs cannot
// see a private repository — GitHub answers 404, the same as for a
// repository that is not yours. Read as "no access" it strands the visitor:
// every write refused, and nothing they can do about it but sign in again,
// which is exactly what was reported. GitHub names the token's scopes in the
// answer, so the two cases are told apart: a token without `repo` is a stale
// authorization (ErrBadToken — the server drops the session and the sign-in
// gate comes back), a token with it simply cannot see that repository.
func TestATokenWithoutTheRepoScopeIsAStaleAuthorization(t *testing.T) {
	cases := []struct {
		name, scopes string
		status       int
		hasHeader    bool
		wantBad      bool
	}{
		{"minted before the scope was asked for", "project", http.StatusNotFound, true, true},
		{"no scopes at all", "", http.StatusNotFound, true, true},
		{"forbidden with the old scopes", "project, read:org", http.StatusForbidden, true, true},
		{"has the scope, cannot see this repository", "repo, project", http.StatusNotFound, true, false},
		{"has the scope, forbidden", "repo", http.StatusForbidden, true, false},
		{"a forge that names no scopes", "", http.StatusNotFound, false, false},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if tc.hasHeader {
				w.Header().Set("X-OAuth-Scopes", tc.scopes)
			}
			w.WriteHeader(tc.status)
		}))
		read, write, err := NewGitHubAt(srv.URL).Access(context.Background(), srv.Client(), "tok", "https://github.com/acme/repo.git")
		switch {
		case tc.wantBad && !errors.Is(err, ErrBadToken):
			t.Errorf("%s: err = %v, want ErrBadToken", tc.name, err)
		case !tc.wantBad && err != nil:
			t.Errorf("%s: err = %v, want none", tc.name, err)
		case !tc.wantBad && (read || write):
			t.Errorf("%s: %v %v, want no access", tc.name, read, write)
		}
		srv.Close()
	}
}

// fakeGitHub is the GitHub REST surface the GitHub forge uses.
func fakeGitHub(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer alice-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"login": "alice", "name": "Alice Liddell", "avatar_url": "https://avatars.githubusercontent.com/u/1?v=4"})
	})
	mux.HandleFunc("/repos/acme/aeman-db", func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer alice-token":
			_ = json.NewEncoder(w).Encode(map[string]any{"permissions": map[string]bool{"admin": true, "push": true, "pull": true}})
		case "Bearer bob-token":
			_ = json.NewEncoder(w).Encode(map[string]any{"permissions": map[string]bool{"pull": true}})
		case "Bearer expired-token":
			w.WriteHeader(http.StatusUnauthorized)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	mux.HandleFunc("/repos/acme/aeman-db/collaborators/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer server-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		login := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/repos/acme/aeman-db/collaborators/"), "/permission")
		switch login {
		case "alice":
			_ = json.NewEncoder(w).Encode(map[string]any{"permission": "admin"})
		case "bob":
			_ = json.NewEncoder(w).Encode(map[string]any{"permission": "read"})
		case "nobody":
			_ = json.NewEncoder(w).Encode(map[string]any{"permission": "none"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// The GitHub forge keeps the behaviour the server had: the permissions block
// decides, 404/403 is invisible, 401 is a bad token; readers are the
// collaborators with any permission but "none".
func TestGitHubKeepsThePermissionsBlockRules(t *testing.T) {
	srv := fakeGitHub(t)
	gh := NewGitHubAt(srv.URL)
	ctx := context.Background()
	u, err := gh.User(ctx, srv.Client(), "alice-token")
	if err != nil || u.Login != "alice" || u.Name != "Alice Liddell" || !strings.HasPrefix(u.AvatarURL, "https://avatars.githubusercontent.com/u/1") {
		t.Fatalf("User = %+v, %v", u, err)
	}
	repo := "https://github.com/acme/aeman-db.git"
	if read, write, err := gh.Access(ctx, srv.Client(), "alice-token", repo); err != nil || !read || !write {
		t.Fatalf("admin: %v %v %v", read, write, err)
	}
	if read, write, err := gh.Access(ctx, srv.Client(), "bob-token", repo); err != nil || !read || write {
		t.Fatalf("pull only: %v %v %v", read, write, err)
	}
	if read, write, err := gh.Access(ctx, srv.Client(), "stranger-token", repo); err != nil || read || write {
		t.Fatalf("stranger: %v %v %v", read, write, err)
	}
	if _, _, err := gh.Access(ctx, srv.Client(), "expired-token", repo); !errors.Is(err, ErrBadToken) {
		t.Fatalf("expired: %v", err)
	}
	got, err := gh.Readers(ctx, srv.Client(), "server-token", repo, []string{"alice", "bob", "nobody", "stranger"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["alice"].Login != "alice" || got["bob"].Login != "bob" {
		t.Fatalf("readers = %+v; want alice and bob", got)
	}
}

// collabListing is a GitHub collaborator listing over two pages, beside the
// per-login permission endpoint the old code asked once per person. Both
// count their calls, so a test can say which way the answer came.
func fakeGitHubListing(t *testing.T, listing, perLogin *atomic.Int32, refuseListing bool) *httptest.Server {
	t.Helper()
	pages := map[string][]map[string]any{
		"1": {
			{"login": "alice", "permissions": map[string]bool{"admin": true, "push": true, "pull": true}},
			{"login": "bob", "permissions": map[string]bool{"pull": true}},
		},
		"2": {
			{"login": "nobody", "permissions": map[string]bool{}},
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/aeman-db/collaborators", func(w http.ResponseWriter, r *http.Request) {
		listing.Add(1)
		if refuseListing {
			// A credential that may read the repository but not list who
			// else can — the answer GitHub gives a token without it.
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.Header.Get("Authorization") != "Bearer server-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}
		if page == "1" {
			w.Header().Set("Link", `<`+"http://"+r.Host+`/repos/acme/aeman-db/collaborators?page=2>; rel="next"`)
		}
		_ = json.NewEncoder(w).Encode(pages[page])
	})
	mux.HandleFunc("/repos/acme/aeman-db/collaborators/", func(w http.ResponseWriter, r *http.Request) {
		perLogin.Add(1)
		login := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/repos/acme/aeman-db/collaborators/"), "/permission")
		switch login {
		case "alice":
			_ = json.NewEncoder(w).Encode(map[string]any{"permission": "admin"})
		case "bob":
			_ = json.NewEncoder(w).Encode(map[string]any{"permission": "read"})
		case "nobody":
			_ = json.NewEncoder(w).Encode(map[string]any{"permission": "none"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// Who else reads a domain used to cost one request per person, in a row:
// ten people on a board with two domains meant twenty round trips before the
// board could be answered. GitHub lists the collaborators with their
// permissions in one request, paginated — so that is what is asked.
func TestGitHubReadersAreOneListingNotAQuestionPerPerson(t *testing.T) {
	var listing, perLogin atomic.Int32
	srv := fakeGitHubListing(t, &listing, &perLogin, false)
	gh := NewGitHubAt(srv.URL)
	got, err := gh.Readers(context.Background(), srv.Client(), "server-token",
		"https://github.com/acme/aeman-db.git", []string{"alice", "bob", "nobody", "stranger"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["alice"].Login != "alice" || got["bob"].Login != "bob" {
		t.Fatalf("readers = %+v; want alice and bob", got)
	}
	if got["alice"].AvatarURL == "" {
		t.Fatal("a reader must carry an avatar for the picker")
	}
	if n := perLogin.Load(); n != 0 {
		t.Fatalf("%d per-person questions; the listing answers them all", n)
	}
	if n := listing.Load(); n != 2 {
		t.Fatalf("%d listing requests, want 2 (the second page follows the Link header)", n)
	}
}

// Not every credential may list a repository's collaborators. A refusal is
// not an outage: the old question, one per person, still answers.
func TestGitHubReadersFallBackToOneQuestionPerPersonWhenTheListingIsRefused(t *testing.T) {
	var listing, perLogin atomic.Int32
	srv := fakeGitHubListing(t, &listing, &perLogin, true)
	gh := NewGitHubAt(srv.URL)
	got, err := gh.Readers(context.Background(), srv.Client(), "server-token",
		"https://github.com/acme/aeman-db.git", []string{"alice", "bob", "nobody", "stranger"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["alice"].Login != "alice" || got["bob"].Login != "bob" {
		t.Fatalf("readers = %+v; want alice and bob", got)
	}
	if n := perLogin.Load(); n != 4 {
		t.Fatalf("%d per-person questions, want one per asked login", n)
	}
}
