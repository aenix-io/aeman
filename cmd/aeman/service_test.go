package main

import (
	"context"
	"encoding/xml"
	"errors"
	"flag"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aenix-io/aeman/internal/server"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
)

// `aeman service` installs the daemon under the platform's user-level
// service manager. The unit file is the whole contract: it is read by
// launchd or systemd, which hand the daemon none of the environment the
// install was typed in — so what is not in the file does not exist, and
// what is in the file is world-readable to anything running as this user.

// svcRecorder stands in for launchctl and systemctl. It records each
// invocation together with whether the unit file was still on disk at that
// moment, which is how the order of stop-then-remove is pinned.
type svcRecorder struct {
	unitPath  string
	calls     []string
	unitThere []bool
	out       string
	err       error
}

func (r *svcRecorder) run(_ context.Context, name string, args ...string) (string, error) {
	r.calls = append(r.calls, strings.Join(append([]string{name}, args...), " "))
	_, err := os.Stat(r.unitPath)
	r.unitThere = append(r.unitThere, err == nil)
	return r.out, r.err
}

// testMgr is a service manager over a throwaway home, with the subprocess
// seam recorded. goos is a field so Linux CI exercises the plist and a Mac
// exercises the systemd unit.
func testMgr(t *testing.T, goos string) (*serviceMgr, *svcRecorder) {
	t.Helper()
	r := &svcRecorder{}
	m := &serviceMgr{goos: goos, home: t.TempDir(), uid: "501", run: r.run}
	r.unitPath = m.unitPath()
	return m, r
}

// captureOutput runs fn with stdout redirected, because what these verbs
// print IS the product: an operator acts on the sentence, not on the return
// value.
func captureOutput(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		_, _ = io.Copy(&b, r)
		done <- b.String()
	}()
	fn()
	os.Stdout = saved
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}

// testUnit is what an install of a one-repository board produces, with env
// standing in for the installing shell's environment.
func testUnit(t *testing.T, m *serviceMgr, env map[string]string) unit {
	t.Helper()
	cfg := parseGitFlags(t, map[string]string{"AEMAN_REPOS": "board=https://example.com/board.git"}, nil)
	return serviceUnit("/opt/homebrew/bin/aeman", "127.0.0.1:8766", false, cfg,
		func(k string) string { return env[k] }, m.logPath())
}

