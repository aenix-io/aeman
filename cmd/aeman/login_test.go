package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aenix-io/aeman/internal/forge"
	"github.com/aenix-io/aeman/internal/tokenstore"
)

// piped is stdin as a command gets it when a token is piped in: no
// terminal, so no prompt and no hidden read.
func piped(s string) tokenInput { return tokenInput{r: strings.NewReader(s)} }

// The token lands under the forge instance it belongs to, and the person is
// told whose it is — the forge answered before anything was written, so the
// login on screen is the one the commits will carry.
func TestLoginStoresTheTokenUnderTheForgeHost(t *testing.T) {
	f, client := fakeForge(t, map[string]string{"ghp_typed": "alice"})
	store := tokenstore.NewFake()
	var out strings.Builder

	if err := login(context.Background(), f, client, store, "github.com", piped("ghp_typed\n"), &out); err != nil {
		t.Fatal(err)
	}
	if tok, err := store.Get("github.com"); err != nil || tok != "ghp_typed" {
		t.Fatalf("stored = %q, %v", tok, err)
	}
	if !strings.Contains(out.String(), "alice") || !strings.Contains(out.String(), "github.com") {
		t.Fatalf("output = %q; want the login and the account", out.String())
	}
}

// A token the forge refuses is not stored. An item holding a typo is worse
// than no item at all: every later command finds it, stops looking, and
// fails on a credential nobody can see.
func TestLoginRejectsBadTokenWithoutStoring(t *testing.T) {
	f, client := fakeForge(t, map[string]string{"ghp_good": "alice"})
	store := tokenstore.NewFake()
	var out strings.Builder

	err := login(context.Background(), f, client, store, "github.com", piped("ghp_typo\n"), &out)
	if err == nil || !strings.Contains(err.Error(), "GitHub") {
		t.Fatalf("error = %v; want one naming the forge", err)
	}
	if store.Sets != 0 {
		t.Fatalf("the store was written %d times after a refusal, want 0", store.Sets)
	}
	if out.String() != "" {
		t.Fatalf("output = %q; want nothing said on a refusal", out.String())
	}
}

// `gh auth token | aeman login` is the migration path off a wrapper script,
// so a token arrives on standard input with a newline and whatever the pipe
// carries after it. The first line is the token; there is no terminal, so
// nothing is prompted for.
func TestLoginReadsOneLineFromPipedStdin(t *testing.T) {
	f, client := fakeForge(t, map[string]string{"ghp_x": "alice"})
	store := tokenstore.NewFake()
	var out strings.Builder

	if err := login(context.Background(), f, client, store, "github.com", piped("ghp_x\nrubbish\n"), &out); err != nil {
		t.Fatal(err)
	}
	if tok, _ := store.Get("github.com"); tok != "ghp_x" {
		t.Fatalf("stored = %q, want the first line only", tok)
	}
	if strings.Contains(out.String(), "Token for") {
		t.Fatalf("output = %q; a pipe must not be prompted", out.String())
	}
}

// On a terminal the token is asked for with the echo off, and it never
// reaches the screen — not in the prompt, not in the confirmation.
func TestLoginPromptsHiddenOnATerminal(t *testing.T) {
	f, client := fakeForge(t, map[string]string{"ghp_typed": "alice"})
	store := tokenstore.NewFake()
	var out strings.Builder
	reads := 0
	var prompt strings.Builder
	in := tokenInput{tty: true, prompt: &prompt, readPass: func() (string, error) { reads++; return "ghp_typed\n", nil }}

	if err := login(context.Background(), f, client, store, "github.com", in, &out); err != nil {
		t.Fatal(err)
	}
	if reads != 1 {
		t.Fatalf("the hidden read ran %d times, want 1", reads)
	}
	// The prompt goes to its own writer — stderr in the real command — so a
	// redirected stdout still shows the question and holds only the result.
	if !strings.Contains(prompt.String(), "Token for github.com (input hidden): ") {
		t.Fatalf("prompt = %q; want the question", prompt.String())
	}
	if strings.Contains(out.String(), "Token for github.com") {
		t.Fatalf("output = %q; the prompt does not belong on stdout", out.String())
	}
	if strings.Contains(out.String(), "ghp_typed") || strings.Contains(prompt.String(), "ghp_typed") {
		t.Fatalf("out = %q, prompt = %q; the token must never be echoed", out.String(), prompt.String())
	}
}

