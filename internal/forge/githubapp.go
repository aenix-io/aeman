package forge

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

// GitHubApp is the server credential minted instead of kept: the server
// signs a short-lived JWT with the app's private key, asks which
// installation a repository belongs to, and trades the JWT for an
// installation token — scoped to the repositories the app is installed on,
// expiring within the hour, renewed here before it runs out. One key covers
// every organisation the app is installed in; nothing is issued by hand and
// nothing quietly expires in a .env file.
type GitHubApp struct {
	id     string
	key    *rsa.PrivateKey
	api    string
	client *http.Client
	now    func() time.Time

	mu       sync.Mutex
	installs map[string]int64    // repository slug -> installation
	tokens   map[int64]appMinted // installation -> its current token
	// homeURL is the app's own page on the forge (asked once, for the
	// install link a refusal carries); "" until first needed.
	homeURL string
}

type appMinted struct {
	value   string
	expires time.Time
}

// appTokenSlack is how much life a token must have left to be handed out:
// a git push must never start on a token about to die mid-request.
const appTokenSlack = 5 * time.Minute

// NewGitHubApp is a GitHub App credential for github.com. id is the app id
// (the JWT issuer), pemKey the private key GitHub generated for the app.
// The key is validated here — a broken PEM stops the server at startup, not
// the first push an hour in.
func NewGitHubApp(id string, pemKey []byte) (*GitHubApp, error) {
	return NewGitHubAppAt(githubAPIBase, &http.Client{Timeout: 15 * time.Second}, id, pemKey)
}

// NewGitHubAppAt is NewGitHubApp with the REST base elsewhere — tests.
func NewGitHubAppAt(api string, client *http.Client, id string, pemKey []byte) (*GitHubApp, error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("github app: an app id is required")
	}
	key, err := parseAppKey(pemKey)
	if err != nil {
		return nil, fmt.Errorf("github app %s: %w", id, err)
	}
	return &GitHubApp{id: id, key: key, api: strings.TrimRight(api, "/"), client: client, now: time.Now,
		installs: map[string]int64{}, tokens: map[int64]appMinted{}}, nil
}

// parseAppKey reads the app's private key: GitHub hands it out as PKCS#1
// ("RSA PRIVATE KEY"); PKCS#8 is accepted for a key that was re-wrapped.
func parseAppKey(pemKey []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemKey)
	if block == nil {
		return nil, errors.New("the private key is not PEM")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("the private key parses as neither PKCS#1 nor PKCS#8: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("the private key is not RSA")
	}
	return key, nil
}

// Token is the installation token for the repository, minted or renewed as
// needed. A repository the app is not installed on is a plain error — not a
// mystery 401 three layers later.
func (a *GitHubApp) Token(ctx context.Context, repoURL string) (string, error) {
	parts := repoPath(repoURL)
	if len(parts) < 2 {
		return "", fmt.Errorf("github app: repository url %q has no owner/repo", repoURL)
	}
	slug := parts[len(parts)-2] + "/" + parts[len(parts)-1]
	// One mint at a time: a burst of requests right after the expiry must
	// not each go mint their own. The forge round trips run under the lock;
	// they happen about once an hour per installation.
	a.mu.Lock()
	defer a.mu.Unlock()
	install, known := a.installs[slug]
	if !known {
		id, err := a.installationOf(ctx, slug)
		if err != nil {
			return "", err
		}
		install, a.installs[slug] = id, id
	}
	if tok, ok := a.tokens[install]; ok && a.now().Add(appTokenSlack).Before(tok.expires) {
		return tok.value, nil
	}
	minted, err := a.mint(ctx, install)
	if err != nil {
		return "", err
	}
	a.tokens[install] = minted
	return minted.value, nil
}

// installationOf asks which installation serves the repository.
func (a *GitHubApp) installationOf(ctx context.Context, slug string) (int64, error) {
	resp, err := a.get(ctx, http.MethodGet, "/repos/"+slug+"/installation")
	if err != nil {
		return 0, err
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		_ = resp.Body.Close()
		// The refusal carries the very link that fixes it — nobody should
		// have to construct an install URL from an app id by hand.
		return 0, &AppNotInstalledError{App: a.id, Slug: slug, InstallURL: a.InstallURL(ctx)}
	case resp.StatusCode/100 != 2:
		_ = resp.Body.Close()
		return 0, fmt.Errorf("github app: installation lookup for %s answered %s", slug, resp.Status)
	}
	var body struct {
		ID int64 `json:"id"`
	}
	if err := decodeJSON(resp, &body); err != nil {
		return 0, err
	}
	if body.ID == 0 {
		return 0, fmt.Errorf("github app: installation lookup for %s named no installation", slug)
	}
	return body.ID, nil
}

// AppNotInstalledError is a repository the app has no installation for —
// the one startup trouble a page with a button can fix, so callers tell it
// apart (errors.As) and serve that page instead of refusing to run.
type AppNotInstalledError struct {
	App        string // the app id
	Slug       string // owner/repo
	InstallURL string // where the app is installed
}

func (e *AppNotInstalledError) Error() string {
	return fmt.Sprintf("github app %s is not installed on %s — install it on the repository (or its organisation): %s", e.App, e.Slug, e.InstallURL)
}

// IsAppClientID reports whether a client id belongs to a GitHub App rather
// than an OAuth App. A GitHub App has no scopes — its permissions come from
// the installation, and GitHub ignores a scope parameter — so asking for
// one would leave "scope=repo" in a URL that grants nothing of the sort.
// GitHub App client ids carry an Iv prefix (Iv1.<hex> historically, Iv23li…
// now); anything else is taken for an OAuth App, whose sign-in breaks
// without its scope — so the doubt falls the safe way.
func IsAppClientID(clientID string) bool {
	return strings.HasPrefix(clientID, "Iv")
}