func writeUnit(t *testing.T, m *serviceMgr, content string) {
	t.Helper()
	p := m.unitPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// launchd starts a unit with a minimal PATH that has neither Homebrew nor
// ~/go/bin on it, and the daemon reads its credential from the forge's CLI.
// Without the installing shell's PATH the daemon comes up and then cannot
// find `gh`.
func TestServiceUnitCarriesPATH(t *testing.T) {
	m, _ := testMgr(t, "darwin")
	env := map[string]string{"PATH": "/opt/homebrew/bin:/usr/bin", "AEMAN_TZ": "Europe/Amsterdam"}
	u := testUnit(t, m, env)
	for name, content := range map[string]string{"plist": launchdPlist(u), "systemd": systemdUnit(u)} {
		if !strings.Contains(content, "/opt/homebrew/bin:/usr/bin") {
			t.Errorf("the %s unit carries no PATH:\n%s", name, content)
		}
		// The board's days live in one zone for everybody, and a unit
		// inherits nothing.
		if !strings.Contains(content, "Europe/Amsterdam") {
			t.Errorf("the %s unit drops AEMAN_TZ:\n%s", name, content)
		}
	}
}

func TestServiceUnitCarriesNoToken(t *testing.T) {
	m, _ := testMgr(t, "darwin")
	cfg := parseGitFlags(t, map[string]string{
		"AEMAN_REPOS":           "board=https://example.com/board.git",
		"AEMAN_GIT_TOKEN":       "ghp_sharedsecret",
		"AEMAN_GIT_TOKEN_BOARD": "ghp_boardsecret",
	}, nil)
	env := map[string]string{
		"PATH":                  "/usr/bin",
		"AEMAN_GIT_TOKEN":       "ghp_sharedsecret",
		"AEMAN_GIT_TOKEN_BOARD": "ghp_boardsecret",
	}
	u := serviceUnit("/usr/local/bin/aeman", "127.0.0.1:8766", false, cfg,
		func(k string) string { return env[k] }, m.logPath())
	for name, content := range map[string]string{"plist": launchdPlist(u), "systemd": systemdUnit(u)} {
		for _, secret := range []string{"ghp_sharedsecret", "ghp_boardsecret"} {
			if strings.Contains(content, secret) {
				t.Errorf("the %s unit carries %s:\n%s", name, secret, content)
			}
		}
		if !strings.Contains(content, "board=https://example.com/board.git") {
			t.Errorf("the %s unit lost the repository, so the check above proves nothing:\n%s", name, content)
		}
	}
}

func TestServiceUnitRunsTheAbsoluteBinaryWithListenAndTheGitFlags(t *testing.T) {
	m, _ := testMgr(t, "darwin")
	u := testUnit(t, m, map[string]string{"PATH": "/usr/bin"})
	for name, content := range map[string]string{"plist": launchdPlist(u), "systemd": systemdUnit(u)} {
		for _, want := range []string{
			"/opt/homebrew/bin/aeman", "mcp", "--listen", "127.0.0.1:8766",
			"--repo", "board=https://example.com/board.git", "--data",
		} {
			if !strings.Contains(content, want) {
				t.Errorf("the %s unit does not carry %q:\n%s", name, want, content)
			}
		}
		// Not typed, so not there: an unauthenticated board on the network
		// must never be something an install picks up by itself.
		if strings.Contains(content, "--listen-insecure") {
			t.Errorf("the %s unit carries --listen-insecure unasked:\n%s", name, content)
		}
	}

	// And when it IS typed it has to arrive, or the daemon refuses its own
	// address at every start and respawns behind an install that only says
	// nothing answered yet.
	cfg := parseGitFlags(t, map[string]string{"AEMAN_REPOS": "board=https://example.com/board.git"}, nil)
	insecure := serviceUnit("/opt/homebrew/bin/aeman", "0.0.0.0:8766", true, cfg,
		func(string) string { return "" }, m.logPath())
	for name, content := range map[string]string{"plist": launchdPlist(insecure), "systemd": systemdUnit(insecure)} {
		if !strings.Contains(content, "--listen-insecure") {
			t.Errorf("the %s unit dropped --listen-insecure after it was typed:\n%s", name, content)
		}
	}
}

// A LaunchAgent, never a system daemon: a root daemon reads the System
// keychain instead of the login keychain the person's credential lives in.
func TestServicePlistIsAUserAgentNotADaemon(t *testing.T) {
	m, r := testMgr(t, "darwin")
	u := testUnit(t, m, map[string]string{"PATH": "/usr/bin"})
	if err := m.install(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	plist, err := os.ReadFile(m.unitPath())
	if err != nil {
		t.Fatal(err)
	}
	content := string(plist)
	// Well-formed XML, not just the right substrings: the committer argument
	// always carries < and >, so every plist ever written depends on
	// xmlText, and launchd refuses a malformed one without saying much.
	if err := xml.Unmarshal(plist, new(struct{})); err != nil {
		t.Fatalf("the plist is not well-formed XML: %v\n%s", err, content)
	}
	if !strings.Contains(m.unitPath(), filepath.Join("Library", "LaunchAgents")) {
		t.Errorf("the plist is not a LaunchAgent: %s", m.unitPath())
	}
	for _, want := range []string{"RunAtLoad", "KeepAlive", "ProcessType", "Background"} {
		if !strings.Contains(content, want) {
			t.Errorf("the plist lacks %s:\n%s", want, content)
		}
	}
	if !strings.Contains(content, filepath.Join(m.home, "Library", "Logs", "aeman", "aeman.log")) {
		t.Errorf("the plist does not log under ~/Library/Logs/aeman:\n%s", content)
	}
	if len(r.calls) != 1 || !strings.Contains(r.calls[0], "launchctl bootstrap gui/501 ") {
		t.Fatalf("calls = %v, want a bootstrap into the login session", r.calls)
	}
	for _, s := range append([]string{content}, r.calls...) {
		if strings.Contains(s, "system/") {
			t.Errorf("something targets the system domain: %s", s)
		}
	}
}

// A service manager that kills the daemon before its stop is done loses the
// commits the queue was holding. The threshold is computed from the budgets
// themselves, not written down: a phase added to the stop must not be able
// to drift past the deadline the unit carries: a phase added to the stop
// must not be able to outlast the manager's patience for it.
func TestServiceUnitStopBudgetOutlivesTheDrain(t *testing.T) {
	m, _ := testMgr(t, "darwin")
	u := testUnit(t, m, map[string]string{"PATH": "/usr/bin"})
	want := int(mcpStopBudget().Seconds())
	budgets := map[string]int{
		"ExitTimeOut":    number(t, regexp.MustCompile(`<key>ExitTimeOut</key>\s*<integer>(\d+)</integer>`), launchdPlist(u)),
		"TimeoutStopSec": number(t, regexp.MustCompile(`TimeoutStopSec=(\d+)`), systemdUnit(u)),
	}
	for name, got := range budgets {
		if got < want {
			t.Errorf("%s = %d, want at least the %ds the whole stop can take", name, got, want)
		}
	}
}

func number(t *testing.T, re *regexp.Regexp, content string) int {
	t.Helper()
	m := re.FindStringSubmatch(content)
	if m == nil {
		t.Fatalf("%s not found in:\n%s", re, content)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// systemd does not see a unit file until it is told to look; enabling one
// it has not read fails.
func TestSystemdReloadsBeforeEnabling(t *testing.T) {
	m, r := testMgr(t, "linux")
	if err := m.install(t.Context(), testUnit(t, m, map[string]string{"PATH": "/usr/bin"})); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 2 {
		t.Fatalf("calls = %v, want a reload and an enable", r.calls)
	}
	if !strings.Contains(r.calls[0], "daemon-reload") {
		t.Errorf("first call = %q, want daemon-reload", r.calls[0])
	}
	if !strings.Contains(r.calls[1], "enable --now aeman.service") {
		t.Errorf("second call = %q, want enable --now", r.calls[1])
	}
	if !strings.Contains(r.calls[0], "--user") || !strings.Contains(r.calls[1], "--user") {
		t.Errorf("calls = %v, want the user manager, not the system one", r.calls)
	}
}

// Removing the unit file under a running daemon leaves it holding the data
// directory with nothing left to stop it by. And a machine with nothing
// installed is the state uninstall exists to reach, not an error.
func TestServiceUninstallStopsBeforeRemovingAndIsIdempotent(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		t.Run(goos, func(t *testing.T) {
			m, r := testMgr(t, goos)
			if err := m.install(t.Context(), testUnit(t, m, map[string]string{"PATH": "/usr/bin"})); err != nil {
				t.Fatal(err)
			}
			installCalls := len(r.calls)

			warn, err := m.uninstall(t.Context())
			if err != nil {
				t.Fatalf("uninstall: %v", err)
			}
			if warn != "" {
				t.Fatalf("a clean stop reported a warning: %s", warn)
			}
			stop := r.calls[installCalls:]
			if len(stop) == 0 {
				t.Fatal("uninstall stopped nothing")
			}
			if !r.unitThere[installCalls] {
				t.Errorf("the unit file was already gone when %q ran", stop[0])
			}
			if _, err := os.Stat(m.unitPath()); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the unit file survived uninstall: %v", err)
			}

			// Nothing installed: no error, and nothing to stop.
			before := len(r.calls)
			if _, err := m.uninstall(t.Context()); err != nil {
				t.Fatalf("second uninstall: %v", err)
			}
			if len(r.calls) != before {
				t.Errorf("the second uninstall ran %v", r.calls[before:])
			}
		})
	}
}

// A unit the manager refused starts nothing, and leaving it on disk makes
// the next install fail on "already exists" for a service that never ran.
func TestServiceInstallTakesBackAUnitTheManagerRefused(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		t.Run(goos, func(t *testing.T) {
			m, r := testMgr(t, goos)
			r.err = errors.New("Load failed: 5: Input/output error")

			err := m.install(t.Context(), testUnit(t, m, map[string]string{"PATH": "/usr/bin"}))
			if err == nil {
				t.Fatal("a refused start was reported as a successful install")
			}
			if _, err := os.Stat(m.unitPath()); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("the unit file outlived the failed install: %v", err)
			}
			// On systemd, an `enable --now` that made the wants symlink and
			// then failed to start leaves it behind; removing only the unit
			// file makes the next daemon-reload complain about a dangling
			// link, so the take-back disables first.
			if goos == "linux" && !strings.Contains(strings.Join(r.calls, "\n"), "--user disable") {
				t.Errorf("the take-back never disabled the unit: %v", r.calls)
			}
			// And the next attempt gets to try, rather than meeting a corpse.
			r.err = nil
			if err := m.install(t.Context(), testUnit(t, m, map[string]string{"PATH": "/usr/bin"})); err != nil {
				t.Fatalf("the retry was refused: %v", err)
			}
		})
	}
}