// Enter on an empty prompt, or an empty pipe, stores nothing and says how a
// token is given — both ways, since which one applies depends on how the
// command was started.
func TestLoginRefusesAnEmptyToken(t *testing.T) {
	f, client := fakeForge(t, map[string]string{"ghp_good": "alice"})
	for _, in := range []tokenInput{piped("\n"), piped(""), {tty: true, readPass: func() (string, error) { return "   ", nil }}} {
		store := tokenstore.NewFake()
		var out strings.Builder
		err := login(context.Background(), f, client, store, "github.com", in, &out)
		if err == nil || !strings.Contains(err.Error(), "aeman login") {
			t.Fatalf("error = %v; want one naming both ways of giving a token", err)
		}
		if store.Sets != 0 {
			t.Fatalf("the store was written %d times for an empty token, want 0", store.Sets)
		}
	}
}

// The account a login writes is the forge instance the board lives on: the
// primary repository's host, else a self-hosted GitLab named on its own,
// else github.com. A GitLab asked for with no host at all is still refused.
func TestLoginAccountIsTheForgeHost(t *testing.T) {
	target := func(args ...string) (forge.Forge, string, error) {
		fs := flag.NewFlagSet("login", flag.ContinueOnError)
		gf := addGitFlags(fs, func(string) string { return "" })
		if err := fs.Parse(args); err != nil {
			t.Fatal(err)
		}
		return gf.forgeTarget()
	}
	cases := []struct {
		args []string
		kind forge.Kind
		host string
	}{
		{[]string{"--repo", "https://github.com/acme/board.git"}, forge.GitHub, "github.com"},
		{[]string{"--repo", "https://gitlab.example.org/a/b.git"}, forge.GitLab, "gitlab.example.org"},
		{[]string{"--gitlab-url", "https://gitlab.example.org"}, forge.GitLab, "gitlab.example.org"},
		{nil, forge.GitHub, "github.com"},
	}
	for _, tc := range cases {
		f, host, err := target(tc.args...)
		if err != nil || f.Kind() != tc.kind || host != tc.host {
			t.Errorf("%v: forge = %v, host = %q, err = %v; want %s and %q", tc.args, f, host, err, tc.kind, tc.host)
		}
	}
	if _, _, err := target("--forge", "gitlab"); err == nil {
		t.Fatal("a GitLab with no host to log in to must be refused")
	}
}

// With no usable secret store there is nowhere to put the token, and aeman
// does not invent one: no dotfile, no plaintext anywhere. The error names
// the variables that work without a store, and keeps what the store said
// after them — the store cannot say WHY it failed in any form this code
// can branch on (on macOS a locked keychain is just whatever
// /usr/bin/security printed), so the tool's own words are the only thing
// a person has to debug it with.
func TestLoginFailsWhenTheKeychainIsUnavailable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	f, client := fakeForge(t, map[string]string{"ghp_typed": "alice"})
	store := tokenstore.NewFake()
	// What a locked login keychain actually produces, not a sentinel this
	// package invented: security(1) classifies nothing but a missing item.
	store.Err = errors.New("keychain: darwin cli: security add-generic-password: exit status 51")
	var out strings.Builder

	err := login(context.Background(), f, client, store, "github.com", piped("ghp_typed\n"), &out)
	if err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("error = %v; want one naming the variables that stand in for the store", err)
	}
	if !strings.Contains(err.Error(), "exit status 51") {
		t.Fatalf("error = %v; want the store's own words kept after the guidance", err)
	}
	// Not AEMAN_GIT_TOKEN: it would push and leave the commits unattributed.
	if strings.Contains(err.Error(), "AEMAN_GIT_TOKEN") {
		t.Fatalf("error = %v; AEMAN_GIT_TOKEN is a push credential, not an identity", err)
	}
	left, rerr := os.ReadDir(home)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if len(left) != 0 {
		t.Fatalf("%d files written under HOME; a failed login must leave the token nowhere", len(left))
	}
}