// IsUserToServerToken reports whether a token was minted for a person BY a
// GitHub App: such a token reaches only the repositories the app is
// installed on, so a refusal about someone's own repository means "not
// installed there", not "no access". GitHub says which kind a token is in
// its prefix — ghu_ for user-to-server, gho_ for an OAuth App's, ghp_ for a
// classic personal token, ghs_ for an installation's own.
func IsUserToServerToken(token string) bool {
	return strings.HasPrefix(token, "ghu_")
}

// InstallURL is where the app is installed on an account: the app's own
// page plus /installations/new, which lets the person pick the account and
// the repositories. Asked from the forge once; a lookup that fails answers
// the app's settings path, which always exists.
func (a *GitHubApp) InstallURL(ctx context.Context) string {
	if a.homeURL == "" {
		resp, err := a.get(ctx, http.MethodGet, "/app")
		if err == nil && resp.StatusCode/100 == 2 {
			var body struct {
				HTMLURL string `json:"html_url"`
			}
			if err := decodeJSON(resp, &body); err == nil && body.HTMLURL != "" {
				a.homeURL = body.HTMLURL
			}
		} else if err == nil {
			_ = resp.Body.Close()
		}
	}
	if a.homeURL == "" {
		return "https://github.com/settings/apps (the app's page → Install App)"
	}
	return a.homeURL + "/installations/new"
}

// mint trades the app JWT for an installation token.
func (a *GitHubApp) mint(ctx context.Context, install int64) (appMinted, error) {
	resp, err := a.get(ctx, http.MethodPost, "/app/installations/"+strconv.FormatInt(install, 10)+"/access_tokens")
	if err != nil {
		return appMinted{}, err
	}
	if resp.StatusCode/100 != 2 {
		_ = resp.Body.Close()
		return appMinted{}, fmt.Errorf("github app: token mint for installation %d answered %s", install, resp.Status)
	}
	var body struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := decodeJSON(resp, &body); err != nil {
		return appMinted{}, err
	}
	if body.Token == "" {
		return appMinted{}, fmt.Errorf("github app: installation %d minted an empty token", install)
	}
	return appMinted{value: body.Token, expires: body.ExpiresAt}, nil
}

// get performs one App-authenticated request — the JWT is signed fresh each
// time; it is cheap, and a cached one would just be one more expiry to track.
func (a *GitHubApp) get(ctx context.Context, method, path string) (*http.Response, error) {
	jwt, err := a.jwt()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, a.api+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "aeman")
	return a.client.Do(req)
}

// jwt is the app's proof of identity: RS256 over {iat, exp, iss}, one
// minute of clock skew allowed backwards, well under GitHub's ten-minute
// cap forwards.
func (a *GitHubApp) jwt() (string, error) {
	now := a.now()
	head := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims := base64.RawURLEncoding.EncodeToString(fmt.Appendf(nil, `{"iat":%d,"exp":%d,"iss":%q}`,
		now.Add(-time.Minute).Unix(), now.Add(9*time.Minute).Unix(), a.id))
	signing := head + "." + claims
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, a.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", fmt.Errorf("github app: signing the JWT: %w", err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// GitAuthFor is the git transport credential for one repository: the token
// is asked for on every request, so one renewed between two pushes is
// picked up without re-wiring the remote.
func (a *GitHubApp) GitAuthFor(repoURL string) *AppGitAuth {
	return &AppGitAuth{app: a, url: repoURL}
}

// AppGitAuth stamps a fresh installation token onto each git HTTP request.
type AppGitAuth struct {
	app *GitHubApp
	url string
}

var _ interface {
	transport.AuthMethod
	SetAuth(*http.Request)
} = (*AppGitAuth)(nil)

var _ githttp.AuthMethod = (*AppGitAuth)(nil)

func (g *AppGitAuth) Name() string   { return "github-app" }
func (g *AppGitAuth) String() string { return "github-app" }

// SetAuth cannot return an error; when the mint fails the request goes out
// bare and the operation fails with the forge's own refusal, to be retried
// by the sync — the mint error itself surfaces on the next Token caller.
func (g *AppGitAuth) SetAuth(r *http.Request) {
	tok, err := g.app.Token(r.Context(), g.url)
	if err != nil {
		return
	}
	r.SetBasicAuth("x-access-token", tok)
}

// CanPushGit asks the git transport itself whether a token may push: the
// receive-pack advertisement (GET /info/refs?service=git-receive-pack)
// answers 200 exactly when it may, 403 when it may not, 401 when the token
// is nobody there. The REST permissions block is a different animal — a
// GitHub App's user token can push a repository the REST probe says nothing
// useful about, and the transport is the authority the push will face
// anyway.
func CanPushGit(ctx context.Context, client *http.Client, f Forge, token, repoURL string) (bool, error) {
	target := strings.TrimRight(repoURL, "/")
	if !strings.HasSuffix(target, ".git") {
		target += ".git"
	}
	target += "/info/refs?service=git-receive-pack"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil) //nolint:gosec // the repository the visitor asked to link, under the forge's host
	if err != nil {
		return false, err
	}
	auth := f.GitAuth(token)
	req.SetBasicAuth(auth.Username, auth.Password)
	resp, err := client.Do(req) //nolint:gosec // see above
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("receive-pack advertisement answered %s", resp.Status)
	}
}