// A board spanning two organisations is exactly what AEMAN_GIT_TOKEN_<NAME>
// exists for, and a unit file cannot carry it. The install has the tokens in
// hand at that moment, so it is the one place that can say so.
func TestServiceInstallNamesTheDomainsWhoseCredentialCannotTravel(t *testing.T) {
	cfg := parseGitFlags(t, map[string]string{
		"AEMAN_REPOS":            "shared=https://example.com/a.git,closed=https://example.org/b.git",
		"AEMAN_GIT_TOKEN":        "ghp_shared",
		"AEMAN_GIT_TOKEN_CLOSED": "ghp_closed",
	}, nil)
	want := []string{"AEMAN_GIT_TOKEN", "AEMAN_GIT_TOKEN_CLOSED"}
	if got := strandedCredentials(cfg, func(string) string { return "" }); !slices.Equal(got, want) {
		t.Fatalf("strandedCredentials = %v, want %v", got, want)
	}

	// A token with no per-repository companions is stranded just the same:
	// an operator who authenticates by PAT and has no CLI login gets a
	// daemon that answers and never pushes.
	plain := parseGitFlags(t, map[string]string{
		"AEMAN_REPOS":     "shared=https://example.com/a.git,other=https://example.com/b.git",
		"AEMAN_GIT_TOKEN": "ghp_shared",
	}, nil)
	if got := strandedCredentials(plain, func(string) string { return "" }); !slices.Equal(got, []string{"AEMAN_GIT_TOKEN"}) {
		t.Fatalf("strandedCredentials = %v, want the shared token alone", got)
	}

	// A credential inside a repository URL is dropped from the unit, so the
	// install has to name that one too.
	inURL := parseGitFlags(t, map[string]string{
		"AEMAN_REPOS": "board=https://x-access-token:ghp_secret@github.com/acme/board.git",
	}, nil)
	if got := strandedCredentials(inURL, func(string) string { return "" }); len(got) != 1 ||
		!strings.Contains(got[0], "board") {
		t.Fatalf("strandedCredentials = %v, want the URL's credential named", got)
	}

	// None of the non-http forms embeds a credential, so this list stays
	// empty for them. That is a statement about secrets in the URL, not
	// about whether the daemon can authenticate at all: an ssh remote needs
	// an agent a unit does not have, which is its own warning with its own
	// test (TestSSHRemotesAreNamedBecauseAUnitHasNoAgent).
	for _, remote := range []string{
		"ssh://git@github.com/acme/board.git",
		"git@github.com:acme/board.git",
		"file:///srv/boards/board.git",
		"/srv/boards/board.git",
	} {
		cfg := parseGitFlags(t, map[string]string{"AEMAN_REPOS": "board=" + remote}, nil)
		if got := strandedCredentials(cfg, func(string) string { return "" }); len(got) != 0 {
			t.Fatalf("strandedCredentials(%s) = %v, want nothing", remote, got)
		}
	}

	// A GitHub App is a live push credential too, and the one form that
	// still passed without a word: the unit carries no App, so at runtime
	// fillGitToken finds none and falls back to the forge CLI's token — a
	// different identity with a different reach, or nothing at all.
	app := parseGitFlags(t, map[string]string{
		"AEMAN_REPOS":          "board=https://github.com/acme/board.git",
		"AEMAN_GITHUB_APP_ID":  "12345",
		"AEMAN_GITHUB_APP_KEY": string(testAppKeyPEM(t)),
	}, nil)
	if app.App == nil {
		t.Fatal("the fixture did not configure an App, so this proves nothing")
	}
	if got := strandedCredentials(app, func(string) string { return "" }); !slices.Contains(got, "AEMAN_GITHUB_APP_ID") {
		t.Fatalf("strandedCredentials = %v, want the App named", got)
	}

	// The ordinary case: nothing configured by hand, the daemon uses the
	// forge's CLI exactly as `aeman mcp` would, and there is no warning.
	viaCLI := parseGitFlags(t, map[string]string{"AEMAN_REPOS": "shared=https://example.com/a.git"}, nil)
	if got := strandedCredentials(viaCLI, func(string) string { return "" }); len(got) != 0 {
		t.Fatalf("strandedCredentials = %v, want none", got)
	}
}

// `status` asks about the INSTALLED daemon, so the unit's own address is the
// one to probe. An `AEMAN_MCP_LISTEN` left in the operator's shell used to
// win over it, which reported "no answer" about a daemon that was answering
// on the port its unit names. A --listen typed on the command line still
// wins, because then the operator is asking about that address.
func TestServiceStatusAsksTheAddressTheUnitNames(t *testing.T) {
	m, r := testMgr(t, "linux")
	if err := m.install(t.Context(), testUnit(t, m, map[string]string{"PATH": "/usr/bin"})); err != nil {
		t.Fatal(err)
	}
	r.out = "active"
	openServiceMgr = func() (*serviceMgr, error) { return m, nil }
	t.Cleanup(func() { openServiceMgr = newServiceMgr })
	t.Setenv("AEMAN_MCP_LISTEN", "127.0.0.1:9999")

	out := captureOutput(t, func() {
		if err := runService([]string{"status"}); err != nil {
			t.Fatalf("status: %v", err)
		}
	})
	if !strings.Contains(out, "127.0.0.1:8766") {
		t.Errorf("status did not ask the address the unit names: %s", out)
	}
	if strings.Contains(out, "9999") {
		t.Errorf("an inherited AEMAN_MCP_LISTEN outranked the installed unit: %s", out)
	}
}

// An ssh remote authenticates through an agent, and a unit cannot reach the
// operator's: a systemd user unit inherits no SSH_AUTH_SOCK at all, and a
// launchd agent is given launchd's own socket rather than the one the
// installing shell exported. Carrying the value would be worse than saying
// nothing, since that path dies with the session it was typed in, so the
// install has to name the repositories it applies to.
//
// Both scp spellings count, with a user and without: git reads `host:path`
// as ssh, and url.Parse takes that one as a scheme called "github.com" with
// an opaque body rather than refusing it, so a check that only looks at
// schemes and parse failures misses it. A Windows drive letter parses the
// same shape and is not a host.
func TestSSHRemotesAreNamedBecauseAUnitHasNoAgent(t *testing.T) {
	cfg := parseGitFlags(t, map[string]string{
		"AEMAN_REPOS": "board=ssh://git@github.com/acme/board.git,scp=git@github.com:acme/extra.git,alt=git+ssh://git@host/x.git,bare=github.com:acme/third.git",
	}, nil)
	if got, want := sshRemotes(cfg), []string{"board", "scp", "alt", "bare"}; !slices.Equal(got, want) {
		t.Fatalf("sshRemotes = %v, want %v", got, want)
	}

	// Every other form reaches its remote without an agent, and naming one
	// of those sends the reader after a problem they do not have.
	cfg = parseGitFlags(t, map[string]string{
		"AEMAN_REPOS": `board=https://github.com/acme/board.git,local=/srv/boards/b.git,f=file:///srv/x.git,win=C:\Users\op\board.git`,
	}, nil)
	if got := sshRemotes(cfg); len(got) != 0 {
		t.Fatalf("sshRemotes = %v, want nothing", got)
	}
}