// Logging out of a host that was never logged in to is the state asked for,
// not a failure — and nothing is deleted to reach it.
func TestLogoutOfNothing(t *testing.T) {
	store := tokenstore.NewFake()
	var out strings.Builder

	if err := logout(store, "github.com", &out); err != nil {
		t.Fatalf("logout of nothing = %v, want nil", err)
	}
	// The removal runs even here. A blank item also reads as absent, and
	// skipping the delete on that answer would leave one in the store that
	// no command could remove.
	if store.Deletes != 1 {
		t.Fatalf("%d deletes, want 1: the removal is not conditional on the read", store.Deletes)
	}
	if !strings.Contains(out.String(), "No usable token was stored for github.com") {
		t.Fatalf("output = %q", out.String())
	}
}

// deniedReads is a store that holds items but will not hand a value over
// — a keychain that answers the read with the tool's own failure rather
// than with a value or a missing-item code.
type deniedReads struct{ *tokenstore.Fake }

func (deniedReads) Get(string) (string, error) {
	return "", errors.New("keychain: darwin cli: security find-generic-password: exit status 36")
}

// Only "there is no item" ends a logout early. Any other refusal from the
// store still gets the removal attempted, because a read that fails does
// not say the item is absent — and if the removal fails too, that error is
// what comes back rather than a claim that the token is gone.
func TestLogoutTriesTheRemovalWhenTheStoreRefusesToRead(t *testing.T) {
	store := deniedReads{tokenstore.NewFake().Put("github.com", "ghp_x")}
	var out strings.Builder

	if err := logout(store, "github.com", &out); err != nil {
		t.Fatalf("logout = %v, want the delete to have been attempted", err)
	}
	if store.Deletes != 1 {
		t.Fatalf("%d deletes, want 1", store.Deletes)
	}
	if _, ok := store.Fake.Get("github.com"); ok == nil {
		t.Fatal("the item is still there")
	}

	failing := deniedReads{tokenstore.NewFake()}
	refused := errors.New("keychain: darwin cli: security delete-generic-password: exit status 36")
	failing.Err = refused
	if err := logout(failing, "github.com", &strings.Builder{}); !errors.Is(err, refused) {
		t.Fatalf("error = %v; a failing delete must surface, not be reported as success", err)
	}
}

// Logging out takes the one host's item and says where it was, so the
// person can check the store themselves; the other hosts keep theirs.
func TestLogoutRemovesTheItem(t *testing.T) {
	store := tokenstore.NewFake().Put("github.com", "ghp_x").Put("gitlab.example.org", "glpat_y")
	var out strings.Builder

	if err := logout(store, "github.com", &out); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("github.com"); err == nil {
		t.Fatal("the item is still there")
	}
	if tok, err := store.Get("gitlab.example.org"); err != nil || tok != "glpat_y" {
		t.Fatalf("the other forge's token = %q, %v; want it untouched", tok, err)
	}
	if !strings.Contains(out.String(), tokenstore.Service) || !strings.Contains(out.String(), "github.com") {
		t.Fatalf("output = %q; want the service and the account", out.String())
	}
}

// A signal at the prompt does BOTH halves: the terminal goes back to how
// it was, and the signal is handed on so the command actually ends. Doing
// only the first is worse than doing nothing — it makes `aeman login`
// unkillable by Ctrl-C with the echo freshly back on under a live read,
// so the next thing typed, which is the token, goes to the scrollback.
func TestASignalRestoresTheTerminalAndIsHandedOn(t *testing.T) {
	sigs := make(chan os.Signal, 1)
	restored := make(chan struct{})
	passed := make(chan os.Signal, 1)

	stop := onFirstSignal(sigs, func() { close(restored) }, func(s os.Signal) { passed <- s })
	defer stop()
	sigs <- os.Interrupt

	select {
	case <-restored:
	case <-time.After(time.Second):
		t.Fatal("the terminal was not restored")
	}
	select {
	case got := <-passed:
		if got != os.Interrupt {
			t.Fatalf("passed on %v, want the signal that arrived", got)
		}
	case <-time.After(time.Second):
		t.Fatal("the signal was swallowed: the command would never end")
	}
}