// target runs an install's agreement step over a scripted environment.
// Deliberately not through runService: these are the guards that stand
// between the suite and a LaunchAgent installed on whoever runs it, so the
// test for them must not depend on their holding.
func target(t *testing.T, env map[string]string, args []string) (string, error) {
	t.Helper()
	// Never the real cache directory: with AEMAN_DATA unset, config()
	// resolves --data through defaultDataDir(), which is the path a
	// person's own aeman uses.
	if env["AEMAN_DATA"] == "" {
		env["AEMAN_DATA"] = t.TempDir()
	}
	fs := flag.NewFlagSet("service install", flag.ContinueOnError)
	listen := fs.String("listen", env["AEMAN_MCP_LISTEN"], "")
	insecure := fs.Bool("listen-insecure", false, "")
	gf := addGitFlags(fs, func(k string) string { return env[k] })
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	addr, _, err := installTarget(*listen, *insecure, gf)
	return addr, err
}

// A stop the manager did not confirm removes the unit file anyway and can
// leave the daemon running on its data directory. The next uninstall must
// not read that missing file as a stopped process: it is the state the first
// run warned about, and the recovery the install itself prints (uninstall,
// then install) walks into it. Both runs have to name the one lever left.
func TestRunServiceUninstallNamesTheLeverAfterAnUnconfirmedStop(t *testing.T) {
	for _, tc := range []struct{ goos, lever, said string }{
		{"linux", "systemctl --user stop " + systemdUnitName, "Failed to connect to bus: No such file or directory"},
		{"darwin", "launchctl bootout gui/501/" + serviceLabel, "Could not find domain for gui/501"},
	} {
		t.Run(tc.goos, func(t *testing.T) { runUninstallLeverCase(t, tc.goos, tc.lever, tc.said) })
	}
}

func runUninstallLeverCase(t *testing.T, goos, lever, said string) {
	t.Helper()
	m, r := testMgr(t, goos)
	if err := m.install(t.Context(), testUnit(t, m, map[string]string{"PATH": "/usr/bin"})); err != nil {
		t.Fatal(err)
	}
	r.err = errors.New(said)
	openServiceMgr = func() (*serviceMgr, error) { return m, nil }
	t.Cleanup(func() { openServiceMgr = newServiceMgr })

	first := captureOutput(t, func() {
		if err := runService([]string{"uninstall"}); err != nil {
			t.Fatalf("uninstall: %v", err)
		}
	})
	if !strings.Contains(first, "may still be running") {
		t.Fatalf("an unconfirmed stop was reported as a clean one: %s", first)
	}
	if !strings.Contains(first, lever) {
		t.Fatalf("the warning does not name what stops it: %s", first)
	}

	// The unit file is gone now and the daemon may not be.
	second := captureOutput(t, func() {
		if err := runService([]string{"uninstall"}); err != nil {
			t.Fatalf("second uninstall: %v", err)
		}
	})
	if strings.Contains(second, "no longer installed as a service") {
		t.Fatalf("a daemon that may still hold its data directory was reported as uninstalled: %s", second)
	}
	if !strings.Contains(second, lever) {
		t.Fatalf("the second run does not name what stops it: %s", second)
	}
}

// A stop the manager never confirmed is the case that matters: on a session
// with no user bus, systemd cannot stop anything, the unit file goes anyway,
// and the daemon keeps running and holding the data directory with nothing
// left to stop it by. Reporting that as "no longer installed" leaves the
// operator with a puzzle instead of an answer.
func TestServiceUninstallSaysWhenTheStopWasNotConfirmed(t *testing.T) {
	m, r := testMgr(t, "linux")
	if err := m.install(t.Context(), testUnit(t, m, map[string]string{"PATH": "/usr/bin"})); err != nil {
		t.Fatal(err)
	}
	r.err = errors.New("Failed to connect to bus: No such file or directory")

	warn, err := m.uninstall(t.Context())
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if warn == "" {
		t.Fatal("an unconfirmed stop was reported as a clean one")
	}
	if !strings.Contains(warn, "bus") {
		t.Fatalf("the warning drops what the manager said: %s", warn)
	}
	// The unit still goes: leaving it would strand the operator with a unit
	// they cannot remove either.
	if _, err := os.Stat(m.unitPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the unit file survived: %v", err)
	}
}

// On macOS `bootout` exits non-zero for a label that was never loaded and
// for a launchd that cannot be asked at all, and the exit status alone does
// not separate them. Only the first is safe to be quiet about: the second
// leaves the daemon running with its unit already removed, which is the one
// state uninstall must never report as done.
func TestServiceUninstallTellsAnUnloadedLabelFromAnUnreachableLaunchd(t *testing.T) {
	for _, tc := range []struct {
		name     string
		said     string
		wantWarn bool
	}{
		{"never loaded", `Could not find service "io.aenix.aeman" in domain for login`, false},
		{"launchd unreachable", "Could not connect to the launchd domain", true},
		{"said nothing at all", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, r := testMgr(t, "darwin")
			if err := m.install(t.Context(), testUnit(t, m, map[string]string{"PATH": "/usr/bin"})); err != nil {
				t.Fatal(err)
			}
			r.err = errors.New("exit status 113")
			r.out = tc.said

			warn, err := m.uninstall(t.Context())
			if err != nil {
				t.Fatalf("uninstall: %v", err)
			}
			if got := warn != ""; got != tc.wantWarn {
				t.Fatalf("warn = %q, want a warning: %v", warn, tc.wantWarn)
			}
			// The unit goes either way: leaving it would strand the operator
			// with a unit they cannot remove either.
			if _, err := os.Stat(m.unitPath()); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("the unit file survived: %v", err)
			}
		})
	}
}

// On Windows nothing else is worth saying, so the platform refusal has to
// come before the flag checks: "needs the board's repository" would send
// the operator to fix a flag on a system where the command cannot work.
func TestRunServiceInstallOnWindowsSaysSoBeforeAnythingElse(t *testing.T) {
	t.Setenv("AEMAN_REPOS", "") // no board configured, the other refusal
	openServiceMgr = func() (*serviceMgr, error) {
		return &serviceMgr{goos: "windows", home: t.TempDir(), uid: "0",
			run: func(context.Context, string, ...string) (string, error) {
				t.Error("Windows reached the service manager")
				return "", nil
			}}, nil
	}
	t.Cleanup(func() { openServiceMgr = newServiceMgr })

	err := runService([]string{"install"})
	if err == nil {
		t.Fatal("install was accepted on Windows")
	}
	if !strings.Contains(err.Error(), "aeman mcp --listen") {
		t.Fatalf("the refusal is not the platform one: %v", err)
	}
}

func TestServiceInstallRefusesWithoutARepository(t *testing.T) {
	_, err := target(t, map[string]string{}, nil)
	if err == nil {
		t.Fatal("install with no board was accepted")
	}
	if !strings.Contains(err.Error(), "--repo") {
		t.Fatalf("the refusal does not name --repo: %v", err)
	}
}

func TestServiceInstallRefusesANonLoopbackListen(t *testing.T) {
	env := map[string]string{"AEMAN_REPOS": "board=https://example.com/board.git"}
	_, err := target(t, env, []string{"--listen", "0.0.0.0:8766"})
	if err == nil {
		t.Fatal("install on 0.0.0.0 was accepted")
	}
	if !strings.Contains(err.Error(), "--listen-insecure") {
		t.Fatalf("the refusal does not name the way through: %v", err)
	}

	// And the default is the loopback address, not whatever was left unset.
	addr, err := target(t, env, nil)
	if err != nil {
		t.Fatalf("a plain install was refused: %v", err)
	}
	if addr != defaultListenAddr {
		t.Fatalf("address = %q, want %q", addr, defaultListenAddr)
	}
}

// Port 0 is fine for a run somebody is watching, since the log names the
// port the kernel picked — but a unit baked with it starts a daemon nothing
// can be pointed at, and the install would print `http://127.0.0.1:0/mcp`
// as the address to use.
func TestServiceInstallRefusesPortZero(t *testing.T) {
	env := map[string]string{"AEMAN_REPOS": "board=https://example.com/board.git"}
	_, err := target(t, env, []string{"--listen", "127.0.0.1:0"})
	if err == nil {
		t.Fatal("a unit on port 0 was accepted")
	}
	if !strings.Contains(err.Error(), "fixed port") {
		t.Fatalf("the refusal does not say why: %v", err)
	}

	// An empty port is refused earlier and by the address check, which
	// wants a number, so it never reaches the zero test above. The message
	// pins that order: swapped, a port that is not a number would be
	// reported as a missing fixed port.
	_, err = target(t, env, []string{"--listen", "127.0.0.1:"})
	if err == nil {
		t.Fatal("a unit on an empty port was accepted")
	}
	if !strings.Contains(err.Error(), "not a port number") {
		t.Fatalf("the empty port was not refused by the address check: %v", err)
	}
}

// PATH travels because the daemon has to find `gh`; where `gh` keeps its
// token is the same question and travels for the same reason. An operator
// who moved that directory gets a daemon that runs the right binary against
// the wrong config, finds no token, and says nothing about why.
func TestServiceUnitCarriesWhereTheForgeCLIKeepsItsConfig(t *testing.T) {
	m, _ := testMgr(t, "linux")
	set := map[string]string{
		"PATH":            "/usr/bin",
		"GH_CONFIG_DIR":   "/home/op/cfg/gh",
		"GLAB_CONFIG_DIR": "/home/op/cfg/glab-cli",
		"XDG_CONFIG_HOME": "/home/op/cfg",
	}
	u := serviceUnit("/usr/local/bin/aeman", "127.0.0.1:8766", false,
		parseGitFlags(t, map[string]string{"AEMAN_REPOS": "board=https://example.com/b.git"}, nil),
		func(k string) string { return set[k] }, m.logPath())
	for k, want := range map[string]string{"GH_CONFIG_DIR": "/home/op/cfg/gh", "GLAB_CONFIG_DIR": "/home/op/cfg/glab-cli", "XDG_CONFIG_HOME": "/home/op/cfg"} {
		if got := u.Env[k]; got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}

	// Unset stays unset: a unit naming an empty config directory would send
	// the CLI somewhere neither of them meant.
	only := map[string]string{"PATH": "/usr/bin"}
	u = serviceUnit("/usr/local/bin/aeman", "127.0.0.1:8766", false,
		parseGitFlags(t, map[string]string{"AEMAN_REPOS": "board=https://example.com/b.git"}, nil),
		func(k string) string { return only[k] }, m.logPath())
	for _, k := range []string{"GH_CONFIG_DIR", "GLAB_CONFIG_DIR", "XDG_CONFIG_HOME"} {
		if _, ok := u.Env[k]; ok {
			t.Errorf("%s was written into the unit while unset", k)
		}
	}
}

// Over http(s) a username IS the credential half the time: `https://<pat>@host`
// is the standard token-in-URL form, and nothing in the URL tells it apart
// from a person's login. So the whole userinfo is stripped and the whole
// userinfo is called a credential, because the operator who reads "username"
// about a PAT stops looking. On other schemes a login without a password is
// not a secret and never reaches this list at all.
func TestStrandedCredentialsCallsHTTPUserinfoACredential(t *testing.T) {
	none := func(string) string { return "" }
	for _, remote := range []string{
		"https://alice@github.com/acme/board.git",
		"https://ghp_inthisurl@github.com/acme/board.git",
		"https://x-access-token:ghp_x@github.com/acme/board.git",
		"ssh://git:s3cret@github.com/acme/board.git",
	} {
		cfg := parseGitFlags(t, map[string]string{"AEMAN_REPOS": "board=" + remote}, nil)
		got := strandedCredentials(cfg, none)
		if len(got) != 1 || !strings.Contains(got[0], "credential") {
			t.Errorf("strandedCredentials(%s) = %v, want the credential named", remote, got)
		}
		if strings.Contains(strings.Join(got, " "), "username") {
			t.Errorf("%s: userinfo that may be a token was called a username: %v", remote, got)
		}
	}

	// A login with no password on a non-web scheme carries no secret and is
	// kept in the unit, so there is nothing to report as stranded.
	cfg := parseGitFlags(t, map[string]string{"AEMAN_REPOS": "board=ssh://git@github.com/acme/board.git"}, nil)
	if got := strandedCredentials(cfg, none); len(got) != 0 {
		t.Fatalf("strandedCredentials = %v, want nothing for an ssh login", got)
	}
}

// A port something else already holds has to be refused before the unit file
// exists. The daemon fails its bind at every start, while the install prints
// the address of whatever DOES answer there: `/healthz` names no board, so
// another aeman passes for this one and every client that follows the
// printed line writes into the wrong board. Advisory in the same way the
// data-directory claim is, since the port can go in the moment between this
// check and the daemon's own bind.
func TestServiceInstallRefusesAPortSomethingElseHolds(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	taken := ln.Addr().String()
	data := t.TempDir()

	m, _ := testMgr(t, "linux")
	err = m.prepare(data, taken)
	if err == nil {
		t.Fatal("an install over a held port was accepted")
	}
	if !strings.Contains(err.Error(), taken) {
		t.Fatalf("the refusal does not name the address: %v", err)
	}
	if !strings.Contains(err.Error(), "--listen") {
		t.Fatalf("the refusal does not say how to give this board a port of its own: %v", err)
	}

	// Freed, and the install is fine again.
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	if err := m.prepare(data, taken); err != nil {
		t.Fatalf("the port is still refused after the holder let go: %v", err)
	}
}