// Nothing arrives on the ordinary path, so neither half runs and the stop
// returns without waiting on anything.
func TestNoSignalRunsNeitherHalf(t *testing.T) {
	ran := false
	stop := onFirstSignal(make(chan os.Signal), func() { ran = true }, func(os.Signal) { ran = true })
	stop()
	if ran {
		t.Fatal("nothing should have run without a signal")
	}
}

// The guard is a no-op where there is no terminal to restore, which is
// every piped run and every test — so `aeman login` under a pipe installs
// no signal handler and the returned stop is safe to call.
func TestTerminalRestoreDoesNothingWithoutATerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })

	stop := restoreTerminalOnInterrupt(int(r.Fd()))
	if stop == nil {
		t.Fatal("stop must never be nil; runLogin defers it")
	}
	stop()
	stop() // idempotent: the deferred call must not panic on a second run
}

// A forge that cannot be reached is not a rejection, and neither one
// stores anything. The 401 arm was already pinned; this is the other, and
// it matters more for a machine behind a VPN that is not up — the case
// docs/api.md sends people to the environment variables for.
func TestLoginStoresNothingWhenTheForgeCannotBeReached(t *testing.T) {
	// Nothing is listening there, so User fails without an HTTP answer.
	f := forge.NewGitHubAt("http://127.0.0.1:1")
	store := tokenstore.NewFake()
	var out strings.Builder

	err := login(context.Background(), f, &http.Client{Timeout: time.Second}, store, "github.com", piped("ghp_typed\n"), &out)
	if err == nil {
		t.Fatal("an unreachable forge must fail the login")
	}
	if store.Sets != 0 {
		t.Fatalf("the store was written %d times; nothing may be stored before the forge has answered", store.Sets)
	}
	if out.String() != "" {
		t.Fatalf("output = %q; want nothing said", out.String())
	}
}

// The stop is called once by runLogin's defer, and the guard above says
// calling it twice is safe — so it has to be, on the branch that actually
// installs a handler rather than only on the no-terminal short-circuit.
func TestTheSignalWatchStopIsIdempotent(t *testing.T) {
	stop := onFirstSignal(make(chan os.Signal), func() {}, func(os.Signal) {})
	stop()
	stop() // must not panic: closing a closed channel would
}

// A token given as an argument is refused, not quietly ignored.
// `aeman login ghp_...` is the natural first attempt and it is the thing
// this command exists to prevent: the argument is in the shell's history
// and in every ps listing before the command starts. The refusal must not
// repeat it, which would put it in the terminal a second time.
//
// The parse is tested on its own because the commands around it build the
// real OS keychain, and logout deletes from it without asking.
func TestATokenGivenAsAnArgumentIsRefused(t *testing.T) {
	const secret = "ghp_typed_on_the_command_line"
	repo := []string{"--repo", "board=https://github.com/o/r"}
	noEnv := func(string) string { return "" }
	for _, tc := range []struct{ name, hint string }{{"login", loginHint}, {"logout", logoutHint}} {
		name := tc.name
		_, _, err := forgeTargetFor(name, tc.hint, noEnv, append(append([]string{}, repo...), secret))
		if err == nil {
			t.Fatalf("%s: a positional argument must be refused", name)
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("%s: the error repeats the argument: %v", name, err)
		}
		// logout takes no token at all, so it must not be answered with
		// how to supply one.
		if name == "logout" && strings.Contains(err.Error(), "aeman login`") {
			t.Fatalf("logout is told how to pipe in a token it never reads: %v", err)
		}
		if _, host, err := forgeTargetFor(name, tc.hint, noEnv, repo); err != nil || host != "github.com" {
			t.Fatalf("%s without arguments: %q, %v", name, host, err)
		}
	}
}

// The question goes to stderr, so `aeman login > file` still shows it and
// the file holds only what the command produced. Every other test here
// supplies its own writer, which pins that the prompt goes to the writer
// but not which writer the command uses — swapping stdinToken to stdout
// leaves them all green. stdinToken opens no store, so it is safe to build
// here.
func TestThePromptGoesToStderr(t *testing.T) {
	if in := stdinToken(); in.prompt != os.Stderr {
		t.Fatal("the prompt must go to stderr, or redirecting stdout hides the question and captures it")
	}
}