// A daemon of this same board holds the directory AND the port. The
// directory is what names that case, so it is asked first: told about the
// port instead, the operator would go looking for a second address rather
// than realise this board's own daemon is already running.
func TestServiceInstallNamesTheDirectoryWhenBothAreHeld(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	data := t.TempDir()
	held, err := server.DataDirHold(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Close() })

	m, _ := testMgr(t, "linux")
	err = m.prepare(data, ln.Addr().String())
	if err == nil {
		t.Fatal("an install over a held directory and a held port was accepted")
	}
	if !strings.Contains(err.Error(), data) {
		t.Fatalf("the port was reported before the directory: %v", err)
	}
}

// Installing over a directory another process holds writes a unit that
// starts, dies on the lock, and is respawned for as long as the machine is
// up — after the install has printed success. The refusal belongs before
// anything is written.
func TestServiceInstallRefusesADataDirSomethingElseHolds(t *testing.T) {
	data := t.TempDir()
	held, err := server.DataDirHold(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Close() })

	m, _ := testMgr(t, "linux")
	// Port 0 always binds, so the probe passes and the directory is what
	// this asserts.
	if err := m.prepare(data, "127.0.0.1:0"); err == nil {
		t.Fatal("an install over a held data directory was accepted")
	} else if !strings.Contains(err.Error(), data) {
		t.Fatalf("the refusal does not name the directory: %v", err)
	}

	// Released, and the install is fine again.
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	if err := m.prepare(data, "127.0.0.1:0"); err != nil {
		t.Fatalf("the directory is still refused after the holder let go: %v", err)
	}
}

// The ordinary first install: the directory is not there yet, so nothing
// can be holding it — and checking must not create it, or a refused typo in
// --data leaves a stray behind.
func TestDataDirAvailableOnADirectoryThatIsNotThereYet(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-yet")
	if err := dataDirAvailable(dir); err != nil {
		t.Fatalf("a directory that does not exist was reported as held: %v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("checking created the directory: %v", err)
	}
}

// An upgrade or a reinstall is the ordinary case, and then the daemon being
// replaced is still running and still holding the data directory. The
// refusal has to be about the unit: the directory's own message is written
// for a second CLIENT, and both ways out it offers — point a client at the
// running daemon, or give this process another --data — would have the
// operator stand up a second daemon rather than replace the one they came to
// replace. The order is the contract: a refusal pinned on the inner call
// says nothing about which guard the entry point reaches first.
func TestServiceInstallOverALiveDaemonSaysToUninstall(t *testing.T) {
	m, _ := testMgr(t, "darwin")
	data := t.TempDir()
	held, err := server.DataDirHold(data) // the daemon that is running
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Close() })
	if err := m.install(t.Context(), testUnit(t, m, map[string]string{"PATH": "/usr/bin"})); err != nil {
		t.Fatal(err)
	}

	err = m.prepare(data, "127.0.0.1:0")
	if err == nil {
		t.Fatal("an install over a live daemon was accepted")
	}
	if !strings.Contains(err.Error(), "aeman service uninstall") {
		t.Fatalf("the refusal is not about the unit: %v", err)
	}
	if strings.Contains(err.Error(), "claude mcp add") {
		t.Fatalf("the refusal is the one written for a second client: %v", err)
	}
}

// The manager accepting a unit says nothing about the daemon coming up: a
// busy port or a bad repository leaves it dying at every start and being
// respawned, while install printed success. So install asks the daemon.
func TestServiceInstallWaitsForTheDaemonToAnswer(t *testing.T) {
	m, _ := testMgr(t, "linux")

	ts := httptest.NewServer(testMCPHandler(boardservicetest.New(nil, nil)))
	t.Cleanup(ts.Close)
	live, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if !m.awaitHealthy(live, strings.TrimPrefix(ts.URL, "http://")) {
		t.Fatal("a daemon that was answering was reported as silent")
	}

	// A squatter on the port is one of the cases this exists to catch: it
	// answers 200 on /healthz with something aeman did not write, and
	// counting that as the daemon would have install print success over a
	// unit that restart-loops on the bind.
	squat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "<html>not aeman</html>")
	}))
	t.Cleanup(squat.Close)
	brief, cancelBrief := context.WithTimeout(t.Context(), 700*time.Millisecond)
	defer cancelBrief()
	if m.awaitHealthy(brief, strings.TrimPrefix(squat.URL, "http://")) {
		t.Fatal("something that is not aeman was accepted as the daemon")
	}

	// Nothing there: the wait ends with the deadline rather than hanging on
	// a unit that will never come up.
	dead, cancelDead := context.WithTimeout(t.Context(), 700*time.Millisecond)
	defer cancelDead()
	start := time.Now()
	if m.awaitHealthy(dead, freeAddr(t)) {
		t.Fatal("a closed port was reported as a healthy daemon")
	}
	if waited := time.Since(start); waited > 5*time.Second {
		t.Fatalf("the wait ran %v past its deadline", waited)
	}
}

// The forge's own token variables are resolved after config(), so they are
// not on the GitConfig — and just as absent from the unit. An operator with
// only GH_TOKEN exported and no CLI login is exactly the silent failure the
// warning exists for.
func TestStrandedCredentialsNamesTheForgeTokenVariables(t *testing.T) {
	cfg := parseGitFlags(t, map[string]string{"AEMAN_REPOS": "board=https://github.com/acme/board.git"}, nil)
	shell := map[string]string{"GH_TOKEN": "ghp_fromtheshell"}
	got := strandedCredentials(cfg, func(k string) string { return shell[k] })
	if !slices.Contains(got, "GH_TOKEN") {
		t.Fatalf("strandedCredentials = %v, want GH_TOKEN named", got)
	}
}

// systemd expands %specifiers and $variables in a command line, and quoting
// stops neither: both have to be doubled. A repository URL, a data path or
// a committer name can carry either.
func TestSystemdArgEscapesWhatQuotingDoesNot(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"/usr/local/bin/aeman", "/usr/local/bin/aeman"},
		{"aeman <aeman@localhost>", `"aeman <aeman@localhost>"`},
		{"/data/100%/board", `"/data/100%%/board"`},
		{"/data/$HOME/board", `"/data/$$HOME/board"`},
		// A unit is line-oriented, so a newline cannot be quoted into a
		// command line — it has to be escaped or the unit will not parse.
		{"aeman\n<aeman@localhost>", `"aeman\n<aeman@localhost>"`},
	} {
		if got := systemdArg(tc.in); got != tc.want {
			t.Errorf("systemdArg(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// Environment= is the other dialect: specifiers expand there, variables
	// do not, so a dollar in PATH must survive as itself. That line is as
	// line-oriented as the command is, though, so a newline needs the same
	// escape it gets above — and the values carried into it are no longer
	// just PATH.
	for _, tc := range []struct{ in, want string }{
		{"/opt/$TOOLS/bin", "/opt/$TOOLS/bin"},
		{"/data/100%/cfg", `/data/100%%/cfg`},
		{"/opt/a\nb", `/opt/a\nb`},
		{"/opt/a\rb", `/opt/a\rb`},
	} {
		if got := systemdValue(tc.in); got != tc.want {
			t.Errorf("systemdValue(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Writing over a unit that is already loaded leaves the manager running the
// old command line while the file says otherwise.
func TestServiceInstallRefusesToOverwriteALiveUnit(t *testing.T) {
	m, r := testMgr(t, "darwin")
	u := testUnit(t, m, map[string]string{"PATH": "/usr/bin"})
	if err := m.install(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	before := len(r.calls)
	err := m.install(t.Context(), u)
	if err == nil {
		t.Fatal("the second install overwrote the unit")
	}
	if !strings.Contains(err.Error(), "aeman service uninstall") {
		t.Fatalf("the refusal does not name the way out: %v", err)
	}
	if len(r.calls) != before {
		t.Errorf("the refused install still ran %v", r.calls[before:])
	}
}

func TestServiceIsNotSupportedOnWindows(t *testing.T) {
	r := &svcRecorder{}
	m := &serviceMgr{goos: "windows", home: t.TempDir(), uid: "0", run: r.run}
	verbs := map[string]error{
		"install": m.install(t.Context(), unit{Label: serviceLabel, Bin: `C:\aeman.exe`}),
	}
	_, verbs["uninstall"] = m.uninstall(t.Context())
	_, verbs["status"] = m.status(t.Context(), "")
	for verb, err := range verbs {
		if err == nil {
			t.Errorf("%s was accepted on Windows", verb)
			continue
		}
		// A refusal with no way forward is a dead end: name the command
		// that does the same thing by hand.
		if !strings.Contains(err.Error(), "aeman mcp --listen 127.0.0.1:8766") {
			t.Errorf("%s: the refusal offers nothing to do instead: %v", verb, err)
		}
	}
	if len(r.calls) != 0 {
		t.Errorf("Windows ran %v", r.calls)
	}
}

// The three questions are independent and a person needs all three: a unit
// file on disk does not mean the manager loaded it, and a loaded unit does
// not mean the process behind it can serve.
func TestServiceStatusSeparatesInstalledRunningAndHealthy(t *testing.T) {
	m, r := testMgr(t, "linux")
	r.err = errors.New("inactive")
	writeUnit(t, m, systemdUnit(testUnit(t, m, map[string]string{"PATH": "/usr/bin"})))

	dead, err := m.status(t.Context(), "127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	if !dead.Installed || dead.Running || dead.Healthy {
		t.Fatalf("installed but not running = %+v", dead)
	}
	// Where to look next is the point of asking.
	if out := dead.String(); !strings.Contains(out, dead.Logs) || dead.Logs == "" {
		t.Errorf("the report does not say where the logs are:\n%s", out)
	}

	ts := httptest.NewServer(testMCPHandler(boardservicetest.New(nil, nil)))
	t.Cleanup(ts.Close)
	live, err := m.status(t.Context(), strings.TrimPrefix(ts.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	if !live.Healthy {
		t.Fatalf("a daemon answering /healthz is not reported healthy: %+v", live)
	}
	if live.Installed != dead.Installed || live.Running != dead.Running {
		t.Errorf("the health answer moved the other two lines: %+v then %+v", dead, live)
	}
}

// A daemon that answers is not the same as a daemon that works: `status`
// has to pass on what it called itself, or it prints "ok" over a board that
// stopped syncing a week ago.
func TestServiceStatusRepeatsWhatTheDaemonCallsItself(t *testing.T) {
	m, r := testMgr(t, "linux")
	r.err = errors.New("inactive")
	writeUnit(t, m, systemdUnit(testUnit(t, m, map[string]string{"PATH": "/usr/bin"})))

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"degraded","unpushedAgeSeconds":600}`)
	}))
	t.Cleanup(ts.Close)

	st, err := m.status(t.Context(), strings.TrimPrefix(ts.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	if !st.Healthy {
		t.Fatal("a daemon that answered was reported as silent")
	}
	if st.Health != "degraded" {
		t.Fatalf("Health = %q, want degraded", st.Health)
	}
	if out := st.String(); !strings.Contains(out, "health:  degraded") {
		t.Fatalf("the report hides it:\n%s", out)
	}
}

// The two halves of a RUNNING daemon's report: one that is running and
// degraded still needs the operator pointed at its logs, and one that is
// running and well must not be. Both are asserted here rather than only
// through the not-running path, because an assertion on the absence of
// "health:  ok" means nothing unless something also produces it.
func TestServiceStatusOnARunningDaemon(t *testing.T) {
	m, r := testMgr(t, "linux")
	r.err = nil // the manager answers, so is-active succeeds
	writeUnit(t, m, systemdUnit(testUnit(t, m, map[string]string{"PATH": "/usr/bin"})))

	answer := func(body string) string {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, body)
		}))
		t.Cleanup(ts.Close)
		st, err := m.status(t.Context(), strings.TrimPrefix(ts.URL, "http://"))
		if err != nil {
			t.Fatal(err)
		}
		if !st.Running {
			t.Fatal("the manager answered but Running is false, so this proves nothing")
		}
		// The word is "loaded" deliberately: launchctl print exits 0 for a
		// job it is throttling after repeated crashes, so "running" would
		// claim more than the manager was asked.
		if strings.Contains(st.String(), "manager: running") {
			t.Fatalf("the report says running for what the manager only has loaded:\n%s", st.String())
		}
		return st.String()
	}

	// Running and not well: the logs are where the answer is.
	out := answer(`{"status":"degraded","unpushedAgeSeconds":600}`)
	for _, want := range []string{"manager: loaded", "health:  degraded", "logs:"} {
		if !strings.Contains(out, want) {
			t.Errorf("a running degraded daemon is missing %q:\n%s", want, out)
		}
	}

	// Running and well: no logs line, because there is nothing to look up.
	out = answer(`{"status":"ok","unpushedAgeSeconds":0}`)
	for _, want := range []string{"manager: loaded", "health:  ok"} {
		if !strings.Contains(out, want) {
			t.Errorf("a healthy daemon is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "logs:") {
		t.Errorf("a healthy daemon was sent to read its logs:\n%s", out)
	}
}

// Something answering on the port that is not the daemon must not be read
// as the daemon saying it is well.
func TestServiceStatusDoesNotCallAnUnreadableAnswerOK(t *testing.T) {
	m, r := testMgr(t, "linux")
	r.err = errors.New("inactive")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "<html>whatever else lives on this port</html>")
	}))
	t.Cleanup(ts.Close)

	st, err := m.status(t.Context(), strings.TrimPrefix(ts.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	if out := st.String(); strings.Contains(out, "health:  ok") {
		t.Fatalf("an answer nothing could read was reported as ok:\n%s", out)
	}
}

// Asking after the daemon should not mean retyping the address the install
// already recorded.
func TestStatusReadsTheAddressBackFromTheUnit(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		t.Run(goos, func(t *testing.T) {
			m, r := testMgr(t, goos)
			r.err = errors.New("not loaded")
			cfg := parseGitFlags(t, map[string]string{"AEMAN_REPOS": "board=https://example.com/board.git"}, nil)
			u := serviceUnit("/usr/local/bin/aeman", "127.0.0.1:9999", false, cfg,
				func(string) string { return "" }, m.logPath())
			render := systemdUnit
			if goos == "darwin" {
				render = launchdPlist
			}
			if got := addrFromUnit(render(u)); got != "127.0.0.1:9999" {
				t.Fatalf("addrFromUnit = %q, want the address the unit runs on", got)
			}
			writeUnit(t, m, render(u))
			st, err := m.status(t.Context(), "")
			if err != nil {
				t.Fatal(err)
			}
			if st.Addr != "127.0.0.1:9999" {
				t.Fatalf("status address = %q, want the one in the unit", st.Addr)
			}
		})
	}
}

// A version-managed install puts a symlink on PATH and swings it at every
// upgrade. The unit has to name the symlink: os.Executable reads through it
// on Linux, and a unit pointing at /opt/aeman-1.2.3/aeman fails 203/EXEC the
// moment that version is removed, which Restart=always then retries forever.
func TestServiceInstallsTheSymlinkNotTheVersionBehindIt(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "aeman-1.2.3")
	if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "aeman")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if got := stableBinaryPath(link, real); got != link {
		t.Errorf("stableBinaryPath = %q, want the symlink %q", got, link)
	}

	// A different aeman earlier on PATH is not the process doing the
	// install, so the binary actually running stands.
	other := filepath.Join(dir, "other")
	if err := os.WriteFile(other, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := stableBinaryPath(other, real); got != real {
		t.Errorf("stableBinaryPath = %q, want the running binary %q", got, real)
	}
	// Nothing by that name any more: fall back rather than guess.
	if got := stableBinaryPath(filepath.Join(dir, "gone"), real); got != real {
		t.Errorf("stableBinaryPath = %q, want %q", got, real)
	}
}

// The whole install, driven through the real entry point. Every other test
// on this path calls an inner helper, and a guard pinned on an inner
// helper says nothing about the order the entry point applies its guards
// in — a refusal can be pinned and still be unreachable. This one asserts
// the ORDER of what `runService("install")` does, and that a refusal part
// way through leaves no trace of any later step.
func TestRunServiceInstallDoesItsStepsInOrder(t *testing.T) {
	home := t.TempDir()
	data := t.TempDir()
	// The address has to be free when `prepare` looks and answering when the
	// health wait does, which is the production sequence: the manager starts
	// the daemon between them. So the stand-in daemon is started by the fake
	// manager, on the enable it is handed, rather than before the install.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listen := probe.Addr().String()
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		"AEMAN_REPOS":      "board=https://example.com/board.git",
		"AEMAN_DATA":       data,
		"AEMAN_MCP_LISTEN": listen,
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
	// Nothing else may be inherited into the flag set from whoever runs the
	// suite, or the assertions below describe their machine, not the code.
	for _, k := range []string{"GH_TOKEN", "GITHUB_TOKEN", "GITLAB_TOKEN", "AEMAN_GIT_TOKEN", "AEMAN_GITHUB_APP_ID"} {
		t.Setenv(k, "")
	}

	// One recorder across both calls, so the second install's effect on it
	// can be compared with the first's.
	r := &svcRecorder{}
	start := func(ctx context.Context, name string, args ...string) (string, error) {
		out, err := r.run(ctx, name, args...)
		if strings.Contains(strings.Join(args, " "), "enable --now") {
			ln, lerr := net.Listen("tcp", listen)
			if lerr != nil {
				t.Fatalf("the daemon's address was taken between the check and the start: %v", lerr)
			}
			ts := httptest.NewUnstartedServer(testMCPHandler(boardservicetest.New(nil, nil)))
			_ = ts.Listener.Close()
			ts.Listener = ln
			ts.Start()
			t.Cleanup(ts.Close)
		}
		return out, err
	}
	mgr := &serviceMgr{goos: "linux", home: home, uid: "501", run: start}
	r.unitPath = mgr.unitPath()
	openServiceMgr = func() (*serviceMgr, error) { return mgr, nil }
	t.Cleanup(func() { openServiceMgr = newServiceMgr })

	if err := runService([]string{"install"}); err != nil {
		t.Fatalf("install: %v", err)
	}

	// Step order, read off the manager: the unit was written, then handed to
	// systemd, reload before enable.
	if len(r.calls) != 2 || !strings.Contains(r.calls[0], "daemon-reload") || !strings.Contains(r.calls[1], "enable --now") {
		t.Fatalf("manager calls = %v, want reload then enable", r.calls)
	}
	// The unit exists, under the temp home and nowhere else.
	unit, err := os.ReadFile(mgr.unitPath())
	if err != nil {
		t.Fatalf("the unit was not written: %v", err)
	}
	if !strings.HasPrefix(mgr.unitPath(), home) {
		t.Fatalf("the unit went outside the temp home: %s", mgr.unitPath())
	}
	// --listen came from the environment, since no flag was given.
	if got := addrFromUnit(string(unit)); got != listen {
		t.Fatalf("--listen in the unit = %q, want %q from AEMAN_MCP_LISTEN", got, listen)
	}
	// And the resolved storage flags are in it, which is what makes the
	// unit runnable without the installing shell.
	for _, want := range []string{"board=https://example.com/board.git", data} {
		if !strings.Contains(string(unit), want) {
			t.Errorf("the unit does not carry %q", want)
		}
	}

	// A refusal at the prepare step must leave nothing behind it: no unit
	// rewritten, no manager call, no health probe.
	held, err := server.DataDirHold(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Close() })
	before := len(r.calls)
	if err := runService([]string{"install"}); err == nil {
		t.Fatal("a second install over a live unit and a held directory was accepted")
	}
	if len(r.calls) != before {
		t.Fatalf("a refused install still called the manager: %v", r.calls[before:])
	}
}

// The verb is a verb: a leading flag is not one, and `help` prints usage
// rather than an error about an unknown verb.
func TestRunServiceVerbDispatch(t *testing.T) {
	openServiceMgr = func() (*serviceMgr, error) {
		t.Error("a bad verb reached the service manager")
		return nil, errors.New("must not be called")
	}
	t.Cleanup(func() { openServiceMgr = newServiceMgr })

	if err := runService([]string{"help"}); err != nil {
		t.Errorf("help: %v", err)
	}
	if err := runService([]string{"--help"}); err != nil {
		t.Errorf("--help: %v", err)
	}
	if err := runService(nil); err == nil {
		t.Error("no verb at all was accepted")
	}
	err := runService([]string{"reinstall"})
	if err == nil || !strings.Contains(err.Error(), "install, uninstall or status") {
		t.Errorf("an unknown verb does not name the real ones: %v", err)
	}
}
